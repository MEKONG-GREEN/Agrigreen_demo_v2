package app

// W11.45: Word Báo cáo phòng BM-11 qua pf/export (`cap/export`, W10.17). Bố cục theo báo cáo giấy của phòng: dòng cơ quan
// "TRUNG TÂM KHUYẾN NÔNG <TỈNH> / PHÒNG CHĂN NUÔI - THỦY SẢN", "Số: {n}/BC-CNTS", "<tỉnh>, ngày …"; BÁO CÁO "Kết quả thực
// hiện nhiệm vụ Nông nghiệp và Môi trường"; tuần: A. Kết quả (1.1 Thủy sản, 1.2 Chăn nuôi — bảng Stt/Nội dung/Số lượng),
// B. Hạn chế, C. Nhiệm vụ (kết quả chương trình, dự án; phối hợp; kế hoạch tuần tới), D. Đề xuất; tháng/quý/năm: I. Kết quả,
// II. Đánh giá chung, III. Phương hướng; "Nơi nhận" theo danh sách nơi nhận của báo cáo; KT. TRƯỞNG PHÒNG.
//
// Số văn bản chỉ cấp cho báo cáo ĐÃ GỬI (một số mỗi báo cáo, dùng lại khi xuất lại; cấp trong giao dịch của pf/export → không
// trùng khi xuất đồng thời). Bản Đang soạn/Chờ duyệt in "…/BC-CNTS" (không tiêu số). Đọc báo cáo trong giao dịch KN rồi ĐÓNG;
// pf/export và đơn vị hành chính gọi NGOÀI giao dịch KN.

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/store"
	exportport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/doctpl"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Mẫu Word BM-11.
const (
	BM11WordOwner      = "pf/extension"
	BM11WordWeekKey    = "cnts.bm11_tuan"
	BM11WordPeriodKey  = "cnts.bm11_ky"
	BM11WordVersion    = 1
	BM11WordSeries     = "BC-CNTS"
	bm11VarNumber      = "so_van_ban"
	bm11VarPlaceDate   = "dia_danh_ngay"
	bm11ListLivestock  = "chan_nuoi"
	bm11ListPrograms   = "chuong_trinh"
	bm11ListRecipients = "noi_nhan"
	bm11ListMissing    = "xa_thieu"
	bm11ListSupplement = "bo_sung"
)

// BM11WordSubject là đối tượng của lịch sử Word và số văn bản trong pf/export: một báo cáo nhận một số.
func BM11WordSubject(id uuid.UUID) string { return "cnts_bm11:" + id.String() }

