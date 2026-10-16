package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	pd "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	ps "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/store"
	ss "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcsource/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type SaveInput struct {
	BaseVersion int64              `json:"base_version"`
	Admin       map[string]*string `json:"admin"`
	Texts       map[string]string  `json:"texts"`
}
type ExportInput struct {
	BaseVersion            int64         `json:"base_version"`
	AcknowledgeProvisional bool          `json:"acknowledge_provisional"`
	Filter                 domain.Filter `json:"filter"`
}

func sameVersion(draft *store.Draft, base int64) bool {
	if draft == nil {
		return base == 0
	}
	return draft.Version == base
}
func (s *Service) writablePeriod(ctx context.Context, id uuid.UUID) (caller, ps.Period, string, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return caller{}, ps.Period{}, "", err
	}
	var p ps.Period
	var term string
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		var e error
		p, term, e = loadPeriod(ctx, tx, id)
		if e != nil {
			return e
		}
		if e = writer(c, p); e != nil {
			return e
		}
		if term == pd.PeriodCancelled {
			return ErrCancelled
		}
		final, e := store.GetFinal(ctx, tx, id)
		if e != nil {
			return e
		}
		if final != nil {
			return ErrFinal
		}
		return nil
	})
	return c, p, term, err
}

func (s *Service) Save(ctx context.Context, id uuid.UUID, in SaveInput, idem string) (Detail, error) {
	if idem == "" {
		return Detail{}, problem.IdempotencyRequired
	}
	c, p, _, err := s.writablePeriod(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	edits := domain.Edits{Admin: in.Admin, Texts: in.Texts}
	if err = edits.Validate(domain.Template(p.Type)); err != nil {
		return Detail{}, ErrInvalid.WithCause(err)
	}
	key := "khtc:report:save:" + id.String() + ":" + idem
	var draft *store.Draft
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error { var e error; draft, e = store.LatestDraft(ctx, tx, id); return e })
	if err != nil {
		return Detail{}, err
	}
	var content domain.Draft
	if draft == nil {
		data, defaults, e := s.capture(ctx, c, p)
		if e != nil {
			return Detail{}, e
		}
		content, e = s.initialDraft(ctx, c, data, defaults)
		if e != nil {
			return Detail{}, e
		}
	} else {
		content = draft.Content
	}
	content, err = domain.Edit(content, edits, c.principal.ActorRef, s.now())
	if err != nil {
		return Detail{}, ErrInvalid.WithCause(err)
	}
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		if e := store.Lock(ctx, tx, c.principal.TenantID, id); e != nil {
			return e
		}
		if done, e := store.DraftByIdem(ctx, tx, key); e != nil || done != nil {
			return e
		}
		_, term, e := loadPeriod(ctx, tx, id)
		if e != nil {
			return e
		}
		if term == pd.PeriodCancelled {
			return ErrCancelled
		}
		final, e := store.GetFinal(ctx, tx, id)
		if e != nil {
			return e
		}
		if final != nil {
			return ErrFinal
		}
		latest, e := store.LatestDraft(ctx, tx, id)
		if e != nil {
			return e
		}
		if !sameVersion(latest, in.BaseVersion) {
			return ErrVersion
		}
		row := store.Draft{ID: ids.New(), PeriodID: id, Action: "save", Content: content, InputHash: content.Data.InputHash, ActorRef: c.principal.ActorRef, At: s.now()}
		if e := store.InsertDraft(ctx, tx, c.principal.TenantID, &row, key); e != nil {
			return fmt.Errorf("insert report draft: %w", e)
		}
		return nil
	})
	if err != nil {
		return Detail{}, classify(err)
	}
	return s.Get(ctx, id)
}

