// Package store: truy cập DB của pf/masterdata, chỉ qua pool của vai
// masterdata_app (R-05). Mọi hàm nhận pgx.Tx đã ở ngữ cảnh tenant
// (tenant.WithTenantTx).
//
// Ngày (cột date) truyền dạng chuỗi YYYY-MM-DD kèm ép kiểu ::date và số thập phân
// truyền dạng chuỗi kèm ::numeric, để chạy đúng ở chế độ QueryExecModeExec của
// PgBouncer (tham số văn bản, không suy kiểu từ prepared statement).
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/domain"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

func day(t time.Time) string { return t.Format(time.DateOnly) }

func dayPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := day(*t)
	return &s
}

const unitCols = `code, valid_from, valid_to, name, level, coalesce(parent_code, ''), successor_codes`

// validAt là điều kiện "có hiệu lực trong ngày $n".
func validAt(alias, param string) string {
	return alias + ".valid_from <= " + param + "::date AND (" + alias + ".valid_to IS NULL OR " + alias + ".valid_to > " + param + "::date)"
}

func scanUnit(row pgx.Row) (domain.Unit, error) {
	var u domain.Unit
	var level string
	err := row.Scan(&u.Code, &u.ValidFrom, &u.ValidTo, &u.Name, &level, &u.ParentCode, &u.Successors)
	u.Level = partyport.Level(level)
	return u, err
}

