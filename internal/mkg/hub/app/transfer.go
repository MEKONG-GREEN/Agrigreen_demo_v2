package app

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Lỗi chuyển kho (Q-MKG-10).
var (
	ErrTransferNotFound     = problem.New(404, "hub.transfer_not_found", "Không tìm thấy phiếu chuyển kho")
	ErrTransferSameFacility = problem.New(422, "hub.transfer_same_facility", "Cơ sở đích phải khác cơ sở đi")
	ErrTransferNotInTransit = problem.New(409, "hub.transfer_not_in_transit", "Phiếu chuyển không còn đang đi đường")
	ErrTransferWrongSite    = problem.New(422, "hub.transfer_wrong_facility", "Phiếu chuyển không đến cơ sở này")
	ErrTransferTicket       = problem.New(422, "hub.transfer_ticket_invalid", "Phiếu cân không phải phiếu HUB_IN của lô thuộc phiếu chuyển này")
)

// TransferInput là xuất chuyển từ cơ sở From sang cơ sở To.
type TransferInput struct {
	From, To    uuid.UUID
	Mass        mass.Kg
	Allocations []domain.Allocation // rỗng → theo rủi ro hub.risk.v2
	EventTime   time.Time
	DeviceID    *string
	IdemKey     string
}

// StartTransfer xuất hàng khỏi A (phiếu xuất destination_ref = B, dịch chuyển
// transfer_out) → phiếu chuyển ở trạng thái đang đi đường. Không bù trực tiếp
// được dịch chuyển transfer_out (hub.correction_unsupported): hủy = nhận lại ở A.
func (s *Service) StartTransfer(ctx context.Context, in TransferInput) (t store.Transfer, replayed bool, err error) {
	p, err := s.operator(ctx, in.IdemKey, &in.EventTime)
	if err != nil {
		return t, false, err
	}
	if in.From == in.To {
		return t, false, ErrTransferSameFacility
	}
	if in.Mass.Decimal().Sign() <= 0 {
		return t, false, problem.Validation.WithField("mass_kg", "phải > 0")
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		from, err := activeFacility(ctx, tx, in.From, false)
		if err != nil {
			return err
		}
		if _, err := activeFacility(ctx, tx, in.To, true); err != nil {
			return err
		}
		id := ids.New()
		dest := in.To
		res, err := s.dispatchTx(ctx, tx, p.TenantID, p.ActorRef, from.ID, DispatchInput{
			FacilityID: from.ID, Mass: in.Mass, DestinationRef: &dest, Allocations: in.Allocations, EventTime: in.EventTime,
			DeviceID: in.DeviceID, IdemKey: in.IdemKey,
		}, domain.MoveTransferOut, "transfer", &id)
		if err != nil {
			return err
		}
		var lots []store.TransferLot
		for _, a := range res.Dispatch.Allocations {
			lots = append(lots, store.TransferLot{LotID: a.LotID, MassKg: a.MassKg})
		}
		ev := store.TransferEvent{
			ID: ids.New(), TenantID: p.TenantID, TransferID: id, Kind: "dispatched", FromFacility: from.ID, ToFacility: in.To,
			DispatchID: res.Dispatch.ID, Mass: in.Mass, Lots: lots, EventTime: in.EventTime, ActorRef: p.ActorRef, IdemKey: in.IdemKey + ":transfer",
		}
		if err := insert(store.InsertTransferEvent(ctx, tx, &ev, in.DeviceID)); err != nil {
			return err
		}
		t = store.Transfer{ID: id, FromFacility: from.ID, ToFacility: in.To, DispatchID: res.Dispatch.ID, Status: "in_transit",
			Dispatched: in.Mass, SourceLots: lots, DispatchedAt: in.EventTime}
		return store.InsertTransfer(ctx, tx, p.TenantID, t)
	}, func(tx pgx.Tx) (bool, error) {
		id, err := store.TransferEventByIdem(ctx, tx, in.IdemKey+":transfer")
		if err != nil || id == nil {
			return false, err
		}
		cur, err := store.TransferByID(ctx, tx, *id, false)
		if cur != nil {
			t = *cur
		}
		return cur != nil, err
	})
	return t, replayed, err
}

