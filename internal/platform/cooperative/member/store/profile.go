package store

// Hồ sơ HTX pf_coop.cooperative (CGH FN-03): mã số thuế, diện tích đăng ký, ngày thành lập, vị trí, trụ sở.
// Khóa là org_ref (FK → pf_party.org, D-7) — Nút kind=cooperative do pf/party ghi trước (tạo HTX hai bước).

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// Profile là hồ sơ HTX.
type Profile struct {
	OrgRef        uuid.UUID
	TaxCode       string
	AreaHa        *decimal.Decimal
	EstablishedOn *time.Time
	Lat, Lon      *decimal.Decimal
	Address       string
	UpdatedAt     time.Time
	// Confirmed (D2, ADR-0052): Nút thuộc neo HTX đã được HTX thật nhận quyền. false = "Chưa xác nhận" (HTX tạm do
	// KN/MKG lập, hoặc HTX có trước D2 chưa nhận quyền).
	Confirmed bool
}

// confirmedExpr: Nút c.org_ref thuộc neo đã nhận quyền (pf_coop.htx_anchor_alias + htx_anchor, pf 000155).
const confirmedExpr = `EXISTS (SELECT 1 FROM pf_coop.htx_anchor_alias al JOIN pf_coop.htx_anchor a
    ON a.tenant_id = al.tenant_id AND a.anchor_id = al.anchor_id
    WHERE al.tenant_id = c.tenant_id AND al.org_ref = c.org_ref AND a.status = 'claimed')`

const profileCols = `c.org_ref, coalesce(c.tax_code, ''), c.area_ha::text, c.established_on::text,
  ST_Y(c.location)::numeric(10,7)::text, ST_X(c.location)::numeric(10,7)::text, coalesce(c.head_office, ''), c.updated_at, ` + confirmedExpr

func scanProfile(row pgx.Row) (Profile, error) {
	var p Profile
	var area, est, lat, lon *string
	err := row.Scan(&p.OrgRef, &p.TaxCode, &area, &est, &lat, &lon, &p.Address, &p.UpdatedAt, &p.Confirmed)
	if err != nil {
		return p, err
	}
	if p.AreaHa, err = parseDec(area); err != nil {
		return p, err
	}
	if est != nil {
		t, err := time.Parse(time.DateOnly, *est)
		if err != nil {
			return p, err
		}
		p.EstablishedOn = &t
	}
	if lat != nil && lon != nil {
		if p.Lat, err = parseDec(lat); err != nil {
			return p, err
		}
		if p.Lon, err = parseDec(lon); err != nil {
			return p, err
		}
	}
	return p, nil
}

// ProfilesByRefs đọc hồ sơ theo lô.
func ProfilesByRefs(ctx context.Context, tx pgx.Tx, refs []uuid.UUID) (map[uuid.UUID]Profile, error) {
	out := map[uuid.UUID]Profile{}
	if len(refs) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, "SELECT "+profileCols+" FROM pf_coop.cooperative c WHERE c.org_ref = ANY($1::text[]::uuid[])", uuidStrings(refs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out[p.OrgRef] = p
	}
	return out, rows.Err()
}

// InsertProfile ghi hồ sơ HTX; đã có hồ sơ của Nút → inserted = false, không đổi gì (bước 2 lũy đẳng của
// tạo HTX hai bước).
func InsertProfile(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, p Profile) (bool, error) {
	var est, lat, lon *string
	if p.EstablishedOn != nil {
		s := p.EstablishedOn.Format(time.DateOnly)
		est = &s
	}
	if p.Lat != nil && p.Lon != nil {
		a, b := p.Lat.String(), p.Lon.String()
		lat, lon = &a, &b
	}
	tag, err := tx.Exec(ctx, `INSERT INTO pf_coop.cooperative (tenant_id, org_ref, tax_code, area_ha, established_on, location, head_office)
	  VALUES ($1, $2, $3, $4::numeric, $5::date,
	    CASE WHEN $6::text IS NULL THEN NULL
	         ELSE ST_SetSRID(ST_MakePoint($7::text::float8, $6::text::float8), 4326) END, $8)
	  ON CONFLICT (tenant_id, org_ref) DO NOTHING`,
		tenantID, p.OrgRef, nilIfEmpty(p.TaxCode), decPtr(p.AreaHa), est, lat, lon, nilIfEmpty(strings.TrimSpace(p.Address)))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// UpdateProfile ghi diện tích, mã số thuế, trụ sở hiện hành của HTX (bảng chiếu, cùng giao dịch với sổ hồ sơ KN).
func UpdateProfile(ctx context.Context, tx pgx.Tx, ref uuid.UUID, area *decimal.Decimal, taxCode, headOffice string) error {
	_, err := tx.Exec(ctx, `UPDATE pf_coop.cooperative SET area_ha = $2::numeric, tax_code = $3, head_office = $4, updated_at = now()
	  WHERE org_ref = $1`, ref, decPtr(area), nilIfEmpty(taxCode), nilIfEmpty(strings.TrimSpace(headOffice)))
	return err
}

// TaxCodeOwner trả HTX đang giữ mã số thuế trong tenant, Nil nếu chưa có.
func TaxCodeOwner(ctx context.Context, tx pgx.Tx, tax string) (uuid.UUID, error) {
	var ref uuid.UUID
	err := tx.QueryRow(ctx, "SELECT org_ref FROM pf_coop.cooperative WHERE tax_code = $1", tax).Scan(&ref)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	return ref, err
}

// MatchProfiles trả Nút có hồ sơ khớp bộ lọc: mã số thuế bắt đầu bằng taxPrefix (rỗng = không lọc), diện tích
// trong [areaMin, areaMax] (nil = không lọc).
//
// confirmation (D2): "confirmed" | "unconfirmed" | rỗng = không lọc.
func MatchProfiles(ctx context.Context, tx pgx.Tx, taxPrefix string, areaMin, areaMax *decimal.Decimal, confirmation string) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT c.org_ref FROM pf_coop.cooperative c
	  WHERE ($1 = '' OR c.tax_code LIKE $1 || '%')
	    AND ($2::numeric IS NULL OR c.area_ha >= $2::numeric)
	    AND ($3::numeric IS NULL OR c.area_ha <= $3::numeric)
	    AND ($4 = '' OR (`+confirmedExpr+`) = ($4 = 'confirmed'))`,
		escapeLike(strings.TrimSpace(taxPrefix)), decPtr(areaMin), decPtr(areaMax), confirmation)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// LocationsInBBox đọc vị trí hồ sơ HTX trong khung nhìn WGS84, sắp theo org_ref (lọc trạng thái Nút ở app/).
func LocationsInBBox(ctx context.Context, tx pgx.Tx, minLon, minLat, maxLon, maxLat float64) ([]Profile, error) {
	rows, err := tx.Query(ctx, "SELECT "+profileCols+` FROM pf_coop.cooperative c
	  WHERE c.location IS NOT NULL AND c.location && ST_MakeEnvelope($1, $2, $3, $4, 4326)
	  ORDER BY c.org_ref`, minLon, minLat, maxLon, maxLat)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Profile
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