func (s *Service) Refresh(ctx context.Context, id uuid.UUID, base int64, idem string) (Detail, error) {
	if idem == "" {
		return Detail{}, problem.IdempotencyRequired
	}
	c, p, term, err := s.writablePeriod(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	if term != "" {
		return Detail{}, ErrChanged.WithDetail("Kỳ đã đóng giữ số đã chốt")
	}
	var prior *store.Draft
	if err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error { var e error; prior, e = store.LatestDraft(ctx, tx, id); return e }); err != nil {
		return Detail{}, err
	}
	if !sameVersion(prior, base) {
		return Detail{}, ErrVersion
	}
	// Never hold the report/snapshot lock while invoking another transaction's source refresh.
	if s.Sources == nil {
		return Detail{}, ErrDependency
	}
	if _, err = s.Sources.Refresh(ctx, id, "report:"+id.String()+":"+idem); err != nil {
		return Detail{}, fmt.Errorf("refresh report sources: %w", classify(err))
	}
	data, defaults, err := s.capture(ctx, c, p)
	if err != nil {
		return Detail{}, err
	}
	old := domain.Draft{}
	if prior != nil {
		old = prior.Content
	}
	content := domain.Refresh(old, data, defaults)
	if prior == nil {
		content, err = s.initialDraft(ctx, c, data, defaults)
		if err != nil {
			return Detail{}, err
		}
	}
	key := "khtc:report:refresh:" + id.String() + ":" + idem
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		if e := store.Lock(ctx, tx, c.principal.TenantID, id); e != nil {
			return e
		}
		if done, e := store.DraftByIdem(ctx, tx, key); e != nil || done != nil {
			return e
		}
		latest, e := store.LatestDraft(ctx, tx, id)
		if e != nil {
			return e
		}
		if !sameVersion(latest, base) {
			return ErrVersion
		}
		_, term, e := loadPeriod(ctx, tx, id)
		if e != nil {
			return e
		}
		if term != "" {
			return ErrChanged.WithDetail("Kỳ đã đóng giữ số đã chốt")
		}
		row := store.Draft{ID: ids.New(), PeriodID: id, Action: "refresh", Content: content, InputHash: content.Data.InputHash, ActorRef: c.principal.ActorRef, At: s.now()}
		if e := store.InsertDraft(ctx, tx, c.principal.TenantID, &row, key); e != nil {
			return fmt.Errorf("insert report draft: %w", e)
		}
		return nil
	})
	if err != nil {
		return Detail{}, classify(err)
	}
	return s.Get(ctx, id)
}

func (s *Service) Export(ctx context.Context, id uuid.UUID, in ExportInput, idem string) (store.Export, error) {
	if err := in.Filter.Validate(); err != nil {
		return store.Export{}, ErrInvalid.WithCause(err)
	}
	if idem == "" {
		return store.Export{}, problem.IdempotencyRequired
	}
	c, _, _, err := s.writablePeriod(ctx, id)
	if err != nil {
		return store.Export{}, err
	}
	key := "khtc:report:export:" + id.String() + ":" + idem
	var result *store.Export
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		if e := store.Lock(ctx, tx, c.principal.TenantID, id); e != nil {
			return e
		}
		var e error
		if result, e = store.ExportByIdem(ctx, tx, key); e != nil || result != nil {
			return e
		}
		final, e := store.GetFinal(ctx, tx, id)
		if e != nil {
			return e
		}
		if final != nil {
			return ErrFinal
		}
		draft, e := store.LatestDraft(ctx, tx, id)
		if e != nil {
			return e
		}
		if draft == nil || !sameVersion(draft, in.BaseVersion) {
			return ErrVersion.WithDetail("Lưu bản soạn hiện tại trước khi xuất")
		}
		provisional := len(draft.Content.Data.Provisional) > 0
		if provisional && !in.AcknowledgeProvisional {
			return ErrProvisional
		}
		latest, e := store.LatestExport(ctx, tx, id)
		if e != nil {
			return e
		}
		version := int64(1)
		if latest != nil {
			version = latest.Version + 1
		}
		body, e := RenderXLSX(draft.Content, in.Filter, version)
		if e != nil {
			return e
		}
		hash := sha256.Sum256(body)
		result = &store.Export{ID: ids.New(), PeriodID: id, DraftID: draft.ID, Content: draft.Content, InputHash: draft.InputHash, Filter: in.Filter,
			FileName: fmt.Sprintf("bao-cao-khtc-%s-%s-v%d.xlsx", draft.Content.Data.Template, id.String(), version), Body: body, BodyHash: hex.EncodeToString(hash[:]),
			Provisional: provisional, ActorRef: c.principal.ActorRef, At: s.now()}
		if e = store.InsertExport(ctx, tx, c.principal.TenantID, result, key); e != nil {
			return e
		}
		if result.Version != version {
			return ErrVersion
		}
		return nil
	})
	if err != nil {
		return store.Export{}, classify(err)
	}
	return *result, nil
}

