package app

// Mặt hàng theo thời kỳ (W11.73, pf 000141; BRD Báo giá nông sản v1.3 FN-AD-04.03, FN-XA-03.01 "Đề xuất mặt hàng"):
// mặt hàng có phiên bản + valid_from (mẫu của pf 000138), nhóm hàng, gán mặt hàng Địa phương cho xã, đề xuất từ xã, bộ
// mặt hàng theo vụ, "thay thế cho". Tệp này: ItemService, quyền, ngày hiệu lực, mặt hàng. item_catalog.go: nhóm hàng,
// gán xã, bộ theo vụ, đề xuất.
//
// Quyền (BR-06; cờ "Quản trị nghiệp vụ" của W11.72 nối qua ItemHooks.Steward):
//   - đọc: mọi cấp của profile (TW chỉ xem) và vai quản trị;
//   - thêm mặt hàng, sửa mặt hàng CHƯA có giá: Cấp Tỉnh, vai quản trị (Admin hỗ trợ — được ghi vết bằng actor_ref);
//   - sửa mặt hàng đã có giá, ngưng, đổi thứ tự, nhóm hàng, gán xã, bộ theo vụ, duyệt/từ chối đề xuất: Quản trị nghiệp vụ
//     (Cấp Tỉnh có cờ QTNV — mặc định mọi Cấp Tỉnh khi chưa nối Steward) và vai quản trị;
//   - đề xuất mặt hàng: Cấp Xã, Cấp Tỉnh.

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

// ItemHooks là điểm nối của các gói song song/kế tiếp (ADR-0049 mục 4). Trường nil → giá trị mặc định ghi bên cạnh.
type ItemHooks struct {
	// HasPrice: mặt hàng đã có giá (W11.75 — phiếu báo giá). nil → chưa có giá (khung chưa có phiếu).
	HasPrice func(ctx context.Context, tx pgx.Tx, itemCode string) (bool, error)
	// HasSheets: ngày day đã có phiếu gửi của xã point (point rỗng = bất kỳ xã) (W11.75). nil → chưa có.
	HasSheets func(ctx context.Context, tx pgx.Tx, day time.Time, point string) (bool, error)
	// NextDay: ngày báo giá kế tiếp sau today theo lịch báo giá (W11.74). nil → ngày hôm sau.
	NextDay func(ctx context.Context, tx pgx.Tx, today time.Time) (time.Time, error)
	// Steward: người gọi Cấp Tỉnh có quyền Quản trị nghiệp vụ báo giá (W11.72). nil → mọi Cấp Tỉnh (giả định).
	Steward func(ctx context.Context) bool
}

// ItemService cài đặt port.Items và thao tác HTTP của mặt hàng theo thời kỳ.
type ItemService struct {
	*Service
	Hooks ItemHooks
}

var _ port.Items = (*ItemService)(nil)

// NewItemService dựng ItemService trên Service của module.
func NewItemService(s *Service) *ItemService { return &ItemService{Service: s} }

func (s *ItemService) steward(ctx context.Context, c caller) bool {
	return c.manage || (c.level == "TINH" && (s.Hooks.Steward == nil || s.Hooks.Steward(ctx)))
}

func editor(c caller) bool { return c.manage || c.level == "TINH" }

var (
	errNotEditor  = problem.Forbidden.WithDetail("chỉ Cấp Tỉnh hoặc vai quản trị ghi được danh mục mặt hàng")
	errNotSteward = problem.Forbidden.WithDetail("chỉ Quản trị nghiệp vụ báo giá (hoặc vai quản trị) làm được thao tác này")
)

func (s *ItemService) hasPrice(ctx context.Context, tx pgx.Tx, code string) (bool, error) {
	if s.Hooks.HasPrice == nil {
		return false, nil
	}
	return s.Hooks.HasPrice(ctx, tx, code)
}

func (s *ItemService) hasSheets(ctx context.Context, tx pgx.Tx, day time.Time, point string) (bool, error) {
	if s.Hooks.HasSheets == nil {
		return false, nil
	}
	return s.Hooks.HasSheets(ctx, tx, day, point)
}

