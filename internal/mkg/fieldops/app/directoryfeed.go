package app

// Bộ điều hợp FEED của danh bạ HTX/thửa (W5.5 — ADR-0033 §2, §5; kiến trúc v4.1 §41.4; R-INT-02).
//
//   - Job fieldops_directory_feed (SyncDirectoryFeeds) kéo cooperative_directory, plot_directory qua cổng tích hợp
//     (port.DirectoryFeed), áp từng trang trong MỘT giao dịch cùng cursor (mkg 000036): cursor chỉ tiến khi trang đã
//     áp → dừng giữa chừng thì lượt sau đọc tiếp đúng chỗ, áp lại là no-op (seq). Đọc tới đầu feed (has_more =
//     false) → caught_up + as_of + max_age của trang.
//   - 403 party.share_grant_missing (thu hồi / hết hạn) → xóa bản ghi của feed, ghi revoked_at: mục coi là "không
//     có" (lập lệnh gom bị từ chối như bề mặt cũ trả 403). Cấp lại → đọc lại từ đầu.
//   - 410 cursor quá hạn → xóa bản ghi, đọc lại từ đầu (feed lớp partner phát lại mọi bản ghi còn hiệu lực ≤ 7 ngày,
//     nên phần còn giữ là trạng thái đầy đủ — port.PartnerFeedRefresh phía nền tảng).
//   - Cờ Service.DirectorySource (AG_FIELDOPS_DIRECTORY_SOURCE) chọn nguồn đối chiếu của lập/sửa lệnh gom:
//     "partner" (mặc định — bản sao cũ mkg 000033 + tra /v1/partner/*) hoặc "feed" (bảng mkg 000036; không bao giờ
//     gọi nền tảng trong thao tác người dùng). Ở chế độ feed: feed chưa đọc tới đầu → chưa đối chiếu
//     (fieldops.directory_unverified, không chặn); đã đọc tới đầu, mục vắng → "không có" (422 như bề mặt cũ);
//     as_of quá max_age của feed → fieldops.directory_stale.
//   - Chạy song song một kỳ: sau mỗi lượt kéo, CompareDirectorySources đối soát bản sao cũ với feed và trả khác biệt
//     để job ghi log (fieldops.directory_source_diff) — điều kiện bật "feed" và gỡ /v1/partner/* (W5.10).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Giới hạn một lượt kéo.
const (
	dirFeedPageLimit = 500
	dirFeedMaxPages  = 200 // ≤ 100 000 mục mỗi feed mỗi lượt; còn lại lượt sau đọc tiếp từ cursor
	maxDiffs         = 50  // khác biệt chi tiết tối đa trả cho job ghi log
)

// feedRepo đọc / ghi trạng thái và bản ghi feed (mặc định: bảng mkg 000036; kiểm thử đơn vị thay bản giả).
type feedRepo interface {
	states(ctx context.Context, tid uuid.UUID) (map[string]store.FeedState, error)
	// apply áp các mục (ref đã kiểm) và ghi trạng thái st trong MỘT giao dịch.
	apply(ctx context.Context, tid uuid.UUID, feed string, items []feedItem, st store.FeedState) error
	// reset xóa mọi bản ghi của feed và ghi trạng thái st trong một giao dịch; trả số dòng đã xóa.
	reset(ctx context.Context, tid uuid.UUID, feed string, st store.FeedState) (int64, error)
	// records đọc bản ghi của refs (nil = mọi bản ghi) của feed.
	records(ctx context.Context, tid uuid.UUID, feed string, refs []uuid.UUID) (map[uuid.UUID]json.RawMessage, error)
	// legacy đọc mọi dòng bản sao cũ (mkg 000033) cho đối soát.
	legacy(ctx context.Context, tid uuid.UUID) ([]store.ReplicaHTX, []store.ReplicaPlot, error)
}

// feedItem là mục feed đã kiểm: ref = entity_key (UUID).
type feedItem struct {
	port.DirectoryFeedItem
	ref uuid.UUID
}

type dbFeedRepo struct{ db tenant.Beginner }

func (r dbFeedRepo) states(ctx context.Context, tid uuid.UUID) (map[string]store.FeedState, error) {
	var out map[string]store.FeedState
	err := tenant.WithTenantTx(ctx, r.db, tid, func(tx pgx.Tx) error {
		var e error
		out, e = store.FeedStates(ctx, tx)
		return e
	})
	return out, err
}

