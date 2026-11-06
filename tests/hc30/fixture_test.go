// Package hc30_test verifies HCTH acceptance through the real HRM HTTP module and PostgreSQL.
// Identity is an in-memory fake; this suite does not measure deployed authentication or network latency.
package hc30_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	sessionport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

type unitsStub []port.Unit

func (u unitsStub) InternalUnits(context.Context, uuid.UUID) ([]port.Unit, error) { return u, nil }

type fixture struct {
	t           *testing.T
	r           *gin.Engine
	who         map[string]authz.Principal
	tenant, org uuid.UUID
}

func (f *fixture) call(as, method, path, key string, body any, want int) map[string]any {
	f.t.Helper()
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			f.t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req := httptest.NewRequestWithContext(ctx, method, path, bytes.NewReader(raw))
	req.Header.Set("X-Test-As", as)
	req.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, req)
	if w.Code != want {
		f.t.Fatalf("%s %s %s: got%d want%d: %s", as, method, path, w.Code, want, w.Body.String())
	}
	var out map[string]any
	dec := json.NewDecoder(w.Body)
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil {
		f.t.Fatal(err)
	}
	return out
}
func newFixture(t *testing.T) *fixture {
	t.Helper()
	env := pgtest.Get(t)
	f := &fixture{t: t, who: map[string]authz.Principal{}, tenant: ids.New(), org: ids.New()}
	mod, err := hrm.New(env.Pool(t, migrations.PF, dbadmin.AppPlatform), clock.NewFixed(time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)), port.Access{Session: ownerSession, Department: "hanh_chinh_tong_hop", ManageBy: []string{"admin"}, ReadBy: []string{"kn_tinh"}})
	if err != nil {
		t.Fatal(err)
	}
	vault := clienttest.New()
	mod.UseRecords(vault, unitsStub{{Code: "trong_trot", Name: "Phòng Trồng trọt", Active: true}, {Code: "chan_nuoi", Name: "Phòng Chăn nuôi", Active: true}, {Code: "hcth", Name: "HCTH", Active: true}}, true)
	f.add("admin", "admin", "hcth", false, f.tenant)
	f.add("hcth", "kn_tinh", "hcth", false, f.tenant)
	f.add("ld_tt", "kn_tinh", "trong_trot", true, f.tenant)
	f.add("ld_cn", "kn_tinh", "chan_nuoi", true, f.tenant)
	f.add("other", "admin", "hcth", false, ids.New())
	mod.UseDocs(&proofFiles{vault: vault, res: mod.FileResolver(), files: map[uuid.UUID]proofFile{}})
	for _, name := range []string{"admin", "hcth", "ld_tt", "ld_cn"} {
		p := f.who[name]
		p.Roles = append(p.Roles, client.RoleRegistrar, client.RoleEvidenceViewer)
		f.who[name] = p
	}
	gin.SetMode(gin.TestMode)
	f.r = gin.New()
	f.r.Use(func(c *gin.Context) {
		if p, ok := f.who[c.GetHeader("X-Test-As")]; ok {
			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
		}
	})
	mod.RegisterApp(f.r.Group("/v1"))
	f.call("admin", http.MethodPost, "/v1/hrm/defaults", "", nil, http.StatusCreated)
	return f
}
func (f *fixture) add(name, role, unit string, manager bool, tenant uuid.UUID) {
	dept := unit
	if unit == "hcth" {
		dept = "hanh_chinh_tong_hop"
	}
	k := &authz.KNContext{Role: role, Department: dept, InternalUnit: unit, Manager: manager, UnitRef: f.org, OrgRef: f.org}
	if role == "admin" {
		k.Scope.All = true
	}
	f.who[name] = bindSession(f.t, authz.Principal{ActorRef: ids.New(), TenantID: tenant, KN: k})
}

// bindSession gắn ngữ cảnh fixture vào phiên chủ KN thật (registry + bộ mã chủ phiên của pf/extension), như middleware
// pf/accounts ở production — mẫu của KNSPLIT-2-CONTEXT-I. Mã ngữ cảnh sinh ở đây chỉ dùng cho kiểm thử.
func bindSession(t *testing.T, p authz.Principal) authz.Principal {
	t.Helper()
	if p.KN == nil {
		return p
	}
	kc := *p.KN
	if kc.ID == uuid.Nil {
		kc.ID = ids.New()
	}
	p.KN = &kc
	registry, err := authz.NewSessionRegistry(authz.SessionRegistration{Profile: "kn", Project: func(_ context.Context, p authz.Principal) (authz.Session, bool, error) {
		k := p.KN
		selected, err := sessionport.NewSessionContext(sessionport.SessionContext{ID: k.ID, Role: k.Role, Department: k.Department,
			OrgRef: k.OrgRef, UnitRef: k.UnitRef, Communes: k.Communes, Scope: k.Scope, Duties: k.Duties, InternalUnit: k.InternalUnit,
			Manager: k.Manager, ViewOnly: k.ViewOnly})
		return selected, true, err
	}})
	if err != nil {
		t.Fatal(err)
	}
	selected, err := registry.ResolveSelected(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	p.SelectedSession = &selected
	return p
}

// ownerSession là chính sách phiên của pf/hrm đọc từ phiên chủ KN (như assembly extension.HRMSession).
func ownerSession(ctx context.Context) (port.SessionContext, error) {
	_, k, err := sessionport.RequireSession(ctx)
	if err != nil {
		return port.SessionContext{}, err
	}
	return port.SessionContext{Role: k.Role, Department: k.Department, UnitRef: k.UnitRef, InternalUnit: k.InternalUnit,
		Manager: k.Manager, ViewOnly: k.ViewOnly, ReadOnly: k.ReadOnly()}, nil
}

func newStaff(typ, name, phone string, events ...map[string]any) map[string]any {
	return map[string]any{"type": typ,
		"profile": map[string]any{"full_name": name, "phone": phone, "address": "Phường Long Xuyên", "birth_date": "1988-05-01",
			"gender": "nu", "email": phone + "@ttkn.vn"},
		"events": events}
}

func ev(kind, from string, payload map[string]any) map[string]any {
	return map[string]any{"kind": kind, "valid_from": from, "payload": payload}
}
