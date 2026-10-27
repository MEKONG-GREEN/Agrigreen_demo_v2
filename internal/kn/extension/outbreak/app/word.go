package app

// BF-CN-OUTBREAK (PRD UP-08): "Xuất Word theo mẫu" Báo cáo tình hình dịch bệnh BM-10 qua pf/export (`cap/export`, #17):
// pf/export ghi sổ phiên bản mẫu, cấp số văn bản "{n}/BC-TTKN" (một số cho mỗi ổ dịch, dùng lại ở lần xuất sau; cấp
// trong giao dịch của pf/export nên không trùng khi xuất đồng thời), điền dòng "<tỉnh>, ngày …", dựng .docx tất định
// và ghi sổ lần xuất. Bố cục theo báo cáo mẫu "BC tình hình bệnh dịch tả heo CP xã Hòa Thuận" (Phòng CNTS):
// quốc hiệu – số – trích yếu; 1. Tình hình dịch bệnh, xử lý ổ dịch, diễn biến, vùng đệm; 2. Phòng chống, kiến nghị.
//
// Đọc ổ dịch trong giao dịch KN rồi ĐÓNG giao dịch; mọi lời gọi module khác (đơn vị hành chính, tên danh mục,
// pf/export) chạy NGOÀI giao dịch KN (P0-POOL-KN).

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/outbreak/domain"
	exportport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/doctpl"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Mẫu Word BM-10.
const (
	WordOwner       = "pf/extension"
	WordTemplateKey = "cnts.bm10"
	WordVersion     = 1
	WordSeries      = "BC-TTKN"
	VarNumber       = "so_van_ban"
	VarPlaceDate    = "dia_danh_ngay"
	listProgress    = "dien_bien"
)

// WordSubject là đối tượng của lịch sử Word và số văn bản trong pf/export: một ổ dịch nhận một số.
func WordSubject(ref uuid.UUID) string { return "cnts_outbreak:" + ref.String() }

