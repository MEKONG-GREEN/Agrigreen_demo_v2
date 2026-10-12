package app

// Mặt hàng theo thời kỳ (W11.73) — nhóm hàng (tab "Nhóm hàng"), gán mặt hàng cho xã (tab "Gán cho xã"), bộ mặt hàng
// theo vụ (tab "Bộ theo vụ"), đề xuất mặt hàng từ xã (tab "Đề xuất từ xã", FN-XA-03.01). Xem item.go về quyền.

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

// ---------------------------------------------------------------------------------------------------------------------
// Nhóm hàng (B6): thêm, sửa tên/thứ tự, xóa (= ngưng) nhóm. Sổ pf_price.commodity_group của pf 000138. Thay đổi nhóm
// hàng có hiệu lực ngay (hôm nay) — tên nhóm không đổi danh sách mặt hàng của phiếu.
// ---------------------------------------------------------------------------------------------------------------------

// GroupView là nhóm hàng đang hiệu lực kèm số mặt hàng đang hiệu lực trong nhóm.
type GroupView struct {
	port.Group
	Items int
}

// ListGroups đọc nhóm hàng tại ngày at (zero → hôm nay).
func (s *ItemService) ListGroups(ctx context.Context, profile string, at time.Time) ([]GroupView, error) {
	_, c, err := s.resolve(ctx, profile)
	if err != nil {
		return nil, err
	}
	day := s.dayOr(at)
	out := []GroupView{}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		sn, err := loadSnap(ctx, tx)
		if err != nil {
			return err
		}
		count := map[string]int{}
		for _, st := range domain.ItemStates(sn.vers, day) {
			if st.State == domain.ItemStateActive {
				count[st.GroupCode]++
			}
		}
		for _, g := range domain.EffectiveAt(sn.groups, day) {
			if g.Status != domain.StatusActive {
				continue
			}
			out = append(out, GroupView{Group: port.Group{Code: g.V.Code, Name: g.V.Name, SortOrder: g.V.SortOrder, Version: g.Seq,
				ValidFrom: g.ValidFrom}, Items: count[g.V.Code]})
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

// GroupInput là nội dung nhóm hàng (Code rỗng khi thêm → hệ thống sinh nhom_<n>; SortOrder 0 khi thêm → cuối danh sách).
type GroupInput struct {
	Code      string
	Name      string
	SortOrder int
}

// CreateGroup thêm nhóm hàng có hiệu lực từ hôm nay.
func (s *ItemService) CreateGroup(ctx context.Context, profile string, in GroupInput, idem string) (WriteResult, error) {
	_, c, err := s.resolve(ctx, profile)
	if err != nil {
		return WriteResult{}, err
	}
	if !s.steward(ctx, c) {
		return WriteResult{}, errNotSteward
	}
	var res WriteResult
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		sn, err := loadSnap(ctx, tx)
		if err != nil {
			return err
		}
		have := map[string]bool{}
		maxSort := 0
		for _, g := range sn.groups {
			have[g.V.Code] = true
			if g.V.SortOrder > maxSort {
				maxSort = g.V.SortOrder
			}
		}
		code := in.Code
		if code == "" {
			for n := len(have) + 1; ; n++ {
				if code = fmt.Sprintf("nhom_%d", n); !have[code] {
					break
				}
			}
		} else if have[code] {
			return problem.Conflict.WithDetail("mã nhóm hàng " + code + " đã có (kể cả đã xóa) — mã không tái sử dụng")
		}
		sortOrder := in.SortOrder
		if sortOrder == 0 {
			sortOrder = maxSort + 10
		}
		e := domain.Entry{Kind: domain.KindGroup, Code: code, Name: strings.TrimSpace(in.Name), SortOrder: sortOrder, ValidFrom: s.today()}
		if err := domain.Validate(e); err != nil {
			return problem.Validation.WithDetail(err.Error())
		}
		seq, err := store.InsertGroupVersion(ctx, tx, ids.New(), c.tenant, e, domain.StatusActive, s.Clock.Now(), c.ref, idemKey(idem, "group:"+code))
		if err != nil {
			return classify(err)
		}
		res = WriteResult{Code: code, Version: seq, ValidFrom: e.ValidFrom, Written: 1}
		return nil
	})
	return res, err
}

// ReviseGroup sửa tên/thứ tự nhóm hàng (hiệu lực từ hôm nay; tên cũ giữ trong lịch sử).
func (s *ItemService) ReviseGroup(ctx context.Context, profile, code string, in GroupInput, idem string) (WriteResult, error) {
	return s.groupVersion(ctx, profile, code, &in, idem)
}

