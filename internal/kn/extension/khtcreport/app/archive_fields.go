package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (q ArchiveQuery) fields() domain.ArchiveFieldSelector {
	return domain.ArchiveFieldSelector{Code: q.Field, Status: q.FieldStatus}
}
func (q ArchiveQuery) scanQuery() store.ArchiveScanQuery {
	return store.ArchiveScanQuery{Year: q.Year, Kind: q.Kind, Search: strings.TrimSpace(q.Search), PeriodSearch: strings.TrimSpace(q.PeriodSearch), Versions: q.AllVersions, PeriodID: q.PeriodID}
}

type ArchiveFieldsResult struct {
	Items      []store.ArchiveFieldChoice `json:"items"`
	HasMore    bool                       `json:"has_more"`
	NextCursor string                     `json:"next_cursor"`
}

func (s *Service) ArchiveFields(ctx context.Context, q ArchiveQuery) (ArchiveFieldsResult, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return ArchiveFieldsResult{}, err
	}
	if err = q.Validate(); err != nil {
		return ArchiveFieldsResult{}, err
	}
	out := ArchiveFieldsResult{Items: []store.ArchiveFieldChoice{}}
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		var e error
		out.Items, e = store.ArchiveFieldChoices(ctx, tx, q.scanQuery(), q.fields(), q.After, q.Limit+1)
		return e
	})
	if err != nil {
		return ArchiveFieldsResult{}, err
	}
	if len(out.Items) > q.Limit {
		out.HasMore = true
		out.Items = out.Items[:q.Limit]
		out.NextCursor = out.Items[len(out.Items)-1].Code
	}
	return out, nil
}

// Functional field filtering is never an authority grant. Callers retain reader
// and tenant/period checks before this metadata-only predicate.
func requireArchiveField(ctx context.Context, tx pgx.Tx, period, export uuid.UUID, field domain.ArchiveFieldSelector) (*domain.ArchiveFields, error) {
	if !field.Valid() {
		return nil, ErrInvalid
	}
	meta, err := store.ArchiveFieldsByID(ctx, tx, period, export)
	if err != nil {
		return nil, err
	}
	if meta == nil || !field.Matches(*meta) {
		return nil, ErrNotFound
	}
	return meta, nil
}
func (s *Service) CheckArchiveField(ctx context.Context, period, export uuid.UUID, field domain.ArchiveFieldSelector) error {
	c, err := s.reader(ctx)
	if err != nil {
		return err
	}
	if !field.Valid() {
		return ErrInvalid
	}
	return s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		if _, _, e := loadPeriod(ctx, tx, period); e != nil {
			return e
		}
		_, e := requireArchiveField(ctx, tx, period, export, field)
		return e
	})
}
func (s *Service) ArchiveHistory(ctx context.Context, period uuid.UUID, field domain.ArchiveFieldSelector) ([]store.Export, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	if !field.Valid() {
		return nil, ErrInvalid
	}
	out := []store.Export{}
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		p, _, e := loadPeriod(ctx, tx, period)
		if e != nil {
			return e
		}
		out, e = store.ArchiveHistoryFiltered(ctx, tx, store.ArchiveScanQuery{Year: p.Year, PeriodID: period, Versions: true}, field)
		return e
	})
	return out, err
}

// Excel is already immutable bytes. Reading it must not deserialize unrelated
// legacy decimal fields merely to calculate classification or egress records.
func (s *Service) DownloadArchiveExcel(ctx context.Context, period, export uuid.UUID, field domain.ArchiveFieldSelector) (store.ArchiveStoredBody, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return store.ArchiveStoredBody{}, err
	}
	if !field.Valid() {
		return store.ArchiveStoredBody{}, ErrInvalid
	}
	var body *store.ArchiveStoredBody
	var count int
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		if _, _, e := loadPeriod(ctx, tx, period); e != nil {
			return e
		}
		meta, e := requireArchiveField(ctx, tx, period, export, field)
		if e != nil {
			return e
		}
		if !meta.RowCountKnown {
			return ErrDependency.WithDetail("Không xác định được số dòng trong cấu trúc bản xuất đã lưu")
		}
		count = meta.AllRows
		body, e = store.ArchiveBody(ctx, tx, period, export)
		return e
	})
	if err != nil {
		return store.ArchiveStoredBody{}, err
	}
	if body == nil {
		return store.ArchiveStoredBody{}, ErrNotFound
	}
	sum := sha256.Sum256(body.Body)
	if hex.EncodeToString(sum[:]) != body.BodyHash {
		return store.ArchiveStoredBody{}, ErrDependency.WithDetail("Tệp lưu không khớp băm")
	}
	// Preserve the existing egress contract: count the entire frozen rows array,
	// not selected items or inferred0. This records access, never network delivery.
	_ = meter.Guard(ctx, s.Egress, s.EgressMode, meter.Egress{Internal: true, SourceID: ids.New(), Channel: meter.ChannelReportExport, DataTenantID: c.principal.TenantID, CounterpartyKind: meter.CounterpartyActor, CounterpartyRef: c.principal.ActorRef, Product: "kn_khtc_report", Records: int64(count), Bytes: int64(len(body.Body)), OccurredAt: s.now(), ActorRef: c.principal.ActorRef, TraceID: telemetry.TraceID(ctx)}, nil)
	return *body, nil
}
