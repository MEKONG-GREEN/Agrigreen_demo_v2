package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/e2e/harness"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// R-95: không bao giờ sinh dữ liệu tổng hợp trên production.
func TestRefusesProduction(t *testing.T) {
	for _, env := range []string{"production", "PRODUCTION", " prod "} {
		_, err := Run(context.Background(), Config{Env: env, Token: "x", Scale: "small", AppURL: "http://127.0.0.1:1"})
		if !errors.Is(err, ErrProduction) {
			t.Fatalf("AG_ENV=%q phải bị từ chối, nhận %v", env, err)
		}
	}
	if _, err := Run(context.Background(), Config{Env: "dev", Token: "x", Scale: "huge"}); err == nil {
		t.Fatal("scale lạ phải lỗi")
	}
}

func fixtureUnits() ([]Unit, map[string][]Unit) {
	provs := []Unit{{Code: "92", Name: "Cần Thơ"}, {Code: "91", Name: "An Giang"}, {Code: "82", Name: "Đồng Tháp"}}
	communes := map[string][]Unit{}
	for _, p := range provs {
		for i := 1; i <= 6; i++ {
			// Mã giả lập chỉ dùng trong kiểm thử (không phải mã TCTK).
			communes[p.Code] = append(communes[p.Code], Unit{Code: fmt.Sprintf("FX-%s-%d", p.Code, i), Name: fmt.Sprintf("Xã %d", i), Parent: p.Code})
		}
	}
	communes["92"][0].Code, communes["92"][0].Name = "31135", "Phường Ninh Kiều" // xã có toạ độ trong bảng (mã QĐ 19/2025)
	return provs, communes
}

func planPersons(p Plan) []Person {
	var out []Person
	for _, pp := range p.Provinces {
		for _, cp := range pp.Communes {
			for _, c := range cp.Coops {
				out = append(out, c.Rep, c.Owner)
				for _, h := range c.Households {
					out = append(out, h.Person)
				}
			}
		}
	}
	return out
}

// Tất định theo seed; phân phối và ràng buộc của R-95 (không người thật, toạ độ ĐBSCL).
func TestPlanDeterministicAndPlausible(t *testing.T) {
	provs, communes := fixtureUnits()
	day := time.Date(2026, 9, 28, 10, 0, 0, 0, clock.Zone())
	a := BuildPlan(42, scales["medium"], provs, communes, day)
	b := BuildPlan(42, scales["medium"], provs, communes, day)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("cùng seed phải sinh cùng kế hoạch")
	}
	if c := BuildPlan(43, scales["medium"], provs, communes, day); reflect.DeepEqual(a, c) {
		t.Fatal("seed khác phải sinh dữ liệu khác")
	}
	if len(a.Provinces) != 3 || a.Provinces[0].Code != "92" {
		t.Fatalf("thứ tự tỉnh: %v", len(a.Provinces))
	}
	phones := map[string]bool{}
	for _, p := range planPersons(a) {
		if len(p.Phone) != 10 || (!strings.HasPrefix(p.Phone, "09990") && !strings.HasPrefix(p.Phone, "09991")) {
			t.Fatalf("SĐT phải thuộc dải tổng hợp 0999[01]xxxxx: %q", p.Phone)
		}
		if phones[p.Phone] {
			t.Fatalf("SĐT trùng trong một lần sinh: %q", p.Phone)
		}
		phones[p.Phone] = true
		if len(strings.Fields(p.FullName)) != 3 {
			t.Fatalf("họ tên phải gồm họ, đệm, tên: %q", p.FullName)
		}
	}
	plots, statuses := 0, map[string]bool{}
	for _, pp := range a.Provinces {
		for _, cp := range pp.Communes {
			if cp.Center.Lat < 8.5 || cp.Center.Lat > 11.2 || cp.Center.Lon < 104.4 || cp.Center.Lon > 106.9 {
				t.Fatalf("tâm xã %s ngoài ĐBSCL: %v", cp.Code, cp.Center)
			}
			var boxes [][4]float64
			for _, c := range cp.Coops {
				for _, m := range c.Machines {
					statuses[m.Status] = true
				}
				for _, h := range c.Households {
					if h.NationalID != "" && (len(h.NationalID) != 12 || h.NationalID[3:5] != "99") {
						t.Fatalf("CCCD tổng hợp sai dạng: %q", h.NationalID)
					}
					for _, pl := range h.Plots {
						plots++
						if pl.AreaHa < 0.3 || pl.AreaHa > 3 {
							t.Fatalf("diện tích thửa %.3f ha ngoài 0,3–3", pl.AreaHa)
						}
						if len(pl.Ring) != 5 || pl.Ring[0] != pl.Ring[4] {
							t.Fatalf("vòng thửa phải là tứ giác khép kín: %v", pl.Ring)
						}
						bx := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
						for _, pt := range pl.Ring {
							if math.Abs(pt[0]-cp.Center.Lon) > 0.05 || math.Abs(pt[1]-cp.Center.Lat) > 0.05 {
								t.Fatalf("thửa lệch quá 0,05° khỏi tâm xã %s", cp.Code)
							}
							bx = [4]float64{math.Min(bx[0], pt[0]), math.Min(bx[1], pt[1]), math.Max(bx[2], pt[0]), math.Max(bx[3], pt[1])}
						}
						for _, o := range boxes {
							if bx[0] < o[2] && o[0] < bx[2] && bx[1] < o[3] && o[1] < bx[3] {
								t.Fatalf("hai thửa trong xã %s chồng lấn khung bao", cp.Code)
							}
						}
						boxes = append(boxes, bx)
					}
				}
			}
		}
	}
	if plots < 100 || !statuses["hoat_dong"] {
		t.Fatalf("quy mô medium quá nhỏ: %d thửa, trạng thái máy %v", plots, statuses)
	}
}

