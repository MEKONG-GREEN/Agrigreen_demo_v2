package app

// BF-CN-PUBLISH (#108 — CN-07, FN-07, UR-TINH-08, UR-XA-13): cấu hình phát hành biểu kỳ CNTS và màn theo dõi.
//   - Đối tượng: nhóm xã (tất cả xã của tỉnh / theo khu vực / xã có nuôi thủy sản / xã có mô hình / chọn từng xã) hoặc
//     cơ sở giết mổ (BM-01). Nhóm xã giải ở máy chủ: khu vực từ pf/party (như phát hành TT — FN-TI-02.02 B2), xã có mô
//     hình = xã có điểm trình diễn CNTS đang hoạt động, xã có nuôi thủy sản = xã có điểm ghi nhật ký thủy sản BM-19
//     (extension/cntsmodel). Biểu kỳ hiện không có biểu theo hộ — nhật ký hộ mô hình ở extension/cntsdiary.
//   - Cán bộ tỉnh phụ trách (kiểm: lần gán Cấp Tỉnh còn hiệu lực cùng phòng, cùng Nút), giờ chốt (hạn + N giờ) ghi vào
//     sổ cnts_publication cùng các kỳ native đã sinh; cảnh báo trễ liên tiếp gửi người phụ trách của kỳ.
//   - Nháp của cán bộ xã (cnts_period_draft) để màn theo dõi có trạng thái Nháp.

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/store"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Đối tượng phát hành biểu kỳ CNTS (CN-07).
const (
	CNTSTargetCommunes    = "communes"    // chọn từng xã
	CNTSTargetProvince    = "province"    // tất cả xã của tỉnh
	CNTSTargetRegions     = "regions"     // theo khu vực
	CNTSTargetAquaculture = "aquaculture" // xã có nuôi thủy sản
	CNTSTargetModel       = "model"       // xã có mô hình
	CNTSTargetFacilities  = "facilities"  // cơ sở giết mổ (BM-01)
)

// CNTSTargets là các đối tượng hợp lệ.
var CNTSTargets = []string{CNTSTargetCommunes, CNTSTargetProvince, CNTSTargetRegions, CNTSTargetAquaculture, CNTSTargetModel, CNTSTargetFacilities}

// maxCloseAfterHours: giờ chốt muộn nhất sau hạn nộp (30 ngày).
const maxCloseAfterHours = 720

func dedupStrings(xs []string) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		x = strings.TrimSpace(x)
		if x != "" && !slices.Contains(out, x) {
			out = append(out, x)
		}
	}
	return out
}

