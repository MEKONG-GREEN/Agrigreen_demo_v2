package domain

// Đặc tả 5 mẫu KNQG — đúng mục, bảng của mẫu Word gốc (Biểu mẫu dự án KNQG của phòng CNTS: 03 Mau BC ket qua kiem tra
// DA sửa đổi, 04 Mau BBNT tap huan MH DA sửa đổi, 05 Mau BB nghiem thu địa bàn DA sửa đổi, 07 Mau BB nghiem thu hang
// nam); Mẫu 01/TĐ-DA không có tệp gốc trong bộ biểu của phòng — mục theo BRD UR-TINH-20 (giả định, docs/kn/gia-dinh/W11.53.md).

func txt(key, label string) Field  { return Field{Key: key, Label: label, Kind: KindText} }
func long(key, label string) Field { return Field{Key: key, Label: label, Kind: KindLong} }
func req(f Field) Field            { f.Required = true; return f }
func auto(f Field) Field           { f.Auto = true; return f }
func hint(f Field, h string) Field { f.Hint = h; return f }
func kind(f Field, k string) Field { f.Kind = k; return f }

func col(key, label, kind string) Column { return Column{Key: key, Label: label, Kind: kind} }
func autoCol(c Column) Column            { c.Auto = true; return c }

var yesNo = []Option{{Value: "co", Label: "Đạt"}, {Value: "khong", Label: "Không đạt"}}
var haveOpts = []Option{{Value: "co", Label: "Có"}, {Value: "chua_du", Label: "Chưa đủ"}, {Value: "chua_co", Label: "Chưa có"}}

// people là bảng thành phần (họ tên, chức vụ/đơn vị).
func people(key, label, second string, max int) Table {
	return Table{Key: key, Label: label, MaxRows: max, Columns: []Column{col("ho_ten", "Ông/bà", KindText), col("chuc_vu", second, KindText)}}
}

// indicators là bảng chỉ tiêu mô hình (Mẫu 05: Yêu cầu HĐ/Thực tế; Mẫu 07: Theo HĐ/Thực tế).
func indicators(form string) Table {
	cols := []Column{col("ma", "Mã", KindText), col("chi_tieu", "Chỉ tiêu mô hình", KindText), col("dvt", "Đơn vị tính", KindText),
		col("yeu_cau", map[string]string{Form05: "Yêu cầu HĐ", Form07: "Theo HĐ"}[form], KindText)}
	if form == Form05 {
		cols = append(cols, autoCol(col("thuc_te_nhat_ky", "Thực tế từ nhật ký", KindText)))
	}
	cols = append(cols, col("thuc_te", "Thực hiện/thực tế đạt được", KindText))
	if form == Form05 {
		cols = append(cols, Column{Key: "dat", Label: "Đánh giá", Kind: KindChoice, Options: yesNo})
	}
	cols = append(cols, col("ghi_chu", "Ghi chú", KindText))
	return Table{Key: "chi_tieu", Label: map[string]string{Form05: "4. Kết quả thực hiện", Form07: "3. Kết quả chung mô hình (theo phụ lục HĐ đã ký)"}[form],
		MaxRows: 60, Columns: cols}
}

