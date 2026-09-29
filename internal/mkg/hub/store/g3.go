package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// LockStream tuần tự hóa thao tác trên một chuỗi nghiệp vụ trong giao dịch.
func LockStream(ctx context.Context, tx pgx.Tx, key string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 727109))", key)
	return err
}

func kgArg(k *mass.Kg) *string {
	if k == nil {
		return nil
	}
	s := k.String()
	return &s
}

func kgScan(s *string) (*mass.Kg, error) {
	if s == nil {
		return nil, nil
	}
	k, err := mass.ParseKg(*s)
	if err != nil {
		return nil, err
	}
	return &k, nil
}

func none(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

// ---------------------------------------------------------------------------
// Tham số có phiên bản
// ---------------------------------------------------------------------------

// ParamVersion là một phiên bản bộ tham số (kèm quyết định nếu có).
type ParamVersion struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	Set           string
	Seq           int64
	Rule          string
	Params        json.RawMessage
	Reason        string
	EffectiveFrom time.Time
	EventTime     time.Time
	RecordedAt    time.Time
	ProposedBy    uuid.UUID
	IdemKey       string
	Decision      *string
	DecidedBy     *uuid.UUID
	DecidedAt     *time.Time
}

// ParamStream là chuỗi của một bộ tham số.
func ParamStream(set string) string { return "param:" + set }

// InsertParamVersion ghi sổ phiên bản; seq = số phiên bản do DB đặt.
func InsertParamVersion(ctx context.Context, tx pgx.Tx, v *ParamVersion, deviceID *string) error {
	return tx.QueryRow(ctx, `INSERT INTO mkg_hub.param_version
	    (id, tenant_id, stream_key, param_set, params, reason, effective_from, event_time, actor_ref, device_id, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9, $10, $11, $12) RETURNING seq, recorded_at`,
		v.ID, v.TenantID, ParamStream(v.Set), v.Set, string(v.Params), v.Reason, v.EffectiveFrom.UTC(), v.EventTime.UTC(),
		v.ProposedBy, deviceID, v.IdemKey, v.Rule).Scan(&v.Seq, &v.RecordedAt)
}

const paramCols = `v.id, v.tenant_id, v.param_set, v.seq, v.rule_version, v.params::text, v.reason, v.effective_from, v.event_time,
  v.recorded_at, v.actor_ref, v.idem_key, d.decision, d.actor_ref, d.event_time`

const paramFrom = ` FROM mkg_hub.param_version v LEFT JOIN mkg_hub.param_decision d ON d.version_id = v.id `

func queryParams(ctx context.Context, tx pgx.Tx, where string, args ...any) ([]ParamVersion, error) {
	rows, err := tx.Query(ctx, "SELECT "+paramCols+paramFrom+"WHERE "+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ParamVersion
	for rows.Next() {
		var v ParamVersion
		var params string
		if err := rows.Scan(&v.ID, &v.TenantID, &v.Set, &v.Seq, &v.Rule, &params, &v.Reason, &v.EffectiveFrom, &v.EventTime,
			&v.RecordedAt, &v.ProposedBy, &v.IdemKey, &v.Decision, &v.DecidedBy, &v.DecidedAt); err != nil {
			return nil, err
		}
		v.Params = json.RawMessage(params)
		out = append(out, v)
	}
	return out, rows.Err()
}

func oneParam(ctx context.Context, tx pgx.Tx, where string, args ...any) (*ParamVersion, error) {
	vs, err := queryParams(ctx, tx, where+" LIMIT 1", args...)
	if err != nil || len(vs) == 0 {
		return nil, err
	}
	return &vs[0], nil
}

// ParamVersionBySeq đọc phiên bản số seq của bộ.
func ParamVersionBySeq(ctx context.Context, tx pgx.Tx, set string, seq int64) (*ParamVersion, error) {
	return oneParam(ctx, tx, "v.param_set = $1 AND v.seq = $2", set, seq)
}

// ParamVersionByIdem tìm phiên bản đã ghi với idem_key.
func ParamVersionByIdem(ctx context.Context, tx pgx.Tx, key string) (*ParamVersion, error) {
	return oneParam(ctx, tx, "v.idem_key = $1", key)
}

// ParamVersions liệt kê phiên bản của bộ, mới trước.
func ParamVersions(ctx context.Context, tx pgx.Tx, set string) ([]ParamVersion, error) {
	return queryParams(ctx, tx, "v.param_set = $1 ORDER BY v.seq DESC", set)
}

// PendingParam cho biết bộ đang có phiên bản chờ duyệt.
func PendingParam(ctx context.Context, tx pgx.Tx, set string) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1`+paramFrom+`WHERE v.param_set = $1 AND d.id IS NULL)`, set).Scan(&ok)
	return ok, err
}

// ParamDecision là quyết định với một phiên bản tham số.
type ParamDecision struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	Set         string
	VersionID   uuid.UUID
	VersionSeq  int64
	Decision    string
	Reason      *string
	ProposerRef uuid.UUID
	ActorRef    uuid.UUID
	EventTime   time.Time
	IdemKey     string
}

// InsertParamDecision ghi sổ quyết định (DB chặn người duyệt = người đặt).
func InsertParamDecision(ctx context.Context, tx pgx.Tx, d *ParamDecision) error {
	_, err := tx.Exec(ctx, `INSERT INTO mkg_hub.param_decision
	    (id, tenant_id, stream_key, param_set, version_id, version_seq, decision, reason, proposer_ref, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		d.ID, d.TenantID, ParamStream(d.Set), d.Set, d.VersionID, d.VersionSeq, d.Decision, d.Reason, d.ProposerRef,
		d.EventTime.UTC(), d.ActorRef, d.IdemKey, domain.RuleParam)
	return err
}

