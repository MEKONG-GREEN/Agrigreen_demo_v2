package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
)

// g3Ledgers là các sổ G3 có kiểm idem_key trước khi ghi (consumer: lỗi trong
// giao dịch của inbox.Process làm hỏng cả giao dịch, nên không dựa vào 23505).
var g3Ledgers = map[string]bool{
	"season_link": true, "farm_log_link": true, "collection_evidence": true, "log_comparison": true, "estimate": true, "alert": true,
}

// IdemExists cho biết sổ G3 đã có dòng với idem_key.
func IdemExists(ctx context.Context, tx pgx.Tx, ledger, key string) (bool, error) {
	if !g3Ledgers[ledger] {
		return false, fmt.Errorf("mrv: sổ %q không thuộc G3", ledger)
	}
	var ok bool
	err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pf_mrv."+ledger+" WHERE idem_key = $1::text)", key).Scan(&ok)
	return ok, err
}

// SystemActor là tác nhân của các dòng do consumer ghi (không phải người).
var SystemActor = uuid.MustParse("00000000-0000-7000-8000-00000000c001")

// ---------------------------------------------------------------------------
// Tham chiếu vụ

// SeasonRow là một dòng sổ tham chiếu vụ.
type SeasonRow struct {
	SeasonRef   uuid.UUID
	PlotRef     uuid.UUID
	CommuneCode string
	SeasonCode  string
	SeasonYear  int
	Status      string
	SowingDate  *time.Time
	SourceEvent string
	EventTime   time.Time
	ActorRef    uuid.UUID
	IdemKey     string
	RecordedAt  time.Time
}

