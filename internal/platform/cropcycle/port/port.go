// Package port là giao diện công khai DUY NHẤT của module pf/cropcycle mà module khác được import (R-02).
//
// W3.3: vụ mùa, nhật ký canh tác, SOP, sản lượng (cùng sự kiện pf.cropcycle.{season,farm_log,harvest,sop,
// coop_season_area}.*, vai farmer/sop_admin, thực thể đồng bộ) chuyển sang pf/cooperative (cooperativeport,
// docs/design/cooperative.md). Module chỉ còn van thu gom MKG ↔ HTX (collection.go) — đến G2, rồi ADR-0033 quyết
// định nơi đặt.
package port

// RoleCoopManager là vai quản lý HTX (khai ở pf/party): xác nhận / tranh chấp bảng kê thu gom của HTX mình.
const RoleCoopManager = "coop_manager"