// Chạy bộ sinh (small) với nền tảng thật trong tiến trình qua HTTP; chạy lại cùng
// seed không tạo thêm bản ghi.
func TestSeedAgainstInProcessStack(t *testing.T) {
	s := harness.New(t)
	tid := harness.NewTenant()
	u := s.User(harness.UserSpec{Tenant: tid, MFA: true, Roles: []string{
		"platform_admin", "masterdata_admin", "registry_admin", "vault_registrar", "kn_province_admin", "cgh_admin"}})
	logs := &harness.LogBuffer{}
	cfg := Config{AppURL: s.App.URL, AdminURL: s.Admin.URL, Token: u.Token, Seed: 7, Scale: "small", Env: "dev",
		Log: telemetry.NewLogger("seed-synthetic", logs)}
	st, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	sc := scales["small"]
	coops := sc.Provinces * sc.CommunesPerProv * sc.CoopsPerCommune
	want := map[string]int{
		"cooperative": coops, "household": coops * sc.HouseholdsPerCoop, "owner": coops,
		"task": sc.Provinces * sc.CommunesPerProv * sc.TasksPerCommune, "balance_run": 2, "season_area": coops * 2,
	}
	for k, n := range want {
		if st.Created[k] != n {
			t.Errorf("%s: muốn tạo %d, tạo %d (%v)", k, n, st.Created[k], st.Created)
		}
	}
	if st.Created["plot"] < coops*sc.HouseholdsPerCoop || st.Created["machine"] == 0 || st.Created["field_report"] == 0 {
		t.Fatalf("thiếu thửa/máy/báo cáo: %v", st.Created)
	}

	// Kiểm qua API như người dùng: cán bộ TTKN QG đọc HTX; Cục KTHT đọc kết quả cân đối.
	viewer := s.User(harness.UserSpec{Tenant: tid, Roles: []string{"platform_admin", "cgh_viewer"}})
	r := viewer.Do(harness.App, http.MethodGet, "/v1/cooperatives?limit=200", "", nil)
	if r.Status != 200 || len(r.Items()) != coops {
		t.Fatalf("danh sách HTX: %d %d", r.Status, len(r.Items()))
	}
	plan := BuildPlan(7, sc, nil, nil, time.Now().In(clock.Zone()))
	runs := viewer.Do(harness.App, http.MethodGet, fmt.Sprintf("/v1/mechanization/balance-runs?season_code=DX&season_year=%d", plan.SeasonYear), "", nil)
	if runs.Status != 200 || len(runs.Items()) != 1 {
		t.Fatalf("lượt cân đối DX: %d %s", runs.Status, runs.Raw)
	}
	res := viewer.Do(harness.App, http.MethodGet, "/v1/mechanization/balance-runs/"+runs.Items()[0]["id"].(string), "", nil)
	seen := map[string]bool{}
	for _, it := range res.Items("results") {
		seen[it["status"].(string)] = true
	}
	if len(seen) < 2 || seen["unclassified"] {
		t.Fatalf("kết quả cân đối phải phân loại được và đa dạng: %v", seen)
	}

	// Chạy lại cùng seed: idempotent — không bản ghi mới.
	st2, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"org", "cooperative", "household", "plot", "owner", "machine", "task", "field_report"} {
		if st2.Created[k] != 0 {
			t.Errorf("chạy lại tạo thêm %d %s", st2.Created[k], k)
		}
	}
	if r := viewer.Do(harness.App, http.MethodGet, "/v1/cooperatives?limit=200", "", nil); len(r.Items()) != coops {
		t.Fatalf("chạy lại làm tăng số HTX: %d", len(r.Items()))
	}

	// Log của bộ sinh và của nền tảng không chứa định danh tổng hợp (R-14).
	for _, line := range []string{logs.String(), s.Logs.String()} {
		if strings.Contains(line, "09990") || strings.Contains(line, "09991") || strings.Contains(line, "Ấp Tổng Hợp") {
			t.Fatal("log chứa SĐT/địa chỉ tổng hợp")
		}
	}
}