// cntsTarget giải đối tượng của lô thành danh sách đơn vị nhận (sr.Units) và kiểm người phụ trách, giờ chốt.
func (s *Service) cntsTarget(ctx context.Context, in CNTSSeriesInput, sr *CNTSSeries) error {
	f := sr.Form
	target := strings.TrimSpace(in.Target)
	if target == "" {
		target = CNTSTargetCommunes
		if f.UnitKind == domain.UnitFacility {
			target = CNTSTargetFacilities
		}
	}
	if !slices.Contains(CNTSTargets, target) {
		return problem.Validation.WithField("target", "communes | province | regions | aquaculture | model | facilities")
	}
	if (f.UnitKind == domain.UnitFacility) != (target == CNTSTargetFacilities) {
		if f.UnitKind == domain.UnitFacility {
			return problem.Validation.WithField("target", f.BM+" nhập theo cơ sở giết mổ — đối tượng là cơ sở")
		}
		return problem.Validation.WithField("target", "đối tượng cơ sở giết mổ chỉ dùng với BM-01")
	}
	regions := dedupStrings(in.Regions)
	if target == CNTSTargetRegions && len(regions) == 0 {
		return problem.Validation.WithField("regions", "chọn ít nhất một khu vực")
	}
	if target != CNTSTargetRegions && len(regions) > 0 {
		return problem.Validation.WithField("regions", "chỉ dùng với đối tượng theo khu vực")
	}
	if in.CloseAfterHours != nil && (*in.CloseAfterHours < 0 || *in.CloseAfterHours > maxCloseAfterHours) {
		return problem.Validation.WithField("close_after_hours", "0–720 giờ sau hạn nộp")
	}
	sr.Target, sr.Regions = target, regions
	a, err := s.knActor(ctx)
	if err != nil {
		return err
	}
	// Xã của tỉnh (kèm khu vực): cần cho nhóm xã; có thì trả về để màn chọn khu vực / xã và kiểm xã chọn tay.
	var all map[string]partyport.CommuneRegion
	if f.UnitKind == domain.UnitCommune && s.Regions != nil && a.province != "" {
		if all, err = s.Regions.CommuneRegions(ctx, a.province); err != nil {
			return err
		}
		for _, c := range all {
			sr.Communes = append(sr.Communes, c)
		}
		sort.Slice(sr.Communes, func(i, j int) bool {
			x, y := sr.Communes[i], sr.Communes[j]
			if x.SortOrder != y.SortOrder {
				return x.SortOrder < y.SortOrder
			}
			return x.CommuneCode < y.CommuneCode
		})
	}
	switch target {
	case CNTSTargetCommunes, CNTSTargetFacilities:
		sr.Units = dedupStrings(in.Units)
		if all != nil && target == CNTSTargetCommunes {
			for _, u := range sr.Units {
				if _, ok := all[u]; !ok {
					return ErrNoCommunes.WithField("units", u+": không phải xã đang hoạt động của tỉnh")
				}
			}
		}
	default:
		if all == nil {
			if s.Regions == nil {
				return ErrDependency.WithDetail("chưa nối khu vực (pf/party) — chọn từng xã")
			}
			return ErrOutOfScope.WithDetail("ngữ cảnh phiên không gắn tỉnh — chọn từng xã")
		}
		var pick func(partyport.CommuneRegion) bool
		switch target {
		case CNTSTargetProvince:
			pick = func(partyport.CommuneRegion) bool { return true }
		case CNTSTargetRegions:
			pick = func(c partyport.CommuneRegion) bool { return slices.Contains(regions, c.RegionCode) }
		default:
			var withSites []string
			if err := s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
				var e error
				withSites, e = store.CNTSSiteCommunes(ctx, tx, target == CNTSTargetAquaculture)
				return e
			}); err != nil {
				return err
			}
			pick = func(c partyport.CommuneRegion) bool { return slices.Contains(withSites, c.CommuneCode) }
		}
		for _, c := range sr.Communes {
			if pick(c) {
				sr.Units = append(sr.Units, c.CommuneCode)
			}
		}
		if len(sr.Units) == 0 {
			return ErrNoCommunes.WithField("target", "nhóm đã chọn không có xã nào")
		}
	}
	return s.checkCNTSOwner(ctx, a, in.OwnerRef)
}

// checkCNTSOwner: người phụ trách phải có lần gán Cấp Tỉnh còn hiệu lực của phòng, cùng Nút với người phát hành.
func (s *Service) checkCNTSOwner(ctx context.Context, a actor, owner *uuid.UUID) error {
	if owner == nil {
		return nil
	}
	if *owner == uuid.Nil {
		return problem.Validation.WithField("owner_ref", "chọn cán bộ phụ trách")
	}
	if s.Assignments == nil {
		return ErrDependency.WithDetail("chưa nối gán vai trò (pf/accounts)")
	}
	asg, err := s.Assignments.ActiveAssignments(ctx, port.SessionRoleProvince, domain.DeptChanNuoiThuySan)
	if err != nil {
		return err
	}
	now := s.now()
	for _, x := range asg {
		if x.UserRef == *owner && x.EffectiveAt(now) && (a.k.OrgRef == uuid.Nil || x.OrgRef == a.k.OrgRef) {
			return nil
		}
	}
	return problem.Validation.WithField("owner_ref", "không phải cán bộ Cấp Tỉnh Phòng Chăn nuôi – Thủy sản của đơn vị")
}