var specs = map[string]Spec{
	Form01: {
		Form: Form01, Code: "Mẫu 01/TĐ-DA", BM: "BM-12", Title: "Báo cáo tiến độ thực hiện dự án theo tháng", Lifecycle: LifecycleApprove,
		Period: "month", Unique: true,
		Fields: []Field{
			auto(txt("ten_du_an", "Tên dự án")), auto(txt("ma_du_an", "Mã dự án")), auto(txt("chu_nhiem", "Chủ nhiệm dự án")),
			txt("to_chuc_chu_tri", "Tổ chức chủ trì"), auto(txt("thoi_gian_du_an", "Thời gian thực hiện")), auto(txt("kinh_phi", "Kinh phí (đồng)")),
			long("can_cu", "Căn cứ (hợp đồng, thuyết minh)"),
			long("tuyen_truyen", "4. Thông tin tuyên truyền"), req(long("danh_gia", "5. Đánh giá chung")), long("kien_nghi", "6. Kiến nghị, đề xuất"),
		},
		Tables: []Table{
			{Key: "mo_hinh", Label: "1. Xây dựng mô hình", MaxRows: 100, Hint: "Một dòng mỗi điểm trình diễn",
				Columns: []Column{col("dia_diem", "Điểm trình diễn, địa điểm", KindText), col("quy_mo_ke_hoach", "Quy mô kế hoạch", KindText),
					col("quy_mo_thuc_hien", "Quy mô thực hiện", KindText), col("so_ho_ke_hoach", "Số hộ kế hoạch", KindInt),
					col("so_ho_thuc_hien", "Số hộ thực hiện", KindInt), col("ket_qua", "Kết quả đến kỳ báo cáo", KindText)}},
			{Key: "vat_tu", Label: "2. Cung ứng giống, thiết bị, vật tư", MaxRows: 100, Hint: "Đã cấp vượt kế hoạch → cảnh báo",
				Columns: []Column{col("ten", "Giống, thiết bị, vật tư", KindText), col("dvt", "ĐVT", KindText), col("ke_hoach", "Kế hoạch", KindNumber),
					col("da_cap", "Đã cấp", KindNumber), col("thoi_gian", "Thời gian cấp", KindText), col("ghi_chu", "Ghi chú", KindText)}},
			{Key: "tap_huan", Label: "3. Tập huấn hộ tham gia và nhân rộng", MaxRows: 30,
				Columns: []Column{col("noi_dung", "Nội dung", KindText), col("lop_ke_hoach", "Số lớp kế hoạch", KindInt),
					col("lop_thuc_hien", "Số lớp thực hiện", KindInt), col("luot_ke_hoach", "Lượt người kế hoạch", KindInt),
					col("luot_thuc_hien", "Lượt người thực hiện", KindInt), col("ghi_chu", "Ghi chú", KindText)}},
		},
		Sections: []Section{
			{Title: "Thông tin chung", Fields: []string{"ten_du_an", "ma_du_an", "chu_nhiem", "to_chuc_chu_tri", "thoi_gian_du_an", "kinh_phi", "can_cu"}},
			{Title: "1. Xây dựng mô hình", Tables: []string{"mo_hinh"}},
			{Title: "2. Cung ứng giống, thiết bị, vật tư", Tables: []string{"vat_tu"}},
			{Title: "3. Tập huấn", Tables: []string{"tap_huan"}},
			{Title: "4–6. Tuyên truyền, đánh giá, kiến nghị", Fields: []string{"tuyen_truyen", "danh_gia", "kien_nghi"}},
		},
	},
	Form03: {
		Form: Form03, Code: "Mẫu 03", BM: "BM-13", Title: "Báo cáo kết quả kiểm tra, đánh giá dự án khuyến nông trung ương", Lifecycle: LifecycleSign,
		Numbered: true,
		Fields: []Field{
			req(long("can_cu", "Căn cứ")), txt("quyet_dinh_doan", "Quyết định thành lập đoàn kiểm tra"),
			kind(txt("ngay_kiem_tra", "Ngày kiểm tra"), KindDate),
			req(long("thuc_dia", "2. Kiểm tra thực địa (công việc đã triển khai, kết quả đến thời điểm kiểm tra)")),
			long("nx_ho_so", "Về hồ sơ, tài liệu"), long("nx_to_chuc", "Về công tác tổ chức thực hiện"),
			long("nx_tien_do", "Về tiến độ và kết quả đến thời điểm kiểm tra"), long("ton_tai", "Những hạn chế, tồn tại cần khắc phục"),
			long("kien_nghi", "III. Kiến nghị, đề xuất"), long("noi_nhan", "Nơi nhận"),
		},
		Tables: []Table{
			{Key: "doan", Label: "1. Thành phần đoàn kiểm tra", MaxRows: 30,
				Columns: []Column{col("ho_ten", "Họ và tên", KindText), col("chuc_vu", "Chức vụ, đơn vị công tác", KindText), col("chuc_danh", "Chức danh trong đoàn", KindText)}},
			{Key: "doi_tuong", Label: "2. Thời gian, địa điểm, đối tượng kiểm tra", MaxRows: 30,
				Columns: []Column{col("ten", "Tên dự án/mô hình", KindText), col("chu_tri", "Tổ chức chủ trì, chủ nhiệm dự án", KindText),
					col("don_vi", "Đơn vị thực hiện", KindText), col("dia_diem", "Địa điểm kiểm tra", KindText), col("quy_mo", "Quy mô thực hiện", KindText)}},
			{Key: "ho_so", Label: "II.1. Kiểm tra hồ sơ, tài liệu", MaxRows: 60, Hint: "Danh mục theo mẫu, thêm được dòng",
				Columns: []Column{col("ten", "Hồ sơ, tài liệu", KindText), {Key: "tinh_trang", Label: "Tình trạng", Kind: KindChoice, Options: haveOpts}, col("ghi_chu", "Ghi chú", KindText)}},
		},
		Sections: []Section{
			{Title: "Căn cứ", Fields: []string{"can_cu"}},
			{Title: "I. Thông tin chung", Fields: []string{"quyet_dinh_doan", "ngay_kiem_tra"}, Tables: []string{"doan", "doi_tuong"}},
			{Title: "II. Kết quả kiểm tra", Fields: []string{"thuc_dia"}, Tables: []string{"ho_so"}},
			{Title: "3. Nhận xét, đánh giá", Fields: []string{"nx_ho_so", "nx_to_chuc", "nx_tien_do", "ton_tai"}},
			{Title: "III. Kiến nghị, đề xuất", Fields: []string{"kien_nghi", "noi_nhan"}},
		},
	},
	Form04: {
		Form: Form04, Code: "Mẫu 04/DA-NTTH", BM: "BM-14", Title: "Bản nghiệm thu hiện trường lớp tập huấn nhân rộng mô hình", Lifecycle: LifecycleSign,
		Fields: []Field{
			hint(txt("hop_dong", "Căn cứ Hợp đồng số"), "Số, ngày hợp đồng giữa TTKN Quốc gia với tổ chức chủ trì"),
			txt("ke_hoach", "Kế hoạch tổ chức lớp được phê duyệt ngày"),
			kind(txt("ngay", "Hôm nay, ngày"), KindDate), txt("tai", "Tại"),
			req(txt("ten_lop", "1. Tên lớp tập huấn")), txt("dia_diem", "2. Địa điểm tổ chức"), kind(req(txt("so_hoc_vien", "3. Số học viên tham gia")), KindInt),
			kind(txt("tu_ngay", "4. Tập huấn từ ngày"), KindDate), kind(txt("den_ngay", "đến ngày"), KindDate),
			txt("tham_quan", "5. Thời gian tham quan"), txt("dia_diem_tham_quan", "6. Địa điểm tham quan"),
			long("noi_dung", "1. Nội dung chương trình"), long("tai_lieu", "2. Chất lượng giảng dạy — Tài liệu"),
			long("giang_vien", "2. Chất lượng giảng dạy — Giảng viên"), long("to_chuc", "3. Công tác tổ chức lớp học"),
			req(long("nhan_xet", "4. Nhận xét chung")), kind(txt("so_ban", "Số bản lập"), KindInt),
		},
		Tables: []Table{
			people("tp_to_chuc", "I. Đại diện đơn vị tổ chức lớp tập huấn", "Chức vụ", 10),
			people("tp_quan_ly", "II. Đại diện cơ quan quản lý, khuyến nông địa phương", "Chức vụ", 10),
			people("tp_hoc_vien", "III. Đại diện học viên", "Đơn vị", 10),
		},
		Sections: []Section{
			{Title: "Căn cứ", Fields: []string{"hop_dong", "ke_hoach", "ngay", "tai"}},
			{Title: "Thành phần nghiệm thu", Tables: []string{"tp_to_chuc", "tp_quan_ly", "tp_hoc_vien"}},
			{Title: "IV. Kết quả nghiệm thu", Fields: []string{"ten_lop", "dia_diem", "so_hoc_vien", "tu_ngay", "den_ngay", "tham_quan", "dia_diem_tham_quan"}},
			{Title: "Đánh giá nhận xét", Fields: []string{"noi_dung", "tai_lieu", "giang_vien", "to_chuc", "nhan_xet", "so_ban"}},
		},
	},
	Form05: {
		Form: Form05, Code: "Mẫu 05/DA-NTMH", BM: "BM-15", Title: "Bản nghiệm thu kết quả thực hiện mô hình khuyến nông", Lifecycle: LifecycleSign,
		PerSite: true, Unique: true,
		Fields: []Field{
			txt("hop_dong", "Căn cứ Hợp đồng số"), kind(txt("ngay", "Hôm nay, ngày"), KindDate), txt("tai", "Tại"),
			req(txt("ten_mo_hinh", "1. Tên mô hình trình diễn (dự án)")), req(txt("dia_diem", "2. Địa điểm xây dựng mô hình")),
			req(txt("don_vi_thuc_hien", "3. Đơn vị thực hiện")),
			long("chon_diem", "Chọn điểm, chọn hộ"), long("giong_vat_tu", "Kết quả mua và hỗ trợ giống, vật tư (chất lượng, tiến độ, hình thức cấp phát)"),
			long("chi_dao_kt", "Chỉ đạo kỹ thuật xây dựng mô hình"), long("nhan_rong", "2. Nhân rộng mô hình (tập huấn, tham quan, tổng kết)"),
			long("hieu_qua", "3. Hiệu quả, tác động của dự án"),
			kind(txt("kinh_phi_hd", "1. Tổng kinh phí theo hợp đồng (đồng)"), KindNumber), kind(txt("kinh_phi_qt", "2. Kinh phí đã thực hiện, đề nghị quyết toán (đồng)"), KindNumber),
			req(long("danh_gia", "IV.1. Đánh giá đạt/không đạt yêu cầu, đề nghị quyết toán")), long("tiep_tuc", "IV.2. Nội dung tiếp tục triển khai"),
			long("ton_tai", "IV.3. Tồn tại, lý do, kiến nghị"),
		},
		Tables: []Table{
			{Key: "thanh_phan", Label: "Chúng tôi gồm", MaxRows: 40, Hint: "Đơn vị ký HĐ, đơn vị cơ sở, cơ quan quản lý/Khuyến nông tỉnh, chính quyền xã, hộ tham gia, cán bộ phụ trách",
				Columns: []Column{{Key: "nhom", Label: "Đại diện", Kind: KindChoice, Options: []Option{
					{Value: "don_vi_hd", Label: "Đơn vị ký hợp đồng / tổ chức chủ trì"}, {Value: "co_so", Label: "Đơn vị cơ sở thực hiện mô hình"},
					{Value: "quan_ly", Label: "Cơ quan quản lý ngành NN và MT, Khuyến nông"}, {Value: "chinh_quyen", Label: "Chính quyền xã, phường"},
					{Value: "ho", Label: "Hộ tham gia"}, {Value: "can_bo", Label: "Cán bộ phụ trách xây dựng mô hình"}, {Value: "khac", Label: "Cơ quan, đơn vị khác"}}},
					col("ho_ten", "Ông/bà", KindText), col("chuc_vu", "Chức vụ / đơn vị", KindText)}},
			indicators(Form05),
			{Key: "ho_tro", Label: "5. Giá trị giống, vật tư đã hỗ trợ cho nông dân", MaxRows: 60,
				Columns: []Column{col("ten", "Tên giống, vật tư", KindText), col("so_luong", "Số lượng", KindText), col("gia_tri", "Giá trị (đồng)", KindNumber), col("ghi_chu", "Ghi chú", KindText)}},
			{Key: "phu_luc_1", Label: "Phụ lục I — Kết quả của các hộ tham gia", MaxRows: 200,
				Columns: []Column{col("chu_ho", "Chủ hộ tham gia", KindText), col("quy_mo", "Quy mô", KindText), col("ket_qua", "Kết quả chính đạt được", KindText), col("ghi_chu", "Ghi chú", KindText)}},
			{Key: "phu_luc_2", Label: "Phụ lục II — Kết quả nhân rộng mô hình", MaxRows: 200,
				Columns: []Column{col("chu_ho", "Chủ hộ thực hiện nhân rộng", KindText), col("dia_chi", "Địa chỉ", KindText), col("quy_mo", "Quy mô", KindText), col("ghi_chu", "Ghi chú", KindText)}},
		},
		Sections: []Section{
			{Title: "Căn cứ, thành phần", Fields: []string{"hop_dong", "ngay", "tai"}, Tables: []string{"thanh_phan"}},
			{Title: "I. Kết quả về khối lượng thực hiện mô hình", Fields: []string{"ten_mo_hinh", "dia_diem", "don_vi_thuc_hien"}, Tables: []string{"chi_tieu", "ho_tro"}},
			{Title: "II. Đánh giá về chất lượng mô hình", Fields: []string{"chon_diem", "giong_vat_tu", "chi_dao_kt", "nhan_rong", "hieu_qua"}},
			{Title: "III. Kinh phí mô hình", Fields: []string{"kinh_phi_hd", "kinh_phi_qt"}},
			{Title: "IV. Nhận xét", Fields: []string{"danh_gia", "tiep_tuc", "ton_tai"}},
			{Title: "Phụ lục", Tables: []string{"phu_luc_1", "phu_luc_2"}},
		},
	},
	Form07: {
		Form: Form07, Code: "Mẫu 07/NTKQ-DA", BM: "BM-16", Title: "Bản nghiệm thu kết quả thực hiện dự án khuyến nông hàng năm", Lifecycle: LifecycleNational,
		Period: "year", Unique: true,
		Fields: []Field{
			req(txt("hop_dong", "Căn cứ hợp đồng số (HĐ-KN), ngày")), txt("ben_b_ten", "Tổ chức chủ trì (bên B)"),
			txt("quyet_dinh", "Quyết định phê duyệt điều chỉnh (nếu có)"), kind(txt("ngay", "Hôm nay, ngày"), KindDate), txt("tai", "Tại"),
			req(txt("ten_mo_hinh", "1. Tên mô hình/dự án")),
			long("giong_vat_tu", "Kết quả hỗ trợ giống, vật tư (chất lượng, đơn vị cung cấp, hình thức mua, cấp phát)"),
			long("chi_dao_kt", "Chỉ đạo kỹ thuật xây dựng mô hình"), long("tap_huan", "2. Công tác tập huấn, tham quan, hội thảo tổng kết"),
			long("hieu_qua", "3. Hiệu quả, tác động của dự án"),
			kind(txt("gia_tri_hd", "Giá trị hợp đồng (đồng)"), KindNumber), kind(txt("da_nhan", "Số tiền bên B đã nhận (đồng)"), KindNumber),
			req(long("nhan_xet", "IV. Nhận xét chung (kết quả, đạt/không đạt, đề nghị quyết toán)")), long("che_do_bao_cao", "Chấp hành quy chế, chế độ báo cáo"),
			long("tiep_tuc", "Nội dung tiếp tục triển khai"), long("ton_tai", "Tồn tại, lý do, kiến nghị"), kind(txt("so_ban", "Số bản lập"), KindInt),
		},
		Tables: []Table{
			people("ben_a", "Đại diện Trung tâm Khuyến nông Quốc gia (Bên A)", "Chức vụ", 10),
			people("ben_b", "Đại diện Tổ chức chủ trì dự án (Bên B)", "Chức vụ", 10),
			{Key: "diem", Label: "2. Kết quả thực hiện tại các điểm trình diễn", MaxRows: 200, Auto: true,
				Hint: "Tổng hợp từ các Mẫu 05 đã ký của dự án",
				Columns: []Column{col("dia_diem", "Địa điểm trình diễn", KindText), col("quy_mo", "Qui mô thực hiện", KindText), col("so_ho", "Số hộ tham gia", KindText),
					col("thoi_gian", "Thời gian triển khai", KindText), col("ket_qua", "Kết quả đạt được (chỉ tiêu chính)", KindText)}},
			indicators(Form07),
		},
		Sections: []Section{
			{Title: "Căn cứ, thành phần", Fields: []string{"hop_dong", "ben_b_ten", "quyet_dinh", "ngay", "tai"}, Tables: []string{"ben_a", "ben_b"}},
			{Title: "I. Kết quả về khối lượng thực hiện", Fields: []string{"ten_mo_hinh"}, Tables: []string{"diem", "chi_tieu"}},
			{Title: "II. Kết quả về chất lượng mô hình", Fields: []string{"giong_vat_tu", "chi_dao_kt", "tap_huan", "hieu_qua"}},
			{Title: "III. Kinh phí trong năm", Fields: []string{"gia_tri_hd", "da_nhan"}},
			{Title: "IV. Nhận xét chung", Fields: []string{"nhan_xet", "che_do_bao_cao", "tiep_tuc", "ton_tai", "so_ban"}},
		},
	},
}

