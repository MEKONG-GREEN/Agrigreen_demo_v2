package app

// Ngưỡng cảnh báo giá cao / thấp bất thường (W11.74 — BRD Báo giá v1.3 FN-AD-04.06). Tham số có phiên bản: mỗi lần
// sửa là một dòng mới của pf_price.alert_threshold; áp dụng từ lần gửi kế tiếp, không tính lại cảnh báo đã sinh (BR-03).

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

var _ port.Thresholds = (*Service)(nil)

// ThresholdView là màn "Ngưỡng cảnh báo".
type ThresholdView struct {
	Profile    string
	Set        domain.ThresholdSet
	DefaultSet bool // ngưỡng mặc định đã ghi ở sổ (false = mặc định của Profile)
	Rows       []store.ThresholdRow
	History    []store.ThresholdRow
	CanManage  bool
}

const thresholdHistoryLimit = 200

func (s *Service) thresholdSet(ctx context.Context, tx pgx.Tx, prof port.Profile) (domain.ThresholdSet, []store.ThresholdRow, bool, error) {
	d, err := scheduleDefaults(prof)
	if err != nil {
		return domain.ThresholdSet{}, nil, false, err
	}
	rows, err := store.Thresholds(ctx, tx, prof.Name)
	if err != nil {
		return domain.ThresholdSet{}, nil, false, err
	}
	set := domain.ThresholdSet{Default: d.threshold, Groups: map[string]domain.Threshold{}, Items: map[string]domain.Threshold{}}
	stored := false
	for _, r := range rows {
		switch r.Threshold.Scope {
		case domain.ThresholdScopeDefault:
			set.Default, stored = r.Threshold, true
		case domain.ThresholdScopeGroup:
			set.Groups[r.Threshold.Code] = r.Threshold
		case domain.ThresholdScopeItem:
			set.Items[r.Threshold.Code] = r.Threshold
		}
	}
	return set, rows, stored, nil
}

// ThresholdsView đọc ngưỡng đang hiệu lực + lịch sử thay đổi (mọi cấp của Profile đọc được).
func (s *Service) ThresholdsView(ctx context.Context, profile string) (ThresholdView, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return ThresholdView{}, err
	}
	v := ThresholdView{Profile: prof.Name, CanManage: c.manage}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		var e error
		if v.Set, v.Rows, v.DefaultSet, e = s.thresholdSet(ctx, tx, prof); e != nil {
			return e
		}
		v.History, e = store.ThresholdHistory(ctx, tx, prof.Name, thresholdHistoryLimit)
		return e
	})
	return v, err
}

// SaveThreshold ghi một phiên bản ngưỡng (default kèm tham số chung; group phải là nhóm hàng đang hiệu lực; item phải
// có trong danh mục mặt hàng W11.73). Vai ManageBy.
func (s *Service) SaveThreshold(ctx context.Context, profile, idem string, t domain.Threshold) (domain.Threshold, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return t, err
	}
	if !c.manage {
		return t, problem.Forbidden.WithDetail("chỉ vai quản trị nghiệp vụ báo giá cấu hình được ngưỡng")
	}
	if err := domain.ValidateThreshold(t); err != nil {
		return t, calendarMapErr(err)
	}
	now := s.Clock.Now()
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		if t.Scope == domain.ThresholdScopeItem { // W11.73: mặt hàng phải có trong danh mục (mọi trạng thái)
			ok, err := store.ItemExists(ctx, tx, t.Code)
			if err != nil {
				return err
			}
			if !ok {
				return port.ErrItemNotFound.WithDetail("mặt hàng " + t.Code)
			}
		}
		if t.Scope == domain.ThresholdScopeGroup {
			gs, err := groups(ctx, tx, domain.BusinessDay(now))
			if err != nil {
				return err
			}
			found := false
			for _, g := range gs {
				found = found || g.Code == t.Code
			}
			if !found {
				return problem.Validation.WithDetail("nhóm hàng " + t.Code + " không có hoặc đã ngưng")
			}
		}
		var e error
		t.Version, e = store.InsertThreshold(ctx, tx, store.CalMeta{ID: ids.New(), Tenant: c.tenant, EventTime: now, Actor: c.ref,
			Idem: calendarIdem(idem, "threshold")}, prof.Name, t, "active")
		return e
	})
	if err != nil {
		return t, calendarClassify(err)
	}
	return t, nil
}

// RemoveThreshold xóa ngưỡng nhóm/mặt hàng (phiên bản 'retired'); ngưỡng mặc định không xóa được (AC-04).
func (s *Service) RemoveThreshold(ctx context.Context, profile, idem, scope, code string) error {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return err
	}
	if !c.manage {
		return problem.Forbidden.WithDetail("chỉ vai quản trị nghiệp vụ báo giá cấu hình được ngưỡng")
	}
	if scope == domain.ThresholdScopeDefault {
		return port.ErrDefaultThreshold
	}
	now := s.Clock.Now()
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		set, _, _, err := s.thresholdSet(ctx, tx, prof)
		if err != nil {
			return err
		}
		var t domain.Threshold
		var ok bool
		switch scope {
		case domain.ThresholdScopeGroup:
			t, ok = set.Groups[code]
		case domain.ThresholdScopeItem:
			t, ok = set.Items[code]
		}
		if !ok {
			return port.ErrThresholdNotFound
		}
		_, err = store.InsertThreshold(ctx, tx, store.CalMeta{ID: ids.New(), Tenant: c.tenant, EventTime: now, Actor: c.ref,
			Idem: calendarIdem(idem, "threshold-retire")}, prof.Name, t, "retired")
		return err
	})
	return calendarClassify(err)
}

// Threshold (port.Thresholds) trả ngưỡng áp cho mặt hàng item thuộc nhóm group cho tenant của ctx.
func (s *Service) Threshold(ctx context.Context, profile, group, item string) (decimal.Decimal, string, error) {
	prof, ok := s.calendarProfile(profile)
	if !ok {
		return decimal.Zero, "", port.ErrProfileNotFound
	}
	var t domain.Threshold
	err := s.ctxTx(ctx, func(tx pgx.Tx) error {
		set, _, _, err := s.thresholdSet(ctx, tx, prof)
		t = set.For(group, item)
		return err
	})
	return t.Pct, t.Scope, err
}

// AlertParams (port.Thresholds) trả N, ngưỡng đi ngang, ngưỡng phiếu điều tra cho tenant của ctx.
func (s *Service) AlertParams(ctx context.Context, profile string) (int, decimal.Decimal, decimal.Decimal, error) {
	prof, ok := s.calendarProfile(profile)
	if !ok {
		return 0, decimal.Zero, decimal.Zero, port.ErrProfileNotFound
	}
	var p domain.AlertParams
	err := s.ctxTx(ctx, func(tx pgx.Tx) error {
		set, _, _, err := s.thresholdSet(ctx, tx, prof)
		if err == nil && set.Default.Params != nil {
			p = *set.Default.Params
		}
		return err
	})
	return p.LookbackDays, p.FlatPct, p.SurveyPct, err
}