// today là ngày nghiệp vụ hiện tại (giờ VN).
func (s *ItemService) today() time.Time { return domain.BusinessDay(s.Clock.Now()) }

// nextDay là ngày báo giá kế tiếp (lịch W11.74; mặc định ngày mai) — ngày hiệu lực mặc định của mọi thay đổi (BR-02).
func (s *ItemService) nextDay(ctx context.Context, tx pgx.Tx) (time.Time, error) {
	t := s.today()
	if s.Hooks.NextDay == nil {
		return t.AddDate(0, 0, 1), nil
	}
	d, err := s.Hooks.NextDay(ctx, tx, t)
	if err != nil {
		return time.Time{}, err
	}
	return domain.BusinessDay(d), nil
}

// fromDay trả ngày hiệu lực của một thay đổi: d (zero → ngày báo giá kế tiếp), không trong quá khứ, không là hôm nay khi
// hôm nay đã có phiếu gửi (luồng ngoại lệ FN-AD-04.03).
func (s *ItemService) fromDay(ctx context.Context, tx pgx.Tx, d time.Time, point string) (time.Time, error) {
	if d.IsZero() {
		return s.nextDay(ctx, tx)
	}
	d = dayOf(d)
	t := s.today()
	if d.Before(t) {
		return time.Time{}, port.ErrItemDate.WithDetail("ngày hiệu lực " + d.Format(time.DateOnly) + " đã qua")
	}
	if d.Equal(t) {
		has, err := s.hasSheets(ctx, tx, t, point)
		if err != nil {
			return time.Time{}, err
		}
		if has {
			return time.Time{}, port.ErrItemDate.WithDetail("hôm nay đã có phiếu báo giá gửi — áp dụng từ ngày báo giá kế tiếp")
		}
	}
	return d, nil
}

// dayOf giữ phần ngày (theo lịch của t) dạng 00:00 UTC — ngày gửi lên từ HTTP đã là ngày lịch.
func dayOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// idemKey ghép khóa chống trùng của từng dòng sổ từ khóa của lời gọi (vắng → khóa ngẫu nhiên, mỗi lời gọi một lần ghi).
func idemKey(base, suffix string) string {
	if base == "" {
		base = ids.New().String()
	}
	return base + ":" + suffix
}

// snap là ảnh sổ mặt hàng của tenant trong một giao dịch.
type snap struct {
	items   []store.ItemVersion
	vers    []domain.Ver[domain.Item]
	assigns []domain.Ver[domain.Assignment]
	sets    []domain.Ver[domain.ItemSet]
	groups  []domain.Ver[domain.Entry]
}

func loadSnap(ctx context.Context, tx pgx.Tx) (*snap, error) {
	var (
		sn  snap
		err error
	)
	if sn.items, err = store.LoadItems(ctx, tx); err != nil {
		return nil, err
	}
	sn.vers = make([]domain.Ver[domain.Item], 0, len(sn.items))
	for _, v := range sn.items {
		sn.vers = append(sn.vers, v.Ver)
	}
	if sn.assigns, err = store.LoadAssignments(ctx, tx); err != nil {
		return nil, err
	}
	if sn.sets, err = store.LoadSets(ctx, tx); err != nil {
		return nil, err
	}
	if sn.groups, err = store.LoadGroups(ctx, tx); err != nil {
		return nil, err
	}
	return &sn, nil
}

// groupOrder trả thứ tự hiển thị của nhóm hàng đang hiệu lực tại ngày at.
func (sn *snap) groupOrder(at time.Time) map[string]int {
	out := map[string]int{}
	for code, g := range domain.EffectiveAt(sn.groups, at) {
		if g.Status == domain.StatusActive {
			out[code] = g.V.SortOrder
		}
	}
	return out
}

func (sn *snap) groupActive(code string, at time.Time) bool {
	g, ok := domain.EffectiveAt(sn.groups, at)[code]
	return ok && g.Status == domain.StatusActive
}

