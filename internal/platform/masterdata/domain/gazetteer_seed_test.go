package domain

import (
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/seeddata"
)

// Số liệu đối chiếu của bộ seed — phải khớp bảng ở docs/requirements/danh-muc-hanh-chinh.md
// (nguồn: Cục Thống kê, QĐ 19/2025/QĐ-TTg; các NQ sắp xếp cấp xã của UBTVQH ngày 16/06/2025).
var (
	// Tỉnh mới → {tổng, xã, phường, đặc khu}.
	wantNewProvinces = map[string][4]int{
		"91": {102, 85, 14, 3},  // An Giang — NQ 1654/NQ-UBTVQH15
		"92": {103, 72, 31, 0},  // Cần Thơ — NQ 1668/NQ-UBTVQH15
		"82": {102, 82, 20, 0},  // Đồng Tháp — NQ 1663/NQ-UBTVQH15
		"86": {124, 105, 19, 0}, // Vĩnh Long — NQ 1687/NQ-UBTVQH15
		"96": {64, 55, 9, 0},    // Cà Mau — NQ 1655/NQ-UBTVQH15
		"80": {96, 82, 14, 0},   // Tây Ninh — NQ 1682/NQ-UBTVQH15
	}
	// Tỉnh cũ (đến 30/06/2025) → {tỉnh kế thừa, số huyện, số xã/phường/thị trấn}.
	wantOldProvinces = map[string]struct {
		successor           string
		districts, communes int
	}{
		"89": {"91", 11, 155}, "91": {"91", 15, 143},
		"87": {"82", 12, 141}, "82": {"82", 11, 164},
		"86": {"86", 8, 102}, "83": {"86", 9, 148}, "84": {"86", 9, 104},
		"92": {"92", 9, 80}, "94": {"92", 11, 108}, "93": {"92", 8, 75},
		"96": {"96", 9, 100}, "95": {"96", 7, 64},
		"72": {"80", 9, 94}, "80": {"80", 15, 186},
	}
)

var (
	provinceCodeRe = regexp.MustCompile(`^[0-9]{2}$`)
	districtCodeRe = regexp.MustCompile(`^[0-9]{3}$`)
	communeCodeRe  = regexp.MustCompile(`^[0-9]{5}$`)
)

type seedIndex struct {
	units []Unit
	g     fakeGazetteer
}

func loadSeed(t *testing.T) seedIndex {
	t.Helper()
	_, units, err := ParseGazetteer(seeddata.AdminUnits)
	if err != nil {
		t.Fatalf("dữ liệu nhúng: %v", err)
	}
	return seedIndex{units: units, g: fakeGazetteer(units)}
}

func (s seedIndex) at(code string, d time.Time) *Unit {
	u, _ := s.g.at(code, d)
	return u
}

var (
	beforeReorg = ReorgDate.AddDate(0, 0, -1)
	afterReorg  = Day(2026, 1, 1)
)

func communeKind(name string) string {
	for _, k := range []string{"Xã", "Phường", "Đặc khu", "Thị trấn"} {
		if strings.HasPrefix(name, k+" ") {
			return k
		}
	}
	return ""
}

// Số đơn vị từng tỉnh, trước và sau 01/07/2025, khớp tài liệu nguồn.
func TestSeedCountsMatchDoc(t *testing.T) {
	s := loadSeed(t)
	newCounts := map[string][4]int{}
	oldDistricts, oldCommunes := map[string]int{}, map[string]int{}
	for _, u := range s.units {
		switch {
		case u.Level == port.LevelCommune && u.ValidAt(afterReorg):
			c := newCounts[u.ParentCode]
			c[0]++
			switch communeKind(u.Name) {
			case "Xã":
				c[1]++
			case "Phường":
				c[2]++
			case "Đặc khu":
				c[3]++
			default:
				t.Errorf("%s %q: loại hình lạ sau sắp xếp", u.Code, u.Name)
			}
			newCounts[u.ParentCode] = c
		case u.Level == port.LevelDistrict:
			oldDistricts[u.ParentCode]++
		case u.Level == port.LevelCommune:
			d := s.at(u.ParentCode, beforeReorg)
			if d == nil || d.Level != port.LevelDistrict {
				t.Fatalf("%s: huyện cha %s không có hiệu lực ngày 30/06/2025", u.Code, u.ParentCode)
			}
			if communeKind(u.Name) == "" || communeKind(u.Name) == "Đặc khu" {
				t.Errorf("%s %q: loại hình lạ trước sắp xếp", u.Code, u.Name)
			}
			oldCommunes[d.ParentCode]++
		}
	}
	for p, want := range wantNewProvinces {
		if newCounts[p] != want {
			t.Errorf("tỉnh mới %s: %v, muốn %v", p, newCounts[p], want)
		}
	}
	if len(newCounts) != len(wantNewProvinces) {
		t.Errorf("xã hiện hành thuộc tỉnh ngoài danh sách: %v", newCounts)
	}
	for p, want := range wantOldProvinces {
		if oldDistricts[p] != want.districts || oldCommunes[p] != want.communes {
			t.Errorf("tỉnh cũ %s: %d huyện, %d xã; muốn %d, %d", p, oldDistricts[p], oldCommunes[p], want.districts, want.communes)
		}
	}
}

