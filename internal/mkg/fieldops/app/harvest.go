package app

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Cảnh báo (không chặn) trả kèm kết quả ghi — mã ổn định cho client.
const (
	WarnHarvestUnknown       = "fieldops.harvest_date_unknown"        // FM-01: nền tảng chưa có / chưa chia sẻ ngày gặt → ghi được, gắn cờ
	WarnHarvestUnavailable   = "fieldops.harvest_date_unavailable"    // FM-01: không gọi được nền tảng, không có bảng đệm → ghi được, gắn cờ
	WarnPlannedBeforeHarvest = "fieldops.planned_before_harvest"      // FM-01: ngày bắt đầu dự kiến trước ngày gặt của thửa
	WarnDeadlineAtRisk       = "fieldops.baling_deadline_at_risk"     // FM-02: ngày kết thúc dự kiến sau hạn cuộn
	WarnOverCapacity         = "fieldops.crew_over_capacity_override" // FM-06: giám sát giao vượt năng lực kèm lý do
)

// ParamBalingDeadlineDays là khóa tham số N của FM-02.
const ParamBalingDeadlineDays = "baling_deadline_days"

// fetchHarvest đọc ngày gặt của thửa từ nền tảng (NGOÀI giao dịch DB) và chọn vụ ứng với
// khung lệnh gom bắt đầu từ anchor. Không bao giờ trả lỗi: không có → unknown, không gọi
// được → unavailable (người gọi dùng bảng đệm nếu có).
func (s *Service) fetchHarvest(ctx context.Context, orderID, plot uuid.UUID, anchor time.Time) store.PlotHarvest {
	h := store.PlotHarvest{OrderID: orderID, PlotRef: plot, Check: domain.HarvestUnavailable, FetchedAt: s.Clock.Now()}
	if s.Harvest == nil {
		return h
	}
	xs, err := s.Harvest.PlotSeasons(ctx, plot)
	switch {
	case errors.Is(err, port.ErrDirectoryNotFound):
		h.Check = domain.HarvestUnknown
		return h
	case err != nil:
		return h
	}
	sel := domain.SelectSeason(domain.SeasonsFrom(xs), anchor)
	if sel == nil {
		h.Check = domain.HarvestUnknown
		return h
	}
	d := sel.Expected
	h.Check, h.Date, h.OpenDate, h.SeasonRef = domain.HarvestOK, &d, sel.Open, sel.SeasonRef
	return h
}

// Chặn trên của việc đọc ngày gặt từ nền tảng (R-INT-02: không để thao tác người dùng chờ nền tảng vô hạn).
const (
	harvestBudget    = 3 * time.Second // tổng thời gian chờ trong một thao tác người dùng (lập/sửa/giao lệnh, ghi cuộn)
	harvestJobBudget = 2 * time.Minute // mỗi lệnh gom trong job fieldops_harvest_dates
	harvestParallel  = 8               // lời gọi song song tối đa
)

// fetchPlots đọc ngày gặt của các thửa của lệnh gom (ngoài giao dịch) trong ngân sách harvestBudget.
func (s *Service) fetchPlots(ctx context.Context, o store.Order) []store.PlotHarvest {
	return s.fetchPlotsWithin(ctx, o, harvestBudget)
}

// fetchPlotsWithin đọc song song (tối đa harvestParallel) với tổng thời gian ≤ budget; thửa chưa có kết
// quả khi hết hạn → unavailable (gắn cờ, dùng bảng đệm nếu có) — không bao giờ chặn thao tác.
func (s *Service) fetchPlotsWithin(ctx context.Context, o store.Order, budget time.Duration) []store.PlotHarvest {
	bctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	got := newSlots[store.PlotHarvest](len(o.PlotRefs))
	runBounded(bctx, len(o.PlotRefs), harvestParallel, func(c context.Context, i int) {
		got.set(i, s.fetchHarvest(c, o.OrderID, o.PlotRefs[i], o.WindowFrom))
	})
	out := make([]store.PlotHarvest, 0, len(o.PlotRefs))
	for i, h := range got.close() {
		if h == nil {
			out = append(out, store.PlotHarvest{OrderID: o.OrderID, PlotRef: o.PlotRefs[i], Check: domain.HarvestUnavailable,
				FetchedAt: s.Clock.Now()})
			continue
		}
		out = append(out, *h)
	}
	return out
}

