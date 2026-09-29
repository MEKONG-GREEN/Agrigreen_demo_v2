package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/domain"
)

// FarmLog là một phiên bản nhật ký (một dòng sổ farm_log).
type FarmLog struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	Version          int64 // = seq trong chuỗi của nhật ký
	LogRef           uuid.UUID
	SeasonRef        uuid.UUID
	PlotRef          uuid.UUID
	HouseholdRef     uuid.UUID
	CooperativeRef   uuid.UUID
	CommuneCode      string
	ActivityCode     string
	SOPStepSeq       *int
	GPS              domain.GPS
	LocationStatus   string
	DistanceM        *decimal.Decimal
	MachineCodes     []string
	Inputs           []domain.Input
	Note             string
	FileRefs         []uuid.UUID
	BatchRef         uuid.UUID
	BecameCurrent    bool
	OnBehalf         bool
	Channel          string
	EventTime        time.Time
	RecordedAt       time.Time
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	ActorRef         uuid.UUID
	DeviceID         *string
	IdemKey          string
}

// StreamLog là khóa chuỗi của nhật ký.
func StreamLog(logRef uuid.UUID) string { return "farm_log:" + logRef.String() }

func inputsJSON(in []domain.Input) (string, error) {
	if in == nil {
		in = []domain.Input{}
	}
	b, err := json.Marshal(in)
	return string(b), err
}