// InsertSeasonLink ghi sổ tham chiếu vụ.
func InsertSeasonLink(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r *SeasonRow) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_mrv.season_link
	    (id, tenant_id, stream_key, season_ref, plot_ref, commune_code, season_code, season_year, status, sowing_date,
	     source_event, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15) RETURNING recorded_at`,
		ids.New(), tid, "season:"+r.SeasonRef.String(), r.SeasonRef, r.PlotRef, r.CommuneCode, r.SeasonCode, r.SeasonYear,
		r.Status, r.SowingDate, r.SourceEvent, r.EventTime.UTC(), r.ActorRef, r.IdemKey, domain.RuleVersionG3).Scan(&r.RecordedAt)
}

// seasonHeads: bản mới nhất (theo thời điểm sự việc) của mỗi vụ, ghi tới as_of ($1, NULL = hiện tại).
const seasonHeads = `SELECT DISTINCT ON (season_ref) season_ref, plot_ref, commune_code, season_code, season_year, status,
    sowing_date, source_event, event_time, actor_ref, idem_key, recorded_at
  FROM pf_mrv.season_link WHERE ($1::timestamptz IS NULL OR recorded_at <= $1::timestamptz)
  ORDER BY season_ref, event_time DESC, recorded_at DESC, seq DESC`

func scanSeason(row pgx.Row) (SeasonRow, error) {
	var r SeasonRow
	err := row.Scan(&r.SeasonRef, &r.PlotRef, &r.CommuneCode, &r.SeasonCode, &r.SeasonYear, &r.Status, &r.SowingDate,
		&r.SourceEvent, &r.EventTime, &r.ActorRef, &r.IdemKey, &r.RecordedAt)
	return r, err
}

// SeasonAt trả tham chiếu vụ tại as_of (nil = hiện tại); nil khi chưa biết vụ.
func SeasonAt(ctx context.Context, tx pgx.Tx, seasonRef uuid.UUID, asOf *time.Time) (*SeasonRow, error) {
	r, err := scanSeason(tx.QueryRow(ctx, "SELECT * FROM ("+seasonHeads+") h WHERE h.season_ref = $2", asOf, seasonRef))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// SeasonFilter lọc danh sách vụ.
type SeasonFilter struct {
	All        bool
	Units      []string
	Unit       string
	SeasonCode string
	PlotRef    *uuid.UUID
}

func units(u []string) []string {
	if u == nil {
		return []string{}
	}
	return u
}

// Seasons phân trang vụ hiện hành trong phạm vi theo season_ref (R-53).
func Seasons(ctx context.Context, tx pgx.Tx, f SeasonFilter, after uuid.UUID, limit int) ([]SeasonRow, error) {
	rows, err := tx.Query(ctx, "SELECT * FROM ("+seasonHeads+`) h
	  WHERE h.season_ref > $2 AND ($3::bool OR h.commune_code = ANY($4::text[]))
	    AND ($5::text = '' OR h.commune_code = $5::text) AND ($6::text = '' OR h.season_code = $6::text)
	    AND ($7::uuid IS NULL OR h.plot_ref = $7::uuid)
	  ORDER BY h.season_ref LIMIT $8`, nil, after, f.All, units(f.Units), f.Unit, f.SeasonCode, f.PlotRef, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (SeasonRow, error) { return scanSeason(r) })
}

// SeasonRefsOf trả các vụ hiện hành của một thửa với mã vụ (nối minh chứng KN ↔ vụ).
func SeasonRefsOf(ctx context.Context, tx pgx.Tx, plotRef uuid.UUID, seasonCode string) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, "SELECT h.season_ref FROM ("+seasonHeads+") h WHERE h.plot_ref = $2 AND h.season_code = $3::text ORDER BY h.season_ref",
		nil, plotRef, seasonCode)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// ---------------------------------------------------------------------------
// Liên kết nhật ký HTX

// FarmLogRow là một dòng sổ liên kết nhật ký.
type FarmLogRow struct {
	Kind           string
	LogID          uuid.UUID
	LogRef         uuid.UUID
	LogVersion     *int64
	SeasonRef      uuid.UUID
	PlotRef        *uuid.UUID
	CommuneCode    *string
	ActivityCode   *string
	LocationStatus *string
	BecameCurrent  *bool
	Decision       *string
	AuthorRef      uuid.UUID
	CatalogVersion *int
	LogEventTime   time.Time
	EventTime      time.Time
	IdemKey        string
}

// InsertFarmLogLink ghi sổ liên kết nhật ký.
func InsertFarmLogLink(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r FarmLogRow) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_mrv.farm_log_link
	    (id, tenant_id, stream_key, kind, log_id, log_ref, log_version, season_ref, plot_ref, commune_code, activity_code,
	     location_status, became_current, decision, author_ref, catalog_version, log_event_time, event_time, actor_ref,
	     idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)`,
		ids.New(), tid, "log:"+r.LogRef.String(), r.Kind, r.LogID, r.LogRef, r.LogVersion, r.SeasonRef, r.PlotRef, r.CommuneCode,
		r.ActivityCode, r.LocationStatus, r.BecameCurrent, r.Decision, r.AuthorRef, r.CatalogVersion, r.LogEventTime.UTC(),
		r.EventTime.UTC(), SystemActor, r.IdemKey, domain.RuleVersionG3)
	return err
}

// FarmLogCurrent là bản hiện hành của một nhật ký đã liên kết.
type FarmLogCurrent struct {
	LogRef         uuid.UUID
	LogID          uuid.UUID
	LogVersion     int64
	SeasonRef      uuid.UUID
	PlotRef        uuid.UUID
	CommuneCode    string
	ActivityCode   string
	LocationStatus string
	AuthorRef      uuid.UUID
	CatalogVersion *int
	LogEventTime   time.Time
	Decision       *string // quyết định duyệt HTX mới nhất của bản hiện hành (approved | needs_supplement)
	RowHash        []byte
}

// FarmLogFilter lọc nhật ký hiện hành.
type FarmLogFilter struct {
	SeasonRef *uuid.UUID
	All       bool
	Units     []string
	AsOf      *time.Time
}