// putHarvests ghi bảng đệm (bỏ qua unavailable — không đè ngày đã biết, không thêm dòng rỗng).
func putHarvests(ctx context.Context, tx pgx.Tx, tid, orderID uuid.UUID, hs []store.PlotHarvest) error {
	for _, h := range hs {
		h.OrderID = orderID
		if err := store.PutPlotHarvest(ctx, tx, tid, h); err != nil {
			return err
		}
	}
	return nil
}

// resolveHarvest kết hợp kết quả gọi trực tiếp với bảng đệm (trong giao dịch): gọi được
// và biết ngày → partner; không thì bảng đệm có ngày → cache; không thì none (gắn cờ).
func resolveHarvest(ctx context.Context, tx pgx.Tx, live store.PlotHarvest) (store.PlotHarvest, string, error) {
	if live.Check == domain.HarvestOK {
		return live, "partner", nil
	}
	cached, err := store.GetPlotHarvest(ctx, tx, live.OrderID, live.PlotRef)
	if err != nil {
		return live, "", err
	}
	if cached != nil && cached.Check == domain.HarvestOK {
		return *cached, "cache", nil
	}
	return live, "none", nil
}

func harvestWarning(check string) string {
	switch check {
	case domain.HarvestUnknown:
		return WarnHarvestUnknown
	case domain.HarvestUnavailable:
		return WarnHarvestUnavailable
	}
	return ""
}

// balingDays trả N của FM-02 hiệu lực vào ngày day: phiên bản đã duyệt mới nhất, không có
// thì mặc định 3 (tạm, chờ khách). label ghim vào kết quả ("baling_deadline_days:v<n>" |
// "provisional-default").
func balingDays(ctx context.Context, tx pgx.Tx, day time.Time) (int, string, bool, error) {
	p, err := store.EffectiveParam(ctx, tx, ParamBalingDeadlineDays, day)
	if err != nil || p == nil {
		return domain.DefaultBalingDeadlineDays, "provisional-default", true, err
	}
	return int(p.Value.IntPart()), ParamBalingDeadlineDays + ":v" + strconv.Itoa(p.VersionSeq), p.Provisional, nil
}

// RefreshHarvestDates làm mới bảng đệm ngày gặt của mọi lệnh gom approved/assigned của tenant
// tid (job fieldops_harvest_dates, hoặc giám sát gọi tay). Trả số (lệnh, thửa) đã biết ngày gặt.
func (s *Service) RefreshHarvestDates(ctx context.Context, tid uuid.UUID) (HarvestRefresh, error) {
	var orders []store.Order
	if err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		var e error
		orders, e = store.OpenOrderPlots(ctx, tx)
		return e
	}); err != nil {
		return HarvestRefresh{}, err
	}
	var r HarvestRefresh
	for _, o := range orders {
		hs := s.fetchPlotsWithin(ctx, o, harvestJobBudget)
		for _, h := range hs {
			r.Plots++
			switch h.Check {
			case domain.HarvestOK:
				r.Known++
			case domain.HarvestUnknown:
				r.Unknown++
			default:
				r.Unavailable++
			}
		}
		if err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error { return putHarvests(ctx, tx, tid, o.OrderID, hs) }); err != nil {
			return r, err
		}
	}
	return r, nil
}

// HarvestRefresh là kết quả một lượt làm mới bảng đệm ngày gặt.
type HarvestRefresh struct {
	Plots       int `json:"plots"`
	Known       int `json:"known"`
	Unknown     int `json:"unknown"`
	Unavailable int `json:"unavailable"`
}

// RefreshHarvest: giám sát làm mới bảng đệm ngày gặt của tenant mình.
func (s *Service) RefreshHarvest(ctx context.Context) (HarvestRefresh, error) {
	p, err := authz.Require(ctx, RoleSupervisor)
	if err != nil {
		return HarvestRefresh{}, err
	}
	return s.RefreshHarvestDates(ctx, p.TenantID)
}

// ---------------------------------------------------------------------------
// FM-02: hạn cuộn sau gặt
// ---------------------------------------------------------------------------

