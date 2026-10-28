// Package store: SQL hồ sơ KN riêng pf_extension.kn_machine_year / kn_plot_crop (migration
// 20261025171651_pf_extension_kn_attrs) trên vai r_extension, luôn trong giao dịch có ngữ cảnh tenant (RLS). Sổ chỉ ghi
// thêm (shared.apply_ledger); bản hiện hành = dòng có seq lớn nhất theo khóa.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knattrs/domain"
)

// Meta là phần chung của một dòng sổ.
type Meta struct {
	ID            uuid.UUID
	EventTime     time.Time
	RecordedAt    time.Time
	ActorRef      uuid.UUID
	OnBehalfOf    *uuid.UUID
	SupportReason *string
	IdemKey       string
}

// ---------------------------------------------------------------------------
// Năm sản xuất máy (TT-10)
// ---------------------------------------------------------------------------

// MachineYear là một dòng sổ kn_machine_year.
type MachineYear struct {
	Meta
	CooperativeRef uuid.UUID
	CommuneCode    string
	Department     string
	MachineType    string
	TypeName       string
	Event          string
	YearMade       *int
}

func machineStream(dept string, coop uuid.UUID) string {
	return "kn_machine_year:" + dept + ":" + coop.String()
}

// LockMachines tuần tự hóa các lần thay hồ sơ năm máy của một HTX trong một phòng ban.
func LockMachines(ctx context.Context, tx pgx.Tx, dept string, coop uuid.UUID) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_extension.'||$1::text, 0))", machineStream(dept, coop))
	return err
}

