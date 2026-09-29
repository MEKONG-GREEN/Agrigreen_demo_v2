// Package defs chứa định nghĩa chỉ số là TỆP KHAI BÁO CÓ PHIÊN BẢN trong kho mã
// (tài liệu kiến trúc mục 16). Mỗi tệp <key>.v<version>.yaml là MỘT phiên bản;
// đổi công thức = thêm tệp phiên bản mới, không sửa tệp cũ (băm của tệp được
// ghim ở pf_metrics.definition_pin — sửa tệp đã dùng bị job từ chối).
package defs

import "embed"

// FS là các tệp định nghĩa nhúng vào nhị phân.
//
//go:embed *.yaml
var FS embed.FS
