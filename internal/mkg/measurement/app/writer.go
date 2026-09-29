package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Producer là tên nguồn phát trên phong bì sự kiện.
const Producer = "mkg/measurement"

// Writer hiện thực các cổng tenant-tường-minh của port (không cần principal):
// TicketWriter, LotMass, DeviceRegistry, DeviceHealth, WeighingSessions. Dùng
// được với pool vai ingest_mkg (cmd/ingest) lẫn measurement_app (module).
type Writer struct {
	DB    tenant.Beginner
	Clock clock.Clock
}

var (
	_ port.TicketWriter     = (*Writer)(nil)
	_ port.LotMass          = (*Writer)(nil)
	_ port.DeviceRegistry   = (*Writer)(nil)
	_ port.DeviceHealth     = (*Writer)(nil)
	_ port.WeighingSessions = (*Writer)(nil)
)

// Record cài đặt port.TicketWriter (R-70, idempotent theo IdemKey).
func (w *Writer) Record(ctx context.Context, tid uuid.UUID, d port.TicketDraft) (port.Ticket, error) {
	if err := domain.NormalizeDraft(&d); err != nil {
		return port.Ticket{}, err
	}
	var out port.Ticket
	run := func() error {
		return tenant.WithTenantTx(ctx, w.DB, tid, func(tx pgx.Tx) error {
			t, _, err := recordTx(ctx, tx, tid, d, nil)
			out = t
			return err
		})
	}
	err := run()
	if errors.Is(err, ledger.ErrDuplicate) {
		err = run() // lần ghi song song cùng khóa đã thắng → lần này là phát lại
	}
	return out, err
}

// recordTx ghi phiếu trong giao dịch có sẵn: phát lại theo idem_key; khóa bảng
// chiếu của lô; kiểm tuyến, phiếu hiệu lực / phiếu bù, thiết bị, phiên; ghi sổ +
// bảng chiếu + outbox nội bộ. d đã qua NormalizeDraft.
func recordTx(ctx context.Context, tx pgx.Tx, tid uuid.UUID, d port.TicketDraft, importRef *uuid.UUID) (port.Ticket, bool, error) {
	if prev, err := store.TicketByIdem(ctx, tx, d.IdemKey); err != nil || prev != nil {
		if prev != nil {
			return *prev, true, nil
		}
		return port.Ticket{}, false, err
	}
	if prev, err := store.TicketByID(ctx, tx, d.ID); err != nil {
		return port.Ticket{}, false, err
	} else if prev != nil {
		return port.Ticket{}, false, fmt.Errorf("%w: id đã dùng cho phiếu khác", port.ErrInvalidTicket)
	}
	lm, _, err := store.LockLotMass(ctx, tx, tid, d.LotRef, d.Route)
	if err != nil {
		return port.Ticket{}, false, err
	}
	if lm.Route != d.Route {
		return port.Ticket{}, false, fmt.Errorf("%w: lô đã có tuyến %s", port.ErrInvalidTicket, lm.Route)
	}
	cur, has := lm.Points[d.Point]
	switch {
	case has && d.Supersedes == nil:
		return port.Ticket{}, false, fmt.Errorf("%w: lô đã có phiếu hiệu lực ở %s — phiếu mới phải là phiếu bù (supersedes)", port.ErrInvalidTicket, d.Point)
	case d.Supersedes != nil:
		old, err := store.TicketByID(ctx, tx, *d.Supersedes)
		if err != nil {
			return port.Ticket{}, false, err
		}
		if old == nil || old.LotRef != d.LotRef || old.Point != d.Point {
			return port.Ticket{}, false, fmt.Errorf("%w: supersedes phải trỏ phiếu cùng lô, cùng điểm cân", port.ErrInvalidTicket)
		}
		if !has || cur.TicketID != old.ID {
			return port.Ticket{}, false, fmt.Errorf("%w: phiếu bị bù không còn hiệu lực", port.ErrInvalidTicket)
		}
	}
	if d.Source == port.SourceEdge {
		w := d.Integration.Window
		dev, err := store.GetDevice(ctx, tx, w.DeviceID, w.Epoch, false)
		if err != nil {
			return port.Ticket{}, false, err
		}
		if dev == nil || dev.RawStatus == "pending" || dev.RawStatus == "rejected" {
			return port.Ticket{}, false, fmt.Errorf("%w: %s kỳ %d", port.ErrUnknownDevice, w.DeviceID, w.Epoch)
		}
		if dev.Point != d.Point {
			return port.Ticket{}, false, fmt.Errorf("%w: thiết bị gắn điểm %s", port.ErrInvalidTicket, dev.Point)
		}
	}
	if d.SessionRef != nil {
		s, err := store.GetSession(ctx, tx, *d.SessionRef, false)
		if err != nil {
			return port.Ticket{}, false, err
		}
		if s == nil || s.LotRef != d.LotRef || s.Point != d.Point {
			return port.Ticket{}, false, fmt.Errorf("%w: phiên cân không thuộc lô / điểm cân", port.ErrInvalidTicket)
		}
	}
	seq, rec, err := store.InsertTicket(ctx, tx, tid, d, importRef)
	if err != nil {
		return port.Ticket{}, false, ledger.Classify(err)
	}
	t := port.Ticket{TicketDraft: d, Seq: seq, RecordedAt: rec}
	if err := store.ApplyTicket(ctx, tx, t); err != nil {
		return t, false, err
	}
	ev := port.WeighTicketRecordedV1{
		TicketID: t.ID, LotRef: t.LotRef, Point: t.Point, Route: t.Route, Source: t.Source, MassKg: t.MassKg,
		EventTime: t.EventTime.UTC(), SupersedesID: t.Supersedes,
	}
	if t.MoisturePct != "" {
		m := t.MoisturePct
		ev.MoisturePct = &m
	}
	if t.Integration != nil {
		rv := t.Integration.RuleVersion
		ev.RuleVersion = &rv
	}
	err = outbox.Enqueue(ctx, tx, outbox.Event{
		ID: t.ID, TenantID: tid, Type: port.EventWeighTicketRecorded, SchemaVersion: 1, OccurredAt: ev.EventTime,
		TraceID: telemetry.TraceID(ctx), Producer: Producer, Payload: ev, Kind: outbox.KindInternal,
	})
	return t, false, err
}

