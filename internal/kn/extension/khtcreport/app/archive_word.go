package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	pd "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/store"
	exportport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func bindingFromRun(period uuid.UUID, x *store.Export, run exportport.Run) store.WordBinding {
	return store.WordBinding{PeriodID: period, ExportID: x.ID, RunID: run.ID, ExportInputHash: x.InputHash, ExportBodyHash: x.BodyHash, WordVersion: run.Seq, Template: run.Template, TemplateVersion: run.TemplateVersion, TemplateHash: run.TemplateHash, InputHash: run.InputHash, BodyHash: run.BodyHash, Size: run.Size, FileName: run.FileName, ContentType: run.ContentType, DocNumber: run.DocNumber, DocNumberAuto: run.DocNumberAuto, ActorRef: run.ActorRef, At: run.At}
}

func matchesBoundWord(b store.WordBinding, run exportport.Run) bool {
	// Both persisted ledgers use PostgreSQL's microsecond timestamp precision.
	return b.RunID == run.ID && run.Subject == WordSubject(b.PeriodID) && b.WordVersion == run.Seq && b.Template == run.Template && b.TemplateVersion == run.TemplateVersion && b.TemplateHash == run.TemplateHash && b.InputHash == run.InputHash && b.BodyHash == run.BodyHash && b.Size == run.Size && b.FileName == run.FileName && b.ContentType == run.ContentType && b.DocNumber == run.DocNumber && b.DocNumberAuto == run.DocNumberAuto && b.ActorRef == run.ActorRef && b.At.Truncate(time.Microsecond).Equal(run.At.Truncate(time.Microsecond))
}

// bindWord is called after the exporter port has committed and outside any owner
// transaction. Only immutable metadata is appended under the owner's period lock.
func (s *Service) bindWord(ctx context.Context, c caller, period uuid.UUID, x *store.Export, req exportport.Request, run exportport.Run) error {
	renderer := WordTemplate(x.Content.Data.Template)
	if req.Template == fixedWordKey {
		renderer = FixedWordTemplate()
	}
	if renderer.Key != req.Template || renderer.Version != req.Version {
		return ErrDependency
	}
	if err := verifyWordBinding(req, renderer.Hash(), run); err != nil {
		return err
	}
	candidate := bindingFromRun(period, x, run)
	return s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		if err := store.Lock(ctx, tx, c.principal.TenantID, period); err != nil {
			return err
		}
		p, term, err := loadPeriod(ctx, tx, period)
		if err != nil {
			return err
		}
		if err = writer(c, p); err != nil {
			return err
		}
		if term == pd.PeriodCancelled {
			return ErrCancelled
		}
		header, err := store.ArchiveFileHeader(ctx, tx, period, x.ID)
		if err != nil {
			return err
		}
		if header == nil {
			return ErrNotFound
		}
		if header.InputHash != x.InputHash || header.BodyHash != x.BodyHash {
			return ErrDependency
		}
		existing, err := store.WordBindingByRun(ctx, tx, run.ID)
		if err != nil {
			return err
		}
		if existing != nil {
			if existing.PeriodID != period || existing.ExportID != x.ID || existing.ExportInputHash != x.InputHash || existing.ExportBodyHash != x.BodyHash || !matchesBoundWord(*existing, run) {
				return ErrDependency.WithDetail("Word đã được liên kết với bản xuất khác")
			}
			return nil
		}
		return store.InsertWordBinding(ctx, tx, c.principal.TenantID, ids.New(), c.principal.ActorRef, s.now(), candidate)
	})
}

// archiveHeader checks only owner ledgers and immutable file metadata; no draft,
// current source or template provider is consulted by saved-file reads.
func archiveHeader(ctx context.Context, tx pgx.Tx, period, export uuid.UUID) (*store.ExportFileHeader, error) {
	if period == uuid.Nil || export == uuid.Nil {
		return nil, ErrInvalid
	}
	_, term, err := loadPeriod(ctx, tx, period)
	if err != nil {
		return nil, err
	}
	if term == pd.PeriodCancelled {
		return nil, ErrNotFound
	}
	header, err := store.ArchiveFileHeader(ctx, tx, period, export)
	if err != nil {
		return nil, err
	}
	if header == nil {
		return nil, ErrNotFound
	}
	if header.Size <= 0 || header.Version < 1 || len(header.InputHash) != 64 || len(header.BodyHash) != 64 {
		return nil, ErrDependency
	}
	return header, nil
}
func boundWordMetadata(ctx context.Context, tx pgx.Tx, h *store.ExportFileHeader, run uuid.UUID) (*store.WordBinding, error) {
	if run == uuid.Nil {
		return nil, ErrInvalid
	}
	b, err := store.WordBindingByRun(ctx, tx, run)
	if err != nil {
		return nil, err
	}
	if b == nil || b.PeriodID != h.PeriodID || b.ExportID != h.ID {
		return nil, ErrNotFound
	}
	if b.ExportInputHash != h.InputHash || b.ExportBodyHash != h.BodyHash || b.Size <= 0 {
		return nil, ErrDependency
	}
	return b, nil
}
func (s *Service) auditArchive(ctx context.Context, tx pgx.Tx, c caller, action string, selection ArchiveSelection, expected int64, files int, targets []store.ArchiveTarget) error {
	if c.kn.ID == uuid.Nil {
		return problem.Forbidden
	}
	event := store.ArchiveAccess{ID: ids.New(), Actor: c.principal.ActorRef, Context: c.kn.ID, Action: action, Format: selection.Format, ExpectedBytes: expected, FileCount: files, Trace: telemetry.TraceID(ctx), At: s.now(), Targets: targets}
	if selection.PeriodID != uuid.Nil {
		event.PeriodID = &selection.PeriodID
	}
	if selection.ExportID != uuid.Nil {
		event.ExportID = &selection.ExportID
	}
	if selection.RunID != uuid.Nil {
		event.RunID = &selection.RunID
	}
	return store.InsertArchiveAccess(ctx, tx, c.principal.TenantID, event)
}

