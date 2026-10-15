package app

// Lịch trực KSGM (BM-03, UR-TINH-10, FN-15): lưới tháng cơ sở × viên chức × ngày → ca N/Đ/NĐ; lưu là một phiên bản
// mới; cảnh báo không chặn; đổi ca phát sự kiện cho viên chức liên quan; "Lịch trực của tôi" (UR-XA-03).

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/cntscal/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/cntscal/store"
	exdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// FacilityView là cơ sở giết mổ hoạt động trong tháng (hàng của lưới).
type FacilityView struct {
	Ref         uuid.UUID
	Code        string
	Name        string
	CommuneCode string
	Kind        string
}

// OfficerView là viên chức Cấp Xã CNTS (gợi ý cho lưới).
type OfficerView struct {
	Ref      uuid.UUID
	Communes []string
	Scope    accport.XaScope
}

// RosterView là lịch trực của tháng cho API.
type RosterView struct {
	Month      time.Time
	Province   string
	Legacy     bool
	Version    int64 // 0 = chưa lập
	Source     string
	Note       string
	Cells      []domain.Cell
	Facilities []FacilityView
	Officers   []OfficerView
	Warnings   []domain.Warning
	UpdatedAt  *time.Time
	UpdatedBy  *uuid.UUID
	Versions   []store.RosterVersion
	CanEdit    bool
	Names      map[uuid.UUID]string
	// Notified: viên chức có ca thay đổi ở lần lưu này (thông báo).
	Notified []uuid.UUID
	Replayed bool
}