// ParamDecisionByIdem tìm quyết định đã ghi với idem_key.
func ParamDecisionByIdem(ctx context.Context, tx pgx.Tx, key string) (*ParamDecision, error) {
	var d ParamDecision
	err := tx.QueryRow(ctx, `SELECT id, param_set, version_id, version_seq, decision FROM mkg_hub.param_decision WHERE idem_key = $1`, key).
		Scan(&d.ID, &d.Set, &d.VersionID, &d.VersionSeq, &d.Decision)
	if err != nil {
		return nil, none(err)
	}
	return &d, nil
}

// UpsertParamCurrent đặt phiên bản hiện hành của bộ.
func UpsertParamCurrent(ctx context.Context, tx pgx.Tx, v ParamVersion, approver uuid.UUID, at time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO mkg_hub.param_current
	    (tenant_id, param_set, version_id, version_seq, rule_version, params, effective_from, proposed_by, approved_by, approved_at)
	  VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9, $10)
	  ON CONFLICT (tenant_id, param_set) DO UPDATE SET version_id = EXCLUDED.version_id, version_seq = EXCLUDED.version_seq,
	    rule_version = EXCLUDED.rule_version, params = EXCLUDED.params, effective_from = EXCLUDED.effective_from,
	    proposed_by = EXCLUDED.proposed_by, approved_by = EXCLUDED.approved_by, approved_at = EXCLUDED.approved_at, updated_at = now()`,
		v.TenantID, v.Set, v.ID, v.Seq, v.Rule, string(v.Params), v.EffectiveFrom.UTC(), v.ProposedBy, approver, at.UTC())
	return err
}

// ParamCurrent đọc phiên bản hiện hành của bộ; ok = false khi chưa duyệt phiên bản nào.
func ParamCurrent(ctx context.Context, tx pgx.Tx, set string) (seq int64, params json.RawMessage, ok bool, err error) {
	var s string
	err = tx.QueryRow(ctx, `SELECT version_seq, params::text FROM mkg_hub.param_current WHERE param_set = $1`, set).Scan(&seq, &s)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil, false, nil
	}
	return seq, json.RawMessage(s), err == nil, err
}

// ---------------------------------------------------------------------------
// Phiếu cân ↔ lô
// ---------------------------------------------------------------------------

// WeighLink là một dòng sổ mkg_hub.weigh_link.
type WeighLink struct {
	ID                 uuid.UUID
	TenantID           uuid.UUID
	Seq                int64
	LotRef             uuid.UUID
	Point              string
	TicketID           *uuid.UUID
	TicketSource       *string
	SupersedesTicketID *uuid.UUID
	Measured           *mass.Kg
	MoisturePct        *string
	Book               *mass.Kg
	Diff               *mass.Kg
	Tolerance          *mass.Kg
	Outcome            string
	DispatchID         *uuid.UUID
	MovementID         *uuid.UUID
	Reason             *string
	ParamVersion       *string
	EventTime          time.Time
	RecordedAt         time.Time
	ActorRef           uuid.UUID
	IdemKey            string
}

// InsertWeighLink ghi sổ.
func InsertWeighLink(ctx context.Context, tx pgx.Tx, l *WeighLink) error {
	return tx.QueryRow(ctx, `INSERT INTO mkg_hub.weigh_link
	    (id, tenant_id, stream_key, lot_ref, point, ticket_id, ticket_source, supersedes_ticket_id, measured_kg, moisture_pct,
	     book_kg, diff_kg, tolerance_kg, outcome, dispatch_id, movement_id, reason, param_version, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::numeric, $10::numeric, $11::numeric, $12::numeric, $13::numeric, $14, $15, $16,
	          $17, $18, $19, $20, $21, $22)
	  RETURNING seq, recorded_at`,
		l.ID, l.TenantID, domain.StreamKey(l.LotRef), l.LotRef, l.Point, l.TicketID, l.TicketSource, l.SupersedesTicketID,
		kgArg(l.Measured), l.MoisturePct, kgArg(l.Book), kgArg(l.Diff), kgArg(l.Tolerance), l.Outcome, l.DispatchID, l.MovementID,
		l.Reason, l.ParamVersion, l.EventTime.UTC(), l.ActorRef, l.IdemKey, domain.RuleWeighLink).Scan(&l.Seq, &l.RecordedAt)
}

const weighCols = `id, tenant_id, seq, lot_ref, point, ticket_id, ticket_source, supersedes_ticket_id, measured_kg::text, moisture_pct::text,
  book_kg::text, diff_kg::text, tolerance_kg::text, outcome, dispatch_id, movement_id, reason, param_version, event_time, recorded_at,
  actor_ref, idem_key`

func queryWeigh(ctx context.Context, tx pgx.Tx, where string, args ...any) ([]WeighLink, error) {
	rows, err := tx.Query(ctx, "SELECT "+weighCols+" FROM mkg_hub.weigh_link WHERE "+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WeighLink
	for rows.Next() {
		var l WeighLink
		var m, b, d, t *string
		if err := rows.Scan(&l.ID, &l.TenantID, &l.Seq, &l.LotRef, &l.Point, &l.TicketID, &l.TicketSource, &l.SupersedesTicketID,
			&m, &l.MoisturePct, &b, &d, &t, &l.Outcome, &l.DispatchID, &l.MovementID, &l.Reason, &l.ParamVersion, &l.EventTime,
			&l.RecordedAt, &l.ActorRef, &l.IdemKey); err != nil {
			return nil, err
		}
		for _, p := range []struct {
			s   *string
			dst **mass.Kg
		}{{m, &l.Measured}, {b, &l.Book}, {d, &l.Diff}, {t, &l.Tolerance}} {
			k, err := kgScan(p.s)
			if err != nil {
				return nil, err
			}
			*p.dst = k
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// WeighLinkByIdem tìm dòng đã ghi với idem_key.
func WeighLinkByIdem(ctx context.Context, tx pgx.Tx, key string) (*WeighLink, error) {
	ls, err := queryWeigh(ctx, tx, "idem_key = $1 LIMIT 1", key)
	if err != nil || len(ls) == 0 {
		return nil, err
	}
	return &ls[0], nil
}

// WeighLinksOfLot liệt kê dòng của lô theo seq.
func WeighLinksOfLot(ctx context.Context, tx pgx.Tx, lot uuid.UUID) ([]WeighLink, error) {
	return queryWeigh(ctx, tx, "lot_ref = $1 ORDER BY seq", lot)
}

// LotWeigh là phiếu hiệu lực của (lô, điểm cân).
type LotWeigh struct {
	LotID              uuid.UUID
	Point              string
	TicketID           *uuid.UUID
	SupersedesTicketID *uuid.UUID
	Measured           *mass.Kg
	MoisturePct        *string
	Status             string
	Basis              string
	Book               *mass.Kg
	DispatchID         *uuid.UUID
	LastLinkID         uuid.UUID
}

const lotWeighCols = `lot_id, point, ticket_id, supersedes_ticket_id, measured_kg::text, moisture_pct::text, status, basis, book_kg::text,
  dispatch_id, last_link_id`

func oneLotWeigh(ctx context.Context, tx pgx.Tx, where string, args ...any) (*LotWeigh, error) {
	var w LotWeigh
	var m, b *string
	err := tx.QueryRow(ctx, "SELECT "+lotWeighCols+" FROM mkg_hub.lot_weigh_current WHERE "+where, args...).Scan(
		&w.LotID, &w.Point, &w.TicketID, &w.SupersedesTicketID, &m, &w.MoisturePct, &w.Status, &w.Basis, &b, &w.DispatchID, &w.LastLinkID)
	if err != nil {
		return nil, none(err)
	}
	if w.Measured, err = kgScan(m); err != nil {
		return nil, err
	}
	if w.Book, err = kgScan(b); err != nil {
		return nil, err
	}
	return &w, nil
}

// LotWeighOf đọc phiếu hiệu lực của (lô, điểm cân).
func LotWeighOf(ctx context.Context, tx pgx.Tx, lot uuid.UUID, point string) (*LotWeigh, error) {
	return oneLotWeigh(ctx, tx, "lot_id = $1 AND point = $2", lot, point)
}

// LotWeighByTicket đọc dòng hiệu lực theo phiếu.
func LotWeighByTicket(ctx context.Context, tx pgx.Tx, ticket uuid.UUID) (*LotWeigh, error) {
	return oneLotWeigh(ctx, tx, "ticket_id = $1", ticket)
}

// UpsertLotWeigh ghi bảng chiếu phiếu hiệu lực (R-21).
func UpsertLotWeigh(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, w LotWeigh) error {
	_, err := tx.Exec(ctx, `INSERT INTO mkg_hub.lot_weigh_current
	    (tenant_id, lot_id, point, ticket_id, supersedes_ticket_id, measured_kg, moisture_pct, status, basis, book_kg, dispatch_id, last_link_id)
	  VALUES ($1, $2, $3, $4, $5, $6::numeric, $7::numeric, $8, $9, $10::numeric, $11, $12)
	  ON CONFLICT (tenant_id, lot_id, point) DO UPDATE SET ticket_id = EXCLUDED.ticket_id,
	    supersedes_ticket_id = EXCLUDED.supersedes_ticket_id, measured_kg = EXCLUDED.measured_kg, moisture_pct = EXCLUDED.moisture_pct,
	    status = EXCLUDED.status, basis = EXCLUDED.basis, book_kg = EXCLUDED.book_kg, dispatch_id = EXCLUDED.dispatch_id,
	    last_link_id = EXCLUDED.last_link_id, updated_at = now()`,
		tenantID, w.LotID, w.Point, w.TicketID, w.SupersedesTicketID, kgArg(w.Measured), w.MoisturePct, w.Status, w.Basis,
		kgArg(w.Book), w.DispatchID, w.LastLinkID)
	return err
}

// LatestDispatchWithLot tìm phiếu xuất mới nhất có phân bổ từ lô; trả id và khối lượng phân bổ.
func LatestDispatchWithLot(ctx context.Context, tx pgx.Tx, lot uuid.UUID) (*uuid.UUID, *mass.Kg, error) {
	var id uuid.UUID
	var kg string
	err := tx.QueryRow(ctx, `SELECT d.id, a->>'mass_kg' FROM mkg_hub.dispatch d, jsonb_array_elements(d.allocations) a
	  WHERE a->>'lot_id' = $1 ORDER BY d.event_time DESC, d.id DESC LIMIT 1`, lot.String()).Scan(&id, &kg)
	if err != nil {
		return nil, nil, none(err)
	}
	k, err := mass.ParseKg(kg)
	if err != nil {
		return nil, nil, err
	}
	return &id, &k, nil
}

// ---------------------------------------------------------------------------
// Kiểm kê
// ---------------------------------------------------------------------------

// Stocktake là một lần đếm (dòng sổ mkg_hub.stocktake) kèm trạng thái.
type Stocktake struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	FacilityID   uuid.UUID
	Seq          int64
	Lines        []domain.StocktakeLine
	Book         mass.Kg
	Counted      mass.Kg
	Variance     mass.Kg
	Note         *string
	EventTime    time.Time
	SupersedesID *uuid.UUID
	Reason       *string
	ActorRef     uuid.UUID
	IdemKey      string
	Status       string
	DecidedBy    *uuid.UUID
	DecidedAt    *time.Time
}

// InsertStocktake ghi sổ + bảng chiếu trạng thái chờ duyệt.
func InsertStocktake(ctx context.Context, tx pgx.Tx, s *Stocktake, deviceID *string) error {
	lines, err := json.Marshal(s.Lines)
	if err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `INSERT INTO mkg_hub.stocktake
	    (id, tenant_id, stream_key, facility_id, lines, book_kg, counted_kg, variance_kg, note, event_time, supersedes_id,
	     correction_reason, actor_ref, device_id, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5::jsonb, $6::numeric, $7::numeric, $8::numeric, $9, $10, $11, $12, $13, $14, $15, $16) RETURNING seq`,
		s.ID, s.TenantID, FacilityStream(s.FacilityID), s.FacilityID, string(lines), s.Book.String(), s.Counted.String(),
		s.Variance.String(), s.Note, s.EventTime.UTC(), s.SupersedesID, s.Reason, s.ActorRef, deviceID, s.IdemKey,
		domain.RuleStocktake).Scan(&s.Seq); err != nil {
		return err
	}
	s.Status = "pending"
	_, err = tx.Exec(ctx, `INSERT INTO mkg_hub.stocktake_current (tenant_id, stocktake_id, facility_id, status, variance_kg, counted_by, counted_at)
	  VALUES ($1, $2, $3, 'pending', $4::numeric, $5, $6)`, s.TenantID, s.ID, s.FacilityID, s.Variance.String(), s.ActorRef, s.EventTime.UTC())
	return err
}

const stocktakeCols = `s.id, s.tenant_id, s.facility_id, s.seq, s.lines::text, s.book_kg::text, s.counted_kg::text, s.variance_kg::text,
  s.note, s.event_time, s.supersedes_id, s.correction_reason, s.actor_ref, s.idem_key, c.status, c.decided_by, c.decided_at`

func queryStocktakes(ctx context.Context, tx pgx.Tx, where string, args ...any) ([]Stocktake, error) {
	rows, err := tx.Query(ctx, "SELECT "+stocktakeCols+` FROM mkg_hub.stocktake s
	  JOIN mkg_hub.stocktake_current c ON c.stocktake_id = s.id WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Stocktake
	for rows.Next() {
		var s Stocktake
		var lines, b, c, v string
		if err := rows.Scan(&s.ID, &s.TenantID, &s.FacilityID, &s.Seq, &lines, &b, &c, &v, &s.Note, &s.EventTime, &s.SupersedesID,
			&s.Reason, &s.ActorRef, &s.IdemKey, &s.Status, &s.DecidedBy, &s.DecidedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(lines), &s.Lines); err != nil {
			return nil, err
		}
		var err error
		if s.Book, err = mass.ParseKg(b); err != nil {
			return nil, err
		}
		if s.Counted, err = mass.ParseKg(c); err != nil {
			return nil, err
		}
		if s.Variance, err = mass.ParseKg(v); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func oneStocktake(ctx context.Context, tx pgx.Tx, where string, args ...any) (*Stocktake, error) {
	ss, err := queryStocktakes(ctx, tx, where+" LIMIT 1", args...)
	if err != nil || len(ss) == 0 {
		return nil, err
	}
	return &ss[0], nil
}

// StocktakeByID đọc một lần đếm.
func StocktakeByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Stocktake, error) {
	return oneStocktake(ctx, tx, "s.id = $1", id)
}