func unitActive(ctx context.Context, tx pgx.Tx, code string, at time.Time) (bool, error) {
	rs, err := store.Effective(ctx, tx, domain.KindUnit, at)
	if err != nil {
		return false, err
	}
	for _, r := range rs {
		if r.Code == code {
			return true, nil
		}
	}
	return false, nil
}

// ItemInput là nội dung mặt hàng khi thêm, sửa hoặc duyệt đề xuất.
type ItemInput struct {
	Name, Variety, GroupCode, UnitCode, Spec, SalePlace, Kind string
	// ValidFrom: ngày hiệu lực (zero → ngày báo giá kế tiếp). ValidTo: hiệu lực đến (zero → không thời hạn) — chỉ khi thêm.
	ValidFrom, ValidTo time.Time
	// ReplacesCode: "Thay thế cho" — chỉ khi thêm. Mặt hàng cũ ngưng từ ngày hiệu lực của mặt hàng mới.
	ReplacesCode string
	// Points: xã áp dụng mặt hàng Địa phương (gán từ ngày hiệu lực) — chỉ khi thêm.
	Points []string
}

func (in ItemInput) item(code string, sortOrder int, from time.Time) domain.Item {
	return domain.Item{Code: code, Name: strings.TrimSpace(in.Name), Variety: strings.TrimSpace(in.Variety), GroupCode: in.GroupCode,
		UnitCode: in.UnitCode, Spec: strings.TrimSpace(in.Spec), SalePlace: strings.TrimSpace(in.SalePlace), Kind: in.Kind,
		SortOrder: sortOrder, Status: domain.StatusActive, ValidFrom: from, ReplacesCode: in.ReplacesCode}
}

// WriteResult là kết quả một thao tác ghi danh mục.
type WriteResult struct {
	Code      string
	Version   int64
	ValidFrom time.Time
	Written   int
}

// ItemView là mặt hàng nhìn từ một ngày (màn List FN-AD-04.03).
type ItemView struct {
	domain.ItemState
	// PointCount: số xã đang gán tại ngày (mặt hàng Địa phương); -1 với mặt hàng Cơ bản (mọi xã đang hoạt động).
	PointCount int
	HasPrice   bool
	// NewPrice: nhãn "Giá mới" — mặt hàng thay thế chưa có giá.
	NewPrice   bool
	ProposalID *uuid.UUID
	// Gated: thuộc bộ theo vụ đang tắt tại ngày (không xuất hiện ở màn Nhập báo giá).
	Gated bool
}

// ItemFilter lọc danh sách mặt hàng (bộ lọc màn List).
type ItemFilter struct {
	At    time.Time // zero → hôm nay
	Group string
	Kind  string
	State string // active | pending | retired
	Q     string // tìm theo tên, giống/phẩm cấp, mã
}

// ItemList là danh sách mặt hàng tại một ngày kèm số đề xuất chờ xử lý (số đếm tab "Đề xuất từ xã").
type ItemList struct {
	Day              time.Time
	Items            []ItemView
	PendingProposals int
	CanEdit          bool
	CanSteward       bool
}

func (s *ItemService) dayOr(at time.Time) time.Time {
	if at.IsZero() {
		return s.today()
	}
	return dayOf(at)
}

