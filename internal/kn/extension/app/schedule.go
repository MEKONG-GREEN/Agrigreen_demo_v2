package app

// KN v7.7 gói P5 — lịch công tác tuần: FN-XA-03.08 (Cán bộ xã đăng ký lịch tuần, gửi duyệt) và FN-TI-02.07
// (Cấp Tỉnh xem, duyệt / yêu cầu chỉnh sửa lịch Cán bộ xã cùng phòng ban, cùng tỉnh; đăng ký "Lịch của tôi").
//
// Phiên đã chọn dùng payload do KN sở hữu; resolver/realm cũ chỉ phục vụ request chưa chọn phiên. Sổ chỉ ghi thêm: mỗi lần lưu là một phiên bản
// (work_schedule_item), mỗi thao tác là một sự việc (work_schedule_event) — RB-1.

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/store"
	accountsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi lịch công tác (docs/errors.md, mục pf/extension).
var (
	ErrScheduleNotFound    = problem.New(http.StatusNotFound, "extension.schedule_not_found", "Không tìm thấy lịch công tác")
	ErrScheduleState       = problem.New(http.StatusConflict, "extension.schedule_state_conflict", "Trạng thái lịch không cho phép thao tác này")
	ErrScheduleVersion     = problem.New(http.StatusConflict, "extension.schedule_version_conflict", "Lịch đã có phiên bản mới hơn — tải lại rồi sửa")
	ErrWeekLocked          = problem.New(http.StatusConflict, "extension.week_locked", "Tuần đã kết thúc — lịch bị khóa, chỉ xem")
	ErrNotScheduleApprover = problem.New(http.StatusForbidden, "extension.not_schedule_approver", "Chỉ Cấp Tỉnh cùng phòng ban, cùng tỉnh, có cấp quản lý được duyệt mới duyệt được lịch này")
	ErrScheduleContext     = problem.New(http.StatusForbidden, "extension.schedule_context_required", "Tài khoản chưa được gán phòng ban và xã phụ trách — liên hệ Admin")
	ErrScheduleNoPrevious  = problem.New(http.StatusNotFound, "extension.schedule_no_previous", "Tuần trước chưa có lịch để sao chép")
)

// Mặc định tham số (tạm, chờ khách Q-05, Q-16).
const (
	defaultSubmitDueOffset = -55 // 17:00 Thứ Sáu tuần trước
	defaultReviewDueOffset = 0   // 00:00 Thứ Hai của tuần
	defaultRemindBefore    = 24
)

var defaultApproverLevels = []string{port.MgmtLevelHead}

// ---------------------------------------------------------------------------
// Ngữ cảnh KN
// ---------------------------------------------------------------------------

// knActor là người gọi kèm ngữ cảnh KN.
type knActor struct {
	authz.Principal
	accountsport.KNContext
	ownedSession *port.SessionContext
}

// knActor dựng ngữ cảnh KN: P1 (X-AG-Context) nếu có; ngược lại suy từ vai realm + phạm vi đơn vị:
// kn_province_admin → Cấp Tỉnh (tỉnh của Nút), kn_officer → Cấp Xã (xã của Nút), platform_admin → Admin,
// kn_national_admin → Cấp Quốc gia. Phòng ban mặc định Trồng trọt (gap §4.6: dữ liệu cũ không có phòng ban).
func (s *Service) knActor(ctx context.Context) (knActor, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return knActor{}, problem.Unauthenticated
	}
	a := knActor{Principal: p}
	// A selected session is authoritative, including when its legacy marker is absent.
	// Never let a resolver or broad raw role replace an invalid owner binding.
	if p.SelectedSession != nil || p.KN != nil {
		_, k, err := port.RequireSession(ctx)
		if err != nil {
			return knActor{}, err
		}
		a.ownedSession = &k
		a.KNContext = accountsport.KNContext{Role: k.Role, Department: k.Department,
			Communes: slices.Clone(k.Communes), MgmtLevel: strings.ToLower(strings.TrimSpace(k.MgmtLevel)),
			InternalUnit: k.InternalUnit, ReadOnly: k.ReadOnly()}
		if k.Role != port.SessionRoleAdmin && k.Role != port.SessionRoleNational {
			a.ProvinceCode, err = s.scheduleOwnerProvince(ctx, k)
			if err != nil {
				return knActor{}, err
			}
		}
		return a, nil
	}
	if s.KNContext != nil {
		c, ok, err := s.KNContext.KNContext(ctx)
		if err != nil {
			return knActor{}, err
		}
		if ok {
			c.MgmtLevel = strings.ToLower(strings.TrimSpace(c.MgmtLevel))
			a.KNContext = c
			return a, nil
		}
	}
	switch {
	case p.Has(authz.RolePlatformAdmin):
		a.Role = port.KNRoleAdmin
		return a, nil
	case p.Has(port.RoleKNProvinceAdmin):
		a.Role, a.Department = port.KNRoleTinh, partyport.DeptTrongTrot
		sc, err := s.scope(ctx, p)
		if err != nil {
			return knActor{}, err
		}
		for _, u := range sc.Units {
			prov, level, err := s.provinceOf(ctx, u, "org_ref")
			if err == nil && level == partyport.LevelProvince {
				a.ProvinceCode = prov
				break
			}
		}
		return a, nil
	case p.Has(port.RoleKNOfficer):
		a.Role, a.Department = port.KNRoleXa, partyport.DeptTrongTrot
		sc, err := s.scope(ctx, p)
		if err != nil {
			return knActor{}, err
		}
		for _, u := range sc.Units {
			prov, level, err := s.provinceOf(ctx, u, "org_ref")
			if err != nil || level != partyport.LevelCommune {
				continue
			}
			if a.ProvinceCode == "" {
				a.ProvinceCode = prov
			}
			if prov == a.ProvinceCode {
				a.Communes = append(a.Communes, u)
			}
		}
		return a, nil
	case p.Has(port.RoleKNNationalAdmin):
		a.Role = port.KNRoleTW
		return a, nil
	}
	return knActor{}, problem.Forbidden
}

// registrant kiểm người gọi đăng ký được lịch của mình (Cấp Xã, Cấp Tỉnh; tiên quyết FN-XA-03.08: đã gán phòng
// ban và xã phụ trách).
func (a knActor) registrant() error {
	switch a.Role {
	case port.KNRoleXa:
		if a.Department == "" || a.ProvinceCode == "" || len(a.Communes) == 0 {
			return ErrScheduleContext
		}
	case port.KNRoleTinh:
		if a.Department == "" || a.ProvinceCode == "" {
			return ErrScheduleContext
		}
	default:
		return problem.Forbidden.WithDetail("chỉ Cấp Xã và Cấp Tỉnh đăng ký lịch công tác")
	}
	return nil
}

func (a knActor) level() string {
	if a.Role == port.KNRoleTinh {
		return accountsport.ScheduleLevelProvince
	}
	return accountsport.ScheduleLevelCommune
}

// canView: Cấp Xã chỉ lịch của mình; Cấp Tỉnh lịch cùng phòng ban, cùng tỉnh; Admin mọi lịch; Cấp Quốc gia không.
func (a knActor) canView(sc domain.Schedule) bool {
	switch a.Role {
	case port.KNRoleAdmin:
		return true
	case port.KNRoleXa:
		return sc.OwnerRef == a.ActorRef
	case port.KNRoleTinh:
		return sc.OwnerRef == a.ActorRef || (sc.Department == a.Department && sc.ProvinceCode == a.ProvinceCode && a.ProvinceCode != "")
	}
	return false
}

// readable kiểm vai được xem lịch công tác (ma trận §5: Admin R, Tỉnh, Xã; Cấp Quốc gia không có).
func (a knActor) readable() error {
	switch a.Role {
	case port.KNRoleAdmin, port.KNRoleTinh, port.KNRoleXa:
		return nil
	}
	return problem.Forbidden.WithDetail("vai trò này không xem lịch công tác")
}

// ---------------------------------------------------------------------------
// Tham số
// ---------------------------------------------------------------------------

