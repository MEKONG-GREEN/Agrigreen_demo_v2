package knaudit

// Nguồn nhật ký của KN đăng ký vào cổng #70. Mỗi nguồn là một sổ KN chỉ ghi thêm đã có; bộ đọc chỉ lấy UUID, thời
// điểm, mã sự việc, mã phòng ban và vài trường mã/ref trong danh sách cho phép — KHÔNG đọc lý do, ghi chú, payload,
// tên, địa chỉ, mã số thuế, diện tích hay xã. Câu SQL cố định theo nguồn (không lấy tên bảng/cột từ request).
// Outcome = succeeded: dòng sổ chỉ tồn tại khi thao tác đã ghi thành công (không suy từ mã sự việc); riêng sổ lượt xem
// lưu outcome thật (succeeded/denied).

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Khóa nguồn và module (mã cố định, hiện ở cột "Nguồn" của màn Admin).
const (
	SourceAuditView   = "kn.audit_view"
	SourceProducer    = "kn.producer"
	SourcePeriod      = "kn.period_event"
	SourceReview      = "kn.review_event"
	SourceKHTCPeriod  = "kn.khtc_period_event"
	SourceCNTSGrant   = "kn.cnts_grant"
	ModuleAudit       = "kn.audit"
	ModuleRecords     = "kn.records"
	ModuleReport      = "kn.report"
	ModuleKHTC        = "kn.khtc"
	ModuleCNTS        = "kn.cnts"
	maxWindow         = 93 * 24 * time.Hour
	maxSourceReadRows = 201
)

