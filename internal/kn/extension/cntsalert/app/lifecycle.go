package app

// Vòng đời cảnh báo (Mục V): Mới → Đã giao → Đang xử lý → Đã đóng (ghi kết quả); quá hạn xử lý → Quá hạn (báo Lãnh đạo
// phòng). Cấp Tỉnh giao; người được giao xử lý; khóa lạc quan theo số sự việc cuối (base_seq).

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsalert/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsalert/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/txdeps"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// ListFilter là bộ lọc danh sách cảnh báo.
type ListFilter struct {
	Status, Kind, Level, Template string
	Mine                          bool // chỉ cảnh báo giao cho tôi
	Limit                         int
}

// AlertView là cảnh báo kèm tên người xử lý và cờ quá hạn.
type AlertView struct {
	store.Alert
	Assignee *Actor
	Overdue  bool
	// Quyền của người gọi trên cảnh báo (màn hiển thị nút).
	CanAssign, CanStart, CanClose bool
}

// Detail là cảnh báo kèm lịch sử.
type Detail struct {
	AlertView
	History []HistoryItem
}

// HistoryItem là một sự việc của lịch sử.
type HistoryItem struct {
	store.Event
	Actor    Actor
	Assignee *Actor
}

var listStatuses = []string{"", "open", "overdue", domain.StatusNew, domain.StatusAssigned, domain.StatusInProgress, domain.StatusClosed}

func (s *Service) view(c caller, a store.Alert, name func(uuid.UUID) Actor, now time.Time) AlertView {
	v := AlertView{Alert: a, Overdue: a.Status != domain.StatusClosed && now.After(a.DueAt)}
	if a.AssigneeRef != nil {
		x := name(*a.AssigneeRef)
		v.Assignee = &x
	}
	if a.Status != domain.StatusClosed && c.checkWrite() == nil {
		mgr := c.admin() || c.tinh()
		mine := a.AssigneeRef != nil && *a.AssigneeRef == c.p.ActorRef
		v.CanAssign = mgr
		v.CanStart = a.Status == domain.StatusAssigned && (mine || c.admin())
		v.CanClose = mgr || mine
	}
	return v
}

// List liệt kê cảnh báo trong phạm vi người gọi (khẩn và chưa đóng trước).
func (s *Service) List(ctx context.Context, f ListFilter) ([]AlertView, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	if !containsStr(listStatuses, f.Status) {
		return nil, problem.Validation.WithField("status", "open, overdue, new, assigned, in_progress, closed")
	}
	if f.Kind != "" {
		if _, ok := domain.KindOf(f.Kind); !ok {
			return nil, problem.Validation.WithField("kind", "loại cảnh báo không có")
		}
	}
	if f.Level != "" && f.Level != domain.LevelUrgent && f.Level != domain.LevelAttention {
		return nil, problem.Validation.WithField("level", "urgent hoặc attention")
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 200
	}
	now := s.now()
	sf := store.Filter{Department: domain.Department, Status: f.Status, Kind: f.Kind, Level: f.Level, Template: f.Template, Now: now, Limit: f.Limit}
	if f.Mine {
		me := c.p.ActorRef
		sf.Assignee = &me
	}
	var rows []store.Alert
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.List(ctx, tx, sf)
		return e
	})
	if err != nil {
		return nil, err
	}
	var refs []uuid.UUID
	vis := rows[:0]
	for _, a := range rows {
		if !c.sees(a) {
			continue
		}
		vis = append(vis, a)
		if a.AssigneeRef != nil {
			refs = append(refs, *a.AssigneeRef)
		}
	}
	name := s.actors(ctx, refs)
	out := make([]AlertView, 0, len(vis))
	for _, a := range vis {
		out = append(out, s.view(c, a, name, now))
	}
	return out, nil
}