// ScheduleSettings là cấu hình hiệu lực của một tỉnh (FN-TI-02.07: "thời hạn đăng ký và thời hạn duyệt do TTKN
// tỉnh cấu hình").
type ScheduleSettings struct {
	ProvinceCode     string
	SubmitDueOffset  int
	ReviewDueOffset  int
	RemindBefore     int
	ApproverLevels   []string
	ProvinceApproval string
	Provisional      bool
	// Theo phòng ban (W11.46, Q-057).
	Department    string
	ApproverRule  string  // port.ApproverRuleLevels | port.ApproverRuleFollower
	Checkin       bool    // check-in GPS (CV-07)
	CheckinFarM   float64 // ngưỡng "xa địa điểm"
	CheckinAccM   float64 // ngưỡng GPS yếu (cnts.gps_accuracy_m)
	StorePoint    bool    // cờ QĐ-8: lưu tọa độ check-in (bảng có hạn lưu)
	RetentionDays int     // hạn lưu tọa độ check-in
}

// param tra khóa có chiều tỉnh trước, rồi khóa chung; không có → ok = false.
func (s *Service) param(ctx context.Context, key, province string, at time.Time) (params.ParamValue, bool, error) {
	if s.Params == nil {
		return params.ParamValue{}, false, nil
	}
	keys := []string{key}
	if province != "" {
		keys = []string{key + ":" + province, key}
	}
	for _, k := range keys {
		v, err := s.Params.At(ctx, k, at)
		if errors.Is(err, params.ErrParamNotFound) {
			continue
		}
		if err != nil {
			return params.ParamValue{}, false, err
		}
		return v, true, nil
	}
	return params.ParamValue{}, false, nil
}

func (s *Service) scheduleSettings(ctx context.Context, dept, province string, at time.Time) (ScheduleSettings, error) {
	st := ScheduleSettings{ProvinceCode: province, SubmitDueOffset: defaultSubmitDueOffset, ReviewDueOffset: defaultReviewDueOffset,
		RemindBefore: defaultRemindBefore, ApproverLevels: defaultApproverLevels, ProvinceApproval: port.ProvinceApprovalPeer, Provisional: true}
	ints := []struct {
		key string
		dst *int
	}{{port.ParamScheduleSubmitDueOffsetHours, &st.SubmitDueOffset}, {port.ParamScheduleReviewDueOffsetHours, &st.ReviewDueOffset},
		{port.ParamScheduleRemindBeforeHours, &st.RemindBefore}}
	for _, p := range ints {
		v, ok, err := s.param(ctx, p.key, province, at)
		if err != nil {
			return st, err
		}
		if ok {
			*p.dst, st.Provisional = int(v.Value.IntPart()), false
		}
	}
	if v, ok, err := s.param(ctx, port.ParamScheduleApproverLevels, province, at); err != nil {
		return st, err
	} else if ok && len(domain.ParseLevels(v.Text)) > 0 {
		st.ApproverLevels, st.Provisional = domain.ParseLevels(v.Text), false
	}
	if v, ok, err := s.param(ctx, port.ParamScheduleProvinceApproval, province, at); err != nil {
		return st, err
	} else if ok {
		if t := strings.TrimSpace(v.Text); t == port.ProvinceApprovalAuto || t == port.ProvinceApprovalPeer {
			st.ProvinceApproval, st.Provisional = t, false
		}
	}
	return st, s.deptSettings(ctx, &st, dept, province, at)
}

// SubmitDue, ReviewDue của tuần theo cấu hình.
func (st ScheduleSettings) SubmitDue(week time.Time) time.Time {
	return domain.ScheduleDueAt(week, st.SubmitDueOffset, clock.Zone())
}

// ReviewDue là hạn duyệt của tuần.
func (st ScheduleSettings) ReviewDue(week time.Time) time.Time {
	return domain.ScheduleDueAt(week, st.ReviewDueOffset, clock.Zone())
}

// GetScheduleSettings trả cấu hình của tỉnh (mặc định tỉnh của người gọi).
func (s *Service) GetScheduleSettings(ctx context.Context, province string) (ScheduleSettings, error) {
	a, err := s.knActor(ctx)
	if err != nil {
		return ScheduleSettings{}, err
	}
	if err := a.readable(); err != nil {
		return ScheduleSettings{}, err
	}
	province = strings.TrimSpace(province)
	if province == "" {
		province = a.ProvinceCode
	}
	if a.Role != port.KNRoleAdmin && province != a.ProvinceCode {
		return ScheduleSettings{}, ErrOutOfScope
	}
	return s.scheduleSettings(ctx, a.Department, province, s.now())
}

// canReview: Cấp Tỉnh cùng phòng ban, cùng tỉnh, cấp quản lý được duyệt (Q-05), không phải người đăng ký (R-56).
// Lịch của cán bộ Cấp Tỉnh chỉ duyệt được khi cấu hình "peer" (Q-16).
func (a knActor) canReview(sc domain.Schedule, st ScheduleSettings) error {
	if a.ReadOnly || a.Role != port.KNRoleTinh || sc.Department != a.Department || sc.ProvinceCode != a.ProvinceCode {
		return ErrNotScheduleApprover
	}
	if sc.OwnerRef == a.ActorRef {
		return problem.SelfApproval.WithDetail("Không được duyệt lịch công tác của chính mình")
	}
	if !a.mayApprove(st) {
		return ErrNotScheduleApprover.With("mgmt_level", a.MgmtLevel)
	}
	if sc.OwnerLevel == accountsport.ScheduleLevelProvince && st.ProvinceApproval != port.ProvinceApprovalPeer {
		return ErrNotScheduleApprover
	}
	return nil
}

// ---------------------------------------------------------------------------
// Danh bạ cán bộ KN (P1 hoặc dự phòng pf_extension.staff)
// ---------------------------------------------------------------------------

func (s *Service) knOfficers(ctx context.Context, tid uuid.UUID, dept, province string) ([]accountsport.KNOfficer, error) {
	if s.KNDirectory != nil {
		return s.KNDirectory.Officers(ctx, tid, dept, province)
	}
	if dept != partyport.DeptTrongTrot || province == "" {
		return nil, nil // danh bạ trực chưa có phòng ban: coi mọi cán bộ là Phòng Trồng trọt
	}
	var out []accountsport.KNOfficer
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		units, refs, err := store.ProvinceStaff(ctx, tx, province)
		if err != nil {
			return err
		}
		for i, ref := range refs {
			o := accountsport.KNOfficer{ActorRef: ref, ProvinceCode: units[i][1]}
			if units[i][0] == units[i][1] {
				o.Level = accountsport.ScheduleLevelProvince
			} else {
				o.Level, o.Communes = accountsport.ScheduleLevelCommune, []string{units[i][0]}
			}
			out = append(out, o)
		}
		return nil
	})
	return out, err
}

