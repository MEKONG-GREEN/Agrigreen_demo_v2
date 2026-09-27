package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"
)

// Scale là quy mô dữ liệu sinh ra.
type Scale struct {
	Name               string
	Provinces          int // số tỉnh ĐBSCL (theo thứ tự provinceOrder)
	CommunesPerProv    int
	CoopsPerCommune    int
	HouseholdsPerCoop  int
	MaxPlotsPerHH      int
	TasksPerCommune    int
	PestReportsPerComm int
}

// Các quy mô hỗ trợ.
var scales = map[string]Scale{
	"small":  {Name: "small", Provinces: 2, CommunesPerProv: 2, CoopsPerCommune: 1, HouseholdsPerCoop: 4, MaxPlotsPerHH: 2, TasksPerCommune: 2, PestReportsPerComm: 1},
	"medium": {Name: "medium", Provinces: 6, CommunesPerProv: 5, CoopsPerCommune: 2, HouseholdsPerCoop: 12, MaxPlotsPerHH: 2, TasksPerCommune: 4, PestReportsPerComm: 2},
}

// provinceOrder: tỉnh sau sắp xếp 2025, Cần Thơ trước.
var provinceOrder = []string{"92", "91", "82", "86", "96", "80"}

// Unit là đơn vị hành chính đọc từ pf/masterdata.
type Unit struct{ Code, Name, Parent string }

// Person là người tổng hợp (chủ hộ, người đại diện, chủ máy) — chỉ gửi vào vault.
type Person struct {
	FullName string `pii:"true"`
	Phone    string `pii:"true"`
}

// Plan là toàn bộ dữ liệu sẽ sinh — hàm thuần của (seed, scale, danh sách đơn vị).
type Plan struct {
	Seed       uint64
	Scale      string
	SeasonYear int
	Period     string // tháng báo cáo YYYY-MM
	SownOn     string // ngày gieo YYYY-MM-DD
	Provinces  []ProvincePlan
}

// ProvincePlan là một tỉnh.
type ProvincePlan struct {
	Code, Name string
	Staff      []StaffPlan // cán bộ TTKN tỉnh (danh bạ trực dự phòng)
	Communes   []CommunePlan
}

// CommunePlan là một xã.
type CommunePlan struct {
	Code, Name  string
	Center      LatLon
	Staff       []StaffPlan
	Coops       []CoopPlan
	Tasks       []TaskPlan
	PestReports []PestPlan
}

// StaffPlan là một cán bộ trong danh bạ trực (mã người dùng tổng hợp, không có tài khoản thật).
type StaffPlan struct {
	ActorRef    uuid.UUID
	Title, Name string
}

// CoopPlan là một HTX cùng mọi dữ liệu của nó.
type CoopPlan struct {
	Name        string
	TaxCode     string
	AreaHa      string
	Established string
	Location    LatLon
	Rep         Person
	Households  []HouseholdPlan
	Owner       Person
	Machines    []MachinePlan
	SeasonAreas []SeasonAreaPlan
	Sowing      []SowingPlan
}

// HouseholdPlan là một nông hộ.
type HouseholdPlan struct {
	Person
	NationalID   string `pii:"true"`
	Address      string `pii:"true"`
	CultivatedHa string
	MemberSince  string
	Plots        []PlotPlan
}

// PlotPlan là một thửa.
type PlotPlan struct {
	Ref    uuid.UUID
	Name   string
	AreaHa float64
	Ring   [][2]float64 // [lon, lat], khép kín
}

// MachinePlan là một máy.
type MachinePlan struct {
	Type, Brand, Model, Serial, Fuel, PowerHP, OwnedSince, Status string
	YearMade                                                      int
}

// SeasonAreaPlan là diện tích một vụ của HTX.
type SeasonAreaPlan struct {
	Season string
	AreaHa string
}

// SowingPlan là báo cáo tiến độ gieo sạ của HTX.
type SowingPlan struct {
	ID              uuid.UUID
	Variety, AreaHa string
}

