package app

// FN-19 B4, AC-01: "Xuất Word" biên bản/báo cáo đúng mẫu TTKN Quốc gia qua pf/export (`cap/export`, #17): pf/export ghi sổ
// phiên bản mẫu, dựng .docx tất định và ghi sổ lần xuất (byte + băm); Mẫu 03 (báo cáo) nhận số "{n}/BC-TTKN" (một số mỗi
// báo cáo, cấp trong giao dịch của pf/export) và dòng "<tỉnh>, ngày …". Mẫu dựng từ đặc tả (domain.Spec): quốc hiệu, tiêu
// đề, từng mục với mọi ô và bảng của mẫu Word gốc (ô trống in "……" như mẫu giấy), khối ký.
//
// Đọc biểu trong giao dịch KN rồi ĐÓNG giao dịch; tên xã, tỉnh và pf/export gọi NGOÀI giao dịch (P0-POOL-KN, #156).

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsknqg/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsknqg/store"
	exportport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/doctpl"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Mẫu Word.
const (
	WordOwner    = "pf/extension"
	WordVersion  = 1
	WordSeries   = "BC-TTKN"
	VarNumber    = "so_van_ban"
	VarPlaceDate = "dia_danh_ngay"
	placeholder  = "……………"
	tableWidth   = 9300 // twip — khổ A4 dọc trừ lề
)

// WordTemplateKey là khóa mẫu Word của mẫu form ("cnts.knqg_05").
func WordTemplateKey(form string) string { return "cnts.knqg_" + form }

// WordSubject gom lịch sử lần xuất và số văn bản trong pf/export: một biên bản/báo cáo.
func WordSubject(ref uuid.UUID) string { return "cnts_knqg:" + ref.String() }

func fieldVar(k string) string { return "f_" + k }
func listVar(k string) string  { return "t_" + k }

// wordColumns là cột in ra Word: bỏ mã dòng, cột tự điền (trừ bảng tự dựng), cột "Đánh giá" (gộp vào ghi chú).
func wordColumns(t domain.Table) []domain.Column {
	var out []domain.Column
	for _, c := range t.Columns {
		if c.Key == "ma" || c.Key == "dat" || (c.Auto && !t.Auto) {
			continue
		}
		out = append(out, c)
	}
	return out
}

type header struct {
	title []string // dòng tiêu đề (đậm, giữa)
	intro []string // khuôn câu mở đầu (sau thành phần/căn cứ — đặt ở đầu)
	close []string // khuôn câu kết
	left  []doctpl.Line
	right []doctpl.Line
	extra []doctpl.Block // khối ký phụ (sau khối ký)
}

