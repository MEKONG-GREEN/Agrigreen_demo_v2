package main

// Đổi một dòng của tệp nhân sự cũ thành yêu cầu POST /v1/hrm/staff (hồ sơ + quá trình ban đầu + thẻ TK) kèm danh sách
// lỗi/cảnh báo theo cột. Kiểm phía công cụ (danh mục, đơn vị, bảng hệ số, trùng trong tệp) để chạy thử báo đủ lỗi mà
// không ghi; lần ghi thật vẫn do lớp app của pf/hrm kiểm lại toàn bộ.

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

// Mức của một dòng báo cáo.
const (
	LevelError = "Lỗi"
	LevelWarn  = "Cảnh báo"
)

// Trạng thái dòng.
const (
	StatusValid   = "Hợp lệ"    // chạy thử: sẽ tạo khi ghi
	StatusError   = "Lỗi"       // không ghi
	StatusCreated = "Đã tạo"    // ghi: hồ sơ mới
	StatusExists  = "Đã có"     // ghi: hồ sơ đã có (lần chạy trước hoặc nhập tay) — không ghi lại
	StatusSkipped = "Chưa ghi"  // ghi bị hủy trước dòng này
	staffXa       = "can_bo_xa" // loại nhân sự cán bộ khuyến nông cấp xã
	unitCodeRE    = `^[a-z0-9][a-z0-9_.-]{0,39}$`
	communeRE     = `^[0-9]{5}$`
	gradeParenRE  = `\(([^()]+)\)\s*$`
)