func (r dbFeedRepo) apply(ctx context.Context, tid uuid.UUID, feed string, items []feedItem, st store.FeedState) error {
	return tenant.WithTenantTx(ctx, r.db, tid, func(tx pgx.Tx) error {
		for _, it := range items {
			var err error
			if it.Op == "retract" {
				err = store.ClearFeedRecord(ctx, tx, feed, it.ref, it.Seq)
			} else {
				err = store.PutFeedRecord(ctx, tx, tid, feed, it.ref, it.Seq, it.EventID, it.Record, it.RecordedAt)
			}
			if err != nil {
				return err
			}
		}
		return store.PutFeedState(ctx, tx, tid, st)
	})
}

func (r dbFeedRepo) reset(ctx context.Context, tid uuid.UUID, feed string, st store.FeedState) (int64, error) {
	var n int64
	err := tenant.WithTenantTx(ctx, r.db, tid, func(tx pgx.Tx) error {
		var e error
		if n, e = store.ClearFeedRecords(ctx, tx, feed); e != nil {
			return e
		}
		return store.PutFeedState(ctx, tx, tid, st)
	})
	return n, err
}

func (r dbFeedRepo) records(ctx context.Context, tid uuid.UUID, feed string, refs []uuid.UUID) (map[uuid.UUID]json.RawMessage, error) {
	var out map[uuid.UUID]json.RawMessage
	err := tenant.WithTenantTx(ctx, r.db, tid, func(tx pgx.Tx) error {
		var e error
		out, e = store.FeedRecords(ctx, tx, feed, refs)
		return e
	})
	return out, err
}

func (r dbFeedRepo) legacy(ctx context.Context, tid uuid.UUID) (h []store.ReplicaHTX, p []store.ReplicaPlot, err error) {
	err = tenant.WithTenantTx(ctx, r.db, tid, func(tx pgx.Tx) error {
		var e error
		if h, e = store.AllReplicaHTX(ctx, tx); e != nil {
			return e
		}
		p, e = store.AllReplicaPlots(ctx, tx)
		return e
	})
	return h, p, err
}

func (s *Service) feedRepo() feedRepo {
	if s.feeds != nil {
		return s.feeds
	}
	return dbFeedRepo{db: s.DB}
}

// feedSource cho biết lập/sửa lệnh gom đối chiếu với bản sao dựng từ feed.
func (s *Service) feedSource() bool { return s.DirectorySource == port.DirectorySourceFeed }

// directoryConfigured: nguồn đã chọn có cấu hình (thiếu → lập/sửa lệnh gom đóng 503).
func (s *Service) directoryConfigured() bool {
	if s.feedSource() {
		return s.DirectoryFeed != nil
	}
	return !s.DirectoryUnconfigured && s.Directory != nil
}

// Bản ghi hợp đồng (chỉ trường đối chiếu dùng).
type coopFeedRecord struct {
	Ref         uuid.UUID `json:"cooperative_ref"`
	CommuneCode *string   `json:"commune_code"`
	Active      bool      `json:"active"`
}

type plotFeedRecord struct {
	Ref            uuid.UUID  `json:"plot_ref"`
	CooperativeRef *uuid.UUID `json:"cooperative_ref"`
	CommuneCode    string     `json:"commune_code"`
	Status         string     `json:"status"`
}

// recordRef giải mã ref của bản ghi feed (để kiểm khớp entity_key).
func recordRef(feed string, rec json.RawMessage) (uuid.UUID, error) {
	switch feed {
	case port.FeedCooperativeDirectory:
		var r coopFeedRecord
		err := json.Unmarshal(rec, &r)
		return r.Ref, err
	case port.FeedPlotDirectory:
		var r plotFeedRecord
		err := json.Unmarshal(rec, &r)
		return r.Ref, err
	}
	return uuid.Nil, fmt.Errorf("feed %q lạ", feed)
}

// checkItems kiểm mục của trang: entity_key là UUID, upsert có bản ghi khớp entity_key. Lệch → lỗi, không áp trang.
func checkItems(feed string, items []port.DirectoryFeedItem) ([]feedItem, error) {
	out := make([]feedItem, 0, len(items))
	for _, it := range items {
		ref, err := uuid.Parse(it.EntityKey)
		if err != nil {
			return nil, fmt.Errorf("%s seq %d: entity_key không phải UUID", feed, it.Seq)
		}
		if it.Op == "upsert" {
			rr, err := recordRef(feed, it.Record)
			if err != nil || rr != ref {
				return nil, fmt.Errorf("%s seq %d: bản ghi không khớp entity_key", feed, it.Seq)
			}
		}
		out = append(out, feedItem{DirectoryFeedItem: it, ref: ref})
	}
	return out, nil
}

