package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
)

// DeviceStream là khóa chuỗi của thiết bị.
func DeviceStream(deviceID string) string { return "device:" + deviceID }

// DirectoryTenant trả tenant của thiết bị trong thư mục (không cần ngữ cảnh tenant).
func DirectoryTenant(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, deviceID string) (uuid.UUID, bool, error) {
	var tid uuid.UUID
	err := q.QueryRow(ctx, "SELECT tenant_id FROM mkg_measurement.device_directory WHERE device_id = $1", deviceID).Scan(&tid)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	return tid, err == nil, err
}

// EnsureDirectory ghi device_id → tenant nếu chưa có; trả false khi device_id đã thuộc tenant khác.
func EnsureDirectory(ctx context.Context, tx pgx.Tx, tid uuid.UUID, deviceID string) (bool, error) {
	if _, err := tx.Exec(ctx, `INSERT INTO mkg_measurement.device_directory (device_id, tenant_id) VALUES ($1, $2)
	  ON CONFLICT (device_id) DO NOTHING`, deviceID, tid); err != nil {
		return false, err
	}
	owner, _, err := DirectoryTenant(ctx, tx, deviceID)
	return owner == tid, err
}

// DeviceVersion là một dòng sổ device_version.
type DeviceVersion struct {
	ID          uuid.UUID
	Seq         int64
	DeviceID    string
	Epoch       int
	Action      string
	FacilityRef *uuid.UUID
	Point       *string
	PublicKey   []byte
	KeyID       *string
	CertSHA256  *string
	ScaleModel  *string
	Reason      *string
	EventTime   time.Time
	RecordedAt  time.Time
	ActorRef    uuid.UUID
	IdemKey     string
}

// InsertDeviceVersion ghi đề xuất đăng ký / thu hồi.
func InsertDeviceVersion(ctx context.Context, tx pgx.Tx, tid uuid.UUID, v *DeviceVersion) error {
	return tx.QueryRow(ctx, `INSERT INTO mkg_measurement.device_version
	    (id, tenant_id, stream_key, scale_device_id, epoch, action, facility_ref, point, public_key, key_id, cert_sha256, scale_model,
	     reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17) RETURNING seq, recorded_at`,
		v.ID, tid, DeviceStream(v.DeviceID), v.DeviceID, v.Epoch, v.Action, v.FacilityRef, v.Point, v.PublicKey, v.KeyID,
		v.CertSHA256, v.ScaleModel, v.Reason, v.EventTime.UTC(), v.ActorRef, v.IdemKey, domain.RuleDevice).Scan(&v.Seq, &v.RecordedAt)
}