// WordTemplate là mẫu Word BM-10 (đổi nội dung → tăng WordVersion).
func WordTemplate() doctpl.Template {
	v := func(key, label string, req bool) doctpl.Var { return doctpl.Var{Key: key, Label: label, Required: req} }
	p := func(text string) doctpl.Block {
		return doctpl.Block{Kind: doctpl.KindParagraph, Text: text, Indent: true, Align: doctpl.AlignJustify}
	}
	pIf := func(cond, text string) doctpl.Block { b := p(text); b.If = cond; return b }
	h := func(text string) doctpl.Block { return doctpl.Block{Kind: doctpl.KindHeading, Text: text} }
	return doctpl.Template{Key: WordTemplateKey, Version: WordVersion, Owner: WordOwner, Title: "Báo cáo tình hình dịch bệnh (BM-10)",
		Vars: []doctpl.Var{
			v(VarNumber, "Số văn bản", false), v(VarPlaceDate, "Địa danh, ngày", true), v("benh", "Bệnh (DB-01)", true),
			v("xa", "Xã/phường", true), v("tiep_nhan", "Thời điểm tiếp nhận (DB-02)", true), v("nguon_tin", "Nguồn tin (DB-03)", true),
			v("dia_ban", "Hộ chăn nuôi, địa chỉ (DB-04)", true), v("vi_tri", "Vị trí ổ dịch (DB-05)", true),
			v("vat_nuoi", "Vật nuôi", true), v("quy_mo", "Quy mô đàn (DB-06)", true), v("ngay_tuoi", "Ngày tuổi", false),
			v("so_chet", "Số con chết (DB-07)", true), v("trieu_chung", "Triệu chứng (DB-08)", true),
			v("xet_nghiem", "Lấy mẫu, xét nghiệm (DB-09)", false), v("tieu_huy", "Tiêu hủy (DB-10)", true),
			v("hoa_chat", "Hóa chất (DB-10)", true), v("vung_dem", "Vùng đệm, vùng bị uy hiếp (DB-11)", false),
			v("phong_chong", "Phòng chống, kiến nghị (DB-12)", false), v("trang_thai", "Trạng thái", true),
			v("dien_bien_den", "Diễn biến đến ngày", true), v("phien_ban", "Phiên bản", true),
		},
		Lists: []doctpl.List{{Key: listProgress, Label: "Diễn biến", Fields: []string{"ngay", "su_viec", "so_chet", "tieu_huy", "ghi_chu"}}},
		Body: []doctpl.Block{
			{Kind: doctpl.KindColumns, Split: 3900,
				Left: []doctpl.Line{{Text: "SỞ NÔNG NGHIỆP VÀ MÔI TRƯỜNG"}, {Text: "TRUNG TÂM KHUYẾN NÔNG", Bold: true}, {Text: "Số: {so_van_ban}"}},
				Right: []doctpl.Line{{Text: "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", Bold: true}, {Text: "Độc lập - Tự do - Hạnh phúc", Bold: true},
					{Text: "{dia_danh_ngay}", Italic: true}}},
			{Kind: doctpl.KindParagraph, Text: "BÁO CÁO", Align: doctpl.AlignCenter, Bold: true},
			{Kind: doctpl.KindParagraph, Text: "Tình hình bệnh {benh}\ntrên địa bàn {xa}", Align: doctpl.AlignCenter, Bold: true},
			{Kind: doctpl.KindParagraph, Text: "Trạng thái ổ dịch: {trang_thai} — số liệu đến ngày {dien_bien_den} (phiên bản {phien_ban}).",
				Align: doctpl.AlignCenter, Italic: true},
			{Kind: doctpl.KindSection, Key: "tinh_hinh", Text: "1. Tình hình dịch bệnh, xử lý ổ dịch, nhận định tình hình", Blocks: []doctpl.Block{
				h("1.1 Tình hình dịch bệnh"),
				p("Nguồn tin: {nguon_tin}. Thời điểm tiếp nhận thông tin bệnh: {tiep_nhan}."),
				p("Hộ chăn nuôi, địa chỉ: {dia_ban}. Vị trí ổ dịch: {vi_tri}."),
				p("Quy mô đàn {vat_nuoi} tại thời điểm phát hiện: {quy_mo} con{ngay_tuoi}."),
				p("Số con chết: {so_chet} con."),
				p("Triệu chứng: {trieu_chung}."),
				pIf("xet_nghiem", "Lấy mẫu, xét nghiệm: {xet_nghiem}."),
				h("1.2 Xử lý ổ dịch"),
				p("- Tiêu hủy: {tieu_huy}."),
				p("- Hóa chất tiêu độc, khử trùng: {hoa_chat} lít."),
				h("1.3 Diễn biến"),
				{Kind: doctpl.KindTable, Each: listProgress, EmptyText: "Chưa có diễn biến.", Columns: []doctpl.Column{
					{Header: "Ngày", Value: "{dien_bien.ngay}", Width: 1300},
					{Header: "Sự việc", Value: "{dien_bien.su_viec}", Width: 1900},
					{Header: "Số chết", Value: "{dien_bien.so_chet}", Width: 1000, Align: doctpl.AlignRight},
					{Header: "Tiêu hủy", Value: "{dien_bien.tieu_huy}", Width: 1000, Align: doctpl.AlignRight},
					{Header: "Ghi chú", Value: "{dien_bien.ghi_chu}", Width: 3800},
				}},
				{Kind: doctpl.KindSection, Key: "vung_dem", Text: "1.4 Vùng đệm, vùng bị uy hiếp", If: "vung_dem", Blocks: []doctpl.Block{p("{vung_dem}")}},
			}},
			{Kind: doctpl.KindSection, Key: "phong_chong", Text: "2. Công tác phòng, chống và kiến nghị", Blocks: []doctpl.Block{
				pIf("phong_chong", "{phong_chong}"),
			}},
			p("Trên đây là báo cáo tình hình bệnh {benh} trên địa bàn {xa}./."),
			{Kind: doctpl.KindColumns, Split: 4400,
				Left: []doctpl.Line{{Text: "Nơi nhận:", Bold: true, Italic: true}, {Text: "- Sở Nông nghiệp và Môi trường;"},
					{Text: "- Chi cục Chăn nuôi và Thú y;"}, {Text: "- UBND {xa};"}, {Text: "- Lưu: VT, CNTS."}},
				Right: []doctpl.Line{{Text: "GIÁM ĐỐC", Bold: true}, {Text: ""}, {Text: ""}, {Text: ""}}},
		},
	}
}

// WordTemplates là mẫu Word của gói dịch bệnh (đăng ký vào pf/export ở gốc lắp ráp qua extension.ExportTemplates).
func WordTemplates() []doctpl.Template { return []doctpl.Template{WordTemplate()} }

var sourceVI = map[string]string{"ubnd_xa": "UBND xã", "nong_ho": "nông hộ báo", "can_bo": "cán bộ phát hiện"}
var eventVI = map[string]string{domain.EventReported: "Gửi báo cáo", domain.EventUpdated: "Cập nhật diễn biến",
	domain.EventClosed: "Cấp Tỉnh kết thúc", domain.EventReopened: "Cấp Tỉnh mở lại"}
var statusVI = map[string]string{domain.StatusReported: "Chờ xác nhận", domain.StatusMonitoring: "Đang theo dõi", domain.StatusClosed: "Đã kết thúc"}

