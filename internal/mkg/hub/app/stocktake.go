package app

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Lỗi kiểm kê (Q-MKG-06).
var (
	ErrStocktakeNotFound   = problem.New(404, "hub.stocktake_not_found", "Không tìm thấy phiên kiểm kê")
	ErrStocktakePending    = problem.New(409, "hub.stocktake_pending_exists", "Cơ sở đang có phiên kiểm kê chờ duyệt — duyệt/từ chối, hoặc đếm lại với supersedes_id")
	ErrStocktakeNotPending = problem.New(409, "hub.stocktake_not_pending", "Phiên kiểm kê không còn chờ duyệt")
	ErrStocktakeStale      = problem.New(409, "hub.stocktake_stale", "Tồn theo sổ đã đổi kể từ lúc đếm — đếm lại")
	ErrStocktakeLines      = problem.New(422, "hub.stocktake_lines_invalid", "Dòng kiểm kê phải phủ đủ mọi dạng hàng đang có tồn, không lặp")
)

// StocktakeInput là một lần đếm kiểm kê của cơ sở.
type StocktakeInput struct {
	FacilityID uuid.UUID
	Lines      []domain.CountLine
	Note       string
	// Supersedes + Reason: đếm lại thay phiên đang chờ duyệt.
	Supersedes *uuid.UUID
	Reason     string
	EventTime  time.Time
	DeviceID   *string
	IdemKey    string
}

// SubmitStocktake ghi một lần đếm (hub_operator): so với tồn theo lô LÚC ĐẾM, chờ
// hub_approver khác người đếm duyệt. Chưa có dịch chuyển nào cho tới khi duyệt.
func (s *Service) SubmitStocktake(ctx context.Context, in StocktakeInput) (st store.Stocktake, replayed bool, err error) {
	p, err := s.operator(ctx, in.IdemKey, &in.EventTime)
	if err != nil {
		return st, false, err
	}
	reason := strings.TrimSpace(in.Reason)
	if (in.Supersedes != nil) != (reason != "") || (reason != "" && len([]rune(reason)) < 10) {
		return st, false, problem.Validation.WithField("reason", "đếm lại cần supersedes_id + lý do ≥ 10 ký tự")
	}
	note := strings.TrimSpace(in.Note)
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		f, err := activeFacility(ctx, tx, in.FacilityID, false)
		if err != nil {
			return err
		}
		if err := store.LockStream(ctx, tx, "stocktake:"+f.ID.String()); err != nil {
			return err
		}
		pend, err := store.PendingStocktake(ctx, tx, f.ID)
		if err != nil {
			return err
		}
		switch {
		case pend != nil && (in.Supersedes == nil || *in.Supersedes != pend.ID):
			return ErrStocktakePending
		case pend == nil && in.Supersedes != nil:
			return ErrStocktakeNotPending
		}
		book, err := store.BookByProduct(ctx, tx, f.ID)
		if err != nil {
			return err
		}
		lines, total, err := domain.BuildStocktake(book, in.Lines)
		if err != nil {
			return mapErr(err)
		}
		if pend != nil {
			if err := store.SetStocktakeStatus(ctx, tx, pend.ID, "superseded", nil, nil); err != nil {
				return err
			}
		}
		st = store.Stocktake{
			ID: ids.New(), TenantID: p.TenantID, FacilityID: f.ID, Lines: lines, Book: total.Book, Counted: total.Counted,
			Variance: total.Variance, EventTime: in.EventTime, SupersedesID: in.Supersedes, ActorRef: p.ActorRef, IdemKey: in.IdemKey,
		}
		if note != "" {
			st.Note = &note
		}
		if reason != "" {
			st.Reason = &reason
		}
		return insert(store.InsertStocktake(ctx, tx, &st, in.DeviceID))
	}, func(tx pgx.Tx) (bool, error) {
		prev, err := store.StocktakeByIdem(ctx, tx, in.IdemKey)
		if prev != nil {
			st = *prev
		}
		return prev != nil, err
	})
	return st, replayed, err
}

// DecideStocktakeInput là quyết định với một phiên kiểm kê.
type DecideStocktakeInput struct {
	StocktakeID uuid.UUID
	Decision    domain.Decision
	Reason      string
	IdemKey     string
}

