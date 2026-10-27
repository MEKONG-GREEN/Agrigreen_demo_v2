// Package domain: quy tắc nhật ký nuôi BM-08 (gà lai thương phẩm), BM-09 (vịt xiêm), BM-19 (thủy sản) của phòng Chăn
// nuôi – Thủy sản — W11.48, D4 CN8, BRD CNTS v3.7 UR-XA-11 (NC-01…NC-16), UR-XA-12 (NT-01…NT-15), UR-TINH-12, Mục III.5.
//
// Lứa nuôi (NC-02, NC-03, NC-15) và lần ghi có chỉ số. Mẫu sổ BM-19 chưa có (Q-077) — trường theo bảng NT của BRD là giả
// định tạm (docs/kn/gia-dinh/W11.48.md). Hàm thuần, không I/O.
package domain

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// RuleVersion là phiên bản quy tắc ghi vào sổ.
const RuleVersion = "cnts-diary/1"

// Mẫu nhật ký.
const (
	FormBM08 = "BM-08" // gà lai thương phẩm
	FormBM09 = "BM-09" // vịt xiêm
	FormBM19 = "BM-19" // thủy sản (Q-077: mẫu sổ chờ phòng gửi)
)

// Forms là các mẫu nhật ký.
var Forms = []string{FormBM08, FormBM09, FormBM19}

// IsForm cho biết f là mẫu nhật ký.
func IsForm(f string) bool { return slices.Contains(Forms, f) }

// Loại lần ghi (NT-02; BM-08/09 ghi theo ngày).
const (
	KindDaily     = "daily"      // BM-08/09: lần ghi hằng ngày (kèm sinh trưởng theo tuần tuổi khi cân)
	KindGrowthEnv = "growth_env" // BM-19: cân đo + môi trường
	KindEnv       = "env"        // BM-19: môi trường
	KindHarvest   = "harvest"    // BM-19: thu hoạch
)

// KindsOf trả loại lần ghi hợp lệ của mẫu.
func KindsOf(form string) []string {
	if form == FormBM19 {
		return []string{KindGrowthEnv, KindEnv, KindHarvest}
	}
	return []string{KindDaily}
}

// Sự việc của sổ lứa / lần ghi.
const (
	EventCreated   = "created"
	EventUpdated   = "updated"
	EventClosed    = "closed"
	EventReopened  = "reopened"
	EventSubmitted = "submitted"
	EventReturned  = "returned"
	EventVerified  = "verified"
	EventCorrected = "corrected"
)

// Trạng thái lần ghi (Mục III.5 "Lần ghi nhật ký").
const (
	StatusDraft     = "draft"
	StatusSubmitted = "submitted"
	StatusReturned  = "returned"
	StatusVerified  = "verified"
)

// Trạng thái lứa.
const (
	CycleOpen   = "open"
	CycleClosed = "closed"
)

// Loại ảnh (NT-13: ảnh cân mẫu, ảnh bộ test; NC-16 ảnh đàn).
const (
	PhotoWeighing = "weighing"
	PhotoTestKit  = "test_kit"
	PhotoFlock    = "flock"
	PhotoOther    = "other"
)

// PhotoKinds là loại ảnh hợp lệ.
var PhotoKinds = []string{PhotoWeighing, PhotoTestKit, PhotoFlock, PhotoOther}

// Giới hạn.
const (
	MaxPhotos      = 10
	MaxRows        = 50
	MaxText        = 1000
	MaxShort       = 200
	MinReason      = 5
	MaxPhotoBytes  = 512 << 10 // NT-13 "≤ 500 KB/ảnh sau nén" (làm tròn 512 KiB)
	MaxBackfillDay = 7         // ngày ghi lùi tối đa so thời điểm thiết bị (bù ngày quên ghi)
	MaxOfflineDays = 30        // bản ngoại tuyến gửi muộn tối đa
	FutureSkew     = 5 * time.Minute
)

// FieldErr là lỗi một trường.
type FieldErr struct {
	Field  string
	Reason string
}

// Errors là danh sách lỗi trường (422).
type Errors []FieldErr

func (e Errors) Error() string {
	parts := make([]string, 0, len(e))
	for _, x := range e {
		parts = append(parts, x.Field+": "+x.Reason)
	}
	return strings.Join(parts, "; ")
}

func (e *Errors) add(field, reason string) { *e = append(*e, FieldErr{Field: field, Reason: reason}) }

func (e *Errors) text(field string, s *string, max int) {
	*s = strings.TrimSpace(*s)
	if utf8.RuneCountInString(*s) > max {
		e.add(field, "tối đa "+strconv.Itoa(max)+" ký tự")
	}
}

// ValidReason kiểm lý do (yêu cầu bổ sung, điều chỉnh, mở lại lứa).
func ValidReason(s string) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(s))
	return n >= MinReason && n <= MaxText
}

// Day quy t về ngày (giờ Việt Nam) dạng 00:00 UTC.
func Day(t time.Time, zone *time.Location) time.Time {
	v := t.In(zone)
	return time.Date(v.Year(), v.Month(), v.Day(), 0, 0, 0, 0, time.UTC)
}

// ---------------------------------------------------------------------------
// Lứa nuôi (NC-02, NC-03, NC-15)
// ---------------------------------------------------------------------------

// Supply là một dòng vật tư nhận (NC-03, mục 1 "Thông tin chung" của sổ): vật tư (con giống, thức ăn gà/vịt con, gà/vịt
// lớn, vaccine, chế phẩm…), ngày nhận, số lượng, chủng loại vật tư. Dòng cũ (trước BF-CN-DIARY) chỉ có chủng loại.
type Supply struct {
	Item     string `json:"item,omitempty"`
	Date     string `json:"date"`
	Kind     string `json:"kind"`
	Quantity string `json:"quantity"`
	Unit     string `json:"unit"`
	Note     string `json:"note,omitempty"`
}