func formHeader(form string) header {
	sign := func(s ...string) []doctpl.Line {
		out := make([]doctpl.Line, 0, len(s)+3)
		for i, x := range s {
			out = append(out, doctpl.Line{Text: x, Bold: i == 0, Italic: i > 0})
		}
		return append(out, doctpl.Line{Text: ""}, doctpl.Line{Text: ""}, doctpl.Line{Text: ""})
	}
	switch form {
	case domain.Form01:
		return header{title: []string{"BÁO CÁO TIẾN ĐỘ THỰC HIỆN DỰ ÁN KHUYẾN NÔNG TRUNG ƯƠNG", "{ky_bao_cao}"},
			close: []string{"Trên đây là báo cáo tiến độ thực hiện dự án {ky_bao_cao}./."},
			left:  sign("CHỦ NHIỆM DỰ ÁN", "(Ký, ghi rõ họ tên)"), right: sign("THỦ TRƯỞNG ĐƠN VỊ", "(Ký tên, đóng dấu)")}
	case domain.Form03:
		return header{title: []string{"BÁO CÁO", "Kết quả kiểm tra, đánh giá dự án khuyến nông trung ương"},
			intro: []string{"Trung tâm Khuyến nông đã tổ chức kiểm tra, đánh giá dự án khuyến nông trung ương, kết quả như sau:"},
			left:  []doctpl.Line{{Text: "Nơi nhận:", Bold: true, Italic: true}, {Text: "{f_noi_nhan}"}},
			right: sign("THỦ TRƯỞNG ĐƠN VỊ/TRƯỞNG ĐOÀN KIỂM TRA", "(ký, ghi rõ họ tên, đóng dấu/xác nhận)")}
	case domain.Form04:
		return header{title: []string{"BẢN NGHIỆM THU HIỆN TRƯỜNG", "LỚP TẬP HUẤN NHÂN RỘNG MÔ HÌNH"},
			intro: []string{"Căn cứ kết quả tổ chức thực hiện, chúng tôi thống nhất nghiệm thu và đánh giá kết quả lớp tập huấn như sau:"},
			close: []string{"Bản nghiệm thu được lập thành {f_so_ban} bản có giá trị như nhau, mỗi bên giữ 01 bản trong đó có 01 bản lưu hồ sơ thanh quyết toán với Trung tâm KNQG."},
			left:  sign("ĐẠI DIỆN CƠ QUAN QUẢN LÝ NN/KN", "(ký, họ tên và đóng dấu/xác nhận)"),
			right: sign("ĐƠN VỊ TỔ CHỨC TẬP HUẤN", "(ký, họ tên và đóng dấu)"),
			extra: []doctpl.Block{{Kind: doctpl.KindParagraph, Text: "ĐẠI DIỆN HỌC VIÊN\n(ký, họ tên)", Align: doctpl.AlignCenter, Bold: true}}}
	case domain.Form05:
		return header{title: []string{"BẢN NGHIỆM THU", "KẾT QUẢ THỰC HIỆN MÔ HÌNH KHUYẾN NÔNG", "(Áp dụng cho điểm trình diễn do các đơn vị thực hiện)"},
			intro: []string{"Sau khi nghe báo cáo và kiểm tra thực tế tại điểm trình diễn, chúng tôi thống nhất nghiệm thu, đánh giá kết quả thực hiện của điểm trình diễn như sau:"},
			close: []string{"Bản nghiệm thu được lập thành 02 bản có giá trị như nhau, lưu hồ sơ dự án và hồ sơ quyết toán./."},
			left:  sign("ĐƠN VỊ THỰC HIỆN/TRIỂN KHAI", "(Ký tên, đóng dấu)"),
			right: sign("CƠ QUAN QUẢN LÝ NGÀNH NN VÀ MT/KN", "(Ký tên, đóng dấu/xác nhận)"),
			extra: []doctpl.Block{{Kind: doctpl.KindParagraph, Text: "CHÍNH QUYỀN ĐỊA PHƯƠNG NƠI XÂY DỰNG MH\n(Ký tên, đóng dấu/xác nhận)", Align: doctpl.AlignCenter, Bold: true},
				{Kind: doctpl.KindParagraph, Text: "Đại diện các hộ tham gia (ký, ghi rõ họ tên)", Italic: true}}}
	default: // 07
		return header{title: []string{"BẢN NGHIỆM THU", "Kết quả thực hiện dự án khuyến nông hàng năm", "{ky_bao_cao}"},
			intro: []string{"Căn cứ vào các biên bản nghiệm thu kết quả thực hiện tại điểm trình diễn mô hình, báo cáo kết quả thực hiện dự án và kiểm tra chứng từ của bên B, chúng tôi thống nhất nghiệm thu và đánh giá kết quả, hiệu quả của mô hình khuyến nông trung ương do bên B thực hiện như sau:"},
			close: []string{"Bản nghiệm thu được lập thành {f_so_ban} bản có giá trị như nhau trong đó bên A giữ 02 bản."},
			left:  sign("ĐẠI DIỆN BÊN B", "Chủ nhiệm DA — Thủ trưởng đơn vị", "(Ký, họ tên — Ký tên, đóng dấu)"),
			right: sign("ĐẠI DIỆN BÊN A", "Phòng CM — Lãnh đạo TT", "(Ký, họ tên — Ký tên, đóng dấu)")}
	}
}

