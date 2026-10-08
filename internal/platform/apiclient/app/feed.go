package app

// Kho feed và tập dữ liệu dựng sẵn của cổng tích hợp (W5.3 — R-INT-05, R-INT-06, R-INT-10; ADR-0033 §2,
// ADR-0035). Mọi bản ghi thuộc tenant ĐƠN VỊ VẬN HÀNH (OperatorTenant): tập dữ liệu tính một lần, phát cho mọi
// đối tác có quyền, không lọc từng hàng theo đối tác.
//
// Ghi (job cmd/worker): publish so tập bản ghi mới với trạng thái đã phát (dataset_record) → chỉ nối upsert cho
// bản ghi đổi và retract cho bản ghi biến mất, theo entity_key; MỌI bản ghi qua ValidateRecord của hợp đồng
// contracts/integration TRƯỚC khi ghi (mặc định từ chối — một bản ghi sai thì cả phiên bản không ghi). Tập theo
// phiên bản: một dòng dataset_version bất biến (tổng kiểm = băm ảnh chụp NDJSON) + ảnh chụp ở kho đối tượng
// trong lãnh thổ (khóa đối tượng, KHÔNG lưu URL). Không có thay đổi → không phiên bản mới.
//
// Đọc (cổng): ReadFeed theo cursor (seq), SnapshotURL cấp URL ký ≤ 15 phút lúc yêu cầu.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/integration"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// FeedRuleVersion là phiên bản quy tắc dựng (cột rule_version của dataset_version).
const FeedRuleVersion = "w5.3-1"

// FeedContract trả hợp đồng feed hợp lệ phiên bản cao nhất của name (version = 0) hoặc đúng phiên bản; không có
// → port.ErrUnknownFeed. Hợp đồng retired coi như không có.
func FeedContract(name string, version int) (*integration.Contract, error) {
	if version > 0 {
		c, err := integration.Get(name, version)
		if err != nil || c.Kind != integration.KindFeed || c.Status == integration.StatusRetired {
			return nil, port.ErrUnknownFeed
		}
		return c, nil
	}
	all, err := integration.All()
	if err != nil {
		return nil, err
	}
	var best *integration.Contract
	for _, c := range all {
		if c.Name == name && c.Kind == integration.KindFeed && c.Status != integration.StatusRetired && (best == nil || c.Version > best.Version) {
			best = c
		}
	}
	if best == nil {
		return nil, port.ErrUnknownFeed
	}
	return best, nil
}

// BuildResult là kết quả một lần dựng.
type BuildResult struct {
	Dataset     string
	Version     int64 // 0 với feed không theo phiên bản
	Changed     bool
	Upserts     int
	Retracts    int
	RecordCount int64
	SHA256      string
	KUsed       int
	Suppressed  int
	SnapshotKey string
}

// shaped là đầu ra của bước dựng bản ghi (chạy trong giao dịch, sau khi khóa đầu feed — đọc trạng thái ẩn).
type shaped struct {
	records    []domain.KeyedRecord
	marks      map[string]domain.KMark
	suppressed *int
}

type publishInput struct {
	contract      *integration.Contract
	partition     string
	asOf          time.Time
	adminUnitAsOf *time.Time
	kUsed         *int
	hysteresis    *int
	contributors  *int
	shape         func(tx pgx.Tx, prevHidden map[string]bool) (shaped, error)
	// refresh > 0 (feed lớp partner, W5.5): phát lại bản ghi còn hiệu lực có lần phát gần nhất cũ hơn refresh
	// (port.PartnerFeedRefresh) — phần feed còn giữ luôn là trạng thái đầy đủ.
	refresh time.Duration
	// touch (feed lớp partner, W5.5): lần dựng không đổi vẫn nâng mốc as_of của đầu feed (store.TouchFeedHead).
	touch bool
}

func (s *Service) operatorTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	if s.OperatorTenant == uuid.Nil {
		return ErrDependencyUnavailable.WithDetail("chưa đặt tenant đơn vị vận hành (AG_PUBLIC_OPERATOR_TENANT)")
	}
	return tenant.WithTenantTx(ctx, s.DB, s.OperatorTenant, fn)
}