// facilities trả cơ sở hoạt động trong tháng (đang dùng ngày đầu hoặc ngày cuối tháng).
func (s *Service) facilities(ctx context.Context, c caller, m time.Time) ([]FacilityView, error) {
	seen := map[uuid.UUID]bool{}
	var out []FacilityView
	for _, d := range []time.Time{m, m.AddDate(0, 1, -1)} {
		list, err := s.Catalogs.FacilitiesAt(ctx, d, nil)
		if err != nil {
			return nil, err
		}
		for _, f := range list {
			allowed, err := s.rosterCommune(ctx, c, f.CommuneCode, d)
			if err != nil {
				return nil, err
			}
			if f.Active && allowed && !seen[f.Ref] {
				seen[f.Ref] = true
				out = append(out, FacilityView{Ref: f.Ref, Code: f.Code, Name: f.Name, CommuneCode: f.CommuneCode, Kind: f.Kind})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CommuneCode != out[j].CommuneCode {
			return out[i].CommuneCode < out[j].CommuneCode
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// officers trả cán bộ Cấp Xã CNTS (nil nếu chưa nối danh bạ).
func (s *Service) officers(ctx context.Context, c caller, facilities []FacilityView) ([]OfficerView, map[uuid.UUID]accport.XaScope, error) {
	if s.Officers == nil {
		return nil, nil, nil
	}
	list, err := s.Officers.XaScopes(ctx, c.p.TenantID, DeptCNTS)
	if err != nil {
		return nil, nil, err
	}
	out := make([]OfficerView, 0, len(list))
	idx := map[uuid.UUID]accport.XaScope{}
	allowedFacilities := map[uuid.UUID]bool{}
	for _, f := range facilities {
		allowedFacilities[f.Ref] = true
	}
	for _, x := range list {
		original := x
		x.Communes = nil
		x.Facilities = nil
		for _, code := range original.Communes {
			allowed, err := s.rosterCommune(ctx, c, code, s.today())
			if err != nil {
				return nil, nil, err
			}
			if allowed {
				x.Communes = append(x.Communes, code)
			}
		}
		for _, ref := range original.Facilities {
			if allowedFacilities[ref] {
				x.Facilities = append(x.Facilities, ref)
			}
		}
		if !c.legacyRoster && len(x.Communes) == 0 && len(x.Facilities) == 0 {
			continue
		}
		out = append(out, OfficerView{Ref: x.ActorRef, Communes: x.Communes, Scope: x})
		idx[x.ActorRef] = original // không biến giới hạn rỗng sau lọc thành quyền toàn xã
	}
	return out, idx, nil
}

func grantOf(idx map[uuid.UUID]accport.XaScope) domain.Grant {
	if idx == nil {
		return nil
	}
	return func(officer, facility uuid.UUID, commune string) (bool, bool) {
		x, ok := idx[officer]
		if !ok {
			return false, true // không phải cán bộ Cấp Xã CNTS còn hiệu lực
		}
		return x.CoversFacility(facility, commune), true
	}
}

func facilitiesOf(fs []FacilityView) []domain.Facility {
	out := make([]domain.Facility, 0, len(fs))
	for _, f := range fs {
		out = append(out, domain.Facility{Ref: f.Ref, Commune: f.CommuneCode})
	}
	return out
}

func (s *Service) month(raw string) (time.Time, error) {
	m, err := domain.ParseMonth(raw)
	if err != nil {
		return m, problem.Validation.WithField("month", err.Error())
	}
	if err := domain.CheckMonth(m, s.today()); err != nil {
		return m, problem.Validation.WithField("month", err.Error())
	}
	return m, nil
}

// rosterReader: Admin, Cấp Tỉnh CNTS (lưới toàn tỉnh). Cấp Xã dùng "Lịch trực của tôi".
func (s *Service) rosterReader(ctx context.Context) (caller, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return c, err
	}
	if !c.admin() && c.k.Role != authz.KNTinh {
		return c, problem.Forbidden.WithDetail("Cấp Xã xem lịch trực của mình (Lịch trực của tôi)")
	}
	if s.Catalogs == nil {
		return c, ErrDependency
	}
	return c, nil
}

func (c caller) canPlanRoster() bool {
	return c.admin() || (c.k.Role == authz.KNTinh && !c.k.ReadOnly() &&
		(c.k.ProvinceGroup == "" || c.k.ProvinceGroup == accport.GroupTongHop))
}

// GetRoster đọc lịch trực tháng (lưới, cơ sở, viên chức, cảnh báo, lịch sử lưu).
func (s *Service) GetRoster(ctx context.Context, monthRaw string, province ...string) (RosterView, error) {
	c, err := s.rosterReader(ctx)
	if err != nil {
		return RosterView{}, err
	}
	c, err = s.rosterScope(ctx, c, requestedProvince(province), false)
	if err != nil {
		return RosterView{}, err
	}
	m, err := s.month(monthRaw)
	if err != nil {
		return RosterView{}, err
	}
	v := RosterView{Month: m, Province: c.province, Legacy: c.legacyRoster, Cells: []domain.Cell{}, CanEdit: !c.legacyRoster && c.canPlanRoster()}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		r, err := store.LatestRoster(ctx, tx, m, c.province)
		if err != nil {
			return err
		}
		if r != nil {
			fillRoster(&v, r)
		}
		v.Versions, err = store.RosterVersions(ctx, tx, m, c.province, 20)
		return err
	})
	if err != nil {
		return RosterView{}, err
	}
	return v, s.decorateRoster(ctx, c, &v)
}

func fillRoster(v *RosterView, r *store.Roster) {
	at, by := r.EventTime, r.ActorRef
	v.Version, v.Source, v.Note, v.Cells, v.UpdatedAt, v.UpdatedBy = r.Seq, r.Source, r.Note, r.Cells, &at, &by
	if v.Cells == nil {
		v.Cells = []domain.Cell{}
	}
}

func (s *Service) decorateRoster(ctx context.Context, c caller, v *RosterView) error {
	var err error
	if v.Facilities, err = s.facilities(ctx, c, v.Month); err != nil {
		return err
	}
	offs, idx, err := s.officers(ctx, c, v.Facilities)
	if err != nil {
		return err
	}
	v.Officers = offs
	v.Warnings = domain.RosterWarnings(v.Month, v.Cells, facilitiesOf(v.Facilities), grantOf(idx))
	refs := []uuid.UUID{}
	for _, o := range offs {
		refs = append(refs, o.Ref)
	}
	for _, x := range v.Cells {
		refs = append(refs, x.Officer)
	}
	if v.UpdatedBy != nil {
		refs = append(refs, *v.UpdatedBy)
	}
	for _, x := range v.Versions {
		refs = append(refs, x.ActorRef)
	}
	v.Names = s.names(ctx, uniq(refs))
	return nil
}

func uniq(refs []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := make([]uuid.UUID, 0, len(refs))
	for _, r := range refs {
		if r != uuid.Nil && !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	return out
}

// SaveRosterInput là thân "Lưu" lịch trực tháng.
type SaveRosterInput struct {
	ProvinceCode string
	Month        string
	BaseVersion  *int64 // phiên bản người lập đã thấy (0 = chưa có lịch)
	Cells        []domain.Cell
	Source       string
	Note         string
	IdemKey      string
}

func idemRoster(k string) string { return "duty:" + k }

// SaveRoster lưu lịch trực tháng thành phiên bản mới (cán bộ tổng hợp Cấp Tỉnh, Admin). Ô thuộc cơ sở không hoạt động
// trong tháng bị chặn; cảnh báo (chưa có người trực, trực 2 cơ sở cùng ca, chưa cấp quyền cơ sở) không chặn lưu.
// Viên chức có ca thay đổi nhận sự kiện pf.extension.cnts_duty.changed (thông báo).
func (s *Service) SaveRoster(ctx context.Context, in SaveRosterInput) (RosterView, error) {
	c, err := s.planner(ctx, "chỉ cán bộ tổng hợp Cấp Tỉnh (hoặc Admin) lập lịch trực KSGM", accport.GroupTongHop)
	if err != nil {
		return RosterView{}, err
	}
	c, err = s.rosterScope(ctx, c, in.ProvinceCode, true)
	if err != nil {
		return RosterView{}, err
	}
	if strings.TrimSpace(in.IdemKey) == "" {
		return RosterView{}, problem.Validation.WithField("Idempotency-Key", "bắt buộc")
	}
	m, err := s.month(in.Month)
	if err != nil {
		return RosterView{}, err
	}
	if in.BaseVersion == nil {
		return RosterView{}, errBaseVersionNeed
	}
	switch in.Source {
	case "":
		in.Source = domain.SourceGrid
	case domain.SourceGrid, domain.SourceCopy, domain.SourceExcel:
	default:
		return RosterView{}, ErrRosterInvalid.WithField("source", "grid, copy hoặc excel")
	}
	in.Note = strings.TrimSpace(in.Note)
	if utf8.RuneCountInString(in.Note) > domain.MaxNoteLen {
		return RosterView{}, ErrRosterInvalid.WithField("note", "tối đa 1000 ký tự")
	}
	if exdomain.ContainsPII(in.Note) {
		return RosterView{}, ErrRosterInvalid.WithField("note", exdomain.ErrPIIInText.Error())
	}
	cells, es := domain.NormalizeCells(m, in.Cells)
	if len(es) > 0 {
		return RosterView{}, fieldsProblem(ErrRosterInvalid, es)
	}
	facs, err := s.facilities(ctx, c, m)
	if err != nil {
		return RosterView{}, err
	}
	if len(cells) > 0 && s.Officers == nil {
		return RosterView{}, ErrDependency.WithDetail("Chưa nối danh bạ cán bộ Cấp Xã để kiểm phạm vi phân ca")
	}
	_, officers, err := s.officers(ctx, c, facs)
	if err != nil {
		return RosterView{}, err
	}
	active := map[uuid.UUID]bool{}
	for _, f := range facs {
		active[f.Ref] = true
	}
	for i, x := range cells {
		if s.Officers != nil {
			if _, ok := officers[x.Officer]; !ok {
				return RosterView{}, ErrRosterInvalid.WithField("cells", "cán bộ không thuộc tỉnh lập lịch")
			}
		}
		if !active[x.Facility] {
			return RosterView{}, ErrRosterInvalid.WithField("cells", "ô thứ "+strconv.Itoa(i+1)+": cơ sở giết mổ không hoạt động trong tháng")
		}
	}
	now := s.now()
	v := RosterView{Month: m, Province: c.province, CanEdit: true}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if r, err := store.RosterByIdem(ctx, tx, idemRoster(in.IdemKey)); err != nil || r != nil {
			if r != nil {
				if r.StreamKey != store.RosterStream(m, c.province) {
					return ErrOutOfScope
				}
				fillRoster(&v, r)
				v.Replayed = true
			}
			return err
		}
		if err := store.Lock(ctx, tx, c.p.TenantID.String()+":"+store.RosterStream(m, c.province)); err != nil {
			return err
		}
		prev, err := store.LatestRoster(ctx, tx, m, c.province)
		if err != nil {
			return err
		}
		var cur int64
		var old []domain.Cell
		if prev != nil {
			cur, old = prev.Seq, prev.Cells
		}
		if *in.BaseVersion != cur {
			return ErrRosterConflict.With("current_version", cur)
		}
		r := &store.Roster{Meta: c.meta(now, idemRoster(in.IdemKey)), Month: m, Province: c.province, Source: in.Source, Cells: cells, Note: in.Note}
		if err := store.InsertRoster(ctx, tx, c.p.TenantID, r); err != nil {
			return err
		}
		fillRoster(&v, r)
		v.Notified = domain.ChangedOfficers(old, cells)
		if len(v.Notified) == 0 {
			return nil
		}
		return outbox.Enqueue(ctx, tx, outbox.Event{
			ID: ids.New(), TenantID: c.p.TenantID, Type: port.EventCNTSDutyChanged, SchemaVersion: 1, OccurredAt: now,
			TraceID: telemetry.TraceID(ctx), Producer: port.EventProducerName,
			Payload: port.CNTSDutyChangedV1{Month: domain.MonthKey(m), RosterID: r.ID, Version: r.Seq, OfficerRefs: v.Notified,
				ActorRef: c.p.ActorRef, Department: DeptCNTS, EventTime: now.Format(time.RFC3339)},
		})
	})
	if dup(err) {
		return s.SaveRoster(ctx, in)
	}
	if err != nil {
		return RosterView{}, err
	}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		v.Versions, e = store.RosterVersions(ctx, tx, m, c.province, 20)
		return e
	})
	if err != nil {
		return RosterView{}, err
	}
	return v, s.decorateRoster(ctx, c, &v)
}