// InsertFarmLog ghi một phiên bản nhật ký; DB tính seq và băm.
func InsertFarmLog(ctx context.Context, tx pgx.Tx, l *FarmLog) error {
	inputs, err := inputsJSON(l.Inputs)
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_cropcycle.farm_log
	    (id, tenant_id, stream_key, log_ref, season_ref, plot_ref, household_ref, cooperative_ref, commune_code, activity_code,
	     sop_step_seq, gps, gps_accuracy_m, gps_source, location_status, distance_m, machine_codes, inputs, note, file_refs,
	     batch_ref, became_current, on_behalf, channel, event_time, supersedes_id, correction_reason, actor_ref, device_id,
	     idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::uuid, $6::uuid, $7::uuid, $8::uuid, $9::text, $10::text,
	     $11::int, `+gpsInsertSQL("$12", "$13")+`, $14::numeric, $15::text, $16::text, $17::numeric, $18::text[], $19::jsonb, $20::text, $21::uuid[],
	     $22::uuid, $23::boolean, $24::boolean, $25::text, $26::timestamptz, $27::uuid, $28::text, $29::uuid, $30::text,
	     $31::text, $32::text)
	  RETURNING seq, recorded_at`,
		l.ID, l.TenantID, StreamLog(l.LogRef), l.LogRef, l.SeasonRef, l.PlotRef, nilIfZero(l.HouseholdRef), l.CooperativeRef, l.CommuneCode, l.ActivityCode,
		l.SOPStepSeq, decPtr(l.GPS.Lon), decPtr(l.GPS.Lat), decPtr(l.GPS.AccuracyM), l.GPS.Source, l.LocationStatus, decPtr(l.DistanceM),
		strs(l.MachineCodes), inputs, nilIfEmpty(l.Note), uuids(l.FileRefs),
		nilIfZero(l.BatchRef), l.BecameCurrent, l.OnBehalf, l.Channel, l.EventTime.UTC(), l.SupersedesID, l.CorrectionReason, l.ActorRef, l.DeviceID,
		l.IdemKey, domain.RuleVersion).Scan(&l.Version, &l.RecordedAt)
}

const farmLogCols = `id, tenant_id, seq, log_ref, season_ref, plot_ref, household_ref, cooperative_ref, commune_code, activity_code,
  sop_step_seq, ` + gpsSelect + `, gps_accuracy_m::text, gps_source, location_status, distance_m::text, machine_codes, inputs,
  coalesce(note, ''), file_refs, batch_ref, became_current, on_behalf, channel, event_time, recorded_at, supersedes_id,
  correction_reason, actor_ref, device_id, idem_key`

func scanFarmLog(row pgx.Row) (FarmLog, error) {
	var l FarmLog
	var hh, batch *uuid.UUID
	var lon, lat, acc, dist *string
	var inputs []byte
	var files []string
	if err := row.Scan(&l.ID, &l.TenantID, &l.Version, &l.LogRef, &l.SeasonRef, &l.PlotRef, &hh, &l.CooperativeRef, &l.CommuneCode, &l.ActivityCode,
		&l.SOPStepSeq, &lon, &lat, &acc, &l.GPS.Source, &l.LocationStatus, &dist, &l.MachineCodes, &inputs,
		&l.Note, &files, &batch, &l.BecameCurrent, &l.OnBehalf, &l.Channel, &l.EventTime, &l.RecordedAt, &l.SupersedesID,
		&l.CorrectionReason, &l.ActorRef, &l.DeviceID, &l.IdemKey); err != nil {
		return l, err
	}
	l.HouseholdRef, l.BatchRef = deref(hh), deref(batch)
	var err error
	if l.FileRefs, err = parseUUIDs(files); err != nil {
		return l, err
	}
	for _, p := range []struct {
		s *string
		d **decimal.Decimal
	}{{lon, &l.GPS.Lon}, {lat, &l.GPS.Lat}, {acc, &l.GPS.AccuracyM}, {dist, &l.DistanceM}} {
		if *p.d, err = parseDecPtr(p.s); err != nil {
			return l, err
		}
	}
	return l, json.Unmarshal(inputs, &l.Inputs)
}

func oneFarmLog(ctx context.Context, tx pgx.Tx, where string, arg any) (*FarmLog, error) {
	l, err := scanFarmLog(tx.QueryRow(ctx, "SELECT "+farmLogCols+" FROM pf_cropcycle.farm_log WHERE "+where+" LIMIT 1", arg))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// FarmLogByIdem đọc phiên bản theo idem_key.
func FarmLogByIdem(ctx context.Context, tx pgx.Tx, key string) (*FarmLog, error) {
	return oneFarmLog(ctx, tx, "idem_key = $1::text", key)
}

// FarmLogByID đọc phiên bản theo id.
func FarmLogByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*FarmLog, error) {
	return oneFarmLog(ctx, tx, "id = $1::uuid", id)
}

// FarmLogsByIDs đọc nhiều phiên bản theo id (một truy vấn).
func FarmLogsByIDs(ctx context.Context, tx pgx.Tx, idList []uuid.UUID) (map[uuid.UUID]FarmLog, error) {
	rows, err := tx.Query(ctx, "SELECT "+farmLogCols+" FROM pf_cropcycle.farm_log WHERE id = ANY($1::uuid[])", uuids(idList))
	if err != nil {
		return nil, err
	}
	list, err := collectFarmLogs(rows)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]FarmLog, len(list))
	for _, l := range list {
		out[l.ID] = l
	}
	return out, nil
}

// FarmLogVersions liệt kê mọi phiên bản của nhật ký.
func FarmLogVersions(ctx context.Context, tx pgx.Tx, logRef uuid.UUID) ([]FarmLog, error) {
	rows, err := tx.Query(ctx, "SELECT "+farmLogCols+" FROM pf_cropcycle.farm_log WHERE stream_key = $1::text ORDER BY seq", StreamLog(logRef))
	if err != nil {
		return nil, err
	}
	return collectFarmLogs(rows)
}

// AllFarmLogs đọc toàn bộ sổ nhật ký theo thứ tự ghi (dựng lại bảng chiếu).
func AllFarmLogs(ctx context.Context, tx pgx.Tx) ([]FarmLog, error) {
	rows, err := tx.Query(ctx, "SELECT "+farmLogCols+" FROM pf_cropcycle.farm_log ORDER BY recorded_at, stream_key, seq")
	if err != nil {
		return nil, err
	}
	return collectFarmLogs(rows)
}

func collectFarmLogs(rows pgx.Rows) ([]FarmLog, error) {
	defer rows.Close()
	var out []FarmLog
	for rows.Next() {
		l, err := scanFarmLog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Đầu lô
// ---------------------------------------------------------------------------

// Skipped là một thửa bị bỏ qua trong lượt hàng loạt, kèm lý do.
type Skipped struct {
	PlotRef uuid.UUID `json:"plot_ref"`
	Code    string    `json:"code"`
	Reason  string    `json:"reason"`
}

// Batch là đầu lô nhật ký hàng loạt.
type Batch struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	CooperativeRef uuid.UUID
	ActivityCode   string
	SOPStepSeq     *int
	Requested      int
	Accepted       int
	Skipped        []Skipped
	MachineCodes   []string
	Inputs         []domain.Input
	Note           string
	OnBehalf       bool
	Channel        string
	EventTime      time.Time
	RecordedAt     time.Time
	ActorRef       uuid.UUID
	DeviceID       *string
	IdemKey        string
}

// InsertBatch ghi đầu lô.
func InsertBatch(ctx context.Context, tx pgx.Tx, b *Batch) error {
	inputs, err := inputsJSON(b.Inputs)
	if err != nil {
		return err
	}
	sk := b.Skipped
	if sk == nil {
		sk = []Skipped{}
	}
	skipped, err := json.Marshal(sk)
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_cropcycle.farm_log_batch
	    (id, tenant_id, stream_key, cooperative_ref, activity_code, sop_step_seq, requested, accepted, skipped, machine_codes,
	     inputs, note, on_behalf, channel, event_time, actor_ref, device_id, idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::text, $6::int, $7::int, $8::int, $9::jsonb, $10::text[],
	     $11::jsonb, $12::text, $13::boolean, $14::text, $15::timestamptz, $16::uuid, $17::text, $18::text, $19::text)
	  RETURNING recorded_at`,
		b.ID, b.TenantID, "farm_log_batch:"+b.ID.String(), nilIfZero(b.CooperativeRef), b.ActivityCode, b.SOPStepSeq, b.Requested, b.Accepted,
		string(skipped), strs(b.MachineCodes), inputs, nilIfEmpty(b.Note), b.OnBehalf, b.Channel, b.EventTime.UTC(), b.ActorRef, b.DeviceID,
		b.IdemKey, domain.RuleVersion).Scan(&b.RecordedAt)
}

