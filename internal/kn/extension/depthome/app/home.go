package app

// Trang chủ phòng (FN-17, UR-TINH-06): giới thiệu + chức năng nhiệm vụ, sơ đồ tổ chức (từ tài khoản), thẻ chỉ số theo
// phòng (BR-01: số liệu chỉ từ biểu đã nộp/đã chốt — đọc qua họ closed_figures/v1 của W11.10 và view công bố
// pf_price_pub; đếm nghiệp vụ của chính pf/extension), văn bản mới của kho phòng.

import (
	"context"
	"slices"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/depthome/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/depthome/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// KPI là một thẻ chỉ số. Key ổn định cho giao diện gắn liên kết sang màn chi tiết (FN-17 B3).
type KPI struct {
	Key   string
	Label string
	Value int
	Tone  string // neutral | ok | warn | bad | info
}

// ClosedPeriod là tiến độ một kỳ nguồn của phòng (biểu × kỳ — closed_figures/v1).
type ClosedPeriod struct {
	FormCode      string
	PeriodType    string
	PeriodKey     string
	PeriodLabel   string
	PeriodTo      *time.Time
	UnitsTotal    *int
	UnitsWithData *int
	Closed        bool
	ClosedAt      *time.Time
}

// OfficerView là một dòng sơ đồ tổ chức.
type OfficerView struct {
	Officer
	Name string
}

// LibraryStats là thống kê kho văn bản (văn bản đang hiện, người gọi xem được).
type LibraryStats struct {
	Total, Expired, New30 int
}

// Home là trang chủ phòng.
type Home struct {
	Department     string
	AsOf           time.Time
	Manager        bool // Lãnh đạo phòng/Admin: thẻ chờ duyệt, sửa giới thiệu
	ReadOnly       bool
	CanEditProfile bool
	Profile        *ProfileView
	KPIs           []KPI
	Closed         []ClosedPeriod
	Price          *store.PriceInfo
	Officers       []OfficerView
	Library        LibraryStats
	LatestDocs     []DocView
}

// mondayOf trả thứ Hai của tuần chứa d.
func mondayOf(d time.Time) time.Time {
	wd := (int(d.Weekday()) + 6) % 7
	return d.AddDate(0, 0, -wd)
}

// GetHome dựng trang chủ phòng dept (Admin bắt buộc chọn; Cấp Tỉnh = phòng của phiên).
func (s *Service) GetHome(ctx context.Context, dept string) (Home, error) {
	c, err := s.resolve(ctx, dept, port.SessionRoleAdmin, port.SessionRoleProvince)
	if err != nil {
		return Home{}, err
	}
	now, today := s.now(), s.today()
	h := Home{Department: c.dept, AsOf: now, Manager: c.leaderView(), ReadOnly: c.k.ReadOnly(), CanEditProfile: c.manager()}
	var counts store.Counts
	var docs []store.DocRow
	var folders []store.FolderRow
	var profile []store.ProfileRow
	var price store.PriceInfo
	priceOK := false
	err = s.readTx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var err error
		if counts, err = store.CountsFor(ctx, tx, c.dept, mondayOf(today), now); err != nil {
			return err
		}
		if profile, err = store.ProfileHistory(ctx, tx, c.dept, 1); err != nil {
			return err
		}
		if folders, err = store.FoldersCurrent(ctx, tx, c.dept); err != nil {
			return err
		}
		if docs, err = store.DocsCurrent(ctx, tx, c.dept); err != nil {
			return err
		}
		if c.dept == partyport.DeptThongTinTruyenThong {
			pp := s.PriceProfile
			if pp == "" {
				pp = "kn"
			}
			price, priceOK, err = store.PriceSummary(ctx, tx, pp)
		}
		return err
	})
	if err != nil {
		return Home{}, err
	}
	if priceOK {
		h.Price = &price
	}
	h.KPIs = kpis(c, counts, h.Price)

	// Kho văn bản: thống kê + 5 văn bản mới (đang hiện).
	hiddenFolders := hiddenSet(folders)
	for _, d := range docs {
		if d.Hidden || hiddenFolders[folderKey(d.FolderRef)] {
			continue
		}
		h.Library.Total++
		if domain.Expired(d.EffectiveTo, today) {
			h.Library.Expired++
		}
		if d.Seq >= 1 && firstRecordedWithin(d, now, 30) {
			h.Library.New30++
		}
		if len(h.LatestDocs) < 5 {
			h.LatestDocs = append(h.LatestDocs, s.docView(c, d, folders, today, nil))
		}
	}
	h.KPIs = append(h.KPIs, KPI{Key: "docs_total", Label: "Văn bản trong kho", Value: h.Library.Total, Tone: "info"},
		KPI{Key: "docs_expired", Label: "Văn bản hết hiệu lực", Value: h.Library.Expired, Tone: "neutral"})

	refs := []uuid.UUID{}
	if len(profile) > 0 {
		refs = append(refs, profile[0].ActorRef)
	}
	// Sơ đồ tổ chức: cán bộ Cấp Tỉnh của phòng (từ tài khoản — UR-TINH-06 quy tắc).
	var officers []Officer
	if s.Sources.Officers != nil {
		if officers, err = s.Sources.Officers(ctx, c.p.TenantID, c.dept); err != nil {
			return Home{}, err
		}
	}
	for _, o := range officers {
		refs = append(refs, o.ActorRef)
	}
	for _, d := range h.LatestDocs {
		refs = append(refs, d.ActorRef)
	}
	nm := s.names(ctx, refs)
	if len(profile) > 0 {
		h.Profile = &ProfileView{ProfileRow: profile[0], ActorName: nm[profile[0].ActorRef]}
	}
	for _, o := range officers {
		h.Officers = append(h.Officers, OfficerView{Officer: o, Name: nm[o.ActorRef]})
	}
	sort.SliceStable(h.Officers, func(i, j int) bool {
		if h.Officers[i].Leader != h.Officers[j].Leader {
			return h.Officers[i].Leader
		}
		return h.Officers[i].Name < h.Officers[j].Name
	})
	for i := range h.LatestDocs {
		h.LatestDocs[i].ActorName = nm[h.LatestDocs[i].ActorRef]
	}

	// Số liệu đã chốt (W11.10): 8 kỳ nguồn gần nhất của phòng trong 60 ngày.
	if s.Sources.Closed != nil && (c.dept == partyport.DeptTrongTrot || c.dept == partyport.DeptChanNuoiThuySan) {
		from := today.AddDate(0, 0, -60)
		figs, err := s.Sources.Closed.ClosedFigures(ctx, c.p.TenantID, port.ClosedFiguresQuery{Department: c.dept, From: &from, To: &today})
		if err != nil {
			return Home{}, err
		}
		h.Closed = closedPeriods(figs, 8)
	}
	return h, nil
}

