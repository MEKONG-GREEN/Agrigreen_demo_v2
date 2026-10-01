package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Rebuilt là số dòng bảng chiếu sau khi dựng lại.
type Rebuilt struct {
	UsageCounters  int64
	EgressCounters int64
	Statements     int64
}

// RebuildProjections dựng lại ba bảng chiếu của tenant hiện hành CHỈ từ sổ (R-22): bộ đếm sử dụng
// (cửa sổ ngày/tháng giờ VN theo event_time, dòng '*' = tổng), bộ đếm xuất (chỉ phần được cho qua;
// khóa chỉ có lần bị từ chối vẫn có dòng 0 — như bảng chiếu sống tạo khi khóa), và bảng kê hiện hành.
// tx phải ở ngữ cảnh tenant (RLS giới hạn DELETE/INSERT vào tenant đó).
func RebuildProjections(ctx context.Context, tx pgx.Tx) (Rebuilt, error) {
	var r Rebuilt
	for _, q := range []string{
		`DELETE FROM pf_metering.usage_counter_current`,
		`DELETE FROM pf_metering.egress_counter_current`,
		`DELETE FROM pf_metering.statement_current`,
	} {
		if _, err := tx.Exec(ctx, q); err != nil {
			return r, err
		}
	}
	tag, err := tx.Exec(ctx, `INSERT INTO pf_metering.usage_counter_current (tenant_id, client_id, product, window_kind, window_start, units, calls)
	  SELECT tenant_id, client_id, p, k, w, sum(units), count(*) FROM (
	    SELECT tenant_id, client_id, u.product AS p, 'day' AS k, (event_time AT TIME ZONE 'Asia/Ho_Chi_Minh')::date AS w, units FROM pf_metering.usage_event u
	    UNION ALL
	    SELECT tenant_id, client_id, '*', 'day', (event_time AT TIME ZONE 'Asia/Ho_Chi_Minh')::date, units FROM pf_metering.usage_event
	    UNION ALL
	    SELECT tenant_id, client_id, u.product, 'month', date_trunc('month', event_time AT TIME ZONE 'Asia/Ho_Chi_Minh')::date, units FROM pf_metering.usage_event u
	    UNION ALL
	    SELECT tenant_id, client_id, '*', 'month', date_trunc('month', event_time AT TIME ZONE 'Asia/Ho_Chi_Minh')::date, units FROM pf_metering.usage_event
	  ) x GROUP BY tenant_id, client_id, p, k, w`)
	if err != nil {
		return r, err
	}
	r.UsageCounters = tag.RowsAffected()
	tag, err = tx.Exec(ctx, `INSERT INTO pf_metering.egress_counter_current (tenant_id, channel, counterparty_kind, counterparty_ref, business_date,
	    records, bytes, events)
	  SELECT tenant_id, channel, counterparty_kind, counterparty_ref, business_date,
	         coalesce(sum(records) FILTER (WHERE decision = 'allowed'), 0), coalesce(sum(bytes) FILTER (WHERE decision = 'allowed'), 0),
	         count(*) FILTER (WHERE decision = 'allowed')
	  FROM pf_metering.egress_event GROUP BY tenant_id, channel, counterparty_kind, counterparty_ref, business_date`)
	if err != nil {
		return r, err
	}
	r.EgressCounters = tag.RowsAffected()
	tag, err = tx.Exec(ctx, `INSERT INTO pf_metering.statement_current (tenant_id, statement_id, client_id, client_tenant_id, period, status,
	    job_id, snapshot_sha256, first_seq, last_seq, chain_head_hash, rule_version, subscription_vnd, total_vnd, price_version, requested_by,
	    requested_at, approved_by, approved_at, supersedes_id, correction_reason, superseded_by)
	  SELECT d.tenant_id, d.statement_id, d.client_id, d.client_tenant_id, d.period,
	         CASE WHEN a.statement_id IS NULL THEN 'draft' ELSE 'approved' END,
	         d.job_id, d.snapshot_sha256, d.first_seq, d.last_seq, d.chain_head_hash, d.rule_version, d.subscription_vnd, d.total_vnd,
	         d.price_version, d.requested_by, d.event_time, a.actor_ref, a.event_time, d.supersedes_id, d.correction_reason,
	         (SELECT n.statement_id FROM pf_metering.statement n WHERE n.action = 'drafted' AND n.supersedes_id = d.statement_id
	           ORDER BY n.statement_id DESC LIMIT 1)
	  FROM pf_metering.statement d
	  LEFT JOIN pf_metering.statement a ON a.statement_id = d.statement_id AND a.action = 'approved'
	  WHERE d.action = 'drafted'`)
	if err != nil {
		return r, err
	}
	r.Statements = tag.RowsAffected()
	return r, nil
}
