// Package seeddata nhúng dữ liệu nền mặc định của pf/masterdata (go:embed).
//
//   - admin_units_dbscl.json: đơn vị hành chính ĐBSCL trước và sau sắp xếp 2025,
//     cùng định dạng với Admin API POST /v1/masterdata/admin-units/import.
//   - catalogs.json: danh mục khâu, loại máy, vụ, giống lúa, dịch hại.
//   - params.json: tham số đã phê duyệt sẵn (ngưỡng cân đối CGH QT-02, định mức QT-01).
//
// Dữ liệu được nạp theo từng tenant bằng Service.Seed; chạy lặp an toàn.
package seeddata

import _ "embed"

// AdminUnits là danh mục đơn vị hành chính (định dạng agrigreen.admin_units/v1).
//
//go:embed admin_units_dbscl.json
var AdminUnits []byte

// Catalogs là danh mục dùng chung (định dạng agrigreen.catalogs/v1).
//
//go:embed catalogs.json
var Catalogs []byte

// Params là tham số mặc định đã phê duyệt (định dạng agrigreen.params/v1).
//
//go:embed params.json
var Params []byte
