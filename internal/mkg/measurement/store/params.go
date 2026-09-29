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

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
)

// Loại tham số.
const (
	ParamTolerance = "tolerance"
	ParamLossNorm  = "loss_norm"
)

// ParamStream là khóa chuỗi của loại tham số.
func ParamStream(kind string) string {
	if kind == ParamTolerance {
		return "tolerance:commercial"
	}
	return "loss_norm:drying"
}

// ParamVersion là một phiên bản tham số kèm trạng thái duyệt.
type ParamVersion struct {
	Kind          string
	ID            uuid.UUID
	Seq           int64
	Tolerance     *domain.Tolerance
	LossNorm      *domain.LossNorm
	EffectiveFrom time.Time
	Provisional   bool
	Reason        string
	ProposedBy    uuid.UUID
	RecordedAt    time.Time
	Status        string
	DecidedBy     *uuid.UUID
	DecidedAt     *time.Time
}

// Label là nhãn phiên bản ghim vào đối soát.
func (p ParamVersion) Label() string { return fmt.Sprintf("%s:v%d", p.Kind, p.Seq) }

// InsertTolerance ghi phiên bản dung sai.
func InsertTolerance(ctx context.Context, tx pgx.Tx, tid, id uuid.UUID, t domain.Tolerance, reason string, actor uuid.UUID, at time.Time, idem string) (int64, time.Time, error) {
	var seq int64
	var rec time.Time
	err := tx.QueryRow(ctx, `INSERT INTO mkg_measurement.tolerance_version
	    (id, tenant_id, stream_key, commercial_flag_pct, commercial_critical_pct, roll_flag_pct, roll_critical_pct, effective_from,
	     provisional, reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4::numeric, $5::numeric, $6::numeric, $7::numeric, $8, $9, $10, $11, $12, $13, $14) RETURNING seq, recorded_at`,
		id, tid, ParamStream(ParamTolerance), t.CommercialFlag.StringFixed(2), t.CommercialCrit.StringFixed(2), t.RollFlag.StringFixed(2),
		t.RollCrit.StringFixed(2), t.EffectiveFrom.UTC(), t.Provisional, reason, at.UTC(), actor, idem, domain.RuleParam).Scan(&seq, &rec)
	return seq, rec, err
}

// InsertLossNorm ghi phiên bản định mức hao hụt sấy.
func InsertLossNorm(ctx context.Context, tx pgx.Tx, tid, id uuid.UUID, n domain.LossNorm, reason string, actor uuid.UUID, at time.Time, idem string) (int64, time.Time, error) {
	bands, err := json.Marshal(n.Bands)
	if err != nil {
		return 0, time.Time{}, err
	}
	var seq int64
	var rec time.Time
	err = tx.QueryRow(ctx, `INSERT INTO mkg_measurement.loss_norm_version
	    (id, tenant_id, stream_key, bands, default_loss_pct, abnormal_alert_pct, effective_from, provisional, reason, event_time,
	     actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4::jsonb, $5::numeric, $6::numeric, $7, $8, $9, $10, $11, $12, $13) RETURNING seq, recorded_at`,
		id, tid, ParamStream(ParamLossNorm), string(bands), n.DefaultLoss.StringFixed(2), n.AlertPct.StringFixed(2), n.EffectiveFrom.UTC(),
		n.Provisional, reason, at.UTC(), actor, idem, domain.RuleParam).Scan(&seq, &rec)
	return seq, rec, err
}

// InsertParamCurrent thêm trạng thái chờ duyệt.
func InsertParamCurrent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, kind string, id uuid.UUID, seq int64, eff time.Time, by uuid.UUID) error {
	_, err := tx.Exec(ctx, `INSERT INTO mkg_measurement.param_current (tenant_id, param_kind, version_id, version_seq, status, effective_from, proposed_by)
	  VALUES ($1, $2, $3, $4, 'pending', $5, $6)`, tid, kind, id, seq, eff.UTC(), by)
	return err
}