// StocktakeByIdem tìm lần đếm đã ghi với idem_key.
func StocktakeByIdem(ctx context.Context, tx pgx.Tx, key string) (*Stocktake, error) {
	return oneStocktake(ctx, tx, "s.idem_key = $1", key)
}

// PendingStocktake đọc phiên chờ duyệt của cơ sở.
func PendingStocktake(ctx context.Context, tx pgx.Tx, facility uuid.UUID) (*Stocktake, error) {
	return oneStocktake(ctx, tx, "s.facility_id = $1 AND c.status = 'pending'", facility)
}

// Stocktakes liệt kê phiên kiểm kê của cơ sở, mới trước.
func Stocktakes(ctx context.Context, tx pgx.Tx, facility uuid.UUID, limit int) ([]Stocktake, error) {
	return queryStocktakes(ctx, tx, "s.facility_id = $1 ORDER BY s.seq DESC LIMIT $2", facility, limit)
}

// SetStocktakeStatus cập nhật bảng chiếu trạng thái.
func SetStocktakeStatus(ctx context.Context, tx pgx.Tx, id uuid.UUID, status string, by *uuid.UUID, at *time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE mkg_hub.stocktake_current SET status = $2, decided_by = $3, decided_at = $4, updated_at = now()
	  WHERE stocktake_id = $1`, id, status, by, at)
	return err
}

// StocktakeAdjustment là một dịch chuyển điều chỉnh đã ghi khi duyệt.
type StocktakeAdjustment struct {
	LotID      uuid.UUID `json:"lot_id"`
	MovementID uuid.UUID `json:"movement_id"`
	QtyKg      mass.Kg   `json:"qty_kg"`
}

// StocktakeDecision là quyết định với một phiên kiểm kê.
type StocktakeDecision struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	FacilityID  uuid.UUID
	StocktakeID uuid.UUID
	Decision    string
	Reason      *string
	ProposerRef uuid.UUID
	Adjustments []StocktakeAdjustment
	EventTime   time.Time
	ActorRef    uuid.UUID
	IdemKey     string
}

// InsertStocktakeDecision ghi sổ quyết định (DB chặn người duyệt = người đếm).
func InsertStocktakeDecision(ctx context.Context, tx pgx.Tx, d *StocktakeDecision) error {
	adj := d.Adjustments
	if adj == nil {
		adj = []StocktakeAdjustment{}
	}
	raw, err := json.Marshal(adj)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO mkg_hub.stocktake_decision
	    (id, tenant_id, stream_key, facility_id, stocktake_id, decision, reason, proposer_ref, adjustments, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10, $11, $12, $13)`,
		d.ID, d.TenantID, FacilityStream(d.FacilityID), d.FacilityID, d.StocktakeID, d.Decision, d.Reason, d.ProposerRef, string(raw),
		d.EventTime.UTC(), d.ActorRef, d.IdemKey, domain.RuleStocktake)
	return err
}

