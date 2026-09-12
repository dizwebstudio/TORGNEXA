package api

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/tenancyrepo"
)

const (
	membershipCacheTTL      = time.Second
	membershipCacheCapacity = 4096
	profileCacheTTL         = 5 * time.Minute
	profileFailureCacheTTL  = 15 * time.Second
	profileCacheCapacity    = 4096
)

var authenticatedLatencyBounds = [...]time.Duration{
	500 * time.Microsecond,
	time.Millisecond,
	2 * time.Millisecond,
	5 * time.Millisecond,
	10 * time.Millisecond,
	25 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
	250 * time.Millisecond,
	time.Second,
	5 * time.Second,
	30 * time.Second,
}

// OIDCCallDistribution describes how many bounded external or database calls
// an authenticated request made. It contains no identity or tenant labels.
type OIDCCallDistribution struct {
	Requests      uint64
	TotalCalls    uint64
	MaximumCalls  uint64
	ZeroCalls     uint64
	OneCall       uint64
	MultipleCalls uint64
}

// OIDCLatencyMetrics exposes fixed-bucket authenticated security-path latency
// and approximate p50/p95/p99 upper bounds without retaining request samples.
type OIDCLatencyMetrics struct {
	Count   uint64
	Total   time.Duration
	Buckets []OIDCLatencyBucket
	P50     time.Duration
	P95     time.Duration
	P99     time.Duration
}

// OIDCLatencyBucket is one cumulative, label-free duration bucket.
type OIDCLatencyBucket struct {
	UpperBound time.Duration
	Count      uint64
}

// OIDCHotPathMetrics is the label-free process snapshot for local token
// verification, bounded profile hydration, session checks and membership use.
type OIDCHotPathMetrics struct {
	Requests               uint64
	Authorized             uint64
	Denied                 uint64
	Unavailable            uint64
	JWKSHTTPCalls          uint64
	JWKSCacheHits          uint64
	JWKSStaleUses          uint64
	UserInfoHTTPCalls      uint64
	UserInfoCacheHits      uint64
	SessionDBCalls         uint64
	SessionLastSeenWrites  uint64
	SessionWritesThrottled uint64
	MembershipDBCalls      uint64
	MembershipCacheHits    uint64
	IdPCallsPerRequest     OIDCCallDistribution
	DBCallsPerRequest      OIDCCallDistribution
	Latency                OIDCLatencyMetrics
}

// OIDCHotPathMetricSource exposes authenticated request load metrics to the
// process observability adapter without identity, tenant or route labels.
type OIDCHotPathMetricSource interface {
	OIDCHotPathMetrics() OIDCHotPathMetrics
}

type atomicOIDCCallDistribution struct {
	requests, total, maximum, zero, one, multiple atomic.Uint64
}

func (distribution *atomicOIDCCallDistribution) record(calls uint64) {
	distribution.requests.Add(1)
	distribution.total.Add(calls)
	for {
		peak := distribution.maximum.Load()
		if calls <= peak || distribution.maximum.CompareAndSwap(peak, calls) {
			break
		}
	}
	switch calls {
	case 0:
		distribution.zero.Add(1)
	case 1:
		distribution.one.Add(1)
	default:
		distribution.multiple.Add(1)
	}
}

func (distribution *atomicOIDCCallDistribution) snapshot() OIDCCallDistribution {
	return OIDCCallDistribution{
		Requests: distribution.requests.Load(), TotalCalls: distribution.total.Load(),
		MaximumCalls: distribution.maximum.Load(), ZeroCalls: distribution.zero.Load(),
		OneCall: distribution.one.Load(), MultipleCalls: distribution.multiple.Load(),
	}
}

type atomicOIDCLatency struct {
	count   atomic.Uint64
	totalNS atomic.Int64
	buckets [len(authenticatedLatencyBounds)]atomic.Uint64
}

func (latency *atomicOIDCLatency) record(value time.Duration) {
	if value < 0 {
		value = 0
	}
	latency.count.Add(1)
	latency.totalNS.Add(int64(value))
	for index, bound := range authenticatedLatencyBounds {
		if value <= bound {
			latency.buckets[index].Add(1)
			break
		}
	}
}

