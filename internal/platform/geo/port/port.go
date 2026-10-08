// Package port là giao diện công khai DUY NHẤT của module pf/geo mà module khác
// được import (R-02): vai, sự kiện, log GPS, hub MKG, lớp bản đồ. Thửa và ranh giới HTX
// chuyển sang pf/cooperative ở W3.3 (cooperative/port) — geo khai interface cho phần
// nó cần (coop.go), gốc lắp ráp nối.
package port

// Vai nghiệp vụ của module (tên vai trong Keycloak). plot_editor, plot_viewer do pf/cooperative khai (W3.3);
// geo vẫn nhận hai vai đó khi đọc lớp nền (kho/Hub, đường bộ, log GPS).
const (
	// RoleGeoAdmin: Admin API — nhập ranh giới hành chính, kho/Hub, đường bộ, sức chứa; pf/cooperative kiểm
	// vai này cho nhập tệp ranh giới thửa và ranh giới HTX.
	RoleGeoAdmin = "geo_admin"
	// RoleMapConsumer: ứng dụng tiêu thụ (TMS — mkg/logistics) đọc bản đồ chuẩn hóa chỉ đọc
	// GET /v1/geo/standard-map bằng tài khoản dịch vụ (GIS US-INT-02 / FN-17). Chỉ lớp nền
	// không định danh (ranh giới hành chính, HTX, kho/Hub, đường bộ, đường thủy) — không có thửa.
	RoleMapConsumer = "map_consumer"
	// RoleCoopManager là vai quản lý HTX (khai ở pf/party). Người gọi có vai này chỉ thấy
	// kho/Hub, log GPS gắn với chính HTX mình (Principal.OrgRef).
	RoleCoopManager = "coop_manager"
)
