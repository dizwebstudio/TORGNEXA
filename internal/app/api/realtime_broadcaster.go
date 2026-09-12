package api

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

const (
	defaultRealtimeMaxClientsPerTenant  = 64
	defaultRealtimeMaxClientsPerProcess = 1024
	defaultRealtimeSubscriberBuffer     = 1
	defaultRealtimePollTimeout          = 5 * time.Second
)

var (
	errRealtimeTenantClientLimit  = errors.New("realtime tenant client limit reached")
	errRealtimeProcessClientLimit = errors.New("realtime process client limit reached")
)

type realtimeSignal struct {
	cursor string
}

type realtimeSubscription struct {
	initial string
	events  <-chan realtimeSignal
	stop    func()
	once    sync.Once
}

func (s *realtimeSubscription) close() {
	if s != nil && s.stop != nil {
		s.once.Do(s.stop)
	}
}

type realtimeTenantWatcher struct {
	key         string
	scope       tenancy.Scope
	context     context.Context
	cancel      context.CancelFunc
	ready       chan struct{}
	latest      string
	nextID      uint64
	subscribers map[uint64]chan realtimeSignal
}

type realtimeBroadcasterCounters struct {
	accepted         atomic.Uint64
	rejectedTenant   atomic.Uint64
	rejectedProcess  atomic.Uint64
	auditHeadQueries atomic.Uint64
	auditHeadErrors  atomic.Uint64
	signalsPublished atomic.Uint64
	deliveriesQueued atomic.Uint64
	deliveriesMerged atomic.Uint64
	peakClients      atomic.Uint64
}

// RealtimeBroadcasterMetrics is a process-local, label-free snapshot of SSE
// fan-out saturation and audit-head polling. Tenant identifiers are never
// exposed as metric labels.
type RealtimeBroadcasterMetrics struct {
	ActiveTenants    int
	ActiveClients    int
	PeakClients      uint64
	AcceptedClients  uint64
	RejectedTenant   uint64
	RejectedProcess  uint64
	AuditHeadQueries uint64
	AuditHeadErrors  uint64
	SignalsPublished uint64
	DeliveriesQueued uint64
	DeliveriesMerged uint64
}

// RealtimeBroadcasterMetricSource exposes label-free SSE fan-out metrics to a
// process observability adapter.
type RealtimeBroadcasterMetricSource interface {
	RealtimeBroadcasterMetrics() RealtimeBroadcasterMetrics
}

type realtimeBroadcaster struct {
	repository auditReader
	timing     realtimeTiming

	mu       sync.Mutex
	tenants  map[string]*realtimeTenantWatcher
	clients  int
	counters realtimeBroadcasterCounters
}

var _ RealtimeBroadcasterMetricSource = (*realtimeBroadcaster)(nil)

func newRealtimeBroadcaster(repository auditReader, timing realtimeTiming) *realtimeBroadcaster {
	return &realtimeBroadcaster{
		repository: repository,
		timing:     normalizeRealtimeTiming(timing),
		tenants:    make(map[string]*realtimeTenantWatcher),
	}
}

func (b *realtimeBroadcaster) subscribe(ctx context.Context, scope tenancy.Scope) (*realtimeSubscription, error) {
	key := scope.OrganizationID().String() + "/" + scope.WorkspaceID().String()

	b.mu.Lock()
	watcher := b.tenants[key]
	if watcher != nil && len(watcher.subscribers) >= b.timing.maxClientsPerTenant {
		b.counters.rejectedTenant.Add(1)
		b.mu.Unlock()
		return nil, errRealtimeTenantClientLimit
	}
	if b.clients >= b.timing.maxClientsPerProcess {
		b.counters.rejectedProcess.Add(1)
		b.mu.Unlock()
		return nil, errRealtimeProcessClientLimit
	}
	if watcher == nil {
		watcherContext, cancel := context.WithCancel(context.Background())
		watcher = &realtimeTenantWatcher{
			key:         key,
			scope:       scope,
			context:     watcherContext,
			cancel:      cancel,
			ready:       make(chan struct{}),
			subscribers: make(map[uint64]chan realtimeSignal),
		}
		b.tenants[key] = watcher
		go b.watch(watcher)
	}
	watcher.nextID++
	id := watcher.nextID
	queue := make(chan realtimeSignal, b.timing.subscriberBuffer)
	watcher.subscribers[id] = queue
	b.clients++
	b.counters.accepted.Add(1)
	b.updatePeakLocked()
	b.mu.Unlock()

	subscription := &realtimeSubscription{
		events: queue,
		stop: func() {
			b.unsubscribe(watcher, id)
		},
	}
	select {
	case <-ctx.Done():
		subscription.close()
		return nil, ctx.Err()
	case <-watcher.ready:
	}

	b.mu.Lock()
	if current := b.tenants[key]; current == watcher {
		subscription.initial = watcher.latest
	}
	b.mu.Unlock()
	return subscription, nil
}

