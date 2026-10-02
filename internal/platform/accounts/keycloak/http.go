// Package keycloak là bộ chuyển đổi Keycloak Admin REST của pf/accounts: bản HTTP
// (tài khoản dịch vụ, client_credentials) và bản trong bộ nhớ cho kiểm thử.
//
// Không ghi thân phản hồi của Keycloak vào lỗi hay log: thông báo lỗi của Keycloak
// có thể chứa tên đăng nhập hoặc email (R-14). Lỗi chỉ mang mã trạng thái HTTP.
package keycloak

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// Config là cấu hình tài khoản dịch vụ gọi Admin REST.
type Config struct {
	// BaseURL là gốc máy chủ Keycloak, vd http://keycloak:8080 (không kèm /realms).
	BaseURL      string
	Realm        string
	ClientID     string
	ClientSecret string `json:"-"`
	// Timeout mỗi request (mặc định 10 giây).
	Timeout time.Duration
	// ActionsClientID/ActionsRedirectURI: client và địa chỉ quay về sau khi người
	// dùng hoàn tất liên kết kích hoạt (tùy chọn).
	ActionsClientID    string
	ActionsRedirectURI string
	// ActionLinkURL: điểm cuối của phần mở rộng realm phát liên kết hành động một lần
	// KHÔNG gửi email (tài khoản nông dân, g2-conventions §1). Rỗng → ActionLink trả
	// port.ErrActionLinkUnsupported. Gọi bằng token tài khoản dịch vụ; thân POST
	// {user_id, grant_id, actions, lifespan, client_id, redirect_uri} → {link}; phần mở
	// rộng gắn token với grant_id và từ chối token của lượt cũ (port.ActionLinks).
	ActionLinkURL string
	// HTTP thay http.Client mặc định (kiểm thử).
	HTTP *http.Client
	// Now thay đồng hồ hệ thống khi tính hạn token (kiểm thử).
	Now func() time.Time
}

// ConfigFromEnv đọc AG_KC_ADMIN_URL, AG_KC_REALM, AG_KC_CLIENT_ID, AG_KC_CLIENT_SECRET
// (bắt buộc cùng nhau), AG_KC_TIMEOUT (vd 10s), AG_KC_ACTIONS_CLIENT_ID,
// AG_KC_ACTIONS_REDIRECT_URI, AG_KC_ACTION_LINK_URL. ok=false khi không đặt AG_KC_ADMIN_URL.
func ConfigFromEnv() (cfg Config, ok bool, err error) {
	cfg = Config{
		BaseURL: os.Getenv("AG_KC_ADMIN_URL"), Realm: os.Getenv("AG_KC_REALM"),
		ClientID: os.Getenv("AG_KC_CLIENT_ID"), ClientSecret: os.Getenv("AG_KC_CLIENT_SECRET"),
		ActionsClientID: os.Getenv("AG_KC_ACTIONS_CLIENT_ID"), ActionsRedirectURI: os.Getenv("AG_KC_ACTIONS_REDIRECT_URI"),
		ActionLinkURL: os.Getenv("AG_KC_ACTION_LINK_URL"),
	}
	if cfg.BaseURL == "" {
		return cfg, false, nil
	}
	if cfg.Realm == "" || cfg.ClientID == "" || cfg.ClientSecret == "" {
		return cfg, false, errors.New("keycloak: đặt AG_KC_ADMIN_URL thì phải đặt đủ AG_KC_REALM, AG_KC_CLIENT_ID, AG_KC_CLIENT_SECRET")
	}
	if s := os.Getenv("AG_KC_TIMEOUT"); s != "" {
		d, perr := time.ParseDuration(s)
		if perr != nil || d <= 0 {
			return cfg, false, errors.New("keycloak: AG_KC_TIMEOUT phải là khoảng thời gian dương, vd 10s")
		}
		cfg.Timeout = d
	}
	return cfg, true, nil
}

// Client là Directory qua HTTP. An toàn khi dùng đồng thời.
type Client struct {
	cfg  Config
	hc   *http.Client
	base string // {BaseURL}/admin/realms/{realm}

	mu    sync.Mutex
	token string
	exp   time.Time
}

