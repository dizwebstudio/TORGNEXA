package api

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/uploads"
)

// UploadsPath must match the POST /uploads route registered by
// newReservedContractRoutes; both own the same resource.
const UploadsPath = "/api/v1/uploads"

type uploadStatusReader interface {
	Get(context.Context, tenancy.Scope, uploads.ID) (uploads.Record, error)
}

type uploadReleaseGate interface {
	ResolveReleased(context.Context, tenancy.Scope, uploads.ID) (uploads.ReleasedObjectRef, error)
}

// newUploadReadRoutes exposes an upload's lifecycle state and, once released,
// its bytes. Both are gated by products.read rather than uploads.* because
// today's only consumer is product images: anyone who can see a product must
// be able to see its photos, regardless of who uploaded them.
func newUploadReadRoutes(status uploadStatusReader, gate uploadReleaseGate, content uploads.ReleaseReader) []ProtectedRoute {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { uploadReadRoute(w, r, status, gate, content) })
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: UploadsPath + "/", PathPrefix: true, Permission: "products.read", Handler: handler},
	}
}

func uploadReadRoute(w http.ResponseWriter, r *http.Request, status uploadStatusReader, gate uploadReleaseGate, content uploads.ReleaseReader) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	tail := strings.Trim(strings.TrimPrefix(r.URL.Path, UploadsPath+"/"), "/")
	parts := strings.Split(tail, "/")
	if len(parts) < 1 || parts[0] == "" {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	id := uploads.ID(parts[0])
	if !id.Valid() {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	switch {
	case len(parts) == 1:
		getUploadStatus(w, r, scope, id, status)
	case len(parts) == 2 && parts[1] == "content":
		getUploadContent(w, r, scope, id, gate, content)
	default:
		writeProblem(w, http.StatusNotFound, "Not Found")
	}
}

func getUploadStatus(w http.ResponseWriter, r *http.Request, scope tenancy.Scope, id uploads.ID, status uploadStatusReader) {
	if status == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	record, err := status.Get(r.Context(), scope, id)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	body := map[string]any{
		"id": record.ID, "state": record.State,
		"content_size_bytes": record.ContentSizeBytes, "content_sha256": record.ContentSHA256,
		"version": record.Version,
	}
	if record.QuarantinedAt != nil {
		body["quarantined_at"] = *record.QuarantinedAt
	}
	if record.ReleasedAt != nil {
		body["released_at"] = *record.ReleasedAt
	}
	writeJSON(w, http.StatusOK, body)
}

// getUploadContent re-validates release on every read (ResolveReleased, not
// a cached reference) and only then opens the server-derived released key —
// a re-scan transition or cross-tenant id can never serve stale bytes.
func getUploadContent(w http.ResponseWriter, r *http.Request, scope tenancy.Scope, id uploads.ID, gate uploadReleaseGate, content uploads.ReleaseReader) {
	if gate == nil || content == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	ref, err := gate.ResolveReleased(r.Context(), scope, id)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	object, err := content.OpenReleased(r.Context(), scope, id, ref.ObjectKey())
	if err != nil {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	defer object.Close()
	sniff := make([]byte, 512)
	n, readErr := io.ReadFull(object, sniff)
	if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	w.Header().Set("Content-Type", http.DetectContentType(sniff[:n]))
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(sniff[:n])
	_, _ = io.Copy(w, object)
}