// DirectoryFeedSync là kết quả kéo một feed danh bạ.
type DirectoryFeedSync struct {
	Feed     string     `json:"feed"`
	Pages    int        `json:"pages"`
	Upserts  int        `json:"upserts"`
	Retracts int        `json:"retracts"`
	CaughtUp bool       `json:"caught_up"`
	AsOf     *time.Time `json:"as_of,omitempty"`
	MaxAge   string     `json:"max_age,omitempty"`
	Stale    bool       `json:"stale"`
	Revoked  bool       `json:"revoked"`
	Resynced bool       `json:"resynced"`
	Cleared  int64      `json:"cleared"`
}

// ErrDirectoryFeedUnconfigured: chưa cấu hình cổng tích hợp / khóa API (job không chạy).
var ErrDirectoryFeedUnconfigured = errors.New("fieldops: chưa cấu hình feed danh bạ của cổng tích hợp")

// SyncDirectoryFeeds kéo mọi feed danh bạ cho tenant MKG tid (job fieldops_directory_feed). Lỗi một feed không chặn
// feed kia (trả lỗi gộp).
func (s *Service) SyncDirectoryFeeds(ctx context.Context, tid uuid.UUID) ([]DirectoryFeedSync, error) {
	if s.DirectoryFeed == nil {
		return nil, ErrDirectoryFeedUnconfigured
	}
	repo := s.feedRepo()
	sts, err := repo.states(ctx, tid)
	if err != nil {
		return nil, err
	}
	var out []DirectoryFeedSync
	var errs []error
	for _, f := range port.DirectoryFeeds {
		st, ok := sts[f]
		if !ok {
			st = store.FeedState{Feed: f}
		}
		r, err := s.syncFeed(ctx, repo, tid, st)
		out = append(out, r)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", f, err))
		}
	}
	return out, errors.Join(errs...)
}

func (s *Service) syncFeed(ctx context.Context, repo feedRepo, tid uuid.UUID, st store.FeedState) (r DirectoryFeedSync, _ error) {
	r.Feed = st.Feed
	now := s.Clock.Now()
	resynced := false
	defer func() {
		r.CaughtUp, r.AsOf = st.CaughtUp, st.AsOf
		if st.MaxAge > 0 {
			r.MaxAge = st.MaxAge.String()
		}
		r.Stale = feedStale(st, now, s.dirMaxAge())
	}()
	for r.Pages < dirFeedMaxPages {
		pg, err := s.DirectoryFeed.Page(ctx, st.Feed, st.Cursor, dirFeedPageLimit)
		switch {
		case errors.Is(err, port.ErrFeedGrantMissing):
			// Thu hồi / hết hạn: không giữ dữ liệu bên cấp đã rút quyền; mục coi là "không có".
			t := now
			st = store.FeedState{Feed: st.Feed, ContractVersion: st.ContractVersion, MaxAge: st.MaxAge, RevokedAt: &t, LastPulledAt: &t}
			n, rerr := repo.reset(ctx, tid, st.Feed, st)
			r.Revoked, r.Cleared = true, n
			return r, rerr
		case errors.Is(err, port.ErrFeedCursorExpired) && !resynced:
			// Cursor quá hạn giữ: đọc lại từ đầu phần còn giữ (trạng thái đầy đủ — phát lại ≤ 7 ngày).
			resynced, r.Resynced = true, true
			t := now
			st = store.FeedState{Feed: st.Feed, ContractVersion: st.ContractVersion, MaxAge: st.MaxAge, LastPulledAt: &t}
			n, rerr := repo.reset(ctx, tid, st.Feed, st)
			r.Cleared += n
			if rerr != nil {
				return r, rerr
			}
			continue
		case err != nil:
			t := now
			st.LastPulledAt, st.LastError = &t, err.Error()
			if aerr := repo.apply(ctx, tid, st.Feed, nil, st); aerr != nil {
				return r, errors.Join(err, aerr)
			}
			return r, err
		}
		if pg.Feed != st.Feed {
			return r, fmt.Errorf("%w: trang của feed %q", port.ErrDirectoryUnavailable, pg.Feed)
		}
		items, err := checkItems(st.Feed, pg.Items)
		if err != nil {
			return r, fmt.Errorf("%w: %w", port.ErrDirectoryUnavailable, err)
		}
		t := now
		next := st
		next.Cursor, next.ContractVersion, next.RevokedAt, next.LastPulledAt, next.LastError = pg.NextCursor, pg.ContractVersion, nil, &t, ""
		if pg.MaxAge > 0 {
			next.MaxAge = pg.MaxAge
		}
		if !pg.HasMore {
			a := pg.AsOf
			next.CaughtUp, next.AsOf = true, &a
		}
		if err := repo.apply(ctx, tid, st.Feed, items, next); err != nil {
			return r, err
		}
		st = next
		r.Pages++
		for _, it := range items {
			if it.Op == "retract" {
				r.Retracts++
			} else {
				r.Upserts++
			}
		}
		if !pg.HasMore {
			break
		}
	}
	return r, nil
}