// LotMass cài đặt port.LotMass.
func (w *Writer) LotMass(ctx context.Context, tid, lot uuid.UUID) (port.LotMassView, error) {
	var v port.LotMassView
	err := tenant.WithTenantTx(ctx, w.DB, tid, func(tx pgx.Tx) error {
		m, err := store.GetLotMass(ctx, tx, lot, false)
		if err != nil {
			return err
		}
		if m == nil || len(m.Points) == 0 {
			return port.ErrNotFound
		}
		v = m.ToView()
		return nil
	})
	return v, err
}

// Resolve cài đặt port.DeviceRegistry: tenant suy ra từ thư mục thiết bị (R-16).
func (w *Writer) Resolve(ctx context.Context, deviceID string, epoch int, certSHA256 string) (port.Device, error) {
	var out port.Device
	var tid uuid.UUID
	err := func() error {
		tx, err := w.DB.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		t, ok, err := store.DirectoryTenant(ctx, tx, deviceID)
		if err != nil {
			return err
		}
		if !ok {
			return port.ErrUnknownDevice
		}
		tid = t
		return nil
	}()
	if err != nil {
		return out, err
	}
	err = tenant.WithTenantTx(ctx, w.DB, tid, func(tx pgx.Tx) error {
		d, err := store.GetDevice(ctx, tx, deviceID, epoch, false)
		if err != nil {
			return err
		}
		if d == nil || d.RawStatus == "pending" || d.RawStatus == "rejected" {
			return port.ErrUnknownDevice
		}
		switch d.RawStatus {
		case "revoked":
			return port.ErrDeviceRevoked
		case "superseded":
			return port.ErrEpochSuperseded
		}
		newer, err := store.NewerEpochApproved(ctx, tx, deviceID, epoch)
		if err != nil {
			return err
		}
		if newer {
			return port.ErrEpochSuperseded
		}
		if d.CertSHA256 != certSHA256 {
			return port.ErrCertMismatch
		}
		out = d.Device
		return nil
	})
	return out, err
}

// ReportHealth cài đặt port.DeviceHealth.
func (w *Writer) ReportHealth(ctx context.Context, tid uuid.UUID, h port.HealthReport) error {
	if h.BufferPct != "" {
		p, err := domain.ParsePct("buffer_pct", h.BufferPct)
		if err != nil {
			return err
		}
		h.BufferPct = p.StringFixed(domain.PctScale)
	}
	return tenant.WithTenantTx(ctx, w.DB, tid, func(tx pgx.Tx) error {
		ok, err := store.UpdateHealth(ctx, tx, h)
		if err == nil && !ok {
			return port.ErrUnknownDevice
		}
		return err
	})
}

// PendingSessions trả phiên đã đóng chưa có phiếu ở điểm cân của phiên, trên MỌI
// tenant (tenant lấy từ hàng session_directory không RLS, chi tiết đọc trong giao
// dịch tenant). Cũ trước theo từng tenant; tối đa limit phiên.
func (w *Writer) PendingSessions(ctx context.Context, limit int) ([]port.Session, error) {
	if limit <= 0 {
		limit = 100
	}
	var tids []uuid.UUID
	err := func() error {
		tx, err := w.DB.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		tids, err = store.PendingTenants(ctx, tx)
		return err
	}()
	if err != nil {
		return nil, err
	}
	var out []port.Session
	for _, tid := range tids {
		if len(out) >= limit {
			break
		}
		err := tenant.WithTenantTx(ctx, w.DB, tid, func(tx pgx.Tx) error {
			ss, err := store.PendingSessions(ctx, tx, limit-len(out))
			out = append(out, ss...)
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// TareKg trả bì đã ghi lúc đóng phiên của đúng cửa sổ (0 khi không ghi) — tất định
// cho tích phân lại khi tranh chấp.
func (w *Writer) TareKg(ctx context.Context, tid uuid.UUID, win port.Window) (mass.Kg, error) {
	var out mass.Kg
	err := tenant.WithTenantTx(ctx, w.DB, tid, func(tx pgx.Tx) error {
		s, err := store.SessionByWindow(ctx, tx, win)
		if err != nil || s == nil || s.TareKg == nil {
			return err
		}
		out = *s.TareKg
		return nil
	})
	return out, err
}

// Session cài đặt port.WeighingSessions.
func (w *Writer) Session(ctx context.Context, tid, id uuid.UUID) (port.Session, error) {
	var out port.Session
	err := tenant.WithTenantTx(ctx, w.DB, tid, func(tx pgx.Tx) error {
		s, err := store.GetSession(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if s == nil {
			return port.ErrNotFound
		}
		out = *s
		if s.Status != port.SessionClosed {
			return port.ErrSessionOpen
		}
		return nil
	})
	return out, err
}
