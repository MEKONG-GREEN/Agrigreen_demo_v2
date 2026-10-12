package app

// Xã báo giá + phân công (W11.72, pf 000145; BRD Báo giá nông sản v1.3 FN-AD-04.02 Danh mục xã báo giá, FN-AD-04.01
// gán cán bộ Xã vào xã / phân công xã cho cán bộ Cấp Tỉnh theo ngày hiệu lực; mục II "Nhóm vùng").
//
// Quyền:
//   - đọc danh sách xã, nhóm vùng, ĐVHC: mọi cấp của profile và vai quản trị (BRD: "Cấp Tỉnh xem");
//   - thêm/sửa/ngưng xã, nhóm vùng, phân công: vai quản trị (Admin — FN-AD-04.01/04.02 "Admin · Quản trị hệ thống");
//   - danh bạ cán bộ (tên — dữ liệu cá nhân): vai quản trị và Cấp Tỉnh.
//
// Ngày hiệu lực của mọi thay đổi xã/phân công: vắng → ngày báo giá kế tiếp (lịch W11.74); không trong quá khứ. Ngưng
// xã hôm nay khi xã đã gửi phiếu (W11.75 — PointHooks.Submitted) → áp dụng từ ngày báo giá kế tiếp.

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// PointHooks là điểm nối của xã báo giá (gốc lắp ráp, W11.75). Trường nil → mặc định ghi bên cạnh.
type PointHooks struct {
	// Officers: danh bạ cán bộ KN (pf/accounts qua gốc lắp ráp). nil → phân công không kiểm người, không liệt kê tên.
	Officers port.Officers
	// Submitted: xã point đã gửi phiếu ngày day (W11.75). nil → chưa gửi: lần nhắc 2 nhắc mọi xã có cán bộ Xã; ngưng xã
	// hôm nay áp dụng ngay.
	Submitted func(ctx context.Context, tx pgx.Tx, day time.Time, point string) (bool, error)
}

var errNotPointAdmin = problem.Forbidden.WithDetail("chỉ vai quản trị (Admin) quản lý xã báo giá, nhóm vùng, phân công")

// pointSnap là ảnh sổ xã báo giá của tenant trong một giao dịch.
type pointSnap struct {
	points   []store.PointVersion
	pvers    []domain.Ver[domain.Point]
	officers []store.OfficerVersion
	overs    []domain.Ver[domain.OfficerAssign]
	regions  []domain.Ver[domain.Entry]
}

func loadPointSnap(ctx context.Context, tx pgx.Tx) (*pointSnap, error) {
	var (
		ps  pointSnap
		err error
	)
	if ps.points, err = store.LoadPoints(ctx, tx); err != nil {
		return nil, err
	}
	for _, v := range ps.points {
		ps.pvers = append(ps.pvers, v.Ver)
	}
	if ps.officers, err = store.LoadOfficers(ctx, tx); err != nil {
		return nil, err
	}
	for _, v := range ps.officers {
		ps.overs = append(ps.overs, v.Ver)
	}
	if ps.regions, err = store.LoadEntries(ctx, tx, domain.KindRegion); err != nil {
		return nil, err
	}
	return &ps, nil
}

func (ps *pointSnap) states(at time.Time) map[string]domain.PointState {
	return domain.PointStates(ps.pvers, at)
}

func (ps *pointSnap) regionActive(code string, at time.Time) bool {
	r, ok := domain.EffectiveAt(ps.regions, at)[code]
	return ok && r.Status == domain.StatusActive
}

func (ps *pointSnap) portPoint(st domain.PointState, at time.Time) port.Point {
	off, _ := domain.OfficerAt(ps.overs, domain.OfficerCommune, st.Code, at)
	own, _ := domain.OfficerAt(ps.overs, domain.OfficerProvince, st.Code, at)
	return port.Point{Code: st.Code, Name: st.Name, AdminUnitCode: st.AdminUnitCode, RegionCode: st.RegionCode, Since: st.Since,
		Officer: off, Owner: own}
}

// requirePoints kiểm từng mã là xã báo giá có trong sổ và đang hoạt động tại ngày at (W11.72 — thay liên kết lỏng
// point_code của W11.73). Trả ảnh sổ để bên gọi dùng tiếp.
func requirePoints(ctx context.Context, tx pgx.Tx, codes []string, at time.Time) (*pointSnap, error) {
	ps, err := loadPointSnap(ctx, tx)
	if err != nil {
		return nil, err
	}
	states := ps.states(at)
	for _, code := range codes {
		st, ok := states[code]
		if !ok {
			return nil, port.ErrPointNotFound.WithDetail("xã báo giá " + code)
		}
		if st.State != domain.PointStateActive {
			return nil, port.ErrPointInactive.WithDetail("xã báo giá " + code + " không hoạt động tại " + at.Format(time.DateOnly))
		}
	}
	return ps, nil
}

