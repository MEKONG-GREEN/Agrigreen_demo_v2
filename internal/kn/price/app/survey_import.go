package app

// Nhập phiếu điều tra giá thành từ tệp — HAI BƯỚC (W11.78; BRD Báo giá v1.3 FN-TI-02.11):
//   - Xem trước (PreviewImport): đọc CSV / Excel (gồm tệp câu trả lời Google Form — shared/tabular, không tính công
//     thức), gợi ý ánh xạ cột theo tên cột, chuẩn hóa tên mặt hàng / giống (bỏ dấu, không phân biệt hoa thường, bảng từ
//     đồng nghĩa — AC-01), tên xã → mã ĐVHC, cán bộ điều tra → cán bộ đã khai báo của đợt (AC-01 FN-TI-02.10); kiểm hợp lệ
//     từng dòng (thiếu xã / nhóm cây / mặt hàng / nông hộ, diện tích / sản lượng 0 — AC-02), trùng trong tệp (SĐT + mặt
//     hàng — AC-03), cột công thức của tệp khác giá trị tự tính, cờ CT-06. KHÔNG ghi gì.
//   - Ghi nhận (CommitImport): cùng tệp + ánh xạ, kèm mã băm SHA-256 của lần xem trước (khác → 409 — người dùng ghi
//     đúng cái đã xem); nông hộ ghi vào kho định danh (chủ thể nông hộ, SĐT là chỉ mục mù — kho tự chống trùng), phiếu
//     trùng (cùng đợt, SĐT, mặt hàng) với phiếu đã có → bỏ qua, có cảnh báo; nhập lại cùng tệp không tạo phiếu trùng. Chỉ
//     dòng hợp lệ được ghi (BR-04); lần nhập + dòng lỗi / cảnh báo được lưu; ánh xạ tên thủ công lưu vào bảng từ đồng
//     nghĩa khi người dùng chọn.
// Mọi giá trị tính toán do hệ thống tính lại (CT-05, BR-01). Thông điệp lỗi không chứa giá trị của tệp (R-14).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// Giới hạn tệp nhập (vụ Hè Thu 2026: 720 phiếu).
var surveyLimits = tabular.Limits{MaxBytes: 5 << 20, MaxRows: 3000, MaxCols: 80}

// SurveyField là một trường của biểu mẫu điều tra cần ánh xạ cột.
type SurveyField struct {
	Key      string
	Label    string
	Required bool
}

// SurveyFields là các trường của biểu mẫu (thứ tự hiển thị ở bước Ánh xạ).
var SurveyFields = []SurveyField{
	{"officer", "Tên cán bộ điều tra", false}, {"commune", "Xã", true}, {"crop", "Nhóm cây", true},
	{"item", "Mặt hàng", true}, {"variety", "Giống / phẩm cấp", false}, {"farmer", "Họ tên nông hộ", true},
	{"phone", "Số điện thoại nông hộ", false}, {"area", "Diện tích (ha)", true}, {"output", "Sản lượng (kg)", false},
	{"yield_ha", "Năng suất (kg/ha)", false}, {"price", "Giá bán (đ/kg)", true}, {"other_income", "Thu khác (đ)", false},
	{"seed", "Chi phí giống", false}, {"fertilizer", "Chi phí phân bón", false}, {"pesticide", "Chi phí thuốc BVTV", false},
	{"labor", "Chi phí nhân công", false}, {"machine", "Chi phí máy móc", false}, {"other", "Chi khác", false},
	{"total_cost", "Tổng chi phí (cột công thức của tệp — chỉ đối chiếu)", false},
	{"cost_per_kg", "Giá thành / kg (cột công thức của tệp — chỉ đối chiếu)", false},
}

func surveyFieldKnown(k string) bool {
	for _, f := range SurveyFields {
		if f.Key == k {
			return true
		}
	}
	return false
}