func bm11Template(week bool) doctpl.Template {
	v := func(key, label string, req bool) doctpl.Var { return doctpl.Var{Key: key, Label: label, Required: req} }
	p := func(text string) doctpl.Block {
		return doctpl.Block{Kind: doctpl.KindParagraph, Text: text, Indent: true, Align: doctpl.AlignJustify}
	}
	pIf := func(cond, text string) doctpl.Block { b := p(text); b.If = cond; return b }
	h := func(text string) doctpl.Block { return doctpl.Block{Kind: doctpl.KindHeading, Text: text} }
	sec := func(key, title string, blocks ...doctpl.Block) doctpl.Block {
		return doctpl.Block{Kind: doctpl.KindSection, Key: key, Text: title, Blocks: blocks}
	}
	vars := []doctpl.Var{v(bm11VarNumber, "Số văn bản", false), v(bm11VarPlaceDate, "Địa danh, ngày", true), v("tinh", "Tỉnh (in hoa)", true),
		v("tieu_de_ky", "Kỳ báo cáo", true), v("tong_hop_xa", "Tổng hợp từ n/N xã", true), v("trang_thai", "Trạng thái", true),
		v("khai_thac", "Khai thác trong kỳ (tấn)", true), v("khai_thac_luy_ke", "Khai thác lũy kế năm (tấn)", true),
		v("nuoi_trong", "Nuôi trồng trong kỳ (tấn)", true), v("nuoi_trong_luy_ke", "Nuôi trồng lũy kế năm (tấn)", true),
		v("tong_san_luong", "Tổng sản lượng trong kỳ (tấn)", true), v("ky_toi", "Kỳ tới", false), v("dau_du_lieu", "Dấu dữ liệu", true)}
	for _, s := range append(domain.BM11SectionsOf(domain.BM11Week), domain.BM11SectionsOf(domain.BM11Month)...) {
		vars = appendVar(vars, v("muc_"+s.Key, s.Label, false))
	}
	lists := []doctpl.List{
		{Key: bm11ListLivestock, Label: "Chăn nuôi", Fields: []string{"stt", "noi_dung", "so_luong", "nhom"}},
		{Key: bm11ListPrograms, Label: "Chương trình, dự án", Fields: []string{"ten", "tu_dong", "noi_dung"}},
		{Key: bm11ListRecipients, Label: "Nơi nhận", Fields: []string{"ten"}},
		{Key: bm11ListMissing, Label: "Xã chưa nộp", Fields: []string{"bieu", "xa"}},
		{Key: bm11ListSupplement, Label: "Số liệu kỳ trước nộp bổ sung", Fields: []string{"ky", "chi_tieu", "da_gui", "hien_tai", "dvt"}},
	}
	head := []doctpl.Block{
		{Kind: doctpl.KindColumns, Split: 4300,
			Left: []doctpl.Line{{Text: "TRUNG TÂM KHUYẾN NÔNG {tinh}"}, {Text: "PHÒNG CHĂN NUÔI - THỦY SẢN", Bold: true}, {Text: "Số: {so_van_ban}"}},
			Right: []doctpl.Line{{Text: "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", Bold: true}, {Text: "Độc lập - Tự do - Hạnh phúc", Bold: true},
				{Text: "{dia_danh_ngay}", Italic: true}}},
		{Kind: doctpl.KindParagraph, Text: "BÁO CÁO", Align: doctpl.AlignCenter, Bold: true},
		{Kind: doctpl.KindParagraph, Text: "Kết quả thực hiện nhiệm vụ Nông nghiệp và Môi trường\n({tieu_de_ky})", Align: doctpl.AlignCenter, Bold: true},
		{Kind: doctpl.KindParagraph, Text: "{tong_hop_xa} — {trang_thai}.", Align: doctpl.AlignCenter, Italic: true},
	}
	fishery := []doctpl.Block{
		h("Thủy sản"),
		p("- Khai thác và nuôi trồng thủy sản:"),
		p("+ Tổng sản lượng khai thác tự nhiên {khai_thac} tấn bao gồm cá, tôm, mực và các loại thủy sản khác, lũy kế {khai_thac_luy_ke} tấn."),
		p("+ Về nuôi trồng thủy sản: tổng sản lượng là {nuoi_trong} tấn, lũy kế {nuoi_trong_luy_ke} tấn."),
		p("+ Tổng sản lượng thủy sản trong kỳ: {tong_san_luong} tấn."),
	}
	livestock := []doctpl.Block{h("Chăn nuôi"),
		{Kind: doctpl.KindTable, Each: bm11ListLivestock, EmptyText: "Chưa có số liệu.", Columns: []doctpl.Column{
			{Header: "Stt", Value: "{chan_nuoi.stt}", Width: 800, Align: doctpl.AlignCenter},
			{Header: "Nội dung", Value: "{chan_nuoi.noi_dung}", Width: 4600, BoldIf: "nhom"},
			{Header: "Số lượng", Value: "{chan_nuoi.so_luong}", Width: 2400, Align: doctpl.AlignRight},
			{Header: "Ghi chú", Value: "", Width: 1200},
		}}}
	programs := doctpl.Block{Kind: doctpl.KindLoop, Each: bm11ListPrograms, Blocks: []doctpl.Block{
		{Kind: doctpl.KindParagraph, Text: "• {chuong_trinh.ten}", Bold: true},
		pIf("chuong_trinh.tu_dong", "{chuong_trinh.tu_dong}"),
		pIf("chuong_trinh.noi_dung", "{chuong_trinh.noi_dung}"),
	}}
	text := func(key string) doctpl.Block { return pIf("muc_"+key, "{muc_"+key+"}") }
	tail := []doctpl.Block{
		{Kind: doctpl.KindSection, Key: "phu_luc_xa", Text: "Phụ lục: xã chưa nộp (không ước tính phần thiếu)", If: bm11ListMissing, Blocks: []doctpl.Block{
			{Kind: doctpl.KindTable, Each: bm11ListMissing, Columns: []doctpl.Column{
				{Header: "Biểu", Value: "{xa_thieu.bieu}", Width: 2000}, {Header: "Xã chưa nộp", Value: "{xa_thieu.xa}", Width: 7000}}}}},
		{Kind: doctpl.KindColumns, Split: 4600,
			Left: []doctpl.Line{{Text: "Nơi nhận:", Bold: true, Italic: true}, {Text: "{noi_nhan_1}", If: "noi_nhan_1"}, {Text: "{noi_nhan_2}", If: "noi_nhan_2"},
				{Text: "{noi_nhan_3}", If: "noi_nhan_3"}, {Text: "- Lưu: CNTS."}},
			Right: []doctpl.Line{{Text: "KT. TRƯỞNG PHÒNG", Bold: true}, {Text: "PHÓ TRƯỞNG PHÒNG", Bold: true}, {Text: ""}, {Text: ""}, {Text: ""}}},
		{Kind: doctpl.KindParagraph, Text: "Dấu dữ liệu: {dau_du_lieu}", Italic: true},
	}
	for i := 1; i <= 3; i++ {
		vars = append(vars, v("noi_nhan_"+strconv.Itoa(i), "Nơi nhận "+strconv.Itoa(i), false))
	}
	supplement := doctpl.Block{Kind: doctpl.KindSection, Key: "bo_sung", Text: "Số liệu kỳ trước nộp bổ sung", If: bm11ListSupplement,
		Blocks: []doctpl.Block{{Kind: doctpl.KindTable, Each: bm11ListSupplement, Columns: []doctpl.Column{
			{Header: "Kỳ", Value: "{bo_sung.ky}", Width: 1800}, {Header: "Chỉ tiêu", Value: "{bo_sung.chi_tieu}", Width: 3400},
			{Header: "Đã gửi", Value: "{bo_sung.da_gui}", Width: 1500, Align: doctpl.AlignRight},
			{Header: "Hiện tại", Value: "{bo_sung.hien_tai}", Width: 1500, Align: doctpl.AlignRight},
			{Header: "ĐVT", Value: "{bo_sung.dvt}", Width: 900}}}}}
	var body []doctpl.Block
	if week {
		body = append(head,
			sec("ket_qua", "A. Kết quả đạt được",
				sec("san_xuat", "1. Về sản xuất nông, lâm, thủy sản", append(append(fishery, livestock...), supplement)...),
				sec("nhiem_vu_khac", "2. Một số nhiệm vụ khác có liên quan", text("nhiem_vu_khac"))),
			sec("han_che", "B. Đánh giá hạn chế, vướng mắc", text("han_che")),
			sec("nhiem_vu", "C. Nhiệm vụ", h("1. Kết quả thực hiện các chương trình, dự án"), programs,
				h("2. Công tác phối hợp"), text("phoi_hop"), h("3. Kế hoạch thực hiện nhiệm vụ trong tuần tới"), text("ke_hoach")),
			sec("de_xuat", "D. Đề xuất, kiến nghị", text("de_xuat")))
	} else {
		body = append(head,
			sec("ket_qua", "I. KẾT QUẢ THỰC HIỆN NHIỆM VỤ NÔNG NGHIỆP VÀ MÔI TRƯỜNG",
				sec("san_xuat", "1. Kết quả sản xuất", append(append(fishery, livestock...), supplement)...),
				sec("chuong_trinh", "2. Kết quả thực hiện các chương trình, dự án", programs),
				sec("phoi_hop", "3. Các công tác phối hợp khác", text("phoi_hop"))),
			sec("danh_gia", "II. ĐÁNH GIÁ CHUNG", h("1. Tiến độ thực hiện chương trình, dự án"), text("tien_do"), h("2. Những thuận lợi"),
				text("thuan_loi"), h("3. Những khó khăn"), text("kho_khan"), h("4. Nguyên nhân khách quan và chủ quan"), text("nguyen_nhan")),
			sec("phuong_huong", "III. PHƯƠNG HƯỚNG, NHIỆM VỤ {ky_toi}", text("phuong_huong")),
			sec("de_xuat", "IV. ĐỀ XUẤT, KIẾN NGHỊ", text("de_xuat")))
	}
	body = append(body, tail...)
	key, title := BM11WordPeriodKey, "Báo cáo phòng CNTS tháng, quý, năm (BM-11)"
	if week {
		key, title = BM11WordWeekKey, "Báo cáo phòng CNTS tuần (BM-11)"
	}
	return doctpl.Template{Key: key, Version: BM11WordVersion, Owner: BM11WordOwner, Title: title, Vars: vars, Lists: lists, Body: body}
}