// ListItems đọc danh mục mặt hàng tại ngày (mọi trạng thái), lọc, sắp theo nhóm, thứ tự, mã.
func (s *ItemService) ListItems(ctx context.Context, profile string, f ItemFilter) (ItemList, error) {
	_, c, err := s.resolve(ctx, profile)
	if err != nil {
		return ItemList{}, err
	}
	day := s.dayOr(f.At)
	out := ItemList{Day: day, Items: []ItemView{}, CanEdit: editor(c), CanSteward: s.steward(ctx, c)}
	q := strings.ToLower(strings.TrimSpace(f.Q))
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		sn, err := loadSnap(ctx, tx)
		if err != nil {
			return err
		}
		views, err := s.views(ctx, tx, sn, day)
		if err != nil {
			return err
		}
		for _, v := range views {
			if (f.Group != "" && v.GroupCode != f.Group) || (f.Kind != "" && v.Kind != f.Kind) || (f.State != "" && v.State != f.State) {
				continue
			}
			if q != "" && !strings.Contains(strings.ToLower(v.Code+" "+v.Name+" "+v.Variety), q) {
				continue
			}
			out.Items = append(out.Items, v)
		}
		ps, err := store.LoadProposals(ctx, tx)
		if err != nil {
			return err
		}
		for _, p := range latestProposals(ps) {
			if p.Status == domain.ProposalPending && (c.level != "XA" || p.ProposerRef == c.ref) {
				out.PendingProposals++
			}
		}
		return nil
	})
	return out, err
}

// views chiếu mọi mặt hàng tại ngày day thành ItemView đã sắp.
func (s *ItemService) views(ctx context.Context, tx pgx.Tx, sn *snap, day time.Time) ([]ItemView, error) {
	states := domain.ItemStates(sn.vers, day)
	assigns := domain.EffectiveAt(sn.assigns, day)
	gate := domain.Gate(domain.EffectiveAt(sn.sets, day))
	count := map[string]int{}
	for _, a := range assigns {
		if a.Status == domain.StatusActive {
			count[a.V.ItemCode]++
		}
	}
	proposal := map[string]*uuid.UUID{}
	for _, v := range sn.items {
		if v.ProposalID != nil {
			proposal[v.V.Code] = v.ProposalID
		}
	}
	list := make([]domain.ItemState, 0, len(states))
	for _, st := range states {
		list = append(list, st)
	}
	domain.SortItems(list, sn.groupOrder(day))
	out := make([]ItemView, 0, len(list))
	for _, st := range list {
		priced, err := s.hasPrice(ctx, tx, st.Code)
		if err != nil {
			return nil, err
		}
		v := ItemView{ItemState: st, PointCount: count[st.Code], HasPrice: priced, NewPrice: st.ReplacesCode != "" && !priced,
			ProposalID: proposal[st.Code], Gated: gate[st.Code]}
		if st.Kind == domain.ItemBasic {
			v.PointCount = -1
		}
		out = append(out, v)
	}
	return out, nil
}

// ItemDetail là một mặt hàng: trạng thái hôm nay, lịch sử phiên bản (lịch sử thay đổi, giữ lịch sử tên) và mặt hàng
// thay thế nó.
type ItemDetail struct {
	ItemView
	History    []store.ItemVersion
	ReplacedBy []string
}

// GetItem đọc một mặt hàng tại ngày at (zero → hôm nay).
func (s *ItemService) GetItem(ctx context.Context, profile, code string, at time.Time) (ItemDetail, error) {
	_, c, err := s.resolve(ctx, profile)
	if err != nil {
		return ItemDetail{}, err
	}
	day := s.dayOr(at)
	var out ItemDetail
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		sn, err := loadSnap(ctx, tx)
		if err != nil {
			return err
		}
		views, err := s.views(ctx, tx, sn, day)
		if err != nil {
			return err
		}
		found := false
		for _, v := range views {
			if v.Code == code {
				out.ItemView, found = v, true
			}
		}
		if !found {
			return port.ErrItemNotFound
		}
		by := map[string]bool{}
		for _, v := range sn.items {
			if v.V.Code == code {
				out.History = append(out.History, v)
			}
			if v.V.ReplacesCode == code {
				by[v.V.Code] = true
			}
		}
		for k := range by {
			out.ReplacedBy = append(out.ReplacedBy, k)
		}
		sort.Strings(out.ReplacedBy)
		return nil
	})
	return out, err
}

