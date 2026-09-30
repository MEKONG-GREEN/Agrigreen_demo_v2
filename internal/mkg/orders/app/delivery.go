package app

import (
	"context"
	"encoding/base64"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Giao nhận (mkg.md §5.5): lịch giao theo đơn bán (planned → dispatched), biên bản giao nhận liên kết
// phiếu cân CUSTOMER_IN của measurement (khối lượng chuẩn — không nhập tay), chấp nhận phát sự kiện
// NỘI BỘ mkg.orders.delivery.accepted (vhost mkg.internal; giá, khách không bao giờ qua van). Sửa sai
// sau chấp nhận = dòng accepted mới supersedes dòng trước + sự kiện bản bù (RB-1).

// DeliveryInput là một lượt giao trong lịch giao của đơn bán.
type DeliveryInput struct {
	LotRef      uuid.UUID
	Route       string
	PlannedDate string
	PlannedKg   mass.Kg
	TripRef     *uuid.UUID
	IdemKey     string
}

// AcceptInput là biên bản giao nhận.
type AcceptInput struct {
	EvidenceFileRef uuid.UUID
	EvidenceKind    string // epod | minutes | ebill
	DisputeRef      *uuid.UUID
	AcceptedAt      *time.Time
	IdemKey         string
}

// PlanDelivery thêm một lượt giao vào lịch giao của đơn bán đã xác nhận.
func (s *Service) PlanDelivery(ctx context.Context, orderID uuid.UUID, in DeliveryInput) (store.Delivery, bool, error) {
	p, err := authz.Require(ctx, RoleOfficer)
	if err != nil {
		return store.Delivery{}, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return store.Delivery{}, false, err
	}
	x, bad := problem.Validation, false
	if in.LotRef == uuid.Nil {
		x, bad = x.WithField("lot_ref", "bắt buộc"), true
	}
	if !domain.ValidRoute(in.Route) {
		x, bad = x.WithField("route", "HUB | DIRECT"), true
	}
	if in.PlannedKg.Decimal().Sign() <= 0 {
		x, bad = x.WithField("planned_kg", "phải lớn hơn 0"), true
	}
	day, err := domain.ParseDate("planned_date", in.PlannedDate)
	if err != nil {
		x, bad = x.WithField("planned_date", "ngày YYYY-MM-DD"), true
	}
	if bad {
		return store.Delivery{}, false, x
	}
	now := s.Clock.Now()
	var out store.Delivery
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		o, err := store.GetOrder(ctx, tx, orderID, true)
		if err != nil {
			return err
		}
		if o == nil {
			return ErrOrderNotFound
		}
		if o.Status != domain.SOConfirmed && o.Status != domain.SODelivering {
			return ErrOrderTransition.WithDetail("lập lịch giao cho đơn bán đã xác nhận / đang giao")
		}
		d := store.Delivery{Meta: store.Meta{ID: ids.New(), EventTime: now, ActorRef: p.ActorRef, IdemKey: in.IdemKey, RuleVersion: domain.RuleDelivery},
			Kind: "planned", DeliveryID: ids.New(), OrderID: orderID, Status: string(port.DeliveryPlanned), CustomerRef: o.CustomerRef,
			Route: in.Route, LotRef: in.LotRef, TripRef: in.TripRef, PlannedDate: day, PlannedKg: in.PlannedKg.Decimal(), UnitPrice: o.NetPrice}
		lt, err := store.GetLotTicket(ctx, tx, in.LotRef, "CUSTOMER_IN")
		if err != nil {
			return err
		}
		if lt != nil {
			d.TicketRef, d.TicketKg = &lt.TicketID, decimal.NullDecimal{Decimal: lt.MassKg, Valid: true}
		}
		if err := store.Lock(ctx, tx, p.TenantID, "code:delivery"); err != nil {
			return err
		}
		n, err := store.NextCode(ctx, tx, "delivery_current", "delivery_id")
		if err != nil {
			return err
		}
		d.Code = domain.Code("GN", n, 6)
		if err := store.InsertDeliveryEvent(ctx, tx, p.TenantID, &d); err != nil {
			return err
		}
		return loadDelivery(ctx, tx, d.DeliveryID, &out)
	}, s.replayDelivery(ctx, in.IdemKey, &out))
	return out, replayed, err
}

