package domain

// Biểu mẫu thử (seed/fixture) của cap/forms. 14 biểu thật của KN (câu hỏi khách Q-01) CHƯA có: cơ chế chạy được
// với các biểu này (kiểm thử, UAT, demo), dòng sổ đánh dấu fixture = true và profile production (AllowFixtures =
// false) không phát hành được kỳ trên chúng — bật production chờ Q-01.

// Fixture là một biểu mẫu thử.
type Fixture struct {
	Code   string
	Title  string
	Fields []Field
}

// Fixtures trả các biểu mẫu thử (bản sao mới mỗi lần gọi).
func Fixtures() []Fixture {
	return []Fixture{
		{
			Code:  "FX-01",
			Title: "Biểu mẫu thử — tiến độ gieo trồng (chờ biểu thật Q-01)",
			Fields: []Field{
				{Key: "crop", Label: "Cây trồng", Type: TypeChoice, Required: true, Options: []string{"lua", "ngo", "rau"}},
				{Key: "area_planned", Label: "Diện tích kế hoạch", Type: TypeDecimal, Required: true, Unit: "ha"},
				{Key: "area_sown", Label: "Diện tích đã gieo", Type: TypeDecimal, Required: true, Unit: "ha"},
				{Key: "households", Label: "Số hộ tham gia", Type: TypeInteger},
				{Key: "report_date", Label: "Ngày số liệu", Type: TypeDate, Required: true},
				{Key: "note", Label: "Ghi chú", Type: TypeText},
			},
		},
		{
			Code:  "FX-02",
			Title: "Biểu mẫu thử — tình hình sâu bệnh (chờ biểu thật Q-01)",
			Fields: []Field{
				{Key: "pest", Label: "Đối tượng sâu bệnh", Type: TypeText, Required: true},
				{Key: "area_infected", Label: "Diện tích nhiễm", Type: TypeDecimal, Required: true, Unit: "ha"},
				{Key: "area_treated", Label: "Diện tích đã xử lý", Type: TypeDecimal, Unit: "ha"},
			},
		},
	}
}
