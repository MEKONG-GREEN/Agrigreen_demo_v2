package app

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"slices"
	"time"
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
func pinAppraisalQuarters(ctx context.Context, tx pgx.Tx, year int, node uuid.UUID, members []domain.AppraisalEntry) error {
	periods, err := store.AppraisalCurrent(ctx, tx, "period", nil)
	if err != nil {
		return err
	}
	type candidate struct {
		snapshot domain.AppraisalQuarterSnapshot
		known    time.Time
	}
	byStaff := map[uuid.UUID]map[int]candidate{}
	for _, row := range periods {
		p, err := appraisalPeriod(row)
		if err != nil {
			return err
		}
		if p.Year != year || p.NodeRef != node || p.Quarter < 1 || p.Quarter > 4 {
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
			staff := e.Member.StaffID
			if byStaff[staff] == nil {
				byStaff[staff] = map[int]candidate{}
			}
			if previous, ok := byStaff[staff][p.Quarter]; !ok || p.KnownAt.After(previous.known) {
				byStaff[staff][p.Quarter] = candidate{domain.AppraisalQuarterSnapshot{Quarter: p.Quarter, PeriodID: p.ID, EntryVersion: r.ID, Version: r.Seq, Status: e.Status, Rating: e.ClosedRating, Member: e.Member}, p.KnownAt}
			}
		}
	}
	for i := range members {
		refs := []domain.AppraisalQuarterSnapshot{}
		for _, candidate := range byStaff[members[i].Member.StaffID] {
			refs = append(refs, candidate.snapshot)
		}
		slices.SortFunc(refs, func(a, b domain.AppraisalQuarterSnapshot) int { return a.Quarter - b.Quarter })
		members[i].QuarterResults = refs
	}
	return nil
}
