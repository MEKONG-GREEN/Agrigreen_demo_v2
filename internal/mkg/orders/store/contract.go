package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// Contract là ảnh chụp một phiên bản hợp đồng mua rơm (dòng sổ contract_event / bảng chiếu).
type Contract struct {
	Meta
	Kind           string
	ContractID     uuid.UUID
	Version        int
	Status         string
	Code           string
	HTXRef         uuid.UUID
	Basis          string
	UnitPrice      decimal.Decimal
	CommittedKg    decimal.Decimal
	MaxMoisturePct decimal.NullDecimal
	ValidFrom      time.Time
	ValidTo        time.Time
	Note           *string
	ProposerRef    uuid.UUID
	ApproverRef    *uuid.UUID
	Reason         *string
	LastEventID    uuid.UUID
}

const contractCols = `contract_id, version, status, code, htx_ref, basis, unit_price, committed_kg, max_moisture_pct, valid_from,
  valid_to, note, proposer_ref, approver_ref, reason, event_time`

const contractSelect = `contract_id, version, status, code, htx_ref, basis, unit_price::text, committed_kg::text, max_moisture_pct::text,
  valid_from, valid_to, note, proposer_ref, approver_ref, reason, event_time, last_event_id, last_seq`

func scanContract(row pgx.Row) (Contract, error) {
	var c Contract
	err := row.Scan(&c.ContractID, &c.Version, &c.Status, &c.Code, &c.HTXRef, &c.Basis, &c.UnitPrice, &c.CommittedKg, &c.MaxMoisturePct,
		&c.ValidFrom, &c.ValidTo, &c.Note, &c.ProposerRef, &c.ApproverRef, &c.Reason, &c.EventTime, &c.LastEventID, &c.Seq)
	return c, err
}

// InsertContractEvent ghi sổ hợp đồng và dựng lại bảng chiếu của hợp đồng (cùng giao dịch).
func InsertContractEvent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, c *Contract) error {
	err := tx.QueryRow(ctx, `INSERT INTO mkg_orders.contract_event
	    (id, tenant_id, stream_key, contract_id, version, kind, status, code, htx_ref, basis, unit_price, committed_kg, max_moisture_pct,
	     valid_from, valid_to, note, proposer_ref, approver_ref, reason, event_time, supersedes_id, correction_reason, actor_ref, idem_key,
	     rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::numeric, $12::numeric, $13::numeric, $14, $15, $16, $17, $18, $19, $20, $21,
	          $22, $23, $24, $25)
	  RETURNING seq, recorded_at`,
		c.ID, tid, "contract:"+c.ContractID.String(), c.ContractID, c.Version, c.Kind, c.Status, c.Code, c.HTXRef, c.Basis,
		c.UnitPrice.String(), c.CommittedKg.String(), c.MaxMoisturePct, c.ValidFrom, c.ValidTo, c.Note, c.ProposerRef, c.ApproverRef,
		c.Reason, c.EventTime.UTC(), c.SupersedesID, c.CorrectionReason, c.ActorRef, c.IdemKey, c.RuleVersion).Scan(&c.Seq, &c.RecordedAt)
	if err != nil {
		return err
	}
	_, err = rebuildProjection(ctx, tx, "contract_current", "contract_event", "contract_id, version", contractCols, "contract_id = $1", c.ContractID)
	return err
}

// ContractVersions trả mọi phiên bản của hợp đồng (mới nhất trước).
func ContractVersions(ctx context.Context, tx pgx.Tx, id uuid.UUID) ([]Contract, error) {
	return queryContracts(ctx, tx, "SELECT "+contractSelect+" FROM mkg_orders.contract_current WHERE contract_id = $1 ORDER BY version DESC", id)
}

// ContractVersion đọc một phiên bản; forUpdate khóa dòng.
func ContractVersion(ctx context.Context, tx pgx.Tx, id uuid.UUID, version int) (*Contract, error) {
	return noRows(scanContract(tx.QueryRow(ctx, "SELECT "+contractSelect+
		" FROM mkg_orders.contract_current WHERE contract_id = $1 AND version = $2", id, version)))
}

// ActiveContracts trả hợp đồng hiệu lực (status active) của HTX chồng lên [from, to], trừ hợp đồng except (CT-01).
func ActiveContracts(ctx context.Context, tx pgx.Tx, htx uuid.UUID, from, to time.Time, except uuid.UUID) ([]Contract, error) {
	return queryContracts(ctx, tx, "SELECT "+contractSelect+` FROM mkg_orders.contract_current
	    WHERE htx_ref = $1 AND status = 'active' AND valid_from <= $3 AND valid_to >= $2 AND contract_id <> $4 ORDER BY valid_from`,
		htx, from, to, except)
}

// ContractOn trả hợp đồng hiệu lực của HTX bao trùm ngày on (proto contractFor); nil = không có.
func ContractOn(ctx context.Context, tx pgx.Tx, htx uuid.UUID, on time.Time) (*Contract, error) {
	return noRows(scanContract(tx.QueryRow(ctx, "SELECT "+contractSelect+` FROM mkg_orders.contract_current
	    WHERE htx_ref = $1 AND status = 'active' AND valid_from <= $2 AND valid_to >= $2 ORDER BY valid_from DESC, version DESC LIMIT 1`,
		htx, on)))
}

// ListContracts liệt kê phiên bản mới nhất của mỗi hợp đồng, lọc theo HTX / trạng thái.
func ListContracts(ctx context.Context, tx pgx.Tx, htx *uuid.UUID, status string, limit int) ([]Contract, error) {
	return queryContracts(ctx, tx, "SELECT "+contractSelect+` FROM (
	    SELECT DISTINCT ON (contract_id) * FROM mkg_orders.contract_current ORDER BY contract_id, version DESC) c
	  WHERE ($1::uuid IS NULL OR htx_ref = $1) AND ($2::text = '' OR status = $2::text) ORDER BY code DESC LIMIT $3`, htx, status, limit)
}

func queryContracts(ctx context.Context, tx pgx.Tx, q string, args ...any) ([]Contract, error) {
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Contract
	for rows.Next() {
		c, err := scanContract(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