// Get đọc cảnh báo kèm lịch sử.
func (s *Service) Get(ctx context.Context, ref uuid.UUID) (Detail, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return Detail{}, err
	}
	var a *store.Alert
	var evs []store.Event
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if a, e = store.ByRef(ctx, tx, ref); e != nil || a == nil {
			return e
		}
		evs, e = store.Events(ctx, tx, ref)
		return e
	})
	if err != nil {
		return Detail{}, err
	}
	if a == nil || !c.sees(*a) {
		return Detail{}, ErrAlertNotFound
	}
	refs := []uuid.UUID{}
	if a.AssigneeRef != nil {
		refs = append(refs, *a.AssigneeRef)
	}
	for _, e := range evs {
		refs = append(refs, e.ActorRef)
		if e.AssigneeRef != nil {
			refs = append(refs, *e.AssigneeRef)
		}
	}
	name := s.actors(ctx, dedupRefs(refs))
	d := Detail{AlertView: s.view(c, *a, name, s.now())}
	for _, e := range evs {
		h := HistoryItem{Event: e, Actor: name(e.ActorRef)}
		if e.AssigneeRef != nil && (e.Action == domain.ActionAssigned) {
			x := name(*e.AssigneeRef)
			h.Assignee = &x
		}
		d.History = append(d.History, h)
	}
	return d, nil
}

// ActionInput là dữ liệu của một thao tác vòng đời.
type ActionInput struct {
	BaseSeq  int64 // số sự việc cuối người gọi đã thấy (khóa lạc quan; 0 = không kiểm)
	Assignee uuid.UUID
	DueAt    *time.Time
	Note     string
}

// change chạy một thao tác vòng đời trên cảnh báo ref.
func (s *Service) change(ctx context.Context, c caller, ref uuid.UUID, act string, in ActionInput, idem string,
	check func(a store.Alert) error) (Detail, error) {
	now := s.now()
	onBehalf, support := c.support()
	err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if err := store.Lock(ctx, tx, ref.String()); err != nil {
			return err
		}
		a, err := store.ByRef(ctx, tx, ref)
		if err != nil {
			return err
		}
		if a == nil || !c.sees(*a) {
			return ErrAlertNotFound
		}
		if idem != "" {
			if done, err := store.EventByIdem(ctx, tx, ref, idem); err != nil || done {
				return err
			}
		}
		if in.BaseSeq > 0 && in.BaseSeq != a.LastSeq {
			return ErrAlertConflict.WithDetail(fmt.Sprintf("Đã có người cập nhật lúc %s", a.UpdatedAt.In(time.FixedZone("ICT", 7*3600)).Format("15:04 02/01/2006")))
		}
		if err := check(*a); err != nil {
			return err
		}
		ai := applyInput{Note: in.Note, Actor: c.p.ActorRef, OnBehalf: onBehalf, Support: support, At: now, Idem: idem}
		if ai.Idem == "" {
			ai.Idem = act + ":" + ref.String() + ":" + itoa(a.LastSeq)
		}
		if act == domain.ActionAssigned {
			as := in.Assignee
			due := now.Add(time.Duration(s.alertSetting(ctx, tx, a.Kind).SLAHours) * time.Hour)
			if in.DueAt != nil {
				due = in.DueAt.UTC()
			}
			ai.Assignee, ai.DueAt = &as, &due
		}
		if _, err := s.apply(ctx, tx, c.p.TenantID, a, act, ai); err != nil {
			return err
		}
		if act == domain.ActionAssigned && in.Assignee != c.p.ActorRef {
			return emitChanged(ctx, tx, c.p.TenantID, *a, port.CNTSAlertAssigned, []uuid.UUID{in.Assignee}, now)
		}
		return nil
	})
	if err != nil {
		if dup(err) {
			return s.Get(ctx, ref)
		}
		return Detail{}, err
	}
	return s.Get(ctx, ref)
}

// Assign giao cảnh báo cho người xử lý (Cấp Tỉnh), kèm hạn xử lý (mặc định theo loại — UR-TINH-13 "Cấp Tỉnh đặt").
func (s *Service) Assign(ctx context.Context, ref uuid.UUID, in ActionInput, idem string) (Detail, error) {
	c, err := s.manager(ctx)
	if err != nil {
		return Detail{}, err
	}
	if in.Assignee == uuid.Nil {
		return Detail{}, ErrAlertInvalid.WithField("assignee_ref", "bắt buộc")
	}
	in.Note = strings.TrimSpace(in.Note)
	if utf8.RuneCountInString(in.Note) > 1000 {
		return Detail{}, ErrAlertInvalid.WithField("note", "tối đa 1000 ký tự")
	}
	now := s.now()
	if in.DueAt != nil && !in.DueAt.After(now) {
		return Detail{}, ErrAlertInvalid.WithField("due_at", "hạn xử lý phải sau hiện tại")
	}
	if in.DueAt != nil && in.DueAt.Sub(now) > 30*24*time.Hour {
		return Detail{}, ErrAlertInvalid.WithField("due_at", "tối đa 30 ngày")
	}
	return s.change(ctx, c, ref, domain.ActionAssigned, in, idem, func(a store.Alert) error {
		return s.checkAssignee(ctx, c, a, in.Assignee)
	})
}