// Cycle là các trường nghiệp vụ của một phiên bản lứa.
type Cycle struct {
	Form             string
	Breed            string
	ReceivedDate     time.Time
	InitialCount     int64
	InitialWeightG   *decimal.Decimal
	AgeDays          *int64
	Supplies         []Supply
	VaccinationBatch *uuid.UUID
	Note             string
}

// Normalize kiểm lứa; today là ngày hiện tại (giờ VN).
func (c *Cycle) Normalize(today time.Time) Errors {
	var e Errors
	if !IsForm(c.Form) {
		e.add("form", "BM-08, BM-09 hoặc BM-19")
	}
	e.text("breed", &c.Breed, MaxShort)
	if c.Breed == "" {
		e.add("breed", "nhập giống / đối tượng nuôi")
	}
	if c.ReceivedDate.IsZero() {
		e.add("received_date", "bắt buộc")
	} else if c.ReceivedDate.After(today) {
		e.add("received_date", "không ở tương lai")
	}
	if c.InitialCount < 1 || c.InitialCount > 100_000_000 {
		e.add("initial_count", "1–100.000.000 con")
	}
	if c.InitialWeightG != nil && (!c.InitialWeightG.IsPositive() || c.InitialWeightG.GreaterThan(decimal.New(1, 7))) {
		e.add("initial_weight_g", "phải dương")
	}
	if c.AgeDays != nil && (*c.AgeDays < 0 || *c.AgeDays > 3650) {
		e.add("age_days", "0–3650")
	}
	if len(c.Supplies) > MaxRows {
		e.add("supplies", "tối đa 50 dòng")
	}
	for i := range c.Supplies {
		s := &c.Supplies[i]
		f := "supplies[" + strconv.Itoa(i) + "]"
		if _, err := time.Parse(time.DateOnly, s.Date); err != nil {
			e.add(f+".date", "dạng YYYY-MM-DD")
		}
		e.text(f+".item", &s.Item, MaxShort)
		e.text(f+".kind", &s.Kind, MaxShort)
		if s.Item == "" && s.Kind == "" {
			e.add(f+".item", "nhập vật tư hoặc chủng loại")
		}
		if d, err := decimal.NewFromString(strings.TrimSpace(s.Quantity)); err != nil || d.IsNegative() {
			e.add(f+".quantity", "số không âm")
		} else {
			s.Quantity = d.String()
		}
		e.text(f+".unit", &s.Unit, 20)
		e.text(f+".note", &s.Note, MaxShort)
	}
	e.text("note", &c.Note, MaxText)
	return e
}

// Accounting là hạch toán kinh tế cuối lứa (NC-15). Khoản để trống = 0.
type Accounting struct {
	CostSeed, CostFeed, CostDepreciation, CostTransport, CostVet, CostProbiotic *decimal.Decimal
	RevenueHeads                                                                *int64
	OutputKg, Price, Revenue                                                    *decimal.Decimal
}

// Normalize kiểm hạch toán: tổng thu bắt buộc; số tiền nguyên đồng, không âm.
func (a *Accounting) Normalize() Errors {
	var e Errors
	money := func(field string, d *decimal.Decimal) {
		if d != nil && (d.IsNegative() || !d.Equal(d.Truncate(0)) || d.GreaterThanOrEqual(decimal.New(1, 15))) {
			e.add(field, "số đồng nguyên, không âm")
		}
	}
	money("cost_seed", a.CostSeed)
	money("cost_feed", a.CostFeed)
	money("cost_depreciation", a.CostDepreciation)
	money("cost_transport", a.CostTransport)
	money("cost_vet", a.CostVet)
	money("cost_probiotic", a.CostProbiotic)
	money("revenue_price", a.Price)
	money("revenue_amount", a.Revenue)
	if a.Revenue == nil {
		e.add("revenue_amount", "nhập tổng thu cuối lứa (NC-15)")
	}
	if a.RevenueHeads != nil && *a.RevenueHeads < 0 {
		e.add("revenue_heads", "không âm")
	}
	if a.OutputKg != nil && (a.OutputKg.IsNegative() || a.OutputKg.GreaterThanOrEqual(decimal.New(1, 12))) {
		e.add("revenue_output_kg", "không âm")
	}
	return e
}

func zero(d *decimal.Decimal) decimal.Decimal {
	if d == nil {
		return decimal.Zero
	}
	return *d
}

// TotalCost là tổng chi.
func (a Accounting) TotalCost() decimal.Decimal {
	return zero(a.CostSeed).Add(zero(a.CostFeed)).Add(zero(a.CostDepreciation)).Add(zero(a.CostTransport)).
		Add(zero(a.CostVet)).Add(zero(a.CostProbiotic))
}

// Profit = tổng thu − tổng chi (nil khi chưa hạch toán).
func (a Accounting) Profit() *decimal.Decimal {
	if a.Revenue == nil {
		return nil
	}
	p := a.Revenue.Sub(a.TotalCost())
	return &p
}

// UnitCost là giá thành (đồng/kg) = tổng chi / sản lượng (nil khi sản lượng 0 hoặc chưa có).
func (a Accounting) UnitCost() *decimal.Decimal {
	if a.OutputKg == nil || !a.OutputKg.IsPositive() {
		return nil
	}
	u := a.TotalCost().Div(*a.OutputKg).Round(0)
	return &u
}

// ---------------------------------------------------------------------------
// Lần ghi — chỉ số (metrics) và bảng phụ (tables)
// ---------------------------------------------------------------------------

// Kiểu chỉ số.
const (
	typeInt  = "int"
	typeDec  = "dec"
	typeText = "text"
	typeEnum = "enum"
)

