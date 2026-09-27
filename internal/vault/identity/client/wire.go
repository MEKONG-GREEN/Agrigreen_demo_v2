package client

// Dạng JSON trên dây giữa client HTTP và cmd/identity-vault
// (contracts/openapi/identity-vault.yaml). []byte mã hóa base64 chuẩn.

// CreateRequest là thân POST /v1/subjects.
type CreateRequest struct {
	Purpose    string `json:"purpose,omitempty"`
	Kind       string `json:"kind"`
	FullName   string `json:"full_name" pii:"true"`
	Phone      string `json:"phone,omitempty" pii:"true"`
	NationalID string `json:"national_id,omitempty" pii:"true"`
	Address    string `json:"address,omitempty" pii:"true"`
	AreaCode   string `json:"area_code"`
}

// CreatedResponse là phản hồi 201 của POST /v1/subjects. Lỗi 409 vault.duplicate
// mang cùng hai trường ref và public_key trong problem.
type CreatedResponse struct {
	Ref       string `json:"ref"`
	PublicKey []byte `json:"public_key"`
}

// DisplayResponse là phản hồi GET /v1/subjects/{ref}/display.
type DisplayResponse struct {
	Ref         string `json:"ref"`
	Label       string `json:"label" pii:"true"`
	MaskedPhone string `json:"masked_phone" pii:"true"`
}

// PurposeRequest là thân POST /v1/subjects/{ref}/resolve.
type PurposeRequest struct {
	Purpose string `json:"purpose,omitempty"`
}

// IdentityResponse là phản hồi resolve.
type IdentityResponse struct {
	Ref        string `json:"ref"`
	Kind       string `json:"kind"`
	FullName   string `json:"full_name" pii:"true"`
	Phone      string `json:"phone" pii:"true"`
	NationalID string `json:"national_id" pii:"true"`
	Address    string `json:"address" pii:"true"`
	AreaCode   string `json:"area_code"`
}

// LookupRequest là thân POST /v1/subjects/lookup. Giá trị tra nằm trong thân,
// không bao giờ trong URL.
type LookupRequest struct {
	Purpose string `json:"purpose,omitempty"`
	Value   string `json:"value" pii:"true"`
}

// LookupResponse là phản hồi lookup.
type LookupResponse struct {
	Ref string `json:"ref"`
}

// UnwrapRequest là thân POST /v1/subjects/{ref}/unwrap.
type UnwrapRequest struct {
	Purpose    string `json:"purpose,omitempty"`
	WrappedDEK []byte `json:"wrapped_dek"`
}

// UnwrapResponse mang DEK đã mở — chỉ giữ trong bộ nhớ để giải mã rồi hủy (R-40).
type UnwrapResponse struct {
	DEK []byte `json:"dek"`
}

// ErasureRequest là thân POST /v1/subjects/{ref}/erasure.
type ErasureRequest struct {
	Reason string `json:"reason"`
}

// ErasureResponse là phản hồi xóa định danh.
type ErasureResponse struct {
	Ref    string `json:"ref"`
	Erased bool   `json:"erased"`
}

// PubKeyItem là một khóa công khai trong GET /v1/pubkeys.
type PubKeyItem struct {
	Ref       string `json:"ref"`
	AreaCode  string `json:"area_code"`
	PublicKey []byte `json:"public_key"`
}

// PubKeysResponse là một trang khóa công khai (cursor, R-53).
type PubKeysResponse struct {
	Items      []PubKeyItem `json:"items"`
	NextCursor *string      `json:"next_cursor"`
}