// FarmLogs trả bản hiện hành (phiên bản nhật ký lớn nhất) của mỗi log_ref, kèm
// quyết định duyệt mới nhất của đúng bản đó, ghi tới as_of. Chỉ đọc sổ.
func FarmLogs(ctx context.Context, tx pgx.Tx, f FarmLogFilter) ([]FarmLogCurrent, error) {
	rows, err := tx.Query(ctx, `WITH heads AS (
	    SELECT DISTINCT ON (log_ref) * FROM pf_mrv.farm_log_link
	    WHERE kind = 'recorded' AND ($1::timestamptz IS NULL OR recorded_at <= $1::timestamptz)
	      AND ($2::uuid IS NULL OR season_ref = $2::uuid)
	    ORDER BY log_ref, log_version DESC, recorded_at DESC, seq DESC)
	  SELECT h.log_ref, h.log_id, h.log_version, h.season_ref, h.plot_ref, h.commune_code, h.activity_code,
	         coalesce(h.location_status, ''), h.author_ref, h.catalog_version, h.log_event_time, r.decision, h.row_hash
	  FROM heads h
	  LEFT JOIN LATERAL (SELECT x.decision FROM pf_mrv.farm_log_link x
	                     WHERE x.kind = 'reviewed' AND x.log_id = h.log_id
	                       AND ($1::timestamptz IS NULL OR x.recorded_at <= $1::timestamptz)
	                     ORDER BY x.log_event_time DESC, x.recorded_at DESC, x.seq DESC LIMIT 1) r ON true
	  WHERE ($3::bool OR h.commune_code = ANY($4::text[]))
	  ORDER BY h.log_ref`, f.AsOf, f.SeasonRef, f.All, units(f.Units))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (FarmLogCurrent, error) {
		var c FarmLogCurrent
		err := r.Scan(&c.LogRef, &c.LogID, &c.LogVersion, &c.SeasonRef, &c.PlotRef, &c.CommuneCode, &c.ActivityCode,
			&c.LocationStatus, &c.AuthorRef, &c.CatalogVersion, &c.LogEventTime, &c.Decision, &c.RowHash)
		return c, err
	})
}

// ---------------------------------------------------------------------------
// Bằng chứng thu gom (van)

// CollectionRow là một dòng sổ bằng chứng thu gom.
type CollectionRow struct {
	EvidenceRef   uuid.UUID
	Revision      int64
	PlotRef       uuid.UUID
	SeasonRef     *uuid.UUID
	CollectedOn   time.Time
	Bucket        string
	BucketRule    string
	SourceEventID uuid.UUID
	EventTime     time.Time
	IdemKey       string
	RecordedAt    time.Time
	RowHash       []byte
}

// CollectionIdemKey: một dòng cho mỗi (evidence_ref, revision).
func CollectionIdemKey(ref uuid.UUID, rev int64) string { return fmt.Sprintf("rev:%s:%d", ref, rev) }