// approvers trả người duyệt được lịch của owner (cùng phòng ban, cùng tỉnh, cấp quản lý được duyệt, khác owner).
func (s *Service) approvers(ctx context.Context, tid uuid.UUID, dept, province string, owner uuid.UUID, st ScheduleSettings) ([]uuid.UUID, error) {
	offs, err := s.knOfficers(ctx, tid, dept, province)
	if err != nil {
		return nil, err
	}
	out := []uuid.UUID{}
	followers, err := s.followers(ctx, tid, dept, st)
	if err != nil {
		return nil, err
	}
	for _, o := range offs {
		if o.Level == accountsport.ScheduleLevelProvince && o.ActorRef != owner && officerMayApprove(o, followers, st) {
			out = append(out, o.ActorRef)
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Đăng ký / lưu
// ---------------------------------------------------------------------------

// ScheduleSaveInput là thân "Lưu nháp" / "Gửi duyệt".
type ScheduleSaveInput struct {
	ID          *uuid.UUID
	WeekStart   time.Time
	Action      string
	BaseVersion *int
	Items       []domain.ScheduleItem
	DeviceID    *string
	IdemKey     string
}

// ScheduleDetail là chi tiết lịch cho API.
type ScheduleDetail struct {
	Schedule       domain.Schedule
	ViewingVersion int
	Items          []domain.ScheduleItem
	ApprovedItems  []domain.ScheduleItem
	Changed        map[uuid.UUID]bool
	Conflicts      map[uuid.UUID]bool
	Warnings       []ScheduleWarning
	Versions       []ScheduleVersion
	Locked         bool
	SubmitDue      time.Time
	ReviewDue      time.Time
	ReviewOverdue  bool
	Can            ScheduleCan
	Names          map[uuid.UUID]string
	Replayed       bool
	// W11.46: check-in GPS (phòng ban bật check-in) — khóa theo item_id.
	CheckinEnabled bool
	Checkins       map[uuid.UUID]store.Checkin
}

// ScheduleWarning là cảnh báo không chặn.
type ScheduleWarning struct {
	Code    string
	Message string
	ItemIDs []uuid.UUID
}

// ScheduleVersion là một phiên bản trong lịch sử.
type ScheduleVersion struct {
	Version   int
	Action    string
	At        time.Time
	ActorRef  uuid.UUID
	ItemCount int
}

// ScheduleCan là thao tác người gọi làm được (UI ẩn/hiện nút; backend vẫn kiểm).
type ScheduleCan struct{ Edit, Submit, Approve, RequestChanges bool }

func idemSave(key string) string   { return "save:" + key }
func idemReview(key string) string { return "review:" + key }

// SaveSchedule lưu nháp hoặc gửi duyệt lịch tuần của chính người gọi (FN-XA-03.08 luồng chính, "Lịch của tôi"
// FN-TI-02.07). Mỗi lần lưu là một phiên bản mới (RB-1); gọi lặp cùng khóa trả kết quả lần đầu.
func (s *Service) SaveSchedule(ctx context.Context, in ScheduleSaveInput) (ScheduleDetail, error) {
	a, err := s.knActor(ctx)
	if err != nil {
		return ScheduleDetail{}, err
	}
	if a.ReadOnly {
		return ScheduleDetail{}, problem.Forbidden.WithDetail("ngữ cảnh chỉ xem")
	}
	if err := a.registrant(); err != nil {
		return ScheduleDetail{}, err
	}
	if in.IdemKey == "" {
		return ScheduleDetail{}, problem.IdempotencyRequired
	}
	now, zone := s.now(), clock.Zone()
	if err := domain.CheckWeek(in.WeekStart, now, zone); err != nil {
		return ScheduleDetail{}, fieldProblem(err)
	}
	if in.Action != domain.SaveDraft && in.Action != domain.SaveSubmit {
		return ScheduleDetail{}, problem.Validation.WithField("action", "draft hoặc submit")
	}
	if replay, ok, err := s.scheduleReplay(ctx, a, idemSave(in.IdemKey)); err != nil || ok {
		return replay, err
	}
	if domain.WeekLocked(in.WeekStart, now, zone) {
		return ScheduleDetail{}, ErrWeekLocked
	}
	// Xã chọn được: cán bộ xã → xã phụ trách; cán bộ tỉnh → mọi xã thuộc tỉnh (quyết định PO 25/10/2026).
	allowed, err := s.scheduleCommunes(ctx, a, false)
	if err != nil {
		return ScheduleDetail{}, err
	}
	items, err := domain.NormalizeItems(in.WeekStart, in.Items, communeCodes(allowed), ids.New)
	if err != nil {
		return ScheduleDetail{}, fieldProblem(err)
	}
	if in.Action == domain.SaveSubmit && len(items) == 0 {
		return ScheduleDetail{}, problem.Validation.WithField("items", domain.ErrSubmitEmpty.Error())
	}
	if err := s.checkScheduleItems(ctx, items); err != nil {
		return ScheduleDetail{}, err
	}
	st, err := s.scheduleSettings(ctx, a.Department, a.ProvinceCode, now)
	if err != nil {
		return ScheduleDetail{}, err
	}
	auto := a.level() == accountsport.ScheduleLevelProvince && st.ProvinceApproval == port.ProvinceApprovalAuto
	var approvers []uuid.UUID
	if in.Action == domain.SaveSubmit && !auto {
		if approvers, err = s.approvers(ctx, a.TenantID, a.Department, a.ProvinceCode, a.ActorRef, st); err != nil {
			return ScheduleDetail{}, err
		}
	}

	var sched domain.Schedule
	err = tenant.WithTenantTx(ctx, s.DB, a.TenantID, func(tx pgx.Tx) error {
		if err := store.LockScheduleKey(ctx, tx, a.ActorRef, a.Department, a.level(), in.WeekStart); err != nil {
			return err
		}
		cur, err := store.ScheduleByOwnerWeek(ctx, tx, a.ActorRef, a.Department, a.level(), in.WeekStart, true)
		if err != nil {
			return err
		}
		status, approved := "", 0
		var prevItems []domain.ScheduleItem
		if cur == nil {
			if in.BaseVersion != nil && *in.BaseVersion != 0 {
				return ErrScheduleVersion.With("current_version", 0)
			}
			id := ids.New()
			if in.ID != nil && *in.ID != uuid.Nil {
				if other, err := store.ScheduleByID(ctx, tx, *in.ID, false); err != nil {
					return err
				} else if other != nil {
					return ErrIDReuse
				}
				id = *in.ID
			}
			sched = domain.Schedule{ID: id, OwnerRef: a.ActorRef, OwnerLevel: a.level(), Department: a.Department,
				ProvinceCode: a.ProvinceCode, WeekStart: in.WeekStart, CreatedBy: a.ActorRef, CreatedAt: now}
		} else {
			if in.BaseVersion == nil || *in.BaseVersion != cur.Version {
				return ErrScheduleVersion.With("current_version", cur.Version)
			}
			sched, status, approved = *cur, cur.Status, cur.ApprovedVersion
			if prevItems, err = store.ScheduleItems(ctx, tx, cur.ID, cur.Version); err != nil {
				return err
			}
		}
		act, after, err := domain.NextOnSave(status, in.Action, approved)
		if errors.Is(err, domain.ErrScheduleDraftAfterSubmit) {
			return ErrScheduleState.WithDetail(err.Error())
		}
		if err != nil {
			return fieldProblem(err)
		}
		prevRow := map[uuid.UUID]uuid.UUID{}
		for _, it := range prevItems {
			prevRow[it.ItemID] = it.RowID
		}
		for i := range items {
			if r, ok := prevRow[items[i].ItemID]; ok {
				r := r
				items[i].SupersedesID = &r
			}
		}
		sched.Version++
		late := false
		if in.Action == domain.SaveSubmit {
			at := now
			if sched.FirstSubmitted == nil {
				late = now.After(st.SubmitDue(in.WeekStart))
				sched.FirstSubmitted = &at
			}
			sched.SubmittedAt = &at
		}
		sched.Late = sched.Late || late
		sched.Communes = slices.Clone(a.Communes)
		sched.Status, sched.ItemCount = after, len(items)
		sched.ConflictCount = len(domain.Conflicts(items))
		sched.Amendment = sched.ApprovedVersion > 0
		sched.UpdatedBy, sched.UpdatedAt = a.ActorRef, now
		if err := store.InsertScheduleItems(ctx, tx, a.TenantID, sched.ID, sched.Version, items, a.ActorRef, now,
			idemSave(in.IdemKey)+":i", ids.New); err != nil {
			return err
		}
		ev := s.scheduleEvent(a.TenantID, sched, act, a.ActorRef, now, idemSave(in.IdemKey))
		ev.Late, ev.DeviceID = late, in.DeviceID
		if err := store.InsertScheduleEvent(ctx, tx, &ev); err != nil {
			return err
		}
		sched.LastEventID, sched.LastSeq = ev.ID, ev.Seq
		if in.Action == domain.SaveSubmit {
			if auto {
				// Q-16 "auto": lịch cán bộ Cấp Tỉnh tự duyệt khi gửi — vẫn ghi sự việc duyệt (người duyệt = Hệ thống).
				if err := s.applyReview(ctx, tx, a.TenantID, &sched, SystemActor, domain.ScheduleActApproved, "", "", now,
					idemSave(in.IdemKey)+":auto", true); err != nil {
					return err
				}
			} else if err := s.enqueueSubmitted(ctx, tx, a.TenantID, sched, ev, approvers); err != nil {
				return err
			}
		}
		return store.UpsertSchedule(ctx, tx, a.TenantID, sched)
	})
	if errors.Is(ledger.Classify(err), ledger.ErrDuplicate) {
		if replay, ok, rerr := s.scheduleReplay(ctx, a, idemSave(in.IdemKey)); rerr == nil && ok {
			return replay, nil
		}
	}
	if err != nil {
		return ScheduleDetail{}, err
	}
	return s.scheduleDetail(ctx, a, sched.ID, 0)
}

// checkScheduleItems kiểm HTX đang hoạt động và thuộc xã đã chọn (xã thuộc phạm vi đã kiểm ở NormalizeItems:
// xã phụ trách của cán bộ xã, xã thuộc tỉnh của cán bộ tỉnh).
func (s *Service) checkScheduleItems(ctx context.Context, items []domain.ScheduleItem) error {
	for _, it := range items {
		if it.CooperativeRef != uuid.Nil {
			if it.CommuneCode == "" {
				return problem.Validation.WithField("items.commune_code", "chọn xã của HTX")
			}
			if err := s.checkCooperative(ctx, it.CooperativeRef, it.CommuneCode); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) scheduleEvent(tid uuid.UUID, sc domain.Schedule, act string, actor uuid.UUID, at time.Time, key string) domain.ScheduleEvent {
	return domain.ScheduleEvent{
		ID: ids.New(), TenantID: tid, ScheduleID: sc.ID, Version: sc.Version, Action: act, StatusAfter: sc.Status,
		OwnerRef: sc.OwnerRef, OwnerLevel: sc.OwnerLevel, Department: sc.Department, Province: sc.ProvinceCode,
		Communes: sc.Communes, WeekStart: sc.WeekStart, ItemCount: sc.ItemCount, EventTime: at, ActorRef: actor, IdemKey: key,
	}
}

// applyReview ghi sự việc duyệt / yêu cầu chỉnh sửa lên lịch (đã khóa dòng) và phát pf.extension.work_schedule.reviewed.
func (s *Service) applyReview(ctx context.Context, tx pgx.Tx, tid uuid.UUID, sc *domain.Schedule, actor uuid.UUID, act, reason, note string,
	now time.Time, key string, auto bool) error {
	at := now
	sc.ReviewedAt, sc.ReviewerRef = &at, actor
	sc.UpdatedBy, sc.UpdatedAt = actor, now
	if act == domain.ScheduleActApproved {
		sc.Status, sc.ApprovedVersion, sc.Amendment, sc.LastReason = domain.ScheduleApproved, sc.Version, false, ""
	} else {
		sc.Status, sc.LastReason = domain.ScheduleChangesRequested, reason
	}
	ev := s.scheduleEvent(tid, *sc, act, actor, now, key)
	ev.Reason, ev.Note = reason, note
	if err := store.InsertScheduleEvent(ctx, tx, &ev); err != nil {
		return err
	}
	sc.LastEventID, sc.LastSeq = ev.ID, ev.Seq
	decision := port.ScheduleDecisionApproved
	var r *string
	if act == domain.ScheduleActChangeRequested {
		decision, r = port.ScheduleDecisionChangeRequested, &reason
	}
	return outbox.Enqueue(ctx, tx, outbox.Event{
		ID: ids.New(), TenantID: ev.TenantID, Type: port.EventWorkScheduleReviewed, SchemaVersion: 1, OccurredAt: now,
		TraceID: telemetry.TraceID(ctx), Producer: port.EventProducerName,
		Payload: port.WorkScheduleReviewedV1{
			ScheduleID: sc.ID, OwnerRef: sc.OwnerRef, ReviewerRef: actor, DepartmentCode: sc.Department, ProvinceCode: sc.ProvinceCode,
			WeekStart: sc.WeekStart.Format(time.DateOnly), Version: sc.Version, Decision: decision, Reason: r, Auto: auto,
			EventTime: now.UTC().Format(time.RFC3339),
		},
	})
}

func (s *Service) enqueueSubmitted(ctx context.Context, tx pgx.Tx, tid uuid.UUID, sc domain.Schedule, ev domain.ScheduleEvent, approvers []uuid.UUID) error {
	if approvers == nil {
		approvers = []uuid.UUID{}
	}
	return outbox.Enqueue(ctx, tx, outbox.Event{
		ID: ids.New(), TenantID: tid, Type: port.EventWorkScheduleSubmitted, SchemaVersion: 1, OccurredAt: ev.EventTime,
		TraceID: telemetry.TraceID(ctx), Producer: port.EventProducerName,
		Payload: port.WorkScheduleSubmittedV1{
			ScheduleID: sc.ID, OwnerRef: sc.OwnerRef, OwnerLevel: sc.OwnerLevel, DepartmentCode: sc.Department,
			ProvinceCode: sc.ProvinceCode, CommuneCodes: units0(sc.Communes), WeekStart: sc.WeekStart.Format(time.DateOnly),
			Version: sc.Version, Amendment: sc.Amendment, Late: ev.Late, ApproverRefs: approvers,
			EventTime: ev.EventTime.UTC().Format(time.RFC3339),
		},
	})
}

func units0(u []string) []string {
	if u == nil {
		return []string{}
	}
	return u
}

// scheduleReplay trả kết quả của lần gọi trước cùng khóa (nếu có).
func (s *Service) scheduleReplay(ctx context.Context, a knActor, key string) (ScheduleDetail, bool, error) {
	var ev *domain.ScheduleEvent
	err := tenant.WithTenantTx(ctx, s.DB, a.TenantID, func(tx pgx.Tx) error {
		var e error
		ev, e = store.ScheduleEventByIdem(ctx, tx, key)
		return e
	})
	if err != nil || ev == nil {
		return ScheduleDetail{}, false, err
	}
	d, err := s.scheduleDetail(ctx, a, ev.ScheduleID, ev.Version)
	d.Replayed = true
	return d, true, err
}

// ---------------------------------------------------------------------------
// Duyệt
// ---------------------------------------------------------------------------

// ReviewInput là thân "Duyệt" / "Yêu cầu chỉnh sửa".
type ReviewInput struct {
	ID       uuid.UUID
	Decision string // port.ScheduleDecision*
	Version  int
	Reason   string
	Note     string
	IdemKey  string
}

// ReviewSchedule duyệt hoặc yêu cầu chỉnh sửa phiên bản đang Chờ duyệt (FN-TI-02.07). Người duyệt khác người
// đăng ký (R-56), cùng phòng ban, cùng tỉnh, cấp quản lý được duyệt; tuần đã kết thúc → khóa.
func (s *Service) ReviewSchedule(ctx context.Context, in ReviewInput) (ScheduleDetail, error) {
	a, err := s.knActor(ctx)
	if err != nil {
		return ScheduleDetail{}, err
	}
	if a.ReadOnly {
		return ScheduleDetail{}, problem.Forbidden.WithDetail("ngữ cảnh chỉ xem")
	}
	if a.Role != port.KNRoleTinh {
		return ScheduleDetail{}, problem.Forbidden.WithDetail("chỉ Cấp Tỉnh duyệt lịch công tác")
	}
	if in.IdemKey == "" {
		return ScheduleDetail{}, problem.IdempotencyRequired
	}
	act := domain.ScheduleActApproved
	switch in.Decision {
	case port.ScheduleDecisionApproved:
		in.Reason = ""
	case port.ScheduleDecisionChangeRequested:
		act = domain.ScheduleActChangeRequested
		if err := domain.ValidScheduleReason(in.Reason); err != nil {
			return ScheduleDetail{}, fieldProblem(err)
		}
		in.Reason = strings.TrimSpace(in.Reason)
	default:
		return ScheduleDetail{}, problem.Validation.WithField("decision", "approved hoặc change_requested")
	}
	in.Note = strings.TrimSpace(in.Note)
	if len([]rune(in.Note)) > 1000 || domain.ContainsPII(in.Note) {
		return ScheduleDetail{}, problem.Validation.WithField("note", "tối đa 1000 ký tự, không ghi SĐT/CCCD")
	}
	if replay, ok, err := s.scheduleReplay(ctx, a, idemReview(in.IdemKey)); err != nil || ok {
		return replay, err
	}
	now := s.now()
	st, err := s.scheduleSettings(ctx, a.Department, a.ProvinceCode, now)
	if err != nil {
		return ScheduleDetail{}, err
	}
	err = tenant.WithTenantTx(ctx, s.DB, a.TenantID, func(tx pgx.Tx) error {
		sc, err := store.ScheduleByID(ctx, tx, in.ID, true)
		if err != nil {
			return err
		}
		if sc == nil || !a.canView(*sc) {
			return ErrScheduleNotFound
		}
		if err := a.canReview(*sc, st); err != nil {
			return err
		}
		if domain.WeekLocked(sc.WeekStart, now, clock.Zone()) {
			return ErrWeekLocked
		}
		if sc.Status != domain.SchedulePending {
			return ErrScheduleState.WithDetail(domain.ErrScheduleNotPending.Error()).With("status", sc.Status)
		}
		if in.Version != sc.Version {
			return ErrScheduleVersion.With("current_version", sc.Version)
		}
		if err := s.applyReview(ctx, tx, a.TenantID, sc, a.ActorRef, act, in.Reason, in.Note, now, idemReview(in.IdemKey), false); err != nil {
			return err
		}
		return store.UpsertSchedule(ctx, tx, a.TenantID, *sc)
	})
	if errors.Is(ledger.Classify(err), ledger.ErrDuplicate) {
		if replay, ok, rerr := s.scheduleReplay(ctx, a, idemReview(in.IdemKey)); rerr == nil && ok {
			return replay, nil
		}
	}
	if err != nil {
		return ScheduleDetail{}, err
	}
	return s.scheduleDetail(ctx, a, in.ID, 0)
}

// BulkResult là kết quả duyệt hàng loạt.
type BulkResult struct {
	Approved []uuid.UUID
	Skipped  []BulkSkip
}

// BulkSkip là lịch không duyệt được và mã lỗi.
type BulkSkip struct {
	ID   uuid.UUID
	Code string
}

// BulkApprove duyệt hàng loạt (FN-TI-02.07 luồng phụ): mỗi lịch một giao dịch, lịch không duyệt được ghi lý do
// (của chính mình, ngoài phạm vi, không còn chờ duyệt…).
func (s *Service) BulkApprove(ctx context.Context, idsIn []uuid.UUID, key string) (BulkResult, error) {
	a, err := s.knActor(ctx)
	if err != nil {
		return BulkResult{}, err
	}
	if a.ReadOnly {
		return BulkResult{}, problem.Forbidden.WithDetail("ngữ cảnh chỉ xem")
	}
	if a.Role != port.KNRoleTinh {
		return BulkResult{}, problem.Forbidden.WithDetail("chỉ Cấp Tỉnh duyệt lịch công tác")
	}
	if key == "" {
		return BulkResult{}, problem.IdempotencyRequired
	}
	if len(idsIn) == 0 || len(idsIn) > 200 {
		return BulkResult{}, problem.Validation.WithField("ids", "1–200 lịch")
	}
	res := BulkResult{Approved: []uuid.UUID{}, Skipped: []BulkSkip{}}
	seen := map[uuid.UUID]bool{}
	for _, id := range idsIn {
		if seen[id] {
			continue
		}
		seen[id] = true
		var version int
		err := tenant.WithTenantTx(ctx, s.DB, a.TenantID, func(tx pgx.Tx) error {
			sc, err := store.ScheduleByID(ctx, tx, id, false)
			if err != nil || sc == nil {
				return err
			}
			version = sc.Version
			return nil
		})
		if err == nil {
			_, err = s.ReviewSchedule(ctx, ReviewInput{ID: id, Decision: port.ScheduleDecisionApproved, Version: version,
				IdemKey: key + ":" + id.String()})
		}
		var pb *problem.Problem
		switch {
		case err == nil:
			res.Approved = append(res.Approved, id)
		case errors.As(err, &pb):
			res.Skipped = append(res.Skipped, BulkSkip{ID: id, Code: pb.Code})
		default:
			return res, err
		}
	}
	return res, nil
}

// ---------------------------------------------------------------------------
// Đọc
// ---------------------------------------------------------------------------

// GetSchedule đọc chi tiết lịch (version = 0 → hiện hành); ngoài phạm vi → 404.
func (s *Service) GetSchedule(ctx context.Context, id uuid.UUID, version int) (ScheduleDetail, error) {
	a, err := s.knActor(ctx)
	if err != nil {
		return ScheduleDetail{}, err
	}
	if err := a.readable(); err != nil {
		return ScheduleDetail{}, err
	}
	return s.scheduleDetail(ctx, a, id, version)
}

func (s *Service) scheduleDetail(ctx context.Context, a knActor, id uuid.UUID, version int) (ScheduleDetail, error) {
	var d ScheduleDetail
	var events []domain.ScheduleEvent
	found := false
	err := tenant.WithTenantTx(ctx, s.DB, a.TenantID, func(tx pgx.Tx) error {
		sc, err := store.ScheduleByID(ctx, tx, id, false)
		if err != nil || sc == nil || !a.canView(*sc) {
			return err
		}
		found, d.Schedule = true, *sc
		if version <= 0 || version > sc.Version {
			version = sc.Version
		}
		d.ViewingVersion = version
		if d.Items, err = store.ScheduleItems(ctx, tx, id, version); err != nil {
			return err
		}
		if sc.ApprovedVersion > 0 && sc.ApprovedVersion != version {
			if d.ApprovedItems, err = store.ScheduleItems(ctx, tx, id, sc.ApprovedVersion); err != nil {
				return err
			}
		}
		events, err = store.ScheduleEvents(ctx, tx, id)
		return err
	})
	if err != nil {
		return ScheduleDetail{}, err
	}
	if !found {
		return ScheduleDetail{}, ErrScheduleNotFound
	}
	now := s.now()
	st, err := s.scheduleSettings(ctx, d.Schedule.Department, d.Schedule.ProvinceCode, now)
	if err != nil {
		return ScheduleDetail{}, err
	}
	s.decorate(ctx, a, &d, st, now)
	s.decorateCheckins(ctx, a, &d, st, now)
	for _, e := range events {
		if e.Action == domain.ScheduleActDraftSaved || e.Action == domain.ScheduleActSubmitted || e.Action == domain.ScheduleActAmended {
			d.Versions = append(d.Versions, ScheduleVersion{Version: e.Version, Action: e.Action, At: e.EventTime, ActorRef: e.ActorRef, ItemCount: e.ItemCount})
		}
	}
	refs := []uuid.UUID{d.Schedule.OwnerRef, d.Schedule.ReviewerRef}
	for _, v := range d.Versions {
		refs = append(refs, v.ActorRef)
	}
	d.Names = s.names(ctx, refs)
	return d, nil
}

// decorate tính trường suy diễn: khóa, hạn, cảnh báo, đánh dấu điều chỉnh, thao tác được phép.
func (s *Service) decorate(ctx context.Context, a knActor, d *ScheduleDetail, st ScheduleSettings, now time.Time) {
	_ = ctx
	sc := d.Schedule
	d.Locked = domain.WeekLocked(sc.WeekStart, now, clock.Zone())
	d.SubmitDue, d.ReviewDue = st.SubmitDue(sc.WeekStart), st.ReviewDue(sc.WeekStart)
	d.ReviewOverdue = sc.Status == domain.SchedulePending && now.After(d.ReviewDue)
	d.Conflicts = domain.Conflicts(d.Items)
	d.Changed = map[uuid.UUID]bool{}
	if len(d.ApprovedItems) > 0 {
		d.Changed = domain.ChangedSince(d.Items, d.ApprovedItems)
	}
	if len(d.Conflicts) > 0 {
		var idsC []uuid.UUID
		for _, it := range d.Items {
			if d.Conflicts[it.ItemID] {
				idsC = append(idsC, it.ItemID)
			}
		}
		d.Warnings = append(d.Warnings, ScheduleWarning{Code: "time_overlap", Message: "Có mục công tác trùng thời gian", ItemIDs: idsC})
	}
	if sc.Late {
		d.Warnings = append(d.Warnings, ScheduleWarning{Code: "late_registration", Message: "Đăng ký muộn (gửi sau hạn đăng ký)", ItemIDs: []uuid.UUID{}})
	}
	own := sc.OwnerRef == a.ActorRef
	if !d.Locked && own && (a.Role == port.KNRoleXa || a.Role == port.KNRoleTinh) {
		d.Can.Submit = true
		d.Can.Edit = true
	}
	if !d.Locked && sc.Status == domain.SchedulePending && a.canReview(sc, st) == nil {
		d.Can.Approve, d.Can.RequestChanges = true, true
	}
}

// names tra tên hiển thị (danh bạ cán bộ) — lỗi tra cứu không chặn đọc.
func (s *Service) names(ctx context.Context, refs []uuid.UUID) map[uuid.UUID]string {
	var uniq []uuid.UUID
	seen := map[uuid.UUID]bool{}
	for _, r := range refs {
		if r != uuid.Nil && !seen[r] {
			seen[r] = true
			uniq = append(uniq, r)
		}
	}
	m, err := s.DisplayNames(ctx, uniq)
	if err != nil || m == nil {
		m = map[uuid.UUID]string{}
	}
	m[SystemActor] = "Hệ thống"
	return m
}

// weekOrDefault đọc tuần (YYYY-MM-DD, Thứ Hai); rỗng → tuần kế tiếp.
func (s *Service) weekOrDefault(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return domain.NextWeekStart(s.now(), clock.Zone()), nil
	}
	w, err := domain.ParseDate(raw)
	if err != nil || !domain.ValidWeekStart(w) {
		return time.Time{}, problem.Validation.WithField("week_start", domain.ErrWeekNotMonday.Error())
	}
	return w, nil
}

// ScheduleQuery là tham số danh sách.
type ScheduleQuery struct {
	Week       string
	Status     string
	Commune    string
	OwnerLevel string
	After      uuid.UUID
	Limit      int
}

// SchedulePage là một trang danh sách.
type SchedulePage struct {
	Week  time.Time
	Items []ScheduleDetail
	Next  *uuid.UUID
	Names map[uuid.UUID]string
}

func (a knActor) filter(week time.Time) store.ScheduleFilter {
	f := store.ScheduleFilter{Week: week, Department: a.Department, Province: a.ProvinceCode}
	switch a.Role {
	case port.KNRoleAdmin:
		f.All = true
	case port.KNRoleXa:
		f.Owner = a.ActorRef
	}
	return f
}

// ListSchedules liệt kê lịch của tuần trong phạm vi (tab Chờ duyệt / Đã duyệt của Cấp Tỉnh; Cấp Xã: lịch của mình).
func (s *Service) ListSchedules(ctx context.Context, q ScheduleQuery) (SchedulePage, error) {
	a, err := s.knActor(ctx)
	if err != nil {
		return SchedulePage{}, err
	}
	if err := a.readable(); err != nil {
		return SchedulePage{}, err
	}
	week, err := s.weekOrDefault(q.Week)
	if err != nil {
		return SchedulePage{}, err
	}
	switch q.Status {
	case "", domain.ScheduleDraft, domain.SchedulePending, domain.ScheduleApproved, domain.ScheduleChangesRequested:
	default:
		return SchedulePage{}, problem.Validation.WithField("status", "draft, pending, approved hoặc changes_requested")
	}
	if q.OwnerLevel != "" && q.OwnerLevel != accountsport.ScheduleLevelCommune && q.OwnerLevel != accountsport.ScheduleLevelProvince {
		return SchedulePage{}, problem.Validation.WithField("owner_level", "commune hoặc province")
	}
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 50
	}
	f := a.filter(week)
	f.Status, f.Commune, f.OwnerLevel, f.After, f.Limit = q.Status, strings.TrimSpace(q.Commune), q.OwnerLevel, q.After, q.Limit+1
	if a.Role == port.KNRoleTinh && a.ProvinceCode == "" {
		return SchedulePage{Week: week, Items: []ScheduleDetail{}}, nil
	}
	var list []domain.Schedule
	var items map[uuid.UUID][]domain.ScheduleItem
	var checkins map[uuid.UUID]store.Checkin
	err = tenant.WithTenantTx(ctx, s.DB, a.TenantID, func(tx pgx.Tx) error {
		var e error
		if list, e = store.ListSchedules(ctx, tx, f); e != nil {
			return e
		}
		if len(list) > q.Limit {
			list = list[:q.Limit+1]
		}
		idl, vers := make([]uuid.UUID, 0, len(list)), make([]int, 0, len(list))
		for _, sc := range list {
			idl, vers = append(idl, sc.ID), append(vers, sc.Version)
		}
		if items, e = store.ScheduleItemsOf(ctx, tx, idl, vers); e != nil {
			return e
		}
		checkins, e = store.CheckinsOf(ctx, tx, idl)
		return e
	})
	if err != nil {
		return SchedulePage{}, err
	}
	pg := SchedulePage{Week: week, Items: []ScheduleDetail{}}
	if len(list) > q.Limit {
		list = list[:q.Limit]
		last := list[q.Limit-1].ID
		pg.Next = &last
	}
	now := s.now()
	stCache := map[string]ScheduleSettings{}
	var refs []uuid.UUID
	for _, sc := range list {
		ck := sc.Department + ":" + sc.ProvinceCode
		st, ok := stCache[ck]
		if !ok {
			if st, err = s.scheduleSettings(ctx, sc.Department, sc.ProvinceCode, now); err != nil {
				return SchedulePage{}, err
			}
			stCache[ck] = st
		}
		d := ScheduleDetail{Schedule: sc, ViewingVersion: sc.Version, Items: items[sc.ID]}
		s.decorate(ctx, a, &d, st, now)
		applyCheckins(a, &d, st, now, checkins)
		pg.Items = append(pg.Items, d)
		refs = append(refs, sc.OwnerRef, sc.ReviewerRef)
	}
	pg.Names = s.names(ctx, refs)
	return pg, nil
}

// MySchedule là lịch của người gọi cho một tuần.
type MySchedule struct {
	Week              time.Time
	SubmitDue         time.Time
	ReviewDue         time.Time
	Locked            bool
	LateNow           bool
	Detail            *ScheduleDetail
	PreviousWeek      time.Time
	PreviousAvailable bool
	// CheckinEnabled: phòng ban của người gọi dùng check-in GPS (W11.46) — Mobile hiện nút "Check-in".
	CheckinEnabled bool
	// OwnerLevel, Communes: cấp và xã chọn được khi đăng ký lịch, không phụ thuộc lịch tuần đã có (tuần mới
	// chưa đăng ký vẫn biết xã để chọn — BF-SCHED R-B1). Cán bộ xã: xã phụ trách theo phiên KN; cán bộ tỉnh:
	// mọi xã hiện hành thuộc tỉnh (quyết định PO 25/10/2026). CommuneOptions: cùng danh sách, kèm tên xã.
	OwnerLevel     string
	Communes       []string
	CommuneOptions []CommuneRef
}

// CommuneRef là một xã chọn được khi đăng ký lịch (mã, tên đơn vị hành chính).
type CommuneRef struct {
	Code string
	Name string
}

// scheduleCommunes trả xã người gọi chọn được khi đăng ký lịch (BF-SCHED R-B1, quyết định PO 25/10/2026: cán bộ
// tỉnh cũng bắt buộc Xã/phường): cán bộ xã → xã phụ trách theo phiên KN; cán bộ tỉnh → mọi xã hiện hành thuộc
// tỉnh (cây đơn vị hành chính). withNames: tra tên xã (chỉ `mine` cần; lưu lịch chỉ cần mã).
func (s *Service) scheduleCommunes(ctx context.Context, a knActor, withNames bool) ([]CommuneRef, error) {
	now := s.now()
	var codes []string
	switch a.Role {
	case port.KNRoleXa:
		codes = slices.Clone(a.Communes)
	case port.KNRoleTinh:
		if s.Units == nil {
			return nil, ErrDependency.WithDetail("chưa nối danh mục đơn vị hành chính")
		}
		if a.ProvinceCode == "" {
			return []CommuneRef{}, nil
		}
		if tree, ok := s.Units.(partyport.AdminUnitTree); ok {
			us, err := tree.Children(ctx, a.ProvinceCode, now)
			if err != nil {
				return nil, err
			}
			out := make([]CommuneRef, 0, len(us))
			for _, u := range us {
				if u.Level == partyport.LevelCommune {
					out = append(out, CommuneRef{Code: u.Code, Name: u.Name})
				}
			}
			slices.SortFunc(out, func(x, y CommuneRef) int { return strings.Compare(x.Code, y.Code) })
			return out, nil
		}
		ds, err := s.Units.Descendants(ctx, a.ProvinceCode, now)
		if err != nil {
			return nil, err
		}
		for _, c := range ds {
			if c != a.ProvinceCode {
				codes = append(codes, c)
			}
		}
		slices.Sort(codes)
		withNames = true // cần Get để biết cấp (Descendants không trả cấp)
	}
	out := make([]CommuneRef, 0, len(codes))
	for _, c := range codes {
		r := CommuneRef{Code: c, Name: c}
		if withNames && s.Units != nil {
			u, err := s.Units.Get(ctx, c, now)
			if err == nil && a.Role == port.KNRoleTinh && u.Level != partyport.LevelCommune {
				continue
			}
			if err == nil && u.Name != "" {
				r.Name = u.Name
			}
		}
		out = append(out, r)
	}
	return out, nil
}

func communeCodes(rs []CommuneRef) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Code
	}
	return out
}

// GetMySchedule trả lịch tuần của người gọi (mặc định tuần kế tiếp) kèm hạn đăng ký (FN-XA-03.08 thiết kế:
// "trạng thái + hạn đăng ký").
func (s *Service) GetMySchedule(ctx context.Context, weekRaw string) (MySchedule, error) {
	a, err := s.knActor(ctx)
	if err != nil {
		return MySchedule{}, err
	}
	if err := a.registrant(); err != nil {
		return MySchedule{}, err
	}
	week, err := s.weekOrDefault(weekRaw)
	if err != nil {
		return MySchedule{}, err
	}
	now := s.now()
	st, err := s.scheduleSettings(ctx, a.Department, a.ProvinceCode, now)
	if err != nil {
		return MySchedule{}, err
	}
	m := MySchedule{Week: week, SubmitDue: st.SubmitDue(week), ReviewDue: st.ReviewDue(week),
		Locked: domain.WeekLocked(week, now, clock.Zone()), PreviousWeek: week.AddDate(0, 0, -7), CheckinEnabled: st.Checkin,
		OwnerLevel: a.level()}
	if m.CommuneOptions, err = s.scheduleCommunes(ctx, a, true); err != nil {
		return MySchedule{}, err
	}
	m.Communes = communeCodes(m.CommuneOptions)
	var cur, prev *domain.Schedule
	err = tenant.WithTenantTx(ctx, s.DB, a.TenantID, func(tx pgx.Tx) error {
		var e error
		if cur, e = store.ScheduleByOwnerWeek(ctx, tx, a.ActorRef, a.Department, a.level(), week, false); e != nil {
			return e
		}
		prev, e = store.ScheduleByOwnerWeek(ctx, tx, a.ActorRef, a.Department, a.level(), m.PreviousWeek, false)
		return e
	})
	if err != nil {
		return MySchedule{}, err
	}
	m.PreviousAvailable = prev != nil && prev.ItemCount > 0
	m.LateNow = now.After(m.SubmitDue) && (cur == nil || cur.FirstSubmitted == nil)
	if cur != nil {
		d, err := s.scheduleDetail(ctx, a, cur.ID, 0)
		if err != nil {
			return MySchedule{}, err
		}
		m.Detail = &d
	}
	return m, nil
}

// CopyPreviousWeek tạo nháp tuần week từ lịch tuần trước (ngày dời +7) — FN-XA-03.08 luồng phụ.
func (s *Service) CopyPreviousWeek(ctx context.Context, weekRaw, key string) (ScheduleDetail, error) {
	a, err := s.knActor(ctx)
	if err != nil {
		return ScheduleDetail{}, err
	}
	if a.ReadOnly {
		return ScheduleDetail{}, problem.Forbidden.WithDetail("ngữ cảnh chỉ xem")
	}
	if err := a.registrant(); err != nil {
		return ScheduleDetail{}, err
	}
	if strings.TrimSpace(weekRaw) == "" {
		return ScheduleDetail{}, problem.Validation.WithField("week_start", "bắt buộc")
	}
	week, err := s.weekOrDefault(weekRaw)
	if err != nil {
		return ScheduleDetail{}, err
	}
	if replay, ok, err := s.scheduleReplay(ctx, a, idemSave(key)); err != nil || ok {
		return replay, err
	}
	var prevItems []domain.ScheduleItem
	var cur *domain.Schedule
	err = tenant.WithTenantTx(ctx, s.DB, a.TenantID, func(tx pgx.Tx) error {
		prev, err := store.ScheduleByOwnerWeek(ctx, tx, a.ActorRef, a.Department, a.level(), week.AddDate(0, 0, -7), false)
		if err != nil || prev == nil {
			return err
		}
		v := prev.Version
		if prev.ApprovedVersion > 0 {
			v = prev.ApprovedVersion // ưu tiên bản đã duyệt
		}
		if prevItems, err = store.ScheduleItems(ctx, tx, prev.ID, v); err != nil {
			return err
		}
		cur, err = store.ScheduleByOwnerWeek(ctx, tx, a.ActorRef, a.Department, a.level(), week, false)
		return err
	})
	if err != nil {
		return ScheduleDetail{}, err
	}
	if len(prevItems) == 0 {
		return ScheduleDetail{}, ErrScheduleNoPrevious
	}
	items := make([]domain.ScheduleItem, 0, len(prevItems))
	for _, it := range prevItems {
		it.ItemID, it.RowID, it.SupersedesID = uuid.Nil, uuid.Nil, nil
		it.Day = it.Day.AddDate(0, 0, 7)
		items = append(items, it)
	}
	in := ScheduleSaveInput{WeekStart: week, Action: domain.SaveDraft, Items: items, IdemKey: key}
	if cur != nil {
		v := cur.Version
		in.BaseVersion = &v
		// Giữ các mục đang có, thêm mục sao chép (người dùng tự bỏ mục thừa).
		have, err := s.currentItems(ctx, a.TenantID, cur.ID, cur.Version)
		if err != nil {
			return ScheduleDetail{}, err
		}
		in.Items = append(have, items...)
	}
	return s.SaveSchedule(ctx, in)
}

func (s *Service) currentItems(ctx context.Context, tid, id uuid.UUID, version int) ([]domain.ScheduleItem, error) {
	var out []domain.ScheduleItem
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		var e error
		out, e = store.ScheduleItems(ctx, tx, id, version)
		return e
	})
	return out, err
}