// recordCNTSPublication ghi lần phát hành sau khi cap/forms đã phát hành lô (idem trùng → đọc lại lần đã ghi).
func (s *Service) recordCNTSPublication(ctx context.Context, in CNTSSeriesInput, sr *CNTSSeries, idem string) error {
	a, err := s.knActor(ctx)
	if err != nil {
		return err
	}
	onBehalf, support, err := a.supportInfo()
	if err != nil {
		return err
	}
	dl := sr.Form.Deadline
	if in.Deadline != nil {
		dl = *in.Deadline
	}
	p := store.CNTSPublication{ID: ids.New(), Department: domain.DeptChanNuoiThuySan, TemplateCode: sr.Form.Code, OwnerRef: sr.OwnerRef,
		TargetMode: sr.Target, Regions: sr.Regions, UnitKind: sr.Form.UnitKind, Units: sr.Units, DeadlineOffset: dl.OffsetDays,
		DeadlineTime: dl.Time, CloseAfterHours: sr.CloseAfterHours, TaskCount: sr.Tasks, EventTime: s.now(), ActorRef: a.p.ActorRef,
		OnBehalfOf: onBehalf, SupportReason: support, IdemKey: idemOr(idem, "cnts-publication")}
	if a.k.OrgRef != uuid.Nil {
		org := a.k.OrgRef
		p.OrgRef = &org
	}
	if p.From, err = domain.ParseDate(in.From); err != nil {
		return problem.Validation.WithField("from_date", err.Error())
	}
	if p.To, err = domain.ParseDate(in.To); err != nil {
		return problem.Validation.WithField("to_date", err.Error())
	}
	for _, pp := range sr.Published {
		p.PeriodRefs = append(p.PeriodRefs, pp.ID)
	}
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		if got, err := store.CNTSPublicationByIdem(ctx, tx, p.IdemKey); err != nil || got != nil {
			if got != nil {
				p.ID = got.ID
			}
			return err
		}
		return store.InsertCNTSPublication(ctx, tx, a.p.TenantID, p)
	})
	if errors.Is(ledger.Classify(err), ledger.ErrDuplicate) {
		err = nil
	}
	if err == nil {
		id := p.ID
		sr.PublicationID = &id
	}
	return err
}

// CNTSTrackedPeriod là phần KN của một kỳ native trên màn theo dõi: lần phát hành, người phụ trách, giờ chốt.
type CNTSTrackedPeriod struct {
	PeriodRef     uuid.UUID
	PublicationID uuid.UUID
	TemplateCode  string
	Target        string
	Regions       []string
	OwnerRef      *uuid.UUID
	OwnerName     string
	// CloseAfterHours: giờ chốt = hạn nộp của kỳ + số giờ này (nil = không đặt giờ chốt — Cấp Tỉnh tự chốt).
	CloseAfterHours *int
}

// CNTSDraftMark là dấu nháp của một đơn vị (không kèm số liệu).
type CNTSDraftMark struct {
	PeriodRef uuid.UUID
	Unit      string
	SavedAt   time.Time
}

// CNTSTracking là phần KN của màn theo dõi nộp.
type CNTSTracking struct {
	Periods []CNTSTrackedPeriod
	Drafts  []CNTSDraftMark
}

// trackingWindow: màn theo dõi đọc các lần phát hành trong 400 ngày gần nhất (tối đa 200 lần).
const (
	trackingWindow = 400 * 24 * time.Hour
	trackingLimit  = 200
)