// MetricDef mô tả một chỉ số của mẫu nhật ký.
type MetricDef struct {
	Code     string
	Label    string
	Unit     string
	Type     string
	Ref      string   // mã trường BRD (NC-xx / NT-xx)
	Kinds    []string // loại lần ghi dùng chỉ số
	Required []string // loại lần ghi bắt buộc
	Values   []string // enum
	Scale    int32    // số chữ số thập phân tối đa (dec)
}

// Nguyên nhân chết (NC-07), sức khỏe đàn (NC-08).
var (
	DeathCauses = []string{"benh", "yeu", "de", "thoi_tiet", "khac"}
	HealthState = []string{"tot", "binh_thuong", "kem", "co_benh"}
)

var (
	daily  = []string{KindDaily}
	gEnv   = []string{KindGrowthEnv}
	water  = []string{KindGrowthEnv, KindEnv, KindHarvest}
	water2 = []string{KindGrowthEnv, KindEnv}
)

// poultry là chỉ số BM-08/BM-09 (UR-XA-11).
var poultry = []MetricDef{
	{Code: "alive", Label: "Số con sống", Unit: "con", Type: typeInt, Ref: "NC-05", Kinds: daily, Required: daily},
	{Code: "dead", Label: "Số con chết", Unit: "con", Type: typeInt, Ref: "NC-06", Kinds: daily, Required: daily},
	{Code: "death_cause", Label: "Nguyên nhân chết", Type: typeEnum, Ref: "NC-07", Kinds: daily, Values: DeathCauses},
	{Code: "death_note", Label: "Ghi chú nguyên nhân chết", Type: typeText, Ref: "NC-07", Kinds: daily},
	{Code: "feed_kg", Label: "Thức ăn sử dụng", Unit: "kg", Type: typeDec, Ref: "NC-08", Kinds: daily, Required: daily, Scale: 2},
	{Code: "health", Label: "Sức khỏe đàn", Type: typeEnum, Ref: "NC-08", Kinds: daily, Values: HealthState},
	{Code: "health_note", Label: "Ghi chú sức khỏe", Type: typeText, Ref: "NC-08", Kinds: daily},
	{Code: "age_week", Label: "Tuần tuổi (khi cân)", Unit: "tuần", Type: typeInt, Ref: "NC-12", Kinds: daily},
	{Code: "weight_avg_g", Label: "Trọng lượng bình quân", Unit: "g/con", Type: typeDec, Ref: "NC-12", Kinds: daily, Scale: 1},
}

// aquatic là chỉ số BM-19 (UR-XA-12; Q-077 — giả định tạm theo bảng NT).
var aquatic = []MetricDef{
	{Code: "sample_count", Label: "Số mẫu cân", Unit: "con", Type: typeInt, Ref: "NT-04", Kinds: gEnv, Required: gEnv},
	{Code: "weight_avg_g", Label: "Trọng lượng trung bình", Unit: "g/con", Type: typeDec, Ref: "NT-05", Kinds: gEnv, Required: gEnv, Scale: 1},
	{Code: "length_avg_cm", Label: "Chiều dài trung bình", Unit: "cm", Type: typeDec, Ref: "NT-06", Kinds: gEnv, Required: gEnv, Scale: 1},
	{Code: "loss", Label: "Hao hụt từ lần trước", Unit: "con", Type: typeInt, Ref: "NT-07", Kinds: water, Required: water},
	{Code: "ph", Label: "pH", Type: typeDec, Ref: "NT-08", Kinds: water, Required: water2, Scale: 1},
	{Code: "do_mgl", Label: "Oxy hòa tan", Unit: "mg/l", Type: typeDec, Ref: "NT-09", Kinds: water, Required: water2, Scale: 2},
	{Code: "temp_c", Label: "Nhiệt độ", Unit: "°C", Type: typeDec, Ref: "NT-10", Kinds: water, Scale: 1},
	{Code: "salinity", Label: "Độ mặn", Unit: "‰", Type: typeDec, Ref: "NT-10", Kinds: water, Scale: 1},
	{Code: "feed_kg", Label: "Thức ăn sử dụng từ lần trước", Unit: "kg", Type: typeDec, Ref: "NT-11", Kinds: water, Required: water2, Scale: 2},
	{Code: "action_note", Label: "Biện pháp xử lý, ghi chú", Type: typeText, Ref: "NT-12", Kinds: water},
	{Code: "harvest_kg", Label: "Sản lượng thu hoạch", Unit: "kg", Type: typeDec, Ref: "NT-02", Kinds: []string{KindHarvest}, Required: []string{KindHarvest}, Scale: 2},
	{Code: "harvest_price", Label: "Giá bán", Unit: "đồng/kg", Type: typeDec, Ref: "NT-02", Kinds: []string{KindHarvest}, Scale: 0},
}

// MetricsOf trả chỉ số của mẫu.
func MetricsOf(form string) []MetricDef {
	if form == FormBM19 {
		return aquatic
	}
	return poultry
}

// Bảng phụ của sổ gà/vịt (UR-XA-11).
const (
	TableFeedPurchases = "feed_purchases" // NC-09, mục 3 của sổ: mua thức ăn chăn nuôi
	TableFeedMaterials = "feed_materials" // NC-09, mục 4 của sổ: mua nguyên liệu thức ăn chăn nuôi (BF-CN-DIARY)
	TableTreatments    = "treatments"     // NC-10 (không có ở sổ vịt xiêm 2026)
	TableVaccinations  = "vaccinations"   // NC-11
	TableSales         = "sales"          // NC-13 (không có ở sổ vịt xiêm 2026)
	TableOfficerNotes  = "officer_notes"  // NC-14
)