// CreateItem thêm mặt hàng (B2): mã tự sinh, hiệu lực từ ngày báo giá kế tiếp (mặc định), thứ tự cuối nhóm; mặt hàng Cơ
// bản áp dụng mọi xã, Địa phương gán cho các xã chọn. "Thay thế cho": mặt hàng cũ ngưng từ ngày hiệu lực của mặt hàng
// mới; mặt hàng Địa phương không chọn xã thì nhận các xã đang gán mặt hàng cũ.
func (s *ItemService) CreateItem(ctx context.Context, profile string, in ItemInput, idem string) (WriteResult, error) {
	_, c, err := s.resolve(ctx, profile)
	if err != nil {
		return WriteResult{}, err
	}
	if !editor(c) {
		return WriteResult{}, errNotEditor
	}
	var res WriteResult
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		var e error
		res, e = s.createItemTx(ctx, tx, c, in, nil, idem)
		return e
	})
	return res, err
}

func (s *ItemService) createItemTx(ctx context.Context, tx pgx.Tx, c caller, in ItemInput, proposal *uuid.UUID, idem string) (WriteResult, error) {
	from, err := s.fromDay(ctx, tx, in.ValidFrom, "")
	if err != nil {
		return WriteResult{}, err
	}
	sn, err := loadSnap(ctx, tx)
	if err != nil {
		return WriteResult{}, err
	}
	n, err := store.NextItemNumber(ctx, tx, c.tenant)
	if err != nil {
		return WriteResult{}, err
	}
	code := domain.ItemCode(n)
	maxSort := 0
	for _, v := range sn.vers {
		if v.V.GroupCode == in.GroupCode && v.V.SortOrder > maxSort {
			maxSort = v.V.SortOrder
		}
	}
	it := in.item(code, maxSort+10, from)
	if err := s.checkItem(ctx, tx, sn, it, ""); err != nil {
		return WriteResult{}, err
	}
	if !in.ValidTo.IsZero() && dayOf(in.ValidTo).Before(from) {
		return WriteResult{}, port.ErrItemDate.WithDetail("hiệu lực đến trước hiệu lực từ")
	}
	for _, p := range in.Points {
		if !domain.ValidPointCode(p) {
			return WriteResult{}, problem.Validation.WithDetail("mã xã báo giá không hợp lệ: " + p)
		}
	}
	if in.Kind == domain.ItemBasic && len(in.Points) > 0 {
		return WriteResult{}, port.ErrItemBasicAssign
	}
	now := s.Clock.Now()
	points := in.Points
	if in.ReplacesCode != "" {
		old, ok := domain.ItemStates(sn.vers, from)[in.ReplacesCode]
		if !ok {
			return WriteResult{}, port.ErrItemNotFound.WithDetail("mặt hàng được thay thế " + in.ReplacesCode + " không có")
		}
		if old.State == domain.ItemStateActive {
			ret := old.Item
			ret.Status, ret.ValidFrom, ret.Note = domain.StatusRetired, from, "Thay thế bởi "+code
			if _, err := store.InsertItem(ctx, tx, ids.New(), c.tenant, ret, nil, now, c.ref, idemKey(idem, "retire:"+old.Code)); err != nil {
				return WriteResult{}, classify(err)
			}
		}
		if in.Kind == domain.ItemLocal && len(points) == 0 {
			for _, a := range domain.EffectiveAt(sn.assigns, from) {
				if a.Status == domain.StatusActive && a.V.ItemCode == in.ReplacesCode {
					points = append(points, a.V.PointCode)
				}
			}
			sort.Strings(points)
		}
	}
	seq, err := store.InsertItem(ctx, tx, ids.New(), c.tenant, it, proposal, now, c.ref, idemKey(idem, "item:"+code))
	if err != nil {
		return WriteResult{}, classify(err)
	}
	written := 1
	if !in.ValidTo.IsZero() {
		ret := it
		ret.Status, ret.ValidFrom, ret.Note = domain.StatusRetired, dayOf(in.ValidTo).AddDate(0, 0, 1), "Hết hiệu lực theo kế hoạch"
		if _, err := store.InsertItem(ctx, tx, ids.New(), c.tenant, ret, proposal, now, c.ref, idemKey(idem, "until:"+code)); err != nil {
			return WriteResult{}, classify(err)
		}
		written++
	}
	seen := map[string]bool{}
	for _, p := range points {
		if seen[p] {
			continue
		}
		seen[p] = true
		if err := store.InsertAssignment(ctx, tx, ids.New(), c.tenant, p, code, domain.StatusActive, from, now, c.ref,
			idemKey(idem, "assign:"+p)); err != nil {
			return WriteResult{}, classify(err)
		}
		written++
	}
	return WriteResult{Code: code, Version: seq, ValidFrom: from, Written: written}, nil
}

