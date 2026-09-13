package secretrepo

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"math/big"
	"sync/atomic"
	"time"
)

const (
	defaultOAuthRefreshConcurrency = 8
	oauthRefreshBackoffInitial     = 25 * time.Millisecond
	oauthRefreshBackoffMaximum     = 250 * time.Millisecond
	poolSaturationScale            = 1_000_000
)

var (
	lockWaitBounds = [...]time.Duration{
		10 * time.Millisecond,
		25 * time.Millisecond,
		50 * time.Millisecond,
		100 * time.Millisecond,
		250 * time.Millisecond,
		500 * time.Millisecond,
		time.Second,
	}
	refreshLatencyBounds = [...]time.Duration{
		100 * time.Millisecond,
		250 * time.Millisecond,
		500 * time.Millisecond,
		time.Second,
		2500 * time.Millisecond,
		5 * time.Second,
		15 * time.Second,
	}
)

// DurationBucket is one cumulative, fixed-bound latency bucket.
type DurationBucket struct {
	LessThanOrEqual time.Duration
	Count           uint64
}

// DurationMetrics is a monotonic latency histogram snapshot. The final count
// also includes values above the largest bucket.
type DurationMetrics struct {
	Count   uint64
	Total   time.Duration
	Buckets []DurationBucket
}

// OAuthRefreshPoolMetrics is the current database/sql pool state plus the
// highest saturation observed on an OAuth refresh path. Saturation is parts per
// million so exporters do not need floating-point arithmetic.
type OAuthRefreshPoolMetrics struct {
	MaxOpenConnections int
	OpenConnections    int
	InUseConnections   int
	IdleConnections    int
	WaitCount          int64
	WaitDuration       time.Duration
	SaturationPPM      uint64
	PeakSaturationPPM  uint64
}

// OAuthRefreshRuntimeMetrics is a bounded, label-free process-local snapshot
// for OAuth refresh admission, distributed-lock wait, provider/local rotation
// outcome, and the shared PostgreSQL pool.
type OAuthRefreshRuntimeMetrics struct {
	ConcurrencyLimit  int
	InFlight          int64
	PeakInFlight      int64
	Waiting           int64
	AdmissionWaits    uint64
	AdmissionCanceled uint64
	AdmissionWait     DurationMetrics
	LockAttempts      uint64
	LockContentions   uint64
	LockWait          DurationMetrics
	RefreshSuccesses  uint64
	RefreshFailures   uint64
	RefreshLatency    DurationMetrics
	Pool              OAuthRefreshPoolMetrics
}

type atomicDurationMetrics struct {
	bounds  []time.Duration
	count   atomic.Uint64
	totalNS atomic.Int64
	buckets []atomic.Uint64
}

func newAtomicDurationMetrics(bounds []time.Duration) atomicDurationMetrics {
	return atomicDurationMetrics{bounds: append([]time.Duration(nil), bounds...), buckets: make([]atomic.Uint64, len(bounds))}
}

func (metrics *atomicDurationMetrics) record(value time.Duration) {
	if metrics == nil {
		return
	}
	if value < 0 {
		value = 0
	}
	metrics.count.Add(1)
	metrics.totalNS.Add(int64(value))
	for index, bound := range metrics.bounds {
		if value <= bound {
			metrics.buckets[index].Add(1)
			break
		}
	}
}

func (metrics *atomicDurationMetrics) snapshot() DurationMetrics {
	if metrics == nil {
		return DurationMetrics{}
	}
	buckets := make([]DurationBucket, len(metrics.bounds))
	var cumulative uint64
	for index, bound := range metrics.bounds {
		cumulative += metrics.buckets[index].Load()
		buckets[index] = DurationBucket{LessThanOrEqual: bound, Count: cumulative}
	}
	return DurationMetrics{Count: metrics.count.Load(), Total: time.Duration(metrics.totalNS.Load()), Buckets: buckets}
}

type oauthRefreshRuntime struct {
	database           *sql.DB
	slots              chan struct{}
	backoff            func(uint) time.Duration
	inFlight           atomic.Int64
	peakInFlight       atomic.Int64
	waiting            atomic.Int64
	admissionWaits     atomic.Uint64
	admissionCanceled  atomic.Uint64
	admissionWait      atomicDurationMetrics
	lockAttempts       atomic.Uint64
	lockContentions    atomic.Uint64
	lockWait           atomicDurationMetrics
	refreshSuccesses   atomic.Uint64
	refreshFailures    atomic.Uint64
	refreshLatency     atomicDurationMetrics
	peakPoolSaturation atomic.Uint64
}