func (b *realtimeBroadcaster) unsubscribe(watcher *realtimeTenantWatcher, id uint64) {
	b.mu.Lock()
	current := b.tenants[watcher.key]
	if current != watcher {
		b.mu.Unlock()
		return
	}
	if _, ok := watcher.subscribers[id]; !ok {
		b.mu.Unlock()
		return
	}
	delete(watcher.subscribers, id)
	b.clients--
	if len(watcher.subscribers) == 0 {
		delete(b.tenants, watcher.key)
		watcher.cancel()
	}
	b.mu.Unlock()
}

func (b *realtimeBroadcaster) watch(watcher *realtimeTenantWatcher) {
	b.poll(watcher, true)
	ticker := time.NewTicker(b.timing.pollInterval)
	defer ticker.Stop()
	for {
		// Prefer cancellation before consuming a tick that became ready at the
		// same time. One in-flight query may still finish through its cancelled
		// context, but the watcher cannot start an unbounded tail of new polls.
		select {
		case <-watcher.context.Done():
			return
		default:
		}
		select {
		case <-watcher.context.Done():
			return
		case <-ticker.C:
			b.poll(watcher, false)
		}
	}
}

func (b *realtimeBroadcaster) poll(watcher *realtimeTenantWatcher, initial bool) {
	queryContext, cancel := context.WithTimeout(watcher.context, b.timing.pollTimeout)
	b.counters.auditHeadQueries.Add(1)
	id, err := latestAuditID(queryContext, b.repository, watcher.scope)
	cancel()
	if err != nil {
		b.counters.auditHeadErrors.Add(1)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if err == nil && id != "" && id != watcher.latest {
		watcher.latest = id
		if !initial {
			b.publishLocked(watcher, realtimeSignal{cursor: id})
		}
	}
	if initial {
		close(watcher.ready)
	}
}

func (b *realtimeBroadcaster) publishLocked(watcher *realtimeTenantWatcher, signal realtimeSignal) {
	b.counters.signalsPublished.Add(1)
	for _, queue := range watcher.subscribers {
		select {
		case queue <- signal:
			b.counters.deliveriesQueued.Add(1)
		default:
			// Invalidation is level-triggered: one newest cursor is sufficient.
			// Replace an older pending signal instead of accumulating history.
			select {
			case <-queue:
			default:
			}
			select {
			case queue <- signal:
				b.counters.deliveriesQueued.Add(1)
			default:
			}
			b.counters.deliveriesMerged.Add(1)
		}
	}
}

func (b *realtimeBroadcaster) updatePeakLocked() {
	current := uint64(b.clients)
	for {
		peak := b.counters.peakClients.Load()
		if current <= peak || b.counters.peakClients.CompareAndSwap(peak, current) {
			return
		}
	}
}

// RealtimeBroadcasterMetrics returns the current process-local SSE counters.
func (b *realtimeBroadcaster) RealtimeBroadcasterMetrics() RealtimeBroadcasterMetrics {
	b.mu.Lock()
	activeTenants := len(b.tenants)
	activeClients := b.clients
	b.mu.Unlock()
	return RealtimeBroadcasterMetrics{
		ActiveTenants:    activeTenants,
		ActiveClients:    activeClients,
		PeakClients:      b.counters.peakClients.Load(),
		AcceptedClients:  b.counters.accepted.Load(),
		RejectedTenant:   b.counters.rejectedTenant.Load(),
		RejectedProcess:  b.counters.rejectedProcess.Load(),
		AuditHeadQueries: b.counters.auditHeadQueries.Load(),
		AuditHeadErrors:  b.counters.auditHeadErrors.Load(),
		SignalsPublished: b.counters.signalsPublished.Load(),
		DeliveriesQueued: b.counters.deliveriesQueued.Load(),
		DeliveriesMerged: b.counters.deliveriesMerged.Load(),
	}
}
