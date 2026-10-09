// Package app là ca sử dụng của kho quan sát (W8.2 — ADR-0036 §3, kiến trúc v4.1 §41.4):
//
//   - SyncObservationFeeds (job engine_observation_feed) kéo các feed của cổng tích hợp mà MKG được đọc QUA CÙNG bộ điều
//     hợp feed của W5.5 (mkg/fieldops port.DirectoryFeed — GET /v1/feeds/{feed}, khóa API) với cursor RIÊNG của engine
//     (observation_feed_state). Mỗi trang: kiểm hợp đồng (contracts/integration: ValidateRecord + entity_key — như mọi
//     đối tác, mặc định từ chối), ánh xạ từng mục thành quan sát (domain.Map), ghi sổ + cursor trong MỘT giao dịch. Áp
//     lại cùng mục là no-op (idem_key theo event_id). Đọc tới đầu feed → caught_up + as_of + max_age.
//   - Hợp đồng chưa approved (mọi feed hiện ở trạng thái proposed — chờ A7) chỉ kéo khi bật cờ dev/test
//     AllowProposed (AG_ENGINE_FEED_ALLOW_PROPOSED=1) và KHÔNG ở production — như cổng (AG_GATEWAY_FEED_ALLOW_PROPOSED).
//   - 403 party.share_grant_missing → ghi quan sát rút (grant_revoked) cho mọi entity còn sống của feed, đặt lại cursor;
//     410 cursor quá hạn → đọc lại từ đầu (mục đã áp bỏ qua theo idem_key).
//   - Latest (port.Observations): quan sát mới nhất mỗi entity, tách Fresh / Stale / Missing tại thời điểm đồng hồ —
//     quá max_age không bao giờ lẫn vào Fresh.
package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/integration"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/engine/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/engine/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/engine/store"
	foport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Giới hạn một lượt kéo (như W5.5).
const (
	feedPageLimit = 500
	feedMaxPages  = 200
)

// Service là kho quan sát.
type Service struct {
	DB    tenant.Beginner
	Clock clock.Clock
	// Feed: bộ điều hợp feed của cổng tích hợp (W5.5); nil → không kéo (ErrFeedUnconfigured).
	Feed foport.DirectoryFeed
	// Feeds: feed được kéo (nil = mọi feed có bộ ánh xạ — domain.Feeds()).
	Feeds []string
	// AllowProposed: kéo cả feed có hợp đồng chưa approved (cờ dev/test); bỏ qua khi Production.
	AllowProposed bool
	Production    bool
}

var _ port.Observations = (*Service)(nil)

// FeedSync là kết quả kéo một feed.
type FeedSync struct {
	Feed       string     `json:"feed"`
	Skipped    string     `json:"skipped,omitempty"` // lý do không kéo (contract_not_approved, no_mapper)
	Pages      int        `json:"pages"`
	Items      int        `json:"items"`
	Observed   int        `json:"observed"`   // quan sát mới đã ghi (kể cả rút)
	Retracted  int        `json:"retracted"`  // trong đó: quan sát rút
	Duplicates int        `json:"duplicates"` // quan sát đã có (áp lại) — bỏ qua
	CaughtUp   bool       `json:"caught_up"`
	AsOf       *time.Time `json:"as_of,omitempty"`
	MaxAge     string     `json:"max_age,omitempty"`
	Stale      bool       `json:"stale"`
	Revoked    bool       `json:"revoked"`
	Resynced   bool       `json:"resynced"`
}

func (s *Service) feeds() []string {
	if s.Feeds == nil {
		return domain.Feeds()
	}
	return s.Feeds
}

// pullable: hợp đồng v1 của feed được phép kéo ở môi trường này.
func (s *Service) pullable(feed string) (string, bool) {
	if domain.FeedKinds(feed) == nil {
		return "no_mapper", false
	}
	c, err := integration.Get(feed, 1)
	if err != nil {
		return "no_contract", false
	}
	if c.Status == integration.StatusApproved && c.Servable(s.Production) {
		return "", true
	}
	if s.AllowProposed && !s.Production && c.Servable(false) {
		return "", true
	}
	return "contract_not_approved", false
}

