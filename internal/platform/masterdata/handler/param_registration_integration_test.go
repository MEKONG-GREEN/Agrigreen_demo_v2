package handler_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

func TestRegisteredParamsSeedRestoreAndTenantIsolation(t *testing.T) {
	value := "17.25"
	f := newFixtureWithParams(t, port.ParamRegistration{
		Specs: []port.ParamSpec{{Key: "fixture.delay", Name: "Độ trễ đăng ký", Kind: port.ParamKindDecimal, Unit: "giờ", Min: "0", Max: "72"}},
		Seeds: []port.ParamSeed{{Key: "fixture.delay", Value: &value, Unit: "giờ", EffectiveFrom: "2026-01-01", SourceDoc: "Quyết định fixture"}},
	})
	tid := f.seeded()
	setter := f.caller(tid, port.RoleParamSetter)
	approver := f.caller(tid, port.RoleParamApprover)
	code, body := setter.adm(http.MethodGet, "/v1/masterdata/params?prefix=fixture.", nil)
	if code != 200 {
		t.Fatalf("registered metadata: %d %v", code, body)
	}
	items := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("metadata count: %v", body)
	}
	entry := items[0].(map[string]any)
	if entry["name"] != "Độ trễ đăng ký" || entry["unit"] != "giờ" || entry["min"] != "0" || entry["max"] != "72" || entry["current"].(map[string]any)["value"] != "17.25" {
		t.Fatalf("registered metadata/default: %v", entry)
	}
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	initial, err := f.mod.Params().At(setter.ctx(), "fixture.delay", at)
	if err != nil || initial.Value.String() != "17.25" || initial.Version != 1 {
		t.Fatalf("seed: %+v %v", initial, err)
	}
	res, err := f.mod.Seed(context.Background(), tid)
	if err != nil || res.Params != 0 {
		t.Fatalf("seed idempotency: %+v %v", res, err)
	}
	other := f.caller(ids.New(), port.RoleParamSetter)
	if _, err := f.mod.Params().At(other.ctx(), "fixture.delay", at); err == nil {
		t.Fatal("seed crossed tenant boundary")
	}
	if c, _ := f.caller(tid).adm(http.MethodGet, "/v1/masterdata/params?prefix=fixture.", nil); c != http.StatusForbidden {
		t.Fatalf("metadata permissions changed: %d", c)
	}
	code, body = setter.adm(http.MethodPost, "/v1/masterdata/params/restore-default", map[string]any{"key": "fixture.delay", "reason": "Khôi phục giá trị theo quyết định"})
	if code != http.StatusCreated || body["status"] != "draft" || body["value"] != "17.25" {
		t.Fatalf("restore requires approval: %d %v", code, body)
	}
	id := body["id"].(string)
	if c, b := setter.adm(http.MethodPost, "/v1/masterdata/params/versions/"+id+"/submit", nil); c != http.StatusOK {
		t.Fatalf("submit: %d %v", c, b)
	}
	if c, b := setter.adm(http.MethodPost, "/v1/masterdata/params/versions/"+id+"/approve", nil); c != http.StatusForbidden {
		t.Fatalf("same actor approved: %d %v", c, b)
	}
	if c, b := approver.adm(http.MethodPost, "/v1/masterdata/params/versions/"+id+"/approve", nil); c != http.StatusOK {
		t.Fatalf("approve: %d %v", c, b)
	}
	restored, err := f.mod.Params().At(setter.ctx(), "fixture.delay", time.Now().Add(time.Hour))
	if err != nil || restored.Version != 2 || restored.Value.String() != "17.25" {
		t.Fatalf("approved restore: %+v %v", restored, err)
	}
}