// DispatchDelivery xuất giao (planned → dispatched); đơn bán confirmed → delivering cùng giao dịch.
func (s *Service) DispatchDelivery(ctx context.Context, id uuid.UUID, trip *uuid.UUID, idem string) (store.Delivery, bool, error) {
	p, err := authz.Require(ctx, RoleOfficer)
	if err != nil {
		return store.Delivery{}, false, err
	}
	if err := requireIdem(idem); err != nil {
		return store.Delivery{}, false, err
	}
	var out store.Delivery
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		d, err := store.GetDelivery(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if d == nil {
			return ErrDeliveryNotFound
		}
		if d.Status != string(port.DeliveryPlanned) {
			return ErrDeliveryTransition.WithDetail("chỉ xuất giao lượt giao đang planned")
		}
		d.Meta = store.Meta{ID: ids.New(), EventTime: s.Clock.Now(), ActorRef: p.ActorRef, IdemKey: idem, RuleVersion: domain.RuleDelivery}
		d.Kind, d.Status = "dispatched", string(port.DeliveryDispatched)
		if trip != nil {
			d.TripRef = trip
		}
		if err := store.InsertDeliveryEvent(ctx, tx, p.TenantID, d); err != nil {
			return err
		}
		o, err := store.GetOrder(ctx, tx, d.OrderID, true)
		if err != nil {
			return err
		}
		if o != nil && o.Status == domain.SOConfirmed {
			if err := s.advance(ctx, tx, p.TenantID, o, domain.SODelivering, nil, p.ActorRef, idem+":order"); err != nil {
				return err
			}
		}
		return loadDelivery(ctx, tx, id, &out)
	}, s.replayDelivery(ctx, idem, &out))
	return out, replayed, err
}

// AcceptDelivery ghi biên bản giao nhận: khối lượng = phiếu CUSTOMER_IN đã gắn; tiền = kg/1000 × đơn giá
// sau chiết khấu của đơn bán, làm tròn 2 số; phát mkg.orders.delivery.accepted (nội bộ).
func (s *Service) AcceptDelivery(ctx context.Context, id uuid.UUID, in AcceptInput) (store.Delivery, bool, error) {
	p, err := authz.Require(ctx, RoleOfficer)
	if err != nil {
		return store.Delivery{}, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return store.Delivery{}, false, err
	}
	if in.EvidenceFileRef == uuid.Nil || !domain.ValidEvidenceKind(in.EvidenceKind) {
		return store.Delivery{}, false, problem.Validation.WithField("evidence", "bắt buộc: file_ref + kind (epod | minutes | ebill)")
	}
	now := s.Clock.Now()
	at := now
	if in.AcceptedAt != nil {
		if in.AcceptedAt.After(now.Add(5 * time.Minute)) {
			return store.Delivery{}, false, problem.Validation.WithField("accepted_at", "ở tương lai")
		}
		at = *in.AcceptedAt
	}
	var out store.Delivery
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		d, err := store.GetDelivery(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if d == nil {
			return ErrDeliveryNotFound
		}
		if d.Status != string(port.DeliveryDispatched) {
			return ErrDeliveryTransition.WithDetail("chỉ chấp nhận lượt giao đã xuất (dispatched)")
		}
		if d.TicketRef == nil {
			return ErrCustomerInMissing
		}
		d.EvidenceFileRef, d.EvidenceKind, d.DisputeRef = &in.EvidenceFileRef, &in.EvidenceKind, in.DisputeRef
		d.AcceptedAt = &at
		if err := s.accept(ctx, tx, p.TenantID, d, store.Meta{ID: ids.New(), EventTime: now, ActorRef: p.ActorRef, IdemKey: in.IdemKey}); err != nil {
			return err
		}
		return loadDelivery(ctx, tx, id, &out)
	}, s.replayDelivery(ctx, in.IdemKey, &out))
	return out, replayed, err
}

