package app

// Bật module theo đơn vị tham gia (W1.4, R-MOD-08, pf 000088). Đơn vị tham gia = tenant (v4.1 RB-2).
// Module `enable: always` (modreg) luôn bật, không ghi sổ được; module `per_tenant` bật mặc định (không
// có dòng chiếu = bật) và chỉ tắt khi Admin ghi sự kiện 'disabled' kèm lý do. Mỗi thay đổi là một dòng
// sổ chỉ ghi thêm + bảng chiếu cùng giao dịch (R-21), như trạng thái phòng ban (000062).
//
// Đọc (middleware route, chặn cấp vai, menu) đi qua đệm theo tenant: ghi trong tiến trình xóa đệm ngay;
// bản sao khác thấy thay đổi sau tối đa ModuleCacheTTL.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// ModuleCacheTTL là thời gian lưu đệm tập module đã tắt của một tenant.
const ModuleCacheTTL = 10 * time.Second

// Mã lỗi bật module (docs/errors.md, mục pf/party).
var (
	ErrModuleNotFound = problem.New(http.StatusNotFound, "party.module_not_found", "Không có module này")
	ErrModuleAlwaysOn = problem.New(http.StatusConflict, "party.module_always_on", "Module luôn bật (enable: always) — không tắt theo đơn vị")
	ErrModuleState    = problem.New(http.StatusConflict, "party.module_enablement_state", "Module đã ở trạng thái yêu cầu")
)

// Modules là use case bật module theo đơn vị; thực hiện port.ModuleEnablement.
type Modules struct {
	DB    tenant.Beginner // pool vai party_app
	Clock clock.Clock
	Names func() port.ActorNames // tên người thao tác cho Log (nối sau qua KN)

	mu    sync.Mutex
	cache map[uuid.UUID]moduleCache
}

type moduleCache struct {
	disabled map[string]bool
	at       time.Time
}

var _ port.ModuleEnablement = (*Modules)(nil)

// ModuleView là trạng thái một module trong tenant.
type ModuleView struct {
	Module    string
	Kind      string
	Enable    string // always | per_tenant
	Enabled   bool
	Reason    *string
	UpdatedBy *uuid.UUID
	UpdatedAt *time.Time
}

func (m *Modules) now() time.Time { return m.Clock.Now().UTC() }

// DisabledModules thực hiện port.ModuleEnablement (có đệm).
func (m *Modules) DisabledModules(ctx context.Context, tid uuid.UUID) (map[string]bool, error) {
	now := m.now()
	m.mu.Lock()
	if c, ok := m.cache[tid]; ok && now.Sub(c.at) < ModuleCacheTTL && !now.Before(c.at) {
		m.mu.Unlock()
		return c.disabled, nil
	}
	m.mu.Unlock()
	var states []store.ModuleState
	if err := tenant.WithTenantTx(ctx, m.DB, tid, func(tx pgx.Tx) error {
		var e error
		states, e = store.ModuleStates(ctx, tx)
		return e
	}); err != nil {
		return nil, err
	}
	disabled := map[string]bool{}
	for _, s := range states {
		if s.Status == port.ModuleDisabled && modreg.PerTenant(s.Module) {
			disabled[s.Module] = true
		}
	}
	m.mu.Lock()
	if m.cache == nil {
		m.cache = map[uuid.UUID]moduleCache{}
	}
	m.cache[tid] = moduleCache{disabled: disabled, at: now}
	m.mu.Unlock()
	return disabled, nil
}

// ModuleEnabled thực hiện port.ModuleEnablement: chỉ module per_tenant đã tắt trả false.
func (m *Modules) ModuleEnabled(ctx context.Context, tid uuid.UUID, module string) (bool, error) {
	if !modreg.PerTenant(module) {
		return true, nil
	}
	d, err := m.DisabledModules(ctx, tid)
	if err != nil {
		return false, err
	}
	return !d[module], nil
}

func (m *Modules) invalidate(tid uuid.UUID) {
	m.mu.Lock()
	delete(m.cache, tid)
	m.mu.Unlock()
}

// List trả mọi module của danh mục với trạng thái bật của tenant người gọi. admin = Admin API (vai
// quản trị KN); ngược lại mọi người đã xác thực (đọc cho ngữ cảnh hiện tại: menu, ẩn màn).
func (m *Modules) List(ctx context.Context, admin bool) ([]ModuleView, error) {
	var p authz.Principal
	var err error
	if admin {
		p, err = authz.Require(ctx, knAdminRoles...)
	} else {
		p, err = authenticated(ctx)
	}
	if err != nil {
		return nil, err
	}
	var states []store.ModuleState
	if err := tenant.WithTenantTx(ctx, m.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		states, e = store.ModuleStates(ctx, tx)
		return e
	}); err != nil {
		return nil, err
	}
	byName := map[string]store.ModuleState{}
	for _, s := range states {
		byName[s.Module] = s
	}
	cat := modreg.Catalog()
	out := make([]ModuleView, 0, len(cat))
	for _, e := range cat {
		out = append(out, viewOf(e, byName))
	}
	return out, nil
}