// StocktakeDecisionByIdem tìm quyết định đã ghi với idem_key.
func StocktakeDecisionByIdem(ctx context.Context, tx pgx.Tx, key string) (*StocktakeDecision, error) {
	var d StocktakeDecision
	var adj string
	err := tx.QueryRow(ctx, `SELECT id, stocktake_id, decision, adjustments::text FROM mkg_hub.stocktake_decision WHERE idem_key = $1`, key).
		Scan(&d.ID, &d.StocktakeID, &d.Decision, &adj)
	if err != nil {
		return nil, none(err)
	}
	return &d, json.Unmarshal([]byte(adj), &d.Adjustments)
}

// StocktakeDecisionOf đọc quyết định của một phiên.
func StocktakeDecisionOf(ctx context.Context, tx pgx.Tx, stocktake uuid.UUID) (*StocktakeDecision, error) {
	var d StocktakeDecision
	var adj string
	err := tx.QueryRow(ctx, `SELECT id, stocktake_id, decision, adjustments::text FROM mkg_hub.stocktake_decision WHERE stocktake_id = $1`, stocktake).
		Scan(&d.ID, &d.StocktakeID, &d.Decision, &adj)
	if err != nil {
		return nil, none(err)
	}
	return &d, json.Unmarshal([]byte(adj), &d.Adjustments)
}

