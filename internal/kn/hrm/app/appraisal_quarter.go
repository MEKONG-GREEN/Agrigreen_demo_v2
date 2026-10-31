package app

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"slices"
)

// AppraisalQuarterResult là tham khảo; không tính hay thay kết luận năm của tập thể.
type AppraisalQuarterResult struct {
	Quarter      int       `json:"quarter"`
	PeriodID     uuid.UUID `json:"period_id"`
	EntryVersion uuid.UUID `json:"entry_version"`
	Version      int64     `json:"version"`
	Status       string    `json:"status"`
	Rating       string    `json:"rating"`
}

// Chỉ SQL trong TX HRM. Chụp tối đa bốn đợt, không tải câu trả lời/tên kho định danh.
func pinAppraisalQuarters(ctx context.Context, tx pgx.Tx, year int, members []domain.AppraisalEntry) error {
	periods, err := store.AppraisalCurrent(ctx, tx, "period", nil)
	if err != nil {
		return err
	}
	byStaff := map[uuid.UUID][]domain.AppraisalQuarterSnapshot{}
	for _, row := range periods {
		p, err := appraisalPeriod(row)
		if err != nil {
			return err
		}
		if p.Year != year || p.Quarter < 1 || p.Quarter > 4 {
			continue
		}
		rows, err := store.AppraisalEntrySummaries(ctx, tx, p.ID, false)
		if err != nil {
			return err
		}
		for _, r := range rows {
			e, err := appraisalsReadObject[domain.AppraisalEntry](r)
			if err != nil {
				return err
			}
			if e.Member.ExcludedReason != "" {
				continue
			}
			byStaff[e.Member.StaffID] = append(byStaff[e.Member.StaffID], domain.AppraisalQuarterSnapshot{Quarter: p.Quarter, PeriodID: p.ID, EntryVersion: r.ID, Version: r.Seq, Status: e.Status, Rating: e.ClosedRating, Member: e.Member})
		}
	}
	for i := range members {
		refs := byStaff[members[i].Member.StaffID]
		slices.SortFunc(refs, func(a, b domain.AppraisalQuarterSnapshot) int { return a.Quarter - b.Quarter })
		members[i].QuarterResults = refs
	}
	return nil
}
