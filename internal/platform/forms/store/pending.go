package store

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
)

// PendingSelection contains only normalized server-owned policy facts.
type PendingSelection struct {
	Query               port.PendingQuery
	Scope               port.PendingScope
	NativeAll           bool
	NativeUnits         []string
	NativeDepartment    string
	NativeDepartmentAll bool
	AfterPeriod         uuid.UUID
	AfterUnit           string
}

type PendingResult struct {
	Items             []port.PendingSubmission
	Count, Candidates int64
	Visibility        string
	CoverageComplete  bool
	Invalid           bool
	AnchorFound       bool
}

// PendingNative is one SQL statement: all candidate validation, exact count,
// bounded page and hashed visibility epoch share one MVCC statement snapshot.
// No row history or full-set JSON/string aggregation is materialized in Go.
func PendingNative(ctx context.Context, tx pgx.Tx, in PendingSelection) (PendingResult, error) {
	q, s := in.Query, in.Scope
	var out PendingResult
	var raw []byte
	err := tx.QueryRow(ctx, `WITH periods AS MATERIALIZED (
      SELECT p.*,coalesce(sp.unit_kind,'commune') unit_kind,sp.unit_parents,
             coalesce(sp.max_submissions,0) max_submissions,
             e.action,e.units reopen_units,e.seq event_seq,e.id event_id,
             cardinality(p.units)<>(SELECT count(DISTINCT u) FROM unnest(p.units) u) duplicate_units
        FROM pf_forms.period p
        LEFT JOIN pf_forms.period_spec sp ON sp.tenant_id=p.tenant_id AND sp.period_id=p.id
        LEFT JOIN LATERAL (
          SELECT v.id,v.action,v.units,v.seq FROM pf_forms.period_event v
           WHERE v.tenant_id=p.tenant_id AND v.period_ref=p.id AND v.origin='native'
           ORDER BY v.seq DESC LIMIT 1
        ) e ON true
       WHERE p.profile=$1 AND coalesce(sp.unit_kind,'commune')=$2
         AND (cardinality($3::uuid[])=0 OR p.id=ANY($3::uuid[]))
         AND (cardinality($5::text[])=0 OR p.template_code=ANY($5::text[]))
         AND ($6 OR p.id=ANY($7::uuid[]))
         AND ($10 OR p.template_code=ANY($11::text[]))
         AND ($12 OR p.owner_ref=ANY($13::uuid[]))
    ), targets AS MATERIALIZED (
      SELECT p.id period_id,p.label,p.template_code,p.action,p.reopen_units,p.event_seq,p.event_id,
             p.duplicate_units,p.max_submissions,u.code unit_code,
             coalesce(nullif(p.unit_parents[u.ord],''),u.code) parent
        FROM periods p CROSS JOIN LATERAL unnest(p.units) WITH ORDINALITY u(code,ord)
       WHERE (cardinality($4::text[])=0 OR u.code=ANY($4::text[]))
         AND ($8 OR u.code=ANY($9::text[]))
         AND (CASE WHEN $2='department' THEN $16 OR u.code=$17
                   ELSE $14 OR coalesce(nullif(p.unit_parents[u.ord],''),u.code)=ANY($15::text[]) END)
    ), basis AS MATERIALIZED (
      SELECT t.*,x.id submission_id,x.seq attempt,x.event_time submitted_at,
             x.same_attempt,x.same_identity,r.decision,r.reviews,r.id review_id,
             (t.action IS NULL OR t.action NOT IN ('published','reopened','closed','cancelled')
              OR t.event_seq<1 OR t.duplicate_units OR t.max_submissions<0 OR t.max_submissions>20
              OR t.unit_code!~'^[A-Za-z0-9_.-]{1,40}$'
              OR (x.id IS NOT NULL AND (x.seq<1 OR x.event_time IS NULL OR x.same_attempt<>1 OR x.same_identity<>1))
              OR coalesce(r.reviews,0)>1 OR (r.decision IS NOT NULL AND r.decision NOT IN ('accepted','returned'))) invalid,
             x.id IS NOT NULL AND r.id IS NULL
               AND (t.action='published' OR (t.action='reopened' AND (cardinality(t.reopen_units)=0 OR t.unit_code=ANY(t.reopen_units))))
               AND (t.max_submissions=0 OR x.seq<t.max_submissions) pending
        FROM targets t
        LEFT JOIN LATERAL (
          SELECT b.id,b.seq,b.event_time,count(*) OVER(PARTITION BY b.seq) same_attempt,
                 count(*) OVER(PARTITION BY b.id) same_identity
            FROM pf_forms.submission b WHERE b.tenant_id=current_setting('app.tenant_id')::uuid
             AND b.period_id=t.period_id AND b.unit_code=t.unit_code
           ORDER BY b.seq DESC,b.id DESC LIMIT 1
        ) x ON true
        LEFT JOIN LATERAL (
          SELECT v.id,v.decision,count(*) OVER() reviews FROM pf_forms.review v
           WHERE v.tenant_id=current_setting('app.tenant_id')::uuid AND v.submission_id=x.id
           ORDER BY v.seq DESC LIMIT 1
        ) r ON true
    ), summary AS (
      SELECT count(*) candidates,count(*) FILTER(WHERE pending) total,
             coalesce(bool_or(invalid),false)
               OR count(*)<>count(DISTINCT (period_id,unit_code))
               OR count(submission_id)<>count(DISTINCT submission_id) invalid,
             coalesce(bool_or(pending AND period_id=$18::uuid AND unit_code=$19),false) anchor
        FROM basis
    ), page AS (
      SELECT period_id,label,template_code,unit_code,submission_id,attempt,submitted_at
        FROM basis WHERE pending AND ($18::uuid='00000000-0000-0000-0000-000000000000'::uuid
          OR (period_id,unit_code)>($18::uuid,$19::text))
       ORDER BY period_id,unit_code LIMIT $20
    )
    SELECT summary.total,summary.candidates,encode(sha256(convert_to(jsonb_build_array(current_database(),pg_postmaster_start_time(),pg_current_snapshot()::text)::text,'UTF8')),'hex'),summary.invalid,summary.anchor,
           coalesce((SELECT jsonb_agg(jsonb_build_object('period_id',period_id,'period_label',label,
             'template_code',template_code,'unit',unit_code,'submission_id',submission_id,
             'attempt',attempt,'submitted_at',submitted_at,'decision','','status','submitted') ORDER BY period_id,unit_code)
             FROM page),'[]'::jsonb)
      FROM summary`, q.Profile, q.UnitKind, nonNilUUID(q.PeriodIDs), nonNilString(q.Units), nonNilString(q.TemplateCodes),
		s.AllPeriods, nonNilUUID(s.PeriodIDs), s.AllUnits, nonNilString(s.Units), s.AllTemplates, nonNilString(s.TemplateCodes),
		s.AllOwners, nonNilUUID(s.OwnerNodes), in.NativeAll, nonNilString(in.NativeUnits), in.NativeDepartmentAll, in.NativeDepartment,
		in.AfterPeriod, in.AfterUnit, q.Limit+1).Scan(&out.Count, &out.Candidates, &out.Visibility, &out.Invalid, &out.AnchorFound, &raw)
	if err != nil {
		return PendingResult{}, err
	}
	if err = json.Unmarshal(raw, &out.Items); err != nil {
		return PendingResult{}, err
	}
	out.CoverageComplete = true
	return out, nil
}

func nonNilUUID(v []uuid.UUID) []string {
	out := make([]string, len(v))
	for i, id := range v {
		out[i] = id.String()
	}
	return out
}
func nonNilString(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}