func viewOf(e modreg.CatalogEntry, byName map[string]store.ModuleState) ModuleView {
	v := ModuleView{Module: e.Name, Kind: string(e.Kind), Enable: string(e.Enable), Enabled: true}
	if s, ok := byName[e.Name]; ok && e.Enable == modreg.EnablePerTenant {
		by, at := s.UpdatedBy, s.UpdatedAt
		v.Enabled, v.Reason, v.UpdatedBy, v.UpdatedAt = s.Status != port.ModuleDisabled, s.Reason, &by, &at
	}
	return v
}

// perTenantEntry kiểm module có trong danh mục và bật/tắt được theo đơn vị.
func perTenantEntry(module string) (modreg.CatalogEntry, error) {
	e, ok := modreg.Entry(module)
	if !ok {
		return e, ErrModuleNotFound.With("module", module)
	}
	if e.Enable != modreg.EnablePerTenant {
		return e, ErrModuleAlwaysOn.With("module", module)
	}
	return e, nil
}

// SetEnabled bật/tắt module cho tenant người gọi (Admin API, MFA). Tắt bắt buộc lý do ≥ 10 ký tự. Gọi
// lặp cùng Idempotency-Key → như lần đầu (replayed = true); cùng khóa khác nội dung → idempotency_key_reused.
func (m *Modules) SetEnabled(ctx context.Context, module string, enabled bool, reason, idem string) (ModuleView, bool, error) {
	p, err := authz.Require(ctx, knAdminRoles...)
	if err != nil {
		return ModuleView{}, false, err
	}
	if idem == "" {
		return ModuleView{}, false, problem.IdempotencyRequired
	}
	module = strings.TrimSpace(module)
	entry, err := perTenantEntry(module)
	if err != nil {
		return ModuleView{}, false, err
	}
	if !enabled || strings.TrimSpace(reason) != "" {
		if err := domain.ValidateReason("reason", reason); err != nil {
			return ModuleView{}, false, fieldProblem(err)
		}
	}
	want := port.ModuleEnabled
	if !enabled {
		want = port.ModuleDisabled
	}
	key := "module:set:" + idem
	now := m.now()
	var out ModuleView
	replayed := false
	err = tenant.WithTenantTx(ctx, m.DB, p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockModule(ctx, tx, p.TenantID, module); err != nil {
			return err
		}
		prev, err := store.ModuleEventByIdem(ctx, tx, key)
		if err != nil {
			return err
		}
		cur, err := store.ModuleStateOf(ctx, tx, module)
		if err != nil {
			return err
		}
		byName := map[string]store.ModuleState{}
		if cur != nil {
			byName[module] = *cur
		}
		if prev != nil {
			if prev.Module != module || prev.Status != want {
				return problem.IdempotencyConflict
			}
			out, replayed = viewOf(entry, byName), true
			return nil
		}
		curStatus := port.ModuleEnabled // không có dòng = bật mặc định
		if cur != nil {
			curStatus = cur.Status
		}
		if curStatus == want {
			return ErrModuleState.WithDetail("module "+module+" đang "+want).With("module", module)
		}
		e := store.ModuleEvent{ID: ids.New(), TenantID: p.TenantID, Module: module, Status: want, PrevStatus: curStatus,
			Reason: optReason(reason), EventTime: now, ActorRef: p.ActorRef, IdemKey: key}
		if err := ledger.Classify(store.InsertModuleEvent(ctx, tx, &e)); err != nil {
			return err
		}
		if err := store.PutModuleState(ctx, tx, e); err != nil {
			return err
		}
		byName[module] = store.ModuleState{Module: module, Status: want, Reason: e.Reason, UpdatedBy: p.ActorRef, UpdatedAt: now}
		out = viewOf(entry, byName)
		return nil
	})
	if errors.Is(err, ledger.ErrDuplicate) {
		return m.SetEnabled(ctx, module, enabled, reason, idem)
	}
	if err != nil {
		return ModuleView{}, false, err
	}
	m.invalidate(p.TenantID)
	return out, replayed, nil
}

// Log trả khung Log của một module trong tenant (người, thời điểm, trạng thái cũ → mới, lý do).
func (m *Modules) Log(ctx context.Context, module string, after int64, limit int) ([]LogEntry, error) {
	p, err := authz.Require(ctx, knAdminRoles...)
	if err != nil {
		return nil, err
	}
	if _, ok := modreg.Entry(module); !ok {
		return nil, ErrModuleNotFound.With("module", module)
	}
	var evs []store.ModuleEvent
	if err := tenant.WithTenantTx(ctx, m.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		evs, e = store.ModuleEvents(ctx, tx, module, after, clampLog(limit))
		return e
	}); err != nil {
		return nil, err
	}
	out := make([]LogEntry, 0, len(evs))
	refs := make([]uuid.UUID, 0, len(evs))
	for _, e := range evs {
		old, nw := e.PrevStatus, e.Status
		out = append(out, LogEntry{Seq: e.Seq, Event: e.Status, ActorRef: e.ActorRef, At: e.EventTime, RecordedAt: e.RecordedAt,
			Changes: []domain.FieldChange{{Field: "status", Old: &old, New: &nw}}, Reason: e.Reason})
		refs = append(refs, e.ActorRef)
	}
	if m.Names != nil && len(refs) > 0 {
		if n := m.Names(); n != nil {
			if names, err := n.DisplayNames(ctx, refs); err == nil {
				for i := range out {
					out[i].ActorDisplay = names[out[i].ActorRef]
				}
			}
		}
	}
	return out, nil
}
