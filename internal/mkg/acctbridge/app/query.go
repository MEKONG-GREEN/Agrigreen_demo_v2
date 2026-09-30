package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// ClampLimit giới hạn cỡ trang (mặc định 50, tối đa 200).
func ClampLimit(n int) int {
	switch {
	case n <= 0:
		return 50
	case n > 200:
		return 200
	}
	return n
}

// Journals liệt kê bút toán mới → cũ (con trỏ = journal_id cuối trang).
func (s *Service) Journals(ctx context.Context, f store.JournalFilter) ([]store.Journal, *uuid.UUID, error) {
	switch f.Status {
	case "", domain.StatusBuilt, domain.StatusApproved, domain.StatusPushed, domain.StatusPushFailed, domain.StatusSuperseded:
	default:
		return nil, nil, problem.Validation.WithField("status", "built | approved | pushed | push_failed | superseded")
	}
	f.Limit = ClampLimit(f.Limit)
	lim := f.Limit
	f.Limit++
	var items []store.Journal
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		items, e = store.Journals(ctx, tx, f)
		return e
	})
	var next *uuid.UUID
	if len(items) > lim {
		items = items[:lim]
		id := items[lim-1].JournalID
		next = &id
	}
	return items, next, err
}

// JournalView là một bút toán kèm các lần đẩy.
type JournalView struct {
	Journal  store.Journal
	Attempts []store.Attempt
}

// Journal đọc một bút toán (dòng, trạng thái, lần đẩy).
func (s *Service) Journal(ctx context.Context, id uuid.UUID) (JournalView, error) {
	var v JournalView
	err := s.read(ctx, func(tx pgx.Tx) error {
		jr, err := store.GetJournal(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if jr == nil {
			return ErrJournalNotFound
		}
		v.Journal = *jr
		v.Attempts, err = store.Attempts(ctx, tx, id)
		return err
	})
	return v, err
}

// Job đọc trạng thái một job dựng / đẩy.
func (s *Service) Job(ctx context.Context, id uuid.UUID) (store.Job, error) {
	var j *store.Job
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		j, e = store.JobByID(ctx, tx, id, false)
		return e
	})
	if err != nil {
		return store.Job{}, err
	}
	if j == nil {
		return store.Job{}, ErrJobNotFound
	}
	return *j, nil
}

// Export là tệp xuất của bút toán.
type Export struct {
	Filename    string
	ContentType string
	Body        []byte
	SHA256      string
}

// Export dựng tệp xuất (csv | xml) của bút toán — cùng byte với bộ xuất tệp (tất định, B-06).
func (s *Service) Export(ctx context.Context, id uuid.UUID, format string) (Export, error) {
	v, err := s.Journal(ctx, id)
	if err != nil {
		return Export{}, err
	}
	sj := SummaryJournal(v.Journal)
	body, ct, ok := domain.Render(sj, format)
	if !ok {
		return Export{}, ErrFormatUnsupported
	}
	return Export{Filename: domain.FileStem(sj) + "." + format, ContentType: ct, Body: body, SHA256: domain.SHA256Hex(body)}, nil
}

// RebuildResult tổng kết một lần dựng lại bảng chiếu.
type RebuildResult struct {
	MappingEvents int
	JournalEvents int
	Journals      int
}

// RebuildProjections dựng lại mapping_current + journal_current từ sổ (R-22, diễn tập) trong MỘT giao
// dịch: xóa bảng chiếu, phát lại sổ theo thứ tự ghi, khôi phục trạng thái thử lại từ push_attempt.
// Vai chief_accountant (bề mặt quản trị MFA).
func (s *Service) RebuildProjections(ctx context.Context) (RebuildResult, error) {
	p, err := authz.Require(ctx, RoleChief)
	if err != nil {
		return RebuildResult{}, err
	}
	var res RebuildResult
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockMapping(ctx, tx, p.TenantID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "LOCK TABLE mkg_acctbridge.journal_current IN SHARE ROW EXCLUSIVE MODE"); err != nil {
			return err
		}
		if err := store.ClearProjections(ctx, tx); err != nil {
			return err
		}
		mes, err := store.MappingEvents(ctx, tx)
		if err != nil {
			return err
		}
		for _, e := range mes {
			if err := store.ApplyMappingEvent(ctx, tx, e); err != nil {
				return err
			}
		}
		jes, err := store.JournalEvents(ctx, tx)
		if err != nil {
			return err
		}
		seen := map[uuid.UUID]bool{}
		for _, e := range jes {
			if err := store.ApplyJournalEvent(ctx, tx, e); err != nil {
				return err
			}
			seen[e.JournalID] = true
		}
		res = RebuildResult{MappingEvents: len(mes), JournalEvents: len(jes), Journals: len(seen)}
		return store.RestoreRetryState(ctx, tx)
	})
	return res, err
}
