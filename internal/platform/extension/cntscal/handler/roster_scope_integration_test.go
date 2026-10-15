package handler_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/cntscal/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/cntscal/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

var facProvince92 = uuid.MustParse("0192a000-0000-7000-8000-000000000003")

// CNTS-ROSTER-SCOPE-001: hai tỉnh cùng tenant, cùng tháng phải có lịch/phiên bản riêng;
// không lộ cơ sở, cán bộ, ghi chú/lịch sử và không ghi đè lịch tỉnh khác.
func TestDutyRosterProvinceIsolation_W1146(t *testing.T) {
	f := newFix(t, true)
	a := f.user(tinh("tong_hop"))
	kb := tinh("tong_hop")
	kb.Scope = authz.Scope{Units: []string{"92", "31001"}}
	b := f.user(kb)
	oa := f.user(xa("30001"))
	ob := f.user(xa("31001"))
	f.xa.list = []accport.XaScope{{ActorRef: oa.ref, Communes: []string{"30001"}}, {ActorRef: ob.ref, Communes: []string{"31001"}}}
	path := "/v1/extension/cnts/duty-rosters/2027-10"
	ra := want(t, a.get(path), 200, "", "tỉnh A đọc lịch rỗng")
	if len(list(ra, "facilities")) != 2 || len(list(ra, "officers")) != 1 {
		t.Fatalf("lộ địa bàn/cán bộ tỉnh khác: %v", ra)
	}
	rb := want(t, b.get(path), 200, "", "tỉnh B đọc lịch rỗng")
	if len(list(rb, "facilities")) != 1 || len(list(rb, "officers")) != 1 {
		t.Fatalf("tỉnh B sai phạm vi: %v", rb)
	}
	ca := []map[string]any{{"facility_ref": fac1, "officer_ref": oa.ref, "day": 13, "shift": "N"}}
	want(t, a.post(path, map[string]any{"base_version": 0, "cells": ca, "note": "Ghi chú riêng tỉnh A"}), 201, "", "A lưu")
	rawB := b.get(path)
	rb = want(t, rawB, 200, "", "B không thấy lịch A")
	if num(rb, "version") != "0" || len(list(rb, "cells")) != 0 || len(list(rb, "versions")) != 0 || strings.Contains(string(rawB.raw), "Ghi chú riêng tỉnh A") || strings.Contains(string(rawB.raw), oa.ref.String()) {
		t.Fatalf("lộ lịch/ghi chú/cán bộ A: %s", rawB.raw)
	}
	cb := []map[string]any{{"facility_ref": facProvince92, "officer_ref": ob.ref, "day": 13, "shift": "D"}}
	want(t, b.post(path, map[string]any{"base_version": 0, "cells": cb, "note": "Ghi chú riêng tỉnh B"}), 201, "", "B lưu từ phiên bản 0 riêng")
	rawA := a.get(path)
	ra = want(t, rawA, 200, "", "A giữ nguyên sau B lưu")
	if num(ra, "version") != "1" || len(list(ra, "cells")) != 1 || str(ra, "note") != "Ghi chú riêng tỉnh A" || len(list(ra, "versions")) != 1 || strings.Contains(string(rawA.raw), ob.ref.String()) {
		t.Fatalf("lịch A bị đè/lộ B: %s", rawA.raw)
	}
	want(t, a.post(path, map[string]any{"base_version": 1, "cells": cb}), 422, "extension.cnts_roster_invalid", "A không ghi cơ sở ngoài tỉnh")
	foreignOfficer := []map[string]any{{"facility_ref": fac1, "officer_ref": ob.ref, "day": 13, "shift": "D"}}
	want(t, a.post(path, map[string]any{"base_version": 1, "cells": foreignOfficer}), 422, "extension.cnts_roster_invalid", "A không phân ca cán bộ ngoài tỉnh")
	want(t, a.get(path+"?province_code=92"), 403, "extension.out_of_scope", "A không chọn tỉnh khác")
}