// TablesOf trả bảng phụ của mẫu (NC-16: nhóm trường bật/tắt theo mẫu — sổ vịt xiêm 2026 không có phòng, trị bệnh và xuất
// bán).
func TablesOf(form string) []string {
	switch form {
	case FormBM08:
		return []string{TableFeedPurchases, TableFeedMaterials, TableTreatments, TableVaccinations, TableSales, TableOfficerNotes}
	case FormBM09:
		return []string{TableFeedPurchases, TableFeedMaterials, TableVaccinations, TableOfficerNotes}
	}
	return nil
}

// FeedPurchase NC-09 (mục 3 mua thức ăn, mục 4 mua nguyên liệu thức ăn — cùng cột: ngày, tên, số lượng kg, đơn giá,
// thành tiền tự tính, ghi chú).
type FeedPurchase struct {
	Date      string `json:"date"`
	Name      string `json:"name"`
	QtyKg     string `json:"quantity_kg"`
	UnitPrice string `json:"unit_price"`
	Note      string `json:"note,omitempty"`
}

// Treatment NC-10.
type Treatment struct {
	Date           string `json:"date"`
	Condition      string `json:"condition,omitempty"`
	Symptoms       string `json:"symptoms,omitempty"`
	Drug           string `json:"drug"`
	Dosage         string `json:"dosage,omitempty"`
	Treater        string `json:"treater,omitempty"`
	WithdrawalDays int    `json:"withdrawal_days"`
	Result         string `json:"result,omitempty"`
}

// Vaccination NC-11 (mục 5 của sổ: ngày tiêm, tên vaccine, liều lượng, cách dùng, ngày sản xuất vaccine, ghi chú). Lần
// tiêm (BRD) không có trên sổ giấy — 0 là chưa ghi.
type Vaccination struct {
	Vaccine string `json:"vaccine"`
	Shot    int    `json:"shot"`
	Date    string `json:"date"`
	Dosage  string `json:"dosage,omitempty"`
	Method  string `json:"method,omitempty"`
	MfgDate string `json:"mfg_date,omitempty"`
	Note    string `json:"note,omitempty"`
}

// Sale NC-13.
type Sale struct {
	Date      string `json:"date"`
	Kind      string `json:"kind,omitempty"`
	Heads     int64  `json:"heads"`
	WeightKg  string `json:"weight_kg"`
	UnitPrice string `json:"unit_price"`
}

// OfficerNote NC-14 (phần 3 của sổ: ngày, họ và tên cán bộ, nội dung yêu cầu, ý kiến nông dân).
type OfficerNote struct {
	Date          string `json:"date"`
	Officer       string `json:"officer,omitempty"`
	Content       string `json:"content"`
	FarmerOpinion string `json:"farmer_opinion,omitempty"`
}

// Tables là bảng phụ của một lần ghi.
type Tables struct {
	FeedPurchases []FeedPurchase `json:"feed_purchases,omitempty"`
	FeedMaterials []FeedPurchase `json:"feed_materials,omitempty"`
	Treatments    []Treatment    `json:"treatments,omitempty"`
	Vaccinations  []Vaccination  `json:"vaccinations,omitempty"`
	Sales         []Sale         `json:"sales,omitempty"`
	OfficerNotes  []OfficerNote  `json:"officer_notes,omitempty"`
}

// Entry là phần nghiệp vụ của một phiên bản lần ghi.
type Entry struct {
	Kind      string
	EntryDate time.Time
	Metrics   map[string]string
	Tables    Tables
}

func date(e *Errors, field, s string) (time.Time, bool) {
	d, err := time.Parse(time.DateOnly, strings.TrimSpace(s))
	if err != nil {
		e.add(field, "dạng YYYY-MM-DD")
		return time.Time{}, false
	}
	return d, true
}

func nonNeg(e *Errors, field, s string, scale int32) string {
	d, err := decimal.NewFromString(strings.TrimSpace(s))
	if err != nil || d.IsNegative() || d.GreaterThanOrEqual(decimal.New(1, 12)) {
		e.add(field, "số không âm")
		return s
	}
	if scale >= 0 && !d.Equal(d.Truncate(scale)) {
		e.add(field, "tối đa "+strconv.Itoa(int(scale))+" chữ số thập phân")
	}
	return d.String()
}