// CNTSTracking đọc người phụ trách, giờ chốt của các kỳ đã phát hành (biểu code, rỗng = mọi biểu) và dấu nháp của các
// đơn vị trong phạm vi người gọi — của các kỳ đó và của extra (kỳ phát hành trước BF-CN-PUBLISH, tối đa 400). Hạn,
// trạng thái từng đơn vị đọc ở /v1/forms/tasks.
func (s *Service) CNTSTracking(ctx context.Context, code string, extra []uuid.UUID) (CNTSTracking, error) {
	if len(extra) > 400 {
		return CNTSTracking{}, problem.Validation.WithField("period_ids", "tối đa 400 kỳ")
	}
	a, err := s.knActor(ctx)
	if err != nil {
		return CNTSTracking{}, err
	}
	if !a.k.AllowsDepartment(domain.DeptChanNuoiThuySan) {
		return CNTSTracking{}, problem.Forbidden.WithDetail("ngữ cảnh phiên không thuộc Phòng Chăn nuôi – Thủy sản")
	}
	var org *uuid.UUID
	if a.k.Role != port.SessionRoleAdmin && a.k.Role != port.SessionRoleNational && a.k.OrgRef != uuid.Nil && a.k.Role == port.SessionRoleProvince {
		o := a.k.OrgRef
		org = &o
	}
	out := CNTSTracking{Periods: []CNTSTrackedPeriod{}, Drafts: []CNTSDraftMark{}}
	var pubs []store.CNTSPublication
	if err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		pubs, e = store.CNTSPublications(ctx, tx, domain.DeptChanNuoiThuySan, code, org, s.now().Add(-trackingWindow), trackingLimit)
		return e
	}); err != nil {
		return out, err
	}
	// #108 review: lọc lần phát hành / kỳ theo phạm vi người gọi TRƯỚC khi xuất metadata hay tra tên người phụ trách.
	// Cổng cap/forms (kỳ theo cơ sở) gọi ngoài giao dịch KN.
	pubs = s.visibleCNTSPublications(ctx, a, pubs)
	var periods []uuid.UUID
	for _, p := range pubs {
		periods = append(periods, p.PeriodRefs...)
	}
	periods = append(periods, extra...)
	var drafts []store.CNTSDraft
	if err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		drafts, e = store.CNTSDraftsOf(ctx, tx, periods)
		return e
	}); err != nil {
		return out, err
	}
	var owners []uuid.UUID
	seen := map[uuid.UUID]bool{}
	for _, p := range pubs {
		if p.OwnerRef != nil {
			owners = append(owners, *p.OwnerRef)
		}
		for _, pr := range p.PeriodRefs {
			if seen[pr] { // lần phát hành mới hơn đã có kỳ này (đọc mới nhất trước)
				continue
			}
			seen[pr] = true
			out.Periods = append(out.Periods, CNTSTrackedPeriod{PeriodRef: pr, PublicationID: p.ID, TemplateCode: p.TemplateCode, Target: p.TargetMode,
				Regions: p.Regions, OwnerRef: p.OwnerRef, CloseAfterHours: p.CloseAfterHours})
		}
	}
	if len(owners) > 0 && s.Names != nil {
		if names, err := s.Names.DisplayNames(ctx, owners); err == nil {
			for i, tp := range out.Periods {
				if tp.OwnerRef != nil {
					out.Periods[i].OwnerName = names[*tp.OwnerRef]
				}
			}
		}
	}
	for _, d := range drafts {
		if !draftVisible(a, d.Parent, d.Unit) {
			continue
		}
		out.Drafts = append(out.Drafts, CNTSDraftMark{PeriodRef: d.PeriodRef, Unit: d.Unit, SavedAt: d.SavedAt})
	}
	return out, nil
}

// visibleCNTSPublications giữ các lần phát hành (và kỳ) trong phạm vi người gọi. Admin/TW: mọi lần phát hành của
// tenant (RLS); Cấp Tỉnh: đã lọc theo Nút ở truy vấn; Cấp Xã: chỉ lần phát hành có đơn vị nhận thuộc xã phụ trách —
// biểu theo xã so trực tiếp; biểu theo cơ sở (BM-01) hỏi cap/forms từng kỳ (cap/forms lọc đơn vị theo phạm vi; kỳ không
// thấy hoặc không còn đơn vị nào → bỏ). Vai khác: không thấy gì.
func (s *Service) visibleCNTSPublications(ctx context.Context, a actor, pubs []store.CNTSPublication) []store.CNTSPublication {
	switch a.k.Role {
	case port.SessionRoleAdmin, port.SessionRoleNational, port.SessionRoleProvince:
		return pubs
	case port.SessionRoleCommune:
	default:
		return nil
	}
	out := pubs[:0:0]
	for _, p := range pubs {
		if p.UnitKind != domain.UnitFacility {
			if slices.ContainsFunc(p.Units, func(u string) bool { return draftVisible(a, u, u) }) {
				out = append(out, p)
			}
			continue
		}
		if s.FormsControl == nil {
			continue // không kiểm được xã của cơ sở → không lộ
		}
		var keep []uuid.UUID
		for _, pr := range p.PeriodRefs {
			pg, err := s.FormsControl.Progress(ctx, domain.ProfileCNTS, pr)
			if err == nil && len(pg.Units) > 0 {
				keep = append(keep, pr)
			}
		}
		if len(keep) > 0 {
			p.PeriodRefs = keep
			out = append(out, p)
		}
	}
	return out
}

// draftVisible: dấu nháp trong phạm vi người gọi (Cấp Xã: xã phụ trách; Cấp Tỉnh: Nút xuống dưới; Admin/TW: mọi).
func draftVisible(a actor, parent, unit string) bool {
	switch a.k.Role {
	case port.SessionRoleAdmin, port.SessionRoleNational:
		return true
	case port.SessionRoleCommune:
		return slices.Contains(a.k.Communes, parent) || a.k.Scope.Allows(unit)
	default:
		return a.k.Scope.All || a.k.Scope.Allows(parent) || a.k.Scope.Allows(unit)
	}
}

