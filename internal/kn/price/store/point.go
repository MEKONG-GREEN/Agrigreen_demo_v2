package store

// SQL của xã báo giá + phân công (W11.72, pf 000145): sổ pf_price.region_group, collection_point, point_officer; đọc
// bảng chiếu ĐVHC k_ref.admin_unit (ADR-0029, ADR-0032 — r_price SELECT từ pf 000138). Đọc cả sổ của tenant rồi chiếu ở
// domain (BRD: 26 xã × vài phiên bản).

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
)

// LoadEntries đọc mọi phiên bản của một danh mục nền (đơn vị tính, nhóm hàng, nhóm vùng) của tenant.
func LoadEntries(ctx context.Context, tx pgx.Tx, k domain.Kind) ([]domain.Ver[domain.Entry], error) {
	t, err := table(k)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT code, seq, name, sort_order, status, valid_from FROM `+t+` ORDER BY code, seq`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.Ver[domain.Entry], error) {
		var v domain.Ver[domain.Entry]
		v.V.Kind = k
		err := r.Scan(&v.V.Code, &v.Seq, &v.V.Name, &v.V.SortOrder, &v.Status, &v.ValidFrom)
		v.Key, v.V.ValidFrom = v.V.Code, v.ValidFrom
		return v, err
	})
}

// InsertEntryVersion ghi một phiên bản danh mục nền với trạng thái cho trước (active: thêm/sửa; retired: ngưng).
func InsertEntryVersion(ctx context.Context, tx pgx.Tx, id, tid uuid.UUID, e domain.Entry, status string, at time.Time, actor uuid.UUID,
	idem string) (int64, error) {
	t, err := table(e.Kind)
	if err != nil {
		return 0, err
	}
	var seq int64
	err = tx.QueryRow(ctx, `INSERT INTO `+t+` (id, tenant_id, stream_key, code, name, sort_order, status, valid_from, event_time,
	    actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3 || ':' || $4::text, $4::text, $5, $6, $7, $8::date, $9, $10, $11, $12) RETURNING seq`,
		id, tid, string(e.Kind), e.Code, e.Name, e.SortOrder, status, dateOnly(e.ValidFrom), at.UTC(), actor, idem,
		domain.RuleVersion).Scan(&seq)
	return seq, err
}

// PointVersion là một phiên bản xã báo giá kèm thông tin ghi (lịch sử thay đổi).
type PointVersion struct {
	domain.Ver[domain.Point]
	Meta
}

// LoadPoints đọc mọi phiên bản xã báo giá của tenant (RLS), theo mã rồi seq.
func LoadPoints(ctx context.Context, tx pgx.Tx) ([]PointVersion, error) {
	rows, err := tx.Query(ctx, `SELECT id, code, seq, admin_unit_code, name, region_code, status, valid_from, coalesce(note, ''),
	    event_time, actor_ref
	  FROM pf_price.collection_point ORDER BY code, seq`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (PointVersion, error) {
		var v PointVersion
		p := &v.V
		err := r.Scan(&v.ID, &p.Code, &v.Seq, &p.AdminUnitCode, &p.Name, &p.RegionCode, &p.Status, &p.ValidFrom, &p.Note,
			&v.EventTime, &v.ActorRef)
		v.Key, v.Status, v.ValidFrom = p.Code, p.Status, p.ValidFrom
		return v, err
	})
}

// NextPointNumber khóa bộ sinh mã xã báo giá của tenant đến hết giao dịch và trả số kế tiếp (mã không tái sử dụng — BR-01).
// Khóa cũng tuần tự hóa kiểm "mã ĐVHC đã gắn với xã khác" giữa hai lần ghi đồng thời.
func NextPointNumber(ctx context.Context, tx pgx.Tx, tid uuid.UUID) (int, error) {
	if err := LockPoints(ctx, tx, tid); err != nil {
		return 0, err
	}
	var n int
	err := tx.QueryRow(ctx, `SELECT coalesce(max(substr(code, 4)::int), 0) + 1 FROM pf_price.collection_point`).Scan(&n)
	return n, err
}

// LockPoints khóa sổ xã báo giá của tenant đến hết giao dịch (thêm/sửa/ngưng xã kiểm trùng mã ĐVHC trên cùng ảnh sổ).
func LockPoints(ctx context.Context, tx pgx.Tx, tid uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pf_price.collection_point:' || $1::text, 0))`, tid)
	return err
}