// DeviceVersionByIdem tìm theo idem_key.
func DeviceVersionByIdem(ctx context.Context, tx pgx.Tx, key string) (*DeviceVersion, error) {
	var v DeviceVersion
	err := tx.QueryRow(ctx, `SELECT id, seq, scale_device_id, epoch, action, facility_ref, point, public_key, key_id, cert_sha256,
	    scale_model, reason, event_time, recorded_at, actor_ref, idem_key FROM mkg_measurement.device_version WHERE idem_key = $1`, key).
		Scan(&v.ID, &v.Seq, &v.DeviceID, &v.Epoch, &v.Action, &v.FacilityRef, &v.Point, &v.PublicKey, &v.KeyID, &v.CertSHA256,
			&v.ScaleModel, &v.Reason, &v.EventTime, &v.RecordedAt, &v.ActorRef, &v.IdemKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// MaxEpoch trả kỳ lớn nhất đã đề xuất của thiết bị (0 khi chưa có) — kỳ không dùng lại.
func MaxEpoch(ctx context.Context, tx pgx.Tx, deviceID string) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT coalesce(max(epoch), 0) FROM mkg_measurement.device_version WHERE stream_key = $1`,
		DeviceStream(deviceID)).Scan(&n)
	return n, err
}

// InsertDeviceDecision ghi quyết định duyệt (DB CHECK actor ≠ proposer, R-56).
func InsertDeviceDecision(ctx context.Context, tx pgx.Tx, tid, id uuid.UUID, deviceID string, epoch int, versionID uuid.UUID,
	decision string, reason *string, proposer, actor uuid.UUID, at time.Time, idem string) (int64, error) {
	var seq int64
	err := tx.QueryRow(ctx, `INSERT INTO mkg_measurement.device_decision
	    (id, tenant_id, stream_key, scale_device_id, epoch, version_id, decision, reason, proposer_ref, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING seq`,
		id, tid, DeviceStream(deviceID), deviceID, epoch, versionID, decision, reason, proposer, at.UTC(), actor, idem, domain.RuleDevice).Scan(&seq)
	return seq, err
}

// DecisionByIdem trả (có, trạng thái sau) của một quyết định đã ghi theo idem_key.
func DecisionByIdem(ctx context.Context, tx pgx.Tx, key string) (bool, error) {
	var n int
	err := tx.QueryRow(ctx, "SELECT count(*) FROM mkg_measurement.device_decision WHERE idem_key = $1", key).Scan(&n)
	return n > 0, err
}

// DeviceRow là một dòng device_current.
type DeviceRow struct {
	port.Device
	VersionID       uuid.UUID
	ProposedBy      uuid.UUID
	ProposedAt      time.Time
	DecidedBy       *uuid.UUID
	DecidedAt       *time.Time
	RevokedAt       *time.Time
	RevokeReason    *string
	LastSeenAt      *time.Time
	BufferPct       *string
	ClockSkewMs     *int64
	LastCounter     *int64
	HealthUpdatedAt *time.Time
	RawStatus       string // pending | active | rejected | revoked | superseded
}

const deviceCols = `tenant_id, device_id, epoch, facility_ref, point, public_key, key_id, cert_sha256, scale_model, status, version_id,
  proposed_by, proposed_at, decided_by, decided_at, revoked_at, revoke_reason, last_seen_at, buffer_pct::text, clock_skew_ms,
  last_counter, health_updated_at`

func scanDevice(row pgx.Row) (DeviceRow, error) {
	var d DeviceRow
	var point string
	var pub []byte
	err := row.Scan(&d.TenantID, &d.DeviceID, &d.Epoch, &d.FacilityRef, &point, &pub, &d.KeyID, &d.CertSHA256, &d.ScaleModel,
		&d.RawStatus, &d.VersionID, &d.ProposedBy, &d.ProposedAt, &d.DecidedBy, &d.DecidedAt, &d.RevokedAt, &d.RevokeReason,
		&d.LastSeenAt, &d.BufferPct, &d.ClockSkewMs, &d.LastCounter, &d.HealthUpdatedAt)
	d.Point, d.PublicKey = port.WeighPoint(point), pub
	d.Status = port.DeviceStatus(d.RawStatus)
	if d.DecidedAt != nil {
		d.ApprovedAt = *d.DecidedAt
	}
	return d, err
}

// GetDevice đọc một kỳ đăng ký (nil khi không có).
func GetDevice(ctx context.Context, tx pgx.Tx, deviceID string, epoch int, forUpdate bool) (*DeviceRow, error) {
	q := "SELECT " + deviceCols + " FROM mkg_measurement.device_current WHERE device_id = $1 AND epoch = $2"
	if forUpdate {
		q += " FOR UPDATE"
	}
	d, err := scanDevice(tx.QueryRow(ctx, q, deviceID, epoch))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// DevicesOf liệt kê mọi kỳ của thiết bị (mới nhất trước).
func DevicesOf(ctx context.Context, tx pgx.Tx, deviceID string) ([]DeviceRow, error) {
	return listDevices(ctx, tx, "SELECT "+deviceCols+" FROM mkg_measurement.device_current WHERE device_id = $1 ORDER BY epoch DESC", deviceID)
}

// ListDevices liệt kê thiết bị theo (device_id, epoch) sau con trỏ.
func ListDevices(ctx context.Context, tx pgx.Tx, afterDevice string, afterEpoch, limit int) ([]DeviceRow, error) {
	return listDevices(ctx, tx, "SELECT "+deviceCols+` FROM mkg_measurement.device_current
	  WHERE (device_id, epoch) > ($1, $2) ORDER BY device_id, epoch LIMIT $3`, afterDevice, afterEpoch, limit)
}

func listDevices(ctx context.Context, tx pgx.Tx, q string, args ...any) ([]DeviceRow, error) {
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeviceRow
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// InsertPendingDevice thêm đề xuất đăng ký vào bảng chiếu.
func InsertPendingDevice(ctx context.Context, tx pgx.Tx, tid uuid.UUID, v DeviceVersion) error {
	_, err := tx.Exec(ctx, `INSERT INTO mkg_measurement.device_current
	    (tenant_id, device_id, epoch, facility_ref, point, public_key, key_id, cert_sha256, scale_model, status, version_id, proposed_by, proposed_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'pending', $10, $11, $12)`,
		tid, v.DeviceID, v.Epoch, v.FacilityRef, v.Point, v.PublicKey, v.KeyID, v.CertSHA256, v.ScaleModel, v.ID, v.ActorRef, v.RecordedAt)
	return err
}

// SetDeviceStatus đổi trạng thái kỳ đăng ký trong bảng chiếu.
func SetDeviceStatus(ctx context.Context, tx pgx.Tx, deviceID string, epoch int, status string, by *uuid.UUID, at time.Time, reason *string) error {
	switch status {
	case "active", "rejected":
		_, err := tx.Exec(ctx, `UPDATE mkg_measurement.device_current SET status = $3, decided_by = $4, decided_at = $5, updated_at = now()
		  WHERE device_id = $1 AND epoch = $2`, deviceID, epoch, status, by, at.UTC())
		return err
	case "revoked":
		_, err := tx.Exec(ctx, `UPDATE mkg_measurement.device_current SET status = 'revoked', revoked_at = $3, revoke_reason = $4, updated_at = now()
		  WHERE device_id = $1 AND epoch = $2`, deviceID, epoch, at.UTC(), reason)
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE mkg_measurement.device_current SET status = $3, updated_at = now() WHERE device_id = $1 AND epoch = $2`,
		deviceID, epoch, status)
	return err
}

// SupersedeOlderEpochs đánh dấu các kỳ đang hiệu lực cũ hơn là superseded.
func SupersedeOlderEpochs(ctx context.Context, tx pgx.Tx, deviceID string, epoch int) error {
	_, err := tx.Exec(ctx, `UPDATE mkg_measurement.device_current SET status = 'superseded', updated_at = now()
	  WHERE device_id = $1 AND epoch < $2 AND status = 'active'`, deviceID, epoch)
	return err
}

// NewerActiveOrPending cho biết có kỳ mới hơn đã duyệt (active/revoked/superseded) hay không.
func NewerEpochApproved(ctx context.Context, tx pgx.Tx, deviceID string, epoch int) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM mkg_measurement.device_current
	  WHERE device_id = $1 AND epoch > $2 AND status IN ('active', 'revoked', 'superseded'))`, deviceID, epoch).Scan(&ok)
	return ok, err
}

// UpdateHealth ghi sức khỏe (chỉ cột sức khỏe — quyền của ingest_mkg).
func UpdateHealth(ctx context.Context, tx pgx.Tx, h port.HealthReport) (bool, error) {
	tag, err := tx.Exec(ctx, `UPDATE mkg_measurement.device_current
	   SET last_seen_at = greatest(coalesce(last_seen_at, $3), $3), buffer_pct = $4::numeric, clock_skew_ms = $5,
	       last_counter = greatest(coalesce(last_counter, $6), $6), health_updated_at = now()
	 WHERE device_id = $1 AND epoch = $2`, h.DeviceID, h.Epoch, h.LastSeen.UTC(), strPtr(h.BufferPct), h.ClockSkewMs, h.LastCounter)
	return tag.RowsAffected() == 1, err
}

// ---------------------------------------------------------------------------
// Phiên cân
// ---------------------------------------------------------------------------

// SessionStream là khóa chuỗi phiên cân.
func SessionStream(id uuid.UUID) string { return "session:" + id.String() }

// SessionEvent là một dòng sổ weighing_session.
type SessionEvent struct {
	ID        uuid.UUID
	Session   port.Session
	Action    string
	EventTime time.Time
	ActorRef  uuid.UUID
	IdemKey   string
	Seq       int64
}

// InsertSessionEvent ghi mở/đóng phiên.
func InsertSessionEvent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, e *SessionEvent) error {
	s := e.Session
	var to *int64
	var moist, tare *string
	if e.Action == "close" {
		v := s.Window.CounterTo
		to, moist, tare = &v, strPtr(s.MoisturePct), kgArg(s.TareKg)
	}
	return tx.QueryRow(ctx, `INSERT INTO mkg_measurement.weighing_session
	    (id, tenant_id, stream_key, session_id, action, lot_ref, point, route, facility_ref, scale_device_id, epoch, counter_from,
	     counter_to, moisture_pct, tare_kg, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::numeric, $15::numeric, $16, $17, $18, $19) RETURNING seq`,
		e.ID, tid, SessionStream(s.ID), s.ID, e.Action, s.LotRef, string(s.Point), string(s.Route), s.FacilityRef, s.Window.DeviceID,
		s.Window.Epoch, s.Window.CounterFrom, to, moist, tare, e.EventTime.UTC(), e.ActorRef, e.IdemKey, domain.RuleSession).Scan(&e.Seq)
}

// SessionEventByIdem trả session_id của sự kiện phiên có idem_key.
func SessionEventByIdem(ctx context.Context, tx pgx.Tx, key string) (*uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, "SELECT session_id FROM mkg_measurement.weighing_session WHERE idem_key = $1", key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

const sessionCols = `session_id, tenant_id, lot_ref, point, route, facility_ref, device_id, epoch, counter_from, counter_to, status,
  opened_at, closed_at, closed_by, moisture_pct::text, tare_kg::text`

func scanSession(row pgx.Row) (port.Session, error) {
	var s port.Session
	var point, route, status string
	var to *int64
	var moist, tare *string
	err := row.Scan(&s.ID, &s.TenantID, &s.LotRef, &point, &route, &s.FacilityRef, &s.Window.DeviceID, &s.Window.Epoch, &s.Window.CounterFrom,
		&to, &status, &s.OpenedAt, &s.ClosedAt, &s.ClosedBy, &moist, &tare)
	if err != nil {
		return s, err
	}
	s.Point, s.Route, s.Status = port.WeighPoint(point), port.Route(route), port.SessionStatus(status)
	if to != nil {
		s.Window.CounterTo = *to
	}
	s.MoisturePct = deref(moist)
	s.TareKg, err = kgScan(tare)
	return s, err
}

// GetSession đọc phiên cân (nil khi không có).
func GetSession(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*port.Session, error) {
	q := "SELECT " + sessionCols + " FROM mkg_measurement.weighing_session_current WHERE session_id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	s, err := scanSession(tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// LastClosedCounter trả counter_to lớn nhất của các phiên đã đóng của (thiết bị, kỳ); -1 nếu chưa có.
func LastClosedCounter(ctx context.Context, tx pgx.Tx, deviceID string, epoch int) (int64, error) {
	var n int64
	err := tx.QueryRow(ctx, `SELECT coalesce(max(counter_to), -1) FROM mkg_measurement.weighing_session_current
	  WHERE device_id = $1 AND epoch = $2 AND status = 'closed'`, deviceID, epoch).Scan(&n)
	return n, err
}

// UpsertSessionOpen thêm phiên mở vào bảng chiếu.
func UpsertSessionOpen(ctx context.Context, tx pgx.Tx, tid uuid.UUID, e SessionEvent) error {
	s := e.Session
	_, err := tx.Exec(ctx, `INSERT INTO mkg_measurement.weighing_session_current
	    (tenant_id, session_id, lot_ref, point, route, facility_ref, device_id, epoch, counter_from, status, opened_at, opened_by, last_seq)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'open', $10, $11, $12)`,
		tid, s.ID, s.LotRef, string(s.Point), string(s.Route), s.FacilityRef, s.Window.DeviceID, s.Window.Epoch, s.Window.CounterFrom,
		e.EventTime.UTC(), e.ActorRef, e.Seq)
	return err
}

// CloseSession đóng phiên trong bảng chiếu.
func CloseSession(ctx context.Context, tx pgx.Tx, e SessionEvent) error {
	_, err := tx.Exec(ctx, `UPDATE mkg_measurement.weighing_session_current
	   SET status = 'closed', counter_to = $2, closed_at = $3, closed_by = $4, last_seq = $5, moisture_pct = $6::numeric,
	       tare_kg = $7::numeric, updated_at = now()
	 WHERE session_id = $1`, e.Session.ID, e.Session.Window.CounterTo, e.EventTime.UTC(), e.ActorRef, e.Seq,
		strPtr(e.Session.MoisturePct), kgArg(e.Session.TareKg))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO mkg_measurement.session_directory (session_id, tenant_id, closed_at) VALUES ($1, shared.current_tenant(), $2)`,
		e.Session.ID, e.EventTime.UTC())
	return err
}

