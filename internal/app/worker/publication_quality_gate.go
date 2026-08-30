package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/publicationqualityrepo"
	"github.com/torgnexa/torgnexa/internal/platform/publicationquality"
)

// commercePublicationQualityGate is the narrow pre-egress contract. It does
// not assemble or mutate canonical product data; it only requires a current,
// target-specific receipt produced by the quality center.
type commercePublicationQualityGate interface {
	CheckProduct(context.Context, tenancy.Scope, sdk.Account, string, int64, time.Time) error
}

type postgresPublicationQualityGate struct {
	repository *publicationqualityrepo.Repository
}

func (g postgresPublicationQualityGate) CheckProduct(ctx context.Context, scope tenancy.Scope, account sdk.Account, productID string, productVersion int64, now time.Time) error {
	if g.repository == nil {
		return errors.New("publication quality gate: repository unavailable")
	}
	receipt, err := g.repository.CurrentReceipt(ctx, scope, productID, account.ID, account.ConnectorID, productVersion, now.UTC())
	if errors.Is(err, publicationquality.ErrNotFound) {
		return publicationquality.ErrGateDenied
	}
	if err != nil {
		return fmt.Errorf("publication quality gate: read receipt: %w", err)
	}
	if err := receipt.Validate(); err != nil {
		return publicationquality.ErrGateDenied
	}
	return nil
}
