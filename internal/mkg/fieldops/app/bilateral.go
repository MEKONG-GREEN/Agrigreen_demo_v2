package app

// Luồng song phương phía MKG (W5.6 — ADR-0034, ADR-0033 §3; Đề xuất, bật production chờ C-12). Job
// fieldops_bilateral (SyncBilateral) chạy SONG SONG với van valve_collection (giữ tới W5.10):
//
//  1. Chiếu: mỗi bản ghi van đã xuất (sổ valve_export) → hàng gửi bilateral_send theo hợp đồng song phương
//     (collection_schedule giữ nguyên; htx_collection_statement → roll_count_statement, schedule_ref = lệnh gom), kiểm
//     hợp đồng contracts/integration TRƯỚC khi gửi (mặc định từ chối); bản ghi không chiếu được đánh dấu bỏ qua.
//  2. Gửi: lô ≤ max_batch mỗi hợp đồng qua POST /v1/batches/{hợp đồng}; batch_ref (Idempotency-Key) ghi TRƯỚC khi gọi →
//     lỗi tạm thì lượt sau gửi lại đúng lô (cổng trả lại cùng lô). Kết quả từng bản ghi qua GET /v1/batches/{id}:
//     accepted | rejected (mã problem). Từ chối integration.pair_missing (HTX chưa lập cặp) xếp lại sau 1 giờ.
//  3. Nhận: kéo feed của cặp GET /v1/feeds/bilateral?pair=<htx_ref> cho mọi HTX đã gửi — bản sao gồm cả bản ghi MKG
//     đã gửi lẫn xác nhận / phản đối của HTX (thay GET /v1/partner/htx-collection/responses sau W5.10); 403
//     integration.pair_missing → cặp chấm dứt (giữ bản sao); 410 → đọc lại từ đầu.
//
// CompareBilateral đối soát tổng kiểm theo ngày (bilateral_day.v1, R-INT-10): công thức chung
// contracts/integration.BilateralDayDigest trên bản sao feed (ngày = recorded_at của sổ song phương) so với
// GET /v1/feeds/{hợp đồng}/checksums?pair= của cổng, và đếm bản ghi đã được nhận nhưng chưa thấy trong feed của cặp.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/integration"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Giới hạn một lượt.
const (
	bilateralStageBatch   = 500
	bilateralStageRounds  = 20
	bilateralPageLimit    = 500
	bilateralMaxPages     = 200
	bilateralRequeueAfter = time.Hour
)

// BilateralSync là kết quả một lượt của job fieldops_bilateral.
type BilateralSync struct {
	Staged, Skipped, Requeued int
	Batches                   int // lô đã gửi (mới + gửi lại)
	Accepted, Rejected        int // kết quả đã nhận ở lượt này
	Pairs, Items, Terminated  int // cặp đã kéo, mục feed đã áp, cặp chấm dứt
	Pending, Submitted        int // còn chờ sau lượt
}

func (s *Service) btx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

// SyncBilateral chạy một lượt chiếu → gửi → nhận kết quả → kéo feed của cặp cho tenant MKG tid.
func (s *Service) SyncBilateral(ctx context.Context, tid uuid.UUID) (BilateralSync, error) {
	var r BilateralSync
	if s.Bilateral == nil {
		return r, errors.New("fieldops: chưa cấu hình cổng tích hợp cho luồng song phương")
	}
	if err := s.stageBilateral(ctx, tid, &r); err != nil {
		return r, fmt.Errorf("chiếu bản ghi van: %w", err)
	}
	if err := s.btx(ctx, tid, func(tx pgx.Tx) error {
		n, err := store.RequeuePairMissing(ctx, tx, s.Clock.Now().Add(-bilateralRequeueAfter))
		r.Requeued = int(n)
		return err
	}); err != nil {
		return r, err
	}
	var errs []error
	if err := s.submitBilateral(ctx, tid, &r); err != nil {
		errs = append(errs, fmt.Errorf("gửi lô: %w", err))
	}
	if err := s.pollBilateral(ctx, tid, &r); err != nil {
		errs = append(errs, fmt.Errorf("kết quả lô: %w", err))
	}
	if err := s.pullPairFeeds(ctx, tid, &r); err != nil {
		errs = append(errs, fmt.Errorf("feed của cặp: %w", err))
	}
	_ = s.btx(ctx, tid, func(tx pgx.Tx) error {
		c, err := store.SendCounts(ctx, tx)
		r.Pending, r.Submitted = c["pending"], c["submitted"]
		return err
	})
	return r, errors.Join(errs...)
}