// CorrectionInput là chấp nhận bù của một giao nhận đã chấp nhận.
type CorrectionInput struct {
	TicketRef  *uuid.UUID // phiếu CUSTOMER_IN khác của cùng lô (đã nhận từ measurement); rỗng = phiếu hiệu lực của lô
	DisputeRef *uuid.UUID
	Reason     string
	IdemKey    string
}

// CorrectDelivery ghi chấp nhận bù (supersedes dòng accepted trước, có lý do) và phát sự kiện bản bù.
func (s *Service) CorrectDelivery(ctx context.Context, id uuid.UUID, in CorrectionInput) (store.Delivery, bool, error) {
	p, err := authz.Require(ctx, RoleOfficer)
	if err != nil {
		return store.Delivery{}, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return store.Delivery{}, false, err
	}
	r, err := domain.CheckReason("reason", in.Reason, true)
	if err != nil {
		return store.Delivery{}, false, mapErr(err)
	}
	var out store.Delivery
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		d, err := store.GetDelivery(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if d == nil {
			return ErrDeliveryNotFound
		}
		if d.Status != string(port.DeliveryAccepted) {
			return ErrDeliveryTransition.WithDetail("chỉ bù giao nhận đã chấp nhận")
		}
		if in.TicketRef != nil {
			t, err := store.TicketByID(ctx, tx, *in.TicketRef)
			if err != nil {
				return err
			}
			if t == nil || t.LotRef != d.LotRef || t.Point != "CUSTOMER_IN" {
				return ErrTicketUnknown
			}
			d.TicketRef, d.TicketKg = &t.TicketID, decimal.NullDecimal{Decimal: t.MassKg, Valid: true}
		} else {
			lt, err := store.GetLotTicket(ctx, tx, d.LotRef, "CUSTOMER_IN")
			if err != nil {
				return err
			}
			if lt == nil {
				return ErrCustomerInMissing
			}
			d.TicketRef, d.TicketKg = &lt.TicketID, decimal.NullDecimal{Decimal: lt.MassKg, Valid: true}
		}
		if in.DisputeRef != nil {
			d.DisputeRef = in.DisputeRef
		}
		prev := *d.AcceptanceID
		if err := s.accept(ctx, tx, p.TenantID, d, store.Meta{ID: ids.New(), EventTime: s.Clock.Now(), ActorRef: p.ActorRef, IdemKey: in.IdemKey,
			SupersedesID: &prev, CorrectionReason: &r}); err != nil {
			return err
		}
		return loadDelivery(ctx, tx, id, &out)
	}, s.replayDelivery(ctx, in.IdemKey, &out))
	return out, replayed, err
}