// CurrentMachines trả dòng mới nhất (set) theo loại máy; loại đã removed không còn trong kết quả.
func CurrentMachines(ctx context.Context, tx pgx.Tx, dept string, coop uuid.UUID) ([]MachineYear, error) {
	rows, err := tx.Query(ctx, `SELECT id, cooperative_ref, commune_code, department_code, machine_type, machine_type_name, event,
	    year_made, event_time, recorded_at, actor_ref, on_behalf_of, support_reason, idem_key
	  FROM (SELECT DISTINCT ON (machine_type) * FROM pf_extension.kn_machine_year
	        WHERE department_code = $1::text AND cooperative_ref = $2::uuid
	        ORDER BY machine_type, seq DESC) m
	  WHERE m.event = 'set'
	  ORDER BY machine_type`, dept, coop)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MachineYear{}
	for rows.Next() {
		var m MachineYear
		if err := rows.Scan(&m.ID, &m.CooperativeRef, &m.CommuneCode, &m.Department, &m.MachineType, &m.TypeName, &m.Event, &m.YearMade,
			&m.EventTime, &m.RecordedAt, &m.ActorRef, &m.OnBehalfOf, &m.SupportReason, &m.IdemKey); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// InsertMachine ghi một dòng sổ năm máy.
func InsertMachine(ctx context.Context, tx pgx.Tx, tid uuid.UUID, m *MachineYear) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.kn_machine_year (id, tenant_id, stream_key, cooperative_ref, commune_code,
	    department_code, machine_type, machine_type_name, event, year_made, event_time, actor_ref, on_behalf_of, support_reason,
	    idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::text, $6::text, $7::text, $8::text, $9::text, $10::int, $11::timestamptz,
	    $12::uuid, $13::uuid, $14::text, $15::text, $16::text)
	  RETURNING recorded_at`,
		m.ID, tid, machineStream(m.Department, m.CooperativeRef), m.CooperativeRef, m.CommuneCode, m.Department, m.MachineType, m.TypeName,
		m.Event, m.YearMade, m.EventTime.UTC(), m.ActorRef, m.OnBehalfOf, m.SupportReason, m.IdemKey, domain.RuleVersion).Scan(&m.RecordedAt)
}

// ---------------------------------------------------------------------------
// Cây trồng thửa (TT-11)
// ---------------------------------------------------------------------------

// PlotCrop là một dòng sổ kn_plot_crop.
type PlotCrop struct {
	Meta
	PlotRef     uuid.UUID
	CommuneCode string
	Department  string
	CropCode    string
	CropName    string
}

func cropStream(dept string, plot uuid.UUID) string {
	return "kn_plot_crop:" + dept + ":" + plot.String()
}

const cropCols = `id, plot_ref, commune_code, department_code, crop_code, crop_name, event_time, recorded_at, actor_ref, on_behalf_of,
  support_reason, idem_key`

func scanCrop(row pgx.Row) (PlotCrop, error) {
	var c PlotCrop
	err := row.Scan(&c.ID, &c.PlotRef, &c.CommuneCode, &c.Department, &c.CropCode, &c.CropName, &c.EventTime, &c.RecordedAt, &c.ActorRef,
		&c.OnBehalfOf, &c.SupportReason, &c.IdemKey)
	return c, err
}

// LockCrop tuần tự hóa các lần đặt cây trồng của một thửa trong một phòng ban.
func LockCrop(ctx context.Context, tx pgx.Tx, dept string, plot uuid.UUID) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_extension.'||$1::text, 0))", cropStream(dept, plot))
	return err
}

// CurrentCrop trả cây trồng hiện hành của thửa; chưa có → nil.
func CurrentCrop(ctx context.Context, tx pgx.Tx, dept string, plot uuid.UUID) (*PlotCrop, error) {
	c, err := scanCrop(tx.QueryRow(ctx, `SELECT `+cropCols+` FROM pf_extension.kn_plot_crop
	  WHERE department_code = $1::text AND plot_ref = $2::uuid ORDER BY seq DESC LIMIT 1`, dept, plot))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// InsertCrop ghi một dòng sổ cây trồng (event set).
func InsertCrop(ctx context.Context, tx pgx.Tx, tid uuid.UUID, c *PlotCrop) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.kn_plot_crop (id, tenant_id, stream_key, plot_ref, commune_code, department_code,
	    event, crop_code, crop_name, event_time, actor_ref, on_behalf_of, support_reason, idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::text, $6::text, 'set', $7::text, $8::text, $9::timestamptz, $10::uuid,
	    $11::uuid, $12::text, $13::text, $14::text)
	  RETURNING recorded_at`,
		c.ID, tid, cropStream(c.Department, c.PlotRef), c.PlotRef, c.CommuneCode, c.Department, c.CropCode, c.CropName, c.EventTime.UTC(),
		c.ActorRef, c.OnBehalfOf, c.SupportReason, c.IdemKey, domain.RuleVersion).Scan(&c.RecordedAt)
}

// CropFilter là điều kiện đọc danh sách cây trồng hiện hành.
type CropFilter struct {
	All     bool
	Units   []string // xã trong phạm vi
	Commune string
	Dept    string // rỗng = mọi phòng ban (Admin)
	After   uuid.UUID
	Limit   int
}

// ListCrops trả cây trồng hiện hành theo (phòng ban, thửa) trong phạm vi, theo plot_ref tăng dần.
func ListCrops(ctx context.Context, tx pgx.Tx, f CropFilter) ([]PlotCrop, error) {
	units := f.Units
	if units == nil {
		units = []string{}
	}
	rows, err := tx.Query(ctx, `SELECT `+cropCols+` FROM (
	    SELECT DISTINCT ON (department_code, plot_ref) * FROM pf_extension.kn_plot_crop
	    WHERE ($1::boolean OR commune_code = ANY($2::text[]))
	      AND ($3::text = '' OR commune_code = $3::text)
	      AND ($4::text = '' OR department_code = $4::text)
	      AND plot_ref > $5::uuid
	    ORDER BY department_code, plot_ref, seq DESC) c
	  ORDER BY plot_ref, department_code LIMIT $6::int`, f.All, units, f.Commune, f.Dept, f.After, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PlotCrop{}
	for rows.Next() {
		c, err := scanCrop(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