// SyncObservationFeeds kéo mọi feed đã cấu hình cho tenant MKG tid. Lỗi một feed không chặn feed khác (lỗi gộp).
func (s *Service) SyncObservationFeeds(ctx context.Context, tid uuid.UUID) ([]FeedSync, error) {
	if s.Feed == nil {
		return nil, port.ErrFeedUnconfigured
	}
	var sts map[string]store.FeedState
	if err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		var e error
		sts, e = store.FeedStates(ctx, tx)
		return e
	}); err != nil {
		return nil, err
	}
	var out []FeedSync
	var errs []error
	for _, f := range s.feeds() {
		if why, ok := s.pullable(f); !ok {
			out = append(out, FeedSync{Feed: f, Skipped: why})
			continue
		}
		st, ok := sts[f]
		if !ok {
			st = store.FeedState{Feed: f}
		}
		r, err := s.syncFeed(ctx, tid, st)
		out = append(out, r)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", f, err))
		}
	}
	return out, errors.Join(errs...)
}

func (s *Service) syncFeed(ctx context.Context, tid uuid.UUID, st store.FeedState) (r FeedSync, _ error) {
	r.Feed = st.Feed
	now := s.Clock.Now()
	defer func() {
		r.CaughtUp, r.AsOf = st.CaughtUp, st.AsOf
		if st.MaxAge > 0 {
			r.MaxAge = st.MaxAge.String()
		}
		r.Stale = st.RevokedAt == nil && (!st.CaughtUp || st.AsOf == nil || domain.Stale(*st.AsOf, st.MaxAge, now))
	}()
	resynced := false
	for r.Pages < feedMaxPages {
		pg, err := s.Feed.Page(ctx, st.Feed, st.Cursor, feedPageLimit)
		switch {
		case errors.Is(err, foport.ErrFeedGrantMissing):
			n, next, rerr := s.revoke(ctx, tid, st, now)
			r.Revoked, r.Observed, r.Retracted = true, n, n
			if rerr == nil {
				st = next
			}
			return r, rerr
		case errors.Is(err, foport.ErrFeedCursorExpired) && !resynced:
			// Cursor quá hạn giữ: đọc lại từ đầu phần còn giữ; mục đã áp bỏ qua theo idem_key.
			resynced, r.Resynced = true, true
			st.Cursor, st.CaughtUp = "", false
			continue
		case err != nil:
			t := now
			st.LastPulledAt, st.LastError = &t, err.Error()
			if perr := s.putState(ctx, tid, st); perr != nil {
				return r, errors.Join(err, perr)
			}
			return r, err
		}
		rows, n, err := s.pageRows(st, pg)
		if err != nil {
			return r, err
		}
		t := now
		next := st
		next.Cursor, next.ContractVersion, next.RevokedAt, next.LastPulledAt, next.LastError = pg.NextCursor, pg.ContractVersion, nil, &t, ""
		if ma := s.maxAge(st.Feed, pg); ma > 0 {
			next.MaxAge = ma
		}
		if !pg.HasMore {
			a := pg.AsOf.UTC()
			next.CaughtUp, next.AsOf = true, &a
		}
		ins, dup, err := s.apply(ctx, tid, rows, next)
		if err != nil {
			return r, err
		}
		st = next
		r.Pages++
		r.Items += n
		r.Duplicates += dup
		for _, row := range ins {
			r.Observed++
			if row.Retracted {
				r.Retracted++
			}
		}
		if !pg.HasMore {
			break
		}
	}
	return r, nil
}

// maxAge: max_age của trang; trang không khai → freshness.max_age của hợp đồng.
func (s *Service) maxAge(feed string, pg foport.DirectoryFeedPage) time.Duration {
	if pg.MaxAge > 0 {
		return pg.MaxAge
	}
	if c, err := integration.Get(feed, pg.ContractVersion); err == nil && c.Freshness != nil {
		if d, err := domain.ParseMaxAge(c.Freshness.MaxAge); err == nil {
			return d
		}
	}
	return 0
}

