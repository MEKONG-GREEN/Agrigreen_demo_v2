package handler_test

// BF-CN-DUTY (#171, CN-14): nạp/xuất Excel đúng mẫu "Lịch trực ksgm" (BM-03), cột Địa chỉ và ĐT viên chức trên
// PostgreSQL thật. SĐT, tên đều hư cấu.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

func (u user) raw(method, path, idem string, body []byte) (int, http.Header, []byte) {
	u.f.t.Helper()
	r, _ := http.NewRequest(method, u.f.srv.URL+path, bytes.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+u.token)
	r.Header.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	r.Header.Set(authz.ContextHeader, u.ctxID.String())
	if idem != "" {
		r.Header.Set("Idempotency-Key", idem)
	}
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		u.f.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, b
}

func TestDutyRosterExcel(t *testing.T) {
	f := newFix(t)
	planner := f.user(tinh("tong_hop"))
	viewer := f.user(tinh("vien_chuc"))
	officer := f.user(xa("30001"))
	other := f.user(xa("30002"))
	f.xa.list = []accport.XaScope{{ActorRef: officer.ref, Communes: []string{"30001"}}, {ActorRef: other.ref, Communes: []string{"30002"}}}
	f.phones.m[officer.ref] = "0900000001"
	f.phones.m[other.ref] = "+84 900 000 002"
	const path = "/v1/extension/cnts/duty-rosters/2027-10"

	cells := []map[string]any{
		{"facility_ref": fac1, "officer_ref": officer.ref, "day": 1, "shift": "N"},
		{"facility_ref": fac1, "officer_ref": officer.ref, "day": 31, "shift": "NĐ"},
		{"facility_ref": fac2, "officer_ref": other.ref, "day": 2, "shift": "Đ"},
		{"facility_ref": fac2, "officer_ref": officer.ref, "day": 3, "shift": "Đ"}, // chưa cấp quyền cơ sở 2
	}
	s := want(t, planner.post(path, map[string]any{"base_version": 0, "cells": cells}), 201, "", "lưu lưới")

	// Cột Địa chỉ, ĐT viên chức: SĐT che với người lập lịch; viên chức Cấp Tỉnh khác không thấy cột ĐT.
	if s["can_view_phone"] != true {
		t.Fatalf("người lập lịch xem cột ĐT: %s", s)
	}
	masked := map[string]string{}
	for _, o := range list(s, "officers") {
		m := o.(map[string]any)
		masked[str(m, "ref")] = str(m, "phone_masked")
	}
	if masked[officer.ref.String()] != "0900•••001" || masked[other.ref.String()] != "0900•••002" {
		t.Fatalf("SĐT đã che: %v", masked)
	}
	if fs := list(s, "facilities"); str(fs[0].(map[string]any), "address") != "Ấp Hư Cấu" {
		t.Fatalf("địa chỉ cơ sở: %v", fs)
	}
	g := want(t, viewer.get(path), 200, "", "viên chức Cấp Tỉnh xem lịch")
	if g["can_view_phone"] != false {
		t.Fatalf("viên chức Cấp Tỉnh không có cột ĐT: %s", g)
	}
	for _, o := range list(g, "officers") {
		if o.(map[string]any)["phone_masked"] != nil {
			t.Fatalf("không lộ SĐT với người không lập lịch: %s", g)
		}
	}

	// Xuất .xlsx đúng mẫu.
	st, h, xlsx := planner.raw(http.MethodGet, path+"/export.xlsx?basis="+url.QueryEscape("Theo thông báo số 1/TB ngày 01/10/2027"), "", nil)
	if st != 200 || !strings.Contains(h.Get("Content-Type"), "spreadsheetml") || !strings.Contains(h.Get("Content-Disposition"), "lich-truc-ksgm-89-2027-10.xlsx") {
		t.Fatalf("xuất: %d %v %s", st, h, xlsx)
	}
	x, err := excelize.OpenReader(bytes.NewReader(xlsx))
	if err != nil {
		t.Fatal(err)
	}
	sh := x.GetSheetName(0)
	a1, _ := x.GetCellValue(sh, "A1")
	a2, _ := x.GetCellValue(sh, "A2")
	c6, _ := x.GetCellValue(sh, "C6")
	e6, _ := x.GetCellValue(sh, "E6")
	af6, _ := x.GetCellValue(sh, "AJ6") // ngày 31
	_ = x.Close()
	if !strings.Contains(a1, "THÁNG 10/2027") || a2 != "(Theo thông báo số 1/TB ngày 01/10/2027)" || c6 != "Ấp Hư Cấu" || e6 != "0900000001" || af6 != "NĐ" {
		t.Fatalf("bố cục tệp xuất: %q %q %q %q %q", a1, a2, c6, e6, af6)
	}
	_, _, vx := viewer.raw(http.MethodGet, path+"/export.xlsx", "", nil)
	y, _ := excelize.OpenReader(bytes.NewReader(vx))
	if e, _ := y.GetCellValue(y.GetSheetName(0), "E6"); e != "" {
		t.Fatalf("tệp xuất của người không lập lịch không có SĐT: %q", e)
	}
	_ = y.Close()
	if st, _, b := planner.raw(http.MethodGet, path+"/export.xlsx?basis="+url.QueryEscape("Gọi 0912345678"), "", nil); st != 422 {
		t.Fatalf("dòng căn cứ có SĐT: %d %s", st, b)
	}

	// Nạp lại tệp vừa xuất: cùng lịch, không lỗi; chỉ đọc (phiên bản không đổi).
	st, _, b := viewer.raw(http.MethodPost, path+"/import", key(), xlsx)
	if st != 403 {
		t.Fatalf("viên chức không nạp: %d %s", st, b)
	}
	st, _, b = planner.raw(http.MethodPost, path+"/import", key(), xlsx)
	imp := decode(t, st, b, 200, "nạp lại tệp xuất")
	if len(list(imp, "issues")) != 0 || len(list(imp, "cells")) != 4 || num(imp, "row_count") != "3" {
		t.Fatalf("nạp lại: %s", b)
	}
	if ws := codes(list(imp, "warnings")); ws["officer_not_granted"] != 1 || ws["facility_uncovered"] != 2 {
		t.Fatalf("cảnh báo khi xem trước: %s", b)
	}
	if v := want(t, planner.get(path), 200, "", "chưa lưu"); num(v, "version") != "1" {
		t.Fatalf("nạp không ghi: %s", v)
	}

	// Lưu bản nạp với source=excel → phiên bản mới, cảnh báo, không đổi ca nên không thông báo.
	s2 := want(t, planner.post(path, map[string]any{"base_version": 1, "cells": list(imp, "cells"), "source": "excel"}), 201, "", "lưu bản nạp")
	if num(s2, "version") != "2" || str(s2, "source") != "excel" || len(list(s2, "notified_officer_refs")) != 0 {
		t.Fatalf("lưu source=excel: %s", s2)
	}
	if vs := list(s2, "versions"); str(vs[0].(map[string]any), "source") != "excel" {
		t.Fatalf("lịch sử lưu: %v", vs)
	}

	// Lỗi theo dòng: sửa tệp — viên chức lạ, ký hiệu lạ.
	z, _ := excelize.OpenReader(bytes.NewReader(xlsx))
	zs := z.GetSheetName(0)
	_ = z.SetCellValue(zs, "D6", "Người Không Có")
	_ = z.SetCellValue(zs, "E6", "0911111111")
	_ = z.SetCellValue(zs, "H7", "X")
	zb, _ := z.WriteToBuffer()
	_ = z.Close()
	st, _, b = planner.raw(http.MethodPost, path+"/import", key(), zb.Bytes())
	bad := decode(t, st, b, 200, "tệp có lỗi dòng")
	if is := list(bad, "issues"); len(is) != 2 || strings.Contains(string(b), "0911111111") {
		t.Fatalf("lỗi theo dòng (không lặp SĐT): %s", b)
	}

	// Tệp sai: không phải xlsx, sai tháng, quá cỡ.
	st, _, b = planner.raw(http.MethodPost, path+"/import", key(), []byte("STT,TÊN"))
	decode(t, st, b, 422, "không phải xlsx")
	st, _, b = planner.raw(http.MethodPost, "/v1/extension/cnts/duty-rosters/2027-09/import", key(), xlsx)
	if m := decode(t, st, b, 422, "sai tháng"); str(m, "code") != "extension.cnts_roster_import_invalid" || !strings.Contains(str(m, "detail"), "10/2027") {
		t.Fatalf("sai tháng: %s", b)
	}
	big := append([]byte("PK"), bytes.Repeat([]byte{0}, 4<<20)...)
	if st, _, b = planner.raw(http.MethodPost, path+"/import", key(), big); st != 422 && st != 413 {
		t.Fatalf("quá 4 MB: %d %s", st, b)
	}
}

func decode(t *testing.T, st int, b []byte, wantStatus int, what string) map[string]any {
	t.Helper()
	if st != wantStatus {
		t.Fatalf("%s: muốn %d, nhận %d %s", what, wantStatus, st, b)
	}
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	_ = dec.Decode(&m)
	return m
}