// pointFrom trả ngày hiệu lực của một thay đổi xã/phân công: d (zero → ngày báo giá kế tiếp), không trong quá khứ.
func (s *Service) pointFrom(ctx context.Context, tx pgx.Tx, d time.Time) (time.Time, error) {
	today := domain.BusinessDay(s.Clock.Now())
	if d.IsZero() {
		return s.NextQuoteDay(ctx, tx, today)
	}
	d = dayOf(d)
	if d.Before(today) {
		return time.Time{}, port.ErrValidFrom.WithDetail("ngày hiệu lực " + d.Format(time.DateOnly) + " đã qua")
	}
	return d, nil
}

// communeAt tìm ĐVHC cấp xã code có hiệu lực tại ngày d.
func communeAt(units []store.AdminUnit, code string, d time.Time) (store.AdminUnit, bool) {
	for _, u := range units {
		if u.Code == code && u.Level == "commune" && u.EffectiveAt(d) {
			return u, true
		}
	}
	return store.AdminUnit{}, false
}

// latestUnit trả phiên bản mới nhất của ĐVHC code (để hiện tên, mã kế thừa khi đã hết hiệu lực).
func latestUnit(units []store.AdminUnit, code string) (store.AdminUnit, bool) {
	var (
		out store.AdminUnit
		ok  bool
	)
	for _, u := range units {
		if u.Code == code && (!ok || u.ValidFrom.After(out.ValidFrom)) {
			out, ok = u, true
		}
	}
	return out, ok
}

// ---------------------------------------------------------------------------------------------------------------------
// Nhóm vùng
// ---------------------------------------------------------------------------------------------------------------------

func regions(ctx context.Context, tx pgx.Tx, day time.Time) ([]port.Region, error) {
	rs, err := store.Effective(ctx, tx, domain.KindRegion, day)
	if err != nil {
		return nil, err
	}
	out := make([]port.Region, 0, len(rs))
	for _, r := range rs {
		out = append(out, port.Region{Code: r.Code, Name: r.Name, SortOrder: r.SortOrder, Version: r.Version, ValidFrom: r.ValidFrom})
	}
	return out, nil
}

// RegionView là một nhóm vùng tại ngày, kèm số xã đang hoạt động thuộc nhóm.
type RegionView struct {
	domain.Entry
	Version int64
	Status  string
	Points  int
}

// ListRegions đọc nhóm vùng tại ngày at (zero → hôm nay), mọi trạng thái, sắp theo thứ tự rồi mã.
func (s *Service) ListRegions(ctx context.Context, profile string, at time.Time) ([]RegionView, error) {
	_, c, err := s.resolve(ctx, profile)
	if err != nil {
		return nil, err
	}
	day := s.dayOrToday(at)
	out := []RegionView{}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		ps, err := loadPointSnap(ctx, tx)
		if err != nil {
			return err
		}
		count := map[string]int{}
		for _, st := range ps.states(day) {
			if st.State == domain.PointStateActive {
				count[st.RegionCode]++
			}
		}
		for code, v := range domain.EffectiveAt(ps.regions, day) {
			out = append(out, RegionView{Entry: v.V, Version: v.Seq, Status: v.Status, Points: count[code]})
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].SortOrder != out[j].SortOrder {
				return out[i].SortOrder < out[j].SortOrder
			}
			return out[i].Code < out[j].Code
		})
		return nil
	})
	return out, err
}

// RegionInput là nội dung nhóm vùng (Code rỗng khi thêm → vung_<n>; SortOrder 0 → cuối danh sách / giữ nguyên).
type RegionInput struct {
	Code      string
	Name      string
	SortOrder int
}