// Normalize kiểm phần nghiệp vụ của lần ghi theo mẫu form (hình thức; quy tắc chặn khi gửi ở Evaluate).
func (en *Entry) Normalize(form string) Errors {
	var e Errors
	if !slices.Contains(KindsOf(form), en.Kind) {
		e.add("entry_kind", "không hợp lệ với mẫu "+form+": "+strings.Join(KindsOf(form), ", "))
		return e
	}
	defs := MetricsOf(form)
	out := map[string]string{}
	for k := range en.Metrics {
		if !slices.ContainsFunc(defs, func(d MetricDef) bool { return d.Code == k && slices.Contains(d.Kinds, en.Kind) }) {
			e.add("metrics."+k, "chỉ số không có trong mẫu "+form+" ("+en.Kind+")")
		}
	}
	for _, d := range defs {
		if !slices.Contains(d.Kinds, en.Kind) {
			continue
		}
		v := strings.TrimSpace(en.Metrics[d.Code])
		f := "metrics." + d.Code
		if v == "" {
			if slices.Contains(d.Required, en.Kind) {
				if d.Code == "dead" {
					out[d.Code] = "0" // NC-06 mặc định 0
					continue
				}
				e.add(f, "bắt buộc ("+d.Ref+" "+d.Label+")")
			}
			continue
		}
		switch d.Type {
		case typeInt:
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 0 || n > 100_000_000 {
				e.add(f, "số nguyên không âm")
				continue
			}
			out[d.Code] = strconv.FormatInt(n, 10)
		case typeDec:
			out[d.Code] = nonNeg(&e, f, v, d.Scale)
		case typeEnum:
			if !slices.Contains(d.Values, v) {
				e.add(f, "một trong: "+strings.Join(d.Values, ", "))
				continue
			}
			out[d.Code] = v
		case typeText:
			if utf8.RuneCountInString(v) > MaxText {
				e.add(f, "tối đa 1000 ký tự")
				continue
			}
			out[d.Code] = v
		}
	}
	if form == FormBM19 {
		if v, ok := out["ph"]; ok {
			if d, _ := decimal.NewFromString(v); d.GreaterThan(decimal.NewFromInt(14)) {
				e.add("metrics.ph", "0–14")
			}
		}
	}
	en.Metrics = out
	allowed := TablesOf(form)
	t := &en.Tables
	check := func(name string, n int) bool {
		if n == 0 {
			return false
		}
		if !slices.Contains(allowed, name) {
			e.add("tables."+name, "mẫu "+form+" không có mục này")
			return false
		}
		if n > MaxRows {
			e.add("tables."+name, "tối đa 50 dòng")
			return false
		}
		return true
	}
	purchases := func(name string, rows []FeedPurchase) {
		if !check(name, len(rows)) {
			return
		}
		for i := range rows {
			r := &rows[i]
			f := "tables." + name + "[" + strconv.Itoa(i) + "]"
			date(&e, f+".date", r.Date)
			e.text(f+".name", &r.Name, MaxShort)
			if r.Name == "" {
				e.add(f+".name", "bắt buộc")
			}
			r.QtyKg = nonNeg(&e, f+".quantity_kg", r.QtyKg, 2)
			r.UnitPrice = nonNeg(&e, f+".unit_price", r.UnitPrice, 0)
			e.text(f+".note", &r.Note, MaxShort)
		}
	}
	purchases(TableFeedPurchases, t.FeedPurchases)
	purchases(TableFeedMaterials, t.FeedMaterials)
	if check(TableTreatments, len(t.Treatments)) {
		for i := range t.Treatments {
			r := &t.Treatments[i]
			f := "tables.treatments[" + strconv.Itoa(i) + "]"
			date(&e, f+".date", r.Date)
			for _, s := range []*string{&r.Condition, &r.Symptoms, &r.Dosage, &r.Treater, &r.Result} {
				e.text(f, s, MaxShort)
			}
			e.text(f+".drug", &r.Drug, MaxShort)
			if r.Drug == "" {
				e.add(f+".drug", "bắt buộc")
			}
			if r.WithdrawalDays < 0 || r.WithdrawalDays > 365 {
				e.add(f+".withdrawal_days", "0–365")
			}
		}
	}
	if check(TableVaccinations, len(t.Vaccinations)) {
		for i := range t.Vaccinations {
			r := &t.Vaccinations[i]
			f := "tables.vaccinations[" + strconv.Itoa(i) + "]"
			date(&e, f+".date", r.Date)
			e.text(f+".vaccine", &r.Vaccine, MaxShort)
			if r.Vaccine == "" {
				e.add(f+".vaccine", "bắt buộc")
			}
			if r.Shot < 0 || r.Shot > 20 {
				e.add(f+".shot", "0–20 (0 = không ghi lần)")
			}
			e.text(f+".dosage", &r.Dosage, MaxShort)
			e.text(f+".method", &r.Method, MaxShort)
			e.text(f+".note", &r.Note, MaxShort)
			if strings.TrimSpace(r.MfgDate) != "" {
				date(&e, f+".mfg_date", r.MfgDate)
			}
		}
	}
	if check(TableSales, len(t.Sales)) {
		for i := range t.Sales {
			r := &t.Sales[i]
			f := "tables.sales[" + strconv.Itoa(i) + "]"
			date(&e, f+".date", r.Date)
			e.text(f+".kind", &r.Kind, MaxShort)
			if r.Heads < 0 || r.Heads > 100_000_000 {
				e.add(f+".heads", "số con không âm")
			}
			r.WeightKg = nonNeg(&e, f+".weight_kg", r.WeightKg, 2)
			r.UnitPrice = nonNeg(&e, f+".unit_price", r.UnitPrice, 0)
		}
	}
	if check(TableOfficerNotes, len(t.OfficerNotes)) {
		for i := range t.OfficerNotes {
			r := &t.OfficerNotes[i]
			f := "tables.officer_notes[" + strconv.Itoa(i) + "]"
			date(&e, f+".date", r.Date)
			e.text(f+".officer", &r.Officer, MaxShort)
			e.text(f+".content", &r.Content, MaxText)
			if r.Content == "" {
				e.add(f+".content", "bắt buộc")
			}
			e.text(f+".farmer_opinion", &r.FarmerOpinion, MaxText)
		}
	}
	return e
}

// Int đọc chỉ số nguyên (ok = false nếu thiếu).
func Int(m map[string]string, k string) (int64, bool) {
	v, ok := m[k]
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(v, 10, 64)
	return n, err == nil
}

// Dec đọc chỉ số thập phân (nil nếu thiếu).
func Dec(m map[string]string, k string) *decimal.Decimal {
	v, ok := m[k]
	if !ok {
		return nil
	}
	d, err := decimal.NewFromString(v)
	if err != nil {
		return nil
	}
	return &d
}

// SoldHeads là tổng số con xuất bán trong NGÀY của lần ghi (NC-13 trừ vào số con sống).
func (t Tables) SoldHeads(day time.Time) int64 {
	var n int64
	for _, s := range t.Sales {
		if s.Date == day.Format(time.DateOnly) {
			n += s.Heads
		}
	}
	return n
}

// FeedCost là tổng tiền mua thức ăn và nguyên liệu thức ăn (NC-09, mục 3 + 4 của sổ → gợi ý chi thức ăn NC-15).
func (t Tables) FeedCost() decimal.Decimal {
	sum := decimal.Zero
	for _, r := range slices.Concat(t.FeedPurchases, t.FeedMaterials) {
		q, e1 := decimal.NewFromString(r.QtyKg)
		p, e2 := decimal.NewFromString(r.UnitPrice)
		if e1 == nil && e2 == nil {
			sum = sum.Add(q.Mul(p))
		}
	}
	return sum.Round(0)
}

