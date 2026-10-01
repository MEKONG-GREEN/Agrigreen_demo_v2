// Package seeddata nhúng dữ liệu nền KN v7.7 của pf/party (go:embed), TẠM — chờ khách xác nhận:
//
//   - departments_an_giang.json: 6 phòng ban của TTKN An Giang, Phòng Trồng trọt "Đang áp dụng" (Q-02).
//   - regions_an_giang.json: Khu vực (huyện cũ) của tỉnh An Giang (mã 91, sau sắp xếp 2025) và xã
//     thuộc từng Khu vực, suy từ bảng kế thừa ĐVHC (Q-04).
//
// Nạp theo yêu cầu của Admin (POST /v1/orgs/{ref}/departments/seed, POST /v1/regions/seed); chạy lặp an toàn.
package seeddata

import _ "embed"

// Departments là phòng ban mặc định (định dạng agrigreen.departments/v1).
//
//go:embed departments_an_giang.json
var Departments []byte

// RegionsAnGiang là khu vực An Giang (định dạng agrigreen.regions/v1).
//
//go:embed regions_an_giang.json
var RegionsAnGiang []byte