var (
	_ port.Directory   = (*Client)(nil)
	_ port.ActionLinks = (*Client)(nil)
)

// New tạo client. Không gọi mạng — token lấy ở lần gọi đầu và được lưu tới gần hạn.
func New(cfg Config) (*Client, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("keycloak: BaseURL phải là http(s)://máy[:cổng]")
	}
	if cfg.Realm == "" || cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, errors.New("keycloak: thiếu realm, client_id hoặc client_secret")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	hc := cfg.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: cfg.Timeout}
	}
	root := strings.TrimRight(cfg.BaseURL, "/")
	return &Client{cfg: cfg, hc: hc, base: root + "/admin/realms/" + url.PathEscape(cfg.Realm)}, nil
}

// statusError là phản hồi không thành công (không mang thân phản hồi, R-14).
type statusError struct {
	op     string
	status int
}

func (e *statusError) Error() string { return fmt.Sprintf("keycloak: %s: HTTP %d", e.op, e.status) }

// ---------------------------------------------------------------------------
// Token tài khoản dịch vụ
// ---------------------------------------------------------------------------

func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && c.cfg.Now().Before(c.exp) {
		return c.token, nil
	}
	form := url.Values{"grant_type": {"client_credentials"}}
	tokenURL := strings.TrimRight(c.cfg.BaseURL, "/") + "/realms/" + url.PathEscape(c.cfg.Realm) + "/protocol/openid-connect/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(url.QueryEscape(c.cfg.ClientID), url.QueryEscape(c.cfg.ClientSecret))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := c.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: token: %w", port.ErrUnavailable, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
		return "", fmt.Errorf("%w: %w", port.ErrUnavailable, &statusError{op: "token", status: res.StatusCode})
	}
	var tr struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&tr); err != nil || tr.AccessToken == "" {
		return "", fmt.Errorf("%w: token không đọc được", port.ErrUnavailable)
	}
	life := time.Duration(tr.ExpiresIn) * time.Second
	if life <= 0 {
		life = time.Minute
	}
	// Làm mới sớm 30 giây (hoặc nửa thời hạn nếu token sống ngắn).
	margin := min(30*time.Second, life/2)
	c.token, c.exp = tr.AccessToken, c.cfg.Now().Add(life-margin)
	return c.token, nil
}

func (c *Client) dropToken() {
	c.mu.Lock()
	c.token = ""
	c.mu.Unlock()
}

// do gọi Admin REST; 401 → lấy token mới và thử lại một lần.
func (c *Client) do(ctx context.Context, op, method, rel string, query url.Values, body any, out any) (http.Header, error) {
	var raw []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		raw = b
	}
	for attempt := 0; ; attempt++ {
		tok, err := c.accessToken(ctx)
		if err != nil {
			return nil, err
		}
		u := c.base + rel
		if len(query) > 0 {
			u += "?" + query.Encode()
		}
		var rd io.Reader
		if raw != nil {
			rd = bytes.NewReader(raw)
		}
		req, err := http.NewRequestWithContext(ctx, method, u, rd)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Accept", "application/json")
		if raw != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := c.hc.Do(req)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %w", port.ErrUnavailable, op, err)
		}
		if res.StatusCode == http.StatusUnauthorized && attempt == 0 {
			_ = res.Body.Close()
			c.dropToken()
			continue
		}
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode >= 200 && res.StatusCode < 300 {
			if out != nil && res.StatusCode != http.StatusNoContent {
				if err := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(out); err != nil {
					return res.Header, fmt.Errorf("keycloak: %s: phản hồi không đọc được", op)
				}
			}
			return res.Header, nil
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
		se := &statusError{op: op, status: res.StatusCode}
		switch {
		case res.StatusCode == http.StatusNotFound:
			return nil, fmt.Errorf("%w: %w", port.ErrUserNotFound, se)
		case res.StatusCode == http.StatusConflict:
			return nil, fmt.Errorf("%w: %w", port.ErrUserExists, se)
		case res.StatusCode >= 500 || res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
			return nil, fmt.Errorf("%w: %w", port.ErrUnavailable, se)
		}
		return nil, se
	}
}

