package app

import (
	"context"

	pd "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/store"
	exportport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// directWordBinding resolves only the immutable owner attestation. A filename,
// sequence or template match is never evidence linking a legacy run to a snapshot.
func directWordBinding(ctx context.Context, tx pgx.Tx, period, run uuid.UUID) (*store.WordBinding, error) {
	binding, err := store.WordBindingByRun(ctx, tx, run)
	if err != nil {
		return nil, err
	}
	if binding == nil {
		return nil, ErrDependency.WithDetail("Word đã lưu chưa có chứng cứ liên kết với bản xuất; chưa thể xem hoặc tải")
	}
	if binding.PeriodID != period {
		return nil, ErrNotFound
	}
	header, err := archiveHeader(ctx, tx, period, binding.ExportID)
	if err != nil {
		return nil, err
	}
	if binding.ExportInputHash != header.InputHash || binding.ExportBodyHash != header.BodyHash || binding.Size <= 0 {
		return nil, ErrDependency
	}
	return binding, nil
}

// WordHistory preserves the direct history DTO and the exporter's existing
// 100-run bound. The committed read event is an access attempt, not delivery.
// Unattested legacy metadata is unavailable, never silently linked or backfilled.
func (s *Service) WordHistory(ctx context.Context, id uuid.UUID) ([]exportport.Run, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	if s.Exporter == nil {
		return nil, ErrDependency
	}
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		_, terminal, e := loadPeriod(ctx, tx, id)
		if e != nil {
			return e
		}
		if terminal == pd.PeriodCancelled {
			return ErrNotFound
		}
		return s.auditArchive(ctx, tx, c, "read_history", ArchiveSelection{PeriodID: id, Format: "metadata"}, 0, 0, nil)
	})
	if err != nil {
		return nil, err
	}
	runs, err := s.Exporter.Runs(ctx, WordSubject(id))
	if err != nil {
		return nil, err
	}
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		for _, run := range runs {
			binding, e := directWordBinding(ctx, tx, id, run.ID)
			if e != nil {
				return e
			}
			if !matchesBoundWord(*binding, run) {
				return ErrDependency.WithDetail("Metadata Word đã lưu không khớp chứng cứ liên kết")
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return runs, nil
}

// WordDownload keeps the old URL as an alias of the audited snapshot reader.
// Resolving a binding is owner metadata only; stored-byte I/O happens after the
// snapshot reader commits word_attempt. Reads never create a missing binding.
func (s *Service) WordDownload(ctx context.Context, id, runID uuid.UUID) (exportport.Run, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return exportport.Run{}, err
	}
	var binding *store.WordBinding
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		if _, _, e := loadPeriod(ctx, tx, id); e != nil {
			return e
		}
		var e error
		binding, e = directWordBinding(ctx, tx, id, runID)
		return e
	})
	if err != nil {
		return exportport.Run{}, err
	}
	return s.DownloadArchiveWord(ctx, id, binding.ExportID, runID)
}