// MyDuty là một ca trực của người gọi.
type MyDuty struct {
	Date       time.Time
	Shift      string
	Facility   FacilityView
	Colleagues []uuid.UUID // viên chức khác trực cùng cơ sở, cùng ngày (KS-04)
}

// MyDuties là "Lịch trực KSGM của tôi" (UR-XA-03): ca của người gọi trong tháng theo bản hiện hành.
type MyDuties struct {
	Month   time.Time
	Version int64
	Items   []MyDuty
	Names   map[uuid.UUID]string
}

// GetMyDuties trả ca trực của người gọi (Cấp Xã, Cấp Tỉnh CNTS).
func (s *Service) GetMyDuties(ctx context.Context, monthRaw string, province ...string) (MyDuties, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return MyDuties{}, err
	}
	c, err = s.rosterScope(ctx, c, requestedProvince(province), false)
	if err != nil {
		return MyDuties{}, err
	}
	if s.Catalogs == nil {
		return MyDuties{}, ErrDependency
	}
	m, err := s.month(monthRaw)
	if err != nil {
		return MyDuties{}, err
	}
	out := MyDuties{Month: m, Items: []MyDuty{}}
	var r *store.Roster
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		r, e = store.LatestRoster(ctx, tx, m, c.province)
		return e
	}); err != nil || r == nil {
		return out, err
	}
	out.Version = r.Seq
	// Lịch đã giao cho chính người gọi cho phép đọc tên cơ sở của ca trong tỉnh;
	// không mở quyền nhập liệu hay trả lưới/cán bộ của ca khác.
	labelsCaller := c
	labelsCaller.k.Scope = authz.Scope{All: true}
	facs, err := s.facilities(ctx, labelsCaller, m)
	if err != nil {
		return MyDuties{}, err
	}
	byRef := map[uuid.UUID]FacilityView{}
	for _, f := range facs {
		byRef[f.Ref] = f
	}
	var refs []uuid.UUID
	for _, x := range r.Cells {
		if x.Officer != c.p.ActorRef {
			continue
		}
		d := MyDuty{Date: x.Date(m), Shift: x.Shift, Facility: byRef[x.Facility], Colleagues: []uuid.UUID{}}
		if d.Facility.Ref == uuid.Nil {
			d.Facility = FacilityView{Ref: x.Facility}
		}
		for _, y := range r.Cells {
			if y.Facility == x.Facility && y.Day == x.Day && y.Officer != x.Officer {
				d.Colleagues = append(d.Colleagues, y.Officer)
				refs = append(refs, y.Officer)
			}
		}
		out.Items = append(out.Items, d)
	}
	sort.Slice(out.Items, func(i, j int) bool { return out.Items[i].Date.Before(out.Items[j].Date) })
	out.Names = s.names(ctx, uniq(refs))
	return out, nil
}