// ErrBadPage: trang feed lệch hợp đồng — không áp, cursor giữ nguyên (lượt sau thử lại; log cảnh báo).
var ErrBadPage = errors.New("engine: trang feed không hợp lệ")

// pageRows kiểm trang (hợp đồng, entity_key) và dựng quan sát. Trả số mục của trang.
func (s *Service) pageRows(st store.FeedState, pg foport.DirectoryFeedPage) ([]store.Row, int, error) {
	if pg.Feed != st.Feed {
		return nil, 0, fmt.Errorf("%w: trang của feed %q", ErrBadPage, pg.Feed)
	}
	c, err := integration.Get(st.Feed, pg.ContractVersion)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrBadPage, err)
	}
	maxAge := s.maxAge(st.Feed, pg)
	if maxAge <= 0 {
		maxAge = st.MaxAge
	}
	if maxAge <= 0 {
		return nil, 0, fmt.Errorf("%w: không có max_age", ErrBadPage)
	}
	var rows []store.Row
	for _, it := range pg.Items {
		if len(it.EntityKey) > 200 {
			return nil, 0, fmt.Errorf("%w: seq %d: entity_key quá dài", ErrBadPage, it.Seq)
		}
		if it.Op == "upsert" {
			if err := c.ValidateRecord(it.Record); err != nil {
				return nil, 0, fmt.Errorf("%w: seq %d: %w", ErrBadPage, it.Seq, err)
			}
			if ek, err := c.EntityKeyOf(it.Record); err != nil || ek != it.EntityKey {
				return nil, 0, fmt.Errorf("%w: seq %d: bản ghi không khớp entity_key", ErrBadPage, it.Seq)
			}
		}
		drafts, err := domain.Map(st.Feed, it.Op, it.Record)
		if err != nil {
			return nil, 0, fmt.Errorf("%w: seq %d: %w", ErrBadPage, it.Seq, err)
		}
		for _, d := range drafts {
			row := draftRow(d, st.Feed, it.EntityKey, pg.AsOf, maxAge)
			row.IdemKey = strings.Join([]string{port.SourceAPI, st.Feed, d.Kind, it.EntityKey, it.EventID.String()}, "|")
			row.ContractVersion, row.FeedSeq, row.EventID, row.FeedCursor = pg.ContractVersion, it.Seq, it.EventID, st.Cursor
			if !d.Retracted {
				row.Payload = it.Record
			}
			rows = append(rows, row)
		}
	}
	return rows, len(pg.Items), nil
}

func draftRow(d domain.Draft, feed, entityKey string, asOf time.Time, maxAge time.Duration) store.Row {
	row := store.Row{ID: ids.New(), StreamKey: port.SourceAPI + ":" + feed, Kind: d.Kind, EntityKey: entityKey, Source: port.SourceAPI,
		Feed: feed, AsOf: asOf, MaxAge: maxAge, Retracted: d.Retracted, Basis: d.Basis, MapperVersion: domain.MapperVersion}
	if d.Value != nil {
		v, u := d.Value.Value, d.Value.U
		row.Value, row.Unit, row.Uncertainty = &v, d.Unit, &u
	}
	return row
}

// apply ghi các quan sát chưa có (theo idem_key) và trạng thái feed trong MỘT giao dịch.
func (s *Service) apply(ctx context.Context, tid uuid.UUID, rows []store.Row, st store.FeedState) (ins []store.Row, dup int, _ error) {
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		ins, dup = nil, 0
		keys := make([]string, 0, len(rows))
		for _, r := range rows {
			keys = append(keys, r.IdemKey)
		}
		have, err := store.ExistingIdem(ctx, tx, keys)
		if err != nil {
			return err
		}
		for _, r := range rows {
			if have[r.IdemKey] {
				dup++
				continue
			}
			have[r.IdemKey] = true // trùng trong cùng trang (mục lặp) cũng chỉ ghi một lần
			if err := store.Insert(ctx, tx, tid, r); err != nil {
				return err
			}
			ins = append(ins, r)
		}
		return store.PutFeedState(ctx, tx, tid, st)
	})
	return ins, dup, err
}