func (latency *atomicOIDCLatency) snapshot() OIDCLatencyMetrics {
	count := latency.count.Load()
	buckets := make([]OIDCLatencyBucket, len(authenticatedLatencyBounds))
	var cumulative uint64
	for index, bound := range authenticatedLatencyBounds {
		cumulative += latency.buckets[index].Load()
		buckets[index] = OIDCLatencyBucket{UpperBound: bound, Count: cumulative}
	}
	return OIDCLatencyMetrics{
		Count: count, Total: time.Duration(latency.totalNS.Load()), Buckets: buckets,
		P50: percentileUpperBound(buckets, count, 50),
		P95: percentileUpperBound(buckets, count, 95),
		P99: percentileUpperBound(buckets, count, 99),
	}
}

func percentileUpperBound(buckets []OIDCLatencyBucket, count, percentile uint64) time.Duration {
	if count == 0 {
		return 0
	}
	target := (count*percentile + 99) / 100
	for _, bucket := range buckets {
		if bucket.Count >= target {
			return bucket.UpperBound
		}
	}
	return buckets[len(buckets)-1].UpperBound
}

type oidcHotPathRecorder struct {
	requests, authorized, denied, unavailable                            atomic.Uint64
	jwksHTTP, jwksHits, jwksStale, userInfoHTTP, userInfoHits            atomic.Uint64
	sessionDB, sessionWrites, sessionThrottled, membershipDB, memberHits atomic.Uint64
	idpCalls, dbCalls                                                    atomicOIDCCallDistribution
	latency                                                              atomicOIDCLatency
}

type oidcHotPathRequest struct {
	started  time.Time
	idpCalls atomic.Uint64
	dbCalls  atomic.Uint64
}

type oidcHotPathRequestKey struct{}

func (metrics *oidcHotPathRecorder) begin(ctx context.Context) (context.Context, func(bool, bool)) {
	request := &oidcHotPathRequest{started: time.Now()}
	metrics.requests.Add(1)
	ctx = context.WithValue(ctx, oidcHotPathRequestKey{}, request)
	return ctx, func(authorized, unavailable bool) {
		if authorized {
			metrics.authorized.Add(1)
		} else if unavailable {
			metrics.unavailable.Add(1)
		} else {
			metrics.denied.Add(1)
		}
		metrics.idpCalls.record(request.idpCalls.Load())
		metrics.dbCalls.record(request.dbCalls.Load())
		metrics.latency.record(time.Since(request.started))
	}
}

func oidcRequestMetrics(ctx context.Context) *oidcHotPathRequest {
	request, _ := ctx.Value(oidcHotPathRequestKey{}).(*oidcHotPathRequest)
	return request
}

func (metrics *oidcHotPathRecorder) recordJWKSHTTPCall(ctx context.Context) {
	if metrics == nil {
		return
	}
	metrics.jwksHTTP.Add(1)
	if request := oidcRequestMetrics(ctx); request != nil {
		request.idpCalls.Add(1)
	}
}

func (metrics *oidcHotPathRecorder) recordJWKSCacheHit(context.Context) {
	if metrics != nil {
		metrics.jwksHits.Add(1)
	}
}

func (metrics *oidcHotPathRecorder) recordJWKSStaleUse(context.Context) {
	if metrics != nil {
		metrics.jwksStale.Add(1)
	}
}

func (metrics *oidcHotPathRecorder) recordUserInfoHTTPCall(ctx context.Context) {
	if metrics == nil {
		return
	}
	metrics.userInfoHTTP.Add(1)
	if request := oidcRequestMetrics(ctx); request != nil {
		request.idpCalls.Add(1)
	}
}

func (metrics *oidcHotPathRecorder) recordUserInfoCacheHit() {
	if metrics != nil {
		metrics.userInfoHits.Add(1)
	}
}

func (metrics *oidcHotPathRecorder) recordSessionDBCall(ctx context.Context, written, throttled bool) {
	if metrics == nil {
		return
	}
	metrics.sessionDB.Add(1)
	if written {
		metrics.sessionWrites.Add(1)
	} else if throttled {
		metrics.sessionThrottled.Add(1)
	}
	if request := oidcRequestMetrics(ctx); request != nil {
		request.dbCalls.Add(1)
	}
}

func (metrics *oidcHotPathRecorder) recordMembershipDBCall(ctx context.Context) {
	if metrics == nil {
		return
	}
	metrics.membershipDB.Add(1)
	if request := oidcRequestMetrics(ctx); request != nil {
		request.dbCalls.Add(1)
	}
}

