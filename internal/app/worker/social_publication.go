package worker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/social"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/connectorauth"
	"github.com/torgnexa/torgnexa/internal/platform/connectorruntime"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/socialdispatchrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/workerrepo"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

type socialPublicationStore interface {
	Publication(context.Context, social.Scope, social.PublicationID) (social.Publication, error)
	Variant(context.Context, social.Scope, social.VariantID) (social.ContentVariant, error)
	ChannelAccount(context.Context, social.Scope, social.ChannelAccountID) (social.ChannelAccount, error)
	ChangePublicationStatus(context.Context, social.Scope, social.ChangePublicationStatus, social.Mutation) (social.Publication, error)
}

type socialAccountStore interface {
	AccountByID(context.Context, string, string, string) (sdk.Account, error)
	AccountCapabilities(context.Context, tenancy.Scope, string) ([]sdk.AccountCapabilitySetting, error)
}

type socialReceiptStore interface {
	Receipt(context.Context, tenancy.Scope, string) (socialdispatchrepo.Receipt, error)
	Record(context.Context, tenancy.Scope, socialdispatchrepo.Receipt) (socialdispatchrepo.Receipt, error)
}

func runSocialPublications(ctx context.Context, logger *slog.Logger, dispatch *workerrepo.Repository, publications socialPublicationStore, accounts socialAccountStore, receipts socialReceiptStore, secretProvider secrets.SecretProvider, refreshCoordinator connectorauth.RefreshCoordinator, registry *runtimeRegistry, workerID string, poll time.Duration, batch int, lease time.Duration, media sdk.MediaAccessor) error {
	return pollLoop(ctx, poll, func() error {
		jobs, err := dispatch.Claim(ctx, workerrepo.KindSocialPublication, workerID, batch, lease)
		if errors.Is(err, workerrepo.ErrSchemaUnavailable) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, job := range jobs {
			if err := processSocialPublication(ctx, publications, accounts, receipts, secretProvider, refreshCoordinator, registry, job, media); err != nil {
				if releaseErr := dispatch.Release(ctx, job, retryDelay(job.AttemptCount), "social_publication_failed"); releaseErr != nil {
					return releaseErr
				}
				logger.Warn("social publication deferred", "event", "worker.social_publication_deferred", "publication_id", job.ItemID)
				continue
			}
			if err := dispatch.Complete(ctx, job); err != nil {
				return err
			}
		}
		return nil
	})
}

func processSocialPublication(ctx context.Context, publications socialPublicationStore, accounts socialAccountStore, receipts socialReceiptStore, secretProvider secrets.SecretProvider, refreshCoordinator connectorauth.RefreshCoordinator, registry *runtimeRegistry, job workerrepo.Job, media sdk.MediaAccessor) error {
	secretReady := dependencyReady(secretProvider)
	refreshReady := dependencyReady(refreshCoordinator)
	if ctx == nil || publications == nil || accounts == nil || receipts == nil || !secretReady || !refreshReady || registry == nil || !job.Scope.Valid() {
		return errors.New("worker: invalid social publication dependencies")
	}
	scope, err := social.ParseScope(job.Scope.OrganizationID().String(), job.Scope.WorkspaceID().String())
	if err != nil {
		return err
	}
	publicationID, err := social.ParsePublicationID(job.ItemID)
	if err != nil {
		return err
	}
	publication, err := publications.Publication(ctx, scope, publicationID)
	if err != nil {
		return err
	}
	switch publication.Status {
	case social.PublicationScheduled:
		_, err = changeSocialPublication(ctx, publications, scope, publication, social.PublicationReady, "")
		return err
	case social.PublicationPublishing:
		return recoverSocialPublication(ctx, publications, receipts, job.Scope, scope, publication)
	case social.PublicationReady:
		return publishSocial(ctx, publications, accounts, receipts, secretProvider, refreshCoordinator, registry, job.Scope, scope, publication, media)
	case social.PublicationPublished, social.PublicationFailed, social.PublicationCancelled:
		return nil
	default:
		return social.ErrInvalidState
	}
}

func dependencyReady(value any) bool {
	return value != nil
}