// ---------------------------------------------------------------------------
// Biểu diễn người dùng
// ---------------------------------------------------------------------------

type userRep struct {
	ID               string              `json:"id,omitempty"`
	Username         string              `json:"username,omitempty" pii:"true"`
	Email            string              `json:"email,omitempty" pii:"true"`
	FirstName        string              `json:"firstName,omitempty" pii:"true"`
	LastName         string              `json:"lastName,omitempty" pii:"true"`
	Enabled          bool                `json:"enabled"`
	EmailVerified    bool                `json:"emailVerified"`
	Attributes       map[string][]string `json:"attributes,omitempty" pii:"true"`
	RequiredActions  []string            `json:"requiredActions,omitempty"`
	CreatedTimestamp int64               `json:"createdTimestamp,omitempty"`
}

func first(a map[string][]string, k string) string {
	if v := a[k]; len(v) > 0 {
		return v[0]
	}
	return ""
}

func (r userRep) toUser() (port.User, error) {
	id, err := uuid.Parse(r.ID)
	if err != nil {
		return port.User{}, errors.New("keycloak: id người dùng không phải UUID")
	}
	u := port.User{
		ID: id, Username: r.Username, Email: r.Email, FirstName: r.FirstName, LastName: r.LastName,
		Phone: first(r.Attributes, port.AttrPhone), Enabled: r.Enabled, EmailVerified: r.EmailVerified,
		RequiredActions: slices.Clone(r.RequiredActions),
	}
	u.TenantID, _ = uuid.Parse(first(r.Attributes, port.AttrTenantID))
	u.OrgRef, _ = uuid.Parse(first(r.Attributes, port.AttrOrgRef))
	if r.CreatedTimestamp > 0 {
		u.CreatedAt = time.UnixMilli(r.CreatedTimestamp).UTC()
	}
	return u, nil
}

func userPath(id uuid.UUID) string { return "/users/" + id.String() }

// CreateUser tạo người dùng đã bật, kèm thuộc tính tenant_id/org_ref và required actions.
func (c *Client) CreateUser(ctx context.Context, nu port.NewUser) (uuid.UUID, error) {
	attrs := map[string][]string{port.AttrTenantID: {nu.TenantID.String()}}
	if nu.OrgRef != uuid.Nil { // tài khoản nông dân không có đơn vị công tác
		attrs[port.AttrOrgRef] = []string{nu.OrgRef.String()}
	}
	if nu.Phone != "" {
		attrs[port.AttrPhone] = []string{nu.Phone}
	}
	rep := userRep{
		Username: nu.Username, Email: nu.Email, FirstName: nu.FirstName, LastName: nu.LastName,
		Enabled: true, Attributes: attrs, RequiredActions: nu.RequiredActions,
	}
	hdr, err := c.do(ctx, "tạo người dùng", http.MethodPost, "/users", nil, rep, nil)
	if err != nil {
		return uuid.Nil, err
	}
	loc := hdr.Get("Location")
	id, err := uuid.Parse(path.Base(loc))
	if loc == "" || err != nil {
		return uuid.Nil, errors.New("keycloak: tạo người dùng: thiếu Location hợp lệ")
	}
	return id, nil
}

// GetUser đọc một người dùng.
func (c *Client) GetUser(ctx context.Context, id uuid.UUID) (port.User, error) {
	var rep userRep
	if _, err := c.do(ctx, "đọc người dùng", http.MethodGet, userPath(id), nil, nil, &rep); err != nil {
		return port.User{}, err
	}
	return rep.toUser()
}