// checkAssignee: người được giao là cán bộ của phòng trong tỉnh (Cấp Xã phải phụ trách xã của cảnh báo). Chưa nối danh
// bạ → chấp nhận (dev/test).
func (s *Service) checkAssignee(ctx context.Context, c caller, a store.Alert, ref uuid.UUID) error {
	if s.Dir == nil || a.ProvinceCode == "" {
		return nil
	}
	os, err := s.Dir.Officers(ctx, c.p.TenantID, a.ProvinceCode)
	if err != nil {
		return err
	}
	for _, o := range os {
		if o.ActorRef != ref {
			continue
		}
		if o.Level == "commune" && a.CommuneCode != "" && !containsStr(o.Communes, a.CommuneCode) {
			continue
		}
		return nil
	}
	return ErrAlertInvalid.WithField("assignee_ref", "không phải cán bộ phòng Chăn nuôi – Thủy sản của tỉnh (Cấp Xã phải phụ trách xã của cảnh báo)")
}

// Start: người được giao nhận xử lý (Đã giao → Đang xử lý).
func (s *Service) Start(ctx context.Context, ref uuid.UUID, in ActionInput, idem string) (Detail, error) {
	c, err := s.writer(ctx)
	if err != nil {
		return Detail{}, err
	}
	in.Note = strings.TrimSpace(in.Note)
	if utf8.RuneCountInString(in.Note) > 1000 {
		return Detail{}, ErrAlertInvalid.WithField("note", "tối đa 1000 ký tự")
	}
	return s.change(ctx, c, ref, domain.ActionStarted, in, idem, func(a store.Alert) error {
		if !c.admin() && (a.AssigneeRef == nil || *a.AssigneeRef != c.p.ActorRef) {
			return problem.Forbidden.WithDetail("chỉ người được giao nhận xử lý cảnh báo")
		}
		return nil
	})
}

// Close đóng cảnh báo với kết quả xử lý (≥ 10 ký tự): người được giao, Cấp Tỉnh, Admin.
func (s *Service) Close(ctx context.Context, ref uuid.UUID, in ActionInput, idem string) (Detail, error) {
	c, err := s.writer(ctx)
	if err != nil {
		return Detail{}, err
	}
	in.Note = strings.TrimSpace(in.Note)
	if n := utf8.RuneCountInString(in.Note); n < domain.MinResultLen || n > 2000 {
		return Detail{}, ErrAlertInvalid.WithField("result", "kết quả xử lý 10–2000 ký tự")
	}
	return s.change(ctx, c, ref, domain.ActionClosed, in, idem, func(a store.Alert) error {
		mine := a.AssigneeRef != nil && *a.AssigneeRef == c.p.ActorRef
		if !c.admin() && !c.tinh() && !mine {
			return problem.Forbidden.WithDetail("chỉ người được giao hoặc Cấp Tỉnh đóng cảnh báo")
		}
		return nil
	})
}

// Assignees liệt kê cán bộ của phòng được giao xử lý (Cấp Tỉnh chọn người).
func (s *Service) Assignees(ctx context.Context, commune string) ([]OfficerView, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	if s.Dir == nil {
		return nil, ErrDependency.WithDetail("chưa nối danh bạ cán bộ (pf/accounts)")
	}
	province := s.provinceOf(ctx, commune, s.now())
	if province == "" {
		province = provinceOfCaller(c)
	}
	if province == "" {
		return []OfficerView{}, nil
	}
	os, err := s.Dir.Officers(ctx, c.p.TenantID, province)
	if err != nil {
		return nil, err
	}
	refs := make([]uuid.UUID, 0, len(os))
	for _, o := range os {
		refs = append(refs, o.ActorRef)
	}
	name := s.actors(ctx, refs)
	out := []OfficerView{}
	for _, o := range os {
		if o.Level == "commune" && commune != "" && !containsStr(o.Communes, commune) {
			continue
		}
		out = append(out, OfficerView{Officer: o, Actor: name(o.ActorRef)})
	}
	return out, nil
}