// RetireGroup xóa nhóm hàng (phiên bản 'retired' từ hôm nay) — chặn khi nhóm còn mặt hàng đang hoặc sắp hiệu lực.
func (s *ItemService) RetireGroup(ctx context.Context, profile, code, idem string) (WriteResult, error) {
	return s.groupVersion(ctx, profile, code, nil, idem)
}

func (s *ItemService) groupVersion(ctx context.Context, profile, code string, in *GroupInput, idem string) (WriteResult, error) {
	_, c, err := s.resolve(ctx, profile)
	if err != nil {
		return WriteResult{}, err
	}
	if !s.steward(ctx, c) {
		return WriteResult{}, errNotSteward
	}
	var res WriteResult
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		sn, err := loadSnap(ctx, tx)
		if err != nil {
			return err
		}
		day := s.today()
		cur, ok := domain.EffectiveAt(sn.groups, day)[code]
		if !ok || cur.Status != domain.StatusActive {
			return port.ErrGroupNotFound
		}
		e := cur.V
		e.ValidFrom = day
		status := domain.StatusActive
		if in == nil {
			status = domain.StatusRetired
			for _, it := range latestItems(sn.vers) {
				if it.V.GroupCode == code && it.Status == domain.StatusActive {
					return port.ErrGroupNotEmpty.WithDetail("còn mặt hàng " + it.V.Code)
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
		seq, err := store.InsertGroupVersion(ctx, tx, ids.New(), c.tenant, e, status, s.Clock.Now(), c.ref, idemKey(idem, status+":"+code))
		if err != nil {
			return classify(err)
		}
		res = WriteResult{Code: code, Version: seq, ValidFrom: day, Written: 1}
		return nil
	})
	return res, err
}

// latestItems trả phiên bản cuối (valid_from lớn nhất, rồi seq) của mỗi mặt hàng — trạng thái "về lâu dài".
func latestItems(vs []domain.Ver[domain.Item]) map[string]domain.Ver[domain.Item] {
	return domain.EffectiveAt(vs, time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC))
}

// ---------------------------------------------------------------------------------------------------------------------
// Gán mặt hàng Địa phương cho xã (B5). Mặt hàng Cơ bản không gán riêng (BR-03).
// ---------------------------------------------------------------------------------------------------------------------

// AssignmentView là một gán đang hiệu lực tại ngày.
type AssignmentView struct {
	PointCode string
	ItemCode  string
	Version   int64
	ValidFrom time.Time
}

// ListAssignments đọc các gán đang hiệu lực tại ngày at (zero → hôm nay), lọc theo xã nếu point khác rỗng.
func (s *ItemService) ListAssignments(ctx context.Context, profile, point string, at time.Time) ([]AssignmentView, error) {
	_, c, err := s.resolve(ctx, profile)
	if err != nil {
		return nil, err
	}
	day := s.dayOr(at)
	out := []AssignmentView{}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		as, err := store.LoadAssignments(ctx, tx)
		if err != nil {
			return err
		}
		for _, a := range domain.EffectiveAt(as, day) {
			if a.Status == domain.StatusActive && (point == "" || a.V.PointCode == point) {
				out = append(out, AssignmentView{PointCode: a.V.PointCode, ItemCode: a.V.ItemCode, Version: a.Seq, ValidFrom: a.ValidFrom})
			}
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].PointCode != out[j].PointCode {
				return out[i].PointCode < out[j].PointCode
			}
			return out[i].ItemCode < out[j].ItemCode
		})
		return nil
	})
	return out, err
}

// Assign gán các mặt hàng Địa phương cho xã point từ ngày validFrom (zero → ngày báo giá kế tiếp). Gán đã có bỏ qua.
func (s *ItemService) Assign(ctx context.Context, profile, point string, items []string, validFrom time.Time, idem string) (WriteResult, error) {
	return s.assignment(ctx, profile, point, items, validFrom, domain.StatusActive, idem)
}

// Unassign bỏ gán mặt hàng khỏi xã point từ ngày validFrom; hôm nay xã đã có phiếu → áp dụng từ ngày báo giá kế tiếp.
func (s *ItemService) Unassign(ctx context.Context, profile, point string, items []string, validFrom time.Time, idem string) (WriteResult, error) {
	return s.assignment(ctx, profile, point, items, validFrom, domain.StatusRetired, idem)
}