// CNTS-ROSTER-SCOPE-002: Admin chọn tỉnh rõ ràng; lịch chung cũ chỉ đọc, không đổi hash.
func TestDutyRosterAdminAndLegacy_W1146(t *testing.T) {
	f := newFix(t, true)
	admin := f.user(authz.KNContext{Role: authz.KNAdmin, Scope: authz.Scope{All: true}})
	planner := f.user(tinh("tong_hop"))
	path := "/v1/extension/cnts/duty-rosters/2027-10"
	want(t, admin.get(path), 422, "", "Admin phải chọn tỉnh")
	want(t, admin.get(path+"?province_code=30001"), 422, "", "không nhận xã làm tỉnh")
	r := want(t, admin.get(path+"?province_code=92"), 200, "", "Admin chọn tỉnh B")
	if str(r, "province_code") != "92" || len(list(r, "facilities")) != 1 {
		t.Fatalf("sai tỉnh: %v", r)
	}
	ctx := context.Background()
	legacy := &store.Roster{Meta: store.Meta{ID: ids.New(), ActorRef: admin.ref, EventTime: t0, IdemKey: key()}, Month: time.Date(2027, 10, 1, 0, 0, 0, 0, time.UTC), Source: "grid", Cells: []domain.Cell{}, Note: "Lịch chung trước phân tỉnh"}
	if err := tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error { return store.InsertRoster(ctx, tx, f.tenant, legacy) }); err != nil {
		t.Fatal(err)
	}
	var before, after string
	if err := f.pool.QueryRow(ctx, "SELECT encode(row_hash,'hex') FROM pf_extension.cnts_duty_roster WHERE id=$1", legacy.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	r = want(t, admin.get(path+"?province_code=legacy"), 200, "", "Admin đọc lịch chung")
	if r["legacy"] != true || r["province_code"] != nil || str(r, "note") != legacy.Note || num(r, "version") != "1" {
		t.Fatalf("sai lịch cũ: %v", r)
	}
	want(t, planner.get(path+"?province_code=legacy"), 403, "extension.out_of_scope", "cán bộ không đọc lịch chung")
	want(t, admin.post(path, map[string]any{"province_code": "legacy", "base_version": 1, "cells": []any{}}), 403, "extension.out_of_scope", "lịch cũ không ghi")
	want(t, admin.post(path, map[string]any{"province_code": "92", "base_version": 0, "cells": []any{}, "note": "Lịch riêng mới"}), 201, "", "Admin tạo lịch riêng")
	r = want(t, admin.get(path+"?province_code=92"), 200, "", "đọc lịch riêng")
	if num(r, "version") != "1" || str(r, "note") != "Lịch riêng mới" {
		t.Fatalf("lẫn phiên bản lịch cũ: %v", r)
	}
	if err := f.pool.QueryRow(ctx, "SELECT encode(row_hash,'hex') FROM pf_extension.cnts_duty_roster WHERE id=$1", legacy.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before == "" || before != after {
		t.Fatal("đã thay đổi lịch sử sổ cũ")
	}
}

// IDEM-SCOPE-001: cùng người, cùng payload/khóa nhưng khác tài nguyên hoặc phiên/phạm vi không trả cache cũ.
func TestRosterReplayAuthorization_W1146(t *testing.T) {
	f := newFix(t, true)
	a := f.user(tinh("tong_hop"))
	kb := tinh("tong_hop")
	kb.Scope = authz.Scope{Units: []string{"92", "31001"}}
	b := f.user(kb)
	b.token = a.token
	b.ref = a.ref
	body := map[string]any{"base_version": 0, "cells": []any{}, "note": "Thông tin riêng tỉnh A"}
	path := "/v1/extension/cnts/duty-rosters/2027-10"
	k := key()
	want(t, a.postK(path, k, body), 201, "", "lưu lần đầu")
	want(t, a.postK(path, k, body), 201, "", "gửi lại hợp lệ")
	t.Run("khác tháng", func(t *testing.T) {
		want(t, a.postK("/v1/extension/cnts/duty-rosters/2027-11", k, body), 422, "idempotency_key_reused", "không phát lại tháng khác")
	})
	t.Run("khác ngữ cảnh", func(t *testing.T) {
		want(t, b.postK(path, k, body), 422, "idempotency_key_reused", "không phát lại tỉnh khác")
	})
	t.Run("cùng phiên thu hẹp phạm vi", func(t *testing.T) {
		f.mu.Lock()
		f.ctxs[a.ctxID].Scope = kb.Scope
		f.mu.Unlock()
		want(t, a.postK(path, k, body), 422, "idempotency_key_reused", "không phát lại phạm vi đã đổi")
	})
}

// CNTS-ROSTER-SCOPE-003: chưa nối danh bạ không được nhận UUID cán bộ bất kỳ vào lịch.
func TestRosterRequiresOfficerDirectory_W1146(t *testing.T) {
	f := newFix(t, false, false)
	a := f.user(tinh("tong_hop"))
	body := map[string]any{"base_version": 0, "cells": []map[string]any{{"facility_ref": fac1, "officer_ref": ids.New(), "day": 13, "shift": "N"}}}
	want(t, a.post("/v1/extension/cnts/duty-rosters/2027-10", body), 503, "extension.dependency_unavailable", "thiếu danh bạ không phân ca")
}