// InsertCollection ghi sổ bằng chứng thu gom.
func InsertCollection(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r *CollectionRow) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_mrv.collection_evidence
	    (id, tenant_id, stream_key, evidence_ref, revision, plot_ref, season_ref, collected_on, collected_kg_bucket,
	     bucket_rule_version, source_event_id, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15) RETURNING recorded_at, row_hash`,
		ids.New(), tid, "collection:"+r.EvidenceRef.String(), r.EvidenceRef, r.Revision, r.PlotRef, r.SeasonRef, r.CollectedOn,
		r.Bucket, r.BucketRule, r.SourceEventID, r.EventTime.UTC(), SystemActor, r.IdemKey, domain.RuleVersionG3).
		Scan(&r.RecordedAt, &r.RowHash)
}

// CollectionFilter lọc bằng chứng thu gom hiện hành.
type CollectionFilter struct {
	SeasonRef *uuid.UUID
	All       bool
	Units     []string // lọc theo xã của vụ tại as_of; bằng chứng chưa gắn vụ chỉ thấy khi All
	AsOf      *time.Time
}

// Collections trả bản hiện hành (revision lớn nhất) của mỗi evidence_ref ghi tới as_of.
func Collections(ctx context.Context, tx pgx.Tx, f CollectionFilter) ([]CollectionRow, error) {
	rows, err := tx.Query(ctx, `WITH heads AS (
	    SELECT DISTINCT ON (evidence_ref) * FROM pf_mrv.collection_evidence
	    WHERE ($1::timestamptz IS NULL OR recorded_at <= $1::timestamptz)
	    ORDER BY evidence_ref, revision DESC, recorded_at DESC),
	  seasons AS (`+seasonHeads+`)
	  SELECT c.evidence_ref, c.revision, c.plot_ref, c.season_ref, c.collected_on, c.collected_kg_bucket,
	         c.bucket_rule_version, c.source_event_id, c.event_time, c.idem_key, c.recorded_at, c.row_hash
	  FROM heads c LEFT JOIN seasons s ON s.season_ref = c.season_ref
	  WHERE ($2::uuid IS NULL OR c.season_ref = $2::uuid)
	    AND ($3::bool OR s.commune_code = ANY($4::text[]))
	  ORDER BY c.evidence_ref`, f.AsOf, f.SeasonRef, f.All, units(f.Units))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (CollectionRow, error) {
		var c CollectionRow
		err := r.Scan(&c.EvidenceRef, &c.Revision, &c.PlotRef, &c.SeasonRef, &c.CollectedOn, &c.Bucket, &c.BucketRule,
			&c.SourceEventID, &c.EventTime, &c.IdemKey, &c.RecordedAt, &c.RowHash)
		return c, err
	})
}

// ---------------------------------------------------------------------------
// Minh chứng cán bộ KN của một thửa/vụ (bản hiện hành)

// OfficerRecords trả bản hiện hành của mọi chuỗi minh chứng tại thửa + mã vụ.
func OfficerRecords(ctx context.Context, tx pgx.Tx, plotRef uuid.UUID, seasonCode string) ([]domain.OfficerRecord, error) {
	rows, err := tx.Query(ctx, `SELECT e.id, e.root_id, e.evidence_type, coalesce(e.field_values->>'activity_code', ''),
	    e.event_time, c.review_status
	  FROM pf_mrv.evidence_current c JOIN pf_mrv.evidence e ON e.id = c.head_id
	  WHERE e.plot_ref = $1 AND c.season_code = $2::text
	  ORDER BY e.event_time, e.id`, plotRef, seasonCode)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.OfficerRecord, error) {
		var o domain.OfficerRecord
		var st string
		err := r.Scan(&o.EvidenceID, &o.RootID, &o.EvidenceType, &o.ActivityCode, &o.EventTime, &st)
		o.ReviewStatus = domain.ReviewStatus(st)
		return o, err
	})
}

// HasAcceptedAt cho biết thửa + mã vụ có minh chứng cán bộ "Đạt chuẩn" (bản hiện
// hành tại as_of, chỉ đọc sổ) — KN US-MRV-05.
func HasAcceptedAt(ctx context.Context, tx pgx.Tx, plotRef uuid.UUID, seasonCode string, asOf *time.Time) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `WITH heads AS (
	    SELECT DISTINCT ON (root_id) id, plot_ref, season_code FROM pf_mrv.evidence
	    WHERE ($1::timestamptz IS NULL OR recorded_at <= $1::timestamptz)
	    ORDER BY root_id, chain_version DESC)
	  SELECT EXISTS (SELECT 1 FROM heads h
	    JOIN LATERAL (SELECT x.decision FROM pf_mrv.evidence_review x WHERE x.evidence_id = h.id
	                    AND ($1::timestamptz IS NULL OR x.recorded_at <= $1::timestamptz)
	                  ORDER BY x.recorded_at DESC, x.seq DESC LIMIT 1) r ON true
	    WHERE h.plot_ref = $2 AND h.season_code = $3::text AND r.decision = 'accepted')`, asOf, plotRef, seasonCode).Scan(&ok)
	return ok, err
}

// ---------------------------------------------------------------------------
// Đối chiếu (KN US-MRV-02)

// ComparisonRow là một dòng sổ đối chiếu.
type ComparisonRow struct {
	ID               uuid.UUID
	EvidenceID       uuid.UUID
	EvidenceRootID   uuid.UUID
	LogRef           uuid.UUID
	LogID            uuid.UUID
	SeasonRef        uuid.UUID
	PlotRef          uuid.UUID
	AdminUnitCode    string
	Result           string
	Note             *string
	FileRefs         []domain.FileRef
	EventTime        time.Time
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	ActorRef         uuid.UUID
	IdemKey          string
	Seq              int64
	RecordedAt       time.Time
	RowHash          []byte
}

const comparisonCols = `id, evidence_id, evidence_root_id, log_ref, log_id, season_ref, plot_ref, admin_unit_code, result, note,
  file_refs::text, event_time, supersedes_id, correction_reason, actor_ref, idem_key, seq, recorded_at, row_hash`

func scanComparison(row pgx.Row) (ComparisonRow, error) {
	var c ComparisonRow
	var files string
	err := row.Scan(&c.ID, &c.EvidenceID, &c.EvidenceRootID, &c.LogRef, &c.LogID, &c.SeasonRef, &c.PlotRef, &c.AdminUnitCode,
		&c.Result, &c.Note, &files, &c.EventTime, &c.SupersedesID, &c.CorrectionReason, &c.ActorRef, &c.IdemKey, &c.Seq,
		&c.RecordedAt, &c.RowHash)
	if err != nil {
		return c, err
	}
	err = json.Unmarshal([]byte(files), &c.FileRefs)
	return c, err
}

func oneComparison(ctx context.Context, tx pgx.Tx, where string, args ...any) (*ComparisonRow, error) {
	c, err := scanComparison(tx.QueryRow(ctx, "SELECT "+comparisonCols+" FROM pf_mrv.log_comparison WHERE "+where, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ComparisonByIdem tìm dòng đối chiếu đã ghi với idem_key.
func ComparisonByIdem(ctx context.Context, tx pgx.Tx, key string) (*ComparisonRow, error) {
	return oneComparison(ctx, tx, "idem_key = $1::text"+ledger.NewestByIdem(""), key)
}

// ComparisonByID đọc một dòng đối chiếu.
func ComparisonByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*ComparisonRow, error) {
	return oneComparison(ctx, tx, "id = $1", id)
}

// ComparisonSuperseded cho biết dòng đã có bản ghi bù.
func ComparisonSuperseded(ctx context.Context, tx pgx.Tx, id uuid.UUID) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pf_mrv.log_comparison WHERE supersedes_id = $1)", id).Scan(&ok)
	return ok, err
}

// LockComparisons khóa chuỗi đối chiếu của một vụ (ghi tuần tự bản ghi bù).
func LockComparisons(ctx context.Context, tx pgx.Tx, tid, seasonRef uuid.UUID) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))", "mrv.compare:"+tid.String()+":"+seasonRef.String())
	return err
}

// InsertComparison ghi sổ đối chiếu.
func InsertComparison(ctx context.Context, tx pgx.Tx, tid uuid.UUID, c *ComparisonRow) error {
	files := c.FileRefs
	if files == nil {
		files = []domain.FileRef{}
	}
	fb, err := json.Marshal(files)
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_mrv.log_comparison
	    (id, tenant_id, stream_key, evidence_id, evidence_root_id, log_ref, log_id, season_ref, plot_ref, admin_unit_code,
	     result, note, file_refs, event_time, supersedes_id, correction_reason, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb, $14, $15, $16, $17, $18, $19)
	  RETURNING seq, recorded_at, row_hash`,
		c.ID, tid, domain.StreamKey(c.AdminUnitCode), c.EvidenceID, c.EvidenceRootID, c.LogRef, c.LogID, c.SeasonRef, c.PlotRef,
		c.AdminUnitCode, c.Result, c.Note, string(fb), c.EventTime.UTC(), c.SupersedesID, c.CorrectionReason, c.ActorRef,
		c.IdemKey, domain.RuleVersionG3).Scan(&c.Seq, &c.RecordedAt, &c.RowHash)
}

