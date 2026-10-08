package app

import (
	"context"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	port "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/season/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// RebuildResult đếm số dòng đã dựng lại.
type RebuildResult struct {
	Seasons, Logs, CoopAreas int
}

// RebuildProjections dựng lại toàn bộ bảng chiếu của tenant từ các sổ (R-22):
// season_current, farm_log_current, coop_season_area, yield_summary. Dùng chung
// hàm áp sự kiện với đường ghi trực tiếp (store.ApplySeasonEvent, applyReview).
// Không phát sự kiện. Admin API, vai platform_admin.
func (s *Service) RebuildProjections(ctx context.Context) (RebuildResult, error) {
	p, err := authz.Require(ctx, authz.RolePlatformAdmin)
	if err != nil {
		return RebuildResult{}, err
	}
	var res RebuildResult
	err = s.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		r, err := rebuild(ctx, tx, p.TenantID)
		res = r
		return err
	})
	return res, err
}

func rebuild(ctx context.Context, tx pgx.Tx, tid uuid.UUID) (RebuildResult, error) {
	var res RebuildResult
	if err := store.ClearProjections(ctx, tx); err != nil {
		return res, err
	}
	events, err := store.AllSeasonEvents(ctx, tx)
	if err != nil {
		return res, err
	}
	seasons := map[uuid.UUID]*store.Season{}
	var order []uuid.UUID
	for _, e := range events {
		cur := seasons[e.SeasonRef]
		next := store.ApplySeasonEvent(cur, e)
		if cur == nil {
			order = append(order, e.SeasonRef)
		}
		seasons[e.SeasonRef] = &next
	}
	harvests, err := store.AllHarvests(ctx, tx)
	if err != nil {
		return res, err
	}
	for _, h := range harvests { // khai báo sau cùng (bản ghi bù) là hiện hành
		if sn := seasons[h.SeasonRef]; sn != nil && sn.Status == port.SeasonCompleted {
			sn.HarvestID = h.ID
		}
	}
	for _, ref := range order {
		if err := store.SaveSeason(ctx, tx, tid, *seasons[ref]); err != nil {
			return res, err
		}
	}
	res.Seasons = len(order)

	logs, err := store.AllFarmLogs(ctx, tx)
	if err != nil {
		return res, err
	}
	reviews, err := store.AllReviews(ctx, tx)
	if err != nil {
		return res, err
	}
	type step struct {
		log    *store.FarmLog
		review *store.Review
		at     int64
	}
	steps := make([]step, 0, len(logs)+len(reviews))
	for i := range logs {
		steps = append(steps, step{log: &logs[i], at: logs[i].RecordedAt.UnixNano()})
	}
	for i := range reviews {
		steps = append(steps, step{review: &reviews[i], at: reviews[i].RecordedAt.UnixNano()})
	}
	sort.SliceStable(steps, func(i, j int) bool { return steps[i].at < steps[j].at })
	states := map[uuid.UUID]*store.LogState{}
	var logOrder []uuid.UUID
	for _, st := range steps {
		switch {
		case st.log != nil:
			l := *st.log
			ns := store.StateFromLog(l)
			states[l.ID] = &ns
			logOrder = append(logOrder, l.ID)
			if l.SupersedesID != nil && l.BecameCurrent {
				if prev := states[*l.SupersedesID]; prev != nil {
					prev.Status, prev.SupersededBy = port.LogSuperseded, l.ID
				}
			}
		case st.review != nil:
			if cur := states[st.review.LogVersionID]; cur != nil {
				applyReview(cur, *st.review)
			}
		}
	}
	for _, id := range logOrder {
		if err := store.SaveLogState(ctx, tx, tid, *states[id]); err != nil {
			return res, err
		}
	}
	res.Logs = len(logOrder)

	keys, err := store.CoopSeasonKeys(ctx, tx)
	if err != nil {
		return res, err
	}
	for _, k := range keys {
		if _, _, err := store.RefreshCoopArea(ctx, tx, tid, k.CooperativeRef, k.SeasonCode, k.SeasonYear); err != nil {
			return res, err
		}
	}
	res.CoopAreas = len(keys)
	for _, ref := range order {
		if err := store.RefreshYield(ctx, tx, ref); err != nil {
			return res, err
		}
	}
	return res, nil
}
