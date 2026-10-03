package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/domain"
)

// Cột đọc của sổ minh chứng. Toạ độ đọc thành chuỗi thập phân 7 chữ số lẻ.
const evidenceCols = `e.id, e.tenant_id, e.root_id, e.chain_version, e.evidence_type, e.catalog_version, e.admin_unit_code,
  e.plot_ref, e.household_ref, e.season_code, e.field_values::text, e.file_refs::text,
  ST_Y(e.gps)::numeric(10,7)::text, ST_X(e.gps)::numeric(10,7)::text, e.gps_accuracy_m::text,
  e.event_time, e.supersedes_id, e.correction_reason, e.actor_ref, e.device_id, e.idem_key, e.seq, e.recorded_at, e.row_hash`

func scanEvidence(row pgx.Row, extra ...any) (domain.Evidence, error) {
	var e domain.Evidence
	var values, files string
	var lat, lon, acc *string
	dest := append([]any{&e.ID, &e.TenantID, &e.RootID, &e.ChainVersion, &e.EvidenceType, &e.CatalogVersion, &e.AdminUnitCode,
		&e.PlotRef, &e.HouseholdRef, &e.SeasonCode, &values, &files, &lat, &lon, &acc,
		&e.EventTime, &e.SupersedesID, &e.CorrectionReason, &e.ActorRef, &e.DeviceID, &e.IdemKey, &e.Seq, &e.RecordedAt, &e.RowHash}, extra...)
	if err := row.Scan(dest...); err != nil {
		return e, err
	}
	if err := json.Unmarshal([]byte(values), &e.Values); err != nil {
		return e, err
	}
	if err := json.Unmarshal([]byte(files), &e.FileRefs); err != nil {
		return e, err
	}
	if lat != nil && lon != nil {
		g := domain.GeoPoint{}
		var err error
		if g.Lat, err = decimal.NewFromString(*lat); err != nil {
			return e, err
		}
		if g.Lon, err = decimal.NewFromString(*lon); err != nil {
			return e, err
		}
		if g.AccuracyM, err = parseDec(acc); err != nil {
			return e, err
		}
		e.GPS = &g
	}
	return e, nil
}