var (
	reUnitCode   = regexp.MustCompile(unitCodeRE)
	reCommune    = regexp.MustCompile(communeRE)
	reGradeParen = regexp.MustCompile(gradeParenRE)
	reEmail      = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

// Issue là một lỗi/cảnh báo của dòng.
type Issue struct {
	Level   string
	Field   string // khóa trường chuẩn ("" = cả dòng)
	Message string
}

// EventReq là một sự kiện quá trình của yêu cầu tạo hồ sơ.
type EventReq struct {
	Kind      string            `json:"kind"`
	ValidFrom string            `json:"valid_from"`
	Payload   map[string]string `json:"payload"`
}

// StaffRequest là thân POST /v1/hrm/staff.
type StaffRequest struct {
	Type      string            `json:"type"`
	Profile   map[string]string `json:"profile"`
	Sensitive map[string]string `json:"sensitive,omitempty"`
	Events    []EventReq        `json:"events"`
}

// Row là kết quả xử lý một dòng.
type Row struct {
	Line      int
	SourceKey string
	Status    string
	Code      string // mã hồ sơ đã tạo / đã có
	Issues    []Issue
	Req       *StaffRequest
}

func (r *Row) add(level, field, format string, a ...any) {
	r.Issues = append(r.Issues, Issue{Level: level, Field: field, Message: fmt.Sprintf(format, a...)})
}

func (r *Row) hasError() bool {
	return slices.ContainsFunc(r.Issues, func(i Issue) bool { return i.Level == LevelError })
}

// converter giữ ngữ cảnh đổi dòng.
type converter struct {
	m    *Mapping
	refs *Refs
	col  map[string]int
	asOf string
	// trùng trong tệp
	phones, emails, people map[string]int
}

// Prepare đọc bảng: ánh xạ cột, kiểm cột bắt buộc, đổi từng dòng. Lỗi trả về là lỗi của cả tệp (thiếu cột).
func Prepare(t *tabular.Table, m *Mapping, refs *Refs, asOf string) ([]*Row, map[string]int, error) {
	col := t.Columns(m.aliases)
	var missing []string
	for _, k := range []string{"full_name", "phone"} {
		if _, ok := col[k]; !ok {
			missing = append(missing, k)
		}
	}
	_, b := col["birth_date"]
	_, bm := col["birth_date_male"]
	_, bf := col["birth_date_female"]
	if !b && !bm && !bf {
		missing = append(missing, "birth_date")
	}
	if _, g := col["gender"]; !g && !bm && !bf {
		missing = append(missing, "gender")
	}
	if len(missing) > 0 {
		var names []string
		for _, k := range missing {
			f, _ := fieldByKey(k)
			names = append(names, fmt.Sprintf("%s (%s)", f.Header, k))
		}
		return nil, col, fmt.Errorf("tệp thiếu cột bắt buộc: %s — sửa tiêu đề hoặc bổ sung columns trong tệp ánh xạ", strings.Join(names, ", "))
	}
	if asOf == "" {
		asOf = m.Defaults.AsOf
	}
	if asOf != "" {
		d, _, ok := normDate(asOf)
		if !ok || len(d) != 10 {
			return nil, col, fmt.Errorf("ngày chốt %q phải là YYYY-MM-DD", asOf)
		}
		asOf = d
	}
	c := &converter{m: m, refs: refs, col: col, asOf: asOf, phones: map[string]int{}, emails: map[string]int{}, people: map[string]int{}}
	out := make([]*Row, 0, len(t.Rows))
	for _, tr := range t.Rows {
		out = append(out, c.convert(tr))
	}
	return out, col, nil
}

// cell trả ô của trường k (đã cắt khoảng trắng).
func (c *converter) cell(tr tabular.Row, k string) string { return strings.TrimSpace(tr.Get(c.col, k)) }

// show trả giá trị để in vào báo cáo: trường dữ liệu cá nhân không in giá trị.
func show(field, v string) string {
	if f, ok := fieldByKey(field); ok && f.PII {
		return ""
	}
	return fmt.Sprintf(" %q", v)
}

// date đọc ô ngày của trường k; trả "" khi trống hoặc lỗi (đã ghi lỗi).
func (c *converter) date(r *Row, tr tabular.Row, k string) string {
	v := c.cell(tr, k)
	if v == "" {
		return ""
	}
	d, serial, ok := normDate(v)
	if !ok {
		r.add(LevelError, k, "Ngày%s không đọc được — dùng dd/mm/yyyy, mm/yyyy hoặc yyyy", show(k, v))
		return ""
	}
	if serial {
		r.add(LevelWarn, k, "Ô ngày lưu dạng số của Excel — đã đổi thành ngày%s, cần đối chiếu", show(k, d))
	}
	return d
}

// catalog tra mã danh mục kind cho ô của trường field: bảng đổi của tệp ánh xạ, rồi mã/tên danh mục (ngạch: thêm dạng
// "Tên (mã)"). ok = false khi đã ghi lỗi.
func (c *converter) catalog(r *Row, field, kind, raw string) (catalogItem, bool) {
	items := c.refs.Catalogs[kind]
	find := func(pred func(catalogItem) bool) (catalogItem, bool) {
		var hit *catalogItem
		for i := range items {
			if pred(items[i]) {
				if items[i].Active {
					return items[i], true
				}
				if hit == nil {
					hit = &items[i]
				}
			}
		}
		if hit != nil {
			return *hit, true
		}
		return catalogItem{}, false
	}
	var it catalogItem
	var ok bool
	if code, mapped := lookupValue(c.m, field, raw); mapped {
		it, ok = find(func(x catalogItem) bool { return x.Code == code })
	} else {
		k := fold(raw)
		it, ok = find(func(x catalogItem) bool { return fold(x.Code) == k || fold(x.Name) == k })
		if !ok && kind == "grade" {
			if m := reGradeParen.FindStringSubmatch(raw); m != nil {
				code := strings.Join(strings.Fields(m[1]), "")
				it, ok = find(func(x catalogItem) bool { return strings.EqualFold(x.Code, code) })
			}
		}
	}
	switch {
	case !ok:
		r.add(LevelError, field, "Giá trị%s không có trong danh mục %s — sửa ô hoặc khai báo values.%s trong tệp ánh xạ", show(field, raw), kind, field)
		return catalogItem{}, false
	case !it.Active:
		r.add(LevelError, field, "Mục danh mục %s %q đã ngừng sử dụng", kind, it.Code)
		return catalogItem{}, false
	}
	return it, true
}

// unit tra mã đơn vị nội bộ cho ô của trường field (unit | mgmt_unit).
func (c *converter) unit(r *Row, field, raw string) (string, bool) {
	code, mapped := lookupValue(c.m, field, raw)
	if !mapped && field != "unit" {
		code, mapped = lookupValue(c.m, "unit", raw)
	}
	strip := func(s string) string {
		s = fold(s)
		for _, p := range []string{"phong", "tram", "trai", "bophan"} {
			if strings.HasPrefix(s, p) && len(s) > len(p) {
				return s[len(p):]
			}
		}
		return s
	}
	if len(c.refs.Units) == 0 {
		if !mapped {
			code = strings.ToLower(strings.TrimSpace(raw))
		}
		if !reUnitCode.MatchString(code) {
			r.add(LevelError, field, "Máy chủ chưa nối danh mục đơn vị — khai báo values.%s cho%s trong tệp ánh xạ", field, show(field, raw))
			return "", false
		}
		return code, true
	}
	for _, u := range c.refs.Units {
		if (mapped && u.Code == code) || (!mapped && (fold(u.Code) == fold(raw) || fold(u.Name) == fold(raw) || strip(u.Name) == strip(raw))) {
			if !u.Active {
				r.add(LevelError, field, "Đơn vị %q đã ngừng", u.Code)
				return "", false
			}
			return u.Code, true
		}
	}
	r.add(LevelError, field, "Đơn vị%s không có trong danh mục đơn vị nội bộ — sửa ô hoặc khai báo values.%s", show(field, raw), field)
	return "", false
}

// convert đổi một dòng.
func (c *converter) convert(tr tabular.Row) *Row {
	r := &Row{Line: tr.Line, SourceKey: c.cell(tr, "source_key")}
	req := &StaffRequest{Profile: map[string]string{}}

	// Loại nhân sự (HS-15).
	typ := c.cell(tr, "type")
	if typ == "" {
		typ = c.m.Defaults.Type
	}
	if typ == "" {
		r.add(LevelError, "type", "Thiếu loại nhân sự (hoặc đặt defaults.type trong tệp ánh xạ)")
	} else if v, ok := lookupValue(c.m, "type", typ); ok {
		req.Type = v
	} else {
		r.add(LevelError, "type", "Loại nhân sự %q — cần Viên chức, Hợp đồng lao động hoặc Cán bộ khuyến nông cấp xã", typ)
	}

	// Họ tên (HS-02, BR-05).
	name, stripped := normName(c.cell(tr, "full_name"))
	switch n := utf8.RuneCountInString(name); {
	case n < 2 || n > 100:
		r.add(LevelError, "full_name", "Họ tên trống hoặc quá 100 ký tự")
	case stripped:
		r.add(LevelWarn, "full_name", "Đã bỏ ghi chú/số trong họ tên (BR-05) — kiểm lại họ tên")
	}
	req.Profile["full_name"] = name

	// Giới tính (HS-03) và ngày sinh (HS-04), kể cả dạng năm sinh ở cột Nam/Nữ (NS01, NS04).
	gender := ""
	if g := c.cell(tr, "gender"); g != "" {
		if v, ok := normGender(g); ok {
			gender = v
		} else {
			r.add(LevelError, "gender", "Giới tính phải là Nam hoặc Nữ")
		}
	}
	birthKey := "birth_date"
	birth := c.date(r, tr, "birth_date")
	bm, bf := c.cell(tr, "birth_date_male"), c.cell(tr, "birth_date_female")
	switch {
	case bm != "" && bf != "":
		r.add(LevelError, "birth_date_male", "Năm sinh ghi ở cả cột Nam và cột Nữ")
	case bm != "" || bf != "":
		g, k := "nam", "birth_date_male"
		if bf != "" {
			g, k = "nu", "birth_date_female"
		}
		if gender != "" && gender != g {
			r.add(LevelError, "gender", "Giới tính khác cột năm sinh Nam/Nữ")
		}
		gender, birthKey = g, k
		if birth == "" {
			birth = c.date(r, tr, k)
		}
	}
	if gender == "" && !slices.ContainsFunc(r.Issues, func(i Issue) bool { return i.Field == "gender" }) {
		r.add(LevelError, "gender", "Thiếu giới tính")
	}
	if birth == "" && !slices.ContainsFunc(r.Issues, func(i Issue) bool { return i.Field == birthKey }) {
		r.add(LevelError, birthKey, "Thiếu ngày sinh")
	}
	req.Profile["gender"], req.Profile["birth_date"] = gender, birth

	// SĐT (HS-12): bắt buộc, không trùng.
	phone, added, ok := normPhone(c.cell(tr, "phone"))
	switch {
	case c.cell(tr, "phone") == "":
		r.add(LevelError, "phone", "Thiếu số điện thoại (HS-12)")
	case !ok:
		r.add(LevelError, "phone", "Số điện thoại không hợp lệ — cần 10 số, bắt đầu bằng 0")
	default:
		if added {
			r.add(LevelWarn, "phone", "Đã thêm số 0 đầu (Excel bỏ khi lưu dạng số)")
		}
		if prev, dup := c.phones[phone]; dup {
			r.add(LevelError, "phone", "Trùng số điện thoại với dòng %d", prev)
		} else {
			c.phones[phone] = r.Line
		}
		req.Profile["phone"] = phone
	}
	if e := strings.ToLower(c.cell(tr, "email")); e != "" {
		if !reEmail.MatchString(e) || len(e) > 254 {
			r.add(LevelError, "email", "Email sai định dạng (HS-35)")
		} else if prev, dup := c.emails[e]; dup {
			r.add(LevelError, "email", "Trùng email với dòng %d", prev)
		} else {
			c.emails[e] = r.Line
			req.Profile["email"] = e
		}
	}
	if name != "" && birth != "" {
		k := fold(name) + "|" + birth
		if prev, dup := c.people[k]; dup {
			r.add(LevelWarn, "full_name", "Trùng họ tên và ngày sinh với dòng %d — kiểm tra có phải một người", prev)
		} else {
			c.people[k] = r.Line
		}
	}
	if a := c.cell(tr, "address"); a != "" {
		if utf8.RuneCountInString(a) > 500 {
			r.add(LevelError, "address", "Địa chỉ quá 500 ký tự")
		}
		req.Profile["address"] = a
	}
	if n := c.cell(tr, "note"); n != "" {
		if utf8.RuneCountInString(n) > 1000 {
			r.add(LevelError, "note", "Ghi chú quá 1.000 ký tự (HS-32)")
		}
		req.Profile["note"] = n
	}

	// Ngày hiệu lực của quá trình ban đầu.
	workStart := c.date(r, tr, "work_start")
	d0 := c.date(r, tr, "unit_since")
	if d0 == "" {
		d0 = workStart
	}
	if d0 == "" {
		d0 = c.asOf
	}
	if d0 == "" {
		r.add(LevelError, "unit_since", "Thiếu ngày hiệu lực: cần Ngày về đơn vị, Ngày vào làm hoặc ngày chốt (-as-of / defaults.as_of)")
	}
	ev := func(kind, from string, p map[string]string) {
		if from == "" {
			from = d0
		}
		req.Events = append(req.Events, EventReq{Kind: kind, ValidFrom: from, Payload: p})
	}

	// FN-08: vào làm, nghỉ việc.
	if workStart != "" {
		ev("work_start", workStart, map[string]string{})
	} else {
		r.add(LevelWarn, "work_start", "Thiếu ngày vào làm — hồ sơ ở trạng thái Chưa vào làm, không vào NS01")
	}
	if we := c.date(r, tr, "work_end"); we != "" {
		reason := c.cell(tr, "leave_reason")
		switch {
		case workStart == "":
			r.add(LevelError, "work_end", "Có ngày nghỉ việc nhưng thiếu ngày vào làm (FN-08)")
		case we < workStart:
			r.add(LevelError, "work_end", "Ngày nghỉ việc trước ngày vào làm (FN-08)")
		case reason == "":
			r.add(LevelError, "leave_reason", "Có ngày nghỉ việc nhưng thiếu lý do nghỉ việc")
		default:
			if it, ok := c.catalog(r, "leave_reason", "leave_reason", reason); ok {
				ev("work_end", we, map[string]string{"reason": it.Code})
			}
		}
	}

	// Đơn vị (HS-16; cán bộ xã: xã + đơn vị quản lý HS-37, BR-07).
	if req.Type == staffXa {
		p := map[string]string{}
		if cm := c.cell(tr, "commune"); !reCommune.MatchString(cm) {
			r.add(LevelError, "commune", "Cán bộ xã cần mã xã/phường (mã ĐVHC 5 số)")
		} else {
			p["commune"] = cm
		}
		if mu := c.cell(tr, "mgmt_unit"); mu == "" {
			r.add(LevelError, "mgmt_unit", "Cán bộ xã cần đơn vị quản lý (HS-37)")
		} else if u, ok := c.unit(r, "mgmt_unit", mu); ok {
			p["mgmt_unit"] = u
		}
		ev("unit", "", p)
	} else if req.Type != "" {
		if u := c.cell(tr, "unit"); u == "" {
			r.add(LevelError, "unit", "Thiếu đơn vị công tác (HS-16)")
		} else if code, ok := c.unit(r, "unit", u); ok {
			ev("unit", "", map[string]string{"unit": code})
		}
	}

	// Chức vụ (HS-17..20).
	pos := c.cell(tr, "position")
	if pos == "" {
		pos = c.m.Defaults.Position
	}
	appointed := c.date(r, tr, "appointed_on")
	if pos == "" {
		r.add(LevelError, "position", "Thiếu chức vụ (HS-17) — hoặc đặt defaults.position")
	} else if it, ok := c.catalog(r, "position", "position", pos); ok {
		p := map[string]string{"position": it.Code}
		if t := c.cell(tr, "position_title"); t != "" {
			p["title"] = t
		}
		if a := c.cell(tr, "allowance"); a != "" {
			if v, ok := normCoeff(a); ok {
				p["allowance"] = v
			} else {
				r.add(LevelError, "allowance", "Hệ số PCCV %q không đọc được", a)
			}
		}
		if appointed != "" {
			p["appointed_on"] = appointed
		} else if it.Attrs.Leader {
			r.add(LevelError, "appointed_on", "Chức vụ lãnh đạo %q phải có ngày bổ nhiệm (HS-20)", it.Code)
		}
		ev("position", appointed, p)
	}

	c.grade(r, tr, ev)
	c.degree(r, tr, ev)
	for _, k := range []string{"political_theory", "state_mgmt", "it_level"} {
		if v := c.cell(tr, k); v != "" {
			if it, ok := c.catalog(r, k, k, v); ok {
				ev("training", "", map[string]string{"type": k, "code": it.Code})
			}
		}
	}
	c.sensitive(r, tr, req)

	if r.hasError() {
		r.Status = StatusError
	} else {
		r.Status, r.Req = StatusValid, req
	}
	return r
}

// grade dựng sự kiện ngạch – bậc (HS-23, NB-04, Q-069): bậc đọc được cả khi Excel đổi thành ngày; thiếu bậc thì suy từ
// hệ số; hệ số phải khớp bảng hệ số của ngạch (BR-06: hệ số không nhập tay).
func (c *converter) grade(r *Row, tr tabular.Row, ev func(string, string, map[string]string)) {
	g, st, co := c.cell(tr, "grade"), c.cell(tr, "step"), c.cell(tr, "coefficient")
	since := c.date(r, tr, "step_since")
	if g == "" {
		if st != "" || co != "" {
			r.add(LevelError, "grade", "Có bậc/hệ số nhưng thiếu ngạch")
		}
		return
	}
	it, ok := c.catalog(r, "grade", "grade", g)
	if !ok {
		return
	}
	coefs := c.refs.Scales[it.Code]
	if len(coefs) == 0 {
		r.add(LevelError, "grade", "Ngạch %s chưa có bảng hệ số — Admin khai báo bảng hệ số trước khi nạp", it.Code)
		return
	}
	step := 0
	if st != "" {
		s, of, fromDate, ok := normStep(st)
		switch {
		case !ok:
			r.add(LevelError, "step", "Bậc %q không đọc được", st)
			return
		case fromDate:
			r.add(LevelWarn, "step", "Ô bậc bị Excel đổi thành ngày (%s) — đã đọc là bậc %d/%d, cần đối chiếu", st, s, of)
		}
		if of != 0 && of != len(coefs) {
			r.add(LevelWarn, "step", "Số bậc ghi trong tệp (%d) khác số bậc của ngạch %s (%d)", of, it.Code, len(coefs))
		}
		step = s
	}
	coef := ""
	if co != "" {
		v, ok := normCoeff(co)
		if !ok {
			r.add(LevelError, "coefficient", "Hệ số %q không đọc được", co)
			return
		}
		coef = v
	}
	switch {
	case step == 0 && coef == "":
		r.add(LevelError, "step", "Có ngạch nhưng thiếu bậc và hệ số")
		return
	case step == 0:
		i := slices.Index(coefs, coef)
		if i < 0 {
			r.add(LevelError, "coefficient", "Hệ số %s không có trong bảng hệ số ngạch %s", coef, it.Code)
			return
		}
		step = i + 1
		r.add(LevelWarn, "step", "Thiếu bậc — đã suy ra bậc %d từ hệ số %s", step, coef)
	case step > len(coefs):
		r.add(LevelError, "step", "Bậc %d vượt số bậc của ngạch %s (%d)", step, it.Code, len(coefs))
		return
	case coef != "" && coefs[step-1] != coef:
		r.add(LevelError, "coefficient", "Hệ số %s không khớp bậc %d của ngạch %s (bảng: %s)", coef, step, it.Code, coefs[step-1])
		return
	}
	p := map[string]string{"grade": it.Code, "step": fmt.Sprint(step)}
	from := since
	if since != "" {
		p["step_since"] = since
	} else {
		// Tệp ghi bậc HIỆN HƯỞNG: hiệu lực tại ngày chốt (bảng hệ số có ngày hiệu lực — ngày vào làm có thể trước bảng).
		from = c.asOf
		r.add(LevelWarn, "step_since", "Thiếu ngày xếp bậc — tạm dùng ngày chốt; cần bổ sung để tính mốc nâng bậc (Q-069)")
	}
	if rc := c.cell(tr, "reserved_coeff"); rc != "" {
		if v, ok := normCoeff(rc); ok {
			p["reserved_coeff"] = v
		} else {
			r.add(LevelError, "reserved_coeff", "Hệ số bảo lưu %q không đọc được", rc)
		}
	}
	ev("grade", from, p)
}

// degree dựng văn bằng (HS-24).
func (c *converter) degree(r *Row, tr tabular.Row, ev func(string, string, map[string]string)) {
	lv := c.cell(tr, "education_level")
	extra := c.cell(tr, "major") + c.cell(tr, "degree_form") + c.cell(tr, "institution")
	if lv == "" {
		if extra != "" {
			r.add(LevelWarn, "education_level", "Có chuyên ngành/hình thức/cơ sở đào tạo nhưng thiếu trình độ — bỏ qua văn bằng")
		}
		return
	}
	it, ok := c.catalog(r, "education_level", "education_level", lv)
	if !ok {
		return
	}
	p := map[string]string{"level": it.Code}
	if m := c.cell(tr, "major"); m != "" {
		if mi, ok := c.catalog(r, "major", "major", m); ok {
			p["major"] = mi.Code
		}
	}
	if f := c.cell(tr, "degree_form"); f != "" {
		if v, ok := lookupValue(c.m, "degree_form", f); ok {
			p["form"] = v
		} else {
			r.add(LevelError, "degree_form", "Hình thức đào tạo %q — cần Chính quy hoặc Tại chức", f)
		}
	}
	if s := c.cell(tr, "institution"); s != "" {
		if utf8.RuneCountInString(s) > 200 {
			r.add(LevelError, "institution", "Cơ sở đào tạo quá 200 ký tự")
		}
		p["institution"] = s
	}
	ev("degree", c.date(r, tr, "grad_year"), p)
}

// sensitive dựng thẻ Thống kê BC Sở (TK-01..06) — chỉ viên chức, hợp đồng lao động.
func (c *converter) sensitive(r *Row, tr tabular.Row, req *StaffRequest) {
	keys := []string{"ethnicity", "religion", "party_date", "hometown", "birthplace"}
	has := slices.ContainsFunc(keys, func(k string) bool { return c.cell(tr, k) != "" })
	if !has {
		return
	}
	if req.Type == staffXa {
		r.add(LevelWarn, "ethnicity", "Cán bộ khuyến nông cấp xã không có thẻ Thống kê BC Sở — bỏ qua các cột TK")
		return
	}
	s := map[string]string{}
	for _, k := range []string{"ethnicity", "religion"} {
		if v := c.cell(tr, k); v != "" {
			if it, ok := c.catalog(r, k, k, v); ok {
				s[k] = it.Code
			}
		}
	}
	if d := c.date(r, tr, "party_date"); d != "" {
		s["party_date"] = d
	}
	for _, k := range []string{"hometown", "birthplace"} {
		if v := c.cell(tr, k); v != "" {
			if utf8.RuneCountInString(v) > 300 {
				f, _ := fieldByKey(k)
				r.add(LevelError, k, "%s quá 300 ký tự", f.Header)
			}
			s[k] = v
		}
	}
	if len(s) > 0 {
		req.Sensitive = s
	}
}