// CreateRegion thêm nhóm vùng có hiệu lực từ hôm nay (vai quản trị).
func (s *Service) CreateRegion(ctx context.Context, profile string, in RegionInput, idem string) (WriteResult, error) {
	_, c, err := s.resolve(ctx, profile)
	if err != nil {
		return WriteResult{}, err
	}
	if !c.manage {
		return WriteResult{}, errNotPointAdmin
	}
	var res WriteResult
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		vs, err := store.LoadEntries(ctx, tx, domain.KindRegion)
		if err != nil {
			return err
		}
		have, maxSort := map[string]bool{}, 0
		for _, v := range vs {
			have[v.V.Code] = true
			maxSort = max(maxSort, v.V.SortOrder)
		}
		code := in.Code
		if code == "" {
			for n := len(have) + 1; ; n++ {
				if code = fmt.Sprintf("vung_%d", n); !have[code] {
					break
				}
			}
		} else if have[code] {
			return problem.Conflict.WithDetail("mã nhóm vùng " + code + " đã có (kể cả đã ngưng) — mã không tái sử dụng")
		}
		sortOrder := in.SortOrder
		if sortOrder == 0 {
			sortOrder = maxSort + 10
		}
		e := domain.Entry{Kind: domain.KindRegion, Code: code, Name: strings.TrimSpace(in.Name), SortOrder: sortOrder,
			ValidFrom: domain.BusinessDay(s.Clock.Now())}
		if err := domain.Validate(e); err != nil {
			return problem.Validation.WithDetail(err.Error())
		}
		seq, err := store.InsertEntryVersion(ctx, tx, ids.New(), c.tenant, e, domain.StatusActive, s.Clock.Now(), c.ref,
			idemKey(idem, "region:"+code))
		if err != nil {
			return classify(err)
		}
		res = WriteResult{Code: code, Version: seq, ValidFrom: e.ValidFrom, Written: 1}
		return nil
	})
	return res, err
}

// ReviseRegion đổi tên/thứ tự nhóm vùng từ hôm nay (tên cũ giữ trong lịch sử).
func (s *Service) ReviseRegion(ctx context.Context, profile, code string, in RegionInput, idem string) (WriteResult, error) {
	return s.regionVersion(ctx, profile, code, &in, idem)
}

// RetireRegion ngưng nhóm vùng từ hôm nay — chặn khi còn xã báo giá đang hoặc sắp hoạt động thuộc nhóm.
func (s *Service) RetireRegion(ctx context.Context, profile, code, idem string) (WriteResult, error) {
	return s.regionVersion(ctx, profile, code, nil, idem)
}

func (s *Service) regionVersion(ctx context.Context, profile, code string, in *RegionInput, idem string) (WriteResult, error) {
	_, c, err := s.resolve(ctx, profile)
	if err != nil {
		return WriteResult{}, err
	}
	if !c.manage {
		return WriteResult{}, errNotPointAdmin
	}
	var res WriteResult
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		ps, err := loadPointSnap(ctx, tx)
		if err != nil {
			return err
		}
		day := domain.BusinessDay(s.Clock.Now())
		cur, ok := domain.EffectiveAt(ps.regions, day)[code]
		if !ok || cur.Status != domain.StatusActive {
			return port.ErrRegionNotFound.WithDetail("nhóm vùng " + code)
		}
		e, status := cur.V, domain.StatusActive
		e.ValidFrom = day
		if in == nil {
			status = domain.StatusRetired
			for pc, st := range ps.states(day) {
				if st.RegionCode == code && st.State != domain.PointStateRetired {
					return port.ErrRegionInUse.WithDetail("còn xã báo giá " + pc)
				}
			}
			for _, v := range ps.pvers {
				if v.ValidFrom.After(day) && v.Status == domain.StatusActive && v.V.RegionCode == code {
					return port.ErrRegionInUse.WithDetail("còn xã báo giá " + v.Key + " từ " + v.ValidFrom.Format(time.DateOnly))
				}
			}
		} else {
			e.Name = strings.TrimSpace(in.Name)
			if in.SortOrder > 0 {
				e.SortOrder = in.SortOrder
			}
			if err := domain.Validate(e); err != nil {
				return problem.Validation.WithDetail(err.Error())
			}
		}
		seq, err := store.InsertEntryVersion(ctx, tx, ids.New(), c.tenant, e, status, s.Clock.Now(), c.ref, idemKey(idem, status+":region:"+code))
		if err != nil {
			return classify(err)
		}
		res = WriteResult{Code: code, Version: seq, ValidFrom: day, Written: 1}
		return nil
	})
	return res, err
}

func (s *Service) dayOrToday(at time.Time) time.Time {
	if at.IsZero() {
		return domain.BusinessDay(s.Clock.Now())
	}
	return dayOf(at)
}

// ---------------------------------------------------------------------------------------------------------------------
// Xã báo giá
// ---------------------------------------------------------------------------------------------------------------------

// PointView là xã báo giá nhìn từ một ngày (màn danh sách FN-AD-04.02 B1).
type PointView struct {
	domain.PointState
	RegionName     string
	AdminUnitName  string   // tên ĐVHC hiện hành (phiên bản mới nhất)
	AdminUnitValid bool     // ĐVHC còn hiệu lực tại ngày — false: cần chuyển sang mã ĐVHC mới (sắp xếp ĐVHC)
	Successors     []string // mã ĐVHC kế thừa khi ĐVHC đã hết hiệu lực
	Officer        uuid.UUID
	OfficerSince   time.Time
	Owner          uuid.UUID
	OwnerSince     time.Time
	OfficerName    string // từ danh bạ (chỉ vai quản trị, Cấp Tỉnh)
	OwnerName      string
	ItemCount      int // số mặt hàng xã báo giá tại ngày (Cơ bản ∪ Địa phương đã gán)
}