// StocktakeResult là phiên kiểm kê kèm dịch chuyển điều chỉnh đã ghi.
type StocktakeResult struct {
	Stocktake   store.Stocktake
	Adjustments []store.StocktakeAdjustment
}

// DecideStocktake duyệt / từ chối (hub_approver ≠ người đếm — R-56 ở app + CHECK DB).
// Duyệt: tồn theo sổ phải còn đúng như lúc đếm (không thì đếm lại); mỗi dạng hàng
// thừa → lô điều chỉnh (origin stocktake, nguồn kind = stocktake tỷ lệ 1); thiếu →
// trừ các lô theo thứ tự rủi ro hub.risk.v2. Mọi điều chỉnh là dịch chuyển
// adjustment ref = phiên kiểm kê.
func (s *Service) DecideStocktake(ctx context.Context, in DecideStocktakeInput) (res StocktakeResult, replayed bool, err error) {
	p, err := authz.Require(ctx, RoleHubApprover)
	if err != nil {
		return res, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return res, false, err
	}
	if in.Decision != domain.DecisionApproved && in.Decision != domain.DecisionRejected {
		return res, false, problem.Validation.WithField("decision", "approved hoặc rejected")
	}
	if err := domain.CheckDecisionReason(in.Decision, in.Reason); err != nil {
		return res, false, mapErr(err)
	}
	now := s.Clock.Now()
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		st, err := store.StocktakeByID(ctx, tx, in.StocktakeID)
		if err != nil {
			return err
		}
		if st == nil {
			return ErrStocktakeNotFound
		}
		if err := store.LockStream(ctx, tx, "stocktake:"+st.FacilityID.String()); err != nil {
			return err
		}
		if st, err = store.StocktakeByID(ctx, tx, in.StocktakeID); err != nil {
			return err
		}
		if st.Status != "pending" {
			return ErrStocktakeNotPending
		}
		if st.ActorRef == p.ActorRef {
			return problem.SelfApproval.WithCause(authz.ErrSelfApproval)
		}
		d := store.StocktakeDecision{
			ID: ids.New(), TenantID: p.TenantID, FacilityID: st.FacilityID, StocktakeID: st.ID, Decision: string(in.Decision),
			ProposerRef: st.ActorRef, EventTime: now, ActorRef: p.ActorRef, IdemKey: in.IdemKey,
		}
		if r := strings.TrimSpace(in.Reason); r != "" {
			d.Reason = &r
		}
		if in.Decision == domain.DecisionApproved {
			if d.Adjustments, err = s.applyStocktake(ctx, tx, p.TenantID, p.ActorRef, *st, now, in.IdemKey); err != nil {
				return err
			}
		}
		if err := insert(store.InsertStocktakeDecision(ctx, tx, &d)); err != nil {
			return err
		}
		if err := store.SetStocktakeStatus(ctx, tx, st.ID, string(in.Decision), &p.ActorRef, &now); err != nil {
			return err
		}
		st.Status, st.DecidedBy, st.DecidedAt = string(in.Decision), &p.ActorRef, &now
		res = StocktakeResult{Stocktake: *st, Adjustments: d.Adjustments}
		return nil
	}, func(tx pgx.Tx) (bool, error) {
		d, err := store.StocktakeDecisionByIdem(ctx, tx, in.IdemKey)
		if err != nil || d == nil {
			return false, err
		}
		st, err := store.StocktakeByID(ctx, tx, d.StocktakeID)
		if err != nil || st == nil {
			return false, err
		}
		res = StocktakeResult{Stocktake: *st, Adjustments: d.Adjustments}
		return true, nil
	})
	return res, replayed, err
}