// ComparisonFilter lọc đối chiếu hiện hành.
type ComparisonFilter struct {
	SeasonRef *uuid.UUID
	All       bool
	Units     []string
	AsOf      *time.Time
}

// Comparisons trả các dòng đối chiếu chưa bị bù (tại as_of).
func Comparisons(ctx context.Context, tx pgx.Tx, f ComparisonFilter) ([]ComparisonRow, error) {
	rows, err := tx.Query(ctx, "SELECT "+comparisonCols+` FROM pf_mrv.log_comparison c
	  WHERE ($1::timestamptz IS NULL OR c.recorded_at <= $1::timestamptz)
	    AND ($2::uuid IS NULL OR c.season_ref = $2::uuid)
	    AND ($3::bool OR c.admin_unit_code = ANY($4::text[]))
	    AND NOT EXISTS (SELECT 1 FROM pf_mrv.log_comparison n WHERE n.supersedes_id = c.id
	                      AND ($1::timestamptz IS NULL OR n.recorded_at <= $1::timestamptz))
	  ORDER BY c.id`, f.AsOf, f.SeasonRef, f.All, units(f.Units))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ComparisonRow, error) { return scanComparison(r) })
}

// ---------------------------------------------------------------------------
// Phương pháp ước tính (quy trình duyệt)