// PointFilter lọc danh sách xã.
type PointFilter struct {
	At     time.Time
	Region string
	State  string // active | pending | retired; rỗng = mọi trạng thái
	Mine   bool   // Cấp Xã: xã mình phụ trách; Cấp Tỉnh: xã được phân công cho mình
}

// ListPoints đọc danh mục xã báo giá tại ngày (zero → hôm nay), sắp theo nhóm vùng, mã.
func (s *Service) ListPoints(ctx context.Context, profile string, f PointFilter) ([]PointView, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return nil, err
	}
	day := s.dayOrToday(f.At)
	names := s.officerNames(ctx, prof, c)
	out := []PointView{}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		ps, err := loadPointSnap(ctx, tx)
		if err != nil {
			return err
		}
		units, err := store.LoadAdminUnits(ctx, tx, "commune")
		if err != nil {
			return err
		}
		sn, err := loadSnap(ctx, tx)
		if err != nil {
			return err
		}
		regionOrder := map[string]int{}
		regionName := map[string]string{}
		for code, r := range domain.EffectiveAt(ps.regions, day) {
			regionOrder[code], regionName[code] = r.V.SortOrder, r.V.Name
		}
		items := domain.ItemStates(sn.vers, day)
		assigns := domain.EffectiveAt(sn.assigns, day)
		gate := domain.Gate(domain.EffectiveAt(sn.sets, day))
		order := sn.groupOrder(day)
		for _, st := range ps.states(day) {
			if (f.Region != "" && st.RegionCode != f.Region) || (f.State != "" && st.State != f.State) {
				continue
			}
			v := PointView{PointState: st, RegionName: regionName[st.RegionCode]}
			v.Officer, v.OfficerSince = domain.OfficerAt(ps.overs, domain.OfficerCommune, st.Code, day)
			v.Owner, v.OwnerSince = domain.OfficerAt(ps.overs, domain.OfficerProvince, st.Code, day)
			if f.Mine && !mine(c, v) {
				continue
			}
			v.OfficerName, v.OwnerName = names[v.Officer], names[v.Owner]
			if u, ok := communeAt(units, st.AdminUnitCode, day); ok {
				v.AdminUnitName, v.AdminUnitValid = u.Name, true
			} else if u, ok := latestUnit(units, st.AdminUnitCode); ok {
				v.AdminUnitName, v.Successors = u.Name, u.Successors
			}
			if st.State == domain.PointStateActive {
				v.ItemCount = len(domain.PointItems(items, assigns, gate, st.Code, order))
			}
			out = append(out, v)
		}
		sort.Slice(out, func(i, j int) bool {
			ri, rj := rank(regionOrder, out[i].RegionCode), rank(regionOrder, out[j].RegionCode)
			if ri != rj {
				return ri < rj
			}
			return out[i].Code < out[j].Code
		})
		return nil
	})
	return out, err
}

// mine: xã do người gọi phụ trách — Cấp Xã: cán bộ Xã phụ trách chính; Cấp Tỉnh: cán bộ được phân công.
func mine(c caller, v PointView) bool {
	switch c.level {
	case "XA":
		return v.Officer == c.ref
	case "TINH":
		return v.Owner == c.ref
	}
	return false
}

func rank(order map[string]int, code string) int {
	if r, ok := order[code]; ok {
		return r
	}
	return 1 << 30
}

// officerNames trả tên cán bộ từ danh bạ (vai quản trị, Cấp Tỉnh); lỗi danh bạ → không tên (không chặn danh sách).
func (s *Service) officerNames(ctx context.Context, prof port.Profile, c caller) map[uuid.UUID]string {
	out := map[uuid.UUID]string{}
	if s.Points.Officers == nil || (!c.manage && c.level != "TINH") {
		return out
	}
	for _, level := range []string{"XA", "TINH"} {
		os, err := s.Points.Officers.Officers(ctx, prof, level)
		if err != nil {
			return out
		}
		for _, o := range os {
			out[o.Ref] = o.Name
		}
	}
	return out
}

// PointHistory là một phiên bản xã báo giá (lịch sử thay đổi).
type PointHistory struct {
	domain.Point
	Version   int64
	EventTime time.Time
	ActorRef  uuid.UUID
}