func appendVar(vs []doctpl.Var, v doctpl.Var) []doctpl.Var {
	for _, x := range vs {
		if x.Key == v.Key {
			return vs
		}
	}
	return append(vs, v)
}

// BM11WordTemplates là mẫu Word BM-11 (đăng ký vào pf/export ở gốc lắp ráp qua extension.ExportTemplates).
func BM11WordTemplates() []doctpl.Template {
	return []doctpl.Template{bm11Template(true), bm11Template(false)}
}

var bm11StatusVI = map[string]string{domain.BM11Draft: "Đang soạn (bản nháp)", domain.BM11Pending: "Chờ duyệt (bản nháp)",
	domain.BM11Returned: "Trả lại (bản nháp)", domain.BM11Sent: "Đã duyệt, đã gửi"}

// viNum định dạng số thập phân kiểu Việt Nam (1.234,5) từ chuỗi chính xác (không qua số thực).
func viNum(s string) string {
	d, err := decimal.NewFromString(orZero(s))
	if err != nil {
		return s
	}
	neg := d.IsNegative()
	str := d.Abs().String()
	ip, fp, _ := strings.Cut(str, ".")
	var b strings.Builder
	for i, r := range ip {
		if i > 0 && (len(ip)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	out := b.String()
	if fp != "" {
		out += "," + fp
	}
	if neg {
		out = "-" + out
	}
	return out
}

// BM11WordData dựng dữ liệu mẫu từ nội dung phiên bản hiện hành.
func BM11WordData(r store.BM11Report, c domain.BM11Content, status, provinceName, hash string, managerNames map[string]string) doctpl.Data {
	d := doctpl.Data{Vars: map[string]string{
		bm11VarNumber: "", bm11VarPlaceDate: "", "tinh": strings.ToUpper(provinceName), "tieu_de_ky": r.Label,
		"tong_hop_xa": BM11CommunesText(c.Coverage), "trang_thai": bm11StatusVI[status],
		"khai_thac": viNum(c.Fishery.CatchCur), "khai_thac_luy_ke": viNum(c.Fishery.CatchYTD), "nuoi_trong": viNum(c.Fishery.AquaCur),
		"nuoi_trong_luy_ke": viNum(c.Fishery.AquaYTD), "tong_san_luong": viNum(c.Fishery.TotalCur), "dau_du_lieu": hash,
	}, Lists: map[string][]doctpl.Item{bm11ListLivestock: {}, bm11ListPrograms: {}, bm11ListRecipients: {}, bm11ListMissing: {}, bm11ListSupplement: {}}}
	switch r.Type {
	case domain.BM11Month:
		d.Vars["ky_toi"] = "THÁNG TỚI"
	case domain.BM11Quarter:
		d.Vars["ky_toi"] = "QUÝ TỚI"
	case domain.BM11Year:
		d.Vars["ky_toi"] = "NĂM TỚI"
	}
	for _, s := range domain.BM11SectionsOf(r.Type) {
		d.Vars["muc_"+s.Key] = c.Sections[s.Key]
	}
	for i, code := range r.Recipients {
		if i < 3 {
			d.Vars["noi_nhan_"+strconv.Itoa(i+1)] = "- " + domain.BM11RecipientLabel(code) + ";"
		}
	}
	for _, g := range domain.BM11Groups {
		d.Lists[bm11ListLivestock] = append(d.Lists[bm11ListLivestock], doctpl.Item{"stt": "", "noi_dung": "* " + g.Label, "so_luong": "", "nhom": "1"})
		n := 0
		for _, l := range domain.BM11Lines {
			if l.Group != g.Key {
				continue
			}
			stt, label := "", "- "+l.Label
			if !l.Sub {
				n++
				stt, label = strconv.Itoa(n), l.Label
			}
			d.Lists[bm11ListLivestock] = append(d.Lists[bm11ListLivestock], doctpl.Item{"stt": stt, "noi_dung": label,
				"so_luong": viNum(c.Livestock[l.Key]) + " " + l.Unit, "nhom": ""})
		}
	}
	for _, p := range c.Programs {
		auto := fmt.Sprintf("Đã triển khai %d điểm trình diễn, %d hộ tham gia.", p.Sites, p.Households)
		if m := managerNames[p.ManagerRef]; m != "" {
			auto += " Chủ nhiệm: " + m + "."
		}
		d.Lists[bm11ListPrograms] = append(d.Lists[bm11ListPrograms], doctpl.Item{"ten": p.Name, "tu_dong": auto, "noi_dung": p.Text})
	}
	for _, cv := range c.Coverage {
		if len(cv.MissingNames) > 0 {
			d.Lists[bm11ListMissing] = append(d.Lists[bm11ListMissing], doctpl.Item{"bieu": cv.Form, "xa": strings.Join(cv.MissingNames, ", ")})
		}
	}
	for _, x := range c.Supplement {
		d.Lists[bm11ListSupplement] = append(d.Lists[bm11ListSupplement], doctpl.Item{"ky": x.Period, "chi_tieu": x.Label,
			"da_gui": viNum(x.Sent), "hien_tai": viNum(x.Now), "dvt": x.Unit})
	}
	return d
}

func (s *Service) bm11WordReady(ctx context.Context, id uuid.UUID) (bm11Who, store.BM11Report, []store.BM11Event, error) {
	w, err := s.bm11Who(ctx)
	if err != nil {
		return w, store.BM11Report{}, nil, err
	}
	if s.Exporter == nil {
		return w, store.BM11Report{}, nil, ErrDependency.WithDetail("chưa nối pf/export")
	}
	r, evs, err := s.bm11Load(ctx, w, id) // giao dịch KN đóng tại đây
	return w, r, evs, err
}

// ExportBM11Word xuất Word báo cáo (người xem được báo cáo) qua pf/export.
func (s *Service) ExportBM11Word(ctx context.Context, id uuid.UUID, idem string) (exportport.Run, error) {
	if idem == "" {
		return exportport.Run{}, problem.IdempotencyRequired
	}
	_, r, evs, err := s.bm11WordReady(ctx, id)
	if err != nil {
		return exportport.Run{}, err
	}
	c, cev, ok := lastContent(evs)
	if !ok {
		return exportport.Run{}, ErrBM11NotFound
	}
	status := evs[len(evs)-1].Status
	refs := []uuid.UUID{}
	for _, p := range c.Programs {
		if u, err := uuid.Parse(p.ManagerRef); err == nil {
			refs = append(refs, u)
		}
	}
	nm := s.names(ctx, refs)
	mn := map[string]string{}
	for k, v := range nm {
		mn[k.String()] = v
	}
	prov := s.provinceName(ctx, r.Province)
	place := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(prov, "Tỉnh "), "Thành phố "))
	data := BM11WordData(r, c, status, place, cev.ContentHash, mn)
	key := BM11WordPeriodKey
	if r.Type == domain.BM11Week {
		key = BM11WordWeekKey
	}
	req := exportport.Request{Template: key, Version: BM11WordVersion, Subject: BM11WordSubject(id),
		FileName: fmt.Sprintf("bao-cao-phong-cnts-%s-%s-v%d.docx", r.Type, r.PeriodKey, cev.Seq), Data: data,
		Date: &exportport.DateLine{Var: bm11VarPlaceDate, Place: or(place, "……")}, IdemKey: idem}
	if status == domain.BM11Sent {
		req.Number = &exportport.Numbering{Series: BM11WordSeries, Var: bm11VarNumber}
	} else {
		data.Vars[bm11VarNumber] = "…/" + BM11WordSeries
	}
	return s.Exporter.Export(ctx, req)
}

func or(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// BM11WordHistory trả các lần xuất Word của báo cáo (mới nhất trước).
func (s *Service) BM11WordHistory(ctx context.Context, id uuid.UUID) ([]exportport.Run, error) {
	if _, _, _, err := s.bm11WordReady(ctx, id); err != nil {
		return nil, err
	}
	return s.Exporter.Runs(ctx, BM11WordSubject(id))
}

// BM11WordDownload trả byte Word đã lưu (pf/export kiểm băm).
func (s *Service) BM11WordDownload(ctx context.Context, id, run uuid.UUID) (exportport.Run, error) {
	if _, _, _, err := s.bm11WordReady(ctx, id); err != nil {
		return exportport.Run{}, err
	}
	return s.Exporter.Run(ctx, BM11WordSubject(id), run)
}