// BalingItem là trạng thái hạn cuộn của một (lệnh gom, thửa).
type BalingItem struct {
	Row    store.BalingRow
	Baling domain.Baling
}

// BalingPage là một trang hạn cuộn.
type BalingPage struct {
	Items        []BalingItem
	Next         *[2]uuid.UUID
	Days         int
	ParamVersion string
	Provisional  bool
	Today        time.Time
}

func balingOf(r store.BalingRow, today time.Time, days int) domain.Baling {
	var harvest, baled *time.Time
	if r.HarvestCheck != nil && *r.HarvestCheck == domain.HarvestOK && r.HarvestDate != nil {
		harvest = r.HarvestDate
	}
	if r.BaledAt != nil {
		d := domain.Day(*r.BaledAt)
		baled = &d
	}
	return domain.BalingState(harvest, baled, today, days)
}

// BalingDeadlines liệt kê hạn cuộn (FM-02) của các lệnh gom đang mở; status lọc theo trạng
// thái (overdue, baled_late, flagged = overdue ∪ baled_late, …; rỗng = tất cả). Trang đầy đủ
// limit mục (quét tiếp khi lọc).
func (s *Service) BalingDeadlines(ctx context.Context, orderID *uuid.UUID, status string, after [2]uuid.UUID, limit int) (BalingPage, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var pg BalingPage
	pg.Today = domain.Day(s.Clock.Now())
	var orders []uuid.UUID
	if orderID != nil {
		orders = []uuid.UUID{*orderID}
	}
	err := s.read(ctx, func(tx pgx.Tx) error {
		var err error
		if pg.Days, pg.ParamVersion, pg.Provisional, err = balingDays(ctx, tx, pg.Today); err != nil {
			return err
		}
		cur := after
		for {
			rows, err := store.BalingRows(ctx, tx, orders, cur, 500)
			if err != nil {
				return err
			}
			for _, r := range rows {
				cur = [2]uuid.UUID{r.OrderID, r.PlotRef}
				b := balingOf(r, pg.Today, pg.Days)
				if !matchBaling(status, b) {
					continue
				}
				if len(pg.Items) == limit {
					last := [2]uuid.UUID{pg.Items[limit-1].Row.OrderID, pg.Items[limit-1].Row.PlotRef}
					pg.Next = &last
					return nil
				}
				pg.Items = append(pg.Items, BalingItem{Row: r, Baling: b})
			}
			if len(rows) < 500 {
				return nil
			}
		}
	})
	return pg, err
}

func matchBaling(status string, b domain.Baling) bool {
	switch status {
	case "":
		return true
	case "flagged":
		return b.Flagged()
	}
	return b.Status == status
}

// OrderBaling tính hạn cuộn của mọi thửa của các lệnh orderIDs (chi tiết / danh sách lệnh gom).
func (s *Service) orderBaling(ctx context.Context, tx pgx.Tx, orderIDs []uuid.UUID) (map[uuid.UUID][]BalingItem, int, error) {
	out := map[uuid.UUID][]BalingItem{}
	if len(orderIDs) == 0 {
		return out, 0, nil
	}
	today := domain.Day(s.Clock.Now())
	days, _, _, err := balingDays(ctx, tx, today)
	if err != nil {
		return nil, 0, err
	}
	var cur [2]uuid.UUID
	for {
		rows, err := store.BalingRows(ctx, tx, orderIDs, cur, 1000)
		if err != nil {
			return nil, 0, err
		}
		for _, r := range rows {
			cur = [2]uuid.UUID{r.OrderID, r.PlotRef}
			out[r.OrderID] = append(out[r.OrderID], BalingItem{Row: r, Baling: balingOf(r, today, days)})
		}
		if len(rows) < 1000 {
			return out, days, nil
		}
	}
}

// OverdueFlags trả cờ FM-02 theo lệnh gom (ít nhất một thửa quá hạn chưa cuộn).
func OverdueFlags(items map[uuid.UUID][]BalingItem) map[uuid.UUID]bool {
	out := map[uuid.UUID]bool{}
	for id, xs := range items {
		for _, x := range xs {
			if x.Baling.Status == domain.BalingOverdue {
				out[id] = true
			}
		}
	}
	return out
}