// SalesSum trả tổng số con, khối lượng, tiền bán (NC-13 → gợi ý thu NC-15).
func (t Tables) SalesSum() (heads int64, kg, amount decimal.Decimal) {
	for _, r := range t.Sales {
		heads += r.Heads
		w, e1 := decimal.NewFromString(r.WeightKg)
		p, e2 := decimal.NewFromString(r.UnitPrice)
		if e1 == nil {
			kg = kg.Add(w)
		}
		if e1 == nil && e2 == nil {
			amount = amount.Add(w.Mul(p))
		}
	}
	return heads, kg, amount.Round(0)
}

// ---------------------------------------------------------------------------
// Vị trí (NT-15, NC-16; Mục III.5 dòng 9)
// ---------------------------------------------------------------------------

// Fix là tọa độ thiết bị lúc ghi.
type Fix struct {
	Lat, Lng  float64
	AccuracyM *decimal.Decimal
	Mock      bool // thiết bị báo vị trí giả lập
}

// Validate kiểm tọa độ (lãnh thổ Việt Nam mở rộng) và sai số.
func (f Fix) Validate() Errors {
	var e Errors
	if math.IsNaN(f.Lat) || math.IsNaN(f.Lng) || f.Lat < 5 || f.Lat > 25 || f.Lng < 100 || f.Lng > 118 {
		e.add("location", "tọa độ ngoài phạm vi Việt Nam")
	}
	if f.AccuracyM != nil && (f.AccuracyM.IsNegative() || f.AccuracyM.GreaterThan(decimal.NewFromInt(100000))) {
		e.add("location.accuracy_m", "0–100000 m")
	}
	return e
}

// Point là tọa độ (vĩ, kinh độ).
type Point struct{ Lat, Lng float64 }