// OfficerHistory là một phiên bản phân công (lịch sử phân công — AC-02).
type OfficerHistory struct {
	Role       string
	OfficerRef uuid.UUID
	Name       string
	ValidFrom  time.Time
	Version    int64
	EventTime  time.Time
	ActorRef   uuid.UUID
}

// PointDetail là một xã báo giá kèm lịch sử.
type PointDetail struct {
	PointView
	Versions []PointHistory
	Officers []OfficerHistory
}

// GetPoint đọc một xã báo giá tại ngày at và lịch sử thay đổi, lịch sử phân công.
func (s *Service) GetPoint(ctx context.Context, profile, code string, at time.Time) (PointDetail, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return PointDetail{}, err
	}
	all, err := s.ListPoints(ctx, profile, PointFilter{At: at})
	if err != nil {
		return PointDetail{}, err
	}
	var d PointDetail
	found := false
	for _, v := range all {
		if v.Code == code {
			d.PointView, found = v, true
		}
	}
	if !found {
		return PointDetail{}, port.ErrPointNotFound.WithDetail("xã báo giá " + code)
	}
	names := s.officerNames(ctx, prof, c)
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		ps, err := loadPointSnap(ctx, tx)
		if err != nil {
			return err
		}
		for _, v := range ps.points {
			if v.Key == code {
				d.Versions = append(d.Versions, PointHistory{Point: v.V, Version: v.Seq, EventTime: v.EventTime, ActorRef: v.ActorRef})
			}
		}
		for _, v := range ps.officers {
			if v.V.PointCode == code {
				d.Officers = append(d.Officers, OfficerHistory{Role: v.V.Role, OfficerRef: v.V.OfficerRef, Name: names[v.V.OfficerRef],
					ValidFrom: v.ValidFrom, Version: v.Seq, EventTime: v.EventTime, ActorRef: v.ActorRef})
			}
		}
		sort.SliceStable(d.Officers, func(i, j int) bool {
			if !d.Officers[i].ValidFrom.Equal(d.Officers[j].ValidFrom) {
				return d.Officers[i].ValidFrom.Before(d.Officers[j].ValidFrom)
			}
			return d.Officers[i].EventTime.Before(d.Officers[j].EventTime)
		})
		return nil
	})
	return d, err
}

// PointInput là nội dung thêm/sửa xã báo giá. Sửa: trường rỗng giữ nguyên.
type PointInput struct {
	AdminUnitCode string
	RegionCode    string
	ValidFrom     time.Time // zero → ngày báo giá kế tiếp
	Note          string    // lý do (sửa: chuyển ĐVHC/đổi nhóm vùng — tùy chọn; ngưng: bắt buộc)
}

// CreatePoint thêm xã báo giá (B2): chọn ĐVHC cấp xã có hiệu lực (tên xã theo ĐVHC), nhóm vùng, ngày bắt đầu báo giá.
// Mã XBG#### hệ thống sinh (BR-01). ĐVHC đã gắn với xã báo giá khác đang hoạt động → 409 (AC-02).
func (s *Service) CreatePoint(ctx context.Context, profile string, in PointInput, idem string) (WriteResult, error) {
	_, c, err := s.resolve(ctx, profile)
	if err != nil {
		return WriteResult{}, err
	}
	if !c.manage {
		return WriteResult{}, errNotPointAdmin
	}
	var res WriteResult
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		n, err := store.NextPointNumber(ctx, tx, c.tenant)
		if err != nil {
			return err
		}
		from, err := s.pointFrom(ctx, tx, in.ValidFrom)
		if err != nil {
			return err
		}
		ps, err := loadPointSnap(ctx, tx)
		if err != nil {
			return err
		}
		p := domain.Point{Code: domain.PointCode(n), AdminUnitCode: strings.TrimSpace(in.AdminUnitCode), RegionCode: in.RegionCode,
			Status: domain.StatusActive, ValidFrom: from, Note: strings.TrimSpace(in.Note)}
		if err := s.checkPoint(ctx, tx, ps, &p); err != nil {
			return err
		}
		seq, err := store.InsertPoint(ctx, tx, ids.New(), c.tenant, p, s.Clock.Now(), c.ref, idemKey(idem, "point:"+p.Code))
		if err != nil {
			return classify(err)
		}
		res = WriteResult{Code: p.Code, Version: seq, ValidFrom: from, Written: 1}
		return nil
	})
	return res, err
}