// MethodRow là một phiên bản phương pháp.
type MethodRow struct {
	ID           uuid.UUID
	RuleVersion  string
	Version      int
	Status       string
	Params       []domain.ParamPin
	SourceNote   string
	SetterRef    uuid.UUID
	ApproverRef  *uuid.UUID
	DecidedAt    *time.Time
	DecisionNote *string
	IdemKey      string
	CreatedAt    time.Time
}

const methodCols = `id, rule_version, version, status, params::text, source_note, setter_ref, approver_ref, decided_at,
  decision_note, idem_key, created_at`

func scanMethod(row pgx.Row) (MethodRow, error) {
	var m MethodRow
	var params string
	err := row.Scan(&m.ID, &m.RuleVersion, &m.Version, &m.Status, &params, &m.SourceNote, &m.SetterRef, &m.ApproverRef,
		&m.DecidedAt, &m.DecisionNote, &m.IdemKey, &m.CreatedAt)
	if err != nil {
		return m, err
	}
	err = json.Unmarshal([]byte(params), &m.Params)
	return m, err
}

func oneMethod(ctx context.Context, tx pgx.Tx, where string, args ...any) (*MethodRow, error) {
	m, err := scanMethod(tx.QueryRow(ctx, "SELECT "+methodCols+" FROM pf_mrv.method_version WHERE "+where, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// MethodByIdem tìm phiên bản đã đề xuất với idem_key.
func MethodByIdem(ctx context.Context, tx pgx.Tx, key string) (*MethodRow, error) {
	return oneMethod(ctx, tx, "idem_key = $1::text", key)
}

// MethodByID đọc một phiên bản; forUpdate khóa dòng.
func MethodByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*MethodRow, error) {
	q := "id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	return oneMethod(ctx, tx, q, id)
}

// ApprovedMethod trả phiên bản ĐÃ DUYỆT mới nhất của một rule_version.
func ApprovedMethod(ctx context.Context, tx pgx.Tx, ruleVersion string) (*MethodRow, error) {
	return oneMethod(ctx, tx, "rule_version = $1::text AND status = 'approved' ORDER BY version DESC LIMIT 1", ruleVersion)
}

// InsertMethod ghi đề xuất phương pháp (trạng thái submitted); số phiên bản tăng dần theo rule_version.
func InsertMethod(ctx context.Context, tx pgx.Tx, tid uuid.UUID, m *MethodRow) error {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))", "mrv.method:"+tid.String()+":"+m.RuleVersion); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, "SELECT coalesce(max(version), 0) + 1 FROM pf_mrv.method_version WHERE rule_version = $1::text",
		m.RuleVersion).Scan(&m.Version); err != nil {
		return err
	}
	pb, err := json.Marshal(m.Params)
	if err != nil {
		return err
	}
	m.Status = "submitted"
	return tx.QueryRow(ctx, `INSERT INTO pf_mrv.method_version (id, tenant_id, rule_version, version, status, params, source_note,
	    setter_ref, idem_key) VALUES ($1, $2, $3, $4, 'submitted', $5::jsonb, $6, $7, $8) RETURNING created_at`,
		m.ID, tid, m.RuleVersion, m.Version, string(pb), m.SourceNote, m.SetterRef, m.IdemKey).Scan(&m.CreatedAt)
}

