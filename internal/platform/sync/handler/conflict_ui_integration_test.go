package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// ConflictVersions: memSink cho xem hai phiên bản (hiện thực port.ConflictViewer).
func (s *memSink) ConflictVersions(_ context.Context, sc authz.Scope, id uuid.UUID) ([]port.ConflictVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cid := range s.conflicts {
		if cid == id {
			return []port.ConflictVersion{
				{RecordID: ids.New(), Role: port.VersionRoleRecord, Label: "TH0000001", EventTime: time.Now(), Data: json.RawMessage(`{"area_ha":"1.0000"}`)},
				{RecordID: ids.New(), Role: port.VersionRoleCurrent, Label: "TH0000001", Current: true, EventTime: time.Now(), Data: json.RawMessage(`{"area_ha":"1.2000"}`)},
			}, nil
		}
	}
	return nil, port.ErrConflictUnknown
}

// US-SYNC-03: danh sách lọc theo entity/SLA, chi tiết hai phiên bản (không PII), xử lý chọn phiên bản hoặc bản ghi bù.
func TestConflictResolutionUI(t *testing.T) {
	f := newFixture(t)
	c := f.officer()
	c.register()
	r1 := rec(c, 1, "plot_boundary", map[string]any{"behave": "conflict", "conflict_key": "a"})
	r2 := rec(c, 2, "plot_boundary", map[string]any{"behave": "conflict", "conflict_key": "a"})
	r3 := rec(c, 3, "plot_boundary", map[string]any{"behave": "conflict", "conflict_key": "b"})
	res := results(t, c.post("/v1/sync/push", map[string]any{"device_id": c.device.String(), "records": []any{r1, r2, r3}}))
	cidA, cidB := res[0].ConflictID, res[2].ConflictID
	resolver := f.officer(port.RoleConflictResolver)

	lst := resolver.adminGet("/v1/sync/conflicts?entity=plot_boundary&sla=on_track")
	if its, _ := lst.body["items"].([]any); lst.status != http.StatusOK || len(its) != 2 || its[0].(map[string]any)["sla_status"] != "on_track" {
		t.Fatalf("on_track: %d %s", lst.status, lst.raw)
	}
	if its, _ := resolver.adminGet("/v1/sync/conflicts?entity=field_report").body["items"].([]any); len(its) != 0 {
		t.Fatalf("lọc entity: %v", its)
	}
	f.clk.Advance(50 * time.Hour) // còn 22 giờ < 24 giờ → sắp quá hạn
	if its, _ := resolver.adminGet("/v1/sync/conflicts?sla=due_soon").body["items"].([]any); len(its) != 2 {
		t.Fatalf("due_soon: %v", its)
	}
	if r := resolver.adminGet("/v1/sync/conflicts?sla=xyz"); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("sla sai: %d", r.status)
	}

	d := resolver.adminGet("/v1/sync/conflicts/" + cidA)
	recs, _ := d.body["records"].([]any)
	vers, _ := d.body["versions"].([]any)
	if d.status != http.StatusOK || len(recs) != 2 || len(vers) != 2 || d.body["versions_available"] != true || d.body["sla_status"] != "due_soon" {
		t.Fatalf("chi tiết: %d %s", d.status, d.raw)
	}
	if _, hasPayload := recs[0].(map[string]any)["payload"]; hasPayload {
		t.Fatal("chi tiết không được trả payload (có thể chứa PII)")
	}

	path := "/v1/sync/conflicts/" + cidA + "/resolve"
	note := "Giữ bản vẽ thứ hai sau khi đối chiếu thực địa"
	if r := resolver.adminPost(path, map[string]any{"note": note, "resolution": "keep_version", "resolved_record_id": ids.New().String()}); code(r) != "sync.conflict_resolution_invalid" {
		t.Fatalf("keep_version với bản ghi lạ: %d %s", r.status, r.raw)
	}
	r := resolver.adminPost(path, map[string]any{"note": note, "resolution": "keep_version", "resolved_record_id": r2["id"]})
	if r.status != http.StatusOK || r.body["status"] != "resolved" || r.body["sla_status"] != "resolved_on_time" {
		t.Fatalf("keep_version: %d %s", r.status, r.raw)
	}
	if d := resolver.adminGet("/v1/sync/conflicts/" + cidA); d.body["resolution"] != "keep_version" {
		t.Fatalf("resolution: %s", d.raw)
	}
	// Bản ghi bù: id mới (do module sở hữu tạo), không được là bản ghi của xung đột.
	pathB := "/v1/sync/conflicts/" + cidB + "/resolve"
	if r := resolver.adminPost(pathB, map[string]any{"note": note, "resolution": "correction", "resolved_record_id": r3["id"]}); code(r) != "sync.conflict_resolution_invalid" {
		t.Fatalf("correction trỏ bản ghi cũ: %d %s", r.status, r.raw)
	}
	corr := ids.New().String()
	if r := resolver.adminPost(pathB, map[string]any{"note": note, "resolution": "correction", "resolved_record_id": corr}); r.status != http.StatusOK {
		t.Fatalf("correction: %d %s", r.status, r.raw)
	}
	if d := resolver.adminGet("/v1/sync/conflicts/" + cidB); d.body["resolution"] != "correction" || d.body["resolved_record_id"] != corr {
		t.Fatalf("resolution correction: %s", d.raw)
	}
}