// BookByProduct là tồn theo lô của cơ sở gộp theo dạng hàng.
func BookByProduct(ctx context.Context, tx pgx.Tx, facility uuid.UUID) (map[domain.Product]mass.Kg, error) {
	rows, err := tx.Query(ctx, `SELECT product, sum(remaining_kg)::numeric(14,3)::text FROM mkg_hub.lot_balance
	  WHERE facility_id = $1 AND status = 'in_stock' GROUP BY product`, facility)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[domain.Product]mass.Kg{}
	for rows.Next() {
		var p, s string
		if err := rows.Scan(&p, &s); err != nil {
			return nil, err
		}
		k, err := mass.ParseKg(s)
		if err != nil {
			return nil, err
		}
		out[domain.Product(p)] = k
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Chuyển kho
// ---------------------------------------------------------------------------

// TransferLot là một dòng lô của phiếu chuyển.
type TransferLot struct {
	SourceLotID *uuid.UUID `json:"source_lot_id,omitempty"`
	LotID       uuid.UUID  `json:"lot_id"`
	MassKg      mass.Kg    `json:"mass_kg"`
}

// TransferEvent là một dòng sổ mkg_hub.transfer_event.
type TransferEvent struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	TransferID     uuid.UUID
	Kind           string // dispatched | received
	FromFacility   uuid.UUID
	ToFacility     uuid.UUID
	DispatchID     uuid.UUID
	Mass           mass.Kg
	Loss           *mass.Kg
	Lots           []TransferLot
	WeighTicketIDs []uuid.UUID
	EventTime      time.Time
	ActorRef       uuid.UUID
	IdemKey        string
}

// InsertTransferEvent ghi sổ.
func InsertTransferEvent(ctx context.Context, tx pgx.Tx, e *TransferEvent, deviceID *string) error {
	lots, err := json.Marshal(e.Lots)
	if err != nil {
		return err
	}
	tickets := e.WeighTicketIDs
	if tickets == nil {
		tickets = []uuid.UUID{}
	}
	tk, err := json.Marshal(tickets)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO mkg_hub.transfer_event
	    (id, tenant_id, stream_key, transfer_id, kind, from_facility_id, to_facility_id, dispatch_id, mass_kg, loss_kg, lots,
	     weigh_ticket_refs, event_time, actor_ref, device_id, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::numeric, $10::numeric, $11::jsonb, $12::jsonb, $13, $14, $15, $16, $17)`,
		e.ID, e.TenantID, "transfer:"+e.TransferID.String(), e.TransferID, e.Kind, e.FromFacility, e.ToFacility, e.DispatchID,
		e.Mass.String(), kgArg(e.Loss), string(lots), string(tk), e.EventTime.UTC(), e.ActorRef, deviceID, e.IdemKey, domain.RuleTransfer)
	return err
}

// TransferEventByIdem tìm dòng đã ghi với idem_key; trả transfer_id.
func TransferEventByIdem(ctx context.Context, tx pgx.Tx, key string) (*uuid.UUID, error) {
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT transfer_id FROM mkg_hub.transfer_event WHERE idem_key = $1`, key).Scan(&id); err != nil {
		return nil, none(err)
	}
	return &id, nil
}