// BatchByIdem đọc đầu lô theo idem_key.
func BatchByIdem(ctx context.Context, tx pgx.Tx, key string) (*Batch, error) {
	var b Batch
	var coop *uuid.UUID
	var skipped, inputs []byte
	err := tx.QueryRow(ctx, `SELECT id, tenant_id, cooperative_ref, activity_code, sop_step_seq, requested, accepted, skipped,
	    machine_codes, inputs, coalesce(note, ''), on_behalf, channel, event_time, recorded_at, actor_ref, device_id, idem_key
	  FROM pf_cropcycle.farm_log_batch WHERE idem_key = $1::text LIMIT 1`, key).Scan(&b.ID, &b.TenantID, &coop, &b.ActivityCode,
		&b.SOPStepSeq, &b.Requested, &b.Accepted, &skipped, &b.MachineCodes, &inputs, &b.Note, &b.OnBehalf, &b.Channel, &b.EventTime,
		&b.RecordedAt, &b.ActorRef, &b.DeviceID, &b.IdemKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b.CooperativeRef = deref(coop)
	if err := json.Unmarshal(skipped, &b.Skipped); err != nil {
		return nil, err
	}
	return &b, json.Unmarshal(inputs, &b.Inputs)
}

// BatchLines trả id các phiên bản nhật ký của lô.
func BatchLines(ctx context.Context, tx pgx.Tx, batch uuid.UUID) ([]FarmLog, error) {
	rows, err := tx.Query(ctx, "SELECT "+farmLogCols+" FROM pf_cropcycle.farm_log WHERE batch_ref = $1::uuid AND supersedes_id IS NULL ORDER BY id", batch)
	if err != nil {
		return nil, err
	}
	return collectFarmLogs(rows)
}

// ---------------------------------------------------------------------------
// Duyệt
// ---------------------------------------------------------------------------

// Review là một lần duyệt nhật ký.
type Review struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	LogRef         uuid.UUID
	LogVersionID   uuid.UUID
	SeasonRef      uuid.UUID
	CooperativeRef uuid.UUID
	Decision       string
	Note           string
	AuthorRef      uuid.UUID
	EventTime      time.Time
	RecordedAt     time.Time
	ActorRef       uuid.UUID
	IdemKey        string
}