func oneEvidence(ctx context.Context, tx pgx.Tx, where string, args ...any) (*domain.Evidence, error) {
	e, err := scanEvidence(tx.QueryRow(ctx, "SELECT "+evidenceCols+" FROM pf_mrv.evidence e WHERE "+where, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// EvidenceByIdemKey tìm bản ghi đã ghi với idem_key (gọi lặp / đồng bộ lặp).
func EvidenceByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*domain.Evidence, error) {
	return oneEvidence(ctx, tx, "e.idem_key = $1::text", key)
}

// EvidenceByID đọc một phiên bản minh chứng.
func EvidenceByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*domain.Evidence, error) {
	return oneEvidence(ctx, tx, "e.id = $1", id)
}

// InsertEvidence ghi sổ; DB tính seq, chuỗi băm, recorded_at.
func InsertEvidence(ctx context.Context, tx pgx.Tx, e *domain.Evidence) error {
	values, err := json.Marshal(e.Values)
	if err != nil {
		return err
	}
	if e.Values == nil {
		values = []byte("{}")
	}
	files := []domain.FileRef{}
	if e.FileRefs != nil {
		files = e.FileRefs
	}
	fb, err := json.Marshal(files)
	if err != nil {
		return err
	}
	var lat, lon, acc *string
	if e.GPS != nil {
		la, lo := e.GPS.Lat.String(), e.GPS.Lon.String()
		lat, lon = &la, &lo
		acc = decStr(e.GPS.AccuracyM)
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_mrv.evidence
	    (id, tenant_id, stream_key, root_id, chain_version, evidence_type, catalog_version, admin_unit_code, plot_ref,
	     household_ref, season_code, field_values, file_refs, gps, gps_accuracy_m, event_time, supersedes_id,
	     correction_reason, actor_ref, device_id, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13::jsonb,
	     CASE WHEN $14::text IS NULL THEN NULL
	          ELSE ST_SetSRID(ST_MakePoint($15::text::numeric::float8, $14::text::numeric::float8), 4326) END,
	     $16::text::numeric, $17, $18, $19, $20, $21, $22, $23)
	  RETURNING seq, recorded_at, row_hash`,
		e.ID, e.TenantID, domain.StreamKey(e.AdminUnitCode), e.RootID, e.ChainVersion, e.EvidenceType, e.CatalogVersion,
		e.AdminUnitCode, e.PlotRef, e.HouseholdRef, e.SeasonCode, string(values), string(fb), lat, lon, acc,
		e.EventTime.UTC(), e.SupersedesID, e.CorrectionReason, e.ActorRef, e.DeviceID, e.IdemKey, domain.RuleVersion).
		Scan(&e.Seq, &e.RecordedAt, &e.RowHash)
}

// CurrentForUpdate đọc bảng chiếu của chuỗi và khóa dòng (R-21).
func CurrentForUpdate(ctx context.Context, tx pgx.Tx, rootID uuid.UUID) (*domain.Current, error) {
	var c domain.Current
	var status string
	err := tx.QueryRow(ctx, `SELECT root_id, head_id, chain_version, evidence_type, admin_unit_code, captured_by, review_status, review_id
	    FROM pf_mrv.evidence_current WHERE root_id = $1 FOR UPDATE`, rootID).
		Scan(&c.RootID, &c.HeadID, &c.ChainVersion, &c.EvidenceType, &c.AdminUnitCode, &c.CapturedBy, &status, &c.ReviewID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.ReviewStatus = domain.ReviewStatus(status)
	return &c, nil
}

// UpsertCurrent đặt bản hiện hành của chuỗi; bản mới luôn chờ duyệt.
func UpsertCurrent(ctx context.Context, tx pgx.Tx, e domain.Evidence) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_mrv.evidence_current
	    (tenant_id, root_id, head_id, chain_version, evidence_type, catalog_version, admin_unit_code, season_code,
	     event_time, captured_by, review_status, review_id)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'pending', NULL)
	  ON CONFLICT (tenant_id, root_id) DO UPDATE SET
	    head_id = EXCLUDED.head_id, chain_version = EXCLUDED.chain_version, evidence_type = EXCLUDED.evidence_type,
	    catalog_version = EXCLUDED.catalog_version, admin_unit_code = EXCLUDED.admin_unit_code,
	    season_code = EXCLUDED.season_code, event_time = EXCLUDED.event_time, captured_by = EXCLUDED.captured_by,
	    review_status = 'pending', review_id = NULL, updated_at = now()`,
		e.TenantID, e.RootID, e.ID, e.ChainVersion, e.EvidenceType, e.CatalogVersion, e.AdminUnitCode, e.SeasonCode,
		e.EventTime.UTC(), e.ActorRef)
	return err
}

// SetReview ghi kết quả duyệt vào bảng chiếu.
func SetReview(ctx context.Context, tx pgx.Tx, rootID uuid.UUID, status domain.ReviewStatus, reviewID uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE pf_mrv.evidence_current SET review_status = $2, review_id = $3, updated_at = now()
	  WHERE root_id = $1`, rootID, string(status), reviewID)
	return err
}

// ChainActors trả mọi người đã thu một phiên bản trong chuỗi.
func ChainActors(ctx context.Context, tx pgx.Tx, rootID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, "SELECT DISTINCT actor_ref FROM pf_mrv.evidence WHERE root_id = $1", rootID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// Chain trả mọi phiên bản của chuỗi theo thứ tự.
func Chain(ctx context.Context, tx pgx.Tx, rootID uuid.UUID) ([]domain.Evidence, error) {
	rows, err := tx.Query(ctx, "SELECT "+evidenceCols+" FROM pf_mrv.evidence e WHERE e.root_id = $1 ORDER BY e.chain_version", rootID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Evidence
	for rows.Next() {
		e, err := scanEvidence(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

const reviewCols = `r.id, r.tenant_id, r.evidence_id, r.root_id, r.decision, r.reason, r.event_time, r.actor_ref, r.idem_key, r.seq, r.recorded_at`

func scanReview(row pgx.Row) (domain.Review, error) {
	var r domain.Review
	var d string
	err := row.Scan(&r.ID, &r.TenantID, &r.EvidenceID, &r.RootID, &d, &r.Reason, &r.EventTime, &r.ActorRef, &r.IdemKey, &r.Seq, &r.RecordedAt)
	r.Decision = domain.ReviewStatus(d)
	return r, err
}

// ReviewByIdemKey tìm quyết định đã ghi với idem_key.
func ReviewByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*domain.Review, error) {
	r, err := scanReview(tx.QueryRow(ctx, "SELECT "+reviewCols+" FROM pf_mrv.evidence_review r WHERE r.idem_key = $1::text", key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ReviewsOfRoot trả mọi quyết định duyệt của một chuỗi.
func ReviewsOfRoot(ctx context.Context, tx pgx.Tx, rootID uuid.UUID) ([]domain.Review, error) {
	rows, err := tx.Query(ctx, "SELECT "+reviewCols+" FROM pf_mrv.evidence_review r WHERE r.root_id = $1 ORDER BY r.recorded_at, r.seq", rootID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Review
	for rows.Next() {
		r, err := scanReview(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// InsertReview ghi sổ quyết định duyệt.
func InsertReview(ctx context.Context, tx pgx.Tx, r *domain.Review, unitCode string) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_mrv.evidence_review
	    (id, tenant_id, stream_key, evidence_id, root_id, decision, reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING seq, recorded_at`,
		r.ID, r.TenantID, domain.StreamKey(unitCode), r.EvidenceID, r.RootID, string(r.Decision), r.Reason, r.EventTime.UTC(),
		r.ActorRef, r.IdemKey, domain.RuleVersion).Scan(&r.Seq, &r.RecordedAt)
}