func (metrics *oidcHotPathRecorder) recordMembershipCacheHit() {
	if metrics != nil {
		metrics.memberHits.Add(1)
	}
}

func (metrics *oidcHotPathRecorder) snapshot() OIDCHotPathMetrics {
	if metrics == nil {
		return OIDCHotPathMetrics{}
	}
	return OIDCHotPathMetrics{
		Requests: metrics.requests.Load(), Authorized: metrics.authorized.Load(), Denied: metrics.denied.Load(), Unavailable: metrics.unavailable.Load(),
		JWKSHTTPCalls: metrics.jwksHTTP.Load(), JWKSCacheHits: metrics.jwksHits.Load(), JWKSStaleUses: metrics.jwksStale.Load(),
		UserInfoHTTPCalls: metrics.userInfoHTTP.Load(), UserInfoCacheHits: metrics.userInfoHits.Load(),
		SessionDBCalls: metrics.sessionDB.Load(), SessionLastSeenWrites: metrics.sessionWrites.Load(), SessionWritesThrottled: metrics.sessionThrottled.Load(),
		MembershipDBCalls: metrics.membershipDB.Load(), MembershipCacheHits: metrics.memberHits.Load(),
		IdPCallsPerRequest: metrics.idpCalls.snapshot(), DBCallsPerRequest: metrics.dbCalls.snapshot(), Latency: metrics.latency.snapshot(),
	}
}

type membershipCacheEntry struct {
	member    tenancyrepo.Member
	expiresAt time.Time
	usedAt    time.Time
}

type membershipCache struct {
	store   workspaceMembershipStore
	metrics *oidcHotPathRecorder
	now     func() time.Time
	mu      sync.Mutex
	entries map[string]membershipCacheEntry
	flights map[string]*membershipFlight
}

type membershipFlight struct {
	done   chan struct{}
	member tenancyrepo.Member
	err    error
}

func newMembershipCache(store workspaceMembershipStore, metrics *oidcHotPathRecorder) *membershipCache {
	return &membershipCache{store: store, metrics: metrics, now: time.Now, entries: make(map[string]membershipCacheEntry), flights: make(map[string]*membershipFlight)}
}

func (cache *membershipCache) resolve(ctx context.Context, scope tenancy.Scope, identity tenancyrepo.MemberIdentity) (tenancyrepo.Member, error) {
	key := scope.OrganizationID().String() + "\x00" + scope.WorkspaceID().String() + "\x00" + identity.SubjectRef
	for {
		now := cache.now().UTC()
		cache.mu.Lock()
		entry, found := cache.entries[key]
		if found && now.Before(entry.expiresAt) {
			entry.usedAt = now
			cache.entries[key] = entry
			cache.mu.Unlock()
			cache.metrics.recordMembershipCacheHit()
			return entry.member, nil
		}
		if found {
			delete(cache.entries, key)
		}
		if flight, running := cache.flights[key]; running {
			cache.mu.Unlock()
			select {
			case <-flight.done:
				if errors.Is(flight.err, context.Canceled) || errors.Is(flight.err, context.DeadlineExceeded) {
					continue
				}
				return flight.member, flight.err
			case <-ctx.Done():
				return tenancyrepo.Member{}, ctx.Err()
			}
		}
		flight := &membershipFlight{done: make(chan struct{})}
		cache.flights[key] = flight
		cache.mu.Unlock()

		cache.metrics.recordMembershipDBCall(ctx)
		flight.member, flight.err = cache.store.ResolveActiveMember(ctx, scope, identity)
		cache.mu.Lock()
		if flight.err == nil {
			if len(cache.entries) >= membershipCacheCapacity {
				cache.evictOldest()
			}
			cache.entries[key] = membershipCacheEntry{member: flight.member, expiresAt: now.Add(membershipCacheTTL), usedAt: now}
		}
		delete(cache.flights, key)
		close(flight.done)
		cache.mu.Unlock()
		return flight.member, flight.err
	}
}

func (cache *membershipCache) put(scope tenancy.Scope, subjectRef string, member tenancyrepo.Member) {
	now := cache.now().UTC()
	key := scope.OrganizationID().String() + "\x00" + scope.WorkspaceID().String() + "\x00" + subjectRef
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if len(cache.entries) >= membershipCacheCapacity {
		cache.evictOldest()
	}
	cache.entries[key] = membershipCacheEntry{member: member, expiresAt: now.Add(membershipCacheTTL), usedAt: now}
}

