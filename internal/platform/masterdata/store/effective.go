package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
)

// Sổ phụ hiệu lực theo ngày của danh mục CNTS (W11.41, pf 000146, FN-06 BR-03).

// InsertEffective ghi một dòng sổ catalog_effective; DB tính seq, chuỗi băm, recorded_at.
func InsertEffective(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, v *domain.Version) error {
	after, err := json.Marshal(v.After)
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_masterdata.catalog_effective
	    (id, tenant_id, stream_key, catalog, code, change_id, valid_from, after, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7::date, $8::jsonb, $9, $10, $11, $12)
	  RETURNING seq, recorded_at`,
		v.ID, tenantID, domain.CatalogStreamKey(v.Catalog, v.Code), v.Catalog, v.Code, v.ChangeID, day(v.ValidFrom), string(after),
		v.EventTime.UTC(), v.ActorRef, v.IdemKey, domain.EffectiveRuleVersion).Scan(&v.Seq, &v.RecordedAt)
}

const effectiveCols = `id, catalog, code, change_id, valid_from, after::text, event_time, actor_ref, idem_key, seq, recorded_at`

func scanEffective(rows pgx.Rows, err error) ([]domain.Version, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Version
	for rows.Next() {
		var v domain.Version
		var after string
		if err := rows.Scan(&v.ID, &v.Catalog, &v.Code, &v.ChangeID, &v.ValidFrom, &after, &v.EventTime, &v.ActorRef, &v.IdemKey,
			&v.Seq, &v.RecordedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(after), &v.After); err != nil {
			return nil, err
		}
		if v.After.Attrs == nil {
			v.After.Attrs = map[string]string{}
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// EffectiveVersions liệt kê các phiên bản hiệu lực của một mục theo (valid_from, seq) tăng dần; ValidTo = valid_from của
// phiên bản kế tiếp (phiên bản cùng ngày bị phiên bản ghi sau thay thế — ValidTo = chính ngày đó).
func EffectiveVersions(ctx context.Context, tx pgx.Tx, catalog, code string) ([]domain.Version, error) {
	vs, err := scanEffective(tx.Query(ctx, "SELECT "+effectiveCols+` FROM pf_masterdata.catalog_effective
	    WHERE catalog = $1 AND code = $2 ORDER BY valid_from, seq`, catalog, code))
	if err != nil {
		return nil, err
	}
	for i := 0; i+1 < len(vs); i++ {
		t := vs[i+1].ValidFrom
		vs[i].ValidTo = &t
	}
	return vs, nil
}

// HasEffective cho biết mục đã có dòng sổ hiệu lực (mục nạp sẵn trước W11.41 / seed thì chưa có).
func HasEffective(ctx context.Context, tx pgx.Tx, catalog, code string) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pf_masterdata.catalog_effective WHERE catalog = $1 AND code = $2)`,
		catalog, code).Scan(&ok)
	return ok, err
}

// EffectiveAt trả phiên bản có hiệu lực tại ngày d của một mục; nil khi không có.
func EffectiveAt(ctx context.Context, tx pgx.Tx, catalog, code string, d time.Time) (*domain.Version, error) {
	vs, err := scanEffective(tx.Query(ctx, "SELECT "+effectiveCols+` FROM pf_masterdata.catalog_effective
	    WHERE catalog = $1 AND code = $2 AND valid_from <= $3::date ORDER BY valid_from DESC, seq DESC LIMIT 1`, catalog, code, day(d)))
	if err != nil || len(vs) == 0 {
		return nil, err
	}
	return &vs[0], nil
}

// ItemsAt trả mọi mục của danh mục với giá trị có hiệu lực tại ngày d: mục có sổ hiệu lực lấy phiên bản
// valid_from ≤ d mới nhất (mục chưa có hiệu lực tại d thì không có); mục chưa có sổ (seed) lấy bảng hiện trạng.
func ItemsAt(ctx context.Context, tx pgx.Tx, catalog string, d time.Time) ([]domain.Item, error) {
	rows, err := tx.Query(ctx, `
	  WITH v AS (
	    SELECT DISTINCT ON (code) code, valid_from, after
	      FROM pf_masterdata.catalog_effective
	     WHERE catalog = $1 AND valid_from <= $2::date
	     ORDER BY code, valid_from DESC, seq DESC)
	  SELECT i.code, coalesce(v.after->>'name', i.name), coalesce(v.after->'attrs', i.attrs)::text,
	         coalesce((v.after->>'active')::boolean, i.active), v.valid_from
	    FROM pf_masterdata.catalog_item i
	    LEFT JOIN v ON v.code = i.code
	   WHERE i.catalog = $1
	     AND (v.code IS NOT NULL OR NOT EXISTS (
	           SELECT 1 FROM pf_masterdata.catalog_effective e WHERE e.catalog = $1 AND e.code = i.code))
	   ORDER BY i.code`, catalog, day(d))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Item
	for rows.Next() {
		it := domain.Item{Catalog: catalog}
		var attrs string
		var from *time.Time
		if err := rows.Scan(&it.Code, &it.Name, &attrs, &it.Active, &from); err != nil {
			return nil, err
		}
		if from != nil {
			it.ValidFrom = *from
		}
		it.Attrs = map[string]string{}
		if err := json.Unmarshal([]byte(attrs), &it.Attrs); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// FacilityCodeByRef trả mã cơ sở giết mổ theo facility_ref ("" khi không có).
func FacilityCodeByRef(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (string, error) {
	var code string
	err := tx.QueryRow(ctx, `SELECT code FROM pf_masterdata.catalog_item WHERE catalog = $1 AND attrs->>'facility_ref' = $2`,
		port.CatalogCNTSSlaughterFacility, ref.String()).Scan(&code)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return code, err
}

// StampOwners trả mã mộc dấu → mã cơ sở (khác exceptCode) đang giữ mã đó, cho các mã trong stamps. Kiểm theo
// bảng hiện trạng (giá trị mới nhất) của mọi cơ sở, kể cả đã ngừng dùng: mã mộc dấu là định danh, không tái dùng.
func StampOwners(ctx context.Context, tx pgx.Tx, stamps []string, exceptCode string) (map[string]string, error) {
	out := map[string]string{}
	if len(stamps) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `
	  SELECT s, i.code
	    FROM pf_masterdata.catalog_item i,
	         unnest(string_to_array(coalesce(i.attrs->>'stamp_codes_gs', ''), ',') || string_to_array(coalesce(i.attrs->>'stamp_codes_gc', ''), ',')) AS s
	   WHERE i.catalog = $1 AND i.code <> $2
	     AND (string_to_array(coalesce(i.attrs->>'stamp_codes_gs', ''), ',') || string_to_array(coalesce(i.attrs->>'stamp_codes_gc', ''), ',')) && $3::text[]
	     AND s = ANY($3::text[])`, port.CatalogCNTSSlaughterFacility, exceptCode, stamps)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var s, code string
		if err := rows.Scan(&s, &code); err != nil {
			return nil, err
		}
		out[s] = code
	}
	return out, rows.Err()
}
