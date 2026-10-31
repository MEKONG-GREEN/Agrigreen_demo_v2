package domain

// Seed preserves the existing fixed report section identities. Implicit heading
// parents are made explicit; historical drafts still use their original layout.
// These are definitions for Admin to review and publish, never an implicit rebind.
func Seed(key string) Definition {
	d := Definition{Key: key, Nodes: []Node{}}
	if !Fixed(key) {
		return d
	}
	add := func(id, parent, title string, order int, mode string) {
		n := Node{ID: id, Parent: parent, Title: title, Order: order, Repeat: "none", Bindings: []Binding{}, LockedAdmin: []string{}}
		switch mode {
		case "text":
			n.Body = "{loi_van}"
			n.TextSlot = true
			n.Bindings = []Binding{{Variable: "loi_van", Source: "text", Field: "narrative"}}
		case "previous", "current", "all":
			n.Repeat = "plan_item"
			n.Tab = mode
			n.Preamble = "- Chương trình {chuong_trinh}"
			n.Body = "+ {ten_hang_muc}: trong kỳ thực hiện {so_trong_ky} {don_vi} (nội dung: {noi_dung}). Lũy kế {luy_ke} {don_vi}."
			n.Bindings = []Binding{{Variable: "chuong_trinh", Source: "plan", Field: "program_name"}, {Variable: "ten_hang_muc", Source: "plan", Field: "item_name"}, {Variable: "don_vi", Source: "plan", Field: "unit"}, {Variable: "so_trong_ky", Source: "auto", Field: "value"}, {Variable: "noi_dung", Source: "auto", Field: "content"}, {Variable: "luy_ke", Source: "calculated", Field: "year_to_date"}}
		case "metric_tt", "metric_cn", "metric_ts":
			n.Repeat = "metric"
			n.Department = "chan_nuoi_thuy_san"
			if mode == "metric_tt" {
				n.Department = "trong_trot"
			}
			// The separate seed references prevent animal/aquaculture rows from being repeated twice.
			ref := ""
			if mode == "metric_cn" {
				ref = "section:A.I.2"
			}
			if mode == "metric_ts" {
				ref = "section:A.I.3"
			}
			n.Body = "{chi_tieu}: {so_trong_ky} {don_vi}; lũy kế năm {luy_ke} {don_vi}."
			n.Bindings = []Binding{{Variable: "chi_tieu", Source: "module", Field: "label", Ref: ref}, {Variable: "so_trong_ky", Source: "module", Field: "value", Ref: ref}, {Variable: "don_vi", Source: "module", Field: "unit", Ref: ref}, {Variable: "luy_ke", Source: "calculated", Field: "year_to_date", Ref: ref}}
		case "price":
			n.Repeat = "commodity"
			n.Body = "{mat_hang}: từ {gia_tu} đến {gia_den} {don_vi}."
			n.Bindings = []Binding{{Variable: "mat_hang", Source: "module", Field: "label"}, {Variable: "gia_tu", Source: "module", Field: "from_price"}, {Variable: "gia_den", Source: "module", Field: "to_price"}, {Variable: "don_vi", Source: "module", Field: "unit"}}
		case "form", "hcth":
			n.Repeat = "form_bullet"
			n.Body = "{phong}: {noi_dung}"
			if mode == "hcth" {
				n.Department = "hanh_chinh_tong_hop"
			}
			n.Bindings = []Binding{{Variable: "phong", Source: "form", Field: "department"}, {Variable: "noi_dung", Source: "form", Field: "content", Ref: "section:" + id}}
		}
		d.Nodes = append(d.Nodes, n)
	}
	switch key {
	case "mau_1":
		add("A", "", "KẾT QUẢ ĐẠT ĐƯỢC", 10, "")
		add("A.I", "A", "Tình hình sản xuất nông, lâm, thủy sản", 10, "")
		add("A.I.1", "A.I", "Trồng trọt", 10, "metric_tt")
		add("A.I.2", "A.I", "Chăn nuôi", 20, "metric_cn")
		add("A.I.3", "A.I", "Thủy sản", 30, "metric_ts")
		add("A.II", "A", "Kết quả thực hiện công tác khuyến nông", 20, "")
		add("A.II.1", "A.II", "Chương trình, dự án còn theo dõi năm trước", 10, "previous")
		add("A.II.2", "A.II", "Chương trình triển khai trong năm", 20, "current")
		add("A.II.3", "A.II", "Công tác khác", 30, "")
		add("A.II.3.1", "A.II.3", "Công tác báo cáo", 10, "text")
		add("A.II.3.2", "A.II.3", "Công tác khác", 20, "form")
		add("A.III", "A", "Thông tin giá cả thị trường", 30, "price")
		add("A.IV", "A", "Công tác CCHC, tổ chức cán bộ", 40, "hcth")
		add("B", "", "Đánh giá hạn chế, vướng mắc", 20, "text")
		add("C", "", "Nhiệm vụ kỳ tới", 30, "")
		add("C.1", "C", "Nhiệm vụ kỳ tới: Sản xuất NLTS", 10, "text")
		add("C.2", "C", "Nhiệm vụ kỳ tới: Công tác khuyến nông", 20, "text")
		add("C.3", "C", "Nhiệm vụ kỳ tới: Giá cả", 30, "text")
		add("C.4", "C", "Nhiệm vụ kỳ tới: CCHC", 40, "text")
		add("D", "", "Đề xuất, kiến nghị", 40, "text")
	case "mau_2", "mau_3", "mau_4":
		add("I", "", "Kết quả thực hiện nhiệm vụ công tác khuyến nông", 10, "")
		add("I.1", "I", "Chương trình, dự án còn theo dõi năm trước", 10, "previous")
		add("I.2", "I", "Chương trình triển khai trong năm", 20, "current")
		add("I.3", "I", "Công tác khác", 30, "")
		add("I.3.1", "I.3", "Công tác báo cáo", 10, "text")
		add("I.3.2", "I.3", "Công tác khác", 20, "form")
		add("II", "", "Công tác tổ chức cán bộ, cải cách hành chính", 20, "")
		add("II.1", "II", "Công tác cán bộ", 10, "hcth")
		add("II.2", "II", "Công tác cải cách hành chính", 20, "hcth")
		add("III", "", "Đánh giá kết quả thực hiện", 30, "")
		add("III.1", "III", "Thuận lợi", 10, "text")
		add("III.2", "III", "Khó khăn", 20, "text")
		add("III.3", "III", "Nguyên nhân", 30, "text")
		add("IV", "", "Phương hướng, nhiệm vụ kỳ tới", 40, "text")
		add("V", "", "Đề xuất và kiến nghị", 50, "text")
	case "mau_5":
		add("I", "", "Tiến độ thực hiện các chương trình", 10, "all")
		d.Nodes[len(d.Nodes)-1].TextSlot = true
		add("II", "", "Kế hoạch thực hiện kỳ tới", 20, "text")
		add("phu_luc", "", "Tiến độ thực hiện chương trình khuyến nông", 30, "all")
	}
	return d
}
