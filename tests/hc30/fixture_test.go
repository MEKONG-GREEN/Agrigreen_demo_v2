// Package hc30_test verifies HCTH workflows through public module roots and HTTP.
package hc30_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	platformasm "github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/contact"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm"
	hrmport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

type fixtureUnits struct{}

func (fixtureUnits) InternalUnits(context.Context, uuid.UUID) ([]hrmport.Unit, error) {
	return []hrmport.Unit{
		{Code: "trong_trot", Name: "Phòng Trồng trọt", Active: true},
		{Code: "chan_nuoi", Name: "Phòng Chăn nuôi", Active: true},
		{Code: "hcth", Name: "Phòng Hành chính - Tổng hợp", Active: true},
	}, nil
}

type fixture struct {
	t      *testing.T
	server *httptest.Server
	people map[string]authz.Principal
}

type response struct {
	status int
	header http.Header
	body   map[string]any
	raw    []byte
}

// The test owns only the authentication boundary, unit-directory port and vault
// port. Business modules, PostgreSQL, idempotency and assembly adapters are real.
// No legacy KNContext or invented realm grant supplies KN authorization.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	env := pgtest.Get(t)
	clk := clock.NewFixed(time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC))
	pool := env.SizedPool(t, migrations.PF, dbadmin.AppPlatform, 2)
	hm, err := hrm.New(pool, clk, platformasm.KNHRMAccess)
	if err != nil {
		t.Fatal(err)
	}
	vault := clienttest.New()
	hm.UseRecords(vault, fixtureUnits{}, true)
	cm, err := contact.New(pool, clk, platformasm.KNContactAccess, vault)
	if err != nil {
		t.Fatal(err)
	}
	cm.UseStaff(platformasm.KNContactStaff{Dir: hm.Directory()}, nil)
	f := &fixture{t: t, people: map[string]authz.Principal{}}
	tid, node := ids.New(), ids.New()
	add := func(name, role, dept, unit string, manager, viewOnly bool, tenantID uuid.UUID) {
		t.Helper()
		owned, err := knport.NewSessionContext(knport.SessionContext{
			ID: ids.New(), Role: role, Department: dept, InternalUnit: unit, Manager: manager,
			ViewOnly: viewOnly, OrgRef: node, UnitRef: node, Scope: authz.Scope{All: true},
		})
		if err != nil {
			t.Fatal(err)
		}
		f.people[name] = authz.Principal{ActorRef: ids.New(), TenantID: tenantID, MFA: true, SelectedSession: &owned}
	}
	add("admin", knport.SessionRoleAdmin, "", "", false, false, tid)
	add("hcth", knport.SessionRoleProvince, "hanh_chinh_tong_hop", "hcth", false, false, tid)
	add("manager_tt", knport.SessionRoleProvince, "trong_trot", "trong_trot", true, false, tid)
	add("manager_cn", knport.SessionRoleProvince, "chan_nuoi", "chan_nuoi", true, false, tid)
	add("self", knport.SessionRoleProvince, "trong_trot", "trong_trot", false, false, tid)
	add("commune", knport.SessionRoleCommune, "trong_trot", "trong_trot", false, false, tid)
	add("national", knport.SessionRoleNational, "", "", false, false, tid)
	add("readonly", knport.SessionRoleAdmin, "", "", false, true, tid)
	add("other_tenant", knport.SessionRoleAdmin, "", "", false, false, ids.New())
	f.people["realm_only"] = authz.Principal{ActorRef: ids.New(), TenantID: tid, MFA: true, Roles: []string{authz.RolePlatformAdmin}}
	foreign, err := authz.NewSession("hc30_other_owner", ids.New(), authz.SessionScope{})
	if err != nil {
		t.Fatal(err)
	}
	f.people["wrong_owner"] = authz.Principal{ActorRef: ids.New(), TenantID: tid, SelectedSession: &foreign}
	engine := httpx.NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)), 1<<20)
	group := engine.Group("/v1", func(c *gin.Context) {
		if p, ok := f.people[c.GetHeader("X-HC30-Actor")]; ok {
			ctx := tenant.WithID(c.Request.Context(), p.TenantID)
			c.Request = c.Request.WithContext(authz.WithPrincipal(ctx, p))
		}
		c.Next()
	}, idempotency.Middleware(env.Pool(t, migrations.PF, "platform_api")))
	hm.RegisterApp(group)
	cm.RegisterApp(group)
	f.server = httptest.NewServer(engine)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fixture) request(as, method, path, key string, body any, want int) response {
	f.t.Helper()
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			f.t.Fatal(err)
		}
	}
	req, err := http.NewRequestWithContext(context.Background(), method, f.server.URL+path, bytes.NewReader(raw))
	if err != nil {
		f.t.Fatal(err)
	}
	req.Header.Set("X-HC30-Actor", as)
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set(idempotency.Header, key)
	}
	res, err := f.server.Client().Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		f.t.Fatal(err)
	}
	if res.StatusCode != want {
		f.t.Fatalf("%s %s %s: status %d want %d: %s", as, method, path, res.StatusCode, want, data)
	}
	r := response{status: res.StatusCode, header: res.Header.Clone(), raw: data}
	if len(data) > 0 && bytes.Contains([]byte(res.Header.Get("Content-Type")), []byte("json")) {
		d := json.NewDecoder(bytes.NewReader(data))
		d.UseNumber()
		if err := d.Decode(&r.body); err != nil {
			f.t.Fatal(err)
		}
	}
	return r
}

func event(kind, from string, payload map[string]any) map[string]any {
	return map[string]any{"kind": kind, "valid_from": from, "payload": payload}
}

func staff(name, phone, typ, unit string) map[string]any {
	unitPayload := map[string]any{"unit": unit}
	if typ == "can_bo_xa" {
		unitPayload = map[string]any{"commune": "30292", "mgmt_unit": unit}
	}
	return map[string]any{"type": typ, "profile": map[string]any{
		"full_name": name, "phone": phone, "birth_date": "1990-03-15", "gender": "nu",
		"email": "private-" + phone + "@example.invalid", "address": "Địa chỉ tổng hợp riêng tư",
	}, "events": []any{
		event("unit", "2020-01-01", unitPayload),
		event("position", "2020-01-01", map[string]any{"position": "vien_chuc"}),
		event("work_start", "2020-01-01", map[string]any{}),
	}}
}

func (f *fixture) withTest(t *testing.T) *fixture {
	t.Helper()
	scoped := *f
	scoped.t = t
	return &scoped
}