var code = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,79}$`)

type ledger struct {
	key, module, prefix string
	fields              map[string]accport.AuditValueKind
	query               string
	dept                bool
	// extra trả đích Scan của các cột thêm và hàm dựng Changes sau khi Scan.
	extra func() ([]any, func(*accport.AuditRecord))
}

// ledgerSQL ghép câu đọc từ hằng của nguồn: cột chung id, entity, actor_ref, event_time, recorded_at, mã sự việc, phòng ban.
// Thứ tự giảm dần (recorded_at, id); vị trí Before theo quy ước #70: cùng recorded_at thì nguồn có khóa nhỏ hơn đứng sau.
func ledgerSQL(table, entity, action, dept, extra string) string {
	d := "''"
	if dept != "" {
		d = "coalesce(" + dept + ", '')"
	}
	q := "SELECT id, " + entity + ", actor_ref, event_time, recorded_at, " + action + ", " + d + extra + "\n  FROM " + table + `
 WHERE recorded_at >= $1::timestamptz AND recorded_at < $2::timestamptz AND recorded_at <= $3::timestamptz
   AND ($4::uuid IS NULL OR actor_ref = $4::uuid)
   AND ($5::text = '' OR (` + action + `) = $5::text)
   AND ($6::timestamptz IS NULL OR recorded_at < $6::timestamptz
        OR (recorded_at = $6::timestamptz AND ($7::boolean OR ($8::boolean AND id < $9::uuid))))`
	if dept != "" {
		q += "\n   AND ($11::text = '' OR " + dept + " = $11::text)"
	}
	return q + "\n ORDER BY recorded_at DESC, id DESC LIMIT $10::integer"
}

func codes(field string, values ...string) *accport.AuditChange {
	out := []string{}
	for _, v := range values {
		if v == "" {
			continue
		}
		if !code.MatchString(v) {
			return nil
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return &accport.AuditChange{Field: field, After: &accport.AuditValue{Kind: accport.AuditCodes, Codes: out}}
}
func ref(field string, id *uuid.UUID) *accport.AuditChange {
	if id == nil || *id == uuid.Nil {
		return nil
	}
	v := *id
	return &accport.AuditChange{Field: field, After: &accport.AuditValue{Kind: accport.AuditRef, Ref: &v}}
}
func boolean(field string, b bool) *accport.AuditChange {
	return &accport.AuditChange{Field: field, After: &accport.AuditValue{Kind: accport.AuditBoolean, Boolean: &b}}
}
func appendChanges(r *accport.AuditRecord, cs ...*accport.AuditChange) {
	for _, c := range cs {
		if c != nil {
			r.Changes = append(r.Changes, *c)
		}
	}
}

var ledgers = []ledger{
	{key: SourceAuditView, module: ModuleAudit, prefix: "audit.",
		fields: map[string]accport.AuditValueKind{"denial": accport.AuditCodes, "sources": accport.AuditCodes,
			"filter_actor_ref": accport.AuditRef, "filter_module": accport.AuditCodes, "filter_action": accport.AuditCodes,
			"filter_department": accport.AuditCodes},
		query: ledgerSQL("pf_extension.kn_audit_view", "request_ref",
			`'audit.' || CASE outcome WHEN 'succeeded' THEN 'viewed' ELSE 'denied' END`, "",
			`, outcome, coalesce(denial_code, ''), sources, filter_actor_ref, coalesce(filter_module, ''), coalesce(filter_action, ''),
       coalesce(filter_department, '')`),
		extra: func() ([]any, func(*accport.AuditRecord)) {
			var outcome, denial, mod, act, dept string
			var sources []string
			var actor *uuid.UUID
			return []any{&outcome, &denial, &sources, &actor, &mod, &act, &dept}, func(r *accport.AuditRecord) {
				r.Outcome = outcome
				appendChanges(r, codes("denial", denial), codes("sources", sources...), ref("filter_actor_ref", actor),
					codes("filter_module", mod), codes("filter_action", act), codes("filter_department", dept))
			}
		}},
	{key: SourceProducer, module: ModuleRecords, prefix: "producer.",
		fields: map[string]accport.AuditValueKind{"kind": accport.AuditCodes, "parent_ref": accport.AuditRef,
			"htx_ref": accport.AuditRef, "on_behalf_of": accport.AuditRef},
		query: ledgerSQL("pf_extension.kn_producer", "producer_ref", `'producer.' || event`, "", `, kind, parent_ref, htx_ref, on_behalf_of`),
		extra: func() ([]any, func(*accport.AuditRecord)) {
			var kind string
			var parent, htx, behalf *uuid.UUID
			return []any{&kind, &parent, &htx, &behalf}, func(r *accport.AuditRecord) {
				appendChanges(r, codes("kind", kind), ref("parent_ref", parent), ref("htx_ref", htx), ref("on_behalf_of", behalf))
			}
		}},
	{key: SourcePeriod, module: ModuleReport, prefix: "period.", dept: true,
		fields: map[string]accport.AuditValueKind{"batch_ref": accport.AuditRef, "on_behalf_of": accport.AuditRef},
		query:  ledgerSQL("pf_extension.period_event", "period_id", `'period.' || action`, "department_code", `, batch_id, on_behalf_of`),
		extra: func() ([]any, func(*accport.AuditRecord)) {
			var batch, behalf *uuid.UUID
			return []any{&batch, &behalf}, func(r *accport.AuditRecord) {
				appendChanges(r, ref("batch_ref", batch), ref("on_behalf_of", behalf))
			}
		}},
	{key: SourceReview, module: ModuleReport, prefix: "review.", dept: true,
		fields: map[string]accport.AuditValueKind{"submission_ref": accport.AuditRef, "on_behalf_of": accport.AuditRef},
		query:  ledgerSQL("pf_extension.review_event", "task_id", `'review.' || action`, "department_code", `, submission_id, on_behalf_of`),
		extra: func() ([]any, func(*accport.AuditRecord)) {
			var sub, behalf *uuid.UUID
			return []any{&sub, &behalf}, func(r *accport.AuditRecord) {
				appendChanges(r, ref("submission_ref", sub), ref("on_behalf_of", behalf))
			}
		}},
	{key: SourceKHTCPeriod, module: ModuleKHTC, prefix: "khtc_period.",
		fields: map[string]accport.AuditValueKind{"forced": accport.AuditBoolean, "period_state": accport.AuditCodes},
		query:  ledgerSQL("pf_extension.khtc_period_event", "period_id", `'khtc_period.' || action`, "", `, forced, coalesce(period_state, '')`),
		extra: func() ([]any, func(*accport.AuditRecord)) {
			var forced bool
			var state string
			return []any{&forced, &state}, func(r *accport.AuditRecord) {
				appendChanges(r, boolean("forced", forced), codes("period_state", state))
			}
		}},
	{key: SourceCNTSGrant, module: ModuleCNTS, prefix: "cnts_grant.",
		fields: map[string]accport.AuditValueKind{"grantee_ref": accport.AuditRef, "role": accport.AuditCodes,
			"on_behalf_of": accport.AuditRef},
		query: ledgerSQL("pf_extension.cnts_grant", "grant_ref", `'cnts_grant.' || event`, "", `, grantee_ref, role, on_behalf_of`),
		extra: func() ([]any, func(*accport.AuditRecord)) {
			var grantee uuid.UUID
			var role string
			var behalf *uuid.UUID
			return []any{&grantee, &role, &behalf}, func(r *accport.AuditRecord) {
				appendChanges(r, ref("grantee_ref", &grantee), codes("role", role), ref("on_behalf_of", behalf))
			}
		}},
}

// Sources trả các nguồn nhật ký của KN (bản sao mới mỗi lần gọi) để gốc lắp ráp đăng ký vào AuditRegistry.
func (s *Service) Sources() []accport.AuditSource {
	out := make([]accport.AuditSource, 0, len(ledgers))
	for i := range ledgers {
		l := ledgers[i]
		fields := make(map[string]accport.AuditValueKind, len(l.fields))
		for k, v := range l.fields {
			fields[k] = v
		}
		out = append(out, accport.AuditSource{Key: l.key, Module: l.module, Fields: fields,
			Read: func(ctx context.Context, tid uuid.UUID, q accport.AuditReadQuery) ([]accport.AuditRecord, error) {
				return s.read(ctx, l, tid, q)
			}})
	}
	return out
}

// ErrQuery: truy vấn đọc nguồn ngoài miền cho phép (lõi #70 đã kiểm; đây là lớp phòng thủ của chủ nguồn).
var ErrQuery = errors.New("knaudit: invalid audit source query")

func (s *Service) read(ctx context.Context, l ledger, tid uuid.UUID, q accport.AuditReadQuery) ([]accport.AuditRecord, error) {
	p, ok := authz.FromContext(ctx)
	if !ok || tid == uuid.Nil || p.TenantID != tid || p.ActorRef == uuid.Nil {
		return nil, problem.Forbidden
	}
	if contextual, err := tenant.FromContext(ctx); err == nil && contextual != tid {
		return nil, problem.Forbidden
	} else if err != nil && !errors.Is(err, tenant.ErrMissing) {
		return nil, err
	}
	if s.DB == nil {
		return nil, ErrReceipt
	}
	if q.Limit < 1 || q.Limit > maxSourceReadRows || q.From.IsZero() || !q.To.After(q.From) || q.To.Sub(q.From) > maxWindow || q.AsOf.IsZero() {
		return nil, ErrQuery
	}
	if q.Module != "" && q.Module != l.module {
		return []accport.AuditRecord{}, nil
	}
	if (q.Department != "" && !l.dept) || (q.Action != "" && !strings.HasPrefix(q.Action, l.prefix)) {
		return []accport.AuditRecord{}, nil
	}
	var before, beforeID any
	includeEqual, sameSource := false, false
	if q.Before != nil {
		before, beforeID = q.Before.RecordedAt, q.Before.EventRef
		includeEqual = l.key < q.Before.SourceKey
		sameSource = l.key == q.Before.SourceKey
	}
	var actor any
	if q.ActorRef != uuid.Nil {
		actor = q.ActorRef
	}
	args := []any{q.From, q.To, q.AsOf, actor, q.Action, before, includeEqual, sameSource, beforeID, q.Limit}
	if l.dept {
		args = append(args, q.Department)
	}
	out := []accport.AuditRecord{}
	err := tenant.WithTenantTxOptions(ctx, s.DB, tid, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, l.query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r := accport.AuditRecord{Outcome: "succeeded", Changes: []accport.AuditChange{}}
			var dept string
			targets, build := l.extra()
			if err := rows.Scan(append([]any{&r.EventRef, &r.EntityRef, &r.ActorRef, &r.OccurredAt, &r.RecordedAt, &r.Action, &dept}, targets...)...); err != nil {
				return err
			}
			if code.MatchString(dept) {
				r.Department = dept
			}
			build(&r)
			out = append(out, r)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