// validateRecords kiểm từng bản ghi theo hợp đồng và khớp entity_key; trả bản ghi ở dạng JSON chuẩn.
func validateRecords(c *integration.Contract, recs []domain.KeyedRecord) ([]domain.KeyedRecord, error) {
	out := make([]domain.KeyedRecord, 0, len(recs))
	for _, r := range recs {
		if err := c.ValidateRecord(r.Record); err != nil {
			return nil, port.ErrRecordRejected.WithDetail(fmt.Sprintf("%s %s: %v", c.ID(), r.EntityKey, err)).WithCause(err)
		}
		key, err := c.EntityKeyOf(r.Record)
		if err != nil || key != r.EntityKey {
			return nil, port.ErrRecordRejected.WithDetail(fmt.Sprintf("%s: entity_key %q khác khóa dựng từ bản ghi %q", c.ID(), r.EntityKey, key))
		}
		can, err := domain.CanonicalRecord(r.Record)
		if err != nil {
			return nil, err
		}
		out = append(out, domain.KeyedRecord{EntityKey: r.EntityKey, Record: can})
	}
	return out, nil
}

// publish ghi một lần dựng vào kho feed (một giao dịch, tenant đơn vị vận hành).
func (s *Service) publish(ctx context.Context, in publishInput) (BuildResult, error) {
	c := in.contract
	res := BuildResult{Dataset: c.Name}
	versioned := c.Delivery != nil && c.Delivery.DatasetVersioned
	now := s.Clock.Now().UTC()
	err := s.operatorTx(ctx, func(tx pgx.Tx) error {
		head, err := store.LockFeedHead(ctx, tx, s.OperatorTenant, c.Name, in.partition)
		if err != nil {
			return err
		}
		prevHidden, err := store.CellStates(ctx, tx, c.Name)
		if err != nil {
			return err
		}
		sh, err := in.shape(tx, prevHidden)
		if err != nil {
			return err
		}
		recs, err := validateRecords(c, sh.records)
		if err != nil {
			return err
		}
		body, count, sum, err := domain.Snapshot(recs)
		if err != nil {
			return err
		}
		res.RecordCount, res.SHA256 = count, sum
		if sh.suppressed != nil {
			res.Suppressed = *sh.suppressed
		}
		prev, err := store.DatasetRecords(ctx, tx, c.Name, in.partition)
		if err != nil {
			return err
		}
		type change struct {
			key string
			rec json.RawMessage
			sha string
		}
		var changes []change
		seen := map[string]bool{}
		var due map[string]bool
		if in.refresh > 0 {
			if due, err = store.RefreshDue(ctx, tx, c.Name, in.partition, in.refresh); err != nil {
				return err
			}
		}
		for _, r := range recs {
			seen[r.EntityKey] = true
			sha := domain.RecordSHA256(r.Record)
			if p, ok := prev[r.EntityKey]; !ok || p.SHA256 != sha || due[r.EntityKey] {
				changes = append(changes, change{key: r.EntityKey, rec: r.Record, sha: sha})
				res.Upserts++
			}
		}
		for k, p := range prev {
			if p.SHA256 != "" && !seen[k] {
				changes = append(changes, change{key: k})
				res.Retracts++
			}
		}
		slices.SortFunc(changes, func(a, b change) int { return strings.Compare(a.key, b.key) })

		latest, err := store.LatestDatasetVersion(ctx, tx, c.Name)
		if err != nil {
			return err
		}
		var version *int64
		markVersion := int64(1)
		if latest != nil {
			markVersion = latest.Version
		}
		if len(changes) > 0 || (versioned && latest == nil) {
			res.Changed = true
			if versioned {
				v := int64(1)
				if latest != nil {
					v = latest.Version + 1
				}
				version, markVersion, res.Version = &v, v, v
			}
		} else if latest != nil {
			res.Version = latest.Version
		}
		for k, m := range sh.marks {
			if err := store.PutCellState(ctx, tx, s.OperatorTenant, c.Name, k, m.Suppressed, m.Reason, markVersion); err != nil {
				return err
			}
		}
		if !res.Changed {
			if in.touch {
				return store.TouchFeedHead(ctx, tx, c.Name, in.partition, now)
			}
			return nil
		}
		entries := make([]store.FeedEntry, len(changes))
		for i, ch := range changes {
			op := "upsert"
			if ch.rec == nil {
				op = "retract"
			}
			entries[i] = store.FeedEntry{Feed: c.Name, Partition: in.partition, Seq: head.LastSeq + int64(i) + 1, EventID: ids.New(),
				EntityKey: ch.key, Op: op, ContractVersion: c.Version, DatasetVersion: version, Record: ch.rec, RecordedAt: now}
		}
		if err := store.InsertFeedEntries(ctx, tx, s.OperatorTenant, entries); err != nil {
			return err
		}
		for _, ch := range changes {
			if err := store.PutDatasetRecord(ctx, tx, s.OperatorTenant, c.Name, in.partition, ch.key, ch.rec, ch.sha, version); err != nil {
				return err
			}
		}
		last := head.LastSeq + int64(len(entries))
		if err := store.AdvanceFeedHead(ctx, tx, c.Name, in.partition, last, now); err != nil {
			return err
		}
		if !versioned {
			return nil
		}
		v := store.DatasetVersion{Dataset: c.Name, Version: *version, ContractVersion: c.Version, BuiltAt: now, AsOf: in.asOf,
			AdminUnitAsOf: in.adminUnitAsOf, KUsed: in.kUsed, Hysteresis: in.hysteresis, Contributors: in.contributors, RecordCount: count,
			SHA256: sum, Upserts: res.Upserts, Retracts: res.Retracts, Suppressed: sh.suppressed, RuleVersion: FeedRuleVersion}
		if len(entries) > 0 {
			first := head.LastSeq + 1
			v.FirstSeq, v.LastSeq = &first, &last
		}
		if c.Delivery.Snapshot == "signed_url" {
			if s.Store == nil {
				s.log().WarnContext(ctx, "feed: chưa có kho đối tượng — phiên bản không có ảnh chụp", "event", "integration.snapshot_skipped",
					"dataset", c.Name, "version", *version, "outcome", "degraded")
			} else {
				key := SnapshotKey(s.OperatorTenant, c, *version, ids.New())
				if _, err := s.Store.Put(ctx, key, strings.NewReader(string(body)), int64(len(body)), sum, "application/x-ndjson"); err != nil {
					return fmt.Errorf("feed: ghi ảnh chụp %s: %w", key, err)
				}
				size := int64(len(body))
				v.SnapshotKey, v.SnapshotSize, res.SnapshotKey = &key, &size, key
			}
		}
		return store.InsertDatasetVersion(ctx, tx, s.OperatorTenant, v)
	})
	if err != nil {
		return BuildResult{}, err
	}
	s.log().InfoContext(ctx, "feed: dựng tập dữ liệu", "event", "integration.dataset_built", "dataset", c.Name, "version", res.Version,
		"changed", res.Changed, "upserts", res.Upserts, "retracts", res.Retracts, "records", res.RecordCount, "suppressed", res.Suppressed, "outcome", "ok")
	return res, nil
}