// WordTemplate dựng mẫu Word của một mẫu KNQG từ đặc tả (đổi nội dung → tăng WordVersion).
func WordTemplate(spec domain.Spec) doctpl.Template {
	h := formHeader(spec.Form)
	vars := []doctpl.Var{{Key: VarNumber, Label: "Số văn bản"}, {Key: VarPlaceDate, Label: "Địa danh, ngày"},
		{Key: "ky_bao_cao", Label: "Kỳ báo cáo"}, {Key: "ban_dieu_chinh", Label: "Bản điều chỉnh"}, {Key: "trang_thai", Label: "Trạng thái"}}
	for _, f := range spec.Fields {
		vars = append(vars, doctpl.Var{Key: fieldVar(f.Key), Label: f.Label})
	}
	var lists []doctpl.List
	for _, t := range spec.Tables {
		fs := []string{"stt"}
		for _, c := range wordColumns(t) {
			fs = append(fs, c.Key)
		}
		lists = append(lists, doctpl.List{Key: listVar(t.Key), Label: t.Label, Fields: fs})
	}
	para := func(text string) doctpl.Block {
		return doctpl.Block{Kind: doctpl.KindParagraph, Text: text, Indent: true, Align: doctpl.AlignJustify}
	}
	center := func(text string, bold bool) doctpl.Block {
		return doctpl.Block{Kind: doctpl.KindParagraph, Text: text, Align: doctpl.AlignCenter, Bold: bold}
	}
	var body []doctpl.Block
	if spec.Numbered {
		body = append(body, doctpl.Block{Kind: doctpl.KindColumns, Split: 3900,
			Left: []doctpl.Line{{Text: "SỞ NÔNG NGHIỆP VÀ MÔI TRƯỜNG"}, {Text: "TRUNG TÂM KHUYẾN NÔNG", Bold: true}, {Text: "Số: {so_van_ban}"}},
			Right: []doctpl.Line{{Text: "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", Bold: true}, {Text: "Độc lập - Tự do - Hạnh phúc", Bold: true},
				{Text: "{dia_danh_ngay}", Italic: true}}})
	} else {
		body = append(body, center("CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", true), center("Độc lập – Tự do – Hạnh phúc", true),
			doctpl.Block{Kind: doctpl.KindParagraph, Text: spec.Code, Align: doctpl.AlignRight, Bold: true})
	}
	for i, t := range h.title {
		body = append(body, center(t, i < 2))
	}
	body = append(body, doctpl.Block{Kind: doctpl.KindParagraph, Text: "({ban_dieu_chinh})", Align: doctpl.AlignCenter, Italic: true, If: "ban_dieu_chinh"})
	fieldsOf := func(keys []string) []doctpl.Block {
		var out []doctpl.Block
		for _, k := range keys {
			f, _ := spec.FieldByKey(k)
			if f.Kind == domain.KindLong {
				out = append(out, para("- "+f.Label+":"), para("{"+fieldVar(k)+"}"))
				continue
			}
			out = append(out, para(f.Label+": {"+fieldVar(k)+"}"))
		}
		return out
	}
	tableOf := func(k string) []doctpl.Block {
		t, _ := spec.TableByKey(k)
		cs := wordColumns(t)
		w := (tableWidth - 600) / len(cs)
		cols := []doctpl.Column{{Header: "TT", Value: "{" + listVar(k) + ".stt}", Width: 600, Align: doctpl.AlignCenter}}
		for _, c := range cs {
			al := doctpl.AlignLeft
			if c.Kind == domain.KindNumber || c.Kind == domain.KindInt {
				al = doctpl.AlignRight
			}
			cols = append(cols, doctpl.Column{Header: c.Label, Value: "{" + listVar(k) + "." + c.Key + "}", Width: w, Align: al})
		}
		return []doctpl.Block{{Kind: doctpl.KindHeading, Text: t.Label},
			{Kind: doctpl.KindTable, Each: listVar(k), Columns: cols, EmptyText: "(không có)"}}
	}
	introDone := false
	for i, sec := range spec.Sections {
		if strings.HasPrefix(sec.Title, "Phụ lục") {
			body = append(body, doctpl.Block{Kind: doctpl.KindPageBreak})
			for _, k := range sec.Tables {
				t, _ := spec.TableByKey(k)
				name, sub, _ := strings.Cut(t.Label, " — ")
				body = append(body, center(strings.ToUpper(name), true), center(strings.ToUpper(sub), true),
					center("(Kèm theo Biên bản nghiệm thu kết quả xây dựng mô hình trình diễn)", false),
					para("Tên dự án: {f_ten_mo_hinh}"))
				body = append(body, tableOf(k)[1:]...)
			}
			continue
		}
		blocks := fieldsOf(sec.Fields)
		for _, k := range sec.Tables {
			blocks = append(blocks, tableOf(k)...)
		}
		body = append(body, doctpl.Block{Kind: doctpl.KindSection, Key: "muc_" + strconv.Itoa(i), Text: sec.Title, Blocks: blocks})
		// Câu dẫn sau mục căn cứ/thành phần đầu tiên (như mẫu giấy).
		if !introDone && i == 0 {
			for _, x := range h.intro {
				body = append(body, para(x))
			}
			introDone = true
		}
	}
	for _, x := range h.close {
		body = append(body, para(x))
	}
	body = append(body, doctpl.Block{Kind: doctpl.KindColumns, Left: h.left, Right: h.right})
	body = append(body, h.extra...)
	return doctpl.Template{Key: WordTemplateKey(spec.Form), Version: WordVersion, Owner: WordOwner,
		Title: spec.Code + " — " + spec.Title + " (" + spec.BM + ")", Vars: vars, Lists: lists, Body: body}
}

