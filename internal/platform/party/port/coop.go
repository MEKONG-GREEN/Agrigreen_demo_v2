package port

// W3.3 (docs/design/cooperative.md mục 3, 8.6): hồ sơ HTX, nông hộ, xã viên và sổ hồ sơ KN (kn_profile_event)
// chuyển sang module pf/cooperative (schema pf_coop, vai r_cooperative). pf/party là lõi (kernel) — KHÔNG import
// cooperativeport. Hai chiều phụ thuộc giữa hai module đi qua các interface khai ở đây:
//   - pf/party CẦN (bên tiêu thụ khai, pf/cooperative hiện thực, gốc lắp ráp nối): CoopProfiles;
//   - pf/party CUNG CẤP cho pf/cooperative (đi xuống, như Orgs/ScopeResolver): OrgDirectory, ConsentStates.

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ErrCoopTaxCodeTaken: mã số thuế đã gắn với HTX khác (chỉ mục duy nhất của hồ sơ HTX ở pf/cooperative).
// pf/party quy về problem party.tax_code_taken (tra chủ bằng CoopProfiles.TaxCodeOwner).
var ErrCoopTaxCodeTaken = errors.New("party: mã số thuế đã gắn với HTX khác")

// CoopProfile là hồ sơ HTX/THT (CGH FN-03) ở pf/cooperative — không có dữ liệu cá nhân (người đại diện là
// RepresentativeRef của Org). Tọa độ WGS84; Lat/Lon nil = chưa có (hiển thị tâm xã, nhãn "vị trí tạm").
type CoopProfile struct {
	TaxCode       string
	AreaHa        *decimal.Decimal
	EstablishedOn *time.Time
	Lat, Lon      *decimal.Decimal // numeric(10,7)
	Address       string           // trụ sở (head_office)
	UpdatedAt     time.Time        // chỉ đọc
}

// CoopProfileFilter lọc hồ sơ HTX (tìm theo mã số thuế, lọc diện tích của màn danh sách). Trường rỗng/nil = không lọc.
type CoopProfileFilter struct {
	TaxPrefix string
	AreaMin   *decimal.Decimal
	AreaMax   *decimal.Decimal
}

// Loại chủ thể của sổ hồ sơ KN (kn_profile_event, KN v7.7 FN-XA-03.03).
const (
	ProfileSubjectCooperative = "cooperative"
	ProfileSubjectHousehold   = "household"
)

// CoopProfileChange là một lần sửa hồ sơ HTX (sự kiện bù trên sổ kn_profile_event + bảng chiếu hồ sơ, cùng
// giao dịch ở pf/cooperative). Before/After là ảnh chụp JSON phẳng KHÔNG có dữ liệu cá nhân (gồm cả tên, mã
// người đại diện — phần do pf/party ghi ở pf_party.org sau bước này). AreaHa/TaxCode/Address là giá trị MỚI
// đầy đủ của hồ sơ.
type CoopProfileChange struct {
	Ref              uuid.UUID
	CommuneCode      string
	AreaHa           *decimal.Decimal
	TaxCode          string
	Address          string
	Before, After    []byte
	Reason           *string
	OnBehalfOf       *uuid.UUID
	SupportSessionID *uuid.UUID
	SupportReason    *string
	EventTime        time.Time
	ActorRef         uuid.UUID
	IdemKey          string
}

// CoopProfileEvent là một dòng sổ hồ sơ KN của HTX (khung Log).
type CoopProfileEvent struct {
	SubjectRef    uuid.UUID
	Action        string
	Before, After []byte
	Reason        *string
	SupportReason *string
	ActorRef      uuid.UUID
	EventTime     time.Time
	RecordedAt    time.Time
	Seq           int64
}