// FindUsers tìm người dùng theo thuộc tính (so khớp chính xác), tên đăng nhập hoặc email.
func (c *Client) FindUsers(ctx context.Context, q port.Query) ([]port.User, error) {
	v := url.Values{"briefRepresentation": {"false"}}
	if len(q.Attributes) > 0 {
		keys := make([]string, 0, len(q.Attributes))
		for k := range q.Attributes {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, k+":"+q.Attributes[k])
		}
		v.Set("q", strings.Join(parts, " "))
	}
	if q.Username != "" {
		v.Set("username", q.Username)
		v.Set("exact", "true")
	}
	if q.Email != "" {
		v.Set("email", q.Email)
		v.Set("exact", "true")
	}
	if q.First > 0 {
		v.Set("first", strconv.Itoa(q.First))
	}
	maxN := q.Max
	if maxN <= 0 {
		maxN = 100
	}
	v.Set("max", strconv.Itoa(maxN))
	var reps []userRep
	if _, err := c.do(ctx, "tìm người dùng", http.MethodGet, "/users", v, nil, &reps); err != nil {
		return nil, err
	}
	out := make([]port.User, 0, len(reps))
	for _, r := range reps {
		u, err := r.toUser()
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}

// DeleteUser xóa người dùng (chỉ để bù khi tạo thất bại giữa chừng).
func (c *Client) DeleteUser(ctx context.Context, id uuid.UUID) error {
	_, err := c.do(ctx, "xóa người dùng", http.MethodDelete, userPath(id), nil, nil, nil)
	return err
}

// update đọc biểu diễn ĐẦY ĐỦ, sửa rồi PUT lại — PUT một phần có thể xóa thuộc tính
// khi realm dùng declarative user profile.
func (c *Client) update(ctx context.Context, id uuid.UUID, op string, fn func(m map[string]any)) error {
	var m map[string]any
	if _, err := c.do(ctx, op, http.MethodGet, userPath(id), nil, nil, &m); err != nil {
		return err
	}
	fn(m)
	_, err := c.do(ctx, op, http.MethodPut, userPath(id), nil, m, nil)
	return err
}

// SetEnabled bật/tắt tài khoản.
func (c *Client) SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) error {
	return c.update(ctx, id, "đổi trạng thái", func(m map[string]any) { m["enabled"] = enabled })
}

// SetOrgRef đổi thuộc tính org_ref.
func (c *Client) SetOrgRef(ctx context.Context, id uuid.UUID, org uuid.UUID) error {
	return c.update(ctx, id, "đổi đơn vị", func(m map[string]any) {
		attrs, _ := m["attributes"].(map[string]any)
		if attrs == nil {
			attrs = map[string]any{}
		}
		attrs[port.AttrOrgRef] = []string{org.String()}
		m["attributes"] = attrs
	})
}

// UpdateProfile thực hiện port.ProfileUpdater (KN v7.7 FN-AD-04.04): họ tên, email, SĐT và tên đăng
// nhập (đi theo SĐT/email). Đổi email → emailVerified = false (Keycloak gửi lại VERIFY_EMAIL khi cần).
func (c *Client) UpdateProfile(ctx context.Context, id uuid.UUID, p port.ProfileUpdate) error {
	return c.update(ctx, id, "sửa thông tin", func(m map[string]any) {
		if old, _ := m["email"].(string); !strings.EqualFold(old, p.Email) {
			m["emailVerified"] = false
		}
		if p.Username != "" {
			m["username"] = p.Username
		}
		m["email"], m["firstName"], m["lastName"] = p.Email, p.FirstName, p.LastName
		attrs, _ := m["attributes"].(map[string]any)
		if attrs == nil {
			attrs = map[string]any{}
		}
		if p.Phone == "" {
			delete(attrs, port.AttrPhone)
		} else {
			attrs[port.AttrPhone] = []string{p.Phone}
		}
		m["attributes"] = attrs
	})
}

var _ port.ProfileUpdater = (*Client)(nil)

// AddRequiredActions thêm required action (không trùng).
func (c *Client) AddRequiredActions(ctx context.Context, id uuid.UUID, actions []string) error {
	return c.update(ctx, id, "thêm required action", func(m map[string]any) {
		var cur []string
		if list, ok := m["requiredActions"].([]any); ok {
			for _, a := range list {
				if s, ok := a.(string); ok {
					cur = append(cur, s)
				}
			}
		}
		for _, a := range actions {
			if !slices.Contains(cur, a) {
				cur = append(cur, a)
			}
		}
		m["requiredActions"] = cur
	})
}