// suggestRules: gợi ý ánh xạ theo tên cột đã chuẩn hóa (tabular.NormalizeHeader) của mẫu Google Form hiện hành — mỗi cột
// nhận trường của luật ĐẦU TIÊN khớp, mỗi trường một cột.
var suggestRules = []struct {
	field string
	any   []string // khớp khi tên cột chứa một cụm (cụm là chuỗi token nối "_")
	all   []string // và chứa mọi cụm này
}{
	{"total_cost", []string{"tong_chi_phi", "total_cost"}, nil},
	{"cost_per_kg", []string{"gia_thanh", "cost_per_kg"}, nil},
	{"officer", []string{"can_bo", "officer"}, nil},
	{"phone", []string{"dien_thoai", "sdt", "phone"}, nil},
	{"farmer", []string{"nong_ho", "chu_ho", "ho_ten", "farmer"}, nil},
	{"seed", []string{"giong"}, []string{"chi"}},
	{"seed", []string{"seed"}, nil},
	{"fertilizer", []string{"phan_bon", "fertilizer"}, nil},
	{"pesticide", []string{"thuoc", "bvtv", "pesticide"}, nil},
	{"labor", []string{"nhan_cong", "lao_dong", "labor"}, nil},
	{"machine", []string{"may", "co_gioi", "machine"}, nil},
	{"other_income", []string{"thu_khac", "thu_nhap_khac", "other_income"}, nil},
	{"other", []string{"chi_khac", "chi_phi_khac", "other"}, nil},
	{"crop", []string{"nhom_cay", "crop"}, nil},
	{"variety", []string{"giong", "pham_cap", "variety"}, nil},
	{"item", []string{"mat_hang", "loai_cay", "cay_trong", "item"}, nil},
	{"commune", []string{"xa", "phuong", "commune", "dia_ban"}, nil},
	{"area", []string{"dien_tich", "area"}, nil},
	{"output", []string{"san_luong", "output"}, nil},
	{"yield_ha", []string{"nang_suat", "yield"}, nil},
	{"price", []string{"gia_ban", "price"}, nil},
}

func hasPhrase(key, p string) bool { return strings.Contains("_"+key+"_", "_"+p+"_") }

// SuggestMapping gợi ý ánh xạ trường → chỉ số cột theo tên cột đã chuẩn hóa.
func SuggestMapping(keys []string) map[string]int {
	out := map[string]int{}
	for i, k := range keys {
		for _, r := range suggestRules {
			if _, used := out[r.field]; used {
				continue
			}
			ok := false
			for _, p := range r.any {
				if hasPhrase(k, p) {
					ok = true
					break
				}
			}
			for _, p := range r.all {
				ok = ok && hasPhrase(k, p)
			}
			if ok {
				out[r.field] = i
				break
			}
		}
	}
	return out
}

// ImportInput là tệp + ánh xạ của một lần nhập.
type ImportInput struct {
	File     []byte
	FileName string
	// Mapping: trường → chỉ số cột (0 là cột đầu; -1 = không dùng). nil → gợi ý theo tên cột.
	Mapping map[string]int
	// Names: ánh xạ tên thủ công "item" / "commune" → giá trị trong tệp → mã mặt hàng / mã ĐVHC.
	Names map[string]map[string]string
	// SHA256: mã băm của tệp ở lần xem trước (bắt buộc khi ghi nhận).
	SHA256       string
	SaveSynonyms bool
}

// ImportLine là kết quả một dòng.
type ImportLine struct {
	Row         int
	Level       string // ok | warning | error
	Messages    []string
	Skip        bool // trùng phiếu → bỏ qua
	Commune     string
	CommuneName string
	Crop        string
	Item        string
	ItemName    string
	OfficerID   *uuid.UUID
	CostPerKg   *decimal.Decimal
	Flag        string // cờ CT-06 dự kiến
}

// ImportPreview là kết quả xem trước.
type ImportPreview struct {
	SHA256          string
	FileName        string
	Columns         []string
	Mapping         map[string]int
	Missing         []string            // trường bắt buộc chưa ánh xạ (không xem trước được dòng)
	Unknown         map[string][]string // "item" / "commune" → giá trị chưa khớp danh mục (để ánh xạ thủ công)
	Lines           []ImportLine
	OK, Warn, Err   int
	Skip            int
	AlreadyImported *time.Time // tệp cùng mã băm đã ghi nhận vào đợt
}

// ImportResult là kết quả ghi nhận.
type ImportResult struct {
	ImportID                         uuid.UUID
	Rows, Created, Skipped, Warnings int
	Errors                           int
	Lines                            []ImportLine
}

