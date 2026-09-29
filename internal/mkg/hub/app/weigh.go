package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Lỗi phiếu cân (Q-MKG-04).
var (
	ErrWeighTicketUnknown  = problem.New(409, "hub.weigh_ticket_unknown", "Hub chưa nhận phiếu cân HUB_IN này — thử lại sau khi phiếu về")
	ErrWeighTicketUsed     = problem.New(409, "hub.weigh_ticket_used", "Phiếu cân đã dùng cho một lần nhận khác")
	ErrWeighTicketLot      = problem.New(422, "hub.weigh_ticket_lot_mismatch", "id lô khác lot_ref của phiếu cân")
	ErrWeighMassMismatch   = problem.New(422, "hub.weigh_mass_mismatch", "mass_kg khác khối lượng phiếu cân — để trống để lấy theo phiếu")
	ErrWeighTicketRequired = problem.New(422, "hub.weigh_ticket_required", "Hub có cân: cần weigh_ticket_ref, hoặc manual_reason khi cân hỏng")
)

// receiptBasis xác định căn cứ khối lượng của một lần nhận; trả phiếu chờ (nếu
// nhận bằng phiếu) và điền Mass / MoisturePct từ phiếu.
func (s *Service) receiptBasis(ctx context.Context, tx pgx.Tx, in *ReceiveInput, manualReason string) (*store.LotWeigh, error) {
	if in.WeighTicketRef == nil {
		wp, _, err := activeWeigh(ctx, tx)
		if err != nil {
			return nil, err
		}
		if wp.TicketRequired && manualReason == "" {
			return nil, ErrWeighTicketRequired
		}
		return nil, nil
	}
	w, err := store.LotWeighByTicket(ctx, tx, *in.WeighTicketRef)
	if err != nil {
		return nil, err
	}
	if w == nil || w.Point != "HUB_IN" || w.Measured == nil {
		return nil, ErrWeighTicketUnknown
	}
	if w.Status != "pending" {
		return nil, ErrWeighTicketUsed
	}
	if in.LotID != nil && *in.LotID != w.LotID {
		return nil, ErrWeighTicketLot
	}
	if in.Mass.Decimal().Sign() != 0 && in.Mass.Cmp(*w.Measured) != 0 {
		return nil, ErrWeighMassMismatch
	}
	if err := store.LockStream(ctx, tx, domain.StreamKey(w.LotID)); err != nil {
		return nil, err
	}
	in.Mass = *w.Measured
	if in.MoisturePct == nil && w.MoisturePct != nil {
		m, err := domain.ParsePct("moisture_pct", *w.MoisturePct)
		if err != nil {
			return nil, mapErr(err)
		}
		in.MoisturePct = m
	}
	return w, nil
}

// recordReceiptBasis ghi căn cứ khối lượng nhận vào sổ weigh_link + bảng chiếu.
func (s *Service) recordReceiptBasis(ctx context.Context, tx pgx.Tx, tid, actor uuid.UUID, lot domain.Lot, w *store.LotWeigh, reason string) error {
	if w == nil && reason == "" {
		return nil // nhập tay khi hub chưa bắt buộc phiếu: phiếu về sau đối chiếu theo dung sai
	}
	book := lot.Initial
	link := store.WeighLink{
		ID: ids.New(), TenantID: tid, LotRef: lot.ID, Point: "HUB_IN", Book: &book, EventTime: lot.EventTime, ActorRef: actor,
	}
	next := store.LotWeigh{LotID: lot.ID, Point: "HUB_IN", Book: &book}
	if w != nil {
		link.TicketID, link.Measured, link.Outcome, link.IdemKey = w.TicketID, w.Measured, "receipt", "receipt:"+lot.ID.String()
		next.TicketID, next.SupersedesTicketID, next.Measured, next.MoisturePct = w.TicketID, w.SupersedesTicketID, w.Measured, w.MoisturePct
		next.Status, next.Basis = "applied", domain.BasisTicket
	} else {
		link.Outcome, link.Reason, link.IdemKey = "manual_receipt", &reason, "manual:"+lot.ID.String()
		next.Status, next.Basis = "manual", domain.BasisManual
	}
	if err := insert(store.InsertWeighLink(ctx, tx, &link)); err != nil {
		return err
	}
	next.LastLinkID = link.ID
	return store.UpsertLotWeigh(ctx, tx, tid, next)
}

// Measurements là các liên kết phiếu cân, hàng dự kiến về và hao hụt đã quy kết của một lô.
type Measurements struct {
	LotRef       uuid.UUID
	Links        []store.WeighLink
	HubIn        *store.LotWeigh
	HubOut       *store.LotWeigh
	Expected     map[string]int // cuộn đang hiệu lực theo công đoạn
	ExpectedKg   *mass.Kg       // ước tính = cuộn công đoạn muộn nhất × kg/cuộn mặc định (tham số hub.weigh.v1)
	WeighVersion string
	Loss         *store.LossCurrent
}

// LotMeasurements đọc thông tin đo lường của một lô (cả khi lô chưa về hub).
func (s *Service) LotMeasurements(ctx context.Context, lot uuid.UUID) (Measurements, error) {
	out := Measurements{LotRef: lot}
	err := s.read(ctx, func(tx pgx.Tx) error {
		var err error
		if out.Links, err = store.WeighLinksOfLot(ctx, tx, lot); err != nil {
			return err
		}
		if out.HubIn, err = store.LotWeighOf(ctx, tx, lot, "HUB_IN"); err != nil {
			return err
		}
		if out.HubOut, err = store.LotWeighOf(ctx, tx, lot, "HUB_OUT"); err != nil {
			return err
		}
		if out.Expected, err = store.ExpectedRolls(ctx, tx, lot); err != nil {
			return err
		}
		wp, pinned, err := activeWeigh(ctx, tx)
		if err != nil {
			return err
		}
		out.WeighVersion = pinned
		for _, st := range []string{"boat_load", "gather", "bale"} { // công đoạn muộn nhất có số
			if n, ok := out.Expected[st]; ok {
				k, err := mass.ParseKg(wp.KgPerRollDefault.Decimal().Mul(decimal.NewFromInt(int64(n))).StringFixed(mass.Scale))
				if err != nil {
					return err
				}
				out.ExpectedKg = &k
				break
			}
		}
		if out.Loss, err = store.LossOfLot(ctx, tx, lot); err != nil {
			return err
		}
		l, err := store.LotByID(ctx, tx, lot)
		if err != nil {
			return err
		}
		if l == nil && len(out.Links) == 0 && len(out.Expected) == 0 && out.Loss == nil {
			return ErrLotNotFound
		}
		return nil
	})
	return out, err
}

// FacilityLoss tổng hợp hao hụt đã quy kết (đối soát hiện hành của mỗi lô) cho một cơ sở.
func (s *Service) FacilityLoss(ctx context.Context, facility uuid.UUID) (store.LossSummary, error) {
	var out store.LossSummary
	err := s.read(ctx, func(tx pgx.Tx) error {
		if _, err := activeFacility(ctx, tx, facility, false); err != nil {
			return err
		}
		var err error
		out, err = store.FacilityLoss(ctx, tx, facility)
		return err
	})
	return out, err
}