// ReceiveTransferInput là nhận hàng chuyển ở cơ sở đích.
type ReceiveTransferInput struct {
	TransferID uuid.UUID
	FacilityID uuid.UUID // cơ sở nhận (phải là cơ sở đích)
	// Mass: khối lượng nhận (tay); để trống khi có WeighTicketRefs → Σ khối lượng phiếu.
	Mass            mass.Kg
	WeighTicketRefs []uuid.UUID
	EventTime       time.Time
	DeviceID        *string
	IdemKey         string
}

// ReceiveTransfer nhận ở đích: mỗi lô nguồn thành một lô mới ở B (origin
// transfer, nguồn kind = lot tỷ lệ 1 — giữ chuỗi nguồn gốc), khối lượng nhận chia
// theo khối lượng xuất của lô nguồn (dư lớn nhất, Σ đúng bằng khối lượng nhận);
// hao hụt đường = xuất − nhận (có dấu) ghi ở sổ chuyển.
func (s *Service) ReceiveTransfer(ctx context.Context, in ReceiveTransferInput) (t store.Transfer, replayed bool, err error) {
	p, err := s.operator(ctx, in.IdemKey, &in.EventTime)
	if err != nil {
		return t, false, err
	}
	if in.Mass.Decimal().IsNegative() || (len(in.WeighTicketRefs) == 0 && in.Mass.Decimal().Sign() == 0) {
		return t, false, problem.Validation.WithField("mass_kg", "phải > 0 (hoặc truyền weigh_ticket_refs)")
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		cur, err := store.TransferByID(ctx, tx, in.TransferID, true)
		if err != nil {
			return err
		}
		if cur == nil {
			return ErrTransferNotFound
		}
		if cur.Status != "in_transit" {
			return ErrTransferNotInTransit
		}
		if cur.ToFacility != in.FacilityID {
			return ErrTransferWrongSite
		}
		to, err := activeFacility(ctx, tx, cur.ToFacility, true)
		if err != nil {
			return err
		}
		received, tickets, err := transferTickets(ctx, tx, *cur, in.WeighTicketRefs)
		if err != nil {
			return err
		}
		if len(tickets) == 0 {
			received = in.Mass
		} else if in.Mass.Decimal().Sign() != 0 && in.Mass.Cmp(received) != 0 {
			return ErrWeighMassMismatch
		}
		if received.Decimal().Sign() <= 0 {
			return problem.Validation.WithField("mass_kg", "phải > 0")
		}
		parts := make([]mass.Kg, len(cur.SourceLots))
		for i, l := range cur.SourceLots {
			parts[i] = l.MassKg
		}
		split, err := domain.SplitProportional(received, parts)
		if err != nil {
			return mapErr(err)
		}
		ref := "transfer"
		var out []store.TransferLot
		for i, src := range cur.SourceLots {
			if split[i].Decimal().Sign() <= 0 {
				continue
			}
			origin, err := store.LotByID(ctx, tx, src.LotID)
			if err != nil {
				return err
			}
			if origin == nil {
				return ErrLotNotFound
			}
			l := domain.Lot{
				ID: ids.New(), TenantID: p.TenantID, FacilityID: to.ID, Product: origin.Product, Origin: domain.OriginTransfer,
				Initial: split[i], MoisturePct: origin.MoisturePct,
				Sources:   []domain.Source{{Kind: domain.SourceLot, Ref: src.LotID, Ratio: decimal.NewFromInt(1)}},
				EventTime: in.EventTime, ActorRef: p.ActorRef, IdemKey: in.IdemKey + ":lot:" + strconv.Itoa(i),
			}
			l.Code = domain.LotCode(l.ID)
			if err := insert(store.InsertLot(ctx, tx, &l, in.DeviceID)); err != nil {
				return err
			}
			m := domain.Movement{
				ID: ids.New(), TenantID: p.TenantID, FacilityID: to.ID, LotID: l.ID, Kind: domain.MoveTransferIn, Qty: split[i],
				BalanceAfter: split[i], RefType: &ref, RefID: &cur.ID, EventTime: in.EventTime, ActorRef: p.ActorRef,
				IdemKey: in.IdemKey + ":mv:" + strconv.Itoa(i),
			}
			if err := insert(store.InsertMovement(ctx, tx, &m, in.DeviceID)); err != nil {
				return err
			}
			if err := store.InsertBalance(ctx, tx, p.TenantID, l, split[i], m.Seq); err != nil {
				return err
			}
			sid := src.LotID
			out = append(out, store.TransferLot{SourceLotID: &sid, LotID: l.ID, MassKg: split[i]})
		}
		loss := cur.Dispatched.Sub(received)
		ev := store.TransferEvent{
			ID: ids.New(), TenantID: p.TenantID, TransferID: cur.ID, Kind: "received", FromFacility: cur.FromFacility,
			ToFacility: cur.ToFacility, DispatchID: cur.DispatchID, Mass: received, Loss: &loss, Lots: out, WeighTicketIDs: tickets,
			EventTime: in.EventTime, ActorRef: p.ActorRef, IdemKey: in.IdemKey,
		}
		if err := insert(store.InsertTransferEvent(ctx, tx, &ev, in.DeviceID)); err != nil {
			return err
		}
		for _, tk := range tickets {
			w, err := store.LotWeighByTicket(ctx, tx, tk)
			if err != nil {
				return err
			}
			w.Status = "applied"
			if err := store.UpsertLotWeigh(ctx, tx, p.TenantID, *w); err != nil {
				return err
			}
		}
		if err := store.MarkTransferReceived(ctx, tx, cur.ID, received, loss, out, in.EventTime); err != nil {
			return err
		}
		t, err = loadTransfer(ctx, tx, cur.ID)
		return err
	}, func(tx pgx.Tx) (bool, error) {
		id, err := store.TransferEventByIdem(ctx, tx, in.IdemKey)
		if err != nil || id == nil {
			return false, err
		}
		t, err = loadTransfer(ctx, tx, *id)
		return err == nil, err
	})
	return t, replayed, err
}