func (s *Service) applyStocktake(ctx context.Context, tx pgx.Tx, tid, actor uuid.UUID, st store.Stocktake, now time.Time,
	idem string) ([]store.StocktakeAdjustment, error) {
	lots, err := store.Balances(ctx, tx, st.FacilityID, true) // khóa lô theo thứ tự lot_id
	if err != nil {
		return nil, err
	}
	book, err := store.BookByProduct(ctx, tx, st.FacilityID)
	if err != nil {
		return nil, err
	}
	for _, l := range st.Lines {
		if book[l.Product].Cmp(l.Book) != 0 {
			return nil, ErrStocktakeStale
		}
	}
	ranked, _, err := s.rank(ctx, tx, tid, st.FacilityID, lots, now)
	if err != nil {
		return nil, err
	}
	ref := "stocktake"
	var out []store.StocktakeAdjustment
	for i, l := range st.Lines {
		switch l.Variance.Decimal().Sign() {
		case 1: // thừa → lô điều chỉnh
			lot := domain.Lot{
				ID: ids.New(), TenantID: tid, FacilityID: st.FacilityID, Product: l.Product, Origin: domain.OriginStocktake,
				Initial: l.Variance, Sources: []domain.Source{{Kind: domain.SourceStocktake, Ref: st.ID, Ratio: decimal.NewFromInt(1)}},
				EventTime: now, ActorRef: actor, IdemKey: idem + ":lot:" + strconv.Itoa(i),
			}
			lot.Code = domain.LotCode(lot.ID)
			if err := insert(store.InsertLot(ctx, tx, &lot, nil)); err != nil {
				return nil, err
			}
			m := domain.Movement{
				ID: ids.New(), TenantID: tid, FacilityID: st.FacilityID, LotID: lot.ID, Kind: domain.MoveAdjustment, Qty: l.Variance,
				BalanceAfter: l.Variance, RefType: &ref, RefID: &st.ID, EventTime: now, ActorRef: actor, IdemKey: idem + ":adj:" + lot.ID.String(),
			}
			if err := insert(store.InsertMovement(ctx, tx, &m, nil)); err != nil {
				return nil, err
			}
			if err := store.InsertBalance(ctx, tx, tid, lot, l.Variance, m.Seq); err != nil {
				return nil, err
			}
			out = append(out, store.StocktakeAdjustment{LotID: lot.ID, MovementID: m.ID, QtyKg: l.Variance})
		case -1: // thiếu → trừ theo rủi ro
			var of []domain.Ranked
			for _, r := range ranked {
				if r.Product == l.Product {
					of = append(of, r)
				}
			}
			allocs, err := domain.Allocate(of, mass.Kg{}.Sub(l.Variance))
			if err != nil {
				return nil, ErrStocktakeStale
			}
			for _, a := range allocs {
				b, err := store.BalanceForUpdate(ctx, tx, a.LotID)
				if err != nil {
					return nil, err
				}
				qty := mass.Kg{}.Sub(a.MassKg)
				after, err := domain.Apply(b.Remaining, qty)
				if err != nil {
					return nil, ErrStocktakeStale
				}
				m := domain.Movement{
					ID: ids.New(), TenantID: tid, FacilityID: st.FacilityID, LotID: a.LotID, Kind: domain.MoveAdjustment, Qty: qty,
					BalanceAfter: after, RefType: &ref, RefID: &st.ID, EventTime: now, ActorRef: actor, IdemKey: idem + ":adj:" + a.LotID.String(),
				}
				if err := insert(store.InsertMovement(ctx, tx, &m, nil)); err != nil {
					return nil, err
				}
				if err := store.SetBalance(ctx, tx, a.LotID, after, m.Seq); err != nil {
					return nil, err
				}
				out = append(out, store.StocktakeAdjustment{LotID: a.LotID, MovementID: m.ID, QtyKg: qty})
			}
		}
	}
	return out, nil
}

// Stocktakes liệt kê phiên kiểm kê của cơ sở (mới trước).
func (s *Service) Stocktakes(ctx context.Context, facility uuid.UUID) ([]store.Stocktake, error) {
	var out []store.Stocktake
	err := s.read(ctx, func(tx pgx.Tx) error {
		if _, err := activeFacility(ctx, tx, facility, false); err != nil {
			return err
		}
		var err error
		out, err = store.Stocktakes(ctx, tx, facility, 50)
		return err
	})
	return out, err
}

// Stocktake đọc một phiên kiểm kê kèm điều chỉnh (nếu đã duyệt).
func (s *Service) Stocktake(ctx context.Context, id uuid.UUID) (StocktakeResult, error) {
	var res StocktakeResult
	err := s.read(ctx, func(tx pgx.Tx) error {
		st, err := store.StocktakeByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if st == nil {
			return ErrStocktakeNotFound
		}
		res.Stocktake = *st
		d, err := store.StocktakeDecisionOf(ctx, tx, id)
		if d != nil {
			res.Adjustments = d.Adjustments
		}
		return err
	})
	return res, err
}