// ScheduleCounts là số đếm theo tab.
type ScheduleCounts struct {
	Week                                                     time.Time
	Draft, Pending, Approved, ChangesRequested, Unregistered int
	PendingOpenWeeks                                         int
}

// CountSchedules đếm lịch theo trạng thái của tuần, số chưa đăng ký, số chờ duyệt của mọi tuần chưa kết thúc mà
// người gọi duyệt được (số đếm menu "Lịch công tác", badge extension.work_schedules.pending).
func (s *Service) CountSchedules(ctx context.Context, weekRaw string) (ScheduleCounts, error) {
	a, err := s.knActor(ctx)
	if err != nil {
		return ScheduleCounts{}, err
	}
	if err := a.readable(); err != nil {
		return ScheduleCounts{}, err
	}
	week, err := s.weekOrDefault(weekRaw)
	if err != nil {
		return ScheduleCounts{}, err
	}
	c := ScheduleCounts{Week: week}
	if a.Role == port.KNRoleTinh && a.ProvinceCode == "" {
		return c, nil
	}
	now := s.now()
	reviewer := false
	if a.Role == port.KNRoleTinh {
		st, err := s.scheduleSettings(ctx, a.Department, a.ProvinceCode, now)
		if err != nil {
			return c, err
		}
		reviewer = a.mayApprove(st)
	}
	err = tenant.WithTenantTx(ctx, s.DB, a.TenantID, func(tx pgx.Tx) error {
		m, err := store.ScheduleStatusCounts(ctx, tx, a.filter(week))
		if err != nil {
			return err
		}
		c.Draft, c.Pending, c.Approved, c.ChangesRequested = m[domain.ScheduleDraft], m[domain.SchedulePending],
			m[domain.ScheduleApproved], m[domain.ScheduleChangesRequested]
		if reviewer {
			c.PendingOpenWeeks, err = store.PendingOpenWeeks(ctx, tx, a.Department, a.ProvinceCode,
				domain.WeekStartOf(now, clock.Zone()), a.ActorRef)
		}
		return err
	})
	if err != nil {
		return c, err
	}
	if a.Role == port.KNRoleTinh {
		un, err := s.unregistered(ctx, a, week, "")
		if err != nil {
			return c, err
		}
		c.Unregistered = len(un)
	}
	return c, nil
}