// importRow là một dòng đã kiểm (dữ liệu cá nhân chỉ trong bộ nhớ).
type importRow struct {
	line     *ImportLine
	farmer   string
	phone    string // 84xxxxxxxxx; "" = không có / không hợp lệ
	figures  domain.FormFigures
	commune  string
	crop     string
	item     string
	officer  *uuid.UUID
	writable bool
}

type importPrep struct {
	a       surveyAccess
	sn      *roundSnap
	preview ImportPreview
	rows    []*importRow
	manual  map[string]map[string]string // ánh xạ tên thủ công đã dùng: kind → khóa chuẩn hóa → mã
}

func fileHash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// itemIndex: khóa chuẩn hóa → mã mặt hàng đang hiệu lực (khóa trùng giữa hai mặt hàng → bỏ, phải ánh xạ thủ công).
func itemIndex(states map[string]domain.ItemState) map[string]string {
	idx, amb := map[string]string{}, map[string]bool{}
	put := func(k, code string) {
		if k == "" || amb[k] {
			return
		}
		if c, ok := idx[k]; ok && c != code {
			delete(idx, k)
			amb[k] = true
			return
		}
		idx[k] = code
	}
	for code, st := range states {
		if st.State != domain.ItemStateActive {
			continue
		}
		put(domain.Fold(code), code)
		put(domain.Fold(st.Name+" "+st.Variety), code)
	}
	for code, st := range states { // tên loại không kèm giống: chỉ khi không trùng mặt hàng khác
		if st.State == domain.ItemStateActive {
			put(domain.Fold(st.Name), code)
		}
	}
	return idx
}

func itemLabel(st domain.ItemState) string { return strings.TrimSpace(st.Name + " " + st.Variety) }