func firstRecordedWithin(d store.DocRow, now time.Time, days int) bool {
	// Phiên bản 1 là lúc tạo; bản hiện hành có thể là bản sửa — dùng EventTime của bản hiện hành nếu là "created".
	if d.Event == domain.DocCreated {
		return now.Sub(d.EventTime) <= time.Duration(days)*24*time.Hour
	}
	return false
}

// kpis dựng thẻ theo phòng. Thẻ "chờ duyệt" chỉ cho Lãnh đạo phòng/Admin (FN-17 mô tả).
func kpis(c caller, n store.Counts, price *store.PriceInfo) []KPI {
	out := []KPI{{Key: "schedules_week", Label: "Lịch công tác tuần này", Value: n.SchedulesWeek, Tone: "info"}}
	if c.leaderView() {
		tone := "neutral"
		if n.SchedulesPending > 0 {
			tone = "warn"
		}
		out = append(out, KPI{Key: "schedules_pending", Label: "Lịch công tác chờ duyệt", Value: n.SchedulesPending, Tone: tone})
	}
	warn := func(v int, t string) string {
		if v > 0 {
			return t
		}
		return "ok"
	}
	switch c.dept {
	case partyport.DeptChanNuoiThuySan:
		out = append(out,
			KPI{Key: "alerts_open", Label: "Cảnh báo cần xử lý", Value: n.AlertsOpen, Tone: warn(n.AlertsOpen, "warn")},
			KPI{Key: "alerts_overdue", Label: "Cảnh báo quá hạn", Value: n.AlertsOverdue, Tone: warn(n.AlertsOverdue, "bad")},
			KPI{Key: "outbreaks_active", Label: "Ổ dịch đang theo dõi", Value: n.OutbreaksActive, Tone: warn(n.OutbreaksActive, "bad")})
	case partyport.DeptTrongTrot:
		out = append(out,
			KPI{Key: "tasks_open", Label: "Nhiệm vụ đang mở", Value: n.TasksOpen, Tone: "info"},
			KPI{Key: "tasks_overdue", Label: "Nhiệm vụ quá hạn", Value: n.TasksOverdue, Tone: warn(n.TasksOverdue, "bad")},
			KPI{Key: "field_reports_week", Label: "Báo cáo hiện trường 7 ngày", Value: n.FieldReportsWeek, Tone: "info"})
	case partyport.DeptKeHoachTaiChinh:
		out = append(out,
			KPI{Key: "khtc_periods_year", Label: "Kỳ báo cáo đã mở trong năm", Value: n.KHTCPeriodsYear, Tone: "info"},
			KPI{Key: "khtc_exports", Label: "Lần xuất báo cáo Mẫu 1–5", Value: n.KHTCExports, Tone: "info"})
	case partyport.DeptThongTinTruyenThong:
		if price != nil {
			out = append(out,
				KPI{Key: "price_items", Label: "Mặt hàng trên bảng giá đã duyệt gần nhất", Value: price.Items, Tone: "info"},
				KPI{Key: "price_tickets_week", Label: "Phiếu báo giá tuần gần nhất", Value: price.Tickets, Tone: "info"})
		}
	}
	return out
}