// Mã theo đúng khuôn TCTK: tỉnh 2 chữ số, huyện 3, xã 5; mọi đơn vị cũ đóng đúng
// ngày 01/07/2025, mọi đơn vị mới mở từ ngày đó.
func TestSeedCodesAndValidity(t *testing.T) {
	s := loadSeed(t)
	for _, u := range s.units {
		re := map[port.Level]*regexp.Regexp{port.LevelProvince: provinceCodeRe, port.LevelDistrict: districtCodeRe, port.LevelCommune: communeCodeRe}[u.Level]
		if !re.MatchString(u.Code) {
			t.Errorf("%s (%s): sai khuôn mã TCTK", u.Code, u.Level)
		}
		switch {
		case u.ValidTo == nil:
			if !u.ValidFrom.Equal(ReorgDate) {
				t.Errorf("%s: đơn vị hiện hành phải có hiệu lực từ 01/07/2025", u.Code)
			}
		case !u.ValidTo.Equal(ReorgDate):
			t.Errorf("%s: đơn vị cũ phải đóng đúng 01/07/2025", u.Code)
		}
	}
}

// Mọi đơn vị cũ đã đóng có ≥ 1 kế thừa; kế thừa tồn tại, là đơn vị hiện hành cùng cấp
// (xã → xã, huyện → xã, tỉnh → tỉnh) và nằm trong tỉnh kế thừa của tỉnh cũ.
func TestSeedSuccessorsExistAndAreCurrent(t *testing.T) {
	s := loadSeed(t)
	predecessors := map[string]int{}
	for _, u := range s.units {
		if u.ValidTo == nil {
			continue
		}
		if len(u.Successors) == 0 {
			t.Errorf("%s %q: đã đóng mà không có kế thừa", u.Code, u.Name)
			continue
		}
		oldProv := u.Code
		if u.Level != port.LevelProvince {
			oldProv = u.ParentCode
			if u.Level == port.LevelCommune {
				oldProv = s.at(u.ParentCode, beforeReorg).ParentCode
			}
		}
		wantProv := wantOldProvinces[oldProv].successor
		for _, sc := range u.Successors {
			n := s.at(sc, *u.ValidTo)
			if n == nil || n.ValidTo != nil {
				t.Errorf("%s → %s: kế thừa không tồn tại hoặc không còn hiệu lực", u.Code, sc)
				continue
			}
			wantLevel := port.LevelCommune
			if u.Level == port.LevelProvince {
				wantLevel = port.LevelProvince
			}
			if n.Level != wantLevel {
				t.Errorf("%s → %s: kế thừa sai cấp %s", u.Code, sc, n.Level)
			}
			got := n.Code
			if n.Level == port.LevelCommune {
				got = n.ParentCode
			}
			if got != wantProv {
				t.Errorf("%s (tỉnh cũ %s) → %s thuộc tỉnh %s, muốn %s", u.Code, oldProv, sc, got, wantProv)
			}
			if u.Level == port.LevelCommune {
				predecessors[sc]++
			}
		}
		// ResolveCurrent trên toàn bộ seed trả đúng tập kế thừa (đã sắp xếp). Mã được
		// đơn vị mới dùng lại (vd 31135) thì tra theo mã trả chính đơn vị hiện hành;
		// tập kế thừa đầy đủ của đơn vị cũ đọc ở phiên bản cũ (successor_codes).
		got, err := ResolveCurrent(u.Code, afterReorg, s.g.at, s.g.latest)
		want := slices.Sorted(slices.Values(u.Successors))
		if s.at(u.Code, afterReorg) != nil {
			want = []string{u.Code}
		}
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("Current(%s) = %v, %v; muốn %v", u.Code, got, err, want)
		}
	}
	// Chiều ngược: mọi xã mới đều có ≥ 1 xã cũ tiền thân.
	for _, u := range s.units {
		if u.Level == port.LevelCommune && u.ValidTo == nil && predecessors[u.Code] == 0 {
			t.Errorf("%s %q: xã mới không có tiền thân", u.Code, u.Name)
		}
	}
}

// Ví dụ đối chiếu văn bản: NQ 1668/NQ-UBTVQH15 khoản 1 Điều 1 — phường Ninh Kiều
// hình thành từ phường Tân An, phường Thới Bình, phường Xuân Khánh (quận Ninh Kiều).
func TestSeedSampleMatchesResolution(t *testing.T) {
	s := loadSeed(t)
	nk := s.at("31135", afterReorg)
	if nk == nil || nk.Name != "Phường Ninh Kiều" || nk.ParentCode != "92" {
		t.Fatalf("31135: %+v", nk)
	}
	var preds []string
	for _, u := range s.units {
		if u.Level == port.LevelCommune && u.ValidTo != nil && slices.Contains(u.Successors, "31135") {
			if u.ParentCode != "916" {
				t.Errorf("%s %q: không thuộc quận Ninh Kiều (916)", u.Code, u.Name)
			}
			preds = append(preds, u.Name)
		}
	}
	slices.Sort(preds)
	if want := []string{"Phường Thới Bình", "Phường Tân An", "Phường Xuân Khánh"}; !slices.Equal(preds, want) {
		t.Fatalf("tiền thân Phường Ninh Kiều: %v, muốn %v", preds, want)
	}
}
