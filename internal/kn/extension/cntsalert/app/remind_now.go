package app

// BF-CN-PUBLISH (#108 — CN-07 "màn theo dõi … kèm nút nhắc"): Cấp Tỉnh bấm Nhắc trên một kỳ biểu kỳ CNTS. Nhắc các đơn
// vị chưa gửi (hoặc các đơn vị được chọn trong số đó) theo cấu hình nhắc của biểu (kênh, nội dung mẫu); người nhận là
// cán bộ xã của xã như job nhắc. Mỗi đơn vị tối đa một lượt nhắc tay mỗi giờ (sổ cnts_reminder_sent, lượt
// "manual:<YYYY-MM-DDTHH>").

import (
	"context"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsalert/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsalert/store"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/txdeps"
)

// RemindResult là kết quả một lần bấm Nhắc.
type RemindResult struct {
	Reminded int      // số đơn vị được nhắc lần này
	Skipped  []string // đơn vị đã được nhắc tay trong giờ này
}

func pendingUnit(status string, versions int64) bool {
	return versions == 0 && (status == "not_submitted" || status == "overdue") || status == "returned"
}

// RemindPeriod nhắc các đơn vị chưa gửi (hoặc được Cấp Tỉnh trả lại yêu cầu điều chỉnh) của kỳ native period. units
// rỗng = mọi đơn vị cần nhắc trong phạm vi người gọi.
func (s *Service) RemindPeriod(ctx context.Context, period uuid.UUID, units []string) (RemindResult, error) {
	c, err := s.manager(ctx)
	if err != nil {
		return RemindResult{}, err
	}
	if s.Control == nil {
		return RemindResult{}, ErrDependency.WithDetail("chưa nối tiến độ kỳ native (cap/forms)")
	}
	p, err := s.Control.Progress(ctx, ProfileCNTS, period) // cap/forms kiểm người gọi thấy kỳ, lọc đơn vị theo phạm vi
	if err != nil {
		return RemindResult{}, err
	}
	if !domain.IsForm(p.TemplateCode) {
		return RemindResult{}, problem.Validation.WithField("period_id", "không phải kỳ biểu kỳ CNTS")
	}
	if !accepting(p.Status) {
		return RemindResult{}, problem.Conflict.WithDetail("kỳ đã chốt hoặc đã hủy — không nhắc")
	}
	for _, u := range units {
		if !slices.ContainsFunc(p.Units, func(x formsport.UnitProgress) bool { return x.Unit == u }) {
			return RemindResult{}, problem.Validation.WithField("units", u+": không thuộc kỳ hoặc ngoài phạm vi")
		}
	}
	now := s.now()
	loc := clock.Zone()
	slot := "manual:" + now.In(loc).Format("2006-01-02T15")
	tid := c.p.TenantID
	var out RemindResult
	ctx = txdeps.With(ctx) // tỉnh của xã, cán bộ nhận nhắc đọc ngoài giao dịch (P0-POOL-KN-2)
	err = s.tx(ctx, tid, func(tx pgx.Tx) error {
		out = RemindResult{Skipped: []string{}}
		cs, err := s.configs(ctx, tx)
		if err != nil {
			return err
		}
		cfg, _ := reminderOf(cs, p.TemplateCode)
		for _, u := range p.Units {
			if len(units) > 0 && !slices.Contains(units, u.Unit) {
				continue
			}
			if !pendingUnit(u.Status, u.Versions) {
				continue
			}
			commune := u.Parent
			if commune == "" {
				commune = u.Unit
			}
			rs := &resolver{s: s, tid: tid, province: s.provinceOf(ctx, commune, now)}
			recips := rs.resolve(ctx, []string{domain.RecipCommune}, commune, uuid.Nil)
			ok, err := store.MarkReminder(ctx, tx, tid, p.ID, u.Unit, slot, p.TemplateCode, len(recips), now)
			if err != nil {
				return err
			}
			if !ok {
				out.Skipped = append(out.Skipped, u.Unit)
				continue
			}
			if u.Parent == "" {
				u.Parent = commune
			}
			if err := emitReminded(ctx, tx, tid, p, u, slot, cfg, recips, now, loc); err != nil {
				return err
			}
			out.Reminded++
		}
		return nil
	})
	if err != nil {
		return RemindResult{}, err
	}
	if out.Reminded == 0 && len(out.Skipped) == 0 {
		return out, problem.Validation.WithDetail("không có đơn vị cần nhắc (mọi đơn vị đã gửi)")
	}
	return out, nil
}
