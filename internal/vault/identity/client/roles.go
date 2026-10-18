package client

import (
	"net/http"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Vai của kho định danh (realm role trong token của NGƯỜI DÙNG CUỐI). Kho tự
// kiểm vai trên token được chuyển tiếp — module gọi không thể nâng quyền.
//
//	Thao tác             Vai được phép
//	Create, Lookup       RoleRegistrar
//	Resolve              RoleRegistrar
//	Display              RoleRegistrar, RoleDisplay, RoleEvidenceViewer
//	Unwrap               RoleEvidenceViewer
//	Erase                RoleEraser + MFA
//	PubKeys              RoleRegistrar, RoleDisplay, RoleEvidenceViewer, RolePubKeySync
//	HouseholdPubKeys     như PubKeys, cộng RoleFarmer
const (
	RoleRegistrar      = "vault_registrar"       // cán bộ quản lý hồ sơ hộ/người: tạo, tra, xem đầy đủ
	RoleDisplay        = "vault_display"         // chỉ nhãn hiển thị tối thiểu
	RoleEvidenceViewer = "vault_evidence_viewer" // mở DEK để xem ảnh minh chứng
	RoleEraser         = "vault_eraser"          // xóa định danh theo yêu cầu (RB-3), bắt buộc MFA
	RolePubKeySync     = "vault_pubkey_sync"     // thiết bị hiện trường tải khóa công khai theo địa bàn
	// RoleFarmer là vai nông dân của App HTX (cùng tên vai của pf/accounts). Kho chỉ cho vai
	// này đọc khóa công khai THEO REF nông hộ (HouseholdPubKeys) — không liệt kê theo địa
	// bàn, không đọc định danh. Kho chỉ nhận lời gọi từ pf qua mTLS nội bộ; pf/sync giới hạn
	// ref vào các nông hộ liên kết với tài khoản (R-40: máy mã hóa ảnh bằng khóa hộ mình).
	RoleFarmer = "farmer"
)

// PurposeErasure là mục đích ghi vào sổ truy cập cho thao tác xóa định danh.
const PurposeErasure Purpose = "registry.erasure"

// HeaderPurpose là header mang mục đích (R-42). Thân request có thể mang trường "purpose" thay thế.
const HeaderPurpose = "X-Purpose"

// Mã lỗi ổn định của kho định danh (docs/errors.md).
const (
	CodePurposeRequired = "vault.purpose_required"
	CodeNotFound        = "vault.not_found"
	CodeErased          = "vault.erased"
	CodeDuplicate       = "vault.duplicate"
	CodeUnwrapFailed    = "vault.unwrap_failed"
	CodeUnavailable     = "vault.unavailable"
	// CodeQuotaExceeded: người yêu cầu vượt hạn mức tra/đọc định danh (Lookup, Resolve) — M13.
	CodeQuotaExceeded = "vault.quota_exceeded"
	// CodeSectionInvalid: ngăn hồ sơ không có, mục đích không phải mục đích của ngăn, hoặc trường lạ/quá dài (W11.22).
	CodeSectionInvalid = "vault.section_invalid"
	// CodeProfilesUnsupported: hiện thực kho không có phần mở rộng hồ sơ (Profiles) — lỗi cấu hình.
	CodeProfilesUnsupported = "vault.profiles_unsupported"
)

// Problem tương ứng các lỗi của kho. errors.Is(p, ErrX) đúng nhờ WithCause.
var (
	ProblemPurposeRequired = problem.New(http.StatusBadRequest, CodePurposeRequired, "Thiếu mục đích xử lý dữ liệu cá nhân").WithCause(ErrNoPurpose)
	ProblemNotFound        = problem.New(http.StatusNotFound, CodeNotFound, "Không có chủ thể").WithCause(ErrNotFound)
	ProblemErased          = problem.New(http.StatusGone, CodeErased, "Chủ thể đã bị xóa định danh").WithCause(ErrErased)
	ProblemDuplicate       = problem.New(http.StatusConflict, CodeDuplicate, "Đã có chủ thể cùng số điện thoại hoặc CCCD").WithCause(ErrDuplicate)
	ProblemUnwrapFailed    = problem.New(http.StatusUnprocessableEntity, CodeUnwrapFailed, "Không mở được khóa tệp bằng khóa của chủ thể")
	ProblemQuotaExceeded   = problem.New(http.StatusTooManyRequests, CodeQuotaExceeded, "Vượt hạn mức tra cứu định danh, vui lòng thử lại sau")
	ProblemSectionInvalid  = problem.New(http.StatusUnprocessableEntity, CodeSectionInvalid, "Ngăn hồ sơ, mục đích hoặc trường không hợp lệ")
	// ProblemProfilesUnsupported: 503 — kho được nối không hỗ trợ ngăn hồ sơ.
	ProblemProfilesUnsupported = problem.New(http.StatusServiceUnavailable, CodeProfilesUnsupported, "Kho định danh chưa hỗ trợ hồ sơ nhân sự")
)