// transferTickets kiểm các phiếu HUB_IN dùng khi nhận: mỗi phiếu phải là
// "transfer_arrival" của một lô nguồn của phiếu chuyển; trả Σ khối lượng.
func transferTickets(ctx context.Context, tx pgx.Tx, t store.Transfer, refs []uuid.UUID) (mass.Kg, []uuid.UUID, error) {
	var sum mass.Kg
	src := map[uuid.UUID]bool{}
	for _, l := range t.SourceLots {
		src[l.LotID] = true
	}
	seen := map[uuid.UUID]bool{}
	for _, r := range refs {
		if seen[r] {
			return sum, nil, ErrTransferTicket
		}
		seen[r] = true
		w, err := store.LotWeighByTicket(ctx, tx, r)
		if err != nil {
			return sum, nil, err
		}
		if w == nil || w.Point != "HUB_IN" || w.Status != "transfer_arrival" || !src[w.LotID] || w.Measured == nil {
			return sum, nil, ErrTransferTicket
		}
		sum = sum.Add(*w.Measured)
	}
	return sum, refs, nil
}

func loadTransfer(ctx context.Context, tx pgx.Tx, id uuid.UUID) (store.Transfer, error) {
	cur, err := store.TransferByID(ctx, tx, id, false)
	if err != nil {
		return store.Transfer{}, err
	}
	if cur == nil {
		return store.Transfer{}, ErrTransferNotFound
	}
	return *cur, nil
}

// Transfer đọc phiếu chuyển.
func (s *Service) Transfer(ctx context.Context, id uuid.UUID) (store.Transfer, error) {
	var t store.Transfer
	err := s.read(ctx, func(tx pgx.Tx) error {
		var err error
		t, err = loadTransfer(ctx, tx, id)
		return err
	})
	return t, err
}
