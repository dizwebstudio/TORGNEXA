package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

const RealtimePath = "/api/v1/realtime"

// realtimeEvent is intentionally metadata-only: the browser receives an
// invalidation signal, never raw audit payloads or tenant data through the
// stream. Clients re-read authorized APIs through the normal capability gates.
type realtimeEvent struct {
	Reason string `json:"reason"`
	Cursor string `json:"cursor,omitempty"`
	At     string `json:"at"`
}

// latestAuditReader is an optional fast path for the realtime stream. The
// normal audit list endpoint must decode complete summaries, while the SSE
// invalidation channel only needs the newest opaque ID.
type latestAuditReader interface {
	LatestID(context.Context, tenancy.Scope) (string, error)
}

func latestAuditID(ctx context.Context, repository auditReader, scope tenancy.Scope) (string, error) {
	if fast, ok := repository.(latestAuditReader); ok {
		return fast.LatestID(ctx, scope)
	}
	rows, _, err := repository.List(ctx, scope, 1, "")
	if err != nil || len(rows) == 0 {
		return "", err
	}
	return rows[0].ID, nil
}

func newRealtimeRoutes(repository auditReader) []ProtectedRoute {
	return []ProtectedRoute{{Method: http.MethodGet, Path: RealtimePath, Permission: "operations.realtime.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope, ok := ScopeFromContext(r.Context())
		if !ok || repository == nil {
			writeProblem(w, http.StatusForbidden, "Forbidden")
			return
		}
		flusher, ok := unwrapFlusher(w)
		if !ok {
			writeProblem(w, http.StatusNotImplemented, "Streaming Unsupported")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store, no-transform")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		write := func(name string, value realtimeEvent) bool {
			raw, err := json.Marshal(value)
			if err != nil {
				return false
			}
			if _, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, raw); err != nil {
				return false
			}
			flusher.Flush()
			return true
		}

		latest := ""
		if id, err := latestAuditID(r.Context(), repository, scope); err == nil {
			latest = id
		}
		if !write("ready", realtimeEvent{Reason: "connected", Cursor: latest, At: time.Now().UTC().Format(time.RFC3339Nano)}) {
			return
		}

		poll := time.NewTicker(2 * time.Second)
		heartbeat := time.NewTicker(15 * time.Second)
		defer poll.Stop()
		defer heartbeat.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-poll.C:
				id, err := latestAuditID(r.Context(), repository, scope)
				if err != nil || id == "" || id == latest {
					continue
				}
				latest = id
				if !write("invalidate", realtimeEvent{Reason: "audit", Cursor: latest, At: time.Now().UTC().Format(time.RFC3339Nano)}) {
					return
				}
			case <-heartbeat.C:
				if !write("heartbeat", realtimeEvent{Reason: "heartbeat", Cursor: latest, At: time.Now().UTC().Format(time.RFC3339Nano)}) {
					return
				}
			}
		}
	})}}
}

// unwrapFlusher walks ResponseWriter wrappers that expose the standard
// Unwrap() http.ResponseWriter method (net/http.ResponseController's
// convention) to find the underlying http.Flusher. recoverPanics wraps every
// request's ResponseWriter in *responseTracker, whose embedded
// http.ResponseWriter interface field does not itself satisfy http.Flusher,
// so a direct w.(http.Flusher) assertion always fails for tracked requests.
func unwrapFlusher(w http.ResponseWriter) (http.Flusher, bool) {
	for {
		if flusher, ok := w.(http.Flusher); ok {
			return flusher, true
		}
		unwrapper, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return nil, false
		}
		w = unwrapper.Unwrap()
	}
}