// SnapshotKey là khóa đối tượng của ảnh chụp: mã lần dựng (UUIDv7) trong khóa để lần dựng lỗi giữa chừng không
// chặn lần dựng lại cùng số phiên bản (kho đối tượng ghi một lần).
func SnapshotKey(op uuid.UUID, c *integration.Contract, version int64, build uuid.UUID) string {
	return fmt.Sprintf("integration/%s/snapshots/%s/v%d/%d-%s.ndjson", op, c.Name, c.Version, version, build)
}

// ---------------------------------------------------------------------------
// Đọc (cổng tích hợp)
// ---------------------------------------------------------------------------

// FeedPage là một trang đọc từ kho feed.
type FeedPage struct {
	Entries []store.FeedEntry
	Next    int64
	HasMore bool
	AsOf    time.Time
	// Dataset: phiên bản của mục cuối trang (feed theo phiên bản), không có mục → phiên bản mới nhất.
	Dataset *store.DatasetVersion
	// Expired: cursor < purged_through — Latest là phiên bản mới nhất để đồng bộ lại từ ảnh chụp.
	Expired bool
	Latest  *store.DatasetVersion
}

// ReadFeed đọc tối đa limit mục sau cursor after (nil = từ đầu phần còn giữ) của phiên bản hợp đồng c.
func (s *Service) ReadFeed(ctx context.Context, c *integration.Contract, partition string, after *int64, limit int) (FeedPage, error) {
	var p FeedPage
	versioned := c.Delivery != nil && c.Delivery.DatasetVersioned
	err := s.operatorTx(ctx, func(tx pgx.Tx) error {
		head, err := store.GetFeedHead(ctx, tx, c.Name, partition)
		if err != nil {
			return err
		}
		if head == nil {
			head = &store.FeedHead{}
		}
		if after != nil && *after > head.LastSeq {
			return port.ErrInvalidCursor.WithDetail("cursor vượt đầu feed")
		}
		if versioned {
			if p.Latest, err = store.LatestDatasetVersion(ctx, tx, c.Name); err != nil {
				return err
			}
		}
		if after != nil && *after < head.PurgedThrough {
			p.Expired = true
			return nil
		}
		start := head.PurgedThrough
		if after != nil {
			start = *after
		}
		es, err := store.FeedEntriesAfter(ctx, tx, c.Name, partition, c.Version, start, limit+1)
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
		p.Dataset = p.Latest
		if n := len(es); n > 0 && es[n-1].DatasetVersion != nil && (p.Latest == nil || *es[n-1].DatasetVersion != p.Latest.Version) {
			p.Dataset, err = store.DatasetVersionByNo(ctx, tx, c.Name, *es[n-1].DatasetVersion)
		}
		return err
	})
	return p, err
}

