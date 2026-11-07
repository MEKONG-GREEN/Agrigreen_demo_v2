package app

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// AppraisalProfileResult chỉ là kết luận trong hồ sơ FN18, không phải dữ liệu nâng bậc.
type AppraisalProfileResult struct {
	PeriodID     uuid.UUID `json:"period_id"`
	EntryVersion uuid.UUID `json:"entry_version"`
	Year         int       `json:"year"`
	Quarter      int       `json:"quarter"`
	Label        string    `json:"label"`
	Rating       string    `json:"rating"`
	RecordedAt   time.Time `json:"recorded_at"`
}

func (s *Service) AppraisalResults(ctx context.Context, staff uuid.UUID) ([]AppraisalProfileResult, error) {
	c, err := s.recCaller(ctx)
	if err != nil {
		return nil, err
	}
	if err = c.need(domain.PermView, false); err != nil {
		return nil, err
	}
	out := []AppraisalProfileResult{}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		file, e := loadFile(ctx, tx, staff)
		if e != nil {
			return e
		}
		if !c.on(domain.PermView, file.head, domain.Project(file.events, s.day(time.Time{}), nil)) {
			return port.ErrStaffNotFound
		}
		rows, e := store.AppraisalResults(ctx, tx, staff)
		if e != nil {
			return e
		}
		for _, row := range rows {
			entry, e := appraisalsReadObject[domain.AppraisalEntry](row)
			if e != nil {
				return e
			}
			if entry.Status != domain.AppraisalClosed || entry.ClosedRating == "" || entry.Member.ExcludedReason != "" {
				continue
			}
			if row.PeriodRef == nil {
				return port.ErrAppraisalNotFound
			}
			p, e := loadAppraisalPeriod(ctx, tx, *row.PeriodRef)
			if e != nil {
				return e
			}
			if c.unitRef != p.NodeRef && (c.group != domain.GroupAdmin || c.unitRef != uuid.Nil) {
				continue
			}
			out = append(out, AppraisalProfileResult{PeriodID: p.ID, EntryVersion: row.ID, Year: p.Year, Quarter: p.Quarter, Label: p.Label, Rating: entry.ClosedRating, RecordedAt: row.RecordedAt})
		}
		return store.InsertAccess(ctx, tx, c.tenant, store.Access{ID: ids.New(), Action: "view", StaffID: &staff, ItemCount: len(out), Scope: string(c.m[domain.PermView]), Actor: c.ref, Query: map[string]string{"appraisal_results": staff.String()}}, s.Clock.Now())
	})
	slices.SortFunc(out, func(a, b AppraisalProfileResult) int {
		if a.Year != b.Year {
			return b.Year - a.Year
		}
		if a.Quarter != b.Quarter {
			return b.Quarter - a.Quarter
		}
		return b.RecordedAt.Compare(a.RecordedAt)
	})
	return out, classify(err)
}