// InsertParamDecision ghi quyết định (DB CHECK actor ≠ proposer).
func InsertParamDecision(ctx context.Context, tx pgx.Tx, tid, id uuid.UUID, kind string, versionID uuid.UUID, versionSeq int64,
	decision string, reason *string, proposer, actor uuid.UUID, at time.Time, idem string) error {
	_, err := tx.Exec(ctx, `INSERT INTO mkg_measurement.param_decision
	    (id, tenant_id, stream_key, param_kind, version_id, version_seq, decision, reason, proposer_ref, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		id, tid, ParamStream(kind), kind, versionID, versionSeq, decision, reason, proposer, at.UTC(), actor, idem, domain.RuleParam)
	if err == nil {
		status := "approved"
		if decision == "rejected" {
			status = "rejected"
		}
		_, err = tx.Exec(ctx, `UPDATE mkg_measurement.param_current SET status = $3, decided_by = $4, decided_at = $5, updated_at = now()
		  WHERE param_kind = $1 AND version_id = $2`, kind, versionID, status, actor, at.UTC())
	}
	return err
}

// ParamDecisionExists cho biết đã có quyết định với idem_key.
func ParamDecisionExists(ctx context.Context, tx pgx.Tx, key string) (bool, error) {
	var n int
	err := tx.QueryRow(ctx, "SELECT count(*) FROM mkg_measurement.param_decision WHERE idem_key = $1", key).Scan(&n)
	return n > 0, err
}

// ParamIDByIdem trả id phiên bản tham số đã ghi với idem_key.
func ParamIDByIdem(ctx context.Context, tx pgx.Tx, kind, key string) (*uuid.UUID, error) {
	table := "tolerance_version"
	if kind == ParamLossNorm {
		table = "loss_norm_version"
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, "SELECT id FROM mkg_measurement."+table+" WHERE idem_key = $1", key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func toleranceSelect(where string) string {
	return `SELECT v.id, v.seq, v.commercial_flag_pct::text, v.commercial_critical_pct::text, v.roll_flag_pct::text, v.roll_critical_pct::text,
	  v.effective_from, v.provisional, v.reason, v.actor_ref, v.recorded_at, c.status, c.decided_by, c.decided_at
	  FROM mkg_measurement.tolerance_version v JOIN mkg_measurement.param_current c ON c.param_kind = 'tolerance' AND c.version_id = v.id
	  WHERE ` + where
}

func lossNormSelect(where string) string {
	return `SELECT v.id, v.seq, v.bands::text, v.default_loss_pct::text, v.abnormal_alert_pct::text,
	  v.effective_from, v.provisional, v.reason, v.actor_ref, v.recorded_at, c.status, c.decided_by, c.decided_at
	  FROM mkg_measurement.loss_norm_version v JOIN mkg_measurement.param_current c ON c.param_kind = 'loss_norm' AND c.version_id = v.id
	  WHERE ` + where
}

func scanParam(kind string, row pgx.Row) (ParamVersion, error) {
	p := ParamVersion{Kind: kind}
	if kind == ParamTolerance {
		var a, b, c, d string
		if err := row.Scan(&p.ID, &p.Seq, &a, &b, &c, &d, &p.EffectiveFrom, &p.Provisional, &p.Reason, &p.ProposedBy, &p.RecordedAt,
			&p.Status, &p.DecidedBy, &p.DecidedAt); err != nil {
			return p, err
		}
		t := domain.Tolerance{CommercialFlag: decimal.RequireFromString(a), CommercialCrit: decimal.RequireFromString(b),
			RollFlag: decimal.RequireFromString(c), RollCrit: decimal.RequireFromString(d), EffectiveFrom: p.EffectiveFrom, Provisional: p.Provisional}
		id := p.ID
		t.VersionID, t.Label = &id, p.Label()
		p.Tolerance = &t
		return p, nil
	}
	var bands, def, alert string
	if err := row.Scan(&p.ID, &p.Seq, &bands, &def, &alert, &p.EffectiveFrom, &p.Provisional, &p.Reason, &p.ProposedBy, &p.RecordedAt,
		&p.Status, &p.DecidedBy, &p.DecidedAt); err != nil {
		return p, err
	}
	n := domain.LossNorm{DefaultLoss: decimal.RequireFromString(def), AlertPct: decimal.RequireFromString(alert), EffectiveFrom: p.EffectiveFrom,
		Provisional: p.Provisional}
	if err := json.Unmarshal([]byte(bands), &n.Bands); err != nil {
		return p, err
	}
	id := p.ID
	n.VersionID, n.Label = &id, p.Label()
	p.LossNorm = &n
	return p, nil
}

func selectFor(kind, where string) string {
	if kind == ParamTolerance {
		return toleranceSelect(where)
	}
	return lossNormSelect(where)
}

// ParamBySeq đọc phiên bản theo số (nil khi không có).
func ParamBySeq(ctx context.Context, tx pgx.Tx, kind string, seq int64, forUpdate bool) (*ParamVersion, error) {
	q := selectFor(kind, "v.seq = $1")
	if forUpdate {
		q += " FOR UPDATE OF c"
	}
	p, err := scanParam(kind, tx.QueryRow(ctx, q, seq))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ParamByID đọc phiên bản theo id.
func ParamByID(ctx context.Context, tx pgx.Tx, kind string, id uuid.UUID) (*ParamVersion, error) {
	p, err := scanParam(kind, tx.QueryRow(ctx, selectFor(kind, "v.id = $1"), id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// Params liệt kê mọi phiên bản (mới nhất trước).
func Params(ctx context.Context, tx pgx.Tx, kind string) ([]ParamVersion, error) {
	rows, err := tx.Query(ctx, selectFor(kind, "true")+" ORDER BY v.seq DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ParamVersion
	for rows.Next() {
		p, err := scanParam(kind, rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// EffectiveTolerance trả bảng dung sai ĐÃ DUYỆT có hiệu lực lúc t (mục 16);
// không có → mặc định tạm của prototype (C-4).
func EffectiveTolerance(ctx context.Context, tx pgx.Tx, t time.Time) (domain.Tolerance, error) {
	p, err := scanParam(ParamTolerance, tx.QueryRow(ctx, toleranceSelect(`c.status = 'approved' AND v.effective_from <= $1
	  ORDER BY v.effective_from DESC, v.seq DESC LIMIT 1`), t.UTC()))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DefaultTolerance(), nil
	}
	if err != nil {
		return domain.Tolerance{}, err
	}
	return *p.Tolerance, nil
}

// EffectiveLossNorm như EffectiveTolerance cho định mức hao hụt sấy.
func EffectiveLossNorm(ctx context.Context, tx pgx.Tx, t time.Time) (domain.LossNorm, error) {
	p, err := scanParam(ParamLossNorm, tx.QueryRow(ctx, lossNormSelect(`c.status = 'approved' AND v.effective_from <= $1
	  ORDER BY v.effective_from DESC, v.seq DESC LIMIT 1`), t.UTC()))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DefaultLossNorm(), nil
	}
	if err != nil {
		return domain.LossNorm{}, err
	}
	return *p.LossNorm, nil
}

// ---------------------------------------------------------------------------
// Thuộc tính thương mại của lô
// ---------------------------------------------------------------------------

// Terms là thuộc tính của lô cho đối soát.
type Terms struct {
	ID          uuid.UUID
	LotRef      uuid.UUID
	Basis       port.PricingBasis
	ContractRef *uuid.UUID
	CustomerRef *uuid.UUID
	Seq         int64
}

// InsertTerms ghi thuộc tính (bản sau thay bản trước bằng supersedes).
func InsertTerms(ctx context.Context, tx pgx.Tx, tid uuid.UUID, t *Terms, supersedes *uuid.UUID, reason *string, actor uuid.UUID, at time.Time, idem string) error {
	err := tx.QueryRow(ctx, `INSERT INTO mkg_measurement.lot_terms
	    (id, tenant_id, stream_key, lot_ref, pricing_basis, contract_ref, customer_ref, event_time, supersedes_id, correction_reason,
	     actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING seq`,
		t.ID, tid, "terms:"+t.LotRef.String(), t.LotRef, string(t.Basis), t.ContractRef, t.CustomerRef, at.UTC(), supersedes, reason,
		actor, idem, domain.RuleTerms).Scan(&t.Seq)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO mkg_measurement.lot_terms_current (tenant_id, lot_ref, terms_id, pricing_basis, contract_ref, customer_ref, last_seq)
	  VALUES ($1, $2, $3, $4, $5, $6, $7)
	  ON CONFLICT (tenant_id, lot_ref) DO UPDATE SET terms_id = EXCLUDED.terms_id, pricing_basis = EXCLUDED.pricing_basis,
	    contract_ref = EXCLUDED.contract_ref, customer_ref = EXCLUDED.customer_ref, last_seq = EXCLUDED.last_seq, updated_at = now()`,
		tid, t.LotRef, t.ID, string(t.Basis), t.ContractRef, t.CustomerRef, t.Seq)
	return err
}

// GetTerms đọc thuộc tính hiện hành (nil khi chưa đặt).
func GetTerms(ctx context.Context, tx pgx.Tx, lot uuid.UUID) (*Terms, error) {
	t := Terms{LotRef: lot}
	var basis string
	err := tx.QueryRow(ctx, `SELECT terms_id, pricing_basis, contract_ref, customer_ref, last_seq FROM mkg_measurement.lot_terms_current
	  WHERE lot_ref = $1`, lot).Scan(&t.ID, &basis, &t.ContractRef, &t.CustomerRef, &t.Seq)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	t.Basis = port.PricingBasis(basis)
	return &t, err
}

// TermsByIdem cho biết idem_key đã ghi thuộc tính của lô nào.
func TermsByIdem(ctx context.Context, tx pgx.Tx, key string) (*uuid.UUID, error) {
	var lot uuid.UUID
	err := tx.QueryRow(ctx, "SELECT lot_ref FROM mkg_measurement.lot_terms WHERE idem_key = $1", key).Scan(&lot)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &lot, err
}