// closedPeriods gộp dòng closed_figures/v1 theo (loại kỳ, khóa kỳ, biểu), lấy n kỳ có ngày cuối gần nhất.
func closedPeriods(figs []port.ClosedFigure, n int) []ClosedPeriod {
	type key struct{ pt, pk, form string }
	seen := map[key]int{}
	var out []ClosedPeriod
	for _, f := range figs {
		k := key{f.PeriodType, f.PeriodKey, f.FormCode}
		i, ok := seen[k]
		if !ok {
			seen[k] = len(out)
			out = append(out, ClosedPeriod{FormCode: f.FormCode, PeriodType: f.PeriodType, PeriodKey: f.PeriodKey, PeriodLabel: f.PeriodLabel,
				PeriodTo: f.PeriodTo, UnitsTotal: f.UnitsTotal, UnitsWithData: f.UnitsWithData, Closed: f.KeyClosed, ClosedAt: f.ClosedAt})
			continue
		}
		// Nhiều nguồn cùng khóa: cộng đơn vị, chốt khi mọi nguồn chốt.
		cp := &out[i]
		cp.UnitsTotal = addInt(cp.UnitsTotal, f.UnitsTotal)
		cp.UnitsWithData = addInt(cp.UnitsWithData, f.UnitsWithData)
		cp.Closed = cp.Closed && f.KeyClosed
	}
	slices.SortStableFunc(out, func(a, b ClosedPeriod) int {
		switch {
		case a.PeriodTo == nil && b.PeriodTo == nil:
			return 0
		case a.PeriodTo == nil:
			return 1
		case b.PeriodTo == nil:
			return -1
		}
		return b.PeriodTo.Compare(*a.PeriodTo)
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func addInt(a, b *int) *int {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	v := *a + *b
	return &v
}
