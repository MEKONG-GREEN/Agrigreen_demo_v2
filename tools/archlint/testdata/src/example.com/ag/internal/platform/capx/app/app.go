package app

import "strings"

// AppKN là mã ứng dụng dạng hằng có tên.
const AppKN = "KN"

// AppID là kiểu mã ứng dụng.
type AppID string

// OrgKind là enum miền — "htx" ở đây là loại tổ chức, không phải mã ứng dụng.
type OrgKind string

const (
	OrgHTX OrgKind = "htx"
	AppHTX AppID   = "HTX"
)

func Branch(app string, id AppID, k OrgKind) int {
	if app == "KN" { // want `W9.1: module platform/capx \(kind: capability\) không được rẽ nhánh theo mã ứng dụng "KN"`
		return 1
	}
	if AppKN != app { // want `mã ứng dụng "KN"`
		return 2
	}
	if strings.EqualFold(app, "htx") { // want `mã ứng dụng "htx"`
		return 3
	}
	switch app {
	case "Htx": // want `mã ứng dụng "Htx"`
		return 4
	case "other":
		return 5
	}
	if id == AppHTX { // want `mã ứng dụng "HTX"`
		return 6
	}
	if k == OrgHTX { // enum miền có kiểu riêng: được phép
		return 7
	}
	if app == "SIM" { // không có trong app/*/app.yaml của testdata
		return 8
	}
	_ = map[string]int{"KN": 1} // nhãn, không phải so sánh
	return 0
}