// Transfer là trạng thái hiện hành của một phiếu chuyển.
type Transfer struct {
	ID           uuid.UUID
	FromFacility uuid.UUID
	ToFacility   uuid.UUID
	DispatchID   uuid.UUID
	Status       string // in_transit | received
	Dispatched   mass.Kg
	Received     *mass.Kg
	Loss         *mass.Kg
	SourceLots   []TransferLot
	ReceivedLots []TransferLot
	DispatchedAt time.Time
	ReceivedAt   *time.Time
}

// InsertTransfer tạo bảng chiếu khi xuất chuyển.
func InsertTransfer(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, t Transfer) error {
	src, err := json.Marshal(t.SourceLots)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO mkg_hub.transfer_current
	    (tenant_id, transfer_id, from_facility_id, to_facility_id, dispatch_id, status, dispatched_kg, source_lots, dispatched_at)
	  VALUES ($1, $2, $3, $4, $5, 'in_transit', $6::numeric, $7::jsonb, $8)`,
		tenantID, t.ID, t.FromFacility, t.ToFacility, t.DispatchID, t.Dispatched.String(), string(src), t.DispatchedAt.UTC())
	return err
}

// MarkTransferReceived cập nhật bảng chiếu khi nhận ở đích.
func MarkTransferReceived(ctx context.Context, tx pgx.Tx, id uuid.UUID, received, loss mass.Kg, lots []TransferLot, at time.Time) error {
	raw, err := json.Marshal(lots)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE mkg_hub.transfer_current SET status = 'received', received_kg = $2::numeric, loss_kg = $3::numeric,
	    received_lots = $4::jsonb, received_at = $5, updated_at = now() WHERE transfer_id = $1`,
		id, received.String(), loss.String(), string(raw), at.UTC())
	return err
}

const transferCols = `transfer_id, from_facility_id, to_facility_id, dispatch_id, status, dispatched_kg::text, received_kg::text,
  loss_kg::text, source_lots::text, received_lots::text, dispatched_at, received_at`

func oneTransfer(ctx context.Context, tx pgx.Tx, where string, args ...any) (*Transfer, error) {
	var t Transfer
	var d, src string
	var r, l, rl *string
	err := tx.QueryRow(ctx, "SELECT "+transferCols+" FROM mkg_hub.transfer_current WHERE "+where, args...).Scan(
		&t.ID, &t.FromFacility, &t.ToFacility, &t.DispatchID, &t.Status, &d, &r, &l, &src, &rl, &t.DispatchedAt, &t.ReceivedAt)
	if err != nil {
		return nil, none(err)
	}
	if t.Dispatched, err = mass.ParseKg(d); err != nil {
		return nil, err
	}
	if t.Received, err = kgScan(r); err != nil {
		return nil, err
	}
	if t.Loss, err = kgScan(l); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(src), &t.SourceLots); err != nil {
		return nil, err
	}
	if rl != nil {
		if err := json.Unmarshal([]byte(*rl), &t.ReceivedLots); err != nil {
			return nil, err
		}
	}
	return &t, nil
}