// PendingTenants trả các tenant có phiên đã đóng (hàng xuyên tenant, không RLS).
func PendingTenants(ctx context.Context, tx pgx.Tx) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, "SELECT DISTINCT tenant_id FROM mkg_measurement.session_directory ORDER BY tenant_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// PendingSessions trả phiên đã đóng của tenant hiện tại mà lô CHƯA có phiếu hiệu
// lực ở điểm cân của phiên (cũ trước). Phiếu tay (cân hỏng) cũng làm phiên hết chờ.
func PendingSessions(ctx context.Context, tx pgx.Tx, limit int) ([]port.Session, error) {
	rows, err := tx.Query(ctx, "SELECT "+sessionColsQ("s")+` FROM mkg_measurement.weighing_session_current s
	  LEFT JOIN mkg_measurement.lot_mass_current m ON m.lot_ref = s.lot_ref
	  WHERE s.status = 'closed'
	    AND ((s.point = 'HUB_IN' AND m.hub_in_ticket IS NULL) OR (s.point = 'HUB_OUT' AND m.hub_out_ticket IS NULL))
	  ORDER BY s.closed_at, s.session_id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []port.Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func sessionColsQ(a string) string {
	cols := strings.Split(sessionCols, ",")
	for i, c := range cols {
		cols[i] = a + "." + strings.TrimSpace(c)
	}
	return strings.Join(cols, ", ")
}

// SessionByWindow tìm phiên đã đóng đúng cửa sổ (thiết bị, kỳ, bộ đếm).
func SessionByWindow(ctx context.Context, tx pgx.Tx, w port.Window) (*port.Session, error) {
	s, err := scanSession(tx.QueryRow(ctx, "SELECT "+sessionCols+` FROM mkg_measurement.weighing_session_current
	  WHERE device_id = $1 AND epoch = $2 AND counter_from = $3 AND counter_to = $4 AND status = 'closed'`,
		w.DeviceID, w.Epoch, w.CounterFrom, w.CounterTo))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}
