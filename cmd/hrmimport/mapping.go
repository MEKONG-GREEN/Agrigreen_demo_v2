package main

// Tệp ánh xạ cột (YAML hoặc JSON): trường chuẩn → các tiêu đề cột nguồn có thể gặp; bảng đổi chữ → mã cho từng trường;
// giá trị mặc định. Khách chưa gửi tệp gốc (Q-069) nên ánh xạ mặc định (mapping.default.yaml) khớp tệp mẫu do công cụ
// sinh ra (-template) và bí danh cột thường gặp; tệp thật của HCTH chỉ cần sửa/bổ sung tệp ánh xạ, không sửa mã.

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

//go:embed mapping.default.yaml
var defaultMappingYAML []byte

// Field là một trường chuẩn của dòng nhập.
type Field struct {
	Key      string // khóa trong tệp ánh xạ
	Header   string // tiêu đề cột của tệp mẫu
	Required string // "x" bắt buộc; mô tả điều kiện nếu bắt buộc có điều kiện
	PII      bool   // dữ liệu cá nhân: báo cáo không in giá trị
	Help     string // định dạng / ghi chú cho sheet Hướng dẫn
	Example  string // ví dụ (dữ liệu tổng hợp)
}

// Fields theo thứ tự cột của tệp mẫu.
var Fields = []Field{
	{"source_key", "Mã cũ", "", false, "STT hoặc mã của tệp cũ — chỉ để đối chiếu trong báo cáo, không lưu", "HC-01"},
	{"type", "Loại nhân sự", "x", false, "Viên chức | Hợp đồng lao động | Cán bộ khuyến nông cấp xã (HS-15)", "Viên chức"},
	{"full_name", "Họ và tên", "x", true, "2–100 ký tự, không ghi chú/số trong họ tên (BR-05)", "Nguyễn Văn Mẫu"},
	{"gender", "Giới tính", "x", true, "Nam | Nữ (HS-03)", "Nam"},
	{"birth_date", "Ngày sinh", "x", true, "dd/mm/yyyy, mm/yyyy hoặc yyyy (HS-04)", "15/03/1985"},
	{"phone", "Số điện thoại", "x", true, "10 số, giữ số 0 đầu; không trùng (HS-12)", "0900000001"},
	{"email", "Email", "", true, "Định dạng email; không trùng (HS-35)", "mau01@example.invalid"},
	{"address", "Nơi ở hiện nay", "", true, "Tối đa 500 ký tự (HS-07)", "Phường Mẫu"},
	{"unit", "Đơn vị công tác", "Viên chức, HĐLĐ", false, "Tên hoặc mã đơn vị nội bộ (HS-16)", "Phòng Hành chính - Tổng hợp"},
	{"commune", "Mã xã/phường", "Cán bộ xã", false, "Mã ĐVHC 5 số của xã/phường (BR-07)", "30001"},
	{"mgmt_unit", "Đơn vị quản lý", "Cán bộ xã", false, "Tên hoặc mã đơn vị quản lý thuộc Cấp Tỉnh (HS-37)", "Phòng Trồng trọt"},
	{"unit_since", "Ngày về đơn vị", "", false, "Ngày hiệu lực của đơn vị, chức vụ; trống = ngày vào làm hoặc ngày chốt", "01/01/2020"},
	{"position", "Chức vụ", "x", false, "Tên hoặc mã trong danh mục chức vụ (HS-17)", "Viên chức"},
	{"position_title", "Tên chức vụ ghi rõ", "", false, "HS-18", ""},
	{"allowance", "Hệ số PCCV", "", false, "Số, 2 chữ số thập phân (HS-19)", ""},
	{"appointed_on", "Ngày bổ nhiệm", "Chức vụ lãnh đạo", false, "Ngày quyết định bổ nhiệm (HS-20)", ""},
	{"work_start", "Ngày vào làm", "", false, "FN-08; trống → hồ sơ ở trạng thái Chưa vào làm (cảnh báo)", "01/06/2010"},
	{"work_end", "Ngày nghỉ việc", "", false, "FN-08 (sheet NGHỈ)", ""},
	{"leave_reason", "Lý do nghỉ việc", "Khi có ngày nghỉ việc", false, "Tên hoặc mã trong danh mục lý do nghỉ việc", ""},
	{"grade", "Ngạch/CDNN", "", false, "Tên, mã, hoặc \"Tên (mã)\" trong danh mục ngạch (HS-23)", "Chuyên viên (01.003)"},
	{"step", "Bậc", "Khi có ngạch (hoặc Hệ số bậc)", false, "Số bậc, hoặc \"bậc/số bậc\" (3/9) (NB-04)", "3"},
	{"coefficient", "Hệ số bậc", "", false, "Đối chiếu với bảng hệ số; dùng tìm bậc khi trống bậc (NB-05)", "3,00"},
	{"step_since", "Ngày xếp bậc", "", false, "Thời điểm được xếp bậc hiện hưởng (Q-069)", "01/01/2023"},
	{"reserved_coeff", "Hệ số bảo lưu", "", false, "Số, 2 chữ số thập phân (Q-069)", ""},
	{"education_level", "Trình độ chuyên môn", "", false, "Tên hoặc mã trong danh mục trình độ (HS-24)", "Đại học"},
	{"major", "Chuyên ngành", "", false, "Tên hoặc mã trong danh mục chuyên ngành", ""},
	{"degree_form", "Hình thức đào tạo", "", false, "Chính quy | Tại chức", "Chính quy"},
	{"institution", "Cơ sở đào tạo", "", false, "Tối đa 200 ký tự", ""},
	{"grad_year", "Năm tốt nghiệp", "", false, "yyyy", "2008"},
	{"political_theory", "Lý luận chính trị", "", false, "Danh mục LLCT (HS-26)", ""},
	{"state_mgmt", "Quản lý nhà nước", "", false, "Danh mục QLNN (HS-27)", ""},
	{"it_level", "Tin học", "", false, "Danh mục tin học (HS-29)", ""},
	{"ethnicity", "Dân tộc", "", true, "Thẻ Thống kê BC Sở (TK) — chỉ Viên chức, HĐLĐ", "Kinh"},
	{"religion", "Tôn giáo", "", true, "Thẻ TK — chỉ Viên chức, HĐLĐ", "Không"},
	{"party_date", "Ngày vào Đảng", "", true, "Thẻ TK", ""},
	{"hometown", "Quê quán", "", true, "Thẻ TK", ""},
	{"birthplace", "Nơi sinh", "", true, "Thẻ TK", ""},
	{"note", "Ghi chú", "", true, "Tối đa 1.000 ký tự (HS-32)", ""},
}