// SnapshotLink là URL ký có hạn của ảnh chụp một phiên bản — CHỈ trả trong phản hồi, không lưu, không ghi log.
type SnapshotLink struct {
	URL       string
	ExpiresAt time.Time
	Version   store.DatasetVersion
}

// SnapshotURL cấp URL ký (port.SnapshotURLTTL) cho ảnh chụp phiên bản version của dataset.
func (s *Service) SnapshotURL(ctx context.Context, dataset string, version int64) (SnapshotLink, error) {
	var v *store.DatasetVersion
	if err := s.operatorTx(ctx, func(tx pgx.Tx) error {
		var err error
		v, err = store.DatasetVersionByNo(ctx, tx, dataset, version)
		return err
	}); err != nil {
		return SnapshotLink{}, err
	}
	if v == nil || v.SnapshotKey == nil {
		return SnapshotLink{}, port.ErrSnapshotNotFound
	}
	if s.Store == nil {
		return SnapshotLink{}, ErrDependencyUnavailable.WithDetail("chưa có kho đối tượng")
	}
	ttl := min(port.SnapshotURLTTL, objstore.MaxPresignTTL)
	now := s.Clock.Now()
	u, err := s.Store.PresignGet(ctx, *v.SnapshotKey, ttl)
	if err != nil {
		if errors.Is(err, objstore.ErrNotFound) {
			return SnapshotLink{}, port.ErrSnapshotNotFound
		}
		return SnapshotLink{}, ErrDependencyUnavailable.WithCause(err)
	}
	return SnapshotLink{URL: u, ExpiresAt: now.Add(ttl), Version: *v}, nil
}

// PurgeFeeds dọn mục feed quá hạn giữ 30 ngày (job port.JobFeedRetention) theo lô tới khi hết. Không chạm
// dataset_version / ảnh chụp (bất biến, đối soát R-INT-10).
func (s *Service) PurgeFeeds(ctx context.Context, batch int) (int64, error) {
	if batch <= 0 {
		batch = 5000
	}
	var total int64
	for {
		var n int64
		if err := s.operatorTx(ctx, func(tx pgx.Tx) error {
			var err error
			n, err = store.PurgeExpiredFeedEntries(ctx, tx, batch)
			return err
		}); err != nil {
			return total, err
		}
		total += n
		if n < int64(batch) {
			return total, nil
		}
	}
}