// checkPoint kiểm ĐVHC (cấp xã, có hiệu lực tại ngày hiệu lực — đặt tên xã theo ĐVHC), không trùng xã khác, nhóm vùng.
func (s *Service) checkPoint(ctx context.Context, tx pgx.Tx, ps *pointSnap, p *domain.Point) error {
	if !domain.ValidAdminUnitCode(p.AdminUnitCode) {
		return port.ErrAdminUnitInvalid.WithDetail("mã ĐVHC " + p.AdminUnitCode)
	}
	units, err := store.LoadAdminUnits(ctx, tx, "commune")
	if err != nil {
		return err
	}
	u, ok := communeAt(units, p.AdminUnitCode, p.ValidFrom)
	if !ok {
		return port.ErrAdminUnitInvalid.WithDetail("mã ĐVHC " + p.AdminUnitCode + " tại " + p.ValidFrom.Format(time.DateOnly))
	}
	p.Name = u.Name
	if !ps.regionActive(p.RegionCode, p.ValidFrom) {
		return port.ErrRegionNotFound.WithDetail("nhóm vùng " + p.RegionCode)
	}
	if other := domain.AdminUnitClash(ps.pvers, p.AdminUnitCode, p.Code, p.ValidFrom); other != "" {
		return port.ErrAdminUnitTaken.WithDetail("mã ĐVHC " + p.AdminUnitCode + " đang gắn với xã báo giá " + other)
	}
	if err := domain.ValidatePoint(*p); err != nil {
		return problem.Validation.WithDetail(err.Error())
	}
	return nil
}

// RevisePoint sửa xã báo giá từ ngày hiệu lực: đổi nhóm vùng và/hoặc chuyển sang mã ĐVHC mới khi sắp xếp ĐVHC (giữ mã
// xã và lịch sử — luồng phụ FN-AD-04.02). Tên xã cập nhật theo ĐVHC.
func (s *Service) RevisePoint(ctx context.Context, profile, code string, in PointInput, idem string) (WriteResult, error) {
	return s.pointVersion(ctx, profile, code, in, false, idem)
}

// RetirePoint ngưng xã báo giá từ ngày hiệu lực (B3), có lý do; xã không còn sinh kỳ báo giá. Hôm nay xã đã gửi phiếu
// → áp dụng từ ngày báo giá kế tiếp.
func (s *Service) RetirePoint(ctx context.Context, profile, code string, in PointInput, idem string) (WriteResult, error) {
	return s.pointVersion(ctx, profile, code, in, true, idem)
}

func (s *Service) pointVersion(ctx context.Context, profile, code string, in PointInput, retire bool, idem string) (WriteResult, error) {
	_, c, err := s.resolve(ctx, profile)
	if err != nil {
		return WriteResult{}, err
	}
	if !c.manage {
		return WriteResult{}, errNotPointAdmin
	}
	var res WriteResult
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		if err := store.LockPoints(ctx, tx, c.tenant); err != nil {
			return err
		}
		from := in.ValidFrom
		today := domain.BusinessDay(s.Clock.Now())
		if retire && !from.IsZero() && dayOf(from).Equal(today) && s.Points.Submitted != nil {
			sub, err := s.Points.Submitted(ctx, tx, today, code)
			if err != nil {
				return err
			}
			if sub {
				from = time.Time{} // → ngày báo giá kế tiếp
			}
		}
		from, err := s.pointFrom(ctx, tx, from)
		if err != nil {
			return err
		}
		ps, err := loadPointSnap(ctx, tx)
		if err != nil {
			return err
		}
		st, ok := ps.states(from)[code]
		if !ok {
			return port.ErrPointNotFound.WithDetail("xã báo giá " + code)
		}
		if st.State == domain.PointStateRetired {
			return port.ErrPointInactive.WithDetail("xã báo giá " + code + " đã ngưng tại " + from.Format(time.DateOnly))
		}
		p := st.Point
		p.ValidFrom, p.Note = from, strings.TrimSpace(in.Note)
		if retire {
			p.Status = domain.StatusRetired
			if p.Note == "" {
				return problem.Validation.WithDetail("ngưng xã báo giá phải có lý do")
			}
			if err := domain.ValidatePoint(p); err != nil {
				return problem.Validation.WithDetail(err.Error())
			}
		} else {
			if a := strings.TrimSpace(in.AdminUnitCode); a != "" {
				p.AdminUnitCode = a
			}
			if in.RegionCode != "" {
				p.RegionCode = in.RegionCode
			}
			if err := s.checkPoint(ctx, tx, ps, &p); err != nil {
				return err
			}
			if p.AdminUnitCode == st.AdminUnitCode && p.RegionCode == st.RegionCode && p.Name == st.Name {
				res = WriteResult{Code: code, Version: st.Version, ValidFrom: from}
				return nil // không đổi gì
			}
		}
		seq, err := store.InsertPoint(ctx, tx, ids.New(), c.tenant, p, s.Clock.Now(), c.ref, idemKey(idem, p.Status+":"+code))
		if err != nil {
			return classify(err)
		}
		res = WriteResult{Code: code, Version: seq, ValidFrom: from, Written: 1}
		return nil
	})
	return res, err
}