// CoopProfiles là hồ sơ HTX và số liệu xã viên mà sổ tổ chức (pf/party) cần cho màn HTX (G1, HTX US-HTX-*,
// KN FN-XA-03.03, danh bạ đối tác) — hiện thực ở pf/cooperative, gốc lắp ráp nối (W3.3). Tenant lấy từ
// context. Không kiểm vai/phạm vi — pf/party đã kiểm.
//
// Tạo HTX là HAI bước (không giao dịch nào chạy qua hai vai DB): (1) Nút kind=cooperative ở pf/party
// (sổ org_event, pf.party.org.created); (2) CreateProfile. Bước 2 lũy đẳng theo Nút: gọi lại với cùng Nút
// không tạo bản thứ hai; FK hồ sơ → Nút bảo đảm không có hồ sơ mồ côi.
type CoopProfiles interface {
	// Profiles đọc hồ sơ theo lô; Nút không có hồ sơ không có trong kết quả.
	Profiles(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]CoopProfile, error)
	// CreateProfile ghi hồ sơ của HTX vừa tạo. Đã có hồ sơ → nil, không đổi gì (lũy đẳng). Mã số thuế
	// trùng → ErrCoopTaxCodeTaken.
	CreateProfile(ctx context.Context, ref uuid.UUID, p CoopProfile) error
	// TaxCodeOwner trả HTX đang giữ mã số thuế; uuid.Nil nếu chưa có.
	TaxCodeOwner(ctx context.Context, taxCode string) (uuid.UUID, error)
	// MatchProfiles trả Nút có hồ sơ khớp bộ lọc (thứ tự bất kỳ).
	MatchProfiles(ctx context.Context, f CoopProfileFilter) ([]uuid.UUID, error)
	// ActiveHouseholds đếm nông hộ đang là thành viên (thành viên mở, hộ còn hoạt động) của từng HTX; HTX
	// không có hộ không có trong kết quả.
	ActiveHouseholds(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]int64, error)
	// UpdateProfile ghi một lần sửa hồ sơ (sổ + bảng chiếu, một giao dịch). Đã có dòng sổ cùng IdemKey →
	// nil, không ghi thêm. Mã số thuế trùng → ErrCoopTaxCodeTaken.
	UpdateProfile(ctx context.Context, c CoopProfileChange) error
	// ProfileEventByIdem đọc dòng sổ hồ sơ KN đã ghi với idem_key; nil nếu chưa có.
	ProfileEventByIdem(ctx context.Context, key string) (*CoopProfileEvent, error)
	// ProfileEvents trả sổ sửa hồ sơ của một HTX theo seq tăng dần.
	ProfileEvents(ctx context.Context, ref uuid.UUID) ([]CoopProfileEvent, error)
}

// OrgDirectory đọc tổ chức theo lô cho module tầng trên (pf/cooperative: danh sách, thống kê, marker HTX —
// hồ sơ ở pf_coop, Nút ở pf_party, không JOIN chéo schema). pf/party hiện thực. Tenant lấy từ context;
// không kiểm vai/phạm vi.
type OrgDirectory interface {
	// OrgsByRefs đọc tổ chức theo lô; ref không có bị bỏ qua.
	OrgsByRefs(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]Org, error)
	// OrgsOfKind liệt kê tổ chức theo loại, sắp theo ref; units nil = mọi đơn vị (kể cả không gắn đơn vị),
	// khác nil = chỉ tổ chức gắn một trong các đơn vị; activeOnly bỏ tổ chức đã vô hiệu hóa.
	OrgsOfKind(ctx context.Context, kind OrgKind, units []string, activeOnly bool) ([]Org, error)
}

// ConsentState là trạng thái hiện tại của một đồng ý theo mục đích (G-04).
type ConsentState struct {
	Purpose   string
	Status    string
	EventTime time.Time
}

// ConsentStates đọc bảng chiếu đồng ý của nhiều chủ thể (danh sách nông hộ của pf/cooperative, G-04). pf/party
// hiện thực. Tenant lấy từ context. Chủ thể không có bản ghi không có trong kết quả.
type ConsentStates interface {
	ConsentStates(ctx context.Context, subjects []uuid.UUID) (map[uuid.UUID][]ConsentState, error)
}
