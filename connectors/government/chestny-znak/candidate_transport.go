package chestnyznak

import (
	"context"
	"time"
)

type candidateTransport struct{}

func (candidateTransport) Ping(context.Context, []byte) error { return nil }
func (candidateTransport) ProductByGTIN(_ context.Context, _ []byte, g string) (ProductResponse, error) {
	return ProductResponse{GTIN: g, RemoteID: "product:synthetic", Status: "published", Name: "Synthetic", ObservedAt: time.Date(2026, 8, 12, 3, 0, 0, 0, time.UTC)}, nil
}
func (candidateTransport) CodeStatuses(_ context.Context, _ []byte, c []string) ([]CodeResponse, error) {
	if len(c) == 0 {
		c = []string{"synthetic-code"}
	}
	return []CodeResponse{{Code: c[0], Status: "circulation", GTIN: "04601234567890", ObservedAt: time.Date(2026, 8, 12, 3, 0, 0, 0, time.UTC)}}, nil
}