// ---------------------------------------------------------------------------
// Vai realm
// ---------------------------------------------------------------------------

type roleRep struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (c *Client) mapped(ctx context.Context, id uuid.UUID) ([]roleRep, error) {
	var reps []roleRep
	_, err := c.do(ctx, "đọc vai", http.MethodGet, userPath(id)+"/role-mappings/realm", nil, nil, &reps)
	return reps, err
}

// RealmRoles trả vai realm gán trực tiếp cho người dùng.
func (c *Client) RealmRoles(ctx context.Context, id uuid.UUID) ([]string, error) {
	reps, err := c.mapped(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(reps))
	for _, r := range reps {
		out = append(out, r.Name)
	}
	slices.Sort(out)
	return out, nil
}

// AddRealmRoles gán vai realm. Dùng danh sách "available" của chính người dùng
// (quyền view-users đủ, không cần view-realm).
func (c *Client) AddRealmRoles(ctx context.Context, id uuid.UUID, roles []string) error {
	if len(roles) == 0 {
		return nil
	}
	var avail []roleRep
	if _, err := c.do(ctx, "đọc vai có thể gán", http.MethodGet, userPath(id)+"/role-mappings/realm/available", nil, nil, &avail); err != nil {
		return err
	}
	have, err := c.RealmRoles(ctx, id)
	if err != nil {
		return err
	}
	var add []roleRep
	for _, name := range roles {
		if slices.Contains(have, name) {
			continue
		}
		i := slices.IndexFunc(avail, func(r roleRep) bool { return r.Name == name })
		if i < 0 {
			return fmt.Errorf("%w: %s", port.ErrRoleNotFound, name)
		}
		add = append(add, avail[i])
	}
	if len(add) == 0 {
		return nil
	}
	_, err = c.do(ctx, "gán vai", http.MethodPost, userPath(id)+"/role-mappings/realm", nil, add, nil)
	return err
}

// RemoveRealmRoles thu hồi vai realm (vai không có thì bỏ qua).
func (c *Client) RemoveRealmRoles(ctx context.Context, id uuid.UUID, roles []string) error {
	if len(roles) == 0 {
		return nil
	}
	cur, err := c.mapped(ctx, id)
	if err != nil {
		return err
	}
	var del []roleRep
	for _, r := range cur {
		if slices.Contains(roles, r.Name) {
			del = append(del, r)
		}
	}
	if len(del) == 0 {
		return nil
	}
	_, err = c.do(ctx, "thu hồi vai", http.MethodDelete, userPath(id)+"/role-mappings/realm", nil, del, nil)
	return err
}

// HasOTP cho biết người dùng đã cấu hình TOTP.
func (c *Client) HasOTP(ctx context.Context, id uuid.UUID) (bool, error) {
	var creds []struct {
		Type string `json:"type"`
	}
	if _, err := c.do(ctx, "đọc thông tin xác thực", http.MethodGet, userPath(id)+"/credentials", nil, nil, &creds); err != nil {
		return false, err
	}
	return slices.ContainsFunc(creds, func(c struct {
		Type string `json:"type"`
	}) bool {
		return c.Type == "otp"
	}), nil
}

