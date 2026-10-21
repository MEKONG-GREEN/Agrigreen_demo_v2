package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
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
)

var update = flag.Bool("update", false, "ghi lại testdata (tệp mẫu tổng hợp + báo cáo vàng)")

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

const (
	samplePath = "testdata/nhan-su-tong-hop.xlsx"
	goldenPath = "testdata/bao-cao-chay-thu.golden.csv"
)

type unitsStub []port.Unit

func (u unitsStub) InternalUnits(context.Context, uuid.UUID) ([]port.Unit, error) { return u, nil }

type fixture struct {
	t     *testing.T
	srv   *httptest.Server
	vault *clienttest.Fake
	env   *pgtest.Env
	tid   uuid.UUID
	who   map[string]authz.Principal
}

// newFixture dựng pf/hrm THẬT (DB, sổ chỉ ghi thêm, RLS) sau HTTP /v1, kho định danh giả trong bộ nhớ, xác thực giả
// theo Bearer (token = tên người gọi).
func newFixture(t *testing.T) *fixture {
	env := pgtest.Get(t)
	now := time.Date(2026, 10, 20, 3, 0, 0, 0, time.UTC)
	mod, err := hrm.New(env.Pool(t, migrations.PF, dbadmin.AppPlatform), clock.NewFixed(now),
		port.Access{Session: extension.HRMSession, Department: "hanh_chinh_tong_hop", ManageBy: []string{"admin"}, ReadBy: []string{"kn_tinh"}})
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, vault: clienttest.New(), env: env, tid: ids.New()}
	org := ids.New()
	mod.UseRecords(f.vault, unitsStub{{Code: "hcth", Name: "Phòng Hành chính - Tổng hợp", Active: true},
		{Code: "trong_trot", Name: "Phòng Trồng trọt", Active: true}, {Code: "chan_nuoi", Name: "Phòng Chăn nuôi - Thủy sản", Active: true},
		{Code: "da_ngung", Name: "Trạm cũ", Active: false}}, true)
	f.who = map[string]authz.Principal{
		"admin": {ActorRef: ids.New(), TenantID: f.tid, KN: &authz.KNContext{Role: "admin", Scope: authz.Scope{All: true}}},
		"hcth": {ActorRef: ids.New(), TenantID: f.tid, KN: &authz.KNContext{Role: "kn_tinh", Department: "hanh_chinh_tong_hop",
			InternalUnit: "hcth", UnitRef: org}},
		"vc_tt": {ActorRef: ids.New(), TenantID: f.tid, KN: &authz.KNContext{Role: "kn_tinh", Department: "trong_trot",
			InternalUnit: "trong_trot", UnitRef: org}},
	}
	for as, p := range f.who {
		f.who[as] = bindSession(t, p)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if p, ok := f.who[strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")]; ok {
			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
		}
	})
	mod.RegisterApp(r.Group("/v1"))
	f.srv = httptest.NewServer(r)
	t.Cleanup(f.srv.Close)
	a := &api{base: f.srv.URL + "/v1", token: "admin", hc: f.srv.Client()}
	if _, err := a.do(context.Background(), http.MethodPost, "/hrm/defaults", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	return f
}

// bindSession gắn ngữ cảnh fixture vào phiên chủ KN thật (registry + bộ mã chủ phiên của pf/extension), như middleware
// pf/accounts ở production dựng từ Bearer + X-AG-Context — mẫu của KNSPLIT-2-CONTEXT-I. Mã ngữ cảnh sinh ở đây chỉ
// dùng cho kiểm thử; pf/hrm không còn đọc Principal.KN.
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

func (f *fixture) cfg(as, report string, commit, skip bool) Config {
	return Config{In: samplePath, Report: report, AsOf: "2026-08-31", Commit: commit, SkipErrors: skip, API: f.srv.URL + "/v1",
		Token: as, HTTP: f.srv.Client()}
}