func (s *ItemService) assignment(ctx context.Context, profile, point string, items []string, validFrom time.Time, status, idem string) (WriteResult, error) {
	_, c, err := s.resolve(ctx, profile)
	if err != nil {
		return WriteResult{}, err
	}
	if !s.steward(ctx, c) {
		return WriteResult{}, errNotSteward
	}
	if !domain.ValidPointCode(point) {
		return WriteResult{}, problem.Validation.WithDetail("mã xã báo giá không hợp lệ")
	}
	if len(items) == 0 || len(items) > 500 {
		return WriteResult{}, problem.Validation.WithDetail("chọn 1–500 mặt hàng")
	}
	var res WriteResult
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		from := validFrom
		if status == domain.StatusRetired && !from.IsZero() && dayOf(from).Equal(s.today()) {
			has, err := s.hasSheets(ctx, tx, s.today(), point)
			if err != nil {
				return err
			}
			if has {
				from = time.Time{} // → ngày báo giá kế tiếp
			}
		}
		from, err := s.fromDay(ctx, tx, from, point)
		if err != nil {
			return err
		}
		if status == domain.StatusActive { // W11.72: gán cho xã báo giá có thật, đang hoạt động từ ngày hiệu lực
			if _, err := requirePoints(ctx, tx, []string{point}, from); err != nil {
				return err
			}
		} else if err := knownPoint(ctx, tx, point); err != nil {
			return err
		}
		sn, err := loadSnap(ctx, tx)
		if err != nil {
			return err
		}
		states := domain.ItemStates(sn.vers, from)
		assigns := domain.EffectiveAt(sn.assigns, from)
		now := s.Clock.Now()
		res = WriteResult{Code: point, ValidFrom: from}
		for _, code := range items {
			st, ok := states[code]
			if !ok {
				return port.ErrItemNotFound.WithDetail("mặt hàng " + code)
			}
			if st.Kind == domain.ItemBasic {
				return port.ErrItemBasicAssign.WithDetail("mặt hàng " + code)
			}
			if status == domain.StatusActive && st.State != domain.ItemStateActive {
				return port.ErrItemRetired.WithDetail("mặt hàng " + code + " không có hiệu lực tại " + from.Format(time.DateOnly))
			}
			cur, has := assigns[domain.AssignKey(point, code)]
			if (has && cur.Status == status) || (!has && status == domain.StatusRetired) {
				continue
			}
			if err := store.InsertAssignment(ctx, tx, ids.New(), c.tenant, point, code, status, from, now, c.ref,
				idemKey(idem, status+":"+point+":"+code)); err != nil {
				return classify(err)
			}
			res.Written++
		}
		return nil
	})
	return res, err
}

// PointItemList là danh sách mặt hàng của một xã tại một ngày (màn Nhập báo giá, W11.75).
type PointItemList struct {
	Day   time.Time
	Point string
	Items []domain.ItemState
}

// ListPointItems đọc mặt hàng xã point báo giá tại ngày at (zero → hôm nay).
func (s *ItemService) ListPointItems(ctx context.Context, profile, point string, at time.Time) (PointItemList, error) {
	_, c, err := s.resolve(ctx, profile)
	if err != nil {
		return PointItemList{}, err
	}
	if !domain.ValidPointCode(point) {
		return PointItemList{}, problem.Validation.WithDetail("mã xã báo giá không hợp lệ")
	}
	out := PointItemList{Day: s.dayOr(at), Point: point}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		if err := knownPoint(ctx, tx, point); err != nil { // W11.72
			return err
		}
		var e error
		out.Items, e = pointItems(ctx, tx, point, out.Day)
		return e
	})
	return out, err
}

// ---------------------------------------------------------------------------------------------------------------------
// Bộ mặt hàng theo vụ (BR-08).
// ---------------------------------------------------------------------------------------------------------------------

// SetView là bộ mặt hàng nhìn từ một ngày.
type SetView struct {
	domain.ItemSet
	Version   int64
	ValidFrom time.Time
	State     string // active | pending | retired
}

