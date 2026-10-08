package store

// Kho feed lớp partner theo bên cấp (W5.5): dùng nguyên bảng pf 000126–000127 (partition_key = tenant bên cấp đã có
// từ W5.3) — không migration mới.

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// FeedGrantees trả tenant bên nhận (client_tenant_id) của mọi khách hàng API active có đăng ký active với product =
// feed — job dựng chỉ dựng phân vùng của bên cấp mà ít nhất một bên nhận như vậy đang có cấp quyền.
func FeedGrantees(ctx context.Context, tx pgx.Tx, feed string) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT c.client_tenant_id
	    FROM pf_apiclient.subscription_current s
	    JOIN pf_apiclient.client_current c ON c.tenant_id = s.tenant_id AND c.client_id = s.client_id
	   WHERE s.product = $1::text AND s.status = 'active' AND c.status = 'active'
	   ORDER BY 1`, feed)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// RefreshDue trả các entity_key còn hiệu lực (record khác NULL) của tập × phân vùng mà lần phát gần nhất (updated_at
// của dataset_record, đồng hồ DB) cũ hơn age — job phát lại chúng (port.PartnerFeedRefresh).
func RefreshDue(ctx context.Context, tx pgx.Tx, dataset, partition string, age time.Duration) (map[string]bool, error) {
	rows, err := tx.Query(ctx, `SELECT entity_key FROM pf_apiclient.dataset_record
	   WHERE dataset = $1::text AND partition_key = $2::text AND record IS NOT NULL
	     AND updated_at < now() - make_interval(secs => $3::double precision)`, dataset, partition, age.Seconds())
	if err != nil {
		return nil, err
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(keys))
	for _, k := range keys {
		out[k] = true
	}
	return out, nil
}

// TouchFeedHead ghi mốc dữ liệu của feed khi lần dựng không có thay đổi: last_recorded_at là as_of của trang feed
// (feed lớp partner — dữ liệu đúng tới lần dựng gần nhất, không chỉ tới mục cuối). Không lùi mốc.
func TouchFeedHead(ctx context.Context, tx pgx.Tx, feed, partition string, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_apiclient.feed_head SET last_recorded_at = $3::timestamptz, updated_at = now()
	  WHERE feed = $1::text AND partition_key = $2::text AND (last_recorded_at IS NULL OR last_recorded_at < $3::timestamptz)`,
		feed, partition, at.UTC())
	return err
}