// accept ghi dòng 'accepted' (khối lượng = phiếu CUSTOMER_IN đã gắn) và phát sự kiện nội bộ cùng giao dịch.
func (s *Service) accept(ctx context.Context, tx pgx.Tx, tid uuid.UUID, d *store.Delivery, meta store.Meta) error {
	price, err := money.ParsePrice(d.UnitPrice.String())
	if err != nil {
		return err
	}
	kg, err := mass.ParseKg(d.TicketKg.Decimal.String())
	if err != nil {
		return err
	}
	amt, err := domain.KgAmount(kg, price)
	if err != nil {
		return err
	}
	meta.RuleVersion = domain.RuleDelivery
	d.Meta = meta
	d.Kind, d.Status = "accepted", string(port.DeliveryAccepted)
	d.AcceptedKg = d.TicketKg
	d.Amount = decimal.NullDecimal{Decimal: amt.Decimal(), Valid: true}
	d.AcceptanceID = &d.ID
	if err := store.InsertDeliveryEvent(ctx, tx, tid, d); err != nil {
		return err
	}
	ev := port.DeliveryAcceptedV1{
		DeliveryID: d.DeliveryID, OrderRef: d.OrderID, CustomerRef: d.CustomerRef, Route: d.Route, TripRef: d.TripRef,
		CustomerInTicketRef: *d.TicketRef, Lots: []port.DeliveryLotV1{{LotRef: d.LotRef, MassKg: kg}}, AcceptedKg: kg,
		UnitPrice: price.String(), Amount: amt, DisputeRef: d.DisputeRef, AcceptedAt: d.AcceptedAt.UTC(), RuleVersion: domain.RuleDelivery,
		SupersedesID: meta.SupersedesID, EventTime: meta.EventTime.UTC(),
	}
	if d.EvidenceFileRef != nil {
		ev.Evidence = &port.DeliveryEvidenceV1{FileRef: *d.EvidenceFileRef, Kind: *d.EvidenceKind}
	}
	return outbox.Enqueue(ctx, tx, outbox.Event{
		ID: d.ID, TenantID: tid, Type: port.EventDeliveryAccepted, SchemaVersion: 1, OccurredAt: ev.EventTime,
		TraceID: telemetry.TraceID(ctx), Producer: Producer, Payload: ev, Kind: outbox.KindInternal,
	})
}

// RejectDelivery ghi khách từ chối / hủy lượt giao chưa chấp nhận — có lý do.
func (s *Service) RejectDelivery(ctx context.Context, id uuid.UUID, cancel bool, reason, idem string) (store.Delivery, bool, error) {
	p, err := authz.Require(ctx, RoleOfficer)
	if err != nil {
		return store.Delivery{}, false, err
	}
	if err := requireIdem(idem); err != nil {
		return store.Delivery{}, false, err
	}
	r, err := domain.CheckReason("reason", reason, true)
	if err != nil {
		return store.Delivery{}, false, mapErr(err)
	}
	var out store.Delivery
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		d, err := store.GetDelivery(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if d == nil {
			return ErrDeliveryNotFound
		}
		kind, status := "rejected", string(port.DeliveryRejected)
		if cancel {
			kind, status = "cancelled", "cancelled"
			if d.Status != string(port.DeliveryPlanned) {
				return ErrDeliveryTransition.WithDetail("chỉ hủy lượt giao chưa xuất")
			}
		} else if d.Status != string(port.DeliveryDispatched) {
			return ErrDeliveryTransition.WithDetail("chỉ ghi từ chối cho lượt giao đã xuất")
		}
		d.Meta = store.Meta{ID: ids.New(), EventTime: s.Clock.Now(), ActorRef: p.ActorRef, IdemKey: idem, RuleVersion: domain.RuleDelivery}
		d.Kind, d.Status, d.Reason = kind, status, &r
		if err := store.InsertDeliveryEvent(ctx, tx, p.TenantID, d); err != nil {
			return err
		}
		return loadDelivery(ctx, tx, id, &out)
	}, s.replayDelivery(ctx, idem, &out))
	return out, replayed, err
}

// GetDelivery đọc giao nhận (HTTP).
func (s *Service) GetDelivery(ctx context.Context, id uuid.UUID) (store.Delivery, error) {
	var out store.Delivery
	err := s.read(ctx, func(tx pgx.Tx) error { return loadDelivery(ctx, tx, id, &out) })
	return out, err
}

func loadDelivery(ctx context.Context, tx pgx.Tx, id uuid.UUID, out *store.Delivery) error {
	d, err := store.GetDelivery(ctx, tx, id, false)
	if err != nil {
		return err
	}
	if d == nil {
		return ErrDeliveryNotFound
	}
	*out = *d
	return nil
}

func (s *Service) replayDelivery(ctx context.Context, idem string, out *store.Delivery) func(pgx.Tx) (bool, error) {
	return func(tx pgx.Tx) (bool, error) {
		id, err := store.EntityByIdem(ctx, tx, "delivery_event", "delivery_id", idem)
		if err != nil || id == nil {
			return false, err
		}
		return true, loadDelivery(ctx, tx, *id, out)
	}
}

