package store

// BF-CN-PUBLISH (#108, CN-07): sổ lần phát hành biểu kỳ CNTS (pf_extension.cnts_publication — người phụ trách, đối
// tượng, giờ chốt, kỳ native đã sinh) và bảng chiếu nháp của cán bộ xã (cnts_period_draft). Migration pf
// 20261027200000.

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CNTSPublicationRuleVersion là rule_version của sổ cnts_publication.
const CNTSPublicationRuleVersion = "kn.cnts_publication/v1"

// CNTSPublication là một lần phát hành lô kỳ CNTS.
type CNTSPublication struct {
	ID              uuid.UUID
	Department      string
	OrgRef          *uuid.UUID
	TemplateCode    string
	OwnerRef        *uuid.UUID
	TargetMode      string
	Regions         []string
	UnitKind        string
	Units           []string
	From, To        time.Time
	DeadlineOffset  int
	DeadlineTime    string
	CloseAfterHours *int
	PeriodRefs      []uuid.UUID
	TaskCount       int
	EventTime       time.Time
	ActorRef        uuid.UUID
	OnBehalfOf      *uuid.UUID
	SupportReason   *string
	IdemKey         string
}

// InsertCNTSPublication ghi lần phát hành (sổ chỉ ghi thêm — trùng idem_key trả lỗi ledger.ErrDuplicate khi phân loại).
func InsertCNTSPublication(ctx context.Context, tx pgx.Tx, tid uuid.UUID, p CNTSPublication) error {
	regions := p.Regions
	if regions == nil {
		regions = []string{}
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.cnts_publication (id, tenant_id, stream_key, department_code, org_ref, template_code, owner_ref,
	    target_mode, regions, unit_kind, units, period_from, period_to, deadline_offset, deadline_time, close_after_hours, period_refs,
	    task_count, event_time, actor_ref, on_behalf_of, support_reason, idem_key, rule_version)
	  VALUES ($1, $2, 'cnts_publication:' || $4::text, $3, $23, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15::uuid[], $16, $17, $18, $19, $20, $21, $22)`,
		p.ID, tid, p.Department, p.TemplateCode, p.OwnerRef, p.TargetMode, regions, p.UnitKind, p.Units, p.From, p.To, p.DeadlineOffset,
		p.DeadlineTime, p.CloseAfterHours, uuidStrings(p.PeriodRefs), p.TaskCount, p.EventTime.UTC(), p.ActorRef, p.OnBehalfOf, p.SupportReason, p.IdemKey,
		CNTSPublicationRuleVersion, p.OrgRef)
	return err
}

const cntsPublicationCols = `id, department_code, org_ref, template_code, owner_ref, target_mode, regions, unit_kind, units, period_from, period_to,
  deadline_offset, deadline_time, close_after_hours, period_refs::text[], task_count, event_time, actor_ref, idem_key`

func scanCNTSPublication(r pgx.Row) (CNTSPublication, error) {
	var p CNTSPublication
	var refs []string
	err := r.Scan(&p.ID, &p.Department, &p.OrgRef, &p.TemplateCode, &p.OwnerRef, &p.TargetMode, &p.Regions, &p.UnitKind, &p.Units, &p.From, &p.To,
		&p.DeadlineOffset, &p.DeadlineTime, &p.CloseAfterHours, &refs, &p.TaskCount, &p.EventTime, &p.ActorRef, &p.IdemKey)
	if err != nil {
		return p, err
	}
	for _, s := range refs {
		id, err := uuid.Parse(s)
		if err != nil {
			return p, err
		}
		p.PeriodRefs = append(p.PeriodRefs, id)
	}
	return p, nil
}

// uuidStrings: pgx không mã hóa sẵn []uuid.UUID — gửi mảng chuỗi kèm ép kiểu ::uuid[].
func uuidStrings(xs []uuid.UUID) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		out = append(out, x.String())
	}
	return out
}

// CNTSPublicationByIdem trả lần phát hành có khóa idem (nil nếu chưa có).
func CNTSPublicationByIdem(ctx context.Context, tx pgx.Tx, idem string) (*CNTSPublication, error) {
	p, err := scanCNTSPublication(tx.QueryRow(ctx, "SELECT "+cntsPublicationCols+" FROM pf_extension.cnts_publication WHERE idem_key = $1 LIMIT 1", idem))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &p, err
}

// CNTSPublications trả các lần phát hành của phòng (biểu code, rỗng = mọi biểu; org khác nil = chỉ của Nút đó) từ thời
// điểm since, mới nhất trước.
func CNTSPublications(ctx context.Context, tx pgx.Tx, dept, code string, org *uuid.UUID, since time.Time, limit int) ([]CNTSPublication, error) {
	rows, err := tx.Query(ctx, "SELECT "+cntsPublicationCols+` FROM pf_extension.cnts_publication
	  WHERE department_code = $1 AND ($2 = '' OR template_code = $2) AND event_time >= $3 AND ($5::uuid IS NULL OR org_ref = $5)
	  ORDER BY event_time DESC, id DESC LIMIT $4`, dept, code, since.UTC(), limit, org)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (CNTSPublication, error) { return scanCNTSPublication(r) })
}

// CNTSPublicationOfPeriod trả lần phát hành mới nhất chứa kỳ native period (nil nếu kỳ phát hành trước BF-CN-PUBLISH).
func CNTSPublicationOfPeriod(ctx context.Context, tx pgx.Tx, period uuid.UUID) (*CNTSPublication, error) {
	p, err := scanCNTSPublication(tx.QueryRow(ctx, "SELECT "+cntsPublicationCols+` FROM pf_extension.cnts_publication
	  WHERE period_refs @> ARRAY[$1::uuid] ORDER BY event_time DESC LIMIT 1`, period))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &p, err
}

// CNTSSiteCommunes trả các xã có điểm trình diễn CNTS đang hoạt động (phiên bản cuối không "closed"); aquaculture =
// chỉ điểm ghi nhật ký thủy sản BM-19 (xã có nuôi thủy sản).
func CNTSSiteCommunes(ctx context.Context, tx pgx.Tx, aquaculture bool) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT commune_code FROM (
	    SELECT DISTINCT ON (site_ref) commune_code, event, diary_form FROM pf_extension.cnts_demo_site ORDER BY site_ref, seq DESC) s
	  WHERE event <> 'closed' AND (NOT $1 OR diary_form = 'BM-19') ORDER BY commune_code`, aquaculture)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// CNTSDraft là bản nháp của một đơn vị trong kỳ native.
type CNTSDraft struct {
	PeriodRef    uuid.UUID
	Unit         string
	Parent       string
	TemplateCode string
	Values       map[string]string
	NilReport    bool
	Note         string
	SavedBy      uuid.UUID
	SavedAt      time.Time
}

// UpsertCNTSDraft ghi (đè) nháp của (kỳ, đơn vị).
func UpsertCNTSDraft(ctx context.Context, tx pgx.Tx, tid uuid.UUID, d CNTSDraft) error {
	raw, err := json.Marshal(d.Values)
	if err != nil {
		return err
	}
	var note *string
	if d.Note != "" {
		note = &d.Note
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_extension.cnts_period_draft (tenant_id, period_ref, unit_code, parent_code, template_code, draft_values,
	    nil_report, note, saved_by, saved_at)
	  VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9, $10)
	  ON CONFLICT (tenant_id, period_ref, unit_code) DO UPDATE SET parent_code = EXCLUDED.parent_code, template_code = EXCLUDED.template_code,
	    draft_values = EXCLUDED.draft_values, nil_report = EXCLUDED.nil_report, note = EXCLUDED.note, saved_by = EXCLUDED.saved_by,
	    saved_at = EXCLUDED.saved_at`,
		tid, d.PeriodRef, d.Unit, d.Parent, d.TemplateCode, string(raw), d.NilReport, note, d.SavedBy, d.SavedAt.UTC())
	return err
}

const cntsDraftCols = `period_ref, unit_code, parent_code, template_code, draft_values::text, nil_report, coalesce(note, ''), saved_by, saved_at`

func scanCNTSDraft(r pgx.Row) (CNTSDraft, error) {
	var d CNTSDraft
	var raw string
	if err := r.Scan(&d.PeriodRef, &d.Unit, &d.Parent, &d.TemplateCode, &raw, &d.NilReport, &d.Note, &d.SavedBy, &d.SavedAt); err != nil {
		return d, err
	}
	err := json.Unmarshal([]byte(raw), &d.Values)
	return d, err
}

// CNTSDraftOf trả nháp của (kỳ, đơn vị) — nil nếu chưa có.
func CNTSDraftOf(ctx context.Context, tx pgx.Tx, period uuid.UUID, unit string) (*CNTSDraft, error) {
	d, err := scanCNTSDraft(tx.QueryRow(ctx, "SELECT "+cntsDraftCols+" FROM pf_extension.cnts_period_draft WHERE period_ref = $1 AND unit_code = $2",
		period, unit))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &d, err
}

// CNTSDraftsOf trả nháp của các kỳ native (không kèm số liệu — màn theo dõi chỉ cần dấu nháp).
func CNTSDraftsOf(ctx context.Context, tx pgx.Tx, periods []uuid.UUID) ([]CNTSDraft, error) {
	rows, err := tx.Query(ctx, `SELECT period_ref, unit_code, parent_code, template_code, '{}'::text, nil_report, '', saved_by, saved_at
	  FROM pf_extension.cnts_period_draft WHERE period_ref = ANY($1::uuid[]) ORDER BY period_ref, unit_code`, uuidStrings(periods))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (CNTSDraft, error) { return scanCNTSDraft(r) })
}