// checkItem kiểm nội dung, nhóm hàng và đơn vị tính có hiệu lực tại ngày hiệu lực, chống trùng trong nhóm (bỏ qua self).
func (s *ItemService) checkItem(ctx context.Context, tx pgx.Tx, sn *snap, it domain.Item, self string) error {
	if err := domain.ValidateItem(it); err != nil {
		return problem.Validation.WithDetail(err.Error())
	}
	if !sn.groupActive(it.GroupCode, it.ValidFrom) {
		return problem.Validation.WithDetail("nhóm hàng " + it.GroupCode + " không có hiệu lực tại " + it.ValidFrom.Format(time.DateOnly))
	}
	ok, err := unitActive(ctx, tx, it.UnitCode, it.ValidFrom)
	if err != nil {
		return err
	}
	if !ok {
		return problem.Validation.WithDetail("đơn vị tính " + it.UnitCode + " không có hiệu lực tại " + it.ValidFrom.Format(time.DateOnly))
	}
	key := domain.IdentityKey(it)
	for code, st := range domain.ItemStates(sn.vers, it.ValidFrom) {
		if code != self && code != it.ReplacesCode && st.State != domain.ItemStateRetired && domain.IdentityKey(st.Item) == key {
			return port.ErrItemDuplicate.WithDetail("trùng mặt hàng " + code)
		}
	}
	return nil
}

// ReviseItem sửa mặt hàng (B3) — phiên bản mới từ ngày hiệu lực (mặc định ngày báo giá kế tiếp), giữ thứ tự và "thay thế
// cho". Mặt hàng đã có giá: chỉ sửa tên hiển thị, nơi bán (BR-04), và chỉ Quản trị nghiệp vụ (BR-06).
func (s *ItemService) ReviseItem(ctx context.Context, profile, code string, in ItemInput, idem string) (WriteResult, error) {
	_, c, err := s.resolve(ctx, profile)
	if err != nil {
		return WriteResult{}, err
	}
	if !editor(c) {
		return WriteResult{}, errNotEditor
	}
	var res WriteResult
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		from, err := s.fromDay(ctx, tx, in.ValidFrom, "")
		if err != nil {
			return err
		}
		sn, err := loadSnap(ctx, tx)
		if err != nil {
			return err
		}
		cur, err := activeAt(sn, code, from)
		if err != nil {
			return err
		}
		priced, err := s.hasPrice(ctx, tx, code)
		if err != nil {
			return err
		}
		it := in.item(code, cur.SortOrder, from)
		it.ReplacesCode = cur.ReplacesCode
		if priced {
			if !s.steward(ctx, c) {
				return errNotSteward
			}
			if it.Variety != cur.Variety || it.GroupCode != cur.GroupCode || it.UnitCode != cur.UnitCode || it.Spec != cur.Spec || it.Kind != cur.Kind {
				return port.ErrItemLocked
			}
		}
		if err := s.checkItem(ctx, tx, sn, it, code); err != nil {
			return err
		}
		seq, err := store.InsertItem(ctx, tx, ids.New(), c.tenant, it, nil, s.Clock.Now(), c.ref, idemKey(idem, "revise:"+code))
		if err != nil {
			return classify(err)
		}
		res = WriteResult{Code: code, Version: seq, ValidFrom: from, Written: 1}
		return nil
	})
	return res, err
}