func (s *Service) Finalize(ctx context.Context, id, exportID uuid.UUID, idem string) (Detail, error) {
	if idem == "" {
		return Detail{}, problem.IdempotencyRequired
	}
	c, err := s.reader(ctx)
	if err != nil {
		return Detail{}, err
	}
	var p ps.Period
	if err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		var e error
		p, _, e = loadPeriod(ctx, tx, id)
		if e != nil {
			return e
		}
		return writer(c, p)
	}); err != nil {
		return Detail{}, err
	}
	detail, err := s.Get(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	if detail.Final != nil {
		if detail.Final.ExportID == exportID {
			return detail, nil
		}
		return Detail{}, ErrFinal
	}
	if detail.SourceError != "" {
		return Detail{}, ErrDependency.WithDetail(detail.SourceError)
	}
	if detail.SourceChanged {
		return Detail{}, ErrChanged
	}
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		if e := store.Lock(ctx, tx, c.principal.TenantID, id); e != nil {
			return e
		}
		final, e := store.GetFinal(ctx, tx, id)
		if e != nil {
			return e
		}
		if final != nil {
			if final.ExportID == exportID {
				return nil
			}
			return ErrFinal
		}
		x, e := store.ExportByID(ctx, tx, id, exportID)
		if e != nil {
			return e
		}
		if x == nil {
			return ErrNotFound
		}
		if x.Provisional {
			return ErrProvisional
		}
		draft, e := store.LatestDraft(ctx, tx, id)
		if e != nil {
			return e
		}
		if draft == nil || x.DraftID != draft.ID {
			return ErrVersion
		}
		snapshot, e := ss.LatestSnapshot(ctx, tx, id)
		if e != nil {
			return e
		}
		if x.Content.Data.Sources == nil || snapshot == nil || x.Content.Data.Sources.ID != snapshot.ID {
			return ErrChanged
		}
		return store.InsertFinal(ctx, tx, c.principal.TenantID, store.Final{ID: ids.New(), PeriodID: id, ExportID: exportID, ActorRef: c.principal.ActorRef, At: s.now()}, "khtc:report:final:"+id.String()+":"+idem)
	})
	if err != nil {
		return Detail{}, classify(err)
	}
	return s.Get(ctx, id)
}

func (s *Service) History(ctx context.Context, id uuid.UUID) ([]store.Export, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	var out []store.Export
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		if _, _, e := loadPeriod(ctx, tx, id); e != nil {
			return e
		}
		var e error
		out, e = store.ExportHeaders(ctx, tx, id, 100)
		return e
	})
	return out, err
}
func (s *Service) Download(ctx context.Context, id, exportID uuid.UUID) (store.Export, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return store.Export{}, err
	}
	var out *store.Export
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		if _, _, e := loadPeriod(ctx, tx, id); e != nil {
			return e
		}
		var e error
		out, e = store.ExportByID(ctx, tx, id, exportID)
		return e
	})
	if err != nil {
		return store.Export{}, err
	}
	if out == nil {
		return store.Export{}, ErrNotFound
	}
	hash := sha256.Sum256(out.Body)
	if hex.EncodeToString(hash[:]) != out.BodyHash {
		return store.Export{}, ErrDependency.WithDetail("Tệp lưu không khớp băm")
	}
	_ = meter.Guard(ctx, s.Egress, s.EgressMode, meter.Egress{
		Internal: true, SourceID: ids.New(), Channel: meter.ChannelReportExport, DataTenantID: c.principal.TenantID,
		CounterpartyKind: meter.CounterpartyActor, CounterpartyRef: c.principal.ActorRef, Product: "kn_khtc_report",
		Records: int64(len(out.Content.Data.Rows)), Bytes: int64(len(out.Body)), OccurredAt: s.now(), ActorRef: c.principal.ActorRef, TraceID: telemetry.TraceID(ctx),
	}, nil)
	return *out, nil
}
