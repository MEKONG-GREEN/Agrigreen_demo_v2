// Package client là cửa DUY NHẤT để module ngoài gọi kho định danh (R-04).
//
// Kho định danh chạy ở cụm riêng (ag_vault), dịch vụ identity-vault, gọi qua
// mTLS nội bộ. Mọi thao tác đọc định danh phải kèm MỤC ĐÍCH (R-42); không có mục
// đích thì kho từ chối. Không log giá trị trả về (R-14).
package client

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// Purpose là mục đích xử lý dữ liệu cá nhân, khớp với purpose của pf_party.consent.
type Purpose string

// Mục đích dùng ở ĐTK 1.
const (
	PurposeFieldSupport  Purpose = "extension.field_support" // khuyến nông hỗ trợ hộ
	PurposeContact       Purpose = "extension.contact"       // liên hệ hộ/đại diện HTX
	PurposeMRVEvidence   Purpose = "mrv.evidence"            // ảnh minh chứng MRV
	PurposeRegistryAdmin Purpose = "registry.admin"          // quản trị hồ sơ
	PurposeDedup         Purpose = "registry.dedup"          // chống trùng khi nhập
)

// SubjectKind là loại chủ thể dữ liệu.
type SubjectKind string

// Các loại chủ thể.
const (
	SubjectHousehold SubjectKind = "household" // nông hộ
	SubjectPerson    SubjectKind = "person"    // người: đại diện HTX, chủ máy, cán bộ
)

// Identity là dữ liệu định danh — CHỈ tồn tại trong bộ nhớ khi gửi vào hoặc đọc ra.
type Identity struct {
	Kind       SubjectKind
	FullName   string `pii:"true"`
	Phone      string `pii:"true"`
	NationalID string `pii:"true"` // CCCD — có thể rỗng
	Address    string `pii:"true"`
	// AreaCode là mã xã/tỉnh để phân phối khóa công khai xuống thiết bị đúng địa bàn.
	AreaCode string
}

// Created là kết quả tạo chủ thể.
type Created struct {
	Ref       uuid.UUID // mã tham chiếu ngẫu nhiên, không suy ra từ định danh (R-41)
	PublicKey []byte    // khóa công khai X25519 32 byte
}

// Display là nhãn hiển thị tối thiểu (mục 10.4).
type Display struct {
	Ref         uuid.UUID
	Label       string `pii:"true"` // vd "Nguyễn V. A." — che bớt
	MaskedPhone string `pii:"true"` // vd "09xx xxx 123"
}

// PubKey là khóa công khai của một chủ thể để đồng bộ xuống thiết bị.
type PubKey struct {
	Ref       uuid.UUID
	AreaCode  string
	PublicKey []byte
}

// Lỗi của kho định danh.
var (
	ErrNoPurpose = errors.New("vault: thiếu mục đích")
	ErrNotFound  = errors.New("vault: không có chủ thể")
	ErrErased    = errors.New("vault: chủ thể đã bị xóa định danh")
	ErrDuplicate = errors.New("vault: trùng chủ thể theo chỉ mục mù")
)

// Client là giao diện gọi kho định danh.
type Client interface {
	// Create tạo chủ thể, sinh cặp khóa X25519; trả ErrDuplicate kèm Ref cũ nếu trùng
	// chỉ mục mù (SĐT hoặc CCCD) trong cùng tenant.
	Create(ctx context.Context, purpose Purpose, id Identity) (Created, error)
	// Display trả nhãn hiển thị tối thiểu.
	Display(ctx context.Context, purpose Purpose, ref uuid.UUID) (Display, error)
	// Resolve trả định danh đầy đủ — mọi lần gọi đều ghi nhật ký có mục đích và người yêu cầu.
	Resolve(ctx context.Context, purpose Purpose, ref uuid.UUID) (Identity, error)
	// Lookup tìm chủ thể theo SĐT hoặc CCCD qua chỉ mục mù; không lộ danh sách.
	Lookup(ctx context.Context, purpose Purpose, phoneOrNationalID string) (uuid.UUID, error)
	// Unwrap mở một DEK đã seal bằng khóa công khai của chủ thể (xem ảnh minh chứng).
	Unwrap(ctx context.Context, purpose Purpose, ref uuid.UUID, wrappedDEK []byte) ([]byte, error)
	// Erase hủy khóa bí mật và xóa định danh (RB-3). Ảnh của chủ thể không còn đọc được.
	Erase(ctx context.Context, ref uuid.UUID, reason string) error
	// PubKeys trả khóa công khai của các chủ thể thuộc địa bàn, cho đồng bộ xuống thiết bị.
	PubKeys(ctx context.Context, areaCodes []string) ([]PubKey, error)
}