// Unregistered là một cán bộ chưa gửi lịch tuần.
type Unregistered struct {
	Officer    accountsport.KNOfficer
	Status     string // none | draft
	ScheduleID uuid.UUID
}

// ListUnregistered là tab "Chưa đăng ký" (FN-TI-02.07): cán bộ cùng phòng ban, cùng tỉnh chưa gửi lịch tuần.
func (s *Service) ListUnregistered(ctx context.Context, weekRaw, commune string) (time.Time, []Unregistered, map[uuid.UUID]string, error) {
	a, err := s.knActor(ctx)
	if err != nil {
		return time.Time{}, nil, nil, err
	}
	if a.Role != port.KNRoleTinh && a.Role != port.KNRoleAdmin {
		return time.Time{}, nil, nil, problem.Forbidden.WithDetail("chỉ Cấp Tỉnh xem danh sách chưa đăng ký")
	}
	week, err := s.weekOrDefault(weekRaw)
	if err != nil {
		return time.Time{}, nil, nil, err
	}
	if a.Role == port.KNRoleAdmin || a.ProvinceCode == "" {
		return week, []Unregistered{}, map[uuid.UUID]string{}, nil // Admin không gắn phòng ban/tỉnh
	}
	out, err := s.unregistered(ctx, a, week, strings.TrimSpace(commune))
	if err != nil {
		return week, nil, nil, err
	}
	refs := make([]uuid.UUID, 0, len(out))
	for _, u := range out {
		refs = append(refs, u.Officer.ActorRef)
	}
	return week, out, s.names(ctx, refs), nil
}