// activeAt trả mặt hàng có hiệu lực (active) tại ngày; không có → item_not_found; đã ngưng → item_retired; chưa tới ngày
// hiệu lực → item_date_invalid.
func activeAt(sn *snap, code string, at time.Time) (domain.ItemState, error) {
	st, ok := domain.ItemStates(sn.vers, at)[code]
	switch {
	case !ok:
		return st, port.ErrItemNotFound
	case st.State == domain.ItemStateRetired:
		return st, port.ErrItemRetired
	case st.State == domain.ItemStatePending:
		return st, port.ErrItemDate.WithDetail("mặt hàng chưa có hiệu lực tại " + at.Format(time.DateOnly))
	}
	return st, nil
}

// RetireItem ngưng mặt hàng (B4): hiệu lực đến validTo (zero → đến hết ngày trước ngày báo giá kế tiếp), có lý do.
// Báo cáo các ngày trước vẫn hiển thị mặt hàng (AC-02).
func (s *ItemService) RetireItem(ctx context.Context, profile, code string, validTo time.Time, reason, idem string) (WriteResult, error) {
	_, c, err := s.resolve(ctx, profile)
	if err != nil {
		return WriteResult{}, err
	}
	if !s.steward(ctx, c) {
		return WriteResult{}, errNotSteward
	}
	reason = strings.TrimSpace(reason)
	if err := domain.ValidateText("lý do ngưng", reason, 1, 500); err != nil {
		return WriteResult{}, problem.Validation.WithDetail(err.Error())
	}
	var res WriteResult
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		var from time.Time
		if validTo.IsZero() {
			from, err = s.fromDay(ctx, tx, time.Time{}, "")
		} else {
			from, err = s.fromDay(ctx, tx, dayOf(validTo).AddDate(0, 0, 1), "")
		}
		if err != nil {
			return err
		}
		sn, err := loadSnap(ctx, tx)
		if err != nil {
			return err
		}
		cur, err := activeAt(sn, code, from)
		if err != nil {
			return err
		}
		it := cur.Item
		it.Status, it.ValidFrom, it.Note = domain.StatusRetired, from, reason
		seq, err := store.InsertItem(ctx, tx, ids.New(), c.tenant, it, nil, s.Clock.Now(), c.ref, idemKey(idem, "retire:"+code))
		if err != nil {
			return classify(err)
		}
		res = WriteResult{Code: code, Version: seq, ValidFrom: from, Written: 1}
		return nil
	})
	return res, err
}

// MoveItem đổi thứ tự hiển thị (lên/xuống) trong nhóm từ ngày báo giá kế tiếp: đánh lại thứ tự cả nhóm 10, 20, … và ghi
// phiên bản mới cho mặt hàng đổi thứ tự.
func (s *ItemService) MoveItem(ctx context.Context, profile, code, dir, idem string) (WriteResult, error) {
	_, c, err := s.resolve(ctx, profile)
	if err != nil {
		return WriteResult{}, err
	}
	if !s.steward(ctx, c) {
		return WriteResult{}, errNotSteward
	}
	if dir != "up" && dir != "down" {
		return WriteResult{}, problem.Validation.WithDetail("direction: up | down")
	}
	var res WriteResult
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		from, err := s.nextDay(ctx, tx)
		if err != nil {
			return err
		}
		sn, err := loadSnap(ctx, tx)
		if err != nil {
			return err
		}
		cur, err := activeAt(sn, code, from)
		if err != nil {
			return err
		}
		var peers []domain.ItemState
		for _, st := range domain.ItemStates(sn.vers, from) {
			if st.State == domain.ItemStateActive && st.GroupCode == cur.GroupCode {
				peers = append(peers, st)
			}
		}
		domain.SortItems(peers, nil)
		i := 0
		for k, p := range peers {
			if p.Code == code {
				i = k
			}
		}
		j := i - 1
		if dir == "down" {
			j = i + 1
		}
		res = WriteResult{Code: code, ValidFrom: from}
		if j < 0 || j >= len(peers) {
			return nil // đã ở đầu/cuối nhóm
		}
		peers[i], peers[j] = peers[j], peers[i]
		now := s.Clock.Now()
		for k, p := range peers {
			want := (k + 1) * 10
			if p.SortOrder == want {
				continue
			}
			it := p.Item
			it.SortOrder, it.ValidFrom = want, from
			seq, err := store.InsertItem(ctx, tx, ids.New(), c.tenant, it, nil, now, c.ref, idemKey(idem, fmt.Sprintf("move:%s", p.Code)))
			if err != nil {
				return classify(err)
			}
			if p.Code == code {
				res.Version = seq
			}
			res.Written++
		}
		return nil
	})
	return res, err
}