func publishSocial(ctx context.Context, publications socialPublicationStore, accounts socialAccountStore, receipts socialReceiptStore, secretProvider secrets.SecretProvider, refreshCoordinator connectorauth.RefreshCoordinator, registry *runtimeRegistry, tenantScope tenancy.Scope, scope social.Scope, publication social.Publication, media sdk.MediaAccessor) error {
	variant, err := publications.Variant(ctx, scope, publication.VariantID)
	if err != nil {
		return err
	}
	channel, err := publications.ChannelAccount(ctx, scope, publication.ChannelAccountID)
	if err != nil {
		return err
	}
	request, capability, err := socialPublishRequest(publication, variant)
	if err != nil || channel.Status != social.ChannelActive || !channel.Supports(capability) {
		return failSocialPublication(ctx, publications, scope, publication, "capability_unavailable")
	}
	if len(request.Buttons) > 0 && !channel.Supports(social.CapabilityPostButtons) {
		return failSocialPublication(ctx, publications, scope, publication, "capability_unavailable")
	}
	if request.Kind != sdk.SocialPostText && media == nil {
		return failSocialPublication(ctx, publications, scope, publication, "media_unavailable")
	}
	account, err := accounts.AccountByID(ctx, tenantScope.OrganizationID().String(), tenantScope.WorkspaceID().String(), channel.ConnectorAccountID)
	if err != nil {
		return err
	}
	if account.Status != sdk.AccountActive || account.Family != sdk.FamilySocial || account.Health.Status != sdk.HealthHealthy {
		return failSocialPublication(ctx, publications, scope, publication, "channel_unavailable")
	}
	settings, err := accounts.AccountCapabilities(ctx, tenantScope, account.ID)
	if err != nil {
		return err
	}
	allowed := false
	for _, setting := range settings {
		allowed = allowed || (setting.Capability == sdk.Capability(capability) && setting.Enabled)
	}
	if !allowed {
		return failSocialPublication(ctx, publications, scope, publication, "capability_unavailable")
	}
	if len(request.Buttons) > 0 && !socialAccountCapabilityEnabled(settings, social.CapabilityPostButtons) {
		return failSocialPublication(ctx, publications, scope, publication, "capability_unavailable")
	}
	publisher, err := registry.socialPublisher(tenantScope, account)
	if err != nil {
		return failSocialPublication(ctx, publications, scope, publication, "runtime_unavailable")
	}
	runtime, err := connectorruntime.NewForAccount(secretProvider, refreshCoordinator, tenantScope, account)
	if err != nil {
		return err
	}
	publishing, err := changeSocialPublication(ctx, publications, scope, publication, social.PublicationPublishing, "")
	if err != nil {
		return err
	}
	result, publishErr := publisher.PublishSocial(ctx, account, runtime, request, media)
	if publishErr != nil {
		reason := "remote_rejected"
		var remote *sdk.RemoteError
		if errors.As(publishErr, &remote) {
			reason = socialReasonCode(remote.Code)
		}
		_, statusErr := changeSocialPublication(ctx, publications, scope, publishing, social.PublicationFailed, reason)
		return statusErr
	}
	if result.Validate() != nil || result.Status != sdk.SocialRemotePublished {
		_, statusErr := changeSocialPublication(ctx, publications, scope, publishing, social.PublicationFailed, "remote_contract_invalid")
		return statusErr
	}
	if _, err := receipts.Record(ctx, tenantScope, socialdispatchrepo.Receipt{PublicationID: publication.ID.String(), ConnectorAccountID: account.ID, RemotePublicationID: result.RemotePublicationID, ObservedAt: result.ObservedAt}); err != nil {
		return err
	}
	_, err = changeSocialPublication(ctx, publications, scope, publishing, social.PublicationPublished, "")
	return err
}