func (s *Service) readBoundWord(ctx context.Context, b store.WordBinding) (exportport.Run, error) {
	if s.Exporter == nil {
		return exportport.Run{}, ErrDependency
	}
	run, err := s.Exporter.Run(ctx, WordSubject(b.PeriodID), b.RunID)
	if err != nil {
		return exportport.Run{}, err
	}
	if !matchesBoundWord(b, run) || run.Size <= 0 || run.Size != int64(len(run.Body)) {
		return exportport.Run{}, ErrDependency.WithDetail("Metadata Word đã lưu không khớp chứng cứ liên kết")
	}
	hash := sha256.Sum256(run.Body)
	if hex.EncodeToString(hash[:]) != b.BodyHash {
		return exportport.Run{}, ErrDependency.WithDetail("Word đã lưu không khớp băm")
	}
	return run, nil
}

// DownloadArchiveWord commits a read-attempt audit before calling the stored-file
// port. It never resolves placeholders, renders Word or allocates document numbers.
func (s *Service) DownloadArchiveWord(ctx context.Context, period, export, runID uuid.UUID) (exportport.Run, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return exportport.Run{}, err
	}
	if s.Exporter == nil {
		return exportport.Run{}, ErrDependency
	}
	var binding *store.WordBinding
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		header, e := archiveHeader(ctx, tx, period, export)
		if e != nil {
			return e
		}
		binding, e = boundWordMetadata(ctx, tx, header, runID)
		if e != nil {
			return e
		}
		target := store.ArchiveTarget{PeriodID: period, ExportID: export, RunID: &runID, Format: "word"}
		return s.auditArchive(ctx, tx, c, "word_attempt", ArchiveSelection{PeriodID: period, ExportID: export, RunID: runID, Format: "word"}, binding.Size, 1, []store.ArchiveTarget{target})
	})
	if err != nil {
		return exportport.Run{}, err
	}
	run, e := s.readBoundWord(ctx, *binding)
	if e != nil {
		return exportport.Run{}, e
	}
	if e = s.archiveEgress(ctx, c, 1, run.Size); e != nil {
		return exportport.Run{}, e
	}
	return run, nil
}

type ArchiveWordsQuery struct{ Offset, Limit int }

func (q ArchiveWordsQuery) Validate() error {
	if q.Offset < 0 || q.Offset > 100000 || q.Limit < 1 || q.Limit > 50 {
		return ErrInvalid
	}
	return nil
}

type ArchiveWordsResult struct {
	PeriodID      uuid.UUID           `json:"period_id"`
	ExportID      uuid.UUID           `json:"export_id"`
	ExportVersion int64               `json:"export_version"`
	ExportName    string              `json:"export_name"`
	Items         []store.WordBinding `json:"items"`
	HasMore       bool                `json:"has_more"`
	Offset        int                 `json:"offset"`
}

func (s *Service) ArchiveWords(ctx context.Context, period, export uuid.UUID, q ArchiveWordsQuery) (ArchiveWordsResult, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return ArchiveWordsResult{}, err
	}
	if err = q.Validate(); err != nil {
		return ArchiveWordsResult{}, err
	}
	out := ArchiveWordsResult{PeriodID: period, ExportID: export, Offset: q.Offset, Items: []store.WordBinding{}}
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		header, e := archiveHeader(ctx, tx, period, export)
		if e != nil {
			return e
		}
		out.ExportVersion = header.Version
		out.ExportName = header.Name
		out.Items, e = store.WordBindings(ctx, tx, period, export, q.Offset, q.Limit+1)
		if e != nil {
			return e
		}
		if len(out.Items) > q.Limit {
			out.HasMore = true
			out.Items = out.Items[:q.Limit]
		}
		targets := make([]store.ArchiveTarget, 0, len(out.Items))
		for _, b := range out.Items {
			if b.ExportInputHash != header.InputHash || b.ExportBodyHash != header.BodyHash {
				return ErrDependency
			}
			id := b.RunID
			targets = append(targets, store.ArchiveTarget{PeriodID: period, ExportID: export, RunID: &id, Format: "word"})
		}
		return s.auditArchive(ctx, tx, c, "word_list", ArchiveSelection{PeriodID: period, ExportID: export, Format: "word"}, 0, len(out.Items), targets)
	})
	return out, err
}

func (s *Service) archiveEgress(ctx context.Context, c caller, records, size int64) error {
	return meter.Guard(ctx, s.Egress, s.EgressMode, meter.Egress{Internal: true, SourceID: ids.New(), Channel: meter.ChannelReportExport, DataTenantID: c.principal.TenantID, CounterpartyKind: meter.CounterpartyActor, CounterpartyRef: c.principal.ActorRef, Product: "kn_khtc_report", Records: records, Bytes: size, OccurredAt: s.now(), ActorRef: c.principal.ActorRef, TraceID: telemetry.TraceID(ctx)}, nil)
}
