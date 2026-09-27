// Package port là giao diện công khai DUY NHẤT của pf/mechanization (Bản đồ số
// cơ giới hóa — CGH) mà module khác được import (R-02).
package port

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// Vai nghiệp vụ của module (tên vai trong Keycloak). CGH FN-01 BR-01: chỉ hai
// vai người dùng — Cục KTHT (xem) và Admin (quản trị) — cộng vai hệ thống đồng bộ.
const (
	// RoleCGHViewer: Cục KTHT & PTNT — chỉ xem toàn vùng (phạm vi All qua tổ chức dcrd).
	RoleCGHViewer = "cgh_viewer"
	// RoleCGHAdmin: Admin Bản đồ CGH — nhập/sửa hồ sơ chủ máy, máy, diện tích vụ, chạy cân đối.
	RoleCGHAdmin = "cgh_admin"
	// RoleCGHSync: tác nhân hệ thống nhận dữ liệu vận hành từ App HTX (FN-07, từ G2).
	RoleCGHSync = "cgh_sync"
)

// PurposeMachineOwner là mục đích xử lý dữ liệu cá nhân của chủ máy ở kho định
// danh (R-42): tạo, hiển thị nhãn đã che.
const PurposeMachineOwner client.Purpose = "mechanization.owner"

// Sự kiện module phát ra (schema trong contracts/events/).
const (
	EventBalanceComputed = "pf.mechanization.balance.computed"
)

// BalanceComputedV1 là payload của pf.mechanization.balance.computed: một lần
// cân đối cung–cầu (FN-09) đã được lưu bất biến. Không chứa dữ liệu cá nhân.
//
//ag:event pf.mechanization.balance.computed v1
type BalanceComputedV1 struct {
	RunID         string         `json:"run_id"`
	SeasonCode    string         `json:"season_code"`
	SeasonYear    int            `json:"season_year"`
	ReferenceDate string         `json:"reference_date"`
	RuleVersion   string         `json:"rule_version"`
	ResultCount   int            `json:"result_count"`
	StatusCounts  map[string]int `json:"status_counts"`
	MissingNorms  []string       `json:"missing_norms"`
	ComputedAt    string         `json:"computed_at"`
	Seq           int64          `json:"seq"`
}

// Nguồn dữ liệu vận hành (QT-03).
const (
	SourceAppHTX = "app_htx"
	SourceManual = "manual"
)

// SyncedArea là diện tích canh tác theo vụ nhận từ App HTX (FN-07).
type SyncedArea struct {
	CooperativeRef uuid.UUID
	SeasonCode     string
	SeasonYear     int
	AreaHa         decimal.Decimal
	SourceAt       time.Time // thời điểm cập nhật ở App HTX
	IdemKey        string
}

// AreaFeedResult là kết quả áp một bản ghi đồng bộ theo QT-03.
type AreaFeedResult struct {
	Applied bool
	// Reason giải thích khi không áp (vd bản ghi đã khóa — giữ giá trị nhập tay).
	Reason string
}

// AreaFeed là cửa nhận diện tích vụ từ App HTX (FN-07). App HTX thuộc G2: G1 chỉ
// có đường nhập tay, cửa này đã áp đúng QT-03 để bộ đồng bộ G2 gọi. Người gọi
// phải có vai RoleCGHSync hoặc RoleCGHAdmin.
type AreaFeed interface {
	ApplySyncedArea(ctx context.Context, in SyncedArea) (AreaFeedResult, error)
}
