package opsjobs

import (
	"bytes"
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// ChainCheck là kết quả xác minh một chuỗi (sổ, tenant, stream).
type ChainCheck struct {
	Ledger      string
	Table       string
	TenantID    uuid.UUID
	StreamKey   string
	OK          bool
	RowsChecked int64
	BrokenAtSeq *int64
	Reason      string
}

// VerifyAllChains chạy shared.verify_chain cho mọi chuỗi trong
// shared.chain_head, và đối chiếu đầu chuỗi (last_seq, last_hash) để bắt cả
// việc xóa dòng cuối. pool là ag_migrator: owner nhưng bị FORCE RLS, nên mỗi
// chuỗi được kiểm trong một giao dịch có app.tenant_id của chuỗi đó.
func VerifyAllChains(ctx context.Context, pool *pgxpool.Pool) ([]ChainCheck, error) {
	tables, err := LedgerTables(ctx, pool)
	if err != nil {
		return nil, err
	}
	byLedger := map[string]string{}
	for _, t := range tables {
		if t.Ledger != "" {
			byLedger[t.Ledger] = t.Table
		}
	}
	type head struct {
		ledger, stream string
		tenant         uuid.UUID
		seq            int64
		hash           []byte
	}
	rows, err := pool.Query(ctx, `SELECT ledger, tenant_id, stream_key, last_seq, last_hash
	  FROM shared.chain_head ORDER BY ledger, tenant_id, stream_key`)
	if err != nil {
		return nil, err
	}
	var heads []head
	for rows.Next() {
		var h head
		if err := rows.Scan(&h.ledger, &h.tenant, &h.stream, &h.seq, &h.hash); err != nil {
			rows.Close()
			return nil, err
		}
		heads = append(heads, h)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]ChainCheck, 0, len(heads))
	for _, h := range heads {
		c := ChainCheck{Ledger: h.ledger, TenantID: h.tenant, StreamKey: h.stream, Table: byLedger[h.ledger]}
		if c.Table == "" {
			c.Reason = "không tìm thấy bảng sổ có trigger trg_chain cho sổ này"
			out = append(out, c)
			continue
		}
		err := tenant.WithTenantTxOptions(ctx, pool, h.tenant, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead}, func(tx pgx.Tx) error {
			r, err := ledger.Verify(ctx, tx, c.Table, h.tenant, h.stream)
			if err != nil {
				return err
			}
			c.OK, c.RowsChecked, c.BrokenAtSeq = r.OK, r.RowsChecked, r.BrokenAtSeq
			if r.Reason != nil {
				c.Reason = *r.Reason
			}
			if !c.OK {
				return nil
			}
			// verify_chain chỉ kiểm tính liền mạch; đối chiếu đầu chuỗi để bắt xóa dòng cuối.
			if c.RowsChecked != h.seq {
				c.OK = false
				c.Reason = fmt.Sprintf("số dòng %d khác last_seq %d của chain_head (thiếu dòng cuối?)", c.RowsChecked, h.seq)
				next := c.RowsChecked + 1
				c.BrokenAtSeq = &next
				return nil
			}
			if h.seq == 0 {
				return nil
			}
			var last []byte
			// c.Table do format('%I.%I') của catalog trả về — đã trích dẫn an toàn.
			if err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT row_hash FROM %s WHERE tenant_id = $1 AND stream_key = $2
			    ORDER BY seq DESC LIMIT 1`, c.Table), h.tenant, h.stream).Scan(&last); err != nil {
				return err
			}
			if !bytes.Equal(last, h.hash) {
				c.OK = false
				c.Reason = "row_hash dòng cuối khác last_hash của chain_head"
				seq := h.seq
				c.BrokenAtSeq = &seq
			}
			return nil
		})
		if err != nil {
			return out, fmt.Errorf("opsjobs: xác minh %s/%s: %w", c.Ledger, c.StreamKey, err)
		}
		out = append(out, c)
	}
	return out, nil
}
