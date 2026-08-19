package cian

import (
	"context"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func (c *Connector) ReadClassifiedPublicationStatus(ctx context.Context, a sdk.Account, rt sdk.Runtime, remoteTaskID string) (out sdk.ClassifiedPublicationStatus, err error) {
	if sdk.RequireCapability(Manifest(), "classified.publications.status.read") != nil || !validRemoteID(remoteTaskID) {
		return out, sdk.ErrInvalidClassified
	}
	err = c.with(ctx, a, rt, func(cfg Configuration, token []byte) error {
		r, err := c.transport.Do(ctx, request(OperationImportReport, token))
		if err != nil {
			return remote(sdk.ErrorUnavailable, "transport_unavailable", "", 0)
		}
		if err = normalize(r); err != nil {
			return err
		}
		ev, err := parseImportEvidence(r.Body)
		if err != nil || ev.FeedURL != cfg.FeedURL || ev.OrderID != remoteTaskID {
			return ErrInvalidResponse
		}
		state := sdk.ClassifiedPublicationSucceeded
		if ev.HasProblems {
			state = sdk.ClassifiedPublicationFailed
		} else if ev.ProcessedAt.IsZero() {
			state = sdk.ClassifiedPublicationProcessing
		}
		out = sdk.ClassifiedPublicationStatus{
			RemoteTaskID: ev.OrderID, State: state, Total: ev.Total, Inserted: ev.Inserted,
			Updated: ev.Updated, Deleted: ev.Deleted, Skipped: ev.Skipped, Errors: ev.Errors,
			Notices: ev.Notices, CheckedAt: c.now().UTC(),
		}
		if out.Validate() != nil {
			return ErrInvalidResponse
		}
		return nil
	})
	return out, err
}
