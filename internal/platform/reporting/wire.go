package reporting

import (
	"encoding/json"
	"time"
)

// EventFactRow is the exact Task-049 JSONEachRow shape for
// torgnexa_reporting.event_fact_v1. It intentionally contains AnalyticsData
// rather than the original EventBus payload.
type EventFactRow struct {
	EventID           string          `json:"event_id"`
	EventType         string          `json:"event_type"`
	OccurredAt        time.Time       `json:"occurred_at"`
	IngestedAt        time.Time       `json:"ingested_at"`
	OrganizationID    string          `json:"organization_id"`
	WorkspaceID       string          `json:"workspace_id"`
	EntityType        string          `json:"entity_type"`
	EntityID          string          `json:"entity_id"`
	Source            string          `json:"source"`
	CorrelationID     string          `json:"correlation_id"`
	CausationID       string          `json:"causation_id"`
	ActorID           string          `json:"actor_id"`
	TraceID           string          `json:"trace_id"`
	AnalyticsDataJSON json.RawMessage `json:"analytics_data_json"`
	ReplayID          string          `json:"replay_id"`
	SourceStream      string          `json:"source_stream"`
	SourcePartition   int32           `json:"source_partition"`
	SourceOffset      int64           `json:"source_offset"`
	IngestVersion     uint64          `json:"ingest_version"`
}

func (r EventFactRow) Validate() error {
	if r.EventID == "" || r.EventType == "" || !isUTC(r.OccurredAt) || !isUTC(r.IngestedAt) ||
		r.OrganizationID == "" || r.WorkspaceID == "" || r.EntityType == "" || r.EntityID == "" ||
		r.Source == "" || len(r.AnalyticsDataJSON) == 0 || r.IngestVersion == 0 {
		return ErrInvalid
	}
	return nil
}

func EventFactRows(batch Batch) ([]EventFactRow, error) {
	validated, err := NewBatch(batch.Records)
	if err != nil || validated.DedupToken != batch.DedupToken {
		return nil, ErrInvalid
	}
	rows := make([]EventFactRow, 0, len(batch.Records))
	for _, record := range batch.Records {
		row := EventFactRow{
			EventID: record.EventID, EventType: record.EventType.String(), OccurredAt: record.OccurredAt.Time(),
			IngestedAt: record.IngestedAt.Time(), OrganizationID: record.OrganizationID, WorkspaceID: record.WorkspaceID,
			EntityType: record.EntityType, EntityID: record.EntityID, Source: record.SourceSystem,
			CorrelationID: record.CorrelationID, CausationID: record.CausationID, ActorID: record.ActorID, TraceID: record.TraceID,
			AnalyticsDataJSON: append(json.RawMessage(nil), record.AnalyticsData...), ReplayID: record.ReplayID,
			SourceStream: record.Source.Stream, SourcePartition: record.Source.Partition, SourceOffset: record.Source.Offset,
			IngestVersion: record.IngestVersion,
		}
		if row.Validate() != nil {
			return nil, ErrInvalid
		}
		rows = append(rows, row)
	}
	return rows, nil
}
