package app

// Feed của cặp song phương (W5.6 — ADR-0034 §4, R-INT-05 ngoại lệ "lọc theo cặp"): GET /v1/feeds/bilateral?pair=…
// đọc kho feed W5.3 (pf_apiclient.feed_head/feed_entry) với feed = "bilateral", partition_key = mã cặp
// (port.PairID(counterparty, htx_ref)). Job dựng (BuildBilateral, trong apiclient_feed_build của cmd/worker):
//
//  1. bên nhận = client_tenant_id của khách hàng API active có đăng ký active với một hợp đồng song phương;
//  2. cặp = mọi cặp (kể cả đã chấm dứt — phát nốt bản ghi đã có) mà bên nhận là counterparty (port.BilateralLedger,
//     chạy ở tenant bên nhận — nhánh counterparty của chính sách hai bên);
//  3. mỗi cặp: chép bản ghi của sổ song phương có seq > last_seq của đầu feed, theo seq — seq của feed = seq của sổ
//     (liên tục từ 1, theo thứ tự commit) nên đầu feed chính là vạch đã chép, không sót, không trùng; event_id = mã
//     bản ghi sổ (idempotent phía bên nhận), recorded_at = recorded_at của sổ (ngày của tổng kiểm bilateral_day.v1),
//     contract = tên hợp đồng của bản ghi. Mọi bản ghi qua ValidateRecord của hợp đồng trước khi ghi.
//
// Chỉ ghi thêm; không bao giờ retract (RB-1). Bản ghi song phương không đi vào tập dữ liệu, ảnh chụp hay feed nào khác.

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/integration"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/store"
)

// pairFeedBatch là số bản ghi tối đa chép mỗi giao dịch của một cặp.
const pairFeedBatch = 1000

