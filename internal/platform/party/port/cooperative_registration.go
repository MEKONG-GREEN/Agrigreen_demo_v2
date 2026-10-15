package port

import (
	"context"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"time"
)

// CooperativeRegistration là đầu vào tạo Nút + hồ sơ cho luồng Neo HTX.
// Tenant và vai lấy từ ngữ cảnh xác thực; MST và khóa do dịch vụ Neo đặt.
type CooperativeRegistration struct {
	Name              string
	CommuneCode       string
	ParentRef         uuid.UUID
	TaxCode           string
	IdemKey           string
	RepresentativeRef uuid.UUID
	Representative    *RegistrationRepresentative
	AreaHa            *decimal.Decimal
	EstablishedOn     *time.Time
	Address           string
	Lat, Lon          *decimal.Decimal
}

// RegistrationRepresentative chỉ chuyển vào kho định danh; không lưu trong sổ Neo.
type RegistrationRepresentative struct {
	FullName string `pii:"true"`
	Phone    string `pii:"true"`
}

type CooperativeRegistrar interface {
	RegisterCooperative(context.Context, CooperativeRegistration) (uuid.UUID, error)
}