func or(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func i64(v *int64) string {
	if v == nil {
		return "0"
	}
	return strconv.FormatInt(*v, 10)
}

// WordData dựng dữ liệu mẫu từ lịch sử ổ dịch (mới nhất trước — phần tử đầu là bản hiện hành).
func WordData(vs []OutbreakView, commune string) doctpl.Data {
	o := vs[0]
	vi := "chưa có"
	if o.Location != nil {
		vi = o.Location.Lat.String() + ", " + o.Location.Lon.String()
		if o.GPSAccuracyM != nil {
			vi += " (±" + o.GPSAccuracyM.String() + " m)"
		}
	}
	age := ""
	if o.HerdAgeDays != nil {
		age = " (khoảng " + strconv.FormatInt(*o.HerdAgeDays, 10) + " ngày tuổi)"
	}
	var lab []string
	if o.SampleDate != nil {
		lab = append(lab, "ngày lấy mẫu "+o.SampleDate.Format("02/01/2006"))
	}
	if o.LabName != "" {
		lab = append(lab, "nơi xét nghiệm "+o.LabName)
	}
	if o.LabResult != "" {
		lab = append(lab, "kết quả "+o.LabResult)
	}
	culled := i64(o.CulledHead) + " con"
	if o.CulledKg != nil {
		culled += ", tổng trọng lượng " + o.CulledKg.String() + " kg"
	}
	chem := "0"
	if o.ChemicalLiters != nil {
		chem = o.ChemicalLiters.String()
	}
	d := doctpl.Data{Vars: map[string]string{
		VarNumber: "", VarPlaceDate: "", "benh": o.DiseaseName, "xa": commune,
		"tiep_nhan": o.ReceivedAt.In(clock.Zone()).Format("15:04 ngày 02/01/2006"), "nguon_tin": or(sourceVI[o.Source], o.Source),
		"dia_ban": or(o.Locality, "—"), "vi_tri": vi, "vat_nuoi": strings.ToLower(o.SpeciesName), "quy_mo": strconv.FormatInt(o.HerdSize, 10),
		"ngay_tuoi": age, "so_chet": strconv.FormatInt(o.Dead, 10), "trieu_chung": o.Symptoms, "xet_nghiem": strings.Join(lab, ", "),
		"tieu_huy": culled, "hoa_chat": chem, "vung_dem": o.BufferNote, "phong_chong": o.PreventionNote,
		"trang_thai": statusVI[o.Status], "dien_bien_den": o.AsOf.Format("02/01/2006"), "phien_ban": strconv.FormatInt(o.Seq, 10),
	}, Lists: map[string][]doctpl.Item{listProgress: {}}}
	// Diễn biến: cũ trước.
	for i := len(vs) - 1; i >= 0; i-- {
		x := vs[i]
		note := x.ProgressNote
		if x.StatusReason != nil && *x.StatusReason != "" {
			note = *x.StatusReason
		}
		d.Lists[listProgress] = append(d.Lists[listProgress], doctpl.Item{"ngay": x.AsOf.Format("02/01/2006"),
			"su_viec": or(eventVI[x.Event], x.Event), "so_chet": strconv.FormatInt(x.Dead, 10), "tieu_huy": i64(x.CulledHead), "ghi_chu": or(note, "—")})
	}
	return d
}

// ExportWord xuất Word BM-10 của ổ dịch (Cấp Xã trong xã phụ trách, Cấp Tỉnh, Admin) qua pf/export.
func (s *Service) ExportWord(ctx context.Context, ref uuid.UUID, idem string) (exportport.Run, error) {
	if idem == "" {
		return exportport.Run{}, problem.IdempotencyRequired
	}
	c, err := s.reader(ctx)
	if err != nil {
		return exportport.Run{}, err
	}
	if s.Exporter == nil {
		return exportport.Run{}, ErrDependency.WithDetail("chưa nối pf/export")
	}
	rs, err := s.history(ctx, c, ref) // giao dịch KN đóng tại đây
	if err != nil {
		return exportport.Run{}, err
	}
	vs := s.obViews(ctx, rs)
	commune, place := "xã "+rs[0].CommuneCode, ""
	if s.Units != nil {
		at := s.now()
		if u, err := s.Units.Get(ctx, rs[0].CommuneCode, at); err == nil {
			commune = u.Name
			if u.ParentCode != "" {
				if pv, err := s.Units.Get(ctx, u.ParentCode, at); err == nil {
					place = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(pv.Name, "Tỉnh "), "Thành phố "))
				}
			}
		}
	}
	return s.Exporter.Export(ctx, exportport.Request{Template: WordTemplateKey, Version: WordVersion, Subject: WordSubject(ref),
		FileName: fmt.Sprintf("bao-cao-dich-benh-%s-v%d.docx", ref.String(), rs[0].Seq), Data: WordData(vs, commune),
		Number: &exportport.Numbering{Series: WordSeries, Var: VarNumber}, Date: &exportport.DateLine{Var: VarPlaceDate, Place: or(place, "……")},
		IdemKey: idem})
}

// WordHistory trả các lần xuất Word của ổ dịch (mới nhất trước).
func (s *Service) WordHistory(ctx context.Context, ref uuid.UUID) ([]exportport.Run, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	if s.Exporter == nil {
		return nil, ErrDependency.WithDetail("chưa nối pf/export")
	}
	if _, err := s.history(ctx, c, ref); err != nil {
		return nil, err
	}
	return s.Exporter.Runs(ctx, WordSubject(ref))
}

// WordDownload trả byte Word đã lưu (pf/export kiểm băm).
func (s *Service) WordDownload(ctx context.Context, ref, run uuid.UUID) (exportport.Run, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return exportport.Run{}, err
	}
	if s.Exporter == nil {
		return exportport.Run{}, ErrDependency.WithDetail("chưa nối pf/export")
	}
	if _, err := s.history(ctx, c, ref); err != nil {
		return exportport.Run{}, err
	}
	return s.Exporter.Run(ctx, WordSubject(ref), run)
}
