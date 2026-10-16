// Package store: SQL của bảng số liệu phiếu KHTC (pf 20261015070628 — pf_extension.khtc_form_sheet) trên pool vai của
// pf/extension, luôn trong giao dịch có ngữ cảnh tenant (RLS). Sổ chỉ ghi thêm (shared.apply_ledger).
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcform/domain"
)

// Loại bảng.
const (
	KindDraft  = "draft"
	KindSealed = "sealed"
)

// Sheet là một dòng sổ bảng số liệu.
type Sheet struct {
	ID              uuid.UUID
	PeriodID        uuid.UUID
	FormsPeriodRef  uuid.UUID
	Department      string
	Kind            string
	Year            int
	TemplateCode    string
	TemplateVersion int
	PlanVersionRef  *uuid.UUID
	BaseAttempt     int
	Content         domain.Sheet
	ContentHash     []byte
	ActorRef        uuid.UUID
	EventTime       time.Time
	Seq             int64
}

const cols = `id, period_id, forms_period_ref, department, kind, plan_year, template_code, template_version, plan_version_ref,
  base_attempt, content::text, content_hash, actor_ref, event_time, seq`

func scan(row pgx.Row) (Sheet, error) {
	var s Sheet
	var raw string
	err := row.Scan(&s.ID, &s.PeriodID, &s.FormsPeriodRef, &s.Department, &s.Kind, &s.Year, &s.TemplateCode, &s.TemplateVersion,
		&s.PlanVersionRef, &s.BaseAttempt, &raw, &s.ContentHash, &s.ActorRef, &s.EventTime, &s.Seq)
	if err != nil {
		return s, err
	}
	err = json.Unmarshal([]byte(raw), &s.Content)
	return s, err
}

// Insert ghi một bảng (trigger kiểm kỳ KHTC có thật, phòng tham gia, kỳ chưa đóng/hủy).
func Insert(ctx context.Context, tx pgx.Tx, tid uuid.UUID, s *Sheet, idem string) error {
	raw, hash, err := s.Content.Hash()
	if err != nil {
		return err
	}
	s.ContentHash = hash
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.khtc_form_sheet (id, tenant_id, stream_key, period_id, forms_period_ref, department,
	    kind, plan_year, template_code, template_version, plan_version_ref, base_attempt, content, content_hash, event_time, actor_ref,
	    idem_key, rule_version)
	  VALUES ($1, $2, 'khtc_form_sheet:' || $3::uuid::text || ':' || $5::text, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13, $14,
	    $15, $16, $17)
	  RETURNING seq`,
		s.ID, tid, s.PeriodID, s.FormsPeriodRef, s.Department, s.Kind, s.Year, s.TemplateCode, s.TemplateVersion, s.PlanVersionRef,
		s.BaseAttempt, string(raw), hash, s.EventTime.UTC(), s.ActorRef, idem, domain.RuleVersion).Scan(&s.Seq)
}

// ByIdem đọc bảng đã ghi với khóa idempotent (nil nếu chưa có).
func ByIdem(ctx context.Context, tx pgx.Tx, idem string) (*Sheet, error) {
	return one(scan(tx.QueryRow(ctx, `SELECT `+cols+` FROM pf_extension.khtc_form_sheet WHERE idem_key = $1`, idem)))
}

func one(s Sheet, err error) (*Sheet, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ByID đọc một bảng (nil nếu không có).
func ByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Sheet, error) {
	return one(scan(tx.QueryRow(ctx, `SELECT `+cols+` FROM pf_extension.khtc_form_sheet WHERE id = $1`, id)))
}

// Latest đọc bảng mới nhất của (kỳ, phòng) theo loại (kind rỗng = mọi loại) — nil nếu chưa có.
func Latest(ctx context.Context, tx pgx.Tx, periodID uuid.UUID, dept, kind string) (*Sheet, error) {
	return one(scan(tx.QueryRow(ctx, `SELECT `+cols+` FROM pf_extension.khtc_form_sheet
	  WHERE period_id = $1 AND department = $2 AND ($3 = '' OR kind = $3) ORDER BY seq DESC LIMIT 1`, periodID, dept, kind)))
}

// ByIDs đọc các bảng theo mã.
func ByIDs(ctx context.Context, tx pgx.Tx, idl []uuid.UUID) (map[uuid.UUID]Sheet, error) {
	out := map[uuid.UUID]Sheet{}
	if len(idl) == 0 {
		return out, nil
	}
	strs := make([]string, 0, len(idl))
	for _, id := range idl {
		strs = append(strs, id.String())
	}
	rows, err := tx.Query(ctx, `SELECT `+cols+` FROM pf_extension.khtc_form_sheet WHERE id = ANY($1::uuid[])`, strs)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Sheet, error) { return scan(r) })
	for _, s := range list {
		out[s.ID] = s
	}
	return out, err
}

// DraftAttempts trả, theo phòng, số lần gửi đã thấy (base_attempt) của bảng nháp mới nhất ở kỳ — nháp hiện hành khi bằng
// số lần đã gửi.
func DraftAttempts(ctx context.Context, tx pgx.Tx, periodID uuid.UUID) (map[string]int, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (department) department, base_attempt FROM pf_extension.khtc_form_sheet
	  WHERE period_id = $1 AND kind = 'draft' ORDER BY department, seq DESC`, periodID)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for rows.Next() {
		var d string
		var n int
		if err := rows.Scan(&d, &n); err != nil {
			return nil, err
		}
		out[d] = n
	}
	return out, rows.Err()
}

// SheetUse là một bảng niêm phong có chứa số liệu của các dòng kế hoạch đang hỏi.
type SheetUse struct {
	SheetID        uuid.UUID
	FormsPeriodRef uuid.UUID
	Department     string
	Entries        []uuid.UUID
}

// SealedUses trả các bảng niêm phong của năm kế hoạch year có ô nhập hoặc dòng điều chỉnh của dòng kế hoạch trong refs.
// Bảng chỉ có hiệu lực khi có bài nộp cap/forms trỏ tới (bên gọi kiểm — bảng mồ côi của lần gửi lỗi không tính).
func SealedUses(ctx context.Context, tx pgx.Tx, year int, refs []uuid.UUID) ([]SheetUse, error) {
	keys := make([]string, 0, len(refs))
	for _, r := range refs {
		keys = append(keys, r.String())
	}
	rows, err := tx.Query(ctx, `SELECT u.id, u.forms_period_ref, u.department, array_agg(DISTINCT u.k) FROM (
	    SELECT s.id, s.forms_period_ref, s.department, jsonb_object_keys(s.content->'cells') AS k
	      FROM pf_extension.khtc_form_sheet s WHERE s.plan_year = $1 AND s.kind = 'sealed'
	    UNION ALL
	    SELECT s.id, s.forms_period_ref, s.department, a->>'entry_ref'
	      FROM pf_extension.khtc_form_sheet s, jsonb_array_elements(s.content->'adjustments') a
	     WHERE s.plan_year = $1 AND s.kind = 'sealed'
	  ) u WHERE u.k = ANY($2::text[]) GROUP BY u.id, u.forms_period_ref, u.department`, year, keys)
	if err != nil {
		return nil, err
	}
	var out []SheetUse
	for rows.Next() {
		var su SheetUse
		var ks []string
		if err := rows.Scan(&su.SheetID, &su.FormsPeriodRef, &su.Department, &ks); err != nil {
			return nil, err
		}
		for _, k := range ks {
			if id, err := uuid.Parse(k); err == nil {
				su.Entries = append(su.Entries, id)
			}
		}
		out = append(out, su)
	}
	return out, rows.Err()
}