// DecideMethod ghi quyết định duyệt/từ chối.
func DecideMethod(ctx context.Context, tx pgx.Tx, id uuid.UUID, status string, approver uuid.UUID, at time.Time, note *string) error {
	_, err := tx.Exec(ctx, `UPDATE pf_mrv.method_version SET status = $2, approver_ref = $3, decided_at = $4, decision_note = $5
	  WHERE id = $1`, id, status, approver, at.UTC(), note)
	return err
}

// Methods liệt kê phiên bản phương pháp theo id (R-53).
func Methods(ctx context.Context, tx pgx.Tx, after uuid.UUID, limit int) ([]MethodRow, error) {
	rows, err := tx.Query(ctx, "SELECT "+methodCols+" FROM pf_mrv.method_version WHERE id > $1 ORDER BY id LIMIT $2", after, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (MethodRow, error) { return scanMethod(r) })
}

// ---------------------------------------------------------------------------
// Sổ ước tính

// EstimateRow là một dòng sổ ước tính.
type EstimateRow struct {
	ID               uuid.UUID
	SeasonRef        uuid.UUID
	PlotRef          uuid.UUID
	CommuneCode      string
	SeasonCode       string
	InputSHA256      string
	CountedTonnes    decimal.Decimal
	AvoidedKg        decimal.Decimal
	TransportKg      *decimal.Decimal
	TransportStatus  string
	Inputs           []domain.EstimateInput
	Params           []domain.ParamUse
	Official         bool
	MethodVersionID  *uuid.UUID
	EventTime        time.Time
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	ActorRef         uuid.UUID
	IdemKey          string
	RuleVersion      string
	Seq              int64
	RecordedAt       time.Time
	RowHash          []byte
}

const estimateCols = `id, season_ref, plot_ref, commune_code, season_code, input_sha256, counted_tonnes::text,
  avoided_burning_kgco2e::text, transport_kgco2e::text, transport_status, inputs::text, params::text, official,
  method_version_id, event_time, supersedes_id, correction_reason, actor_ref, idem_key, rule_version, seq, recorded_at, row_hash`

func scanEstimate(row pgx.Row) (EstimateRow, error) {
	var e EstimateRow
	var tonnes, avoided string
	var transport *string
	var inputs, params string
	err := row.Scan(&e.ID, &e.SeasonRef, &e.PlotRef, &e.CommuneCode, &e.SeasonCode, &e.InputSHA256, &tonnes, &avoided,
		&transport, &e.TransportStatus, &inputs, &params, &e.Official, &e.MethodVersionID, &e.EventTime, &e.SupersedesID,
		&e.CorrectionReason, &e.ActorRef, &e.IdemKey, &e.RuleVersion, &e.Seq, &e.RecordedAt, &e.RowHash)
	if err != nil {
		return e, err
	}
	if e.CountedTonnes, err = decimal.NewFromString(tonnes); err != nil {
		return e, err
	}
	if e.AvoidedKg, err = decimal.NewFromString(avoided); err != nil {
		return e, err
	}
	if e.TransportKg, err = parseDec(transport); err != nil {
		return e, err
	}
	if err := json.Unmarshal([]byte(inputs), &e.Inputs); err != nil {
		return e, err
	}
	err = json.Unmarshal([]byte(params), &e.Params)
	return e, err
}