func (s *Service) unregistered(ctx context.Context, a knActor, week time.Time, commune string) ([]Unregistered, error) {
	offs, err := s.knOfficers(ctx, a.TenantID, a.Department, a.ProvinceCode)
	if err != nil {
		return nil, err
	}
	var list []domain.Schedule
	err = tenant.WithTenantTx(ctx, s.DB, a.TenantID, func(tx pgx.Tx) error {
		var e error
		list, e = store.SchedulesOfWeek(ctx, tx, a.Department, a.ProvinceCode, week)
		return e
	})
	if err != nil {
		return nil, err
	}
	byOwner := map[uuid.UUID]domain.Schedule{}
	for _, sc := range list {
		byOwner[sc.OwnerRef] = sc
	}
	out := []Unregistered{}
	for _, o := range offs {
		if commune != "" && !slices.Contains(o.Communes, commune) {
			continue
		}
		sc, ok := byOwner[o.ActorRef]
		switch {
		case !ok:
			out = append(out, Unregistered{Officer: o, Status: "none"})
		case sc.Status == domain.ScheduleDraft:
			out = append(out, Unregistered{Officer: o, Status: "draft", ScheduleID: sc.ID})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ci, cj := strings.Join(out[i].Officer.Communes, ","), strings.Join(out[j].Officer.Communes, ",")
		if out[i].Officer.Level != out[j].Officer.Level {
			return out[i].Officer.Level == accountsport.ScheduleLevelCommune
		}
		return ci < cj
	})
	return out, nil
}