// ledger đếm hồ sơ và dòng quá trình qua Application API (Admin) — mã ngoài module KN không truy vấn thẳng schema pf_hrm
// (knboundary sql-into-kn); kiểm "sổ không chứa dữ liệu cá nhân" thuộc kiểm thử của pf/hrm (TestHRMRecordsHTTP_W1122).
func (f *fixture) ledger() (staff, events int, byCode map[string]map[string]any) {
	f.t.Helper()
	ctx := context.Background()
	a := &api{base: f.srv.URL + "/v1", token: "admin", hc: f.srv.Client()}
	var list struct {
		Total int `json:"total"`
		Items []struct {
			ID   string `json:"id"`
			Code string `json:"code"`
		} `json:"items"`
	}
	if _, err := a.do(ctx, http.MethodGet, "/hrm/staff?limit=100", "", nil, &list); err != nil {
		f.t.Fatal(err)
	}
	byCode = map[string]map[string]any{}
	for _, it := range list.Items {
		var d struct {
			Events  []any          `json:"events"`
			Current map[string]any `json:"current"`
		}
		if _, err := a.do(ctx, http.MethodGet, "/hrm/staff/"+it.ID, "", nil, &d); err != nil {
			f.t.Fatal(err)
		}
		events += len(d.Events)
		byCode[it.Code] = d.Current
	}
	return list.Total, events, byCode
}

