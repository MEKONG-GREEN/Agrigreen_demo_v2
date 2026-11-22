package domain

import (
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// SiteFacts là số liệu một điểm trình diễn (W11.47).
type SiteFacts struct {
	Name       string
	Commune    string // tên xã
	Scale      string // "1.500 con"
	Households int    // hộ đang tham gia
}

// Place trả "điểm, xã".
func (s SiteFacts) Place() string {
	if s.Commune == "" {
		return s.Name
	}
	return s.Name + ", " + s.Commune
}

// DiaryFacts là thực tế của một điểm lấy từ nhật ký nuôi (W11.48) — chỉ lứa có ít nhất một lần ghi Đã xác thực (UR-TINH-12:
// lần ghi phải được chủ nhiệm xác thực trước khi dùng cho Mẫu 05).
type DiaryFacts struct {
	Households int              // hộ có lứa đã xác thực
	Heads      int64            // tổng số con/giống thả đầu kỳ
	OutputKg   *decimal.Decimal // sản lượng xuất bán (lứa đã hạch toán)
	Profit     *decimal.Decimal // lợi nhuận = thu − chi (lứa đã hạch toán)
	From, To   *time.Time       // ngày nhận giống sớm nhất, ngày ghi xác thực muộn nhất
	Verified   int              // số lần ghi đã xác thực
}

// Facts là dữ liệu điền sẵn/điền tự động của một biểu.
type Facts struct {
	ProjectName string
	ProjectCode string
	Manager     string
	Duration    string // "01/03/2027 – 31/12/2027"
	Budget      string // đồng, chuỗi số
	Basis       string
	Sites       []SiteFacts
	Site        *SiteFacts  // Mẫu 05
	Diary       *DiaryFacts // Mẫu 05
	Households  []Row       // Mẫu 05 Phụ lục I: chu_ho, quy_mo
	Signed05    []Row       // Mẫu 07: dòng "diem" từ Mẫu 05 đã ký
}

// Actuals trả "thực tế từ nhật ký" theo mã chỉ tiêu (Mẫu 05).
func (d *DiaryFacts) Actuals() map[string]string {
	out := map[string]string{}
	if d == nil || d.Verified == 0 {
		return out
	}
	out["so_ho"] = strconv.Itoa(d.Households)
	if d.Heads > 0 {
		out["quy_mo"] = strconv.FormatInt(d.Heads, 10) + " con"
	}
	if d.OutputKg != nil {
		out["nang_suat"] = d.OutputKg.String() + " kg"
	}
	if d.Profit != nil {
		out["hieu_qua"] = d.Profit.String() + " đồng"
	}
	if d.From != nil && d.To != nil {
		out["thoi_gian"] = d.From.Format("02/01/2006") + " – " + d.To.Format("02/01/2006")
	}
	return out
}

// Prefill dựng nội dung ban đầu khi lập biểu (B2: "Lập mẫu từ chi tiết dự án; số liệu điền sẵn"), rồi Fill.
func (s Spec) Prefill(f Facts) Content {
	c := Content{Fields: map[string]string{}, Tables: map[string][]Row{}}
	set := func(k, v string) {
		if v = strings.TrimSpace(v); v != "" {
			c.Fields[k] = v
		}
	}
	communes := []string{}
	for _, x := range f.Sites {
		if x.Commune != "" && !contains(communes, x.Commune) {
			communes = append(communes, x.Commune)
		}
	}
	switch s.Form {
	case Form01:
		set("can_cu", f.Basis)
		for _, x := range f.Sites {
			c.Tables["mo_hinh"] = append(c.Tables["mo_hinh"], Row{"dia_diem": x.Place(), "quy_mo_ke_hoach": x.Scale, "so_ho_ke_hoach": strconv.Itoa(x.Households)})
		}
		c.Tables["tap_huan"] = []Row{{"noi_dung": "Tập huấn hộ tham gia mô hình"}, {"noi_dung": "Tập huấn nhân rộng mô hình"}}
	case Form03:
		if f.Basis != "" {
			set("can_cu", "Căn cứ "+f.Basis+";")
		}
		c.Tables["doi_tuong"] = []Row{{"ten": f.ProjectName, "chu_tri": f.Manager, "dia_diem": strings.Join(communes, "; "),
			"quy_mo": strconv.Itoa(len(f.Sites)) + " điểm trình diễn"}}
		for _, d := range DocChecklist {
			c.Tables["ho_so"] = append(c.Tables["ho_so"], Row{"ten": d})
		}
	case Form04:
		set("hop_dong", f.Basis)
		set("ten_lop", "Tập huấn nhân rộng mô hình "+f.ProjectName)
	case Form05:
		set("hop_dong", f.Basis)
		set("ten_mo_hinh", f.ProjectName)
		if f.Site != nil {
			set("dia_diem", f.Site.Place())
		}
		rows := IndicatorRows(Form05)
		act := f.Diary.Actuals()
		for i, r := range rows {
			switch r["ma"] {
			case "quy_mo":
				if f.Site != nil {
					rows[i]["yeu_cau"] = f.Site.Scale
				}
			case "so_ho":
				if f.Site != nil {
					rows[i]["yeu_cau"] = strconv.Itoa(f.Site.Households)
				}
			}
			if v := act[r["ma"]]; v != "" {
				rows[i]["thuc_te"] = v // thực tế lấy từ nhật ký (quy tắc 9); sửa phải có ghi chú
			}
		}
		c.Tables["chi_tieu"] = rows
		c.Tables["phu_luc_1"] = append([]Row(nil), f.Households...)
	case Form07:
		set("hop_dong", f.Basis)
		set("ten_mo_hinh", f.ProjectName)
		c.Tables["chi_tieu"] = IndicatorRows(Form07)
	}
	s.Fill(&c, f)
	return c
}

// Fill ghi các ô/cột/bảng Auto từ số liệu hiện hành (gọi ở mỗi lần lưu và trước khi chuyển trạng thái).
func (s Spec) Fill(c *Content, f Facts) {
	if c.Fields == nil {
		c.Fields = map[string]string{}
	}
	if c.Tables == nil {
		c.Tables = map[string][]Row{}
	}
	put := func(k, v string) {
		if v == "" {
			delete(c.Fields, k)
		} else {
			c.Fields[k] = v
		}
	}
	switch s.Form {
	case Form01:
		put("ten_du_an", f.ProjectName)
		put("ma_du_an", f.ProjectCode)
		put("chu_nhiem", f.Manager)
		put("thoi_gian_du_an", f.Duration)
		put("kinh_phi", f.Budget)
	case Form05:
		act := f.Diary.Actuals()
		for i, r := range c.Tables["chi_tieu"] {
			if v := act[r["ma"]]; v != "" {
				c.Tables["chi_tieu"][i]["thuc_te_nhat_ky"] = v
			} else {
				delete(c.Tables["chi_tieu"][i], "thuc_te_nhat_ky")
			}
		}
	case Form07:
		rows := append([]Row(nil), f.Signed05...)
		if rows == nil {
			rows = []Row{}
		}
		c.Tables["diem"] = rows
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// Summary05 trả dòng "diem" của Mẫu 07 từ nội dung một Mẫu 05 đã ký (kết quả chính: năng suất, hiệu quả).
func Summary05(c Content) Row {
	r := Row{"dia_diem": c.Fields["dia_diem"]}
	var main []string
	for _, x := range c.Tables["chi_tieu"] {
		v := x["thuc_te"]
		switch x["ma"] {
		case "quy_mo":
			r["quy_mo"] = v
		case "so_ho":
			r["so_ho"] = v
		case "thoi_gian":
			r["thoi_gian"] = v
		case "nang_suat", "hieu_qua":
			if v != "" {
				main = append(main, strings.TrimSpace(strings.TrimLeft(x["chi_tieu"], "0123456789. "))+": "+v)
			}
		}
	}
	if len(main) > 0 {
		r["ket_qua"] = strings.Join(main, "; ")
	}
	for k, v := range r {
		if v == "" {
			delete(r, k)
		}
	}
	return r
}