// TaskPlan là một nhiệm vụ hỗ trợ.
type TaskPlan struct {
	ID                              uuid.UUID
	Category, Priority, Description string
	Resolve                         bool
	CoopIndex                       int // -1: không gắn HTX
}

// PestPlan là một báo cáo sâu bệnh.
type PestPlan struct {
	ID                                    uuid.UUID
	Stage, Pest, Severity, AffectedAreaHa string
	Urgent                                bool
}

// weighted chọn theo trọng số.
func weighted[T any](r *rand.Rand, items []T, w []int) T {
	total := 0
	for _, x := range w {
		total += x
	}
	n := r.IntN(total)
	for i, x := range w {
		if n < x {
			return items[i]
		}
		n -= x
	}
	return items[len(items)-1]
}

var (
	varieties       = []string{"OM5451", "OM18", "DT8", "ST25", "IR50404", "JASMINE85"}
	varietyW        = []int{30, 20, 15, 15, 10, 10}
	taskCategories  = []string{"sau_benh", "ky_thuat", "co_gioi", "khac"}
	taskCategoryW   = []int{45, 30, 15, 10}
	priorities      = []string{"low", "normal", "high", "urgent"}
	priorityW       = []int{15, 55, 22, 8}
	pests           = []string{"ray_nau", "sau_cuon_la", "dao_on", "bac_la", "oc_buou_vang", "chuot"}
	pestW           = []int{30, 20, 20, 10, 12, 8}
	stages          = []string{"ma", "de_nhanh", "lam_dong", "tro_bong"}
	severities      = []string{"nhe", "trung_binh", "nang"}
	severityW       = []int{55, 35, 10}
	machineTypes    = []string{"MLD", "MGS", "MPT", "MGD"}
	machineNorms    = map[string]float64{"MLD": 120, "MGS": 100, "MPT": 260, "MGD": 90} // ha/máy/vụ (định mức seed QT-01)
	brands          = [][2]string{{"Kubota", "L4018"}, {"Yanmar", "EF393T"}, {"Kubota", "DC-70"}, {"DJI", "Agras T40"}, {"Iseki", "NT540"}}
	taskDescription = map[string][]string{
		"sau_benh": {"Rầy nâu xuất hiện mật độ cao ở cánh đồng phía đông, cần hướng dẫn phòng trừ", "Lúa có dấu hiệu đạo ôn lá, đề nghị cán bộ kiểm tra đồng ruộng", "Ốc bươu vàng phá hại lúa mới sạ, cần tư vấn biện pháp"},
		"ky_thuat": {"Hướng dẫn kỹ thuật tưới ngập khô xen kẽ cho tổ hợp tác", "Tư vấn lượng phân bón thúc đợt hai cho giống lúa mới", "Hướng dẫn ghi nhật ký canh tác trên ứng dụng"},
		"co_gioi":  {"Cần hỗ trợ điều phối máy gặt đập cho vụ sắp thu hoạch", "Tư vấn chọn máy sạ hàng phù hợp ruộng nhỏ"},
		"khac":     {"Đề nghị tổ chức buổi tập huấn đầu vụ cho thành viên HTX", "Hỗ trợ đăng ký tham gia chương trình lúa giảm phát thải"},
	}
	orgSuffix = []string{"Tổng Hợp", "Mô Phỏng", "Thử Nghiệm", "Dữ Liệu Mẫu"}
)

// uuidV7 sinh UUIDv7 TẤT ĐỊNH từ r: mốc thời gian cố định + thứ tự, phần ngẫu nhiên từ r.
func uuidV7(r *rand.Rand, seq int) uuid.UUID {
	var u uuid.UUID
	ms := uint64(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()) + uint64(seq) //nolint:gosec // seq nhỏ, dương
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], ms)
	copy(u[0:6], ts[2:8])
	binary.BigEndian.PutUint64(u[8:16], r.Uint64())
	binary.BigEndian.PutUint16(u[6:8], uint16(r.Uint32()))
	u[6] = (u[6] & 0x0f) | 0x70 // phiên bản 7
	u[8] = (u[8] & 0x3f) | 0x80 // biến thể RFC 9562
	return u
}