// InsertReview ghi một lần duyệt.
func InsertReview(ctx context.Context, tx pgx.Tx, r *Review) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_cropcycle.farm_log_review
	    (id, tenant_id, stream_key, log_ref, log_version_id, season_ref, cooperative_ref, decision, note, author_ref,
	     event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::uuid, $6::uuid, $7::uuid, $8::text, $9::text, $10::uuid,
	     $11::timestamptz, $12::uuid, $13::text, $14::text)
	  RETURNING recorded_at`,
		r.ID, r.TenantID, "farm_log_review:"+r.LogRef.String(), r.LogRef, r.LogVersionID, r.SeasonRef, r.CooperativeRef, r.Decision,
		nilIfEmpty(r.Note), r.AuthorRef, r.EventTime.UTC(), r.ActorRef, r.IdemKey, domain.RuleVersion).Scan(&r.RecordedAt)
}

const reviewCols = `id, tenant_id, log_ref, log_version_id, season_ref, cooperative_ref, decision, coalesce(note, ''), author_ref,
  event_time, recorded_at, actor_ref, idem_key`

func scanReview(row pgx.Row) (Review, error) {
	var r Review
	err := row.Scan(&r.ID, &r.TenantID, &r.LogRef, &r.LogVersionID, &r.SeasonRef, &r.CooperativeRef, &r.Decision, &r.Note, &r.AuthorRef,
		&r.EventTime, &r.RecordedAt, &r.ActorRef, &r.IdemKey)
	return r, err
}

// ReviewByIdem đọc lần duyệt theo idem_key.
func ReviewByIdem(ctx context.Context, tx pgx.Tx, key string) (*Review, error) {
	r, err := scanReview(tx.QueryRow(ctx, "SELECT "+reviewCols+" FROM pf_cropcycle.farm_log_review WHERE idem_key = $1::text LIMIT 1", key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// AllReviews đọc toàn bộ sổ duyệt theo thứ tự ghi.
func AllReviews(ctx context.Context, tx pgx.Tx) ([]Review, error) {
	rows, err := tx.Query(ctx, "SELECT "+reviewCols+" FROM pf_cropcycle.farm_log_review ORDER BY recorded_at, stream_key, seq")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Review
	for rows.Next() {
		r, err := scanReview(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Bảng chiếu farm_log_current
// ---------------------------------------------------------------------------

// LogState là trạng thái hiện tại của một phiên bản nhật ký.
type LogState struct {
	ID             uuid.UUID
	LogRef         uuid.UUID
	Version        int64
	SeasonRef      uuid.UUID
	PlotRef        uuid.UUID
	HouseholdRef   uuid.UUID
	CooperativeRef uuid.UUID
	CommuneCode    string
	ActivityCode   string
	SOPStepSeq     *int
	EventTime      time.Time
	GPSSource      string
	LocationStatus string
	DistanceM      *decimal.Decimal
	AuthorRef      uuid.UUID
	OnBehalf       bool
	BatchRef       uuid.UUID
	Status         string
	SupersededBy   uuid.UUID
	LastReviewID   uuid.UUID
	ReviewNote     string
	ReviewedBy     uuid.UUID
	ReviewedAt     *time.Time
	RecordedAt     time.Time
	SyncSeq        int64
	UpdatedAt      time.Time
}

// StateFromLog dựng trạng thái chờ duyệt của phiên bản mới.
func StateFromLog(l FarmLog) LogState {
	st := LogState{
		ID: l.ID, LogRef: l.LogRef, Version: l.Version, SeasonRef: l.SeasonRef, PlotRef: l.PlotRef, HouseholdRef: l.HouseholdRef,
		CooperativeRef: l.CooperativeRef, CommuneCode: l.CommuneCode, ActivityCode: l.ActivityCode, SOPStepSeq: l.SOPStepSeq,
		EventTime: l.EventTime, GPSSource: l.GPS.Source, LocationStatus: l.LocationStatus, DistanceM: l.DistanceM,
		AuthorRef: l.ActorRef, OnBehalf: l.OnBehalf, BatchRef: l.BatchRef, Status: "pending", RecordedAt: l.RecordedAt,
	}
	if !l.BecameCurrent {
		st.Status = "superseded"
	}
	return st
}

// SaveLogState ghi (chèn hoặc cập nhật) trạng thái phiên bản; cấp sync_seq mới.
func SaveLogState(ctx context.Context, tx pgx.Tx, tid uuid.UUID, s LogState) error {
	if err := lockSync(ctx, tx, "farm_log"); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_cropcycle.farm_log_current
	    (tenant_id, id, log_ref, version, season_ref, plot_ref, household_ref, cooperative_ref, commune_code, activity_code,
	     sop_step_seq, event_time, gps_source, location_status, distance_m, author_ref, on_behalf, batch_ref, status,
	     superseded_by, last_review_id, review_note, reviewed_by, reviewed_at, recorded_at)
	  VALUES ($1::uuid, $2::uuid, $3::uuid, $4::bigint, $5::uuid, $6::uuid, $7::uuid, $8::uuid, $9::text, $10::text,
	     $11::int, $12::timestamptz, $13::text, $14::text, $15::numeric, $16::uuid, $17::boolean, $18::uuid, $19::text,
	     $20::uuid, $21::uuid, $22::text, $23::uuid, $24::timestamptz, $25::timestamptz)
	  ON CONFLICT (tenant_id, id) DO UPDATE SET
	     status = EXCLUDED.status, superseded_by = EXCLUDED.superseded_by, last_review_id = EXCLUDED.last_review_id,
	     review_note = EXCLUDED.review_note, reviewed_by = EXCLUDED.reviewed_by, reviewed_at = EXCLUDED.reviewed_at,
	     sync_seq = nextval('pf_cropcycle.farm_log_sync_seq'), updated_at = now()`,
		tid, s.ID, s.LogRef, s.Version, s.SeasonRef, s.PlotRef, nilIfZero(s.HouseholdRef), s.CooperativeRef, s.CommuneCode, s.ActivityCode,
		s.SOPStepSeq, s.EventTime.UTC(), s.GPSSource, s.LocationStatus, decPtr(s.DistanceM), s.AuthorRef, s.OnBehalf, nilIfZero(s.BatchRef), s.Status,
		nilIfZero(s.SupersededBy), nilIfZero(s.LastReviewID), nilIfEmpty(s.ReviewNote), nilIfZero(s.ReviewedBy), s.ReviewedAt, s.RecordedAt.UTC())
	return err
}

