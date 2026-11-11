package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// keycloakID là Identity trên Keycloak Local/CI: tạo quản trị gốc của tenant KN mới trong realm (mật khẩu + TOTP ngẫu
// nhiên, chỉ giữ trong bộ nhớ) và đăng nhập bằng client CHỈ-DEV có direct access grants (kn-seed). Không in mật khẩu,
// OTP hay token. Không dùng ở staging/production (main.go từ chối).
type keycloakID struct {
	base, realm, client string // base: http://127.0.0.1:8180
	adminPassword       string // tài khoản bootstrap "admin" của realm master (deploy/compose/.env)
	hc                  *http.Client
	tenant              string
}

func secret(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// totp: RFC 6238, HMAC-SHA1, 6 số, bước 30 giây — khớp otpPolicy của realm.
func totp(key string, at time.Time) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(at.Unix()/30))
	mac := hmac.New(sha1.New, []byte(key))
	_, _ = mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[off:off+4])&0x7fffffff)%1000000)
}

// call gọi Keycloak; form != nil → x-www-form-urlencoded, ngược lại JSON. Lỗi chỉ mang mã HTTP và mã lỗi OAuth.
func (k *keycloakID) call(ctx context.Context, method, path, bearer string, form url.Values, body, out any) error {
	var rd io.Reader
	ctype := "application/json"
	if form != nil {
		rd, ctype = strings.NewReader(form.Encode()), "application/x-www-form-urlencoded"
	} else if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, k.base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", ctype)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res, err := k.hc.Do(req)
	if err != nil {
		return fmt.Errorf("keycloak %s %s: %w", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		return fmt.Errorf("keycloak %s %s: HTTP %d %s", method, path, res.StatusCode, e.Error)
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

type tokenResp struct {
	AccessToken string `json:"access_token"`
}

func (k *keycloakID) adminToken(ctx context.Context) (string, error) {
	var t tokenResp
	err := k.call(ctx, http.MethodPost, "/realms/master/protocol/openid-connect/token", "", url.Values{
		"grant_type": {"password"}, "client_id": {"admin-cli"}, "username": {"admin"}, "password": {k.adminPassword}}, nil, &t)
	return t.AccessToken, err
}

// login lấy token người dùng ở realm bằng mật khẩu (+ OTP nếu có).
func (k *keycloakID) login(ctx context.Context, username, password, otpKey string) (string, error) {
	form := url.Values{"grant_type": {"password"}, "client_id": {k.client}, "username": {username}, "password": {password}, "scope": {"openid"}}
	if otpKey != "" {
		code := totp(otpKey, time.Now())
		form.Set("otp", code)
		form.Set("totp", code)
	}
	var t tokenResp
	if err := k.call(ctx, http.MethodPost, "/realms/"+k.realm+"/protocol/openid-connect/token", "", form, nil, &t); err != nil {
		return "", err
	}
	if t.AccessToken == "" {
		return "", errors.New("keycloak không trả access_token")
	}
	return t.AccessToken, nil
}

// RootToken tạo quản trị gốc (platform_admin) của một tenant MỚI rồi đăng nhập bằng mật khẩu + TOTP.
func (k *keycloakID) RootToken(ctx context.Context) (string, error) {
	admin, err := k.adminToken(ctx)
	if err != nil {
		return "", err
	}
	k.tenant = uuid.NewString()
	username, password, otpKey := "kn-root-"+strings.ToLower(secret(6)), secret(24)+"#7a", secret(24)
	users := "/admin/realms/" + k.realm + "/users"
	if err := k.call(ctx, http.MethodPost, users, admin, nil, map[string]any{
		"username": username, "enabled": true, "emailVerified": true, "email": username + "@example.test",
		"firstName": "Quản trị", "lastName": "Kiểm Khói", "attributes": map[string]any{"tenant_id": []string{k.tenant}},
		"credentials": []any{
			map[string]any{"type": "password", "value": password, "temporary": false},
			map[string]any{"type": "otp", "userLabel": "knsmoke", "secretData": `{"value":"` + otpKey + `"}`,
				"credentialData": `{"subType":"totp","digits":6,"counter":0,"period":30,"algorithm":"HmacSHA1"}`},
		}}, nil); err != nil {
		return "", err
	}
	var found []struct {
		ID string `json:"id"`
	}
	if err := k.call(ctx, http.MethodGet, users+"?exact=true&username="+url.QueryEscape(username), admin, nil, nil, &found); err != nil || len(found) != 1 {
		return "", fmt.Errorf("tra quản trị gốc vừa tạo: %w", err)
	}
	var role map[string]any
	if err := k.call(ctx, http.MethodGet, "/admin/realms/"+k.realm+"/roles/platform_admin", admin, nil, nil, &role); err != nil {
		return "", err
	}
	if err := k.call(ctx, http.MethodPost, users+"/"+found[0].ID+"/role-mappings/realm", admin, nil, []any{role}, nil); err != nil {
		return "", err
	}
	return k.login(ctx, username, password, otpKey)
}

// UserToken kích hoạt tài khoản KN mà pf/accounts vừa tạo trong realm (thay bước người dùng bấm liên kết kích hoạt:
// đặt mật khẩu, gỡ required actions) rồi đăng nhập. Tài khoản phải thuộc đúng tenant của lần chạy này.
func (k *keycloakID) UserToken(ctx context.Context, userID string) (string, error) {
	admin, err := k.adminToken(ctx)
	if err != nil {
		return "", err
	}
	path := "/admin/realms/" + k.realm + "/users/" + url.PathEscape(userID)
	var rep map[string]any
	if err := k.call(ctx, http.MethodGet, path, admin, nil, nil, &rep); err != nil {
		return "", err
	}
	attrs, _ := rep["attributes"].(map[string]any)
	if t, _ := attrs["tenant_id"].([]any); len(t) != 1 || t[0] != k.tenant {
		return "", errors.New("tài khoản KN không thuộc tenant của lần chạy này")
	}
	username, _ := rep["username"].(string)
	rep["requiredActions"], rep["emailVerified"] = []string{}, true
	if err := k.call(ctx, http.MethodPut, path, admin, nil, rep, nil); err != nil {
		return "", err
	}
	password := secret(24) + "#7a"
	if err := k.call(ctx, http.MethodPut, path+"/reset-password", admin, nil,
		map[string]any{"type": "password", "value": password, "temporary": false}, nil); err != nil {
		return "", err
	}
	return k.login(ctx, username, password, "")
}
