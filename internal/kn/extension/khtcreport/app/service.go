// Package app implements the minimum G1 KHTC editor and immutable Excel releases.
package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	period "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	pstore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/store"
	source "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcsource/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	forms "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	party "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrNotFound    = problem.New(http.StatusNotFound, "extension.khtc_report_not_found", "Không tìm thấy bản báo cáo")
	ErrFinal       = problem.New(http.StatusConflict, "extension.khtc_report_final", "Báo cáo đã có Bản cuối, chỉ được xem và tải lại")
	ErrVersion     = problem.New(http.StatusConflict, "extension.khtc_report_version_conflict", "Bản soạn đã thay đổi, tải lại trước khi tiếp tục")
	ErrProvisional = problem.New(http.StatusConflict, "extension.khtc_report_provisional", "Báo cáo còn mục Tạm tính")
	ErrChanged     = problem.New(http.StatusConflict, "extension.khtc_report_source_changed", "Nguồn đã thay đổi, cần cập nhật số liệu trước khi đánh dấu Bản cuối")
	ErrInvalid     = problem.New(http.StatusUnprocessableEntity, "extension.khtc_report_invalid", "Nội dung bản soạn không hợp lệ")
	ErrDependency  = problem.New(http.StatusServiceUnavailable, "extension.dependency_unavailable", "Dịch vụ phụ thuộc chưa sẵn sàng")
	ErrCancelled   = problem.New(http.StatusConflict, "extension.khtc_period_terminal", "Kỳ báo cáo đã hủy")
)

type Service struct {
	DB         tenant.Beginner
	Clock      clock.Clock
	Plans      port.KHTCPlans
	Forms      forms.Entry
	Sources    *source.Service
	Egress     meter.Checkpoint
	EgressMode meter.Mode
}
type caller struct {
	principal authz.Principal
	kn        authz.KNContext
}

func (s *Service) reader(ctx context.Context) (caller, error) {
	p, k, err := authz.RequireKN(ctx)
	if err != nil {
		return caller{}, err
	}
	if k.Role != authz.KNAdmin && (k.Department != party.DeptKeHoachTaiChinh || k.Role != authz.KNTinh && k.Role != authz.KNTW) {
		return caller{}, problem.Forbidden
	}
	if s.DB == nil {
		return caller{}, ErrDependency
	}
	return caller{principal: p, kn: k}, nil
}
func writer(c caller, p pstore.Period) error {
	if c.kn.Role != authz.KNTinh || c.kn.Department != party.DeptKeHoachTaiChinh || c.kn.ReadOnly() {
		return problem.Forbidden
	}
	if c.principal.ActorRef != p.ActorRef {
		return source.ErrNotOwner
	}
	return nil
}
func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}
func (s *Service) now() time.Time { return s.Clock.Now().UTC() }
func terminal(events []pstore.Event) string {
	for _, event := range events {
		if event.Action == pstore.ActionCancelled {
			return period.PeriodCancelled
		}
		if event.Action == pstore.ActionClosed {
			return period.PeriodClosed
		}
	}
	return ""
}
func loadPeriod(ctx context.Context, tx pgx.Tx, id uuid.UUID) (pstore.Period, string, error) {
	p, err := pstore.PeriodByID(ctx, tx, id)
	if err != nil {
		return pstore.Period{}, "", err
	}
	if p == nil {
		return pstore.Period{}, "", ErrNotFound
	}
	events, err := pstore.Events(ctx, tx, []uuid.UUID{id})
	if err != nil {
		return pstore.Period{}, "", err
	}
	return *p, terminal(events[id]), nil
}
func classify(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.ConstraintName {
		case "khtc_report_already_final", "khtc_report_final_cancel":
			return ErrFinal
		case "khtc_report_cancelled":
			return ErrCancelled
		case "khtc_report_export_draft":
			return ErrVersion
		case "khtc_report_final_export":
			return ErrProvisional
		}
	}
	return err
}