const logStateCols = `l.id, l.log_ref, l.version, l.season_ref, l.plot_ref, l.household_ref, l.cooperative_ref, l.commune_code,
  l.activity_code, l.sop_step_seq, l.event_time, l.gps_source, l.location_status, l.distance_m::text, l.author_ref, l.on_behalf,
  l.batch_ref, l.status, l.superseded_by, l.last_review_id, coalesce(l.review_note, ''), l.reviewed_by, l.reviewed_at,
  l.recorded_at, l.sync_seq, l.updated_at`

func scanLogState(row pgx.Row) (LogState, error) {
	var s LogState
	var hh, batch, sup, rev, by *uuid.UUID
	var dist *string
	if err := row.Scan(&s.ID, &s.LogRef, &s.Version, &s.SeasonRef, &s.PlotRef, &hh, &s.CooperativeRef, &s.CommuneCode,
		&s.ActivityCode, &s.SOPStepSeq, &s.EventTime, &s.GPSSource, &s.LocationStatus, &dist, &s.AuthorRef, &s.OnBehalf,
		&batch, &s.Status, &sup, &rev, &s.ReviewNote, &by, &s.ReviewedAt, &s.RecordedAt, &s.SyncSeq, &s.UpdatedAt); err != nil {
		return s, err
	}
	s.HouseholdRef, s.BatchRef, s.SupersededBy, s.LastReviewID, s.ReviewedBy = deref(hh), deref(batch), deref(sup), deref(rev), deref(by)
	var err error
	s.DistanceM, err = parseDecPtr(dist)
	return s, err
}