// ListSets đọc bộ mặt hàng tại ngày at (zero → hôm nay): bộ có hiệu lực, hoặc sắp hiệu lực.
func (s *ItemService) ListSets(ctx context.Context, profile string, at time.Time) ([]SetView, error) {
	_, c, err := s.resolve(ctx, profile)
	if err != nil {
		return nil, err
	}
	day := s.dayOr(at)
	out := []SetView{}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		sets, err := store.LoadSets(ctx, tx)
		if err != nil {
			return err
		}
		eff, up := domain.EffectiveAt(sets, day), domain.Upcoming(sets, day)
		for k, v := range up {
			if _, ok := eff[k]; !ok {
				out = append(out, SetView{ItemSet: v.V, Version: v.Seq, ValidFrom: v.ValidFrom, State: domain.ItemStatePending})
			}
		}
		for _, v := range eff {
			st := domain.ItemStateActive
			if v.Status == domain.StatusRetired {
				st = domain.ItemStateRetired
			}
			out = append(out, SetView{ItemSet: v.V, Version: v.Seq, ValidFrom: v.ValidFrom, State: st})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
		return nil
	})
	return out, err
}

// SetInput là nội dung bộ mặt hàng (ValidFrom zero → ngày báo giá kế tiếp; Retire → ngưng bộ, mặt hàng hết bị bộ chi phối).
type SetInput struct {
	Code, Name, Season string
	ItemCodes          []string
	Enabled            bool
	ValidFrom          time.Time
	Retire             bool
}

// CreateSet tạo bộ mặt hàng theo vụ.
func (s *ItemService) CreateSet(ctx context.Context, profile string, in SetInput, idem string) (WriteResult, error) {
	return s.setVersion(ctx, profile, in, true, idem)
}

// ReviseSet sửa bộ (tên, vụ, danh sách, bật/tắt, ngưng) — phiên bản mới từ ngày hiệu lực.
func (s *ItemService) ReviseSet(ctx context.Context, profile string, in SetInput, idem string) (WriteResult, error) {
	return s.setVersion(ctx, profile, in, false, idem)
}

func (s *ItemService) setVersion(ctx context.Context, profile string, in SetInput, create bool, idem string) (WriteResult, error) {
	_, c, err := s.resolve(ctx, profile)
	if err != nil {
		return WriteResult{}, err
	}
	if !s.steward(ctx, c) {
		return WriteResult{}, errNotSteward
	}
	if !domain.ValidSetCode(in.Code) {
		return WriteResult{}, problem.Validation.WithDetail("mã bộ mặt hàng: chữ thường, số, gạch dưới (≤ 40)")
	}
	in.Name, in.Season = strings.TrimSpace(in.Name), strings.TrimSpace(in.Season)
	if err := domain.ValidateText("tên bộ", in.Name, 1, 100); err != nil {
		return WriteResult{}, problem.Validation.WithDetail(err.Error())
	}
	if err := domain.ValidateText("vụ", in.Season, 1, 50); err != nil {
		return WriteResult{}, problem.Validation.WithDetail(err.Error())
	}
	codes := uniqueSorted(in.ItemCodes)
	if len(codes) == 0 || len(codes) > 500 {
		return WriteResult{}, problem.Validation.WithDetail("bộ có 1–500 mặt hàng")
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
		exists := false
		for _, v := range sn.sets {
			if v.Key == in.Code {
				exists = true
			}
		}
		if create && exists {
			return port.ErrItemSetCodeConflict
		}
		if !create && !exists {
			return port.ErrItemSetNotFound
		}
		known := latestItems(sn.vers)
		for _, code := range codes {
			if _, ok := known[code]; !ok {
				return port.ErrItemNotFound.WithDetail("mặt hàng " + code)
			}
		}
		status := domain.StatusActive
		if in.Retire {
			status = domain.StatusRetired
		}
		set := domain.ItemSet{Code: in.Code, Name: in.Name, Season: in.Season, ItemCodes: codes, Enabled: in.Enabled}
		seq, err := store.InsertSet(ctx, tx, ids.New(), c.tenant, set, status, from, s.Clock.Now(), c.ref, idemKey(idem, "set:"+in.Code))
		if err != nil {
			return classify(err)
		}
		res = WriteResult{Code: in.Code, Version: seq, ValidFrom: from, Written: 1}
		return nil
	})
	return res, err
}