func (cache *membershipCache) evictOldest() {
	var oldestKey string
	var oldest time.Time
	for key, entry := range cache.entries {
		if oldestKey == "" || entry.usedAt.Before(oldest) {
			oldestKey, oldest = key, entry.usedAt
		}
	}
	delete(cache.entries, oldestKey)
}

type resolvedMembership struct {
	scope      tenancy.Scope
	subjectRef string
	member     tenancyrepo.Member
}

type resolvedMembershipState struct{ value *resolvedMembership }
type resolvedMembershipKey struct{}
type membershipCacheBypassKey struct{}

func withResolvedMembership(ctx context.Context) context.Context {
	return context.WithValue(ctx, resolvedMembershipKey{}, &resolvedMembershipState{})
}

func withMembershipCacheBypass(ctx context.Context) context.Context {
	return context.WithValue(ctx, membershipCacheBypassKey{}, true)
}

func membershipCacheBypassed(ctx context.Context) bool {
	value, _ := ctx.Value(membershipCacheBypassKey{}).(bool)
	return value
}

func storeResolvedMembership(ctx context.Context, scope tenancy.Scope, subjectRef string, member tenancyrepo.Member) {
	if state, ok := ctx.Value(resolvedMembershipKey{}).(*resolvedMembershipState); ok {
		state.value = &resolvedMembership{scope: scope, subjectRef: subjectRef, member: member}
	}
}

func loadResolvedMembership(ctx context.Context, scope tenancy.Scope, subjectRef string) (tenancyrepo.Member, bool) {
	state, ok := ctx.Value(resolvedMembershipKey{}).(*resolvedMembershipState)
	if !ok || state.value == nil || state.value.scope != scope || state.value.subjectRef != subjectRef {
		return tenancyrepo.Member{}, false
	}
	return state.value.member, true
}

type oidcProfileCacheEntry struct {
	info      userInfoClaims
	available bool
	expiresAt time.Time
	usedAt    time.Time
}

type oidcProfileCache struct {
	now     func() time.Time
	metrics *oidcHotPathRecorder
	mu      sync.Mutex
	entries map[string]oidcProfileCacheEntry
	flights map[string]chan struct{}
}

func newOIDCProfileCache(metrics *oidcHotPathRecorder) *oidcProfileCache {
	return &oidcProfileCache{now: time.Now, metrics: metrics, entries: make(map[string]oidcProfileCacheEntry), flights: make(map[string]chan struct{})}
}

func (cache *oidcProfileCache) resolve(ctx context.Context, key string, fetch func() (userInfoClaims, bool, error)) (userInfoClaims, bool, error) {
	for {
		now := cache.now().UTC()
		cache.mu.Lock()
		entry, found := cache.entries[key]
		if found && now.Before(entry.expiresAt) {
			entry.usedAt = now
			cache.entries[key] = entry
			cache.mu.Unlock()
			cache.metrics.recordUserInfoCacheHit()
			return entry.info, entry.available, nil
		}
		if found {
			delete(cache.entries, key)
		}
		if flight, running := cache.flights[key]; running {
			cache.mu.Unlock()
			select {
			case <-flight:
				continue
			case <-ctx.Done():
				return userInfoClaims{}, false, ctx.Err()
			}
		}
		flight := make(chan struct{})
		cache.flights[key] = flight
		cache.mu.Unlock()

		info, available, err := fetch()
		ttl := profileCacheTTL
		if !available {
			ttl = profileFailureCacheTTL
		}
		now = cache.now().UTC()
		cache.mu.Lock()
		if err == nil {
			if len(cache.entries) >= profileCacheCapacity {
				cache.evictOldest()
			}
			cache.entries[key] = oidcProfileCacheEntry{info: info, available: available, expiresAt: now.Add(ttl), usedAt: now}
		}
		delete(cache.flights, key)
		close(flight)
		cache.mu.Unlock()
		return info, available, err
	}
}

func (cache *oidcProfileCache) evictOldest() {
	var oldestKey string
	var oldest time.Time
	for key, entry := range cache.entries {
		if oldestKey == "" || entry.usedAt.Before(oldest) {
			oldestKey, oldest = key, entry.usedAt
		}
	}
	delete(cache.entries, oldestKey)
}