// WordTemplates là mẫu Word của 5 mẫu KNQG (đăng ký vào pf/export ở gốc lắp ráp qua extension.ExportTemplates).
func WordTemplates() []doctpl.Template {
	out := make([]doctpl.Template, 0, len(domain.Forms))
	for _, s := range domain.Specs() {
		out = append(out, WordTemplate(s))
	}
	return out
}

var statusVI = map[string]string{domain.StatusDraft: "Đang lập", domain.StatusSubmitted: "Chờ duyệt", domain.StatusApproved: "Đã duyệt",
	domain.StatusReturned: "Trả lại", domain.StatusAwaitingSign: "Chờ ký", domain.StatusSigned: "Đã ký", domain.StatusSent: "Đã gửi Quốc gia",
	domain.StatusConfirmed: "Đã xác nhận"}

// StatusLabel trả nhãn trạng thái.
func StatusLabel(s string) string { return statusVI[s] }

func optionLabel(opts []domain.Option, v string) string {
	for _, o := range opts {
		if o.Value == v {
			return o.Label
		}
	}
	return v
}

func dateVN(v string) string {
	if d, err := time.Parse(time.DateOnly, v); err == nil {
		return d.Format("02/01/2006")
	}
	return v
}

// groupThousands định dạng số "1234567.5" → "1.234.567,5".
func groupThousands(v string) string {
	intPart, frac, hasFrac := strings.Cut(v, ".")
	var b strings.Builder
	for i, r := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	if hasFrac {
		b.WriteString("," + frac)
	}
	return b.String()
}

func formatValue(kind string, opts []domain.Option, v string) string {
	switch {
	case v == "":
		return ""
	case kind == domain.KindDate:
		return dateVN(v)
	case kind == domain.KindNumber || kind == domain.KindInt:
		return groupThousands(v)
	case kind == domain.KindChoice:
		return optionLabel(opts, v)
	}
	return v
}