func oneUnit(row pgx.Row) (*domain.Unit, error) {
	u, err := scanUnit(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func manyUnits(rows pgx.Rows, err error) ([]domain.Unit, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Unit
	for rows.Next() {
		u, err := scanUnit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UnitAt đọc phiên bản của mã code có hiệu lực trong ngày d.
func UnitAt(ctx context.Context, tx pgx.Tx, code string, d time.Time) (*domain.Unit, error) {
	return oneUnit(tx.QueryRow(ctx, "SELECT "+unitCols+" FROM pf_masterdata.admin_unit u WHERE u.code = $1 AND "+validAt("u", "$2"), code, day(d)))
}

// UnitLatestFrom đọc phiên bản mới nhất của mã code bắt đầu không muộn hơn ngày d.
func UnitLatestFrom(ctx context.Context, tx pgx.Tx, code string, d time.Time) (*domain.Unit, error) {
	return oneUnit(tx.QueryRow(ctx, "SELECT "+unitCols+` FROM pf_masterdata.admin_unit
	    WHERE code = $1 AND valid_from <= $2::date ORDER BY valid_from DESC LIMIT 1`, code, day(d)))
}

// AllUnits liệt kê mọi phiên bản đơn vị hành chính của tenant (bản chiếu k_ref.admin_unit, W2.5).
func AllUnits(ctx context.Context, tx pgx.Tx) ([]domain.Unit, error) {
	return manyUnits(tx.Query(ctx, "SELECT "+unitCols+" FROM pf_masterdata.admin_unit ORDER BY code, valid_from"))
}

// UnitVersion đọc đúng phiên bản (code, valid_from) và khóa dòng.
func UnitVersion(ctx context.Context, tx pgx.Tx, code string, from time.Time) (*domain.Unit, error) {
	return oneUnit(tx.QueryRow(ctx, "SELECT "+unitCols+` FROM pf_masterdata.admin_unit
	    WHERE code = $1 AND valid_from = $2::date FOR UPDATE`, code, day(from)))
}

// LockImport tuần tự hóa các lần nhập danh mục của cùng tenant.
func LockImport(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_masterdata.import:' || $1::text, 0))", tenantID.String())
	return err
}

// InsertUnit ghi một phiên bản mới.
func InsertUnit(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, u domain.Unit, source string, actor uuid.UUID) error {
	var parent *string
	if u.ParentCode != "" {
		parent = &u.ParentCode
	}
	succ := u.Successors
	if succ == nil {
		succ = []string{}
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_masterdata.admin_unit
	    (tenant_id, code, valid_from, valid_to, name, level, parent_code, successor_codes, source, actor_ref)
	  VALUES ($1, $2, $3::date, $4::date, $5, $6, $7, $8::text[], $9, $10)`,
		tenantID, u.Code, day(u.ValidFrom), dayPtr(u.ValidTo), u.Name, string(u.Level), parent, succ, source, actor)
	return err
}

// CloseUnit đóng hiệu lực một phiên bản đang mở (trigger chặn mọi thay đổi khác).
func CloseUnit(ctx context.Context, tx pgx.Tx, u domain.Unit, actor uuid.UUID, now time.Time) error {
	succ := u.Successors
	if succ == nil {
		succ = []string{}
	}
	_, err := tx.Exec(ctx, `UPDATE pf_masterdata.admin_unit
	    SET valid_to = $3::date, successor_codes = $4::text[], closed_by = $5, closed_at = $6
	  WHERE code = $1 AND valid_from = $2::date AND valid_to IS NULL`,
		u.Code, day(u.ValidFrom), dayPtr(u.ValidTo), succ, actor, now.UTC())
	return err
}

// ParentCovers kiểm đơn vị cha của u tồn tại, đúng cấp và bao trọn khoảng hiệu lực của u.
func ParentCovers(ctx context.Context, tx pgx.Tx, u domain.Unit) (bool, error) {
	levels := []string{string(partyport.LevelProvince)}
	if u.Level == partyport.LevelCommune {
		levels = append(levels, string(partyport.LevelDistrict))
	}
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pf_masterdata.admin_unit p
	    WHERE p.code = $1 AND p.level = ANY($2::text[]) AND p.valid_from <= $3::date
	      AND (p.valid_to IS NULL OR ($4::date IS NOT NULL AND p.valid_to >= $4::date)))`,
		u.ParentCode, levels, day(u.ValidFrom), dayPtr(u.ValidTo)).Scan(&ok)
	return ok, err
}

// Children liệt kê đơn vị con trực tiếp có hiệu lực trong ngày d, theo mã, sau
// mã after (cursor). parent rỗng → danh sách tỉnh.
func Children(ctx context.Context, tx pgx.Tx, parent string, d time.Time, after string, limit int) ([]domain.Unit, error) {
	if parent == "" {
		return manyUnits(tx.Query(ctx, "SELECT "+unitCols+" FROM pf_masterdata.admin_unit u WHERE u.level = 'province' AND "+
			validAt("u", "$1")+" AND u.code > $2 ORDER BY u.code LIMIT $3", day(d), after, limit))
	}
	return manyUnits(tx.Query(ctx, "SELECT "+unitCols+" FROM pf_masterdata.admin_unit u WHERE u.parent_code = $1 AND "+
		validAt("u", "$2")+" AND u.code > $3 ORDER BY u.code LIMIT $4", parent, day(d), after, limit))
}

// Descendants trả đơn vị code và mọi đơn vị con cháu có hiệu lực trong ngày d
// (gốc đứng đầu, còn lại theo mã). Rỗng nếu code không có hiệu lực tại d.
func Descendants(ctx context.Context, tx pgx.Tx, code string, d time.Time) ([]domain.Unit, error) {
	return manyUnits(tx.Query(ctx, `WITH RECURSIVE t AS (
	    SELECT u.code, 0 AS depth FROM pf_masterdata.admin_unit u WHERE u.code = $1 AND `+validAt("u", "$2")+`
	    UNION
	    SELECT c.code, t.depth + 1 FROM pf_masterdata.admin_unit c JOIN t ON c.parent_code = t.code
	     WHERE `+validAt("c", "$2")+` AND t.depth < 8)
	  SELECT `+unitCols+` FROM pf_masterdata.admin_unit u JOIN t USING (code)
	   WHERE `+validAt("u", "$2")+` ORDER BY t.depth > 0, u.code`, code, day(d)))
}