// IndicatorRows là các dòng chỉ tiêu chuẩn của bảng chi_tieu (mã → tên, đơn vị) theo mẫu Word.
func IndicatorRows(form string) []Row {
	base := []Row{
		{"ma": "quy_mo", "chi_tieu": "1. Qui mô"}, {"ma": "so_ho", "chi_tieu": "2. Số hộ tham gia", "dvt": "hộ"},
		{"ma": "thoi_gian", "chi_tieu": "3. Thời gian triển khai"}, {"ma": "giong", "chi_tieu": "4. Giống (tên giống, phẩm cấp, số lượng)"},
		{"ma": "vat_tu", "chi_tieu": "5. Các loại vật tư hỗ trợ"},
		{"ma": "tap_huan", "chi_tieu": "6. Tập huấn (số lớp, số lượt người)"},
	}
	if form == Form07 {
		base = append(base, Row{"ma": "dao_tao", "chi_tieu": "7. Đào tạo (số lớp ngoài MH, số ngày, số lượt người)"},
			Row{"ma": "tham_quan", "chi_tieu": "8. Tham quan (số ngày, số lượt người)"}, Row{"ma": "tong_ket", "chi_tieu": "9. Tổng kết (số ngày, số lượt người)"},
			Row{"ma": "nang_suat", "chi_tieu": "10. Năng suất"}, Row{"ma": "chat_luong", "chi_tieu": "Chất lượng sản phẩm (nếu có)"},
			Row{"ma": "hieu_qua", "chi_tieu": "Hiệu quả kinh tế", "dvt": "đồng"})
		return base
	}
	return append(base, Row{"ma": "tham_quan", "chi_tieu": "7. Tham quan (số ngày, số lượt người)"},
		Row{"ma": "tong_ket", "chi_tieu": "8. Tổng kết (số ngày, số lượt người)"},
		Row{"ma": "nang_suat", "chi_tieu": "9. Năng suất (sản lượng)", "dvt": "kg"}, Row{"ma": "hieu_qua", "chi_tieu": "Hiệu quả kinh tế (lợi nhuận)", "dvt": "đồng"},
		Row{"ma": "nhan_rong", "chi_tieu": "10. Nhân rộng mô hình"})
}

// DocChecklist là danh mục hồ sơ mặc định của Mẫu 03 (theo báo cáo kiểm tra mẫu của phòng; thêm được dòng).
var DocChecklist = []string{
	"Kế hoạch thực hiện dự án", "Thông báo triển khai dự án tại địa phương", "Quyết định phê duyệt quy chuẩn, tiêu chuẩn hàng hóa",
	"Quyết định phân công viên chức chỉ đạo, phụ trách dự án", "Kế hoạch mua sắm hàng hóa thực hiện dự án",
	"Quyết định, hợp đồng mua sắm giống, thức ăn, vật tư", "Hồ sơ nông dân (đơn tham gia, cam kết đối ứng, biên bản chọn hộ, hợp đồng)",
	"Biên bản bàn giao giống, vật tư", "Danh sách hỗ trợ giống, thiết bị, vật tư; danh sách đối ứng",
}
