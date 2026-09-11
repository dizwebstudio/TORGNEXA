package securityedge

import (
	"container/heap"
	"context"
	"crypto/sha256"
	"errors"
	"regexp"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultMaxLimiterKeys = 100_000
	localLimiterShards    = 64
)

// ErrLimiterUnavailable means admission state could not be evaluated and
// callers must fail closed without invoking protected work.
var ErrLimiterUnavailable = errors.New("securityedge: rate limiter unavailable")

var limitNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,31}$`)

// Limit identifies one bounded counter. Name is a fixed application-owned
// budget class; Key is hashed before it reaches local or distributed storage.
type Limit struct {
	Name   string
	Key    string
	Max    int
	Window time.Duration
}

func (limit Limit) valid() bool {
	return limitNamePattern.MatchString(limit.Name) && limit.Key != "" && len(limit.Key) <= 2048 && limit.Max > 0 && limit.Max <= 1_000_000 && limit.Window >= time.Second && limit.Window <= time.Hour
}

// Decision contains safe response guidance. Remaining never grants authority;
// it is operational metadata for the already-evaluated request.
type Decision struct {
	Remaining  int
	RetryAfter time.Duration
}

// Limiter applies one logical budget. Implementations shared by multiple API
// replicas must make Allow atomic across those replicas.
type Limiter interface {
	Allow(context.Context, Limit) (Decision, error)
}

// LimiterMetricSource exposes bounded, label-free counters. Runtime metric
// adapters may sample these values without accepting attacker-controlled labels.
type LimiterMetricSource interface {
	Metrics() LimiterMetrics
}

type limiterMetrics struct {
	allowed, limited, unavailable, capacity atomic.Uint64
}

// LimiterMetrics is a monotonic process-local view suitable for an observability adapter.
type LimiterMetrics struct {
	Allowed, Limited, Unavailable, CapacityLimited uint64
}

func (metrics *limiterMetrics) snapshot() LimiterMetrics {
	if metrics == nil {
		return LimiterMetrics{}
	}
	return LimiterMetrics{Allowed: metrics.allowed.Load(), Limited: metrics.limited.Load(), Unavailable: metrics.unavailable.Load(), CapacityLimited: metrics.capacity.Load()}
}

type localEntry struct {
	count     int
	expiresAt time.Time
}

type expiryItem struct {
	key       [32]byte
	expiresAt time.Time
}

type expiryQueue []expiryItem

func (queue expiryQueue) Len() int           { return len(queue) }
func (queue expiryQueue) Less(i, j int) bool { return queue[i].expiresAt.Before(queue[j].expiresAt) }
func (queue expiryQueue) Swap(i, j int)      { queue[i], queue[j] = queue[j], queue[i] }
func (queue *expiryQueue) Push(value any)    { *queue = append(*queue, value.(expiryItem)) }
func (queue *expiryQueue) Pop() any {
	old := *queue
	item := old[len(old)-1]
	*queue = old[:len(old)-1]
	return item
}

type localLimiterShard struct {
	mu      sync.Mutex
	entries map[[32]byte]localEntry
	expiry  expiryQueue
}

// LocalLimiter is the explicit single-node/development fallback. It shards
// request synchronization and evicts through expiry heaps, avoiding a global
// mutex and O(n) sweeps in the request path.
type LocalLimiter struct {
	shards     [localLimiterShards]localLimiterShard
	maxKeys    int
	activeKeys atomic.Int64
	now        func() time.Time
	metrics    limiterMetrics
}

// NewLimiter returns the bounded local fallback retained for tests and explicit
// single-node/development deployments. Production API startup requires Valkey.
func NewLimiter() *LocalLimiter { return NewLocalLimiter(defaultMaxLimiterKeys) }

// NewLocalLimiter constructs the bounded development and test fallback.
func NewLocalLimiter(maxKeys int) *LocalLimiter {
	if maxKeys <= 0 {
		maxKeys = defaultMaxLimiterKeys
	}
	limiter := &LocalLimiter{maxKeys: maxKeys, now: time.Now}
	for index := range limiter.shards {
		limiter.shards[index].entries = make(map[[32]byte]localEntry)
	}
	return limiter
}

// Allow atomically consumes one process-local budget unit.
func (limiter *LocalLimiter) Allow(ctx context.Context, limit Limit) (Decision, error) {
	if limiter == nil || ctx == nil || !limit.valid() || limiter.now == nil {
		return Decision{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		limiter.metrics.unavailable.Add(1)
		return Decision{}, errors.Join(ErrLimiterUnavailable, err)
	}
	now := limiter.now().UTC()
	digest := limitDigest(limit)
	shard := &limiter.shards[int(digest[0])%len(limiter.shards)]
	shard.mu.Lock()
	limiter.pruneShardLocked(shard, now)
	entry, exists := shard.entries[digest]
	if !exists {
		if !limiter.reserveKey() {
			shard.mu.Unlock()
			limiter.pruneExpired(now)
			shard.mu.Lock()
			limiter.pruneShardLocked(shard, now)
			entry, exists = shard.entries[digest]
			if !exists && !limiter.reserveKey() {
				shard.mu.Unlock()
				limiter.metrics.limited.Add(1)
				limiter.metrics.capacity.Add(1)
				return Decision{RetryAfter: limit.Window}, ErrRateLimited
			}
		}
		if !exists {
			entry.expiresAt = now.Add(limit.Window)
			heap.Push(&shard.expiry, expiryItem{key: digest, expiresAt: entry.expiresAt})
		}
	}
	if entry.count >= limit.Max {
		shard.mu.Unlock()
		limiter.metrics.limited.Add(1)
		return Decision{RetryAfter: positiveDuration(entry.expiresAt.Sub(now))}, ErrRateLimited
	}
	entry.count++
	shard.entries[digest] = entry
	shard.mu.Unlock()
	limiter.metrics.allowed.Add(1)
	return Decision{Remaining: limit.Max - entry.count}, nil
}

// Metrics returns label-free process-local outcome counters.
func (limiter *LocalLimiter) Metrics() LimiterMetrics { return limiter.metrics.snapshot() }

func (limiter *LocalLimiter) reserveKey() bool {
	for {
		active := limiter.activeKeys.Load()
		if active >= int64(limiter.maxKeys) {
			return false
		}
		if limiter.activeKeys.CompareAndSwap(active, active+1) {
			return true
		}
	}
}

func (limiter *LocalLimiter) pruneExpired(now time.Time) {
	for index := range limiter.shards {
		shard := &limiter.shards[index]
		shard.mu.Lock()
		limiter.pruneShardLocked(shard, now)
		shard.mu.Unlock()
	}
}

func (limiter *LocalLimiter) pruneShardLocked(shard *localLimiterShard, now time.Time) {
	for shard.expiry.Len() > 0 && !shard.expiry[0].expiresAt.After(now) {
		item := heap.Pop(&shard.expiry).(expiryItem)
		if entry, ok := shard.entries[item.key]; ok && entry.expiresAt.Equal(item.expiresAt) {
			delete(shard.entries, item.key)
			limiter.activeKeys.Add(-1)
		}
	}
}

func limitDigest(limit Limit) [32]byte {
	return sha256.Sum256([]byte(limit.Name + "\x00" + limit.Key))
}

func positiveDuration(value time.Duration) time.Duration {
	if value <= 0 {
		return time.Second
	}
	return value
}