// WordData dựng dữ liệu mẫu từ bản hiện hành của biểu.
func WordData(spec domain.Spec, d store.Doc) doctpl.Data {
	data := doctpl.Data{Vars: map[string]string{VarNumber: "", VarPlaceDate: "", "ky_bao_cao": "", "ban_dieu_chinh": "",
		"trang_thai": statusVI[d.Status]}, Lists: map[string][]doctpl.Item{}}
	if l := domain.PeriodLabel(d.Period); l != "" {
		data.Vars["ky_bao_cao"] = strings.ToUpper(l[:1]) + l[1:]
	}
	if d.Revision > 1 {
		data.Vars["ban_dieu_chinh"] = "Bản điều chỉnh lần " + strconv.Itoa(d.Revision-1)
	}
	for _, f := range spec.Fields {
		v := formatValue(f.Kind, f.Options, d.Content.Fields[f.Key])
		if v == "" {
			v = placeholder
		}
		data.Vars[fieldVar(f.Key)] = v
	}
	for _, t := range spec.Tables {
		items := []doctpl.Item{}
		for i, r := range d.Content.Tables[t.Key] {
			it := doctpl.Item{"stt": strconv.Itoa(i + 1)}
			for _, c := range wordColumns(t) {
				it[c.Key] = formatValue(c.Kind, c.Options, r[c.Key])
			}
			if r["dat"] != "" {
				if _, ok := it["ghi_chu"]; ok {
					it["ghi_chu"] = strings.TrimSuffix(optionLabel(yesNoOpts, r["dat"])+"; "+it["ghi_chu"], "; ")
				}
			}
			items = append(items, it)
		}
		data.Lists[listVar(t.Key)] = items
	}
	return data
}

var yesNoOpts = []domain.Option{{Value: "co", Label: "Đạt"}, {Value: "khong", Label: "Không đạt"}}

// wordDoc đọc biểu và kiểm quyền xem (giao dịch KN đóng khi trả về).
func (s *Service) wordDoc(ctx context.Context, c caller, ref uuid.UUID) (store.Doc, *pctx, error) {
	var cur *store.Doc
	var pc *pctx
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if cur, e = store.Latest(ctx, tx, ref); e != nil || cur == nil {
			return e
		}
		pc, e = loadProject(ctx, tx, cur.ProjectRef)
		return e
	}); err != nil {
		return store.Doc{}, nil, err
	}
	if cur == nil || pc == nil || !c.seesDoc(pc.p, *cur, pc.communeOf(*cur)) {
		return store.Doc{}, nil, ErrDocNotFound
	}
	return *cur, pc, nil
}

// ExportWord xuất Word đúng mẫu của biểu (người xem được biểu) qua pf/export.
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
	d, pc, err := s.wordDoc(ctx, c, ref)
	if err != nil {
		return exportport.Run{}, err
	}
	spec, _ := domain.Lookup(d.Form)
	req := exportport.Request{Template: WordTemplateKey(d.Form), Version: WordVersion, Subject: WordSubject(ref),
		FileName: fmt.Sprintf("mau-%s-%s-r%d-v%d.docx", d.Form, ref.String(), d.Revision, d.Seq), Data: WordData(spec, d), IdemKey: idem}
	if spec.Numbered {
		place := ""
		if pv := s.unitName(ctx, pc.p.Province); pv != "" && pv != pc.p.Province {
			place = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(pv, "Tỉnh "), "Thành phố "))
		}
		if place == "" {
			place = "……"
		}
		req.Number = &exportport.Numbering{Series: WordSeries, Var: VarNumber}
		req.Date = &exportport.DateLine{Var: VarPlaceDate, Place: place}
	}
	return s.Exporter.Export(ctx, req)
}

// WordHistory trả các lần xuất Word của biểu (mới nhất trước).
func (s *Service) WordHistory(ctx context.Context, ref uuid.UUID) ([]exportport.Run, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	if s.Exporter == nil {
		return nil, ErrDependency.WithDetail("chưa nối pf/export")
	}
	if _, _, err := s.wordDoc(ctx, c, ref); err != nil {
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
	if _, _, err := s.wordDoc(ctx, c, ref); err != nil {
		return exportport.Run{}, err
	}
	return s.Exporter.Run(ctx, WordSubject(ref), run)
}