func readReport(t *testing.T, path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// W11.30 (D3 H10) đầu–cuối qua Application API pf/hrm THẬT trên PostgreSQL: chạy thử báo lỗi theo dòng và không ghi gì
// (báo cáo khớp tệp vàng, không có dữ liệu cá nhân); ghi bị chặn khi còn lỗi; -skip-errors ghi các dòng hợp lệ qua lớp
// app (mã tự sinh theo thứ tự tệp, sổ chỉ ghi thêm, PII ở kho định danh, thẻ TK ở ngăn riêng); chạy lại không ghi đôi.
func TestHRMImport_W1130(t *testing.T) {
	if *update {
		if err := writeSample(samplePath); err != nil {
			t.Fatal(err)
		}
	}
	f := newFixture(t)
	dir := t.TempDir()
	ctx := context.Background()

	// Người không có quyền profile.edit toàn Trung tâm: dừng trước khi đọc tệp vào hệ thống.
	if _, err := Run(ctx, f.cfg("vc_tt", filepath.Join(dir, "x.csv"), false, false)); err == nil ||
		!strings.Contains(err.Error(), "profile.edit") {
		t.Fatalf("thiếu quyền: %v", err)
	}

	// 1. Chạy thử: lỗi theo dòng, không ghi.
	dry := filepath.Join(dir, "dry.csv")
	s, err := Run(ctx, f.cfg("hcth", dry, false, false))
	if !errors.Is(err, errRowErrors) {
		t.Fatalf("chạy thử: %v", err)
	}
	if s.Total != 10 || s.Valid != 5 || s.Errors != 5 || s.Created != 0 {
		t.Fatalf("tổng kết chạy thử: %+v", s)
	}
	got := readReport(t, dry)
	if *update {
		if err := os.WriteFile(goldenPath, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if want := readReport(t, goldenPath); got != want {
		t.Fatalf("báo cáo chạy thử khác tệp vàng %s:\n%s", goldenPath, got)
	}
	for _, pii := range []string{"Nguyễn Văn An", "Lê Thị Bình", "0900000101", "1985-03-15", "an@example.invalid"} {
		if strings.Contains(got, pii) {
			t.Fatalf("báo cáo chứa dữ liệu cá nhân %q", pii)
		}
	}
	if n, _, _ := f.ledger(); n != 0 {
		t.Fatalf("chạy thử đã ghi %d hồ sơ", n)
	}
	for _, a := range f.vault.Accesses() {
		if a.Op == "create" || a.Op == "section_write" {
			t.Fatalf("chạy thử đã ghi kho định danh: %+v", a)
		}
	}

	// 2. Ghi khi còn dòng lỗi mà không -skip-errors: không ghi dòng nào.
	if _, err := Run(ctx, f.cfg("hcth", filepath.Join(dir, "refuse.csv"), true, false)); err == nil || errors.Is(err, errRowErrors) {
		t.Fatalf("ghi khi còn lỗi phải bị từ chối: %v", err)
	}
	if n, _, _ := f.ledger(); n != 0 {
		t.Fatalf("ghi bị từ chối nhưng có %d hồ sơ", n)
	}

	// 3. Ghi các dòng hợp lệ (báo cáo .xlsx).
	rep := filepath.Join(dir, "commit.xlsx")
	s, err = Run(ctx, f.cfg("hcth", rep, true, true))
	if !errors.Is(err, errRowErrors) || s.Created != 5 || s.Errors != 5 || s.Exists != 0 {
		t.Fatalf("ghi: %+v %v", s, err)
	}
	codes := reportCodes(t, rep)
	want := map[string]string{"2": "VC-00001", "3": "HD-00001", "5": "XA-00001", "6": "VC-00002", "10": "VC-00003"}
	for line, code := range want {
		if codes[line] != code {
			t.Fatalf("dòng %s: mã %q, muốn %s (%v)", line, codes[line], code, codes)
		}
	}
	staff, events, cur := f.ledger()
	if staff != 5 || events < 15 {
		t.Fatalf("sổ: %d hồ sơ, %d dòng quá trình", staff, events)
	}
	// Quá trình ban đầu đúng như tệp: dòng 2 trưởng phòng HCTH, ngạch 01.003 bậc 4; dòng 6 đã nghỉ hưu; dòng 3 bậc đọc
	// từ ô bị Excel đổi thành ngày; cán bộ xã gắn xã + đơn vị quản lý.
	chk := func(code string, want map[string]any) {
		for k, v := range want {
			if got := cur[code][k]; fmt.Sprint(got) != fmt.Sprint(v) {
				t.Fatalf("%s.%s = %v, muốn %v (%v)", code, k, got, v, cur[code])
			}
		}
	}
	chk("VC-00001", map[string]any{"status": "active", "unit": "hcth", "position": "truong_phong", "grade": "01.003", "step": 4,
		"coefficient": "3.33", "step_since": "2023-01-01", "top_degree": "dai_hoc"})
	chk("HD-00001", map[string]any{"status": "active", "unit": "trong_trot", "position": "vien_chuc", "grade": "V.05.02.07", "step": 3})
	chk("XA-00001", map[string]any{"commune": "30001", "mgmt_unit": "trong_trot", "top_degree": "trung_cap"})
	chk("VC-00002", map[string]any{"status": "left", "leave_reason": "nghi_huu", "grade": "01.002", "step": 8})
	chk("VC-00003", map[string]any{"unit": "chan_nuoi", "started_on": "2020-01-01", "step": 2, "top_degree": "thac_si"})
	pctx := authz.WithPrincipal(ctx, f.who["hcth"])
	ref, err := f.vault.Lookup(pctx, client.PurposeHRMProfile, "0900000101")
	if err != nil {
		t.Fatal(err)
	}
	id, err := f.vault.Resolve(pctx, client.PurposeHRMProfile, ref)
	if err != nil || id.FullName == "" || !strings.Contains(id.FullName, "An") {
		t.Fatalf("kho định danh: %+v %v", id, err)
	}
	prof, err := f.vault.GetSection(pctx, client.PurposeHRMProfile, ref, client.SectionHRMProfile)
	if err != nil || prof["birth_date"] != "1985-03-15" || prof["email"] != "an@example.invalid" {
		t.Fatalf("ngăn hrm.profile: %v %v", prof, err)
	}
	sens, err := f.vault.GetSection(pctx, client.PurposeHRMSensitive, ref, client.SectionHRMSensitive)
	if err != nil || sens["ethnicity"] != "kinh" {
		t.Fatalf("ngăn hrm.sensitive: %v %v", sens, err)
	}
	// Dòng 3: SĐT mất số 0 đầu, năm sinh ở cột Nữ, họ tên có ghi chú → đã chuẩn hóa.
	ref3, err := f.vault.Lookup(pctx, client.PurposeHRMProfile, "0900000102")
	if err != nil {
		t.Fatal(err)
	}
	id3, _ := f.vault.Resolve(pctx, client.PurposeHRMProfile, ref3)
	p3, _ := f.vault.GetSection(pctx, client.PurposeHRMProfile, ref3, client.SectionHRMProfile)
	if strings.ContainsAny(id3.FullName, "()0123456789") || p3["gender"] != "nu" || p3["birth_date"] != "1990" {
		t.Fatalf("dòng 3 chuẩn hóa: %q %v", id3.FullName, p3)
	}

	// 4. Chạy lại: không ghi đôi — mọi dòng đã ghi báo "Đã có".
	again := filepath.Join(dir, "again.csv")
	s, err = Run(ctx, f.cfg("hcth", again, true, true))
	if !errors.Is(err, errRowErrors) || s.Created != 0 || s.Exists != 5 {
		t.Fatalf("chạy lại: %+v %v", s, err)
	}
	if s2, e2, _ := f.ledger(); s2 != staff || e2 != events {
		t.Fatalf("chạy lại đã ghi thêm dòng sổ: %d/%d → %d/%d", staff, events, s2, e2)
	}
	if r := readReport(t, again); strings.Count(r, ","+StatusExists+",") < 5 {
		t.Fatalf("báo cáo chạy lại:\n%s", r)
	}
}

// reportCodes đọc báo cáo .xlsx: dòng nguồn → mã hồ sơ.
func reportCodes(t *testing.T, path string) map[string]string {
	x, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = x.Close() }()
	rows, err := x.GetRows(x.GetSheetList()[0])
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, r := range rows[1:] {
		if len(r) > 3 && r[3] != "" {
			out[r[0]] = r[3]
		}
	}
	return out
}

// writeSample ghi tệp mẫu tổng hợp (dữ liệu giả) mô phỏng tệp cũ của HCTH: tiêu đề theo nhiều cách viết, năm sinh ở cột
// Nam/Nữ, ô ngày định dạng ngày, SĐT lưu dạng số, bậc "3/9" bị Excel đổi thành ngày, ngày lưu dạng số Excel, hệ số dạng
// chữ, ghi chú trong họ tên, tên phòng viết tắt — cùng các dòng lỗi điển hình.
func writeSample(path string) error {
	x := excelize.NewFile()
	defer func() { _ = x.Close() }()
	const sh = "Sheet1"
	header := []string{"STT", "Loại", "Họ và tên", "Giới tính", "Ngày sinh", "Năm sinh Nam", "Năm sinh Nữ", "SĐT", "Email", "Phòng",
		"Mã xã", "Đơn vị quản lý", "Chức vụ", "Ngày bổ nhiệm", "Ngày TGCM", "Ngày nghỉ", "Lý do nghỉ", "Ngạch", "Bậc", "Hệ số",
		"Thời điểm được xếp", "Trình độ", "Chuyên ngành", "Hình thức đào tạo", "LLCT", "Dân tộc", "Ghi chú"}
	type cell = any
	date := func(y, m, d int) time.Time { return time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC) }
	rows := [][]cell{
		{"HC-01", "Viên chức", "Nguyễn Văn An", "Nam", "15/03/1985", "", "", "0900000101", "an@example.invalid", "HC-TH", "", "",
			"Trưởng phòng", "01/07/2020", "01/06/2010", "", "", "Chuyên viên (01.003)", "4", "3,33", "01/01/2023", "Đại học", "",
			"Chính quy", "Trung cấp", "Kinh", ""},
		{"HC-02", "Hợp đồng lao động", "Lê Thị Bình (19 VC)", "", "", "", "1990", 900000102, "", "Phòng Trồng trọt", "", "",
			"Nhân viên", "", date(2015, 5, 4), "", "", "V.05.02.07", date(2026, 9, 3), "", "", "", "", "", "", "", ""},
		{"HC-03", "Viên chức", "Phạm Văn Cường", "Nam", "30/02/1988", "", "", "0900000103", "", "Phòng Chăn nuôi - Thủy sản", "", "",
			"Phó Giám đốc", "", "2012", "", "", "01.003", "", "2,67", "", "", "", "", "", "", ""},
		{"XA-01", "Cán bộ xã", "Trần Thị Dung", "Nữ", "1993", "", "", "0900000104", "", "", "30001", "Trồng trọt",
			"Viên chức", "", "03/2018", "", "", "", "", "", "", "TC", "", "", "", "Kinh", ""},
		{"HC-04", "Viên chức", "Võ Văn Em", "Nam", "01/01/1960", "", "", "0900000105", "", "HCTH", "", "",
			"Viên chức", "", "01/09/1985", "31/01/2021", "Nghỉ hưu", "Chuyên viên chính", "8/8", "6,78", "", "", "", "", "", "", ""},
		{"HC-05", "Viên chức", "Đỗ Thị Gấm", "Nữ", "05/1991", "", "", "0900000101", "", "Trạm cũ", "", "",
			"Kế toán trưởng", "", "01/02/2016", "", "", "Chuyên viên", "12", "", "", "", "", "", "", "", ""},
		{"HC-06", "Thực tập sinh", "Huỳnh Văn Hải", "X", "", "", "", "12345", "hai@", "", "", "",
			"", "", "", "", "", "", "3", "", "", "", "", "", "", "", "=HYPERLINK(\"x\")"},
		{"HC-07", "Viên chức", "Nguyễn Văn An", "Nam", "15/03/1985", "", "", "0900000107", "", "HCTH", "", "",
			"Viên chức", "", "01/06/2011", "01/01/2011", "Nghỉ hưu", "01.003", "3", "3,33", "", "", "", "", "", "", ""},
		{"HC-08", "Viên chức", "Bùi Thị Kim", "Nữ", "20/10/1995", "", "", "+84 900 000 108", "", "chan_nuoi", "", "",
			"vien_chuc", "", 43831, "", "", "Khuyến nông viên hạng III", "Bậc 2", "", "", "Thạc sĩ", "Trồng trọt", "Tại chức", "", "", ""},
		{"=1+1", "HĐLĐ", "Mai Văn Lâm", "Nam", "1979", "", "", "0900000109", "", "HCTH", "", "",
			"Viên chức", "", "01/03/2019", "30/06/2026", "", "", "", "", "", "", "", "", "", "", ""},
	}
	dateStyle, err := x.NewStyle(&excelize.Style{NumFmt: 14})
	if err != nil {
		return err
	}
	for j, h := range header {
		ref, _ := excelize.CoordinatesToCellName(j+1, 1)
		if err := x.SetCellStr(sh, ref, h); err != nil {
			return err
		}
	}
	for i, row := range rows {
		for j, v := range row {
			ref, _ := excelize.CoordinatesToCellName(j+1, i+2)
			switch v := v.(type) {
			case string:
				if v != "" {
					if err := x.SetCellStr(sh, ref, v); err != nil {
						return err
					}
				}
			case int:
				if err := x.SetCellInt(sh, ref, int64(v)); err != nil {
					return err
				}
			case time.Time:
				if err := x.SetCellValue(sh, ref, v); err != nil {
					return err
				}
				if err := x.SetCellStyle(sh, ref, ref, dateStyle); err != nil {
					return err
				}
			}
		}
	}
	var b bytes.Buffer
	if err := x.Write(&b); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, b.Bytes(), 0o600)
}