// HasFinal wires the existing period cancellation rule to the actual immutable final ledger.
func (s *Service) HasFinal(ctx context.Context, id string) (bool, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return false, problem.Unauthenticated
	}
	uid, err := uuid.Parse(id)
	if err != nil {
		return false, ErrNotFound
	}
	found := false
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error { f, e := store.GetFinal(ctx, tx, uid); found = f != nil; return e })
	return found, err
}

// GuardCancel serializes the entire cancellation workflow, including the cap/forms side effect.
// The cancellation event uses this transaction so its trigger can reacquire the lock safely.
func (s *Service) GuardCancel(ctx context.Context, id uuid.UUID, fn func(pgx.Tx) error) error {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return problem.Unauthenticated
	}
	return s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.Lock(ctx, tx, p.TenantID, id); err != nil {
			return err
		}
		return fn(tx)
	})
}

type Detail struct {
	PeriodID      uuid.UUID
	DraftID       *uuid.UUID
	Version       int64
	Content       domain.Draft
	Final         *store.Final
	Editable      bool
	CanRefresh    bool
	SourceChanged bool
	SourceError   string
	Terminal      string
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Detail, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return Detail{}, err
	}
	var p pstore.Period
	var term string
	var draft *store.Draft
	var final *store.Final
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		var e error
		if p, term, e = loadPeriod(ctx, tx, id); e != nil {
			return e
		}
		if draft, e = store.LatestDraft(ctx, tx, id); e != nil {
			return e
		}
		final, e = store.GetFinal(ctx, tx, id)
		return e
	})
	if err != nil {
		return Detail{}, err
	}
	out := Detail{PeriodID: id, Final: final, Terminal: term, Editable: term != period.PeriodCancelled && final == nil && writer(c, p) == nil}
	out.CanRefresh = out.Editable && term == ""
	if draft == nil {
		if term == period.PeriodCancelled {
			return Detail{}, ErrCancelled
		}
		data, defaults, e := s.capture(ctx, c, p)
		if e != nil {
			return Detail{}, e
		}
		out.Content, e = s.initialDraft(ctx, c, data, defaults)
		if e != nil {
			return Detail{}, e
		}
	} else {
		out.Content = draft.Content
		out.Version = draft.Version
		did := draft.ID
		out.DraftID = &did
	}
	changed, reason := s.changed(ctx, c, p, out.Content.Data)
	out.SourceChanged, out.SourceError = changed, reason
	return out, nil
}

// changed compares identities only. It never overwrites the saved draft or any historical export bytes.
func (s *Service) changed(ctx context.Context, c caller, p pstore.Period, data domain.Data) (bool, string) {
	current, _, err := s.capture(ctx, c, p)
	if err != nil {
		return false, "Không kiểm tra được nguồn hiện tại"
	}
	if current.InputHash != data.InputHash {
		return true, ""
	}
	if s.Sources != nil {
		state, e := s.Sources.Status(ctx, p.ID)
		if e != nil {
			return false, "Không kiểm tra được nguồn module"
		}
		if state.LiveError != "" {
			return false, state.LiveError
		}
		if state.SourceChanged() {
			return true, ""
		}
	}
	return false, ""
}

// initialDraft seeds M5 section II from the latest earlier, non-cancelled program report.
// Other sections use the current submitted sheets; saved edits are preserved by Refresh.
func (s *Service) initialDraft(ctx context.Context, c caller, data domain.Data, defaults map[string]string) (domain.Draft, error) {
	old := domain.Draft{Texts: map[string]string{}}
	if data.Template == "mau_5" {
		err := s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
			previous, e := store.PreviousProgramDraft(ctx, tx, data.Period.To)
			if e != nil {
				return e
			}
			if previous != nil {
				if value, ok := previous.Content.Texts["II"]; ok {
					old.Texts["II"] = value
				}
			}
			return nil
		})
		if err != nil {
			return domain.Draft{}, err
		}
	}
	return domain.Refresh(old, data, defaults), nil
}
