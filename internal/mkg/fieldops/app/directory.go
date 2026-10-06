package app

// Bản sao danh mục HTX/thửa của nền tảng trong cụm mkg (kiến trúc v4.1 §39 rủi ro 1, §41.4, §43; R-INT-02:
// không gọi đồng bộ nền tảng trong đường găng; kế hoạch W5.5 — as_of / max_age).
//
// Thiết kế:
//   - Lập / sửa lệnh gom đối chiếu HTX và thửa với BẢN SAO (mkg_fieldops.directory_*_replica, mkg 000033),
//     không gọi nền tảng cho mục đã có: nền tảng chậm / ngừng thì MKG vẫn lập được lệnh gom.
//   - Mục có as_of quá max_age (DirectoryMaxAge, mặc định 6 giờ): vẫn đối chiếu theo bản sao; kết quả mang
//     cảnh báo fieldops.directory_stale và directory_as_of (kế hoạch mang cờ dữ liệu cũ — §41.4), không chặn.
//   - Mục CHƯA có trong bản sao (HTX / thửa vừa được chia sẻ, job chưa chạy) hoặc bản sao ghi "không có":
//     tra trực tiếp MỘT lần qua PlatformDirectory, song song tối đa dirParallel, tổng thời gian chờ ≤
//     DirectoryLookupBudget (mặc định 2 giây); kết quả ghi vào bản sao. Đây là ngoại lệ có chặn trên của
//     R-INT-02, chỉ vì bộ điều hợp hiện tại tra theo mã (không liệt kê được danh mục) — W5.5 thay bằng feed
//     theo con trỏ thì bỏ. DirectoryLookupBudget < 0 → tắt hẳn (chỉ bản sao).
//   - Tra không được (nền tảng ngừng / hết ngân sách): mục chưa có → vẫn lập, gắn cờ
//     fieldops.directory_unverified (HTX chưa đối chiếu thì phải ghi commune_code); mục bản sao ghi
//     "không có" → từ chối như cũ (bằng chứng gần nhất là không có).
//   - Bằng chứng khẳng định (không có, thửa thuộc HTX khác / đã ngừng dùng, HTX vô hiệu) → 422 như cũ.
//   - Chưa cấu hình nguồn thửa/HTX (DirectoryUnconfigured) → đóng: 503 fieldops.directory_unavailable.
//   - Job fieldops_directory (module.DirectoryJobs) làm mới mọi mục của bản sao và mục lệnh gom đang mở
//     tham chiếu; tra không được thì giữ dòng cũ (as_of không đổi → cũ dần, bị gắn cờ).

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Cảnh báo (không chặn) của đối chiếu danh mục — mã ổn định cho client.
const (
	WarnDirectoryStale      = "fieldops.directory_stale"      // bản sao HTX/thửa cũ hơn max_age: đối chiếu theo dữ liệu cũ
	WarnDirectoryUnverified = "fieldops.directory_unverified" // HTX/thửa chưa có trong bản sao, không tra được nền tảng
)

// Mặc định của bản sao danh mục.
const (
	DefaultDirectoryMaxAge       = 6 * time.Hour
	DefaultDirectoryLookupBudget = 2 * time.Second
	dirParallel                  = 8   // lời gọi nền tảng song song tối đa (tra dự phòng, job)
	dirSaveChunk                 = 200 // số dòng mỗi giao dịch ghi bản sao của job
)

// dirSnapshot là ảnh chụp danh mục cho một kế hoạch: HTX (nil = chưa có) và các thửa (vắng = chưa có).
type dirSnapshot struct {
	htx   *store.ReplicaHTX
	plots map[uuid.UUID]store.ReplicaPlot
}

// directoryReplica đọc / ghi bản sao danh mục (mặc định: bảng của cụm mkg; kiểm thử đơn vị thay bản giả).
type directoryReplica interface {
	load(ctx context.Context, tid, htx uuid.UUID, plots []uuid.UUID) (dirSnapshot, error)
	save(ctx context.Context, tid uuid.UUID, htx []store.ReplicaHTX, plots []store.ReplicaPlot) error
	refs(ctx context.Context, tid uuid.UUID) (htx, plots []uuid.UUID, err error)
}

