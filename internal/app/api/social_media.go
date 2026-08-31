package api

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/uploads"
)

// releasedUploadMedia is the API-side counterpart of the worker media bridge.
// It resolves the release again for every edit, so a revoked or re-scanned
// upload cannot be reused by a long-lived approval request.
type releasedUploadMedia struct {
	gate    uploadReleaseGate
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
	if err != nil || resolved.UploadID() != id || resolved.SizeBytes() < 1 {
		return nil, sdk.MediaDescriptor{}, sdk.ErrInvalidSocialRequest
	}
	object, err := media.content.OpenReleased(ctx, scope, id, resolved.ObjectKey())
	if err != nil || object == nil {
		return nil, sdk.MediaDescriptor{}, sdk.ErrInvalidSocialRequest
	}
	prefix := make([]byte, socialMediaPrefixBytes(resolved.SizeBytes()))
	read, readErr := io.ReadFull(object, prefix)
	if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
		_ = object.Close()
		return nil, sdk.MediaDescriptor{}, sdk.ErrInvalidSocialRequest
	}
	if read != len(prefix) || read == 0 {
		_ = object.Close()
		return nil, sdk.MediaDescriptor{}, sdk.ErrInvalidSocialRequest
	}
	descriptor := sdk.MediaDescriptor{
		MediaType: http.DetectContentType(prefix[:read]),
		SizeBytes: resolved.SizeBytes(),
		SHA256:    resolved.SHA256(),
	}
	if descriptor.Validate() != nil {
		_ = object.Close()
		return nil, sdk.MediaDescriptor{}, sdk.ErrInvalidSocialRequest
	}
	return &socialPrefixedReadCloser{Reader: io.MultiReader(bytes.NewReader(prefix[:read]), object), closer: object}, descriptor, nil
}

func socialMediaPrefixBytes(size int64) int {
	if size < 512 {
		return int(size)
	}
	return 512
}

type socialPrefixedReadCloser struct {
	io.Reader
	closer io.Closer
}

func (reader *socialPrefixedReadCloser) Close() error {
	if reader == nil || reader.closer == nil {
		return nil
	}
	return reader.closer.Close()
}

var _ sdk.MediaAccessor = releasedUploadMedia{}