func newOAuthRefreshRuntime(database *sql.DB) *oauthRefreshRuntime {
	capacity := defaultOAuthRefreshConcurrency
	if maximum := database.Stats().MaxOpenConnections; maximum > 0 {
		if maximum == 1 {
			capacity = 1
		} else if maximum-1 < capacity {
			capacity = maximum - 1
		}
	}
	return &oauthRefreshRuntime{
		database:       database,
		slots:          make(chan struct{}, capacity),
		backoff:        jitteredOAuthRefreshBackoff,
		admissionWait:  newAtomicDurationMetrics(lockWaitBounds[:]),
		lockWait:       newAtomicDurationMetrics(lockWaitBounds[:]),
		refreshLatency: newAtomicDurationMetrics(refreshLatencyBounds[:]),
	}
}

func (runtime *oauthRefreshRuntime) acquire(ctx context.Context) (func(), error) {
	if runtime == nil || runtime.database == nil || runtime.slots == nil || ctx == nil {
		return nil, fmt.Errorf("oauth refresh admission is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case runtime.slots <- struct{}{}:
		runtime.admitted()
		return runtime.release, nil
	default:
	}
	runtime.admissionWaits.Add(1)
	runtime.waiting.Add(1)
	started := time.Now()
	select {
	case runtime.slots <- struct{}{}:
		runtime.waiting.Add(-1)
		runtime.admissionWait.record(time.Since(started))
		runtime.admitted()
		return runtime.release, nil
	case <-ctx.Done():
		runtime.waiting.Add(-1)
		runtime.admissionCanceled.Add(1)
		runtime.admissionWait.record(time.Since(started))
		return nil, ctx.Err()
	}
}

func (runtime *oauthRefreshRuntime) admitted() {
	current := runtime.inFlight.Add(1)
	for {
		peak := runtime.peakInFlight.Load()
		if current <= peak || runtime.peakInFlight.CompareAndSwap(peak, current) {
			break
		}
	}
	runtime.samplePool()
}

func (runtime *oauthRefreshRuntime) release() {
	// Retire the completed operation before making its slot visible to a
	// waiter. Reversing these steps lets the waiter increment inFlight first
	// and records a false peak above the channel's concurrency limit.
	runtime.inFlight.Add(-1)
	<-runtime.slots
	runtime.samplePool()
}

func (runtime *oauthRefreshRuntime) backoffWait(ctx context.Context, retry uint) error {
	delay := runtime.backoff(retry)
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func jitteredOAuthRefreshBackoff(retry uint) time.Duration {
	ceiling := oauthRefreshBackoffInitial
	for retry > 0 && ceiling < oauthRefreshBackoffMaximum {
		ceiling *= 2
		if ceiling > oauthRefreshBackoffMaximum {
			ceiling = oauthRefreshBackoffMaximum
		}
		retry--
	}
	floor := ceiling / 2
	random, err := rand.Int(rand.Reader, big.NewInt(int64(ceiling-floor)+1))
	if err != nil {
		return floor
	}
	return floor + time.Duration(random.Int64())
}

func (runtime *oauthRefreshRuntime) samplePool() OAuthRefreshPoolMetrics {
	stats := runtime.database.Stats()
	saturation := uint64(0)
	if stats.MaxOpenConnections > 0 && stats.InUse > 0 {
		saturation = uint64(stats.InUse) * poolSaturationScale / uint64(stats.MaxOpenConnections)
	}
	for {
		peak := runtime.peakPoolSaturation.Load()
		if saturation <= peak || runtime.peakPoolSaturation.CompareAndSwap(peak, saturation) {
			break
		}
	}
	return OAuthRefreshPoolMetrics{
		MaxOpenConnections: stats.MaxOpenConnections,
		OpenConnections:    stats.OpenConnections,
		InUseConnections:   stats.InUse,
		IdleConnections:    stats.Idle,
		WaitCount:          stats.WaitCount,
		WaitDuration:       stats.WaitDuration,
		SaturationPPM:      saturation,
		PeakSaturationPPM:  runtime.peakPoolSaturation.Load(),
	}
}

func (runtime *oauthRefreshRuntime) snapshot() OAuthRefreshRuntimeMetrics {
	return OAuthRefreshRuntimeMetrics{
		ConcurrencyLimit:  cap(runtime.slots),
		InFlight:          runtime.inFlight.Load(),
		PeakInFlight:      runtime.peakInFlight.Load(),
		Waiting:           runtime.waiting.Load(),
		AdmissionWaits:    runtime.admissionWaits.Load(),
		AdmissionCanceled: runtime.admissionCanceled.Load(),
		AdmissionWait:     runtime.admissionWait.snapshot(),
		LockAttempts:      runtime.lockAttempts.Load(),
		LockContentions:   runtime.lockContentions.Load(),
		LockWait:          runtime.lockWait.snapshot(),
		RefreshSuccesses:  runtime.refreshSuccesses.Load(),
		RefreshFailures:   runtime.refreshFailures.Load(),
		RefreshLatency:    runtime.refreshLatency.snapshot(),
		Pool:              runtime.samplePool(),
	}
}