// ExecuteActionsEmail gửi liên kết một lần (ADR-0010: không gửi mật khẩu tạm).
func (c *Client) ExecuteActionsEmail(ctx context.Context, id uuid.UUID, actions []string, lifespan time.Duration) error {
	v := url.Values{}
	if lifespan > 0 {
		v.Set("lifespan", strconv.Itoa(int(lifespan/time.Second)))
	}
	if c.cfg.ActionsClientID != "" {
		v.Set("client_id", c.cfg.ActionsClientID)
		if c.cfg.ActionsRedirectURI != "" {
			v.Set("redirect_uri", c.cfg.ActionsRedirectURI)
		}
	}
	_, err := c.do(ctx, "gửi email hành động", http.MethodPut, userPath(id)+"/execute-actions-email", v, actions, nil)
	var se *statusError
	if err != nil && !errors.Is(err, port.ErrUserNotFound) && errors.As(err, &se) && se.status != http.StatusUnauthorized && se.status != http.StatusForbidden {
		// Keycloak trả 500 khi SMTP lỗi, 400 khi người dùng chưa có email hoặc đã khóa.
		return fmt.Errorf("%w: %w", port.ErrEmailFailed, se)
	}
	return err
}

// ActionLink phát liên kết hành động một lần qua phần mở rộng realm (ActionLinkURL).
// Thân phản hồi chứa token: không bao giờ đưa vào lỗi hay log (R-14) — lỗi chỉ mang mã HTTP.
func (c *Client) ActionLink(ctx context.Context, id, grantID uuid.UUID, actions []string, lifespan time.Duration) (string, error) {
	if c.cfg.ActionLinkURL == "" {
		return "", port.ErrActionLinkUnsupported
	}
	body, err := json.Marshal(map[string]any{
		"user_id": id.String(), "grant_id": grantID.String(), "actions": actions, "lifespan": int(lifespan / time.Second),
		"client_id": c.cfg.ActionsClientID, "redirect_uri": c.cfg.ActionsRedirectURI,
	})
	if err != nil {
		return "", err
	}
	for attempt := 0; ; attempt++ {
		tok, err := c.accessToken(ctx)
		if err != nil {
			return "", err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.ActionLinkURL, bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		res, err := c.hc.Do(req)
		if err != nil {
			return "", fmt.Errorf("%w: phát liên kết hành động", port.ErrUnavailable)
		}
		if res.StatusCode == http.StatusUnauthorized && attempt == 0 {
			_ = res.Body.Close()
			c.dropToken()
			continue
		}
		defer func() { _ = res.Body.Close() }()
		se := &statusError{op: "phát liên kết hành động", status: res.StatusCode}
		switch {
		case res.StatusCode == http.StatusNotFound:
			_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
			return "", fmt.Errorf("%w: %w", port.ErrUserNotFound, se)
		case res.StatusCode >= 500 || res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
			_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
			return "", fmt.Errorf("%w: %w", port.ErrUnavailable, se)
		case res.StatusCode < 200 || res.StatusCode >= 300:
			_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
			return "", se
		}
		var out struct {
			Link string `json:"link"`
		}
		if err := json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&out); err != nil {
			return "", errors.New("keycloak: phát liên kết hành động: phản hồi không đọc được")
		}
		u, err := url.Parse(out.Link)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return "", errors.New("keycloak: phát liên kết hành động: liên kết không hợp lệ")
		}
		return out.Link, nil
	}
}

// Logout thu hồi mọi phiên của người dùng.
func (c *Client) Logout(ctx context.Context, id uuid.UUID) error {
	_, err := c.do(ctx, "thu hồi phiên", http.MethodPost, userPath(id)+"/logout", nil, nil, nil)
	return err
}

func bruteForcePath(id uuid.UUID) string { return "/attack-detection/brute-force/users/" + id.String() }

// BruteForceLocked đọc GET /attack-detection/brute-force/users/{id} (trường "disabled").
func (c *Client) BruteForceLocked(ctx context.Context, id uuid.UUID) (bool, error) {
	var st struct {
		Disabled bool `json:"disabled"`
	}
	_, err := c.do(ctx, "đọc trạng thái khóa đăng nhập sai", http.MethodGet, bruteForcePath(id), nil, nil, &st)
	return st.Disabled, err
}

// ClearBruteForce gọi DELETE /attack-detection/brute-force/users/{id} (Admin mở khóa).
func (c *Client) ClearBruteForce(ctx context.Context, id uuid.UUID) error {
	_, err := c.do(ctx, "mở khóa đăng nhập sai", http.MethodDelete, bruteForcePath(id), nil, nil, nil)
	return err
}
