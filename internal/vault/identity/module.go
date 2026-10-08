// Package identity là kho định danh (mục 10, ADR-0004): chủ thể, định danh mã
// hóa, cặp khóa X25519 theo hộ, chỉ mục mù và sổ truy cập.
//
// Module ngoài vault CHỈ được import internal/vault/identity/client (R-04).
// Tệp này nối các lớp cho cmd/identity-vault — gốc lắp ráp duy nhất của vault.
package identity

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/kms"
)

// DBRole là vai DB riêng của module (R-05), trên cụm ag_vault.
const DBRole = "vault_app"

// EnvBlindKey là biến môi trường chứa khóa HMAC của chỉ mục mù (base64 chuẩn, ≥ 32 byte).
const EnvBlindKey = "AG_VAULT_BLIND_KEY"

// KMS là lớp trừu tượng KEK — production cắm KMS/HSM trong lãnh thổ.
type KMS = kms.KMS

// NewLocalKMS tạo KMS phần mềm (dev/test) từ KEK 32 byte.
func NewLocalKMS(id string, kek []byte) (KMS, error) { return kms.NewLocal(id, kek) }

// LocalKMSFromEnv đọc AG_VAULT_KEK (base64 32 byte) và AG_VAULT_KEK_ID.
func LocalKMSFromEnv() (KMS, error) { return kms.LocalFromEnv() }

// KMSFromEnv chọn hiện thực KMS theo AG_VAULT_KMS (rỗng → local) — trả kèm tên nhà cung cấp.
func KMSFromEnv() (KMS, string, error) { return kms.FromEnv(os.Getenv) }

// KMSIsSoftware cho biết k là KMS phần mềm (KEK trong bộ nhớ tiến trình).
func KMSIsSoftware(k KMS) bool { return kms.IsSoftware(k) }

// BlindKeyFromEnv đọc AG_VAULT_BLIND_KEY.
func BlindKeyFromEnv() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv(EnvBlindKey))
	if raw == "" {
		return nil, fmt.Errorf("vault: chưa đặt %s", EnvBlindKey)
	}
	k, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("vault: %s phải là base64 chuẩn", EnvBlindKey)
	}
	if len(k) < 32 {
		return nil, errors.New("vault: khóa chỉ mục mù phải từ 32 byte")
	}
	return k, nil
}

// Config là phụ thuộc của module.
type Config struct {
	DB       tenant.Beginner // pool vai app_vault (hoặc vai cũ vault_app) — mọi giao dịch chạy dưới r_identity, cả shared.idempotency_key
	Clock    clock.Clock
	KMS      KMS
	BlindKey []byte
	// Quota: hạn mức Lookup/Resolve theo người yêu cầu (QuotaFromEnv). nil = không giới hạn.
	Quota *Quota
	// RequireMFASensitive: bắt buộc MFA cho Resolve/Unwrap (như Erase).
	RequireMFASensitive bool
}

// Quota là hạn mức theo người yêu cầu của kho (app/quota.go).
type Quota = app.Quota

// QuotaFromEnv đọc AG_VAULT_QUOTA_PER_HOUR / AG_VAULT_QUOTA_BURST.
func QuotaFromEnv(clk clock.Clock, log *slog.Logger) (*Quota, error) {
	return app.QuotaFromEnv(clk, log)
}

// Module là vault/identity đã nối.
type Module struct {
	svc *app.Service
	db  tenant.Beginner
}

// New dựng module.
func New(cfg Config) (*Module, error) {
	cfg.DB = forModule(cfg.DB)
	svc := &app.Service{DB: cfg.DB, Clock: cfg.Clock, KMS: cfg.KMS, BlindKey: cfg.BlindKey, Quota: cfg.Quota,
		RequireMFASensitive: cfg.RequireMFASensitive}
	if err := svc.Validate(); err != nil {
		return nil, err
	}
	return &Module{svc: svc, db: cfg.DB}, nil
}

// RegisterApp gắn route /v1/subjects… và /v1/pubkeys vào nhóm đã xác thực.
func (m *Module) RegisterApp(rg *gin.RouterGroup) {
	handler.Handler{Svc: m.svc, Idem: idempotency.Middleware(m.db)}.Register(rg)
}

// RegisterAdmin: kho định danh không có route quản trị riêng.
func (m *Module) RegisterAdmin(*gin.RouterGroup) {}

// Client trả client.Client chạy cùng tiến trình (client.NewLocal).
func (m *Module) Client() client.Client { return client.NewLocal(m.svc) }
