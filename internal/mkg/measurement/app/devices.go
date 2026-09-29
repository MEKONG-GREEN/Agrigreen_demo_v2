package app

import (
	"context"
	"crypto/ed25519"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port/edgewire"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

var (
	deviceIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{2,63}$`)
	hex64Re    = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// RegisterInput là đề xuất đăng ký một kỳ thiết bị cân.
type RegisterInput struct {
	DeviceID    string
	Epoch       int // 0 = kỳ kế tiếp
	FacilityRef uuid.UUID
	Point       port.WeighPoint
	PublicKey   []byte
	CertSHA256  string
	ScaleModel  string
	IdemKey     string
}

// ProposeDevice ghi đề xuất đăng ký (scale_supervisor). Có hiệu lực khi người
// KHÁC có vai device_approver duyệt (R-56). Kỳ không bao giờ dùng lại (ADR-0019).
func (s *Service) ProposeDevice(ctx context.Context, in RegisterInput) (d store.DeviceRow, replayed bool, err error) {
	p, err := authz.Require(ctx, port.RoleScaleSupervisor)
	if err != nil {
		return d, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return d, false, err
	}
	v := problem.Validation
	bad := false
	add := func(f, m string) { v, bad = v.WithField(f, m), true }
	if !deviceIDRe.MatchString(in.DeviceID) {
		add("device_id", "3..64 ký tự chữ, số, . _ - (trùng CN/SAN chứng thư mTLS)")
	}
	if in.Point != port.PointHubIn && in.Point != port.PointHubOut {
		add("point", "HUB_IN hoặc HUB_OUT (CUSTOMER_IN không qua agent biên)")
	}
	if len(in.PublicKey) != ed25519.PublicKeySize {
		add("public_key", "khóa công khai Ed25519 32 byte (base64)")
	}
	if !hex64Re.MatchString(in.CertSHA256) {
		add("cert_sha256", "SHA-256 hex thường của chứng thư DER")
	}
	if in.FacilityRef == uuid.Nil {
		add("facility_ref", "bắt buộc")
	}
	if l := len([]rune(in.ScaleModel)); l < 1 || l > 100 {
		add("scale_model", "1..100 ký tự (C-6)")
	}
	if bad {
		return d, false, v
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		ok, err := store.EnsureDirectory(ctx, tx, p.TenantID, in.DeviceID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrDeviceTaken
		}
		maxEpoch, err := store.MaxEpoch(ctx, tx, in.DeviceID)
		if err != nil {
			return err
		}
		epoch := in.Epoch
		if epoch == 0 {
			epoch = maxEpoch + 1
		}
		if epoch <= maxEpoch {
			return problem.Validation.WithField("epoch", "kỳ phải lớn hơn mọi kỳ đã có — không dùng lại bộ đếm (ADR-0019)")
		}
		point, keyID, cert, model := string(in.Point), edgewire.KeyID(in.PublicKey), in.CertSHA256, in.ScaleModel
		fac := in.FacilityRef
		ver := store.DeviceVersion{
			ID: ids.New(), DeviceID: in.DeviceID, Epoch: epoch, Action: "register", FacilityRef: &fac, Point: &point,
			PublicKey: in.PublicKey, KeyID: &keyID, CertSHA256: &cert, ScaleModel: &model, EventTime: s.Clock.Now(),
			ActorRef: p.ActorRef, IdemKey: in.IdemKey,
		}
		if err := insert(store.InsertDeviceVersion(ctx, tx, p.TenantID, &ver)); err != nil {
			return err
		}
		if err := store.InsertPendingDevice(ctx, tx, p.TenantID, ver); err != nil {
			return mapErr(err)
		}
		row, err := store.GetDevice(ctx, tx, in.DeviceID, epoch, false)
		if row != nil {
			d = *row
		}
		return err
	}, func(tx pgx.Tx) (bool, error) {
		prev, err := store.DeviceVersionByIdem(ctx, tx, in.IdemKey)
		if err != nil || prev == nil {
			return false, err
		}
		row, err := store.GetDevice(ctx, tx, prev.DeviceID, prev.Epoch, false)
		if row != nil {
			d = *row
		}
		return true, err
	})
	return d, replayed, err
}

// DecideDevice duyệt / từ chối đề xuất đăng ký (device_approver ≠ người đề xuất).
func (s *Service) DecideDevice(ctx context.Context, deviceID string, epoch int, decision, reason, idem string) (d store.DeviceRow, replayed bool, err error) {
	p, err := authz.Require(ctx, port.RoleDeviceApprover)
	if err != nil {
		return d, false, err
	}
	if err := requireIdem(idem); err != nil {
		return d, false, err
	}
	if decision != "approved" && decision != "rejected" {
		return d, false, problem.Validation.WithField("decision", "approved hoặc rejected")
	}
	if decision == "rejected" && !domain.ValidReason(reason) {
		return d, false, problem.Validation.WithField("reason", "từ chối cần lý do 10..500 ký tự")
	}
	load := func(tx pgx.Tx) error {
		row, err := store.GetDevice(ctx, tx, deviceID, epoch, false)
		if row != nil {
			d = *row
		}
		return err
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		cur, err := store.GetDevice(ctx, tx, deviceID, epoch, true)
		if err != nil {
			return err
		}
		if cur == nil {
			return ErrDeviceNotFound
		}
		if cur.RawStatus != "pending" {
			return ErrNotPending
		}
		if err := authz.EnsureDistinctApproverFor(cur.ProposedBy, p, port.RoleDeviceApprover); err != nil {
			return err
		}
		now := s.Clock.Now()
		if _, err := store.InsertDeviceDecision(ctx, tx, p.TenantID, ids.New(), deviceID, epoch, cur.VersionID, decision, reasonPtr(reason),
			cur.ProposedBy, p.ActorRef, now, idem); err != nil {
			return insert(err)
		}
		status := "active"
		if decision == "rejected" {
			status = "rejected"
		} else if err := store.SupersedeOlderEpochs(ctx, tx, deviceID, epoch); err != nil {
			return err
		}
		actor := p.ActorRef
		if err := store.SetDeviceStatus(ctx, tx, deviceID, epoch, status, &actor, now, nil); err != nil {
			return err
		}
		return load(tx)
	}, func(tx pgx.Tx) (bool, error) {
		ok, err := store.DecisionByIdem(ctx, tx, idem)
		if err != nil || !ok {
			return false, err
		}
		return true, load(tx)
	})
	return d, replayed, err
}

// RevokeDevice thu hồi một kỳ đăng ký NGAY (device_approver hoặc scale_supervisor):
// thu hồi chỉ giảm tin cậy nên không cần người thứ hai; lô mới bị ingest từ chối.
func (s *Service) RevokeDevice(ctx context.Context, deviceID string, epoch int, reason, idem string) (d store.DeviceRow, replayed bool, err error) {
	p, err := authz.Require(ctx, port.RoleDeviceApprover, port.RoleScaleSupervisor)
	if err != nil {
		return d, false, err
	}
	if err := requireIdem(idem); err != nil {
		return d, false, err
	}
	if !domain.ValidReason(reason) {
		return d, false, problem.Validation.WithField("reason", "thu hồi cần lý do 10..500 ký tự")
	}
	load := func(tx pgx.Tx) error {
		row, err := store.GetDevice(ctx, tx, deviceID, epoch, false)
		if row != nil {
			d = *row
		}
		return err
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		cur, err := store.GetDevice(ctx, tx, deviceID, epoch, true)
		if err != nil {
			return err
		}
		if cur == nil {
			return ErrDeviceNotFound
		}
		if cur.RawStatus != "active" {
			return ErrDeviceNotActive
		}
		now := s.Clock.Now()
		r := reason
		ver := store.DeviceVersion{ID: ids.New(), DeviceID: deviceID, Epoch: epoch, Action: "revoke", Reason: &r, EventTime: now,
			ActorRef: p.ActorRef, IdemKey: idem}
		if err := insert(store.InsertDeviceVersion(ctx, tx, p.TenantID, &ver)); err != nil {
			return err
		}
		if err := store.SetDeviceStatus(ctx, tx, deviceID, epoch, "revoked", nil, now, &r); err != nil {
			return err
		}
		return load(tx)
	}, func(tx pgx.Tx) (bool, error) {
		prev, err := store.DeviceVersionByIdem(ctx, tx, idem)
		if err != nil || prev == nil {
			return false, err
		}
		return true, load(tx)
	})
	return d, replayed, err
}

// Devices liệt kê kỳ đăng ký (con trỏ theo device_id, epoch).
func (s *Service) Devices(ctx context.Context, afterDevice string, afterEpoch, limit int) ([]store.DeviceRow, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var out []store.DeviceRow
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		out, e = store.ListDevices(ctx, tx, afterDevice, afterEpoch, limit)
		return e
	})
	return out, err
}

// Device trả mọi kỳ của một thiết bị.
func (s *Service) Device(ctx context.Context, deviceID string) ([]store.DeviceRow, error) {
	var out []store.DeviceRow
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		out, e = store.DevicesOf(ctx, tx, deviceID)
		if e == nil && len(out) == 0 {
			return ErrDeviceNotFound
		}
		return e
	})
	return out, err
}

// ---------------------------------------------------------------------------
// Phiên cân
// ---------------------------------------------------------------------------

// OpenSessionInput mở phiên cân của lô trên một thiết bị.
type OpenSessionInput struct {
	ID          *uuid.UUID
	LotRef      uuid.UUID
	DeviceID    string
	Epoch       int
	CounterFrom int64
	EventTime   time.Time
	IdemKey     string
}

// OpenSession mở cửa sổ lô (scale_supervisor). Điểm cân, cơ sở lấy từ kỳ đăng ký
// ĐANG hiệu lực; cửa sổ không chồng phiên đã đóng; một thiết bị một phiên mở.
func (s *Service) OpenSession(ctx context.Context, in OpenSessionInput) (out port.Session, replayed bool, err error) {
	p, err := authz.Require(ctx, port.RoleScaleSupervisor)
	if err != nil {
		return out, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return out, false, err
	}
	if in.LotRef == uuid.Nil || in.DeviceID == "" || in.Epoch < 1 || in.CounterFrom < 0 {
		return out, false, problem.Validation.WithDetail("cần lot_ref, device_id, epoch ≥ 1, counter_from ≥ 0")
	}
	id := ids.New()
	if in.ID != nil {
		id = *in.ID
	}
	at := in.EventTime
	if at.IsZero() {
		at = s.Clock.Now()
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		dev, err := store.GetDevice(ctx, tx, in.DeviceID, in.Epoch, false)
		if err != nil {
			return err
		}
		if dev == nil || dev.RawStatus != "active" {
			return ErrDeviceNotActive
		}
		last, err := store.LastClosedCounter(ctx, tx, in.DeviceID, in.Epoch)
		if err != nil {
			return err
		}
		if in.CounterFrom <= last {
			return ErrSessionConflict.WithDetail("counter_from phải lớn hơn counter_to của phiên đã đóng trước")
		}
		lm, err := store.GetLotMass(ctx, tx, in.LotRef, false)
		if err != nil {
			return err
		}
		if lm != nil && lm.Route == port.RouteDirect {
			return ErrInvalidTicket.WithDetail("lô DIRECT không cân ở hub")
		}
		e := store.SessionEvent{ID: ids.New(), Action: "open", EventTime: at, ActorRef: p.ActorRef, IdemKey: in.IdemKey,
			Session: port.Session{ID: id, LotRef: in.LotRef, Point: dev.Point, Route: port.RouteHub, FacilityRef: dev.FacilityRef,
				Status: port.SessionOpen, Window: port.Window{DeviceID: in.DeviceID, Epoch: in.Epoch, CounterFrom: in.CounterFrom}}}
		if err := insert(store.InsertSessionEvent(ctx, tx, p.TenantID, &e)); err != nil {
			return err
		}
		if err := store.UpsertSessionOpen(ctx, tx, p.TenantID, e); err != nil {
			return mapErr(err)
		}
		cur, err := store.GetSession(ctx, tx, id, false)
		if cur != nil {
			out = *cur
		}
		return err
	}, func(tx pgx.Tx) (bool, error) { return s.sessionReplay(ctx, tx, in.IdemKey, &out) })
	return out, replayed, err
}

func (s *Service) sessionReplay(ctx context.Context, tx pgx.Tx, key string, out *port.Session) (bool, error) {
	sid, err := store.SessionEventByIdem(ctx, tx, key)
	if err != nil || sid == nil {
		return false, err
	}
	cur, err := store.GetSession(ctx, tx, *sid, false)
	if cur != nil {
		*out = *cur
	}
	return true, err
}

// CloseSession đóng cửa sổ lô tại counter_to (scale_supervisor). Ingest đọc cửa
// sổ đã đóng qua port.WeighingSessions để tích phân (R-70).
func (s *Service) CloseSession(ctx context.Context, id uuid.UUID, counterTo int64, moisturePct string, tare *mass.Kg, at time.Time, idem string) (out port.Session, replayed bool, err error) {
	p, err := authz.Require(ctx, port.RoleScaleSupervisor)
	if err != nil {
		return out, false, err
	}
	if err := requireIdem(idem); err != nil {
		return out, false, err
	}
	if at.IsZero() {
		at = s.Clock.Now()
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		cur, err := store.GetSession(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if cur == nil {
			return ErrSessionNotFound
		}
		if cur.Status != port.SessionOpen {
			return ErrSessionClosed
		}
		if counterTo < cur.Window.CounterFrom {
			return problem.Validation.WithField("counter_to", "phải ≥ counter_from")
		}
		sess := *cur
		sess.Window.CounterTo = counterTo
		sess.MoisturePct, sess.TareKg = moisturePct, tare
		e := store.SessionEvent{ID: ids.New(), Action: "close", Session: sess, EventTime: at, ActorRef: p.ActorRef, IdemKey: idem}
		if err := insert(store.InsertSessionEvent(ctx, tx, p.TenantID, &e)); err != nil {
			return err
		}
		if err := store.CloseSession(ctx, tx, e); err != nil {
			return err
		}
		c, err := store.GetSession(ctx, tx, id, false)
		if c != nil {
			out = *c
		}
		return err
	}, func(tx pgx.Tx) (bool, error) { return s.sessionReplay(ctx, tx, idem, &out) })
	return out, replayed, err
}

// GetSession đọc phiên cân.
func (s *Service) GetSession(ctx context.Context, id uuid.UUID) (port.Session, error) {
	var out port.Session
	err := s.read(ctx, func(tx pgx.Tx) error {
		c, err := store.GetSession(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if c == nil {
			return ErrSessionNotFound
		}
		out = *c
		return nil
	})
	return out, err
}