// prepare đọc tệp, kiểm từng dòng theo đợt (chỉ đọc).
func (s *SurveyService) prepare(ctx context.Context, profile string, round uuid.UUID, in ImportInput) (*importPrep, error) {
	a, err := s.access(ctx, profile)
	if err != nil {
		return nil, err
	}
	if !a.tinh && !a.c.manage {
		return nil, problem.Forbidden.WithDetail("chỉ Cấp Tỉnh, Admin nhập dữ liệu điều tra từ tệp")
	}
	if _, err := s.vault(); err != nil {
		return nil, err
	}
	tb, err := tabular.ReadBytes(in.File, surveyLimits)
	if err != nil {
		return nil, port.ErrSurveyImportInvalid.WithDetail(tabular.Message(err, surveyLimits))
	}
	p := &importPrep{a: a, manual: map[string]map[string]string{"item": {}, "commune": {}}}
	pv := &p.preview
	pv.SHA256, pv.FileName, pv.Columns = fileHash(in.File), cleanFileName(in.FileName), tb.Header
	pv.Unknown = map[string][]string{"item": {}, "commune": {}}
	if in.Mapping == nil {
		pv.Mapping = SuggestMapping(tb.Keys)
	} else {
		pv.Mapping = map[string]int{}
		for k, i := range in.Mapping {
			if !surveyFieldKnown(k) {
				return nil, problem.Validation.WithField("mapping", "trường "+k+" không có trong biểu mẫu")
			}
			if i >= len(tb.Header) {
				return nil, problem.Validation.WithField("mapping", "chỉ số cột vượt số cột của tệp")
			}
			if i >= 0 {
				pv.Mapping[k] = i
			}
		}
	}
	for _, f := range SurveyFields {
		if _, ok := pv.Mapping[f.Key]; f.Required && !ok {
			pv.Missing = append(pv.Missing, f.Key)
		}
	}
	if _, ok := pv.Mapping["output"]; !ok {
		if _, ok := pv.Mapping["yield_ha"]; !ok {
			pv.Missing = append(pv.Missing, "output")
		}
	}
	var (
		surveyPct decimal.Decimal
		states    map[string]domain.ItemState
		syn       map[string]map[string]string
		names     map[string]string
		imports   []store.SurveyImport
	)
	today := s.today()
	if err := s.tx(ctx, a.c.tenant, func(tx pgx.Tx) error {
		if surveyPct, err = s.surveyPct(ctx, tx, a.prof.Name); err != nil {
			return err
		}
		if p.sn, err = s.loadRound(ctx, tx, a.prof.Name, round, true); err != nil {
			return err
		}
		isn, err := loadSnap(ctx, tx)
		if err != nil {
			return err
		}
		states = domain.ItemStates(isn.vers, today)
		if syn, err = store.SurveySynonyms(ctx, tx); err != nil {
			return err
		}
		if names, err = communeNames(ctx, tx); err != nil {
			return err
		}
		imports, err = store.SurveyImports(ctx, tx, round)
		return err
	}); err != nil {
		return nil, err
	}
	if st := p.sn.state(today); st != domain.RoundCollecting && st != domain.RoundReviewing {
		return nil, port.ErrSurveyRoundState.WithDetail("Chỉ nhập tệp khi đợt Đang thu thập hoặc Đang rà soát")
	}
	for _, im := range imports {
		if im.SHA256 == pv.SHA256 {
			t := im.EventTime
			pv.AlreadyImported = &t
			break
		}
	}
	if len(pv.Missing) > 0 {
		return p, nil
	}
	// Cán bộ điều tra đã khai báo: họ tên từ kho định danh (đọc theo lô).
	refs := make([]uuid.UUID, 0, len(p.sn.officers))
	for _, o := range p.sn.officers {
		refs = append(refs, o.SubjectRef)
	}
	idm, err := s.identities(ctx, refs)
	if err != nil {
		return nil, err
	}
	offByCommune, offAll := map[string]map[string]uuid.UUID{}, map[string]uuid.UUID{}
	for _, o := range p.sn.officers {
		k := domain.Fold(idm[o.SubjectRef].FullName)
		if k == "" {
			continue
		}
		if offByCommune[o.Commune] == nil {
			offByCommune[o.Commune] = map[string]uuid.UUID{}
		}
		offByCommune[o.Commune][k] = o.OfficerID
		offAll[k] = o.OfficerID
	}
	items := itemIndex(states)
	communes := map[string]string{} // khóa chuẩn hóa / mã → mã xã của đợt
	inRound := map[string]bool{}
	for _, c := range p.sn.communes {
		inRound[c.Commune] = true
		communes[domain.Fold(c.Commune)] = c.Commune
		if n := names[c.Commune]; n != "" {
			communes[domain.FoldCommune(n)] = c.Commune
		}
	}
	lookItem := func(raw string) string {
		if c := in.Names["item"][raw]; c != "" {
			if _, ok := states[c]; ok {
				p.manual["item"][domain.Fold(raw)] = c
				return c
			}
		}
		k := domain.Fold(raw)
		if c := syn["item"][k]; c != "" {
			if st, ok := states[c]; ok && st.State == domain.ItemStateActive {
				return c
			}
		}
		return items[k]
	}
	lookCommune := func(raw string) string {
		if c := in.Names["commune"][raw]; c != "" {
			p.manual["commune"][domain.FoldCommune(raw)] = c
			return c
		}
		k := domain.FoldCommune(raw)
		if c := syn["commune"][k]; c != "" {
			return c
		}
		if c := communes[k]; c != "" {
			return c
		}
		return communes[domain.Fold(raw)]
	}
	unknown := map[string]map[string]bool{"item": {}, "commune": {}}
	get := func(r tabular.Row, f string) string {
		i, ok := pv.Mapping[f]
		if !ok || i < 0 || i >= len(r.Cells) {
			return ""
		}
		return strings.TrimSpace(r.Cells[i])
	}
	seen := map[string]int{}
	// Giá bán cùng mặt hàng trong đợt (phiếu đã có, không kể Đã xóa) — cho cờ CT-06 dự kiến.
	prices := map[string][]decimal.Decimal{}
	for _, f := range p.sn.forms {
		if f.Status != domain.FormDeleted {
			prices[f.Item] = append(prices[f.Item], effPrice(f))
		}
	}
	for _, r := range tb.Rows {
		ln := &ImportLine{Row: r.Line}
		row := &importRow{line: ln}
		p.rows = append(p.rows, row)
		bad := func(m string) { ln.Messages = append(ln.Messages, m); ln.Level = "error" }
		warn := func(m string) {
			ln.Messages = append(ln.Messages, m)
			if ln.Level != "error" {
				ln.Level = "warning"
			}
		}
		// Xã
		if v := get(r, "commune"); v == "" {
			bad("Thiếu xã")
		} else if c := lookCommune(v); c == "" {
			bad("Tên xã chưa khớp danh mục ĐVHC — chọn ánh xạ thủ công")
			unknown["commune"][v] = true
		} else if !inRound[c] {
			bad("Xã không thuộc danh sách xã điều tra của đợt")
		} else {
			row.commune, ln.Commune, ln.CommuneName = c, c, names[c]
		}
		// Nhóm cây
		if v := get(r, "crop"); v == "" {
			bad("Thiếu nhóm cây")
		} else if c := domain.ParseCrop(v); c == "" {
			bad("Nhóm cây không thuộc Lúa, Rau màu, Nấm, Cây ăn trái")
		} else if !slices.Contains(p.sn.round.Crops, c) {
			bad("Nhóm cây không áp dụng cho đợt")
		} else {
			row.crop, ln.Crop = c, c
		}
		// Mặt hàng (+ giống)
		if v := get(r, "item"); v == "" {
			bad("Thiếu mặt hàng")
		} else {
			raw := v
			code := ""
			if g := get(r, "variety"); g != "" {
				raw = v + " " + g
				code = lookItem(raw)
			}
			if code == "" {
				code = lookItem(v)
			}
			if code == "" {
				bad("Mặt hàng chưa khớp danh mục — chọn ánh xạ thủ công")
				unknown["item"][raw] = true
			} else {
				row.item, ln.Item, ln.ItemName = code, code, itemLabel(states[code])
			}
		}
		// Nông hộ
		row.farmer = strings.Join(strings.Fields(get(r, "farmer")), " ")
		if row.farmer == "" {
			bad("Thiếu họ tên nông hộ")
		} else if utf8.RuneCountInString(row.farmer) > 200 {
			bad("Họ tên nông hộ quá 200 ký tự")
		}
		if v := get(r, "phone"); v == "" {
			warn("Thiếu SĐT nông hộ — không chống trùng theo SĐT")
		} else if ph, ok := domain.NormalizePhone(v); !ok {
			warn("SĐT nông hộ không hợp lệ — bỏ SĐT, không chống trùng theo SĐT")
		} else {
			row.phone = ph
		}
		// Số liệu
		num := func(field, label string, required bool) (decimal.Decimal, bool) {
			v := get(r, field)
			if v == "" {
				if required {
					bad(label + " trống")
				}
				return decimal.Zero, !required
			}
			d, ok := domain.ParseNumber(v)
			if !ok {
				bad(label + " không phải số")
				return decimal.Zero, false
			}
			if d.IsNegative() {
				bad(label + " âm")
				return decimal.Zero, false
			}
			return d, true
		}
		f := domain.FormFigures{Costs: map[string]decimal.Decimal{}}
		area, okA := num("area", "Diện tích", false)
		out, okO := num("output", "Sản lượng", false)
		yha, okY := num("yield_ha", "Năng suất", false)
		if okA && okO && okY {
			if !out.IsPositive() && yha.IsPositive() {
				out = yha.Mul(area).Round(2)
			}
			if !area.IsPositive() || !out.IsPositive() {
				bad("Diện tích, năng suất hoặc sản lượng bằng 0 / trống")
			}
		}
		price, okP := num("price", "Giá bán", true)
		if okP && !price.IsPositive() {
			bad("Giá bán bằng 0")
		}
		f.AreaHa, f.OutputKg, f.SalePrice = area.Round(4), out.Round(2), price.Round(2)
		f.OtherIncome, _ = num("other_income", "Thu khác", false)
		for _, k := range domain.CostKeys {
			c, _ := num(k, domain.CostNames[k], false)
			f.Costs[k] = c.Round(2)
		}
		f.OtherIncome = f.OtherIncome.Round(2)
		row.figures = f
		// Cán bộ điều tra
		if v := get(r, "officer"); v != "" {
			k := domain.Fold(v)
			id, ok := offByCommune[row.commune][k]
			if !ok {
				id, ok = offAll[k]
			}
			if ok {
				row.officer, ln.OfficerID = &id, &id
			} else {
				warn("Cán bộ điều tra không có trong danh sách đã khai báo của đợt")
			}
		} else if _, mapped := pv.Mapping["officer"]; mapped {
			warn("Thiếu tên cán bộ điều tra")
		}
		if ln.Level == "error" {
			continue
		}
		calc := domain.CT05(f)
		ln.CostPerKg = &calc.CostPerKg
		one := decimal.NewFromInt(1)
		if v := get(r, "total_cost"); v != "" {
			if d, ok := domain.ParseNumber(v); !ok || d.Sub(calc.TotalCost).Abs().GreaterThan(one) {
				warn("Cột Tổng chi phí của tệp khác kết quả hệ thống tính lại — dùng giá trị tự tính")
			}
		}
		if v := get(r, "cost_per_kg"); v != "" {
			if d, ok := domain.ParseNumber(v); !ok || d.Sub(calc.CostPerKg).Abs().GreaterThan(one) {
				warn("Cột Giá thành của tệp khác kết quả hệ thống tính lại — dùng giá trị tự tính")
			}
		}
		// Trùng trong tệp (cùng SĐT, mặt hàng) → dòng sau bỏ qua (AC-03).
		if row.phone != "" {
			key := row.phone + "|" + row.item
			if first, dup := seen[key]; dup {
				warn(fmt.Sprintf("Trùng phiếu với dòng %d (cùng đợt, SĐT, mặt hàng) — bỏ qua dòng này", first))
				ln.Skip = true
				continue
			}
			seen[key] = r.Line
		} else if pv.AlreadyImported != nil {
			warn("Tệp đã được ghi nhận vào đợt và dòng không có SĐT — bỏ qua để không tạo phiếu trùng")
			ln.Skip = true
			continue
		}
		row.writable = true
		prices[row.item] = append(prices[row.item], f.SalePrice)
	}
	// Cờ CT-06 dự kiến (trung vị gồm phiếu đã có + dòng sẽ ghi).
	for _, row := range p.rows {
		if !row.writable {
			continue
		}
		if d := domain.CT06(row.figures.SalePrice, prices[row.item], surveyPct); d.Flag != "" {
			row.line.Flag = d.Flag
			dir := "cao"
			if d.Flag == domain.FlagLow {
				dir = "thấp"
			}
			row.line.Messages = append(row.line.Messages, fmt.Sprintf("Giá bán %s bất thường so với trung vị đợt (%s) — ghi nhận kèm cờ để rà soát",
				dir, signedPct(*d.Pct)))
			row.line.Level = "warning"
		}
	}
	for _, row := range p.rows {
		ln := row.line
		if ln.Level == "" {
			ln.Level = "ok"
		}
		switch {
		case ln.Level == "error":
			pv.Err++
		case ln.Skip:
			pv.Skip++
			pv.Warn++
		case ln.Level == "warning":
			pv.Warn++
		default:
			pv.OK++
		}
		pv.Lines = append(pv.Lines, *ln)
	}
	for k, m := range unknown {
		for v := range m {
			pv.Unknown[k] = append(pv.Unknown[k], v)
		}
		slices.Sort(pv.Unknown[k])
	}
	return p, nil
}