// ListFilter lọc danh sách bản hiện hành.
type ListFilter struct {
	EvidenceType  string
	AdminUnitCode string
	ReviewStatus  string
	All           bool
	Units         []string
}

// Listed là một bản hiện hành kèm trạng thái duyệt.
type Listed struct {
	Evidence     domain.Evidence
	ReviewStatus domain.ReviewStatus
	// Urgent: bản hiện hành mang nhãn "Cảnh báo khẩn" (KN US-MRV-04) — điền ở app/.
	Urgent bool
}

// ListCurrent phân trang bản hiện hành theo root_id (R-53), trong phạm vi.
func ListCurrent(ctx context.Context, tx pgx.Tx, f ListFilter, after uuid.UUID, limit int) ([]Listed, error) {
	units := f.Units
	if units == nil {
		units = []string{}
	}
	rows, err := tx.Query(ctx, "SELECT "+evidenceCols+`, c.review_status
	  FROM pf_mrv.evidence_current c JOIN pf_mrv.evidence e ON e.id = c.head_id
	  WHERE c.root_id > $1
	    AND ($2::bool OR c.admin_unit_code = ANY($3::text[]))
	    AND ($4::text = '' OR c.evidence_type = $4::text)
	    AND ($5::text = '' OR c.admin_unit_code = $5::text)
	    AND ($6::text = '' OR c.review_status = $6::text)
	  ORDER BY c.root_id LIMIT $7`, after, f.All, units, f.EvidenceType, f.AdminUnitCode, f.ReviewStatus, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Listed
	for rows.Next() {
		var st string
		e, err := scanEvidence(rows, &st)
		if err != nil {
			return nil, err
		}
		out = append(out, Listed{Evidence: e, ReviewStatus: domain.ReviewStatus(st)})
	}
	return out, rows.Err()
}

// RebuildCurrent dựng lại bảng chiếu từ sổ cho tenant hiện tại (R-22); at là updated_at (đồng hồ chung của app).
func RebuildCurrent(ctx context.Context, tx pgx.Tx, at time.Time) (int64, error) {
	if _, err := tx.Exec(ctx, "DELETE FROM pf_mrv.evidence_current"); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO pf_mrv.evidence_current
	    (tenant_id, root_id, head_id, chain_version, evidence_type, catalog_version, admin_unit_code, season_code,
	     event_time, captured_by, review_status, review_id, updated_at)
	  SELECT h.tenant_id, h.root_id, h.id, h.chain_version, h.evidence_type, h.catalog_version, h.admin_unit_code,
	         h.season_code, h.event_time, h.actor_ref, coalesce(r.decision, 'pending'), r.id, $1::timestamptz
	  FROM (SELECT DISTINCT ON (root_id) * FROM pf_mrv.evidence ORDER BY root_id, chain_version DESC) h
	  LEFT JOIN LATERAL (SELECT id, decision FROM pf_mrv.evidence_review x WHERE x.evidence_id = h.id
	                     ORDER BY x.recorded_at DESC, x.seq DESC LIMIT 1) r ON true`, at.UTC())
	return tag.RowsAffected(), err
}