// projectValve chiếu một bản ghi van sang hợp đồng song phương (đã kiểm hợp đồng, JSON chuẩn).
func projectValve(v store.ValveSource) (store.BilateralSend, error) {
	var (
		name string
		raw  []byte
	)
	switch v.Contract {
	case port.ContractCollectionSchedule:
		name, raw = port.ContractCollectionSchedule, v.Payload
	case port.ContractHTXCollectionStatement:
		var m map[string]any
		dec := json.NewDecoder(bytes.NewReader(v.Payload))
		dec.UseNumber()
		if err := dec.Decode(&m); err != nil {
			return store.BilateralSend{}, err
		}
		m["schedule_ref"] = v.OrderID.String()
		if _, ok := m["season_ref"]; !ok {
			m["season_ref"] = nil
		}
		b, err := json.Marshal(m)
		if err != nil {
			return store.BilateralSend{}, err
		}
		name, raw = port.ContractRollCountStatement, b
	default:
		return store.BilateralSend{}, fmt.Errorf("hợp đồng van %s không có hợp đồng song phương", v.Contract)
	}
	c, err := integration.Get(name, 1)
	if err != nil {
		return store.BilateralSend{}, err
	}
	rec, err := integration.CanonicalJSON(raw)
	if err != nil {
		return store.BilateralSend{}, err
	}
	if err := c.ValidateRecord(rec); err != nil {
		return store.BilateralSend{}, err
	}
	out := store.BilateralSend{SourceID: v.ID, Contract: c.Name, ContractVersion: c.Version, Record: rec}
	if out.EntityKey, err = c.EntityKeyOf(rec); err != nil {
		return out, err
	}
	if out.BusinessKey, err = c.BusinessKeyOf(rec); err != nil {
		return out, err
	}
	key, err := c.PairKeyOf(rec)
	if err != nil {
		return out, err
	}
	if out.HTXRef, err = uuid.Parse(key); err != nil {
		return out, err
	}
	out.PayloadSHA256, err = integration.PayloadSHA256(rec)
	return out, err
}