func uniqueSorted(xs []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, x := range xs {
		if x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------------------------------------------------
// Đề xuất mặt hàng từ xã (FN-XA-03.01 luồng phụ, FN-AD-04.03 tab "Đề xuất từ xã").
// ---------------------------------------------------------------------------------------------------------------------

// ProposalView là trạng thái hiện tại của một đề xuất.
type ProposalView struct {
	store.ProposalVersion
	CreatedAt time.Time
	DecidedAt *time.Time
}

func latestProposals(vs []store.ProposalVersion) []ProposalView {
	by := map[uuid.UUID]*ProposalView{}
	var order []uuid.UUID
	for _, v := range vs {
		p, ok := by[v.ProposalID]
		if !ok {
			p = &ProposalView{CreatedAt: v.EventTime}
			by[v.ProposalID] = p
			order = append(order, v.ProposalID)
		}
		if v.Seq >= p.Seq {
			created := p.CreatedAt
			p.ProposalVersion = v
			p.CreatedAt = created
			if v.Status != domain.ProposalPending {
				t := v.EventTime
				p.DecidedAt = &t
			}
		}
	}
	out := make([]ProposalView, 0, len(order))
	for _, id := range order {
		out = append(out, *by[id])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// ProposalInput là đề xuất mặt hàng mới của Cấp Xã: tên, giống/phẩm cấp, nhóm hàng, đơn vị tính, quy cách, lý do.
type ProposalInput struct {
	PointCode, Name, Variety, GroupCode, UnitCode, Spec, Reason string
}

// CreateProposal ghi đề xuất (Cấp Xã, Cấp Tỉnh). Mặt hàng chỉ xuất hiện ở màn Nhập báo giá khi được duyệt và gán cho xã.
// W11.72: Cấp Xã chỉ đề xuất cho xã báo giá mình phụ trách chính hôm nay (point_code vắng + phụ trách đúng một xã → xã
// đó); Cấp Tỉnh đề xuất cho xã báo giá đang hoạt động.
func (s *ItemService) CreateProposal(ctx context.Context, profile string, in ProposalInput, idem string) (WriteResult, error) {
	_, c, err := s.resolve(ctx, profile)
	if err != nil {
		return WriteResult{}, err
	}
	if c.level != "XA" && c.level != "TINH" {
		return WriteResult{}, problem.Forbidden.WithDetail("chỉ Cấp Xã, Cấp Tỉnh đề xuất mặt hàng")
	}
	p := domain.Proposal{PointCode: in.PointCode, Name: strings.TrimSpace(in.Name), Variety: strings.TrimSpace(in.Variety),
		GroupCode: in.GroupCode, UnitCode: in.UnitCode, Spec: strings.TrimSpace(in.Spec), Reason: strings.TrimSpace(in.Reason),
		Status: domain.ProposalPending}
	if p.PointCode != "" && !domain.ValidPointCode(p.PointCode) {
		return WriteResult{}, problem.Validation.WithDetail("mã xã báo giá không hợp lệ")
	}
	probe := domain.Item{Name: p.Name, Variety: p.Variety, GroupCode: p.GroupCode, UnitCode: p.UnitCode, Spec: p.Spec, Kind: domain.ItemLocal,
		Status: domain.StatusActive, ValidFrom: s.today()}
	if err := domain.ValidateItem(probe); err != nil {
		return WriteResult{}, problem.Validation.WithDetail(err.Error())
	}
	if err := domain.ValidateText("lý do", p.Reason, 1, 500); err != nil {
		return WriteResult{}, problem.Validation.WithDetail(err.Error())
	}
	id := ids.New()
	var res WriteResult
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		if err := s.proposalPoint(ctx, tx, c, &p); err != nil {
			return err
		}
		seq, err := store.InsertProposal(ctx, tx, ids.New(), c.tenant, id, p, c.ref, s.Clock.Now(), c.ref, idemKey(idem, "proposal"))
		if err != nil {
			return classify(err)
		}
		res = WriteResult{Code: id.String(), Version: seq, ValidFrom: s.today(), Written: 1}
		return nil
	})
	return res, err
}

// ListProposals đọc đề xuất (status rỗng = mọi trạng thái), mới nhất trước. Cấp Xã chỉ thấy đề xuất của mình.
func (s *ItemService) ListProposals(ctx context.Context, profile, status string) ([]ProposalView, error) {
	_, c, err := s.resolve(ctx, profile)
	if err != nil {
		return nil, err
	}
	out := []ProposalView{}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		vs, err := store.LoadProposals(ctx, tx)
		if err != nil {
			return err
		}
		for _, p := range latestProposals(vs) {
			if (status == "" || p.Status == status) && (c.level != "XA" || p.ProposerRef == c.ref) {
				out = append(out, p)
			}
		}
		return nil
	})
	return out, err
}

func pendingProposal(ctx context.Context, tx pgx.Tx, id uuid.UUID) (ProposalView, error) {
	vs, err := store.LoadProposals(ctx, tx)
	if err != nil {
		return ProposalView{}, err
	}
	for _, p := range latestProposals(vs) {
		if p.ProposalID == id {
			if p.Status != domain.ProposalPending {
				return p, port.ErrProposalDecided
			}
			return p, nil
		}
	}
	return ProposalView{}, port.ErrProposalNotFound
}

