package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// KCLogin là thông tin lấy token từ Keycloak Local/Dev bằng client agrigreen-seed
// (direct access grants, CHỈ có trong realm dev — không bao giờ ở production).
// Người dùng quản trị dev tạo bằng scripts/dev-users.* và đã cấu hình TOTP một lần
// qua trình duyệt; OTP gửi kèm để token có amr = otp (Admin API bắt buộc MFA).
type KCLogin struct {
	TokenURL string // vd http://localhost:8180/realms/agrigreen/protocol/openid-connect/token
	ClientID string // mặc định agrigreen-seed
	Username string `json:"-"`
	Password string `json:"-"`
	OTP      string `json:"-"`
}

// fetchToken lấy access token bằng password grant + OTP.
func fetchToken(ctx context.Context, hc *http.Client, l KCLogin) (string, error) {
	u, err := url.Parse(l.TokenURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("-kc-token-url phải là URL http(s) tới endpoint token của realm")
	}
	if l.Username == "" || l.Password == "" {
		return "", errors.New("thiếu -kc-user hoặc AG_SEED_KC_PASSWORD")
	}
	client := l.ClientID
	if client == "" {
		client = "agrigreen-seed"
	}
	form := url.Values{"grant_type": {"password"}, "client_id": {client}, "username": {l.Username}, "password": {l.Password}, "scope": {"openid"}}
	if l.OTP != "" {
		form.Set("otp", l.OTP) // Keycloak direct grant đọc "otp" (bản cũ: "totp")
		form.Set("totp", l.OTP)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("gọi Keycloak lấy token: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	var tr struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&tr)
	if res.StatusCode != http.StatusOK || tr.AccessToken == "" {
		// Chỉ mã lỗi OAuth, không in thông tin đăng nhập.
		return "", fmt.Errorf("keycloak từ chối cấp token: HTTP %d %s", res.StatusCode, tr.Error)
	}
	return tr.AccessToken, nil
}