// InsertPoint ghi một phiên bản xã báo giá; seq, chuỗi băm do trigger fn_chain đặt.
func InsertPoint(ctx context.Context, tx pgx.Tx, id, tid uuid.UUID, p domain.Point, at time.Time, actor uuid.UUID, idem string) (int64, error) {
	var seq int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_price.collection_point (id, tenant_id, stream_key, code, admin_unit_code, name, region_code,
	    status, valid_from, note, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'point:' || $3::text, $3::text, $4, $5, $6, $7, $8::date, $9, $10, $11, $12, $13) RETURNING seq`,
		id, tid, p.Code, p.AdminUnitCode, p.Name, p.RegionCode, p.Status, dateOnly(p.ValidFrom), nullText(p.Note), at.UTC(), actor, idem,
		domain.RuleVersion).Scan(&seq)
	return seq, err
}

// OfficerVersion là một phiên bản phân công kèm thông tin ghi.
type OfficerVersion struct {
	domain.Ver[domain.OfficerAssign]
	Meta
}

// LoadOfficers đọc mọi phiên bản phân công người phụ trách xã của tenant.
func LoadOfficers(ctx context.Context, tx pgx.Tx) ([]OfficerVersion, error) {
	rows, err := tx.Query(ctx, `SELECT id, point_code, role, seq, officer_ref, valid_from, event_time, actor_ref
	  FROM pf_price.point_officer ORDER BY point_code, role, seq`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (OfficerVersion, error) {
		var (
			v   OfficerVersion
			ref *uuid.UUID
		)
		o := &v.V
		err := r.Scan(&v.ID, &o.PointCode, &o.Role, &v.Seq, &ref, &v.ValidFrom, &v.EventTime, &v.ActorRef)
		if ref != nil {
			o.OfficerRef = *ref
		}
		v.Key, v.Status = domain.OfficerKey(o.Role, o.PointCode), domain.StatusActive
		return v, err
	})
}

// InsertOfficer ghi một phiên bản phân công (ref Nil = bỏ phân công).
func InsertOfficer(ctx context.Context, tx pgx.Tx, id, tid uuid.UUID, o domain.OfficerAssign, validFrom, at time.Time, actor uuid.UUID,
	idem string) (int64, error) {
	var ref any
	if o.OfficerRef != uuid.Nil {
		ref = o.OfficerRef
	}
	var seq int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_price.point_officer (id, tenant_id, stream_key, point_code, role, officer_ref, valid_from,
	    event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3::text || ':' || $4::text, $4::text, $3::text, $5, $6::date, $7, $8, $9, $10) RETURNING seq`,
		id, tid, o.Role, o.PointCode, ref, dateOnly(validFrom), at.UTC(), actor, idem, domain.RuleVersion).Scan(&seq)
	return seq, err
}

// AdminUnit là một phiên bản ĐVHC của bảng chiếu k_ref.admin_unit (ADR-0032: valid_to không bao gồm, NULL = còn hiệu lực).
type AdminUnit struct {
	Code       string
	Name       string
	Level      string
	ParentCode string
	ValidFrom  time.Time
	ValidTo    *time.Time
	Successors []string
}

// EffectiveAt cho biết phiên bản có hiệu lực tại ngày d.
func (u AdminUnit) EffectiveAt(d time.Time) bool {
	return !u.ValidFrom.After(d) && (u.ValidTo == nil || u.ValidTo.After(d))
}

// LoadAdminUnits đọc mọi phiên bản ĐVHC cấp level (rỗng = mọi cấp) của tenant từ bảng chiếu k_ref.admin_unit.
func LoadAdminUnits(ctx context.Context, tx pgx.Tx, level string) ([]AdminUnit, error) {
	rows, err := tx.Query(ctx, `SELECT code, name, level, coalesce(parent_code, ''), valid_from, valid_to, successor_codes
	  FROM k_ref.admin_unit WHERE ($1 = '' OR level = $1) ORDER BY code, valid_from`, level)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (AdminUnit, error) {
		var u AdminUnit
		err := r.Scan(&u.Code, &u.Name, &u.Level, &u.ParentCode, &u.ValidFrom, &u.ValidTo, &u.Successors)
		return u, err
	})
}