// extraFields là trường chuẩn không có cột riêng trong tệp mẫu, gặp ở tệp cũ: biểu NS01/NS04 ghi năm sinh vào cột Nam
// hoặc cột Nữ — cột chứa giá trị cho biết giới tính.
var extraFields = []Field{
	{"birth_date_male", "Năm sinh Nam", "", true, "Ngày/năm sinh ghi ở cột Nam (giới tính = Nam)", ""},
	{"birth_date_female", "Năm sinh Nữ", "", true, "Ngày/năm sinh ghi ở cột Nữ (giới tính = Nữ)", ""},
}

// fieldByKey tra trường chuẩn.
func fieldByKey(k string) (Field, bool) {
	for _, f := range append(slices.Clone(Fields), extraFields...) {
		if f.Key == k {
			return f, true
		}
	}
	return Field{}, false
}

// Mapping là tệp ánh xạ đã nạp.
type Mapping struct {
	Version  int                          `yaml:"version"`
	Defaults Defaults                     `yaml:"defaults"`
	Columns  map[string][]string          `yaml:"columns"`
	Values   map[string]map[string]string `yaml:"values"`

	aliases map[string]string            // tiêu đề đã chuẩn hóa (tabular.NormalizeHeader) → trường chuẩn
	values  map[string]map[string]string // trường → fold(chữ nguồn) → mã
}

// Defaults là giá trị dùng khi ô trống.
type Defaults struct {
	Type     string `yaml:"type"`     // loại nhân sự khi không có cột/ô Loại nhân sự (vd tệp HĐLĐ-ĐVSN: hop_dong)
	Position string `yaml:"position"` // mã chức vụ khi ô trống (vd vien_chuc)
	AsOf     string `yaml:"as_of"`    // ngày chốt dữ liệu: ngày hiệu lực khi không có ngày nào khác (YYYY-MM-DD)
}

var reMapCode = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$`)

// LoadMapping nạp tệp ánh xạ (rỗng = ánh xạ mặc định nhúng sẵn).
func LoadMapping(path string) (*Mapping, error) {
	raw := defaultMappingYAML
	if path != "" {
		b, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return nil, err
		}
		raw = b
	}
	return parseMapping(raw)
}

func parseMapping(raw []byte) (*Mapping, error) {
	m := &Mapping{}
	if err := yaml.Unmarshal(raw, m); err != nil {
		return nil, fmt.Errorf("tệp ánh xạ: %w", err)
	}
	if m.Version != 1 {
		return nil, fmt.Errorf("tệp ánh xạ: version phải là 1")
	}
	if m.Defaults.AsOf != "" {
		if d, _, ok := normDate(m.Defaults.AsOf); !ok || len(d) != 10 {
			return nil, fmt.Errorf("tệp ánh xạ: defaults.as_of phải là ngày YYYY-MM-DD")
		}
	}
	m.aliases = map[string]string{}
	for key, hs := range m.Columns {
		if _, ok := fieldByKey(key); !ok {
			return nil, fmt.Errorf("tệp ánh xạ: trường %q không có (xem -template, sheet Hướng dẫn)", key)
		}
		for _, h := range append([]string{key}, hs...) {
			n := tabular.NormalizeHeader(h)
			if prev, dup := m.aliases[n]; dup && prev != key {
				return nil, fmt.Errorf("tệp ánh xạ: tiêu đề %q dùng cho cả %s và %s", h, prev, key)
			}
			m.aliases[n] = key
		}
	}
	for _, f := range Fields { // tiêu đề của tệp mẫu luôn nhận
		if n := tabular.NormalizeHeader(f.Header); m.aliases[n] == "" {
			m.aliases[n] = f.Key
		}
	}
	m.values = map[string]map[string]string{}
	for key, vs := range m.Values {
		if _, ok := fieldByKey(key); !ok {
			return nil, fmt.Errorf("tệp ánh xạ: values.%s không phải trường chuẩn", key)
		}
		m.values[key] = map[string]string{}
		for from, to := range vs {
			to = strings.TrimSpace(to)
			if !reMapCode.MatchString(to) {
				return nil, fmt.Errorf("tệp ánh xạ: values.%s[%q] = %q không phải mã", key, from, to)
			}
			m.values[key][fold(from)] = to
		}
	}
	return m, nil
}