// CNTSDraftInput là bản nháp cán bộ xã lưu.
type CNTSDraftInput struct {
	Values    map[string]string
	NilReport bool
	Note      string
}

// Giới hạn nháp.
const (
	maxDraftFields = 300
	maxDraftValue  = 2000
)

// cntsDraftUnit kiểm kỳ native người gọi thấy (cap/forms lọc đơn vị theo phạm vi), đơn vị thuộc kỳ.
func (s *Service) cntsDraftUnit(ctx context.Context, period uuid.UUID, unit string) (actor, formsport.PeriodProgress, formsport.UnitProgress, error) {
	a, err := s.knActor(ctx)
	if err != nil {
		return a, formsport.PeriodProgress{}, formsport.UnitProgress{}, err
	}
	if s.FormsControl == nil {
		return a, formsport.PeriodProgress{}, formsport.UnitProgress{}, ErrDependency.WithDetail("chưa nối tiến độ kỳ native (cap/forms)")
	}
	p, err := s.FormsControl.Progress(ctx, domain.ProfileCNTS, period)
	if err != nil {
		return a, p, formsport.UnitProgress{}, err
	}
	for _, u := range p.Units {
		if u.Unit == unit {
			return a, p, u, nil
		}
	}
	return a, p, formsport.UnitProgress{}, formsport.ErrUnitNotInPeriod
}

// SaveCNTSDraft lưu nháp của đơn vị trong kỳ (Cấp Xã — UR-XA-13 "Lưu nháp"). Kỳ đã chốt/hủy không lưu được.
func (s *Service) SaveCNTSDraft(ctx context.Context, period uuid.UUID, unit string, in CNTSDraftInput) (store.CNTSDraft, error) {
	a, p, u, err := s.cntsDraftUnit(ctx, period, unit)
	if err != nil {
		return store.CNTSDraft{}, err
	}
	if a.k.Role != port.SessionRoleCommune {
		return store.CNTSDraft{}, problem.Forbidden.WithDetail("chỉ cán bộ Cấp Xã lưu nháp báo cáo kỳ")
	}
	if a.k.ReadOnly() {
		return store.CNTSDraft{}, ErrReadOnly
	}
	if p.Status != "open" && p.Status != "reopened" {
		return store.CNTSDraft{}, ErrPeriodClosed
	}
	in.Note = strings.TrimSpace(in.Note)
	if utf8.RuneCountInString(in.Note) > maxDraftValue {
		return store.CNTSDraft{}, problem.Validation.WithField("note", "tối đa 2000 ký tự")
	}
	if len(in.Values) > maxDraftFields {
		return store.CNTSDraft{}, problem.Validation.WithField("values", "quá nhiều trường")
	}
	vals := map[string]string{}
	for k, v := range in.Values {
		if k == "" || len(k) > 64 {
			return store.CNTSDraft{}, problem.Validation.WithField("values", "khóa trường không hợp lệ")
		}
		if utf8.RuneCountInString(v) > maxDraftValue || !utf8.ValidString(v) {
			return store.CNTSDraft{}, problem.Validation.WithField("values."+k, "tối đa 2000 ký tự")
		}
		vals[k] = v
	}
	parent := u.Parent
	if parent == "" {
		parent = u.Unit
	}
	d := store.CNTSDraft{PeriodRef: period, Unit: unit, Parent: parent, TemplateCode: p.TemplateCode, Values: vals, NilReport: in.NilReport,
		Note: in.Note, SavedBy: a.p.ActorRef, SavedAt: s.now()}
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error { return store.UpsertCNTSDraft(ctx, tx, a.p.TenantID, d) })
	return d, err
}

// CNTSDraft trả nháp còn hiệu lực của đơn vị (nil nếu chưa có hoặc đã gửi sau lần lưu nháp cuối).
func (s *Service) CNTSDraft(ctx context.Context, period uuid.UUID, unit string) (*store.CNTSDraft, error) {
	a, _, u, err := s.cntsDraftUnit(ctx, period, unit)
	if err != nil {
		return nil, err
	}
	var d *store.CNTSDraft
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		d, e = store.CNTSDraftOf(ctx, tx, period, unit)
		return e
	})
	if err != nil || d == nil {
		return nil, err
	}
	if u.LastSubmittedAt != nil && !d.SavedAt.After(*u.LastSubmittedAt) {
		return nil, nil
	}
	return d, nil
}