func round2(v float64) string { return fmt.Sprintf("%.2f", math.Round(v*100)/100) }

// BuildPlan dựng kế hoạch sinh dữ liệu. communes: xã hiện hành theo tỉnh (từ API).
// Cùng seed, scale, danh sách đơn vị và ngày → cùng kế hoạch.
func BuildPlan(seed uint64, sc Scale, provinces []Unit, communes map[string][]Unit, today time.Time) Plan {
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)) //nolint:gosec // dữ liệu tổng hợp tất định, không dùng cho bảo mật
	seq := 0
	next := func() uuid.UUID { seq++; return uuidV7(r, seq) }
	vn := today
	p := Plan{Seed: seed, Scale: sc.Name, SeasonYear: vn.Year(), Period: vn.Format("2006-01"),
		SownOn: vn.AddDate(0, 0, -min(vn.Day()-1, 1+r.IntN(10))).Format("2006-01-02")}
	hh, persons, taxSeq := 0, 0, 0
	byCode := map[string]Unit{}
	for _, u := range provinces {
		byCode[u.Code] = u
	}
	for _, code := range provinceOrder {
		if len(p.Provinces) >= sc.Provinces {
			break
		}
		pu, ok := byCode[code]
		if !ok || len(communes[code]) == 0 {
			continue
		}
		pp := ProvincePlan{Code: code, Name: pu.Name}
		pp.Staff = []StaffPlan{{ActorRef: next(), Title: "Cán bộ TTKN tỉnh", Name: "Cán bộ tỉnh " + code + " (tổng hợp)"}}
		// Chọn xã tất định: xáo trộn danh sách đã sắp theo mã.
		cs := append([]Unit(nil), communes[code]...)
		r.Shuffle(len(cs), func(i, j int) { cs[i], cs[j] = cs[j], cs[i] })
		for ci := 0; ci < sc.CommunesPerProv && ci < len(cs); ci++ {
			cu := cs[ci]
			cp := CommunePlan{Code: cu.Code, Name: cu.Name, Center: communeCenter(cu.Code, code)}
			for s := 0; s < 1+r.IntN(2); s++ {
				cp.Staff = append(cp.Staff, StaffPlan{ActorRef: next(), Title: "Khuyến nông viên xã", Name: fmt.Sprintf("KNV %s %d (tổng hợp)", cu.Name, s+1)})
			}
			plotIdx := 0
			for k := 0; k < sc.CoopsPerCommune; k++ {
				taxSeq++
				persons++
				coop := CoopPlan{
					Name:        fmt.Sprintf("HTX %s %s %d", cu.Name, orgSuffix[r.IntN(len(orgSuffix))], k+1),
					TaxCode:     fmt.Sprintf("99%08d", (int(seed%1000)*10000+taxSeq)%100000000),
					Established: fmt.Sprintf("%d-%02d-01", 2008+r.IntN(16), 1+r.IntN(12)),
					Location:    LatLon{round7(cp.Center.Lat + (r.Float64()-0.5)*0.01), round7(cp.Center.Lon + (r.Float64()-0.5)*0.01)},
					Rep:         Person{FullName: fakeName(r), Phone: fakePersonPhone(seed, persons)},
				}
				total := 0.0
				for h := 0; h < sc.HouseholdsPerCoop; h++ {
					hh++
					house := HouseholdPlan{Person: Person{FullName: fakeName(r), Phone: fakePhone(int(seed%10)*10000 + hh)},
						Address: fakeAddress(r, cu.Name), MemberSince: fmt.Sprintf("%d-%02d-15", 2015+r.IntN(11), 1+r.IntN(12))}
					if r.IntN(100) < 40 { // 40 % có CCCD tổng hợp: mã tỉnh 0xx + 99 + 7 số
						house.NationalID = fmt.Sprintf("0%s99%07d", code, hh%10000000)
					}
					area := 0.0
					for n := 0; n < 1+r.IntN(sc.MaxPlotsPerHH); n++ {
						a := plotArea(r)
						house.Plots = append(house.Plots, PlotPlan{Ref: next(), Name: fmt.Sprintf("Thửa %d-%d", hh, n+1), AreaHa: a,
							Ring: plotPolygon(r, cp.Center, plotIdx, a)})
						plotIdx++
						area += a
					}
					house.CultivatedHa = round2(area)
					total += area
					coop.Households = append(coop.Households, house)
				}
				// Diện tích vụ của HTX: đất của thành viên đã vẽ + đất chưa số hóa (hệ số 8–40).
				season := total * (8 + r.Float64()*32)
				coop.AreaHa = round2(season * 1.1)
				coop.SeasonAreas = []SeasonAreaPlan{{Season: "DX", AreaHa: round2(season)}, {Season: "HT", AreaHa: round2(season * (0.85 + r.Float64()*0.15))}}
				persons++
				coop.Owner = Person{FullName: fakeName(r), Phone: fakePersonPhone(seed, persons)}
				// Máy: số máy cần × hệ số 0,4–1,4 → đủ các mức Thiếu / Chú ý / Đủ / Thừa.
				serial := 0
				for _, mt := range machineTypes {
					need := season / machineNorms[mt]
					n := int(math.Round(need * (0.4 + r.Float64())))
					for m := 0; m < n && m < 12; m++ {
						serial++
						b := brands[r.IntN(len(brands))]
						coop.Machines = append(coop.Machines, MachinePlan{Type: mt, Brand: b[0], Model: b[1],
							Serial: fmt.Sprintf("SYN-%d-%s-%d-%03d", seed%100000, cu.Code, k+1, serial), Fuel: "Dầu",
							PowerHP: fmt.Sprintf("%d", 20+r.IntN(80)), YearMade: 2012 + r.IntN(14),
							OwnedSince: fmt.Sprintf("%d-%02d-01", 2016+r.IntN(9), 1+r.IntN(12)),
							Status:     weighted(r, []string{"hoat_dong", "bao_tri", "hong"}, []int{88, 8, 4})})
					}
				}
				coop.Sowing = []SowingPlan{{ID: next(), Variety: weighted(r, varieties, varietyW), AreaHa: round2(season * (0.3 + r.Float64()*0.7))}}
				cp.Coops = append(cp.Coops, coop)
			}
			for tIdx := 0; tIdx < sc.TasksPerCommune; tIdx++ {
				cat := weighted(r, taskCategories, taskCategoryW)
				ds := taskDescription[cat]
				cp.Tasks = append(cp.Tasks, TaskPlan{ID: next(), Category: cat, Priority: weighted(r, priorities, priorityW),
					Description: ds[r.IntN(len(ds))], Resolve: r.IntN(100) < 30, CoopIndex: r.IntN(len(cp.Coops)+1) - 1})
			}
			for pIdx := 0; pIdx < sc.PestReportsPerComm; pIdx++ {
				cp.PestReports = append(cp.PestReports, PestPlan{ID: next(), Stage: stages[r.IntN(len(stages))], Pest: weighted(r, pests, pestW),
					Severity: weighted(r, severities, severityW), AffectedAreaHa: round2(0.2 + r.Float64()*15), Urgent: r.IntN(100) < 15})
			}
			pp.Communes = append(pp.Communes, cp)
		}
		p.Provinces = append(p.Provinces, pp)
	}
	return p
}

// fakePersonPhone là SĐT tổng hợp của người đại diện/chủ máy: dải 09991xxxxx.
func fakePersonPhone(seed uint64, i int) string {
	return fmt.Sprintf("09991%05d", (int(seed%10)*10000+i)%100000)
}