// AssignOfficer phân công người phụ trách xã báo giá từ ngày hiệu lực (FN-AD-04.01): role "xa" = cán bộ Xã phụ trách
// chính (thay người cũ từ ngày hiệu lực — lịch sử giữ, AC-02), "tinh" = cán bộ Cấp Tỉnh. ref Nil = bỏ phân công.
func (s *Service) AssignOfficer(ctx context.Context, profile, code, role string, ref uuid.UUID, validFrom time.Time, idem string) (WriteResult, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return WriteResult{}, err
	}
	if !c.manage {
		return WriteResult{}, errNotPointAdmin
	}
	if !domain.ValidOfficerRole(role) {
		return WriteResult{}, problem.Validation.WithDetail("vai phụ trách phải là xa | tinh")
	}
	if ref != uuid.Nil && s.Points.Officers != nil {
		level := "XA"
		if role == domain.OfficerProvince {
			level = "TINH"
		}
		os, err := s.Points.Officers.Officers(ctx, prof, level)
		if err != nil {
			return WriteResult{}, err
		}
		found := false
		for _, o := range os {
			found = found || o.Ref == ref
		}
		if !found {
			return WriteResult{}, port.ErrOfficerInvalid.WithDetail("cần cán bộ cấp " + level + " của phòng ban " + prof.Department)
		}
	}
	var res WriteResult
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		from, err := s.pointFrom(ctx, tx, validFrom)
		if err != nil {
			return err
		}
		ps, err := loadPointSnap(ctx, tx)
		if err != nil {
			return err
		}
		st, ok := ps.states(from)[code]
		if !ok {
			return port.ErrPointNotFound.WithDetail("xã báo giá " + code)
		}
		if st.State == domain.PointStateRetired {
			return port.ErrPointInactive.WithDetail("xã báo giá " + code + " đã ngưng tại " + from.Format(time.DateOnly))
		}
		res = WriteResult{Code: code, ValidFrom: from}
		cur, _ := domain.OfficerAt(ps.overs, role, code, from)
		if cur == ref {
			return nil // đã phân công như vậy
		}
		seq, err := store.InsertOfficer(ctx, tx, ids.New(), c.tenant, domain.OfficerAssign{PointCode: code, Role: role, OfficerRef: ref},
			from, s.Clock.Now(), c.ref, idemKey(idem, role+":"+code))
		if err != nil {
			return classify(err)
		}
		res.Version, res.Written = seq, 1
		return nil
	})
	return res, err
}

// AdminUnitView là một ĐVHC cấp xã có hiệu lực tại ngày (chọn khi thêm/sửa xã báo giá), kèm xã báo giá đang gắn.
type AdminUnitView struct {
	store.AdminUnit
	TakenBy string // mã xã báo giá đang gắn ĐVHC tại ngày (rỗng = chưa gắn)
}

// ListAdminUnits đọc ĐVHC cấp xã có hiệu lực tại ngày at (zero → hôm nay) từ danh mục ĐVHC dùng chung (k_ref.admin_unit).
// ĐVHC hết hiệu lực không có trong danh sách (luồng ngoại lệ: "ĐVHC hết hiệu lực → không chọn được").
func (s *Service) ListAdminUnits(ctx context.Context, profile string, at time.Time) ([]AdminUnitView, error) {
	_, c, err := s.resolve(ctx, profile)
	if err != nil {
		return nil, err
	}
	day := s.dayOrToday(at)
	out := []AdminUnitView{}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		units, err := store.LoadAdminUnits(ctx, tx, "commune")
		if err != nil {
			return err
		}
		ps, err := loadPointSnap(ctx, tx)
		if err != nil {
			return err
		}
		taken := map[string]string{}
		for code, st := range ps.states(day) {
			if st.State != domain.PointStateRetired {
				taken[st.AdminUnitCode] = code
			}
		}
		for _, u := range units {
			if u.EffectiveAt(day) {
				out = append(out, AdminUnitView{AdminUnit: u, TakenBy: taken[u.Code]})
			}
		}
		return nil
	})
	return out, err
}

// ListOfficers đọc danh bạ cán bộ cấp level (XA | TINH) để phân công — vai quản trị, Cấp Tỉnh (tên là dữ liệu cá nhân).
// ok = false: gốc lắp ráp chưa nối danh bạ.
func (s *Service) ListOfficers(ctx context.Context, profile, level string) ([]port.Officer, bool, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return nil, false, err
	}
	if !c.manage && c.level != "TINH" {
		return nil, false, problem.Forbidden.WithDetail("chỉ vai quản trị, Cấp Tỉnh xem danh bạ cán bộ")
	}
	if level != "XA" && level != "TINH" {
		return nil, false, problem.Validation.WithDetail("level phải là XA | TINH")
	}
	if s.Points.Officers == nil {
		return []port.Officer{}, false, nil
	}
	os, err := s.Points.Officers.Officers(ctx, prof, level)
	if err != nil {
		return nil, true, err
	}
	if os == nil {
		os = []port.Officer{}
	}
	return os, true, nil
}