type dbReplica struct{ db tenant.Beginner }

func (r dbReplica) load(ctx context.Context, tid, htx uuid.UUID, plots []uuid.UUID) (dirSnapshot, error) {
	var snap dirSnapshot
	err := tenant.WithTenantTx(ctx, r.db, tid, func(tx pgx.Tx) error {
		var e error
		if snap.htx, e = store.GetReplicaHTX(ctx, tx, htx); e != nil {
			return e
		}
		snap.plots, e = store.ReplicaPlots(ctx, tx, plots)
		return e
	})
	return snap, err
}

func (r dbReplica) save(ctx context.Context, tid uuid.UUID, htx []store.ReplicaHTX, plots []store.ReplicaPlot) error {
	if len(htx) == 0 && len(plots) == 0 {
		return nil
	}
	return tenant.WithTenantTx(ctx, r.db, tid, func(tx pgx.Tx) error {
		for _, h := range htx {
			if err := store.PutReplicaHTX(ctx, tx, tid, h); err != nil {
				return err
			}
		}
		for _, p := range plots {
			if err := store.PutReplicaPlot(ctx, tx, tid, p); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r dbReplica) refs(ctx context.Context, tid uuid.UUID) (htx, plots []uuid.UUID, err error) {
	err = tenant.WithTenantTx(ctx, r.db, tid, func(tx pgx.Tx) error {
		var e error
		htx, plots, e = store.ReplicaRefs(ctx, tx)
		return e
	})
	return htx, plots, err
}

func (s *Service) dirReplica() directoryReplica {
	if s.replica != nil {
		return s.replica
	}
	return dbReplica{db: s.DB}
}

func (s *Service) dirMaxAge() time.Duration {
	if s.DirectoryMaxAge > 0 {
		return s.DirectoryMaxAge
	}
	return DefaultDirectoryMaxAge
}

func (s *Service) dirBudget() time.Duration {
	if s.DirectoryLookupBudget == 0 {
		return DefaultDirectoryLookupBudget
	}
	return s.DirectoryLookupBudget
}

// lookupHTX tra một HTX trên nền tảng; ok = false khi không gọi được (giữ nguyên bản sao).
func (s *Service) lookupHTX(ctx context.Context, ref uuid.UUID) (store.ReplicaHTX, bool) {
	h, err := s.Directory.Cooperative(ctx, ref)
	now := s.Clock.Now()
	switch {
	case errors.Is(err, port.ErrDirectoryNotFound):
		return store.ReplicaHTX{Ref: ref, AsOf: now}, true
	case err != nil:
		return store.ReplicaHTX{}, false
	}
	return store.ReplicaHTX{Ref: ref, Present: true, CommuneCode: h.CommuneCode, Active: h.Active, AsOf: now}, true
}

// lookupPlot tra một thửa trên nền tảng; ok = false khi không gọi được.
func (s *Service) lookupPlot(ctx context.Context, ref uuid.UUID) (store.ReplicaPlot, bool) {
	p, err := s.Directory.Plot(ctx, ref)
	now := s.Clock.Now()
	switch {
	case errors.Is(err, port.ErrDirectoryNotFound):
		return store.ReplicaPlot{Ref: ref, AsOf: now}, true
	case err != nil:
		return store.ReplicaPlot{}, false
	}
	return store.ReplicaPlot{Ref: ref, Present: true, CooperativeRef: p.CooperativeRef, CommuneCode: p.CommuneCode, Status: p.Status,
		AsOf: now}, true
}

// lookupMissing tra trực tiếp các mục chưa có / ghi "không có" của snap (tổng thời gian ≤ ngân sách), gộp kết
// quả vào snap và ghi vào bản sao (bảng đệm: lỗi ghi không làm hỏng thao tác).
func (s *Service) lookupMissing(ctx context.Context, tid, htx uuid.UUID, plots []uuid.UUID, snap dirSnapshot) dirSnapshot {
	budget := s.dirBudget()
	if budget < 0 {
		return snap
	}
	needHTX := snap.htx == nil || !snap.htx.Present
	var needPlots []uuid.UUID
	for _, p := range plots {
		if r, ok := snap.plots[p]; !ok || !r.Present {
			needPlots = append(needPlots, p)
		}
	}
	n := len(needPlots)
	if needHTX {
		n++
	}
	if n == 0 {
		return snap
	}
	lctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	hs, ps := newSlots[store.ReplicaHTX](1), newSlots[store.ReplicaPlot](len(needPlots))
	runBounded(lctx, n, dirParallel, func(c context.Context, i int) {
		if needHTX {
			if i == 0 {
				if h, ok := s.lookupHTX(c, htx); ok {
					hs.set(0, h)
				}
				return
			}
			i--
		}
		if p, ok := s.lookupPlot(c, needPlots[i]); ok {
			ps.set(i, p)
		}
	})
	gotH, gotP := hs.close(), ps.close()
	if snap.plots == nil {
		snap.plots = map[uuid.UUID]store.ReplicaPlot{}
	}
	var saveH []store.ReplicaHTX
	var saveP []store.ReplicaPlot
	if h := gotH[0]; h != nil {
		snap.htx, saveH = h, []store.ReplicaHTX{*h}
	}
	for _, p := range gotP {
		if p != nil {
			snap.plots[p.Ref] = *p
			saveP = append(saveP, *p)
		}
	}
	_ = s.dirReplica().save(ctx, tid, saveH, saveP)
	return snap
}

// checkDirectory đối chiếu kế hoạch với ảnh chụp danh mục (thuần — kiểm thử đơn vị): điền mã xã từ HTX,
// trả cảnh báo cũ / chưa đối chiếu và as_of cũ nhất đã dùng; bằng chứng khẳng định → lỗi 422.
func checkDirectory(pl plan, snap dirSnapshot, now time.Time, maxAge time.Duration) (plan, error) {
	warn := map[string]bool{}
	use := func(asOf time.Time) {
		if now.Sub(asOf) > maxAge {
			warn[WarnDirectoryStale] = true
		}
		if pl.asOf == nil || asOf.Before(*pl.asOf) {
			t := asOf
			pl.asOf = &t
		}
	}
	h := snap.htx
	switch {
	case h == nil:
		warn[WarnDirectoryUnverified] = true
	case !h.Present:
		return pl, ErrHTXNotFound
	case !h.Active:
		return pl, ErrHTXNotFound.WithDetail("HTX đã vô hiệu hóa")
	default:
		use(h.AsOf)
		if pl.commune == "" {
			pl.commune = h.CommuneCode
		}
	}
	if !communeRe.MatchString(pl.commune) {
		if h == nil {
			return pl, problem.Validation.WithField("commune_code", "chưa đối chiếu được HTX với nền tảng — ghi commune_code")
		}
		return pl, problem.Validation.WithField("commune_code", "nền tảng không có mã xã của HTX — ghi commune_code")
	}
	for _, ref := range pl.plots {
		p, ok := snap.plots[ref]
		switch {
		case !ok:
			warn[WarnDirectoryUnverified] = true
			continue
		case !p.Present:
			return pl, ErrPlotNotFound.WithDetail(ref.String())
		case p.CooperativeRef == nil || *p.CooperativeRef != pl.htx || p.Status == "retired":
			return pl, ErrPlotOtherHTX.WithDetail(ref.String())
		}
		use(p.AsOf)
	}
	pl.warnings = pl.warnings[:0]
	for w := range warn {
		pl.warnings = append(pl.warnings, w)
	}
	slices.Sort(pl.warnings)
	return pl, nil
}

// DirectoryRefresh là kết quả một lượt làm mới bản sao danh mục.
type DirectoryRefresh struct {
	HTX         int `json:"htx"`
	Plots       int `json:"plots"`
	Present     int `json:"present"`
	Absent      int `json:"absent"`
	Unavailable int `json:"unavailable"`
}

// ErrDirectoryRefresh: không tra được mục nào trong lượt làm mới (job báo lỗi để vận hành thấy).
var ErrDirectoryRefresh = errors.New("fieldops: không tra được mục nào của danh mục nền tảng")

// RefreshDirectory làm mới bản sao danh mục HTX/thửa của tenant tid (job fieldops_directory): mọi mục đã có
// ∪ mục lệnh gom đang mở tham chiếu, song song tối đa dirParallel. Tra không được thì giữ dòng cũ.
func (s *Service) RefreshDirectory(ctx context.Context, tid uuid.UUID) (DirectoryRefresh, error) {
	var r DirectoryRefresh
	if s.DirectoryUnconfigured || s.Directory == nil {
		return r, nil
	}
	rep := s.dirReplica()
	htx, plots, err := rep.refs(ctx, tid)
	if err != nil {
		return r, err
	}
	r.HTX, r.Plots = len(htx), len(plots)
	hs, ps := newSlots[store.ReplicaHTX](len(htx)), newSlots[store.ReplicaPlot](len(plots))
	runBounded(ctx, len(htx)+len(plots), dirParallel, func(c context.Context, i int) {
		if i < len(htx) {
			if h, ok := s.lookupHTX(c, htx[i]); ok {
				hs.set(i, h)
			}
			return
		}
		if p, ok := s.lookupPlot(c, plots[i-len(htx)]); ok {
			ps.set(i-len(htx), p)
		}
	})
	var saveH []store.ReplicaHTX
	var saveP []store.ReplicaPlot
	for _, h := range hs.close() {
		switch {
		case h == nil:
			r.Unavailable++
			continue
		case h.Present:
			r.Present++
		default:
			r.Absent++
		}
		saveH = append(saveH, *h)
	}
	for _, p := range ps.close() {
		switch {
		case p == nil:
			r.Unavailable++
			continue
		case p.Present:
			r.Present++
		default:
			r.Absent++
		}
		saveP = append(saveP, *p)
	}
	for len(saveH) > 0 || len(saveP) > 0 {
		nh := min(len(saveH), dirSaveChunk)
		np := min(len(saveP), dirSaveChunk-nh)
		if err := rep.save(ctx, tid, saveH[:nh], saveP[:np]); err != nil {
			return r, err
		}
		saveH, saveP = saveH[nh:], saveP[np:]
	}
	if total := r.HTX + r.Plots; total > 0 && r.Unavailable == total {
		return r, ErrDirectoryRefresh
	}
	return r, nil
}

// ---------------------------------------------------------------------------
// Chạy song song có chặn trên (lời gọi nền tảng ngoài giao dịch DB)
// ---------------------------------------------------------------------------

// runBounded chạy fn(ctx, i) với i ∈ [0, n), song song tối đa parallel, và trả khi xong hết HOẶC ctx hết
// hạn — kể cả khi bộ điều hợp không tôn trọng ctx (lời gọi còn treo bị bỏ, kết quả muộn bị slots loại).
func runBounded(ctx context.Context, n, parallel int, fn func(context.Context, int)) {
	if n == 0 {
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		sem := make(chan struct{}, max(parallel, 1))
		var wg sync.WaitGroup
		defer wg.Wait()
		for i := range n {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				fn(ctx, i)
			}()
		}
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// slots giữ kết quả theo chỉ số dưới khóa; sau close, kết quả muộn bị bỏ.
type slots[T any] struct {
	mu     sync.Mutex
	v      []*T
	closed bool
}

func newSlots[T any](n int) *slots[T] { return &slots[T]{v: make([]*T, n)} }

func (s *slots[T]) set(i int, x T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.v[i] = &x
	}
}

func (s *slots[T]) close() []*T {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return slices.Clone(s.v)
}
