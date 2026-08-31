package worker

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/uploads"
)

// releasedUploadMedia bridges the upload security boundary to social
// connectors. It never accepts an object key from a publication and resolves
// the released reference again for every media read.
type releasedUploadMedia struct {
	gate    *uploads.AccessGate
	content uploads.ReleaseReader
}

func (media releasedUploadMedia) OpenReleased(ctx context.Context, account sdk.Account, ref sdk.SocialMediaRef) (io.ReadCloser, sdk.MediaDescriptor, error) {
	if media.gate == nil || media.content == nil || account.Validate() != nil || ref.Validate() != nil {
		return nil, sdk.MediaDescriptor{}, sdk.ErrInvalidSocialRequest
	}
	scope, err := tenancy.ParseScope(account.OrganizationID, account.WorkspaceID)
	if err != nil {
		return nil, sdk.MediaDescriptor{}, sdk.ErrInvalidSocialRequest
	}
	id := uploads.ID(ref.UploadID)
	resolved, err := media.gate.ResolveReleased(ctx, scope, id)
	if err != nil || resolved.UploadID() != id {
		return nil, sdk.MediaDescriptor{}, sdk.ErrInvalidSocialRequest
	}
	// Re-check the immutable release evidence immediately before opening the
	// object. A re-scan or revoke must invalidate the read path.
	if err := media.gate.ValidateReleasedRef(ctx, scope, resolved); err != nil {
		return nil, sdk.MediaDescriptor{}, sdk.ErrInvalidSocialRequest
	}
	object, err := media.content.OpenReleased(ctx, scope, id, resolved.ObjectKey())
	if err != nil || object == nil || resolved.SizeBytes() < 1 {
		return nil, sdk.MediaDescriptor{}, sdk.ErrInvalidSocialRequest
	}
	prefix := make([]byte, minInt64(resolved.SizeBytes(), 512))
	read, readErr := io.ReadFull(object, prefix)
	if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
		_ = object.Close()
		return nil, sdk.MediaDescriptor{}, sdk.ErrInvalidSocialRequest
	}
	if read != len(prefix) {
		_ = object.Close()
		return nil, sdk.MediaDescriptor{}, sdk.ErrInvalidSocialRequest
	}
	if int64(read) == 0 {
		_ = object.Close()
		return nil, sdk.MediaDescriptor{}, sdk.ErrInvalidSocialRequest
	}
	descriptor := sdk.MediaDescriptor{MediaType: http.DetectContentType(prefix[:read]), SizeBytes: resolved.SizeBytes(), SHA256: resolved.SHA256()}
	if descriptor.Validate() != nil {
		_ = object.Close()
		return nil, sdk.MediaDescriptor{}, sdk.ErrInvalidSocialRequest
	}
	return &prefixedReadCloser{Reader: io.MultiReader(bytes.NewReader(prefix[:read]), object), closer: object}, descriptor, nil
}

type prefixedReadCloser struct {
	io.Reader
	closer io.Closer
}

func (reader *prefixedReadCloser) Close() error {
	if reader == nil || reader.closer == nil {
		return nil
	}
	return reader.closer.Close()
}

func minInt64(value, limit int64) int {
	if value < limit {
		return int(value)
	}
	return int(limit)
}

var _ sdk.MediaAccessor = releasedUploadMedia{}
