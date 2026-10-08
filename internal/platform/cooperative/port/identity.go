package port

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// HouseholdIdentities là cổng hẹp tới kho định danh cho chủ hộ của cooperative/member (R-04, RB-3): gốc module
// không import vault client (cmd/integration-gateway dựng pf/cooperative mà không kéo kho định danh). Bộ điều hợp
// trên vault client: internal/platform/cooperative/port/farmerid.Households (gốc lắp ráp nối).
type HouseholdIdentities interface {
	// RegisterHousehold ghi định danh chủ hộ (mục đích registry.admin). SĐT/CCCD đã có → ref đã có kèm
	// ErrIdentityDuplicate (ref có thể Nil nếu kho không trả). Lỗi khác của kho trả nguyên (problem 4xx giữ nguyên).
	RegisterHousehold(ctx context.Context, id HouseholdIdentity) (uuid.UUID, error)
	// Label trả nhãn đã che (tên, SĐT che) theo mục đích.
	Label(ctx context.Context, purpose IdentityPurpose, ref uuid.UUID) (IdentityLabel, error)
	// Phone trả SĐT liên hệ (mục đích extension.contact); không có hoặc đã xóa → ErrIdentityNotFound.
	Phone(ctx context.Context, ref uuid.UUID) (string, error)
	// LookupPhone tra ref theo SĐT qua chỉ mục mù (mục đích registry.admin); không thấy → ErrIdentityNotFound.
	LookupPhone(ctx context.Context, phone string) (uuid.UUID, error)
}

// IdentityPurpose là mục đích đọc nhãn của HouseholdIdentities.Label.
type IdentityPurpose string

// Mục đích của HouseholdIdentities.Label.
const (
	IdentityFieldSupport IdentityPurpose = "field_support" // danh sách nông hộ của HTX
	IdentityContact      IdentityPurpose = "contact"       // nhãn người của màn hình KN
)

// HouseholdIdentity là dữ liệu cá nhân của chủ hộ — chỉ đi thẳng vào kho định danh, không lưu ở pf_coop.
type HouseholdIdentity struct {
	FullName, Phone, NationalID, Address, AreaCode string
}

// IdentityLabel là nhãn đã che của kho định danh.
type IdentityLabel struct {
	Label, MaskedPhone string
}

// Lỗi của HouseholdIdentities.
var (
	ErrIdentityDuplicate = errors.New("cooperative: định danh đã có trong kho")
	ErrIdentityNotFound  = errors.New("cooperative: không có định danh trong kho")
)