// ApproveProposal duyệt đề xuất: tạo mặt hàng (nội dung in — client điền sẵn từ đề xuất, được chỉnh) và, nếu là mặt hàng
// Địa phương, gán cho xã đề xuất; mặt hàng xuất hiện ở xã từ ngày báo giá kế tiếp (AC-05).
func (s *ItemService) ApproveProposal(ctx context.Context, profile string, id uuid.UUID, in ItemInput, idem string) (WriteResult, error) {
	_, c, err := s.resolve(ctx, profile)
	if err != nil {
		return WriteResult{}, err
	}
	if !s.steward(ctx, c) {
		return WriteResult{}, errNotSteward
	}
	var res WriteResult
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		p, err := pendingProposal(ctx, tx, id)
		if err != nil {
			return err
		}
		if in.Kind == "" {
			in.Kind = domain.ItemLocal
		}
		if in.Kind == domain.ItemLocal && !containsStr(in.Points, p.PointCode) {
			in.Points = append(in.Points, p.PointCode)
		}
		in.ValidTo = time.Time{}
		res, err = s.createItemTx(ctx, tx, c, in, &id, idem)
		if err != nil {
			return err
		}
		dec := p.Proposal
		dec.Status, dec.ItemCode = domain.ProposalApproved, res.Code
		if _, err := store.InsertProposal(ctx, tx, ids.New(), c.tenant, id, dec, p.ProposerRef, s.Clock.Now(), c.ref,
			idemKey(idem, "approve")); err != nil {
			return classify(err)
		}
		res.Written++
		return nil
	})
	return res, err
}

// RejectProposal từ chối đề xuất kèm lý do (cán bộ Xã thấy lý do ở danh sách đề xuất của mình).
func (s *ItemService) RejectProposal(ctx context.Context, profile string, id uuid.UUID, note, idem string) (WriteResult, error) {
	_, c, err := s.resolve(ctx, profile)
	if err != nil {
		return WriteResult{}, err
	}
	if !s.steward(ctx, c) {
		return WriteResult{}, errNotSteward
	}
	note = strings.TrimSpace(note)
	if err := domain.ValidateText("lý do từ chối", note, 1, 500); err != nil {
		return WriteResult{}, problem.Validation.WithDetail(err.Error())
	}
	var res WriteResult
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		p, err := pendingProposal(ctx, tx, id)
		if err != nil {
			return err
		}
		dec := p.Proposal
		dec.Status, dec.DecisionNote = domain.ProposalRejected, note
		seq, err := store.InsertProposal(ctx, tx, ids.New(), c.tenant, id, dec, p.ProposerRef, s.Clock.Now(), c.ref, idemKey(idem, "reject"))
		if err != nil {
			return classify(err)
		}
		res = WriteResult{Code: id.String(), Version: seq, ValidFrom: s.today(), Written: 1}
		return nil
	})
	return res, err
}

func containsStr(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// proposalPoint đặt/kiểm xã của đề xuất theo phân công (W11.72).
func (s *ItemService) proposalPoint(ctx context.Context, tx pgx.Tx, c caller, p *domain.Proposal) error {
	today := s.today()
	if c.level == "XA" {
		ps, err := loadPointSnap(ctx, tx)
		if err != nil {
			return err
		}
		mine := callerPoints(ps, c.ref, today)
		if p.PointCode == "" && len(mine) == 1 {
			p.PointCode = mine[0]
		}
		if p.PointCode == "" || !containsStr(mine, p.PointCode) {
			return port.ErrPointNotAssigned.WithDetail("xã báo giá được phân công: " + strings.Join(mine, ", "))
		}
		return nil
	}
	if p.PointCode == "" {
		return problem.Validation.WithDetail("chọn xã báo giá đề xuất")
	}
	_, err := requirePoints(ctx, tx, []string{p.PointCode}, today)
	return err
}

// knownPoint kiểm mã là xã báo giá có trong sổ (mọi trạng thái).
func knownPoint(ctx context.Context, tx pgx.Tx, code string) error {
	vs, err := store.LoadPoints(ctx, tx)
	if err != nil {
		return err
	}
	for _, v := range vs {
		if v.Key == code {
			return nil
		}
	}
	return port.ErrPointNotFound.WithDetail("xã báo giá " + code)
}
