package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

type AppraisalHistoryPage struct {
	Items      []AppraisalItem `json:"items"`
	More       bool            `json:"more"`
	NextBefore int64           `json:"next_before"`
}

func (s *Service) AppraisalHistory(ctx context.Context, period, staff uuid.UUID, before int64) (AppraisalHistoryPage, error) {
	if before < 0 || before > 999_999_999_999_999_999 {
		return AppraisalHistoryPage{}, problem.Validation.WithField("before", "mốc lịch sử không hợp lệ")
	}
	c, err := s.appraisalCaller(ctx)
	if err != nil {
		return AppraisalHistoryPage{}, err
	}
	if err = s.appraisalRecords(); err != nil {
		return AppraisalHistoryPage{}, err
	}
	v := AppraisalHistoryPage{Items: []AppraisalItem{}}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		if _, _, e := loadAppraisalItem(ctx, tx, c, period, staff); e != nil {
			return e
		}
		rows, e := store.AppraisalEntryHistory(ctx, tx, appraisalEntryKey(period, staff), before)
		if e != nil {
			return e
		}
		v.More = len(rows) > 50
		for _, row := range rows[:min(len(rows), 50)] {
			item, e := appraisalItem(row, c)
			if e != nil {
				return e
			}
			v.Items = append(v.Items, item)
			v.NextBefore = row.Seq
		}
		return store.InsertAppraisalAccess(ctx, tx, c.tenant, store.Access{ID: ids.New(), Action: "view", StaffID: &staff, ItemCount: 1,
			Scope: string(c.scope), Actor: c.ref, Query: map[string]string{"appraisal_history": period.String()}}, s.Clock.Now())
	})
	return v, classify(err)
}

func (s *Service) AppraisalEntryVersion(ctx context.Context, period, staff, version uuid.UUID) (AppraisalItem, error) {
	c, err := s.appraisalCaller(ctx)
	if err != nil {
		return AppraisalItem{}, err
	}
	if err = s.appraisalRecords(); err != nil {
		return AppraisalItem{}, err
	}
	var v AppraisalItem
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		if _, _, e := loadAppraisalItem(ctx, tx, c, period, staff); e != nil {
			return e
		}
		row, ok, e := store.AppraisalVersion(ctx, tx, version)
		if e != nil {
			return e
		}
		if !ok || row.Kind != "entry" || row.Key != appraisalEntryKey(period, staff) {
			return port.ErrAppraisalNotFound
		}
		v, e = appraisalItem(row, c)
		if e != nil {
			return e
		}
		return store.InsertAppraisalAccess(ctx, tx, c.tenant, store.Access{ID: ids.New(), Action: "view", StaffID: &staff, ItemCount: 1,
			Scope: string(c.scope), Actor: c.ref, Query: map[string]string{"appraisal_version": version.String()}}, s.Clock.Now())
	})
	if err != nil {
		return AppraisalItem{}, classify(err)
	}
	named, err := s.appraisalNames(ctx, []AppraisalItem{v})
	if err != nil {
		return AppraisalItem{}, err
	}
	return named[0], nil
}