// ---------------------------------------------------------------------------
// Log (khung Log của màn Detail, kế hoạch §1.5)
// ---------------------------------------------------------------------------

// LogEntry là một dòng Log.
type LogEntry struct {
	Seq        int64
	Event      string
	ActorRef   uuid.UUID
	At         time.Time
	RecordedAt time.Time
	Changes    []domain.FieldChange
	Reason     string
}

// ScheduleLog trả Log của lịch, mới nhất trước; before > 0 → các dòng có seq < before.
func (s *Service) ScheduleLog(ctx context.Context, id uuid.UUID, before int64, limit int) ([]LogEntry, *int64, map[uuid.UUID]string, error) {
	a, err := s.knActor(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := a.readable(); err != nil {
		return nil, nil, nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var events []domain.ScheduleEvent
	var items map[int][]domain.ScheduleItem
	found := false
	err = tenant.WithTenantTx(ctx, s.DB, a.TenantID, func(tx pgx.Tx) error {
		sc, err := store.ScheduleByID(ctx, tx, id, false)
		if err != nil || sc == nil || !a.canView(*sc) {
			return err
		}
		found = true
		if events, err = store.ScheduleEvents(ctx, tx, id); err != nil {
			return err
		}
		items = map[int][]domain.ScheduleItem{}
		for v := 1; v <= sc.Version; v++ {
			if items[v], err = store.ScheduleItems(ctx, tx, id, v); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, nil, err
	}
	if !found {
		return nil, nil, nil, ErrScheduleNotFound
	}
	all := make([]LogEntry, 0, len(events))
	status, lastVersion := "", 0
	for _, e := range events {
		le := LogEntry{Seq: e.Seq, Event: e.Action, ActorRef: e.ActorRef, At: e.EventTime, RecordedAt: e.RecordedAt, Reason: e.Reason}
		if e.StatusAfter != status && e.Action != domain.ScheduleActLocked {
			var old *string
			if status != "" {
				o := domain.StatusVI(status)
				old = &o
			}
			n := domain.StatusVI(e.StatusAfter)
			le.Changes = append(le.Changes, domain.FieldChange{Field: "Trạng thái", Old: old, New: &n})
		}
		switch e.Action {
		case domain.ScheduleActDraftSaved, domain.ScheduleActSubmitted, domain.ScheduleActAmended:
			le.Changes = append(le.Changes, domain.DiffItems(items[lastVersion], items[e.Version])...)
			lastVersion = e.Version
			if e.Late {
				yes := "Có"
				le.Changes = append(le.Changes, domain.FieldChange{Field: "Đăng ký muộn", New: &yes})
			}
		case domain.ScheduleActLocked:
			n := "Đã khóa (tuần đã kết thúc)"
			le.Changes = append(le.Changes, domain.FieldChange{Field: "Khóa", New: &n})
		}
		if e.Note != "" {
			n := e.Note
			le.Changes = append(le.Changes, domain.FieldChange{Field: "Ghi chú duyệt", New: &n})
		}
		status = e.StatusAfter
		all = append(all, le)
	}
	slices.Reverse(all)
	var page []LogEntry
	for _, le := range all {
		if before > 0 && le.Seq >= before {
			continue
		}
		page = append(page, le)
		if len(page) == limit+1 {
			break
		}
	}
	var next *int64
	if len(page) > limit {
		page = page[:limit]
		n := page[limit-1].Seq
		next = &n
	}
	refs := make([]uuid.UUID, 0, len(page))
	for _, le := range page {
		refs = append(refs, le.ActorRef)
	}
	if page == nil {
		page = []LogEntry{}
	}
	return page, next, s.names(ctx, refs), nil
}