func socialPublishRequest(publication social.Publication, variant social.ContentVariant) (sdk.SocialPublishRequest, social.Capability, error) {
	if !publication.ID.Valid() || variant.Validate() != nil {
		return sdk.SocialPublishRequest{}, "", social.ErrInvalidRecord
	}
	request := sdk.SocialPublishRequest{PublicationID: publication.ID.String(), Text: variant.Body}
	request.Buttons = make([]sdk.SocialButton, 0, len(variant.Buttons))
	for _, button := range variant.Buttons {
		request.Buttons = append(request.Buttons, sdk.SocialButton{Text: button.Text, URL: button.URL})
	}
	var capability social.Capability
	switch variant.Format {
	case social.FormatText:
		request.Kind = sdk.SocialPostText
		capability = social.CapabilityPostText
	case social.FormatImage, social.FormatGallery:
		request.Kind = sdk.SocialPostMedia
		capability = social.CapabilityPostMedia
	case social.FormatVideo:
		request.Kind = sdk.SocialPostVideo
		capability = social.CapabilityPostVideo
	default:
		return sdk.SocialPublishRequest{}, "", social.ErrCapabilityMissing
	}
	request.Media = make([]sdk.SocialMediaRef, 0, len(variant.Media))
	for _, ref := range variant.Media {
		request.Media = append(request.Media, sdk.SocialMediaRef{UploadID: ref.UploadID, Kind: sdk.SocialMediaKind(ref.Kind), AltText: ref.AltText})
	}
	if request.Validate() != nil {
		return sdk.SocialPublishRequest{}, "", social.ErrInvalidRecord
	}
	return request, capability, nil
}

func socialAccountCapabilityEnabled(settings []sdk.AccountCapabilitySetting, wanted social.Capability) bool {
	for _, setting := range settings {
		if setting.Capability == sdk.Capability(wanted) && setting.Enabled {
			return true
		}
	}
	return false
}

func socialReasonCode(value string) string {
	value = strings.Map(func(character rune) rune {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '_' {
			return character
		}
		if character == '.' || character == '-' {
			return '_'
		}
		return -1
	}, value)
	if len(value) == 0 || len(value) > 64 || value[0] < 'a' || value[0] > 'z' {
		return "remote_rejected"
	}
	return value
}

func recoverSocialPublication(ctx context.Context, publications socialPublicationStore, receipts socialReceiptStore, tenantScope tenancy.Scope, scope social.Scope, publication social.Publication) error {
	_, err := receipts.Receipt(ctx, tenantScope, publication.ID.String())
	if err == nil {
		_, err = changeSocialPublication(ctx, publications, scope, publication, social.PublicationPublished, "")
		return err
	}
	if !errors.Is(err, socialdispatchrepo.ErrNotFound) {
		return err
	}
	_, err = changeSocialPublication(ctx, publications, scope, publication, social.PublicationFailed, "write_outcome_unknown")
	return err
}

func failSocialPublication(ctx context.Context, publications socialPublicationStore, scope social.Scope, publication social.Publication, reason string) error {
	publishing, err := changeSocialPublication(ctx, publications, scope, publication, social.PublicationPublishing, "")
	if err != nil {
		return err
	}
	_, err = changeSocialPublication(ctx, publications, scope, publishing, social.PublicationFailed, reason)
	return err
}

func changeSocialPublication(ctx context.Context, publications socialPublicationStore, scope social.Scope, publication social.Publication, status social.PublicationStatus, reason string) (social.Publication, error) {
	now := time.Now().UTC()
	return publications.ChangePublicationStatus(ctx, scope, social.ChangePublicationStatus{ID: publication.ID, ExpectedVersion: publication.Version, Status: status, ReasonCode: reason}, social.Mutation{EventID: socialWorkerUUID(now), AuditID: socialWorkerUUID(now), ActorID: "system:worker", Source: "worker.social", CorrelationID: publication.ID.String(), OccurredAt: now})
}

func socialWorkerUUID(now time.Time) string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return ""
	}
	millis := uint64(now.UnixMilli())
	value[0], value[1], value[2], value[3], value[4], value[5] = byte(millis>>40), byte(millis>>32), byte(millis>>24), byte(millis>>16), byte(millis>>8), byte(millis)
	value[6], value[8] = (value[6]&0x0f)|0x70, (value[8]&0x3f)|0x80
	raw := hex.EncodeToString(value[:])
	return raw[0:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:32]
}