// feedMaxAge là max_age áp cho một feed: max_age của hợp đồng (trang feed) nếu đã biết, không thì cấu hình.
func feedMaxAge(st store.FeedState, def time.Duration) time.Duration {
	if st.MaxAge > 0 {
		return st.MaxAge
	}
	return def
}

// feedStale: feed chưa đọc tới đầu, hoặc as_of quá max_age.
func feedStale(st store.FeedState, now time.Time, def time.Duration) bool {
	return st.RevokedAt == nil && (!st.CaughtUp || st.AsOf == nil || now.Sub(*st.AsOf) > feedMaxAge(st, def))
}

// feedSnapshot dựng ảnh chụp danh mục cho một kế hoạch từ bản sao feed; trả kèm max_age áp dụng (nhỏ nhất của hai
// feed). Feed bị thu hồi → mục "không có"; feed đã đọc tới đầu → mục vắng là "không có" (feed lớp partner phát lại
// mọi bản ghi còn hiệu lực nên phần còn giữ là trạng thái đầy đủ — cùng kết quả bề mặt cũ trả 404); feed chưa đọc
// tới đầu → chưa có (unverified, không chặn).
func (s *Service) feedSnapshot(ctx context.Context, tid, htx uuid.UUID, plots []uuid.UUID) (dirSnapshot, time.Duration, error) {
	snap := dirSnapshot{plots: map[uuid.UUID]store.ReplicaPlot{}}
	repo := s.feedRepo()
	sts, err := repo.states(ctx, tid)
	if err != nil {
		return snap, 0, err
	}
	def := s.dirMaxAge()
	cs, ps := sts[port.FeedCooperativeDirectory], sts[port.FeedPlotDirectory]
	maxAge := min(feedMaxAge(cs, def), feedMaxAge(ps, def))
	switch {
	case cs.RevokedAt != nil:
		snap.htx = &store.ReplicaHTX{Ref: htx, AsOf: *cs.RevokedAt}
	case cs.CaughtUp && cs.AsOf != nil:
		recs, err := repo.records(ctx, tid, port.FeedCooperativeDirectory, []uuid.UUID{htx})
		if err != nil {
			return snap, 0, err
		}
		h := store.ReplicaHTX{Ref: htx, AsOf: *cs.AsOf} // feed đã đọc tới đầu là trạng thái đầy đủ: vắng = không có
		if raw, ok := recs[htx]; ok {
			var c coopFeedRecord
			if err := json.Unmarshal(raw, &c); err != nil {
				return snap, 0, err
			}
			h.Present, h.Active = true, c.Active
			if c.CommuneCode != nil {
				h.CommuneCode = *c.CommuneCode
			}
		}
		snap.htx = &h
	}
	switch {
	case ps.RevokedAt != nil:
		for _, p := range plots {
			snap.plots[p] = store.ReplicaPlot{Ref: p, AsOf: *ps.RevokedAt}
		}
	case ps.CaughtUp && ps.AsOf != nil && len(plots) > 0:
		recs, err := repo.records(ctx, tid, port.FeedPlotDirectory, plots)
		if err != nil {
			return snap, 0, err
		}
		for _, ref := range plots {
			raw, ok := recs[ref]
			if !ok {
				snap.plots[ref] = store.ReplicaPlot{Ref: ref, AsOf: *ps.AsOf} // vắng = không có
				continue
			}
			var p plotFeedRecord
			if err := json.Unmarshal(raw, &p); err != nil {
				return snap, 0, err
			}
			snap.plots[ref] = store.ReplicaPlot{Ref: ref, Present: true, CooperativeRef: p.CooperativeRef, CommuneCode: p.CommuneCode,
				Status: p.Status, AsOf: *ps.AsOf}
		}
	}
	return snap, maxAge, nil
}

// DirectoryDiff là một khác biệt giữa bản sao cũ (/v1/partner/*) và bản sao feed.
type DirectoryDiff struct {
	Feed  string    `json:"feed"`
	Ref   uuid.UUID `json:"ref"`
	Kind  string    `json:"kind"`  // feed_missing | legacy_absent | mismatch
	Field string    `json:"field"` // trường lệch (mismatch)
}