// LatestEstimate trả dòng ước tính mới nhất của một vụ (nil khi chưa có).
func LatestEstimate(ctx context.Context, tx pgx.Tx, seasonRef uuid.UUID) (*EstimateRow, error) {
	e, err := scanEstimate(tx.QueryRow(ctx, "SELECT "+estimateCols+" FROM pf_mrv.estimate WHERE season_ref = $1 ORDER BY seq DESC LIMIT 1", seasonRef))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// LockEstimate khóa việc tính lại của một vụ.
func LockEstimate(ctx context.Context, tx pgx.Tx, tid, seasonRef uuid.UUID) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))", "mrv.estimate:"+tid.String()+":"+seasonRef.String())
	return err
}

// InsertEstimate ghi sổ ước tính.
func InsertEstimate(ctx context.Context, tx pgx.Tx, tid uuid.UUID, e *EstimateRow) error {
	inputs, err := json.Marshal(e.Inputs)
	if err != nil {
		return err
	}
	params, err := json.Marshal(e.Params)
	if err != nil {
		return err
	}
	e.ID = ids.New()
	return tx.QueryRow(ctx, `INSERT INTO pf_mrv.estimate
	    (id, tenant_id, stream_key, season_ref, plot_ref, commune_code, season_code, input_sha256, counted_tonnes,
	     avoided_burning_kgco2e, transport_kgco2e, transport_status, inputs, params, official, method_version_id, event_time,
	     supersedes_id, correction_reason, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::text::numeric, $10::text::numeric, $11::text::numeric, $12, $13::jsonb,
	     $14::jsonb, $15, $16, $17, $18, $19, $20, $21, $22)
	  RETURNING seq, recorded_at, row_hash`,
		e.ID, tid, "estimate:"+e.SeasonRef.String(), e.SeasonRef, e.PlotRef, e.CommuneCode, e.SeasonCode, e.InputSHA256,
		e.CountedTonnes.StringFixed(3), e.AvoidedKg.StringFixed(3), decStr(e.TransportKg), e.TransportStatus, string(inputs),
		string(params), e.Official, e.MethodVersionID, e.EventTime.UTC(), e.SupersedesID, e.CorrectionReason, e.ActorRef,
		e.IdemKey, e.RuleVersion).Scan(&e.Seq, &e.RecordedAt, &e.RowHash)
}

// EstimateFilter lọc ước tính mới nhất theo vụ.
type EstimateFilter struct {
	SeasonRef *uuid.UUID
	All       bool
	Units     []string
	Unit      string
	AsOf      *time.Time
	After     uuid.UUID
	Limit     int // 0 = không giới hạn
}

// Estimates trả dòng ước tính mới nhất của mỗi vụ (tại as_of), theo season_ref.
func Estimates(ctx context.Context, tx pgx.Tx, f EstimateFilter) ([]EstimateRow, error) {
	limit := any(nil)
	if f.Limit > 0 {
		limit = f.Limit
	}
	rows, err := tx.Query(ctx, "SELECT "+estimateCols+` FROM (
	    SELECT DISTINCT ON (season_ref) * FROM pf_mrv.estimate
	    WHERE ($1::timestamptz IS NULL OR recorded_at <= $1::timestamptz)
	    ORDER BY season_ref, seq DESC) e
	  WHERE e.season_ref > $2 AND ($3::uuid IS NULL OR e.season_ref = $3::uuid)
	    AND ($4::bool OR e.commune_code = ANY($5::text[])) AND ($6::text = '' OR e.commune_code = $6::text)
	  ORDER BY e.season_ref LIMIT $7`, f.AsOf, f.After, f.SeasonRef, f.All, units(f.Units), f.Unit, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (EstimateRow, error) { return scanEstimate(r) })
}