// LogStateByID đọc trạng thái phiên bản; forUpdate khóa dòng.
func LogStateByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*LogState, error) {
	q := "SELECT " + logStateCols + " FROM pf_cropcycle.farm_log_current l WHERE l.id = $1::uuid"
	if forUpdate {
		q += " FOR UPDATE"
	}
	s, err := scanLogState(tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// LogFilter lọc danh sách nhật ký (chỉ phiên bản chưa bị thay).
type LogFilter struct {
	Scope          Filter
	CooperativeRef uuid.UUID
	SeasonRef      uuid.UUID
	Status         string
	After          uuid.UUID
	Limit          int
}

// ListLogStates liệt kê phiên bản hiện hành trong phạm vi theo id (cursor).
func ListLogStates(ctx context.Context, tx pgx.Tx, f LogFilter) ([]LogState, error) {
	args := append(f.Scope.args(), nilIfZero(f.CooperativeRef), nilIfZero(f.SeasonRef), f.Status, f.After, f.Limit)
	rows, err := tx.Query(ctx, "SELECT "+logStateCols+" FROM pf_cropcycle.farm_log_current l WHERE "+scopeSQL("l", 1)+`
	    AND l.status <> 'superseded'
	    AND ($5::uuid IS NULL OR l.cooperative_ref = $5::uuid) AND ($6::uuid IS NULL OR l.season_ref = $6::uuid)
	    AND ($7::text = '' OR l.status = $7::text) AND l.id > $8::uuid
	  ORDER BY l.id LIMIT $9::int`, args...)
	if err != nil {
		return nil, err
	}
	return collectLogStates(rows)
}

// LogStatesChangedAfter trả phiên bản trong phạm vi có sync_seq > after (chiều xuống).
func LogStatesChangedAfter(ctx context.Context, tx pgx.Tx, scope Filter, after int64, limit int) ([]LogState, error) {
	args := append(scope.args(), after, limit)
	rows, err := tx.Query(ctx, "SELECT "+logStateCols+" FROM pf_cropcycle.farm_log_current l WHERE "+scopeSQL("l", 1)+`
	    AND l.sync_seq > $5::bigint ORDER BY l.sync_seq LIMIT $6::int`, args...)
	if err != nil {
		return nil, err
	}
	return collectLogStates(rows)
}

// LiveLogsOfSeason trả phiên bản hiện hành (chưa bị thay) của vụ.
func LiveLogsOfSeason(ctx context.Context, tx pgx.Tx, season uuid.UUID) ([]LogState, error) {
	rows, err := tx.Query(ctx, "SELECT "+logStateCols+` FROM pf_cropcycle.farm_log_current l
	  WHERE l.season_ref = $1::uuid AND l.status <> 'superseded' ORDER BY l.event_time`, season)
	if err != nil {
		return nil, err
	}
	return collectLogStates(rows)
}

func collectLogStates(rows pgx.Rows) ([]LogState, error) {
	defer rows.Close()
	var out []LogState
	for rows.Next() {
		s, err := scanLogState(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
