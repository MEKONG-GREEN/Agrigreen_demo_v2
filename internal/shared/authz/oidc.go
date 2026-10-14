package authz

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
)

// Verifier xác minh access token OIDC: issuer, audience, chữ ký, hạn (R-54).
// Backend tự xác minh cả token do biên cấp — không tin header do gateway chèn.
type Verifier struct {
	v   *oidc.IDTokenVerifier
	mfa MFAPolicy
}

// NewVerifier dùng discovery của issuer (Keycloak realm).
func NewVerifier(ctx context.Context, issuer, audience string) (*Verifier, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("authz: discovery %s: %w", issuer, err)
	}
	return &Verifier{v: provider.Verifier(&oidc.Config{ClientID: audience, SupportedSigningAlgs: []string{oidc.RS256, oidc.ES256}}), mfa: DefaultMFAPolicy()}, nil
}

// NewVerifierWithKeys dùng tập khóa có sẵn — cho kiểm thử và môi trường không có discovery.
func NewVerifierWithKeys(issuer, audience string, keys oidc.KeySet) *Verifier {
	return &Verifier{v: oidc.NewVerifier(issuer, keys, &oidc.Config{ClientID: audience, SupportedSigningAlgs: []string{oidc.RS256, oidc.ES256}}), mfa: DefaultMFAPolicy()}
}

type claims struct {
	TenantID    string   `json:"tenant_id"`
	Roles       []string `json:"roles"`
	AMR         []string `json:"amr"`
	AuthTime    int64    `json:"auth_time"`
	OrgRef      string   `json:"org_ref"`
	RealmAccess struct {
		Roles []string `json:"roles"`
	} `json:"realm_access"`
}

// MFAPolicy quyết định amr nào (RFC 8176) được tính là yếu tố thứ hai. Vai nhạy cảm
// (quản trị nền tảng, vai kho định danh đọc/xóa dữ liệu cá nhân) chỉ chấp nhận yếu tố
// mạnh — SMS OTP dễ bị chiếm SIM/chuyển tiếp nên KHÔNG tính cho các vai này; vai khác giữ
// danh mục cũ (có sms).
//
// Biến môi trường (MFAPolicyFromEnv, danh sách phân tách bằng dấu phẩy):
//
//	AG_MFA_FACTORS         yếu tố thứ hai cho vai thường (mặc định otp,mfa,hwk,swk,sms)
//	AG_MFA_STRONG_FACTORS  yếu tố thứ hai cho vai nhạy cảm (mặc định otp,mfa,hwk,swk)
//	AG_MFA_STRONG_ROLES    vai nhạy cảm (mặc định platform_admin,vault_registrar,
//	                       vault_evidence_viewer,vault_eraser)
//	AG_MFA_DISABLED        1 = coi mọi phiên là đã qua yếu tố thứ hai — CHỈ máy demo. Hợp lệ khi và chỉ
//	                       khi AG_DEPLOY_ENV=demo VÀ AG_ENV đặt, khác production/prod; ngược lại
//	                       MFAPolicyFromEnv trả lỗi và tiến trình từ chối khởi động (docs/dev/sync-main-2026-10-13.md).
//	                       Chỉ bỏ kiểm amr ở backend; luồng đăng nhập TOTP của Keycloak (D24, ADR-0041) không đổi.
type MFAPolicy struct {
	// Disabled bỏ kiểm yếu tố thứ hai (Principal.MFA luôn true). Chỉ máy demo (DemoMFABypassAllowed).
	Disabled      bool
	Factors       []string // vai thường
	StrongFactors []string // khi người gọi có ít nhất một vai trong StrongRoles
	StrongRoles   []string
}

// Tên biến môi trường của MFAPolicy.
const (
	EnvMFAFactors       = "AG_MFA_FACTORS"
	EnvMFAStrongFactors = "AG_MFA_STRONG_FACTORS"
	EnvMFAStrongRoles   = "AG_MFA_STRONG_ROLES"
	EnvMFADisabled      = "AG_MFA_DISABLED"
	// EnvDeployEnv đánh dấu loại máy triển khai. Chỉ giá trị "demo" mới cho phép AG_MFA_DISABLED. Khác AG_ENV
	// (residency: production|staging|test|development|local) — demo chạy AG_ENV=staging nên AG_ENV không đủ.
	EnvDeployEnv = "AG_DEPLOY_ENV"
	// DeployEnvDemo là giá trị AG_DEPLOY_ENV của máy demo.
	DeployEnvDemo = "demo"
)

// ErrMFABypassNotDemo: AG_MFA_DISABLED bật ở nơi không phải máy demo — tiến trình phải từ chối khởi động.
var ErrMFABypassNotDemo = errors.New("authz: AG_MFA_DISABLED chỉ được bật trên máy demo")

// mfaDisabledRequested đọc AG_MFA_DISABLED: rỗng/0/false = tắt cờ; 1/true = bật; giá trị khác là lỗi (gõ sai
// không được âm thầm hiểu theo hướng nào).
func mfaDisabledRequested(getenv func(string) string) (bool, error) {
	switch v := strings.ToLower(strings.TrimSpace(getenv(EnvMFADisabled))); v {
	case "", "0", "false":
		return false, nil
	case "1", "true":
		return true, nil
	default:
		return false, fmt.Errorf("authz: %s=%q — chỉ nhận 0 hoặc 1", EnvMFADisabled, v)
	}
}