// ---------------------------------------------------------------------------
// port.Deliveries (mkg/costing, tenant tường minh)
// ---------------------------------------------------------------------------

// Delivery cài đặt port.Deliveries.
func (s *Service) Delivery(ctx context.Context, tid, id uuid.UUID) (port.DeliveryView, error) {
	var v port.DeliveryView
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		d, err := store.GetDelivery(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if d == nil {
			return port.ErrDeliveryNotFound
		}
		v, err = DeliveryViewOf(*d)
		return err
	})
	return v, err
}

// Accepted cài đặt port.Deliveries: giao nhận accepted với accepted_at ∈ [from, to), dòng sổ ghi trước
// asOf (ảnh chụp đầu vào của đóng kỳ), sắp theo (accepted_at, id), con trỏ mờ.
func (s *Service) Accepted(ctx context.Context, tid uuid.UUID, from, to, asOf time.Time, cursor string, limit int) (port.DeliveryPage, error) {
	var page port.DeliveryPage
	afterAt, afterID, err := decodeAcceptedCursor(cursor)
	if err != nil {
		return page, err
	}
	limit = clampLimit(limit)
	err = tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		ds, err := store.AcceptedAsOf(ctx, tx, from, to, asOf, afterAt, afterID, limit+1)
		if err != nil {
			return err
		}
		if len(ds) > limit {
			ds = ds[:limit]
			last := ds[limit-1]
			page.Next = encodeAcceptedCursor(*last.AcceptedAt, last.DeliveryID)
		}
		for _, d := range ds {
			v, err := DeliveryViewOf(d)
			if err != nil {
				return err
			}
			page.Items = append(page.Items, v)
		}
		return nil
	})
	return page, err
}

// DeliveryViewOf chuyển ảnh chụp giao nhận sang DTO của port.
func DeliveryViewOf(d store.Delivery) (port.DeliveryView, error) {
	v := port.DeliveryView{DeliveryID: d.DeliveryID, OrderRef: d.OrderID, CustomerRef: d.CustomerRef, Status: port.DeliveryStatus(d.Status),
		Route: d.Route, TripRef: d.TripRef, CustomerInTicketRef: d.TicketRef, AcceptedAt: d.AcceptedAt, RuleVersion: d.RuleVersion,
		RecordedAt: d.RecordedAt}
	price, err := money.ParsePrice(d.UnitPrice.String())
	if err != nil {
		return v, err
	}
	v.UnitPrice = &price
	lotKg := d.PlannedKg
	if d.AcceptedKg.Valid {
		k, err := mass.ParseKg(d.AcceptedKg.Decimal.String())
		if err != nil {
			return v, err
		}
		v.AcceptedKg, lotKg = &k, d.AcceptedKg.Decimal
	}
	lk, err := mass.ParseKg(lotKg.String())
	if err != nil {
		return v, err
	}
	v.Lots = []port.DeliveryLot{{LotRef: d.LotRef, MassKg: lk}}
	if d.Amount.Valid {
		a, err := money.ParseAmount(d.Amount.Decimal.StringFixed(2))
		if err != nil {
			return v, err
		}
		v.Amount = &a
	}
	return v, nil
}

func encodeAcceptedCursor(at time.Time, id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at.UTC().Format(time.RFC3339Nano) + "|" + id.String()))
}

func decodeAcceptedCursor(c string) (*time.Time, *uuid.UUID, error) {
	if c == "" {
		return nil, nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return nil, nil, problem.Validation.WithField("cursor", "không hợp lệ")
	}
	parts := strings.SplitN(string(b), "|", 2)
	if len(parts) != 2 {
		return nil, nil, problem.Validation.WithField("cursor", "không hợp lệ")
	}
	at, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return nil, nil, problem.Validation.WithField("cursor", "không hợp lệ")
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return nil, nil, problem.Validation.WithField("cursor", "không hợp lệ")
	}
	return &at, &id, nil
}
