package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// ErrCollectionConflict: lệnh gom đã gắn với lô cân khác, hoặc lô đã gắn lệnh gom khác mà
// không có lý do đổi (docs/errors.md).
var ErrCollectionConflict = problem.New(409, "measurement.collection_link_conflict",
	"Lệnh gom đã gắn với lô cân khác, hoặc lô đã gắn lệnh gom khác (đổi cần lý do)")

// ConsumerActor là actor_ref của consumer sự kiện nội bộ trong sổ measurement (không phải người).
var ConsumerActor = uuid.MustParse("00000000-0000-0000-0000-0000000e0a51")

var _ port.CollectionLinks = (*Writer)(nil)

// CollectionOfLot cài đặt port.CollectionLinks.
func (w *Writer) CollectionOfLot(ctx context.Context, tid, lot uuid.UUID) (port.CollectionLink, error) {
	var out port.CollectionLink
	err := tenant.WithTenantTx(ctx, w.DB, tid, func(tx pgx.Tx) error {
		var e error
		out, e = collectionOfLot(ctx, tx, lot)
		return e
	})
	return out, err
}

// LotOfCollection cài đặt port.CollectionLinks.
func (w *Writer) LotOfCollection(ctx context.Context, tid, order uuid.UUID) (port.CollectionLink, error) {
	var out port.CollectionLink
	err := tenant.WithTenantTx(ctx, w.DB, tid, func(tx pgx.Tx) error {
		var e error
		out, e = lotOfCollection(ctx, tx, order)
		return e
	})
	return out, err
}

func collectionOfLot(ctx context.Context, tx pgx.Tx, lot uuid.UUID) (port.CollectionLink, error) {
	l, err := store.CollectionOfLot(ctx, tx, lot)
	if err != nil || l == nil {
		return port.CollectionLink{LotRef: lot, CollectionOrderRef: lot}, err
	}
	return port.CollectionLink{LotRef: lot, CollectionOrderRef: l.CollectionOrderRef, Explicit: true}, nil
}

func lotOfCollection(ctx context.Context, tx pgx.Tx, order uuid.UUID) (port.CollectionLink, error) {
	l, err := store.LotOfCollection(ctx, tx, order)
	if err != nil || l == nil {
		return port.CollectionLink{LotRef: order, CollectionOrderRef: order}, err
	}
	return port.CollectionLink{LotRef: l.LotRef, CollectionOrderRef: order, Explicit: true}, nil
}

// linkTx ghi liên kết lô ↔ lệnh gom trong giao dịch có sẵn. Cùng lệnh gom → không ghi
// (nil, false). Lô đã gắn lệnh gom khác → cần lý do (bản ghi bù); lệnh gom đã gắn lô
// khác → ErrCollectionConflict.
func linkTx(ctx context.Context, tx pgx.Tx, tid, actor, lot, order uuid.UUID, origin string, session *uuid.UUID, reason, idem string,
	at time.Time) (*store.CollectionLink, bool, error) {
	cur, err := store.CollectionOfLot(ctx, tx, lot)
	if err != nil {
		return nil, false, err
	}
	if cur != nil && cur.CollectionOrderRef == order {
		return cur, false, nil
	}
	other, err := store.LotOfCollection(ctx, tx, order)
	if err != nil {
		return nil, false, err
	}
	if other != nil && other.LotRef != lot {
		return nil, false, ErrCollectionConflict.WithDetail("lệnh gom đã gắn với lô " + other.LotRef.String())
	}
	l := store.CollectionLink{ID: ids.New(), LotRef: lot, CollectionOrderRef: order, Origin: origin, SessionRef: session, EventTime: at,
		ActorRef: actor, IdemKey: idem}
	if cur != nil {
		if !domain.ValidReason(reason) {
			return nil, false, ErrCollectionConflict.WithDetail("lô đã gắn lệnh gom " + cur.CollectionOrderRef.String() +
				" — đổi cần lý do 10..500 ký tự")
		}
		l.SupersedesID, l.CorrectionReason = &cur.ID, &reason
	}
	if err := store.InsertCollectionLink(ctx, tx, tid, &l); err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "lot_collection_current_order" {
			return nil, false, ErrCollectionConflict
		}
		return nil, false, insert(err)
	}
	return &l, true, nil
}