// ItemsAt (port.Items) đọc mặt hàng có hiệu lực tại ngày at cho tenant của ctx (không kiểm vai — bên gọi là module đã kiểm).
func (s *ItemService) ItemsAt(ctx context.Context, at time.Time) ([]port.Item, error) {
	day := domain.BusinessDay(at)
	var out []port.Item
	err := s.ctxTx(ctx, func(tx pgx.Tx) error {
		sn, err := loadSnap(ctx, tx)
		if err != nil {
			return err
		}
		var list []domain.ItemState
		for _, st := range domain.ItemStates(sn.vers, day) {
			if st.State == domain.ItemStateActive {
				list = append(list, st)
			}
		}
		domain.SortItems(list, sn.groupOrder(day))
		out = toPortItems(list)
		return nil
	})
	return out, err
}

// PointItems (port.Items) đọc mặt hàng xã point báo giá tại ngày at cho tenant của ctx.
func (s *ItemService) PointItems(ctx context.Context, point string, at time.Time) ([]port.Item, error) {
	day := domain.BusinessDay(at)
	var out []port.Item
	err := s.ctxTx(ctx, func(tx pgx.Tx) error {
		list, err := pointItems(ctx, tx, point, day)
		out = toPortItems(list)
		return err
	})
	return out, err
}

func pointItems(ctx context.Context, tx pgx.Tx, point string, day time.Time) ([]domain.ItemState, error) {
	sn, err := loadSnap(ctx, tx)
	if err != nil {
		return nil, err
	}
	return domain.PointItems(domain.ItemStates(sn.vers, day), domain.EffectiveAt(sn.assigns, day),
		domain.Gate(domain.EffectiveAt(sn.sets, day)), point, sn.groupOrder(day)), nil
}

func toPortItems(list []domain.ItemState) []port.Item {
	out := make([]port.Item, 0, len(list))
	for _, st := range list {
		out = append(out, port.Item{Code: st.Code, Name: st.Name, Variety: st.Variety, GroupCode: st.GroupCode, UnitCode: st.UnitCode,
			Spec: st.Spec, SalePlace: st.SalePlace, Kind: st.Kind, SortOrder: st.SortOrder, Version: st.Version, ValidFrom: st.ValidFrom,
			ReplacesCode: st.ReplacesCode})
	}
	return out
}

// NextQuoteDay là ngày báo giá kế tiếp sau today theo lịch báo giá W11.74 (bên dùng đầu tiên; ngày nghỉ, ngày bổ sung),
// đọc trong giao dịch tx của thao tác danh mục. Không có kỳ báo giá trong 60 ngày tới → ngày hôm sau. Dùng làm
// ItemHooks.NextDay (Module.WireItemCalendar).
func (s *Service) NextQuoteDay(ctx context.Context, tx pgx.Tx, today time.Time) (time.Time, error) {
	next := domain.BusinessDay(today).AddDate(0, 0, 1)
	prof, ok := s.calendarProfile("")
	if !ok {
		return next, nil
	}
	st, err := s.calendarState(ctx, tx, prof)
	if err != nil {
		return time.Time{}, err
	}
	ps, err := s.planRange(ctx, tx, prof.Name, st, next, 60)
	if err != nil {
		return time.Time{}, err
	}
	for _, p := range ps {
		if p.Quote {
			return p.Day, nil
		}
	}
	return next, nil
}