// TransferByID đọc phiếu chuyển; forUpdate khóa dòng.
func TransferByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*Transfer, error) {
	q := "transfer_id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	return oneTransfer(ctx, tx, q, id)
}

// InTransitWithLot tìm phiếu chuyển đang đi đường có lô nguồn lot.
func InTransitWithLot(ctx context.Context, tx pgx.Tx, lot uuid.UUID) (*Transfer, error) {
	return oneTransfer(ctx, tx, `status = 'in_transit' AND source_lots @> jsonb_build_array(jsonb_build_object('lot_id', $1::text))
	  ORDER BY dispatched_at DESC LIMIT 1`, lot.String())
}

// ---------------------------------------------------------------------------
// Quy kết hao hụt
// ---------------------------------------------------------------------------

// LotLoss là một dòng sổ mkg_hub.lot_loss.
type LotLoss struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	ReconciliationID uuid.UUID
	LotRef           uuid.UUID
	FacilityID       *uuid.UUID
	Route            string
	HubIn            *mass.Kg
	HubOut           *mass.Kg
	CustomerIn       *mass.Kg
	ExpectedLoss     *mass.Kg
	AbnormalLoss     *mass.Kg
	CommercialDiff   *mass.Kg
	Outcome          string
	ToleranceVersion *string
	LossNormVersion  *string
	DisputeID        *uuid.UUID
	SourceRule       string
	EventTime        time.Time
	ActorRef         uuid.UUID
	IdemKey          string
}

// InsertLotLoss ghi sổ và cập nhật bảng chiếu nếu đối soát mới hơn (theo event_time, rồi id).
func InsertLotLoss(ctx context.Context, tx pgx.Tx, l *LotLoss) error {
	if _, err := tx.Exec(ctx, `INSERT INTO mkg_hub.lot_loss
	    (id, tenant_id, stream_key, reconciliation_id, lot_ref, facility_id, route, hub_in_kg, hub_out_kg, customer_in_kg,
	     expected_loss_kg, abnormal_loss_kg, commercial_diff_kg, outcome, tolerance_version, loss_norm_version, dispute_id,
	     source_rule, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8::numeric, $9::numeric, $10::numeric, $11::numeric, $12::numeric, $13::numeric, $14,
	          $15, $16, $17, $18, $19, $20, $21, $22)`,
		l.ID, l.TenantID, domain.StreamKey(l.LotRef), l.ReconciliationID, l.LotRef, l.FacilityID, l.Route, kgArg(l.HubIn),
		kgArg(l.HubOut), kgArg(l.CustomerIn), kgArg(l.ExpectedLoss), kgArg(l.AbnormalLoss), kgArg(l.CommercialDiff), l.Outcome,
		l.ToleranceVersion, l.LossNormVersion, l.DisputeID, l.SourceRule, l.EventTime.UTC(), l.ActorRef, l.IdemKey, domain.RuleLotLoss); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO mkg_hub.lot_loss_current
	    (tenant_id, lot_ref, facility_id, reconciliation_id, route, hub_in_kg, hub_out_kg, expected_loss_kg, abnormal_loss_kg, outcome, event_time)
	  VALUES ($1, $2, $3, $4, $5, $6::numeric, $7::numeric, $8::numeric, $9::numeric, $10, $11)
	  ON CONFLICT (tenant_id, lot_ref) DO UPDATE SET facility_id = EXCLUDED.facility_id, reconciliation_id = EXCLUDED.reconciliation_id,
	    route = EXCLUDED.route, hub_in_kg = EXCLUDED.hub_in_kg, hub_out_kg = EXCLUDED.hub_out_kg, expected_loss_kg = EXCLUDED.expected_loss_kg,
	    abnormal_loss_kg = EXCLUDED.abnormal_loss_kg, outcome = EXCLUDED.outcome, event_time = EXCLUDED.event_time, updated_at = now()
	  WHERE (EXCLUDED.event_time, EXCLUDED.reconciliation_id) > (mkg_hub.lot_loss_current.event_time, mkg_hub.lot_loss_current.reconciliation_id)`,
		l.TenantID, l.LotRef, l.FacilityID, l.ReconciliationID, l.Route, kgArg(l.HubIn), kgArg(l.HubOut), kgArg(l.ExpectedLoss),
		kgArg(l.AbnormalLoss), l.Outcome, l.EventTime.UTC())
	return err
}

// LotLossExists cho biết đối soát đã được ghi.
func LotLossExists(ctx context.Context, tx pgx.Tx, key string) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM mkg_hub.lot_loss WHERE idem_key = $1)`, key).Scan(&ok)
	return ok, err
}

// LossCurrent là đối soát hiện hành của một lô.
type LossCurrent struct {
	LotRef           uuid.UUID
	ReconciliationID uuid.UUID
	Route            string
	HubIn, HubOut    *mass.Kg
	ExpectedLoss     *mass.Kg
	AbnormalLoss     *mass.Kg
	Outcome          string
	EventTime        time.Time
}