// LinkCollection gắn lô cân với lệnh gom của mkg/fieldops (reconciliation_officer hoặc
// scale_supervisor) — dùng cho lô DIRECT (chỉ CUSTOMER_IN) hoặc khi phiên cân không mang
// collection_order_ref. Đổi liên kết cần lý do. Gắn xong đối soát lại lô (nếu đủ phiếu)
// để số cuộn của lệnh gom vào đối chiếu RB-6.
func (s *Service) LinkCollection(ctx context.Context, lot, order uuid.UUID, reason, idem string) (out store.CollectionLink, replayed bool, err error) {
	p, err := authz.Require(ctx, port.RoleReconciliationOfficer, port.RoleScaleSupervisor)
	if err != nil {
		return out, false, err
	}
	if err := requireIdem(idem); err != nil {
		return out, false, err
	}
	if lot == uuid.Nil || order == uuid.Nil {
		return out, false, problem.Validation.WithField("collection_order_ref", "bắt buộc")
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		l, _, err := linkTx(ctx, tx, p.TenantID, p.ActorRef, lot, order, store.LinkManual, nil, reason, idem, s.Clock.Now())
		if err != nil {
			return err
		}
		out = *l
		_, _, err = s.reconcileTx(ctx, tx, p, lot, idem+"#recon", true)
		return err
	}, func(tx pgx.Tx) (bool, error) {
		l, err := store.CollectionLinkByIdem(ctx, tx, idem)
		if err != nil || l == nil {
			return false, err
		}
		out = *l
		return true, nil
	})
	return out, replayed, err
}

// Collection đọc liên kết hiện hành của lô (Explicit = false: quy ước đồng nhất).
func (s *Service) Collection(ctx context.Context, lot uuid.UUID) (port.CollectionLink, *store.CollectionLink, error) {
	var out port.CollectionLink
	var row *store.CollectionLink
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		if row, e = store.CollectionOfLot(ctx, tx, lot); e != nil {
			return e
		}
		out, e = collectionOfLot(ctx, tx, lot)
		return e
	})
	return out, row, err
}

// rollTotal đọc số cuộn của lệnh gom liên kết với lô (RB-6). weighedAt = thời điểm phiếu
// cân đầu tiên của lô: hiện thực có RollTotalsAsOf thì ước lượng kg/cuộn học trước mốc đó.
func (s *Service) rollTotal(ctx context.Context, tx pgx.Tx, tid, lot uuid.UUID, weighedAt time.Time) (*port.RollTotal, error) {
	link, err := collectionOfLot(ctx, tx, lot)
	if err != nil {
		return nil, err
	}
	var rt port.RollTotal
	if a, ok := s.rolls().(port.RollTotalsAsOf); ok && !weighedAt.IsZero() {
		rt, err = a.RollTotalAsOf(ctx, tid, link.CollectionOrderRef, weighedAt)
	} else {
		rt, err = s.rolls().RollTotal(ctx, tid, link.CollectionOrderRef)
	}
	switch {
	case err == nil:
		return &rt, nil
	case errors.Is(err, port.ErrNotFound):
		return nil, nil
	}
	return nil, err
}

// ---------------------------------------------------------------------------
// Consumer sự kiện nội bộ (cmd/worker-internal, vai measurement_app)
// ---------------------------------------------------------------------------

// RollCountRecorded đối soát lại lô cân liên kết với lệnh gom khi số cuộn mới về
// (mkg.fieldops.roll_count.recorded). Chạy trong giao dịch inbox (tenant của phong bì).
// Lô chưa đủ phiếu → không làm gì. Idempotent: inbox (R-61) + idem theo roll_count_id,
// và đầu vào không đổi → không ghi lần đối soát mới.
func (s *Service) RollCountRecorded(ctx context.Context, tx pgx.Tx, tid, order, rollCountID uuid.UUID) error {
	link, err := lotOfCollection(ctx, tx, order)
	if err != nil {
		return err
	}
	lm, err := store.GetLotMass(ctx, tx, link.LotRef, false)
	if err != nil || lm == nil || len(lm.Points) == 0 {
		return err // lô chưa cân: đối soát sẽ chạy khi có CUSTOMER_IN
	}
	p := authz.Principal{TenantID: tid, ActorRef: ConsumerActor}
	_, _, err = s.reconcileTx(ctx, tx, p, link.LotRef, "rolls:"+rollCountID.String()+":"+link.LotRef.String(), true)
	return err
}