func (s *Service) stageBilateral(ctx context.Context, tid uuid.UUID, r *BilateralSync) error {
	for round := 0; round < bilateralStageRounds; round++ {
		n := 0
		err := s.btx(ctx, tid, func(tx pgx.Tx) error {
			src, err := store.UnsentValveExports(ctx, tx, bilateralStageBatch)
			if err != nil {
				return err
			}
			n = len(src)
			for _, v := range src {
				row, perr := projectValve(v)
				if perr != nil {
					contract := port.ContractRollCountStatement
					if v.Contract == port.ContractCollectionSchedule {
						contract = port.ContractCollectionSchedule
					}
					if err := store.MarkValveSkipped(ctx, tx, tid, v, contract, "fieldops.bilateral_unmappable"); err != nil {
						return err
					}
					r.Skipped++
					continue
				}
				ok, err := store.InsertBilateralSend(ctx, tx, tid, row)
				if err != nil {
					return err
				}
				if ok {
					r.Staged++
				} else if err := store.MarkValveSkipped(ctx, tx, tid, v, row.Contract, "fieldops.bilateral_duplicate"); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil || n < bilateralStageBatch {
			return err
		}
	}
	return nil
}

func (s *Service) submitBilateral(ctx context.Context, tid uuid.UUID, r *BilateralSync) error {
	// Lô đã gán nhưng chưa có xác nhận 202: gửi lại đúng batch_ref + thứ tự.
	var unacked []store.BilateralSend
	if err := s.btx(ctx, tid, func(tx pgx.Tx) error {
		var err error
		unacked, err = store.UnackedSends(ctx, tx)
		return err
	}); err != nil {
		return err
	}
	byRef := map[uuid.UUID][]store.BilateralSend{}
	var order []uuid.UUID
	for _, x := range unacked {
		if _, ok := byRef[*x.BatchRef]; !ok {
			order = append(order, *x.BatchRef)
		}
		byRef[*x.BatchRef] = append(byRef[*x.BatchRef], x)
	}
	var errs []error
	for _, ref := range order {
		if err := s.sendBatch(ctx, tid, ref, byRef[ref], r); err != nil {
			errs = append(errs, err)
		}
	}
	for _, contract := range []string{port.ContractCollectionSchedule, port.ContractRollCountStatement} {
		c, err := integration.Get(contract, 1)
		if err != nil {
			return err
		}
		max := bilateralStageBatch
		if c.Ingest != nil && c.Ingest.MaxBatch > 0 && c.Ingest.MaxBatch < max {
			max = c.Ingest.MaxBatch
		}
		for i := 0; i < bilateralStageRounds; i++ {
			var rows []store.BilateralSend
			ref := ids.New()
			if err := s.btx(ctx, tid, func(tx pgx.Tx) error {
				var err error
				if rows, err = store.PendingSends(ctx, tx, contract, max); err != nil || len(rows) == 0 {
					return err
				}
				idsOf := make([]uuid.UUID, len(rows))
				for j, x := range rows {
					idsOf[j] = x.SourceID
				}
				return store.MarkSubmitted(ctx, tx, ref, idsOf)
			}); err != nil {
				return err
			}
			if len(rows) == 0 {
				break
			}
			if err := s.sendBatch(ctx, tid, ref, rows, r); err != nil {
				errs = append(errs, err)
				break
			}
			if len(rows) < max {
				break
			}
		}
	}
	return errors.Join(errs...)
}

// sendBatch gửi một lô; lỗi tạm giữ nguyên trạng thái submitted (gửi lại lượt sau), từ chối cả lô đưa về pending.
func (s *Service) sendBatch(ctx context.Context, tid, ref uuid.UUID, rows []store.BilateralSend, r *BilateralSync) error {
	items := make([]port.BilateralItem, len(rows))
	for i, x := range rows {
		items[i] = port.BilateralItem{EntityKey: x.EntityKey, Record: x.Record}
	}
	id, err := s.Bilateral.SubmitBatch(ctx, rows[0].Contract, rows[0].ContractVersion, ref, items)
	switch {
	case errors.Is(err, port.ErrBilateralRetry):
		return err
	case err != nil:
		_ = s.btx(ctx, tid, func(tx pgx.Tx) error { return store.ResetBatch(ctx, tx, ref, "fieldops.bilateral_batch_refused") })
		return err
	}
	r.Batches++
	return s.btx(ctx, tid, func(tx pgx.Tx) error { return store.SetBatchID(ctx, tx, ref, id) })
}

func (s *Service) pollBilateral(ctx context.Context, tid uuid.UUID, r *BilateralSync) error {
	var open map[uuid.UUID]uuid.UUID
	if err := s.btx(ctx, tid, func(tx pgx.Tx) error {
		var err error
		open, err = store.OpenBatches(ctx, tx)
		return err
	}); err != nil {
		return err
	}
	var errs []error
	for id, ref := range open {
		b, err := s.Bilateral.Batch(ctx, id)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		switch b.Status {
		case "completed":
			err = s.btx(ctx, tid, func(tx pgx.Tx) error {
				for _, x := range b.Results {
					if err := store.ApplyBatchResult(ctx, tx, id, x.Index, x.Outcome, x.ProblemCode); err != nil {
						return err
					}
					if x.Outcome == "accepted" {
						r.Accepted++
					} else {
						r.Rejected++
					}
				}
				return nil
			})
		case "dead_lettered":
			err = s.btx(ctx, tid, func(tx pgx.Tx) error { return store.ResetBatch(ctx, tx, ref, "integration.dead_lettered") })
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Service) pullPairFeeds(ctx context.Context, tid uuid.UUID, r *BilateralSync) error {
	var refs []uuid.UUID
	if err := s.btx(ctx, tid, func(tx pgx.Tx) error {
		var err error
		refs, err = store.PairRefs(ctx, tx)
		return err
	}); err != nil {
		return err
	}
	var errs []error
	for _, htx := range refs {
		if err := s.pullPair(ctx, tid, htx, r); err != nil {
			errs = append(errs, fmt.Errorf("HTX %s: %w", htx, err))
		}
	}
	return errors.Join(errs...)
}

// pullPair kéo feed của một cặp tới đầu feed; mỗi trang áp trong MỘT giao dịch cùng cursor.
func (s *Service) pullPair(ctx context.Context, tid, htx uuid.UUID, r *BilateralSync) error {
	var st store.PairFeedState
	if err := s.btx(ctx, tid, func(tx pgx.Tx) error {
		cur, err := store.GetPairFeedState(ctx, tx, htx)
		if cur != nil {
			st = *cur
		}
		st.HTXRef = htx
		return err
	}); err != nil {
		return err
	}
	r.Pairs++
	for page := 0; page < bilateralMaxPages; page++ {
		cursor := ""
		if st.Cursor != nil {
			cursor = *st.Cursor
		}
		p, err := s.Bilateral.PairPage(ctx, htx, cursor, bilateralPageLimit)
		switch {
		case errors.Is(err, port.ErrPairMissing):
			if st.TerminatedAt == nil {
				r.Terminated++
				now := s.Clock.Now()
				st.TerminatedAt = &now
			}
			st.CaughtUp = false
			return s.btx(ctx, tid, func(tx pgx.Tx) error { return store.PutPairFeedState(ctx, tx, tid, st) })
		case errors.Is(err, port.ErrFeedCursorExpired):
			st.Cursor, st.CaughtUp = nil, false // mục đã áp giữ nguyên (seq ổn định) — đọc lại phần còn giữ
			continue
		case err != nil:
			msg := trimErr(err.Error())
			st.LastError = &msg
			_ = s.btx(ctx, tid, func(tx pgx.Tx) error { return store.PutPairFeedState(ctx, tx, tid, st) })
			return err
		}
		recs := make([]store.PairFeedRecord, 0, len(p.Items))
		for _, it := range p.Items {
			c, err := integration.Get(it.Contract, it.ContractVersion)
			if err != nil {
				return fmt.Errorf("mục seq %d: hợp đồng %s.v%d không có: %w", it.Seq, it.Contract, it.ContractVersion, err)
			}
			if err := c.ValidateRecord(it.Record); err != nil {
				return fmt.Errorf("mục seq %d lệch hợp đồng %s: %w", it.Seq, c.ID(), err)
			}
			rec, err := integration.CanonicalJSON(it.Record)
			if err != nil {
				return err
			}
			bk, err := c.BusinessKeyOf(rec)
			if err != nil {
				return err
			}
			sha, err := integration.PayloadSHA256(rec)
			if err != nil {
				return err
			}
			recs = append(recs, store.PairFeedRecord{HTXRef: htx, Seq: it.Seq, EventID: it.EventID, Contract: c.Name, ContractVersion: c.Version,
				EntityKey: it.EntityKey, BusinessKey: bk, Record: rec, PayloadSHA256: sha, RecordedAt: it.RecordedAt})
		}
		next := p.NextCursor
		st.Cursor, st.TerminatedAt, st.LastError = &next, nil, nil
		st.CaughtUp = !p.HasMore
		if st.CaughtUp {
			at := p.AsOf
			st.AsOf = &at
		}
		if err := s.btx(ctx, tid, func(tx pgx.Tx) error {
			for _, x := range recs {
				if err := store.InsertPairFeedRecord(ctx, tx, tid, x); err != nil {
					return err
				}
			}
			return store.PutPairFeedState(ctx, tx, tid, st)
		}); err != nil {
			return err
		}
		r.Items += len(recs)
		if !p.HasMore {
			return nil
		}
	}
	return nil
}

func trimErr(s string) string {
	if r := []rune(s); len(r) > 300 {
		return string(r[:300])
	}
	return s
}

// BilateralMismatch là một ngày lệch tổng kiểm giữa bản sao MKG và cổng.
type BilateralMismatch struct {
	Contract     string
	HTXRef       uuid.UUID
	Date         string
	LocalCount   int64
	LocalSHA256  string
	RemoteCount  int64
	RemoteSHA256 string
}

// BilateralComparison là kết quả đối soát tổng kiểm theo ngày.
type BilateralComparison struct {
	Days       int // số (hợp đồng × cặp × ngày) đã so
	Matched    int
	Mismatches []BilateralMismatch
	// NotInFeed: bản ghi cổng đã nhận (accepted) chưa thấy trong bản sao feed của cặp (chưa dựng feed / mất mục).
	NotInFeed int
}

// CompareBilateral đối soát tổng kiểm ngày [from, to] của mọi hợp đồng song phương × mọi cặp đang theo dõi.
func (s *Service) CompareBilateral(ctx context.Context, tid uuid.UUID, from, to clock.Date) (BilateralComparison, error) {
	var out BilateralComparison
	if s.Bilateral == nil {
		return out, errors.New("fieldops: chưa cấu hình cổng tích hợp cho luồng song phương")
	}
	start, end := clock.StartOfBusinessDate(from), clock.StartOfBusinessDate(to).Add(24*time.Hour)
	var refs []uuid.UUID
	if err := s.btx(ctx, tid, func(tx pgx.Tx) error {
		var err error
		if refs, err = store.PairRefs(ctx, tx); err != nil {
			return err
		}
		out.NotInFeed, err = store.AcceptedNotInFeed(ctx, tx)
		return err
	}); err != nil {
		return out, err
	}
	for _, htx := range refs {
		for _, contract := range port.BilateralContracts {
			remote, err := s.Bilateral.DayChecksums(ctx, contract, htx, from.String(), to.String())
			if errors.Is(err, port.ErrPairMissing) {
				break // cặp chưa từng lập với tenant này
			}
			if err != nil {
				return out, fmt.Errorf("tổng kiểm %s HTX %s: %w", contract, htx, err)
			}
			var recs []store.PairFeedRecord
			if err := s.btx(ctx, tid, func(tx pgx.Tx) error {
				var err error
				recs, err = store.PairFeedRecords(ctx, tx, contract, htx, start, end)
				return err
			}); err != nil {
				return out, err
			}
			byDay := map[string][]integration.BilateralDayRecord{}
			for _, x := range recs {
				d := clock.BusinessDate(x.RecordedAt).String()
				byDay[d] = append(byDay[d], integration.BilateralDayRecord{BusinessKey: x.BusinessKey, ContractVersion: x.ContractVersion,
					PayloadSHA256: x.PayloadSHA256})
			}
			for _, rd := range remote {
				n, sum := integration.BilateralDayDigest(byDay[rd.Date])
				out.Days++
				if n == rd.RecordCount && sum == rd.SHA256 {
					out.Matched++
					continue
				}
				out.Mismatches = append(out.Mismatches, BilateralMismatch{Contract: contract, HTXRef: htx, Date: rd.Date, LocalCount: n,
					LocalSHA256: sum, RemoteCount: rd.RecordCount, RemoteSHA256: rd.SHA256})
			}
		}
	}
	return out, nil
}