// DirectoryComparison là kết quả đối soát hai nguồn.
type DirectoryComparison struct {
	Skipped      bool            `json:"skipped"` // một feed chưa đọc tới đầu / bị thu hồi — chưa so được
	Compared     int             `json:"compared"`
	Matched      int             `json:"matched"`
	FeedMissing  int             `json:"feed_missing"`  // bản sao cũ có, feed không có
	LegacyAbsent int             `json:"legacy_absent"` // bản sao cũ ghi "không có", feed có
	Mismatch     int             `json:"mismatch"`
	Diffs        []DirectoryDiff `json:"diffs,omitempty"` // tối đa maxDiffs
}

func (c *DirectoryComparison) add(feed string, ref uuid.UUID, kind, field string) {
	switch kind {
	case "feed_missing":
		c.FeedMissing++
	case "legacy_absent":
		c.LegacyAbsent++
	default:
		c.Mismatch++
	}
	if len(c.Diffs) < maxDiffs {
		c.Diffs = append(c.Diffs, DirectoryDiff{Feed: feed, Ref: ref, Kind: kind, Field: field})
	}
}

// CompareDirectorySources đối soát mọi mục của bản sao cũ (mkg 000033 — các HTX/thửa lệnh gom đã tham chiếu) với bản
// sao feed: cùng có/không có, cùng mã xã + còn hoạt động (HTX), cùng HTX + mã xã + trạng thái (thửa).
func (s *Service) CompareDirectorySources(ctx context.Context, tid uuid.UUID) (DirectoryComparison, error) {
	var c DirectoryComparison
	repo := s.feedRepo()
	sts, err := repo.states(ctx, tid)
	if err != nil {
		return c, err
	}
	for _, f := range port.DirectoryFeeds {
		if st := sts[f]; !st.CaughtUp || st.RevokedAt != nil {
			c.Skipped = true
			return c, nil
		}
	}
	lh, lp, err := repo.legacy(ctx, tid)
	if err != nil {
		return c, err
	}
	coops, err := repo.records(ctx, tid, port.FeedCooperativeDirectory, nil)
	if err != nil {
		return c, err
	}
	plots, err := repo.records(ctx, tid, port.FeedPlotDirectory, nil)
	if err != nil {
		return c, err
	}
	for _, h := range lh {
		c.Compared++
		raw, ok := coops[h.Ref]
		switch {
		case h.Present && !ok:
			c.add(port.FeedCooperativeDirectory, h.Ref, "feed_missing", "")
			continue
		case !h.Present && ok:
			c.add(port.FeedCooperativeDirectory, h.Ref, "legacy_absent", "")
			continue
		case !h.Present:
			c.Matched++
			continue
		}
		var f coopFeedRecord
		if err := json.Unmarshal(raw, &f); err != nil {
			return c, err
		}
		commune := ""
		if f.CommuneCode != nil {
			commune = *f.CommuneCode
		}
		switch {
		case commune != h.CommuneCode:
			c.add(port.FeedCooperativeDirectory, h.Ref, "mismatch", "commune_code")
		case f.Active != h.Active:
			c.add(port.FeedCooperativeDirectory, h.Ref, "mismatch", "active")
		default:
			c.Matched++
		}
	}
	for _, p := range lp {
		c.Compared++
		raw, ok := plots[p.Ref]
		switch {
		case p.Present && !ok:
			c.add(port.FeedPlotDirectory, p.Ref, "feed_missing", "")
			continue
		case !p.Present && ok:
			c.add(port.FeedPlotDirectory, p.Ref, "legacy_absent", "")
			continue
		case !p.Present:
			c.Matched++
			continue
		}
		var f plotFeedRecord
		if err := json.Unmarshal(raw, &f); err != nil {
			return c, err
		}
		switch {
		case !sameRef(f.CooperativeRef, p.CooperativeRef):
			c.add(port.FeedPlotDirectory, p.Ref, "mismatch", "cooperative_ref")
		case f.CommuneCode != p.CommuneCode:
			c.add(port.FeedPlotDirectory, p.Ref, "mismatch", "commune_code")
		case f.Status != p.Status:
			c.add(port.FeedPlotDirectory, p.Ref, "mismatch", "status")
		default:
			c.Matched++
		}
	}
	slices.SortFunc(c.Diffs, func(a, b DirectoryDiff) int {
		if a.Feed != b.Feed {
			return compareStr(a.Feed, b.Feed)
		}
		return slices.Compare(a.Ref[:], b.Ref[:])
	})
	return c, nil
}

func sameRef(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func compareStr(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