// OfficerView là cán bộ kèm tên.
type OfficerView struct {
	Officer
	Actor Actor
}

// provinceOfCaller: tỉnh của Cấp Tỉnh — đơn vị đầu của phạm vi (không có → rỗng).
func provinceOfCaller(c caller) string {
	if c.k.Role == port.SessionRoleProvince && len(c.k.Scope.Units) > 0 {
		return c.k.Scope.Units[0]
	}
	return ""
}

// ---------------------------------------------------------------------------
// Job: quá hạn xử lý → Quá hạn, báo Lãnh đạo phòng (một lần mỗi hạn).
// ---------------------------------------------------------------------------

const overdueBatch = 200

// EscalateOverdue ghi "overdue" cho cảnh báo quá hạn xử lý của tenant và báo Lãnh đạo phòng + người được giao.
func (s *Service) EscalateOverdue(ctx context.Context, tid uuid.UUID, now time.Time) (int, error) {
	n := 0
	ctx = txdeps.With(ctx) // P0-POOL-KN-2: Lãnh đạo phòng nhận báo đọc ngoài giao dịch (service.go tx)
	err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		n = 0 // thân giao dịch có thể chạy lại (txdeps)
		as, err := store.Overdue(ctx, tx, now, overdueBatch)
		if err != nil {
			return err
		}
		for i := range as {
			a := &as[i]
			if err := store.Lock(ctx, tx, a.Ref.String()); err != nil {
				return err
			}
			note := "Quá hạn xử lý " + a.DueAt.In(time.FixedZone("ICT", 7*3600)).Format("15:04 02/01/2006") + " — báo Lãnh đạo phòng"
			if _, err := s.apply(ctx, tx, tid, a, domain.ActionOverdue, applyInput{Note: note, Actor: SystemActor, At: now,
				Idem: fmt.Sprintf("overdue:%s:%d", a.Ref, a.DueAt.Unix())}); err != nil {
				return err
			}
			rs := &resolver{s: s, tid: tid, province: a.ProvinceCode}
			recips := rs.resolve(ctx, []string{domain.RecipManager}, a.CommuneCode, uuid.Nil)
			if a.AssigneeRef != nil {
				recips = append(recips, *a.AssigneeRef)
			}
			if err := emitChanged(ctx, tx, tid, *a, port.CNTSAlertOverdue, dedupRefs(recips), now); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	if dup(err) {
		return 0, nil
	}
	return n, err
}

// TenantLister trả các tenant cần chạy job tại at.
type TenantLister func(ctx context.Context, at time.Time) ([]uuid.UUID, error)

// JobTenants dùng pool vai ag_ops_jobs (BYPASSRLS, chỉ đọc cột tìm tenant): tenant có cảnh báo quá hạn xử lý hoặc có kỳ
// native profile kn_cnts (nhắc nộp, trễ liên tiếp).
func JobTenants(pool *pgxpool.Pool) TenantLister {
	return func(ctx context.Context, at time.Time) ([]uuid.UUID, error) {
		rows, err := pool.Query(ctx, `SELECT tenant_id FROM pf_extension.cnts_alert_current WHERE status <> 'closed' AND overdue_at IS NULL AND due_at < $1
		  UNION SELECT tenant_id FROM pf_forms.period WHERE profile = $2`, at.UTC(), ProfileCNTS)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			out = append(out, id)
		}
		return out, rows.Err()
	}
}

// JobResult là kết quả một lần chạy job.
type JobResult struct{ Overdue, Reminders, Streaks int }

// RunJob là thân job extension_cnts_alert: quá hạn xử lý, nhắc nộp, trễ liên tiếp — mọi tenant.
func (s *Service) RunJob(ctx context.Context, tenants TenantLister) (JobResult, error) {
	now := s.now()
	tids, err := tenants(ctx, now)
	if err != nil {
		return JobResult{}, err
	}
	var total JobResult
	var errs []error
	for _, tid := range tids {
		r, err := s.JobTenant(ctx, tid, now)
		total.Overdue += r.Overdue
		total.Reminders += r.Reminders
		total.Streaks += r.Streaks
		if err != nil {
			errs = append(errs, fmt.Errorf("tenant %s: %w", tid, err))
		}
	}
	return total, errors.Join(errs...)
}