// ---------------------------------------------------------------------------------------------------------------------
// port.Points, port.ReminderTargets
// ---------------------------------------------------------------------------------------------------------------------

var _ port.Points = (*Service)(nil)

// PointsAt (port.Points) đọc xã báo giá đang hoạt động tại ngày at cho tenant của ctx (không kiểm vai).
func (s *Service) PointsAt(ctx context.Context, at time.Time) ([]port.Point, error) {
	day := domain.BusinessDay(at)
	var out []port.Point
	err := s.ctxTx(ctx, func(tx pgx.Tx) error {
		ps, err := loadPointSnap(ctx, tx)
		if err != nil {
			return err
		}
		order := map[string]int{}
		for code, r := range domain.EffectiveAt(ps.regions, day) {
			order[code] = r.V.SortOrder
		}
		for _, st := range ps.states(day) {
			if st.State == domain.PointStateActive {
				out = append(out, ps.portPoint(st, day))
			}
		}
		sort.Slice(out, func(i, j int) bool {
			ri, rj := rank(order, out[i].RegionCode), rank(order, out[j].RegionCode)
			if ri != rj {
				return ri < rj
			}
			return out[i].Code < out[j].Code
		})
		return nil
	})
	return out, err
}

// PointAt (port.Points) đọc một xã báo giá tại ngày at; ok = false nếu không có hoặc không hoạt động.
func (s *Service) PointAt(ctx context.Context, code string, at time.Time) (port.Point, bool, error) {
	day := domain.BusinessDay(at)
	var (
		out port.Point
		ok  bool
	)
	err := s.ctxTx(ctx, func(tx pgx.Tx) error {
		ps, err := loadPointSnap(ctx, tx)
		if err != nil {
			return err
		}
		if st, has := ps.states(day)[code]; has && st.State == domain.PointStateActive {
			out, ok = ps.portPoint(st, day), true
		}
		return nil
	})
	return out, ok, err
}

// PointReminders cài port.ReminderTargets (W11.74 FN-XA-03.04) từ xã báo giá + phân công: mỗi xã đang hoạt động tại
// ngày có cán bộ Xã phụ trách → một nhóm người nhận (cán bộ đó); xã "Chưa có cán bộ Xã" không nhắc (FN-TI-02.01 luồng
// ngoại lệ). Lần 2 bỏ xã đã gửi phiếu nếu W11.75 nối PointHooks.Submitted (chưa nối → nhắc lại mọi xã).
type PointReminders struct{ S *Service }

var _ port.ReminderTargets = PointReminders{}

// Targets trả người nhận nhắc của tenant tid tại ngày day (job không có Principal — tenant truyền tường minh).
func (r PointReminders) Targets(ctx context.Context, tid uuid.UUID, _ string, day time.Time, round int) ([]port.ReminderTarget, error) {
	day = domain.BusinessDay(day)
	var out []port.ReminderTarget
	err := r.S.tx(ctx, tid, func(tx pgx.Tx) error {
		ps, err := loadPointSnap(ctx, tx)
		if err != nil {
			return err
		}
		states := ps.states(day)
		codes := make([]string, 0, len(states))
		for code, st := range states {
			if st.State == domain.PointStateActive {
				codes = append(codes, code)
			}
		}
		sort.Strings(codes)
		for _, code := range codes {
			off, _ := domain.OfficerAt(ps.overs, domain.OfficerCommune, code, day)
			if off == uuid.Nil {
				continue
			}
			if round >= 2 && r.S.Points.Submitted != nil {
				sub, err := r.S.Points.Submitted(ctx, tx, day, code)
				if err != nil {
					return err
				}
				if sub {
					continue
				}
			}
			out = append(out, port.ReminderTarget{CommuneCode: code, CommuneName: states[code].Name, RecipientRefs: []uuid.UUID{off}})
		}
		return nil
	})
	return out, err
}

// callerPoints trả mã các xã báo giá đang hoạt động tại ngày at mà người gọi Cấp Xã phụ trách chính.
func callerPoints(ps *pointSnap, ref uuid.UUID, at time.Time) []string {
	return domain.OfficerPoints(ps.overs, ps.states(at), domain.OfficerCommune, ref, at)
}