// DistanceM là khoảng cách mặt cầu (haversine, m) giữa hai điểm.
func DistanceM(a, b Point) float64 {
	const r = 6371008.8
	rad := math.Pi / 180
	dLat, dLng := (b.Lat-a.Lat)*rad, (b.Lng-a.Lng)*rad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(a.Lat*rad)*math.Cos(b.Lat*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * r * math.Asin(math.Min(1, math.Sqrt(h)))
}

// Location là kết quả đánh giá vị trí lúc ghi (chép vào sổ — không có tọa độ).
type Location struct {
	HasFix        bool
	AccuracyM     *decimal.Decimal
	DistanceM     *decimal.Decimal
	NoTargetPoint bool
	Far           bool
	WeakAccuracy  bool
	MockDevice    bool
	MockSuspected bool
	MockReasons   []string
}

// EvaluateLocation so tọa độ lúc ghi với điểm của hộ/điểm trình diễn (target nil = chưa có tọa độ). maxDist, maxAcc là
// ngưỡng (200 m, 50 m). others là tọa độ các lần ghi KHÁC của cùng hộ — trùng y hệt (≤ 1e-7 độ) là dấu hiệu giả lập.
func EvaluateLocation(fix *Fix, target *Point, maxDist, maxAcc decimal.Decimal, others []Point) Location {
	var l Location
	if fix == nil {
		return l
	}
	l.HasFix, l.AccuracyM, l.MockDevice = true, fix.AccuracyM, fix.Mock
	if target == nil {
		l.NoTargetPoint = true
	} else {
		d := decimal.NewFromFloat(DistanceM(Point{fix.Lat, fix.Lng}, *target)).Round(1)
		l.DistanceM = &d
		l.Far = d.GreaterThan(maxDist)
	}
	if fix.AccuracyM != nil && fix.AccuracyM.GreaterThan(maxAcc) {
		l.WeakAccuracy = true
	}
	if fix.Mock {
		l.MockReasons = append(l.MockReasons, "thiết bị báo vị trí giả lập")
	}
	if fix.AccuracyM != nil && fix.AccuracyM.IsZero() {
		l.MockReasons = append(l.MockReasons, "sai số GPS bằng 0")
	}
	for _, o := range others {
		if math.Abs(o.Lat-fix.Lat) <= 1e-7 && math.Abs(o.Lng-fix.Lng) <= 1e-7 {
			l.MockReasons = append(l.MockReasons, "tọa độ trùng y hệt lần ghi trước")
			break
		}
	}
	l.MockSuspected = len(l.MockReasons) > 0
	return l
}

// ---------------------------------------------------------------------------
// Kiểm tra tự động (UR-XA-11, UR-XA-12 "Quy tắc kiểm tra", Mục III.5 dòng 6–9)
// ---------------------------------------------------------------------------

// Mức kết quả kiểm tra.
const (
	LevelBlock = "block" // chặn gửi
	LevelWarn  = "warn"  // cảnh báo, đánh dấu lần ghi
	LevelInfo  = "info"
)

// Loại cảnh báo của cntsalert (Mục III.5) mà kết quả kiểm tra sinh ra.
const (
	AlertWater     = "water_quality"
	AlertLowGrowth = "low_growth"
	AlertMortality = "mortality"
	AlertLocation  = "log_location"
)

// AlertKinds là các loại cảnh báo lần ghi có thể sinh (tự đóng khi hết vi phạm).
var AlertKinds = []string{AlertWater, AlertLowGrowth, AlertMortality, AlertLocation}

// Check là một kết quả kiểm tra tự động.
type Check struct {
	Code    string `json:"code"`
	Level   string `json:"level"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
	Alert   string `json:"alert,omitempty"`
	Value   string `json:"value,omitempty"`
	Compare string `json:"compare,omitempty"`
}

// Previous là lần ghi trước (đã gửi) của cùng lứa để so sánh.
type Previous struct {
	EntryDate time.Time
	Metrics   map[string]string
}

// Thresholds là ngưỡng (tham số cnts.*).
type Thresholds struct {
	PHMin, PHMax, DOMin *decimal.Decimal
	MortalityMaxPct     *decimal.Decimal // nil = chưa cấu hình → không cảnh báo
	WeightStandardG     *decimal.Decimal // chuẩn kỹ thuật (UR-TINH-07) — chưa có → so lần cân trước
}

// EvalInput là đầu vào kiểm tra một lần ghi.
type EvalInput struct {
	Form           string
	Entry          Entry
	InitialCount   int64       // số con đầu kỳ của lứa (lần ghi đầu)
	Prev           *Previous   // lần ghi trước gần nhất (theo ngày ghi) của lứa
	PrevWeight     *Previous   // lần cân trước gần nhất có trọng lượng
	Treatments     []Treatment // NC-10 của các lần ghi khác trong lứa (ngưng thuốc)
	WeighingPhotos int
	Location       Location
	Thresholds     Thresholds
	MaxDistM       decimal.Decimal
	MaxAccM        decimal.Decimal
}

func dstr(d decimal.Decimal) string { return d.String() }

// Evaluate trả kết quả kiểm tra tự động: lỗi chặn (LevelBlock) và cảnh báo. Gọi khi gửi/điều chỉnh/sửa bản đã gửi.
func Evaluate(in EvalInput) []Check {
	var out []Check
	add := func(c Check) { out = append(out, c) }
	m, en := in.Entry.Metrics, in.Entry
	switch in.Form {
	case FormBM08, FormBM09:
		alive, okA := Int(m, "alive")
		dead, _ := Int(m, "dead")
		sold := en.Tables.SoldHeads(en.EntryDate)
		prevAlive := in.InitialCount
		if in.Prev != nil {
			if v, ok := Int(in.Prev.Metrics, "alive"); ok {
				prevAlive = v
			}
		}
		if okA && alive != prevAlive-dead-sold {
			add(Check{Code: "headcount_mismatch", Level: LevelBlock, Field: "metrics.alive",
				Message: "NC-05: số con sống phải bằng số sống lần trước (" + strconv.FormatInt(prevAlive, 10) + ") − số chết (" +
					strconv.FormatInt(dead, 10) + ") − số con xuất bán trong ngày (" + strconv.FormatInt(sold, 10) + ")",
				Value: strconv.FormatInt(alive, 10), Compare: strconv.FormatInt(prevAlive-dead-sold, 10)})
		}
		if dead > 0 && m["death_cause"] == "" {
			add(Check{Code: "death_cause_missing", Level: LevelBlock, Field: "metrics.death_cause",
				Message: "NC-07: có con chết thì phải ghi nguyên nhân chết"})
		}
		if in.Thresholds.MortalityMaxPct != nil && prevAlive > 0 && dead > 0 {
			r := decimal.NewFromInt(dead).Div(decimal.NewFromInt(prevAlive)).Mul(decimal.NewFromInt(100)).Round(2)
			if r.GreaterThan(*in.Thresholds.MortalityMaxPct) {
				add(Check{Code: "mortality_high", Level: LevelWarn, Field: "metrics.dead", Alert: AlertMortality,
					Message: "Tỷ lệ chết " + dstr(r) + "% vượt ngưỡng " + dstr(*in.Thresholds.MortalityMaxPct) + "% — cân nhắc \"Báo nghi dịch\"",
					Value:   dstr(r), Compare: dstr(*in.Thresholds.MortalityMaxPct)})
			}
		}
		// Xuất bán còn trong thời gian ngưng thuốc (NC-10) → cảnh báo.
		all := append(slices.Clone(in.Treatments), en.Tables.Treatments...)
		for _, s := range en.Tables.Sales {
			sd, err := time.Parse(time.DateOnly, s.Date)
			if err != nil {
				continue
			}
			for _, t := range all {
				td, err := time.Parse(time.DateOnly, t.Date)
				if err != nil || t.WithdrawalDays <= 0 {
					continue
				}
				end := td.AddDate(0, 0, t.WithdrawalDays)
				if !sd.Before(td) && sd.Before(end) {
					add(Check{Code: "withdrawal_period", Level: LevelWarn, Field: "tables.sales",
						Message: "Ngày xuất bán " + s.Date + " còn trong thời gian ngưng thuốc " + t.Drug + " (hết ngày " +
							end.AddDate(0, 0, -1).Format(time.DateOnly) + ")"})
					break
				}
			}
		}
	case FormBM19:
		water := false
		if ph := Dec(m, "ph"); ph != nil {
			if (in.Thresholds.PHMin != nil && ph.LessThan(*in.Thresholds.PHMin)) || (in.Thresholds.PHMax != nil && ph.GreaterThan(*in.Thresholds.PHMax)) {
				water = true
				add(Check{Code: "ph_out_of_range", Level: LevelWarn, Field: "metrics.ph", Alert: AlertWater,
					Message: "pH " + dstr(*ph) + " ngoài ngưỡng " + rangeText(in.Thresholds.PHMin, in.Thresholds.PHMax), Value: dstr(*ph)})
			}
		}
		if do := Dec(m, "do_mgl"); do != nil && in.Thresholds.DOMin != nil && do.LessThan(*in.Thresholds.DOMin) {
			water = true
			add(Check{Code: "do_low", Level: LevelWarn, Field: "metrics.do_mgl", Alert: AlertWater,
				Message: "Oxy hòa tan " + dstr(*do) + " mg/l dưới ngưỡng " + dstr(*in.Thresholds.DOMin) + " mg/l", Value: dstr(*do),
				Compare: dstr(*in.Thresholds.DOMin)})
		}
		if water && strings.TrimSpace(m["action_note"]) == "" {
			add(Check{Code: "action_note_missing", Level: LevelBlock, Field: "metrics.action_note",
				Message: "NT-12: có cảnh báo môi trường nước thì ghi biện pháp xử lý"})
		}
		if en.Kind == KindGrowthEnv && in.WeighingPhotos == 0 {
			add(Check{Code: "photo_missing", Level: LevelBlock, Field: "photo_refs",
				Message: "NT-13: lần cân đo phải có ảnh cân mẫu"})
		}
	}
	// Tăng trọng (NC-12, NT-14; Mục III.5 dòng 7).
	if w := Dec(m, "weight_avg_g"); w != nil {
		if std := in.Thresholds.WeightStandardG; std != nil && w.LessThan(*std) {
			add(Check{Code: "low_growth", Level: LevelWarn, Field: "metrics.weight_avg_g", Alert: AlertLowGrowth,
				Message: "Trọng lượng " + dstr(*w) + " g dưới chuẩn " + dstr(*std) + " g", Value: dstr(*w), Compare: dstr(*std)})
		} else if in.PrevWeight != nil {
			if p := Dec(in.PrevWeight.Metrics, "weight_avg_g"); p != nil {
				if !w.GreaterThan(*p) {
					add(Check{Code: "low_growth", Level: LevelWarn, Field: "metrics.weight_avg_g", Alert: AlertLowGrowth,
						Message: "Trọng lượng bình quân " + dstr(*w) + " g không tăng so lần cân trước " + dstr(*p) + " g (" +
							in.PrevWeight.EntryDate.Format("02/01/2006") + ")", Value: dstr(*w), Compare: dstr(*p)})
				}
				if p.IsPositive() {
					g := w.Sub(*p).Div(*p).Mul(decimal.NewFromInt(100)).Round(1)
					add(Check{Code: "growth_pct", Level: LevelInfo, Field: "metrics.weight_avg_g",
						Message: "Tăng trọng so lần trước " + dstr(g) + "%", Value: dstr(g)})
				}
			}
		}
	}
	// Vị trí (NT-15, Mục III.5 dòng 9).
	l := in.Location
	switch {
	case !l.HasFix:
		add(Check{Code: "no_fix", Level: LevelWarn, Field: "location", Message: "Lần ghi không có vị trí GPS"})
	case l.NoTargetPoint:
		add(Check{Code: "no_target_point", Level: LevelWarn, Field: "location", Alert: AlertLocation,
			Message: "Hộ / điểm mô hình chưa có tọa độ — chủ nhiệm cập nhật tọa độ điểm"})
	case l.Far:
		add(Check{Code: "far_from_point", Level: LevelWarn, Field: "location", Alert: AlertLocation,
			Message: "Cách điểm mô hình " + dstr(*l.DistanceM) + " m (> " + dstr(in.MaxDistM) + " m)", Value: dstr(*l.DistanceM),
			Compare: dstr(in.MaxDistM)})
	}
	if l.WeakAccuracy {
		add(Check{Code: "weak_gps", Level: LevelWarn, Field: "location", Alert: AlertLocation,
			Message: "Sai số GPS " + dstr(*l.AccuracyM) + " m (> " + dstr(in.MaxAccM) + " m)", Value: dstr(*l.AccuracyM)})
	}
	if l.MockSuspected {
		add(Check{Code: "mock_location", Level: LevelWarn, Field: "location", Alert: AlertLocation,
			Message: "Nghi vị trí giả lập: " + strings.Join(l.MockReasons, "; ")})
	}
	return out
}

func rangeText(min, max *decimal.Decimal) string {
	switch {
	case min != nil && max != nil:
		return dstr(*min) + "–" + dstr(*max)
	case min != nil:
		return "≥ " + dstr(*min)
	case max != nil:
		return "≤ " + dstr(*max)
	}
	return ""
}

// Blocking trả các kết quả chặn gửi.
func Blocking(cs []Check) []Check {
	var out []Check
	for _, c := range cs {
		if c.Level == LevelBlock {
			out = append(out, c)
		}
	}
	return out
}

// Hits cho biết loại cảnh báo kind có vi phạm trong cs.
func Hits(cs []Check, kind string) []Check {
	var out []Check
	for _, c := range cs {
		if c.Alert == kind && c.Level == LevelWarn {
			out = append(out, c)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Vòng đời lần ghi
// ---------------------------------------------------------------------------

// Hành động trên lần ghi.
const (
	ActUpdate  = "update"
	ActSubmit  = "submit"
	ActReturn  = "return"
	ActVerify  = "verify"
	ActCorrect = "correct"
)

// Transition trả (sự việc, trạng thái mới) của hành động act từ trạng thái cur; ok = false nếu không cho phép.
func Transition(cur, act string) (event, next string, ok bool) {
	switch act {
	case ActUpdate:
		if cur == StatusDraft || cur == StatusSubmitted || cur == StatusReturned {
			return EventUpdated, cur, true
		}
	case ActSubmit:
		if cur == StatusDraft || cur == StatusReturned {
			return EventSubmitted, StatusSubmitted, true
		}
	case ActReturn:
		if cur == StatusSubmitted {
			return EventReturned, StatusReturned, true
		}
	case ActVerify:
		if cur == StatusSubmitted {
			return EventVerified, StatusVerified, true
		}
	case ActCorrect:
		if cur == StatusVerified {
			return EventCorrected, StatusSubmitted, true
		}
	}
	return "", "", false
}

// EditableUntil là ngày cuối được sửa lần ghi đã gửi chưa xác thực ("Chưa xác thực: sửa trong 7 ngày") tính từ ngày ghi.
func EditableUntil(entryDate time.Time, days int) time.Time { return entryDate.AddDate(0, 0, days) }