// LossOfLot đọc đối soát hiện hành của lô.
func LossOfLot(ctx context.Context, tx pgx.Tx, lot uuid.UUID) (*LossCurrent, error) {
	var c LossCurrent
	var hi, ho, el, al *string
	err := tx.QueryRow(ctx, `SELECT lot_ref, reconciliation_id, route, hub_in_kg::text, hub_out_kg::text, expected_loss_kg::text,
	    abnormal_loss_kg::text, outcome, event_time FROM mkg_hub.lot_loss_current WHERE lot_ref = $1`, lot).
		Scan(&c.LotRef, &c.ReconciliationID, &c.Route, &hi, &ho, &el, &al, &c.Outcome, &c.EventTime)
	if err != nil {
		return nil, none(err)
	}
	for _, p := range []struct {
		s   *string
		dst **mass.Kg
	}{{hi, &c.HubIn}, {ho, &c.HubOut}, {el, &c.ExpectedLoss}, {al, &c.AbnormalLoss}} {
		if *p.dst, err = kgScan(p.s); err != nil {
			return nil, err
		}
	}
	return &c, nil
}

// LossSummary là tổng hao hụt đã quy kết cho một cơ sở.
type LossSummary struct {
	Lots          int
	LossAlerts    int
	HubIn, HubOut mass.Kg
	ExpectedLoss  mass.Kg
	AbnormalLoss  mass.Kg
}

// FacilityLoss tổng hợp đối soát hiện hành của mọi lô thuộc cơ sở.
func FacilityLoss(ctx context.Context, tx pgx.Tx, facility uuid.UUID) (LossSummary, error) {
	var s LossSummary
	var hi, ho, el, al string
	err := tx.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE outcome = 'loss_alert'),
	    coalesce(sum(hub_in_kg), 0)::numeric(14,3)::text, coalesce(sum(hub_out_kg), 0)::numeric(14,3)::text,
	    coalesce(sum(expected_loss_kg), 0)::numeric(14,3)::text, coalesce(sum(abnormal_loss_kg), 0)::numeric(14,3)::text
	  FROM mkg_hub.lot_loss_current WHERE facility_id = $1`, facility).Scan(&s.Lots, &s.LossAlerts, &hi, &ho, &el, &al)
	if err != nil {
		return s, err
	}
	for _, p := range []struct {
		s   string
		dst *mass.Kg
	}{{hi, &s.HubIn}, {ho, &s.HubOut}, {el, &s.ExpectedLoss}, {al, &s.AbnormalLoss}} {
		if *p.dst, err = mass.ParseKg(p.s); err != nil {
			return s, err
		}
	}
	return s, nil
}

// ---------------------------------------------------------------------------
// Hàng dự kiến về (bảng chiếu sự kiện đếm cuộn của fieldops)
// ---------------------------------------------------------------------------

// Expected là một bản ghi đếm cuộn đang hiệu lực.
type Expected struct {
	RollCountID  uuid.UUID
	LotRef       uuid.UUID
	PlotRef      uuid.UUID
	HTXRef       uuid.UUID
	Stage        string
	Rolls        int
	SupersedesID *uuid.UUID
	Status       string
	EventTime    time.Time
}

// UpsertExpected ghi một bản ghi đếm cuộn; bản ghi bù đánh dấu bản bị thay là superseded.
// Giao lặp / lệch thứ tự: bản đã bị thay không bao giờ quay lại active.
func UpsertExpected(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, e Expected) error {
	if e.SupersedesID != nil {
		if _, err := tx.Exec(ctx, `INSERT INTO mkg_hub.expected_inbound
		    (tenant_id, roll_count_id, lot_ref, plot_ref, htx_ref, stage, rolls, status, event_time)
		  VALUES ($1, $2, $3, $4, $5, $6, 0, 'superseded', $7)
		  ON CONFLICT (tenant_id, roll_count_id) DO UPDATE SET status = 'superseded', updated_at = now()`,
			tenantID, *e.SupersedesID, e.LotRef, e.PlotRef, e.HTXRef, e.Stage, e.EventTime.UTC()); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `INSERT INTO mkg_hub.expected_inbound
	    (tenant_id, roll_count_id, lot_ref, plot_ref, htx_ref, stage, rolls, supersedes_id, status, event_time)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'active', $9)
	  ON CONFLICT (tenant_id, roll_count_id) DO UPDATE SET lot_ref = EXCLUDED.lot_ref, plot_ref = EXCLUDED.plot_ref,
	    htx_ref = EXCLUDED.htx_ref, stage = EXCLUDED.stage, rolls = EXCLUDED.rolls, supersedes_id = EXCLUDED.supersedes_id,
	    event_time = EXCLUDED.event_time, updated_at = now()`,
		tenantID, e.RollCountID, e.LotRef, e.PlotRef, e.HTXRef, e.Stage, e.Rolls, e.SupersedesID, e.EventTime.UTC())
	return err
}

// ExpectedRolls tổng số cuộn đang hiệu lực của lô theo công đoạn.
func ExpectedRolls(ctx context.Context, tx pgx.Tx, lot uuid.UUID) (map[string]int, error) {
	rows, err := tx.Query(ctx, `SELECT stage, sum(rolls)::int FROM mkg_hub.expected_inbound
	  WHERE lot_ref = $1 AND status = 'active' GROUP BY stage`, lot)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			return nil, err
		}
		out[s] = n
	}
	return out, rows.Err()
}