// DemoMFABypassAllowed kiểm điều kiện bật AG_MFA_DISABLED: AG_DEPLOY_ENV=demo (khớp chính xác, không phân biệt hoa
// thường) VÀ AG_ENV đặt và không phải production/prod. Thiếu biến nào cũng là từ chối — guard không "mở cửa" vì
// quên đặt biến.
func DemoMFABypassAllowed(getenv func(string) string) error {
	env := strings.ToLower(strings.TrimSpace(getenv("AG_ENV")))
	switch {
	case env == "production" || env == "prod":
		return fmt.Errorf("%w: AG_ENV=%s", ErrMFABypassNotDemo, env)
	case env == "":
		return fmt.Errorf("%w: chưa đặt AG_ENV", ErrMFABypassNotDemo)
	}
	if d := strings.ToLower(strings.TrimSpace(getenv(EnvDeployEnv))); d != DeployEnvDemo {
		return fmt.Errorf("%w: %s=%q (phải là %q)", ErrMFABypassNotDemo, EnvDeployEnv, d, DeployEnvDemo)
	}
	return nil
}

// DefaultMFAPolicy: sms chỉ được tính cho vai thường. Tên vai kho định danh lặp lại
// literal (authz là gói dùng chung, không import vault/identity/client — R-04).
func DefaultMFAPolicy() MFAPolicy {
	return MFAPolicy{
		Factors:       []string{"otp", "mfa", "hwk", "swk", "sms"},
		StrongFactors: []string{"otp", "mfa", "hwk", "swk"},
		StrongRoles:   []string{RolePlatformAdmin, "vault_registrar", "vault_evidence_viewer", "vault_eraser"},
	}
}

// MFAPolicyFromEnv đọc chính sách từ biến môi trường; biến rỗng giữ mặc định. Danh
// sách đặt ra nhưng rỗng (vd ",") bị từ chối để không vô tình tắt MFA. AG_MFA_DISABLED bật ngoài máy demo
// (DemoMFABypassAllowed) → ErrMFABypassNotDemo.
func MFAPolicyFromEnv(getenv func(string) string) (MFAPolicy, error) {
	p := DefaultMFAPolicy()
	off, err := mfaDisabledRequested(getenv)
	if err != nil {
		return MFAPolicy{}, err
	}
	if off {
		if err := DemoMFABypassAllowed(getenv); err != nil {
			return MFAPolicy{}, err
		}
		p.Disabled = true
	}
	for _, f := range []struct {
		env string
		dst *[]string
	}{{EnvMFAFactors, &p.Factors}, {EnvMFAStrongFactors, &p.StrongFactors}, {EnvMFAStrongRoles, &p.StrongRoles}} {
		raw := strings.TrimSpace(getenv(f.env))
		if raw == "" {
			continue
		}
		var list []string
		for _, v := range strings.Split(raw, ",") {
			if v = strings.ToLower(strings.TrimSpace(v)); v != "" {
				list = append(list, v)
			}
		}
		if len(list) == 0 {
			return MFAPolicy{}, fmt.Errorf("authz: %s đặt nhưng rỗng", f.env)
		}
		*f.dst = list
	}
	return p, nil
}

// WithMFAPolicy thay chính sách yếu tố thứ hai (mặc định DefaultMFAPolicy).
func (v *Verifier) WithMFAPolicy(p MFAPolicy) *Verifier {
	v.mfa = p
	return v
}

// satisfied cho biết amr có yếu tố thứ hai hợp lệ với tập vai roles.
func (p MFAPolicy) satisfied(amr, roles []string) bool {
	if p.Disabled {
		return true
	}
	factors := p.Factors
	for _, r := range roles {
		if slices.Contains(p.StrongRoles, r) {
			factors = p.StrongFactors
			break
		}
	}
	for _, m := range amr {
		if slices.Contains(factors, strings.ToLower(m)) {
			return true
		}
	}
	return false
}

// Verify xác minh token Bearer và dựng Principal.
func (v *Verifier) Verify(ctx context.Context, raw string) (Principal, error) {
	tok, err := v.v.Verify(ctx, raw)
	if err != nil {
		return Principal{}, fmt.Errorf("authz: token không hợp lệ: %w", err)
	}
	var c claims
	if err := tok.Claims(&c); err != nil {
		return Principal{}, err
	}
	actor, err := uuid.Parse(tok.Subject)
	if err != nil {
		return Principal{}, errors.New("authz: sub phải là UUID")
	}
	tid, err := uuid.Parse(c.TenantID)
	if err != nil || tid == uuid.Nil {
		return Principal{}, errors.New("authz: token thiếu claim tenant_id")
	}
	roles := append(slices.Clone(c.Roles), c.RealmAccess.Roles...)
	p := Principal{ActorRef: actor, TenantID: tid, Roles: roles, MFA: v.mfa.satisfied(c.AMR, roles)}
	if c.OrgRef != "" {
		org, err := uuid.Parse(c.OrgRef)
		if err != nil {
			return Principal{}, errors.New("authz: org_ref phải là UUID")
		}
		p.OrgRef = org
	}
	if c.AuthTime > 0 {
		p.AuthTime = time.Unix(c.AuthTime, 0).UTC()
	}
	return p, nil
}