// pairGrantees trả tenant bên nhận có đăng ký active với ít nhất một hợp đồng song phương.
func (s *Service) pairGrantees(ctx context.Context) ([]uuid.UUID, error) {
	var out []uuid.UUID
	err := s.operatorTx(ctx, func(tx pgx.Tx) error {
		for _, c := range integration.BilateralContracts() {
			gs, err := store.FeedGrantees(ctx, tx, c)
			if err != nil {
				return err
			}
			for _, g := range gs {
				if !slices.Contains(out, g) {
					out = append(out, g)
				}
			}
		}
		return nil
	})
	slices.SortFunc(out, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	return out, err
}

// BuildBilateral chép bản ghi mới của mọi cặp song phương vào feed của cặp. Lỗi một cặp không chặn cặp khác.
func (s *Service) BuildBilateral(ctx context.Context) ([]BuildResult, error) {
	if s.Bilateral == nil {
		return nil, port.ErrBilateralUnavailable
	}
	grantees, err := s.pairGrantees(ctx)
	if err != nil {
		return nil, err
	}
	var (
		out  []BuildResult
		errs []error
	)
	for _, g := range grantees {
		pairs, err := s.Bilateral.CounterpartyPairs(ctx, g)
		if err != nil {
			errs = append(errs, fmt.Errorf("bên nhận %s: %w", g, err))
			continue
		}
		for _, p := range pairs {
			n, err := s.copyPair(ctx, g, p.PairID)
			if err != nil {
				errs = append(errs, fmt.Errorf("cặp %s: %w", p.PairID, err))
				continue
			}
			out = append(out, BuildResult{Dataset: port.FeedBilateral, Changed: n > 0, Upserts: n})
		}
	}
	return out, errors.Join(errs...)
}

// copyPair chép bản ghi của một cặp theo lô tới khi hết rồi nâng mốc as_of của đầu feed (dữ liệu đúng tới lần dựng
// này, kể cả khi không có gì mới); trả số mục đã thêm.
func (s *Service) copyPair(ctx context.Context, counterparty, pairID uuid.UUID) (int, error) {
	n, err := s.copyPairRecords(ctx, counterparty, pairID)
	if err != nil {
		return n, err
	}
	now := s.Clock.Now()
	return n, s.operatorTx(ctx, func(tx pgx.Tx) error {
		if _, err := store.LockFeedHead(ctx, tx, s.OperatorTenant, port.FeedBilateral, pairID.String()); err != nil {
			return err
		}
		return store.TouchFeedHead(ctx, tx, port.FeedBilateral, pairID.String(), now)
	})
}

func (s *Service) copyPairRecords(ctx context.Context, counterparty, pairID uuid.UUID) (int, error) {
	total := 0
	partition := pairID.String()
	for {
		var last int64
		if err := s.operatorTx(ctx, func(tx pgx.Tx) error {
			h, err := store.GetFeedHead(ctx, tx, port.FeedBilateral, partition)
			if h != nil {
				last = h.LastSeq
			}
			return err
		}); err != nil {
			return total, err
		}
		recs, err := s.Bilateral.PairRecords(ctx, counterparty, pairID, last, pairFeedBatch)
		if err != nil || len(recs) == 0 {
			return total, err
		}
		es := make([]store.FeedEntry, 0, len(recs))
		for i, r := range recs {
			if r.Seq != last+int64(i)+1 {
				return total, fmt.Errorf("seq của sổ song phương không liên tục: muốn %d, có %d", last+int64(i)+1, r.Seq)
			}
			c, err := integration.Get(r.Contract, r.ContractVersion)
			if err != nil {
				return total, port.ErrRecordRejected.WithDetail(r.Contract).WithCause(err)
			}
			if err := c.ValidateRecord(r.Payload); err != nil {
				return total, port.ErrRecordRejected.WithDetail(c.ID()).WithCause(err)
			}
			es = append(es, store.FeedEntry{Feed: port.FeedBilateral, Partition: partition, Seq: r.Seq, EventID: r.ID, EntityKey: r.EntityKey,
				Op: "upsert", ContractVersion: r.ContractVersion, Record: r.Payload, RecordedAt: r.RecordedAt, Contract: r.Contract})
		}
		err = s.operatorTx(ctx, func(tx pgx.Tx) error {
			h, err := store.LockFeedHead(ctx, tx, s.OperatorTenant, port.FeedBilateral, partition)
			if err != nil {
				return err
			}
			if h.LastSeq != last {
				return errConcurrentBuild
			}
			if err := store.InsertFeedEntries(ctx, tx, s.OperatorTenant, es); err != nil {
				return err
			}
			return store.AdvanceFeedHead(ctx, tx, port.FeedBilateral, partition, es[len(es)-1].Seq, es[len(es)-1].RecordedAt)
		})
		if errors.Is(err, errConcurrentBuild) {
			continue // một bản dựng khác vừa chép — đọc lại vạch
		}
		if err != nil {
			return total, err
		}
		total += len(es)
		if len(recs) < pairFeedBatch {
			return total, nil
		}
	}
}

var errConcurrentBuild = errors.New("feed cặp: đầu feed đổi trong lúc chép")

// ReadPairFeed đọc tối đa limit mục sau cursor after (nil = từ đầu phần còn giữ) của feed cặp pairID.
func (s *Service) ReadPairFeed(ctx context.Context, pairID uuid.UUID, after *int64, limit int) (FeedPage, error) {
	var p FeedPage
	partition := pairID.String()
	err := s.operatorTx(ctx, func(tx pgx.Tx) error {
		head, err := store.GetFeedHead(ctx, tx, port.FeedBilateral, partition)
		if err != nil {
			return err
		}
		if head == nil {
			head = &store.FeedHead{}
		}
		if after != nil && *after > head.LastSeq {
			return port.ErrInvalidCursor.WithDetail("cursor vượt đầu feed")
		}
		if after != nil && *after < head.PurgedThrough {
			p.Expired = true
			return nil
		}
		start := head.PurgedThrough
		if after != nil {
			start = *after
		}
		es, err := store.PairFeedEntriesAfter(ctx, tx, port.FeedBilateral, partition, start, limit+1)
		if err != nil {
			return err
		}
		if len(es) > limit {
			es, p.HasMore = es[:limit], true
		}
		p.Entries, p.Next = es, start
		if len(es) > 0 {
			p.Next = es[len(es)-1].Seq
		}
		if head.LastRecordedAt != nil {
			p.AsOf = *head.LastRecordedAt
		} else {
			p.AsOf = s.Clock.Now()
		}
		return nil
	})
	return p, err
}