// revoke: bên cấp thu hồi / hết hạn cấp quyền — quan sát rút (grant_revoked) cho mọi entity còn sống của feed, đặt
// lại cursor (cấp lại → đọc lại từ đầu). Sổ chỉ ghi thêm: lịch sử giữ lại, quan sát mới nhất là rút.
func (s *Service) revoke(ctx context.Context, tid uuid.UUID, st store.FeedState, now time.Time) (int, store.FeedState, error) {
	t := now
	next := store.FeedState{Feed: st.Feed, ContractVersion: st.ContractVersion, MaxAge: st.MaxAge, RevokedAt: &t, LastPulledAt: &t}
	maxAge := st.MaxAge
	if maxAge <= 0 {
		maxAge = s.maxAge(st.Feed, foport.DirectoryFeedPage{ContractVersion: max(st.ContractVersion, 1)})
	}
	if maxAge <= 0 {
		maxAge = time.Hour
	}
	cv := max(st.ContractVersion, 1)
	n := 0
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		n = 0
		live, err := store.LiveFromFeed(ctx, tx, st.Feed)
		if err != nil {
			return err
		}
		for _, e := range live {
			row := draftRow(domain.Draft{Kind: e.Kind, Retracted: true, Basis: []string{domain.BasisGrantRevoked}}, st.Feed, e.EntityKey, now, maxAge)
			row.ContractVersion = cv
			row.IdemKey = strings.Join([]string{port.SourceAPI, st.Feed, e.Kind, e.EntityKey, "revoked", now.UTC().Format(time.RFC3339Nano)}, "|")
			if err := store.Insert(ctx, tx, tid, row); err != nil {
				return err
			}
			n++
		}
		return store.PutFeedState(ctx, tx, tid, next)
	})
	return n, next, err
}

func (s *Service) putState(ctx context.Context, tid uuid.UUID, st store.FeedState) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error { return store.PutFeedState(ctx, tx, tid, st) })
}

// Latest cài đặt port.Observations.
func (s *Service) Latest(ctx context.Context, tid uuid.UUID, q port.Query) (port.Snapshot, error) {
	if !slices.Contains(domain.Kinds(), q.Kind) {
		return port.Snapshot{}, fmt.Errorf("%w: %q", port.ErrUnknownKind, q.Kind)
	}
	var obs []port.Observation
	var feedAsOf []*time.Time
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		var e error
		obs, feedAsOf, e = store.Latest(ctx, tx, q.Kind, q.EntityKeys)
		return e
	})
	if err != nil {
		return port.Snapshot{}, err
	}
	return Classify(q, obs, feedAsOf, s.Clock.Now()), nil
}

// Classify tách quan sát mới nhất thành Fresh / Stale / Missing tại now (thuần — kiểm thử đơn vị).
func Classify(q port.Query, obs []port.Observation, feedAsOf []*time.Time, now time.Time) port.Snapshot {
	snap := port.Snapshot{Kind: q.Kind, At: now}
	seen := map[string]bool{}
	for i, o := range obs {
		seen[o.EntityKey] = true
		var fa *time.Time
		if i < len(feedAsOf) {
			fa = feedAsOf[i]
		}
		o.EffectiveAsOf = domain.EffectiveAsOf(o.AsOf, fa)
		switch {
		case o.Retracted:
			snap.Missing = append(snap.Missing, o.EntityKey)
		case domain.Stale(o.EffectiveAsOf, o.MaxAge, now):
			snap.Stale = append(snap.Stale, o)
		default:
			snap.Fresh = append(snap.Fresh, o)
		}
	}
	for _, k := range q.EntityKeys {
		if !seen[k] {
			seen[k] = true
			snap.Missing = append(snap.Missing, k)
		}
	}
	slices.Sort(snap.Missing)
	return snap
}