// effPrice là giá bán dùng để tính của phiếu: giá hiệu chỉnh sau xác minh nếu có (BR-04 FN-TI-02.12), không thì giá gốc.
func effPrice(f store.SurveyForm) decimal.Decimal {
	if f.VerifiedPrice != nil {
		return *f.VerifiedPrice
	}
	return f.SalePrice
}

// ProductSurveyIssuesExport, ProductSurveyFormsExport, ProductSurveyCostExport: sản phẩm của lần xuất trong sổ egress_event.
const (
	ProductSurveyIssuesExport = "price_survey_import_issues"
	ProductSurveyFormsExport  = "price_survey_forms"
	ProductSurveyCostExport   = "price_cost_report"
)

func cleanFileName(n string) string {
	n = strings.TrimSpace(n)
	if i := strings.LastIndexAny(n, `/\`); i >= 0 {
		n = n[i+1:]
	}
	if n == "" {
		n = "tep-nhap"
	}
	if r := []rune(n); len(r) > 200 {
		n = string(r[:200])
	}
	return n
}

// PreviewImport là bước 1–2 (Tải tệp – Ánh xạ – Xem trước): kiểm tệp, không ghi.
func (s *SurveyService) PreviewImport(ctx context.Context, profile string, round uuid.UUID, in ImportInput) (ImportPreview, error) {
	p, err := s.prepare(ctx, profile, round, in)
	if err != nil {
		return ImportPreview{}, err
	}
	return p.preview, nil
}

// CommitImport là bước 3 "Ghi nhận": ghi các dòng hợp lệ của tệp đã xem trước.
func (s *SurveyService) CommitImport(ctx context.Context, profile string, round uuid.UUID, in ImportInput, idem string) (ImportResult, error) {
	p, err := s.prepare(ctx, profile, round, in)
	if err != nil {
		return ImportResult{}, err
	}
	if in.SHA256 == "" || in.SHA256 != p.preview.SHA256 {
		return ImportResult{}, port.ErrSurveyImportChanged
	}
	if len(p.preview.Missing) > 0 {
		return ImportResult{}, port.ErrSurveyImportInvalid.WithDetail("Chưa ánh xạ cột bắt buộc: " + strings.Join(p.preview.Missing, ", "))
	}
	v, err := s.vault()
	if err != nil {
		return ImportResult{}, err
	}
	// Nông hộ → kho định danh (NGOÀI giao dịch DB). Trùng SĐT trong tenant → chủ thể cũ (ProblemDuplicate kèm Ref).
	refs := map[*importRow]uuid.UUID{}
	for _, row := range p.rows {
		if !row.writable {
			continue
		}
		cr, err := v.Create(ctx, surveyPurpose, client.Identity{Kind: client.SubjectHousehold, FullName: row.farmer, Phone: row.phone,
			AreaCode: row.commune})
		switch {
		case err == nil, errors.Is(err, client.ErrDuplicate):
			refs[row] = cr.Ref
		default:
			var pe *problem.Problem
			if errors.As(err, &pe) && pe.Status == 422 {
				row.writable = false
				row.line.Level = "error"
				row.line.Messages = append(row.line.Messages, "Kho định danh từ chối họ tên / SĐT nông hộ")
				continue
			}
			return ImportResult{}, err
		}
	}
	a := p.a
	res := ImportResult{ImportID: ids.New(), Rows: len(p.rows)}
	err = s.tx(ctx, a.c.tenant, func(tx pgx.Tx) error {
		if err := store.LockSurveyRound(ctx, tx, a.c.tenant, round); err != nil {
			return err
		}
		sn, err := s.loadRound(ctx, tx, a.prof.Name, round, true)
		if err != nil {
			return err
		}
		if st := sn.state(s.today()); st != domain.RoundCollecting && st != domain.RoundReviewing {
			return port.ErrSurveyRoundState.WithDetail("Chỉ nhập tệp khi đợt Đang thu thập hoặc Đang rà soát")
		}
		have := map[string]bool{}
		for _, f := range sn.forms {
			if f.Status != domain.FormDeleted && f.HasPhone {
				have[f.FarmerRef.String()+"|"+f.Item] = true
			}
		}
		now := s.Clock.Now()
		for _, row := range p.rows {
			ln := row.line
			if row.writable && row.phone != "" {
				k := refs[row].String() + "|" + row.item
				if have[k] {
					row.writable, ln.Skip = false, true
					ln.Messages = append(ln.Messages, "Trùng phiếu đã có trong đợt (cùng SĐT, mặt hàng) — bỏ qua")
					if ln.Level != "error" {
						ln.Level = "warning"
					}
				}
				have[k] = true
			}
			if row.writable {
				f := store.SurveyForm{FormID: ids.New(), RoundID: round, ImportID: res.ImportID, RowNo: ln.Row, Commune: row.commune,
					Crop: row.crop, Item: row.item, FarmerRef: refs[row], HasPhone: row.phone != "", OfficerID: row.officer,
					AreaHa: row.figures.AreaHa, OutputKg: row.figures.OutputKg, SalePrice: row.figures.SalePrice,
					OtherIncome: row.figures.OtherIncome, Costs: row.figures.Costs, Status: domain.FormImported, Op: "import"}
				if err := store.InsertSurveyForm(ctx, tx, ids.New(), a.c.tenant, f, now, a.c.ref, idemKey(idem, "form:"+strconv.Itoa(ln.Row))); err != nil {
					return err
				}
				res.Created++
			}
			switch {
			case ln.Level == "error":
				res.Errors++
			case ln.Skip:
				res.Skipped++
				res.Warnings++
			case ln.Level == "warning":
				res.Warnings++
			}
			if ln.Level == "error" || ln.Level == "warning" {
				msg := strings.Join(ln.Messages, "; ")
				if r := []rune(msg); len(r) > 1000 {
					msg = string(r[:1000])
				}
				lvl := "warning"
				if ln.Level == "error" {
					lvl = "error"
				}
				if err := store.InsertSurveyIssue(ctx, tx, ids.New(), a.c.tenant, res.ImportID, store.SurveyIssue{RowNo: ln.Row, Level: lvl, Message: msg},
					now, a.c.ref, idemKey(idem, "issue:"+strconv.Itoa(ln.Row))); err != nil {
					return err
				}
			}
			res.Lines = append(res.Lines, *ln)
		}
		if err := store.InsertSurveyImport(ctx, tx, ids.New(), a.c.tenant, store.SurveyImport{ImportID: res.ImportID, RoundID: round,
			SHA256: p.preview.SHA256, FileName: p.preview.FileName, Rows: res.Rows, Created: res.Created, Skipped: res.Skipped,
			Warnings: res.Warnings, Errors: res.Errors}, now, a.c.ref, idemKey(idem, "import")); err != nil {
			return err
		}
		if !in.SaveSynonyms {
			return nil
		}
		if err := store.LockSurveySynonyms(ctx, tx, a.c.tenant); err != nil {
			return err
		}
		cur, err := store.SurveySynonyms(ctx, tx)
		if err != nil {
			return err
		}
		for kind, m := range p.manual {
			for key, code := range m {
				if key == "" || utf8.RuneCountInString(key) > 200 || cur[kind][key] == code {
					continue
				}
				if err := store.InsertSurveySynonym(ctx, tx, ids.New(), a.c.tenant, kind, key, code, true, now, a.c.ref,
					idemKey(idem, "syn:"+kind+":"+key)); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return res, err
}

// ListImports trả các lần nhập của đợt (Cấp Tỉnh, Admin).
func (s *SurveyService) ListImports(ctx context.Context, profile string, round uuid.UUID) ([]store.SurveyImport, error) {
	a, err := s.access(ctx, profile)
	if err != nil {
		return nil, err
	}
	if err := a.needRead(); err != nil {
		return nil, err
	}
	var out []store.SurveyImport
	err = s.tx(ctx, a.c.tenant, func(tx pgx.Tx) error {
		if _, err := s.loadRound(ctx, tx, a.prof.Name, round, false); err != nil {
			return err
		}
		out, err = store.SurveyImports(ctx, tx, round)
		return err
	})
	return out, err
}

// ExportImportIssues xuất danh sách lỗi / cảnh báo của một lần nhập ra Excel (luồng phụ "Tải danh sách lỗi") — không có
// giá trị của tệp, chỉ số dòng và nội dung lỗi.
func (s *SurveyService) ExportImportIssues(ctx context.Context, profile string, round, imp uuid.UUID) (Export, error) {
	a, err := s.access(ctx, profile)
	if err != nil {
		return Export{}, err
	}
	if err := a.needRead(); err != nil {
		return Export{}, err
	}
	var (
		issues []store.SurveyIssue
		found  bool
	)
	err = s.tx(ctx, a.c.tenant, func(tx pgx.Tx) error {
		if _, err := s.loadRound(ctx, tx, a.prof.Name, round, false); err != nil {
			return err
		}
		ims, err := store.SurveyImports(ctx, tx, round)
		if err != nil {
			return err
		}
		for _, im := range ims {
			found = found || im.ImportID == imp
		}
		if !found {
			return port.ErrSurveyImportNotFound
		}
		issues, err = store.SurveyIssues(ctx, tx, imp)
		return err
	})
	if err != nil {
		return Export{}, err
	}
	rows := make([][]string, 0, len(issues))
	for _, is := range issues {
		lvl := "Cảnh báo"
		if is.Level == "error" {
			lvl = "Lỗi"
		}
		rows = append(rows, []string{strconv.Itoa(is.RowNo), lvl, is.Message})
	}
	var buf bytes.Buffer
	if err := tabular.WriteXLSX(&buf, "Danh sách lỗi", []string{"Dòng", "Mức", "Nội dung"}, rows); err != nil {
		return Export{}, err
	}
	x := Export{Body: buf.Bytes(), ContentType: tabular.ContentTypeXLSX, FileName: "danh-sach-loi-" + imp.String()[:8] + ".xlsx", Rows: len(rows)}
	return s.observe(ctx, ProductSurveyIssuesExport, x, nil)
}
