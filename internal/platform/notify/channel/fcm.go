package channel

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
)

// Mặc định của FCM HTTP v1.
const (
	DefaultFCMEndpoint = "https://fcm.googleapis.com"
	DefaultFCMTokenURL = "https://oauth2.googleapis.com/token" //nolint:gosec // URL máy chủ token OAuth2, không phải bí mật
	fcmScope           = "https://www.googleapis.com/auth/firebase.messaging"
)

// FCMConfig là cấu hình FCM HTTP v1 (tài khoản dịch vụ Google).
type FCMConfig struct {
	ProjectID   string
	ClientEmail string
	PrivateKey  *rsa.PrivateKey
	Endpoint    string // rỗng = DefaultFCMEndpoint
	TokenURL    string // rỗng = DefaultFCMTokenURL (hoặc token_uri của tệp khóa)
	HTTP        *http.Client
}

// FCM gửi thông điệp dữ liệu qua FCM HTTP v1. Payload CHỈ có "data" (kind,
// notification_id) — không có khối "notification", không tiêu đề/nội dung (RB-7).
type FCM struct {
	cfg FCMConfig

	mu     sync.Mutex
	access string
	exp    time.Time
}

// NewFCM kiểm cấu hình và dựng kênh.
func NewFCM(cfg FCMConfig) (*FCM, error) {
	if cfg.ProjectID == "" || cfg.ClientEmail == "" || cfg.PrivateKey == nil {
		return nil, errors.New("channel: FCM thiếu project_id, client_email hoặc private_key")
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = DefaultFCMEndpoint
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = DefaultFCMTokenURL
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 10 * time.Second}
	}
	return &FCM{cfg: cfg}, nil
}

// serviceAccount là phần cần dùng của tệp khóa tài khoản dịch vụ.
type serviceAccount struct {
	ProjectID   string `json:"project_id"`
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

// FCMFromEnv đọc AG_FCM_PROJECT_ID, AG_FCM_CREDENTIALS_FILE (JSON tài khoản dịch vụ),
// AG_FCM_ENDPOINT và AG_FCM_TOKEN_URL (tùy chọn, để thử nghiệm).
func FCMFromEnv() (*FCM, error) {
	path := os.Getenv("AG_FCM_CREDENTIALS_FILE")
	if path == "" {
		return nil, errors.New("channel: thiếu AG_FCM_CREDENTIALS_FILE")
	}
	raw, err := os.ReadFile(path) //nolint:gosec // đường dẫn do người vận hành cấu hình
	if err != nil {
		return nil, fmt.Errorf("channel: đọc tệp khóa FCM: %w", err)
	}
	cfg, err := ParseServiceAccount(raw)
	if err != nil {
		return nil, err
	}
	if p := os.Getenv("AG_FCM_PROJECT_ID"); p != "" {
		cfg.ProjectID = p
	}
	if e := os.Getenv("AG_FCM_ENDPOINT"); e != "" {
		cfg.Endpoint = e
	}
	if t := os.Getenv("AG_FCM_TOKEN_URL"); t != "" {
		cfg.TokenURL = t
	}
	return NewFCM(cfg)
}

// ParseServiceAccount đọc tệp khóa JSON của tài khoản dịch vụ Google.
func ParseServiceAccount(raw []byte) (FCMConfig, error) {
	var sa serviceAccount
	if err := json.Unmarshal(raw, &sa); err != nil {
		return FCMConfig{}, errors.New("channel: tệp khóa FCM không phải JSON hợp lệ")
	}
	block, _ := pem.Decode([]byte(sa.PrivateKey))
	if block == nil {
		return FCMConfig{}, errors.New("channel: private_key của FCM không phải PEM")
	}
	var key *rsa.PrivateKey
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rk, ok := k.(*rsa.PrivateKey)
		if !ok {
			return FCMConfig{}, errors.New("channel: private_key của FCM phải là RSA")
		}
		key = rk
	} else if rk, err2 := x509.ParsePKCS1PrivateKey(block.Bytes); err2 == nil {
		key = rk
	} else {
		return FCMConfig{}, errors.New("channel: không đọc được private_key của FCM")
	}
	return FCMConfig{ProjectID: sa.ProjectID, ClientEmail: sa.ClientEmail, PrivateKey: key, TokenURL: sa.TokenURI}, nil
}

// Name thực hiện Pusher.
func (f *FCM) Name() string { return "fcm" }

// fcmRequest là thân của messages:send — chỉ token + data + cờ ưu tiên/nền.
type fcmRequest struct {
	Message fcmMessage `json:"message"`
}

type fcmMessage struct {
	Token   string            `json:"token"`
	Data    map[string]string `json:"data"`
	Android fcmAndroid        `json:"android"`
	APNS    fcmAPNS           `json:"apns"`
}

type fcmAndroid struct {
	Priority string `json:"priority"`
	TTL      string `json:"ttl"`
}

type fcmAPNS struct {
	Headers map[string]string `json:"headers"`
	Payload map[string]any    `json:"payload"`
}

// BuildFCMRequest dựng thân JSON gửi FCM. Xuất ra để kiểm thử khẳng định không có
// tiêu đề, nội dung hay dữ liệu cá nhân (RB-7).
func BuildFCMRequest(m domain.PushMessage) ([]byte, error) {
	prio := "NORMAL"
	if m.Urgent || m.Kind == "sync_now" {
		prio = "HIGH"
	}
	req := fcmRequest{Message: fcmMessage{
		Token:   m.Token,
		Data:    m.Data(),
		Android: fcmAndroid{Priority: prio, TTL: "86400s"},
		// iOS: thông điệp nền (content-available), không hiển thị gì từ máy chủ đẩy.
		APNS: fcmAPNS{
			Headers: map[string]string{"apns-push-type": "background", "apns-priority": "5"},
			Payload: map[string]any{"aps": map[string]any{"content-available": 1}},
		},
	}}
	return json.Marshal(req)
}

// Send thực hiện Pusher.
func (f *FCM) Send(ctx context.Context, m domain.PushMessage) error {
	body, err := BuildFCMRequest(m)
	if err != nil {
		return err
	}
	tok, err := f.accessToken(ctx)
	if err != nil {
		return err
	}
	u := strings.TrimRight(f.cfg.Endpoint, "/") + "/v1/projects/" + url.PathEscape(f.cfg.ProjectID) + "/messages:send"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.cfg.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("channel: gọi FCM: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	switch {
	case resp.StatusCode == http.StatusOK:
		return nil
	case resp.StatusCode == http.StatusUnauthorized:
		f.mu.Lock()
		f.access = ""
		f.mu.Unlock()
		return errors.New("channel: FCM 401 — làm mới access token")
	case fcmTokenError(raw):
		return ErrTokenInvalid
	}
	return fmt.Errorf("channel: FCM trả %d", resp.StatusCode)
}

// fcmTokenError nhận ra lỗi token đã gỡ/hết hạn trong thân lỗi FCM (errorCode
// UNREGISTERED). Lỗi khác (sai project, sai định dạng) KHÔNG làm ngừng token — tránh
// tắt hàng loạt token vì lỗi cấu hình.
func fcmTokenError(raw []byte) bool {
	var e struct {
		Error struct {
			Status  string `json:"status"`
			Details []struct {
				ErrorCode string `json:"errorCode"`
			} `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &e) != nil {
		return false
	}
	for _, d := range e.Error.Details {
		if d.ErrorCode == "UNREGISTERED" {
			return true
		}
	}
	return false
}

// accessToken lấy (và giữ tới gần hết hạn) OAuth2 access token bằng JWT ký RS256
// của tài khoản dịch vụ (grant jwt-bearer).
func (f *FCM) accessToken(ctx context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	if f.access != "" && now.Before(f.exp.Add(-time.Minute)) {
		return f.access, nil
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: f.cfg.PrivateKey}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		return "", err
	}
	assertion, err := jwt.Signed(signer).Claims(jwt.Claims{
		Issuer: f.cfg.ClientEmail, Audience: jwt.Audience{f.cfg.TokenURL},
		IssuedAt: jwt.NewNumericDate(now), Expiry: jwt.NewNumericDate(now.Add(time.Hour)),
	}).Claims(map[string]any{"scope": fcmScope}).Serialize()
	if err != nil {
		return "", err
	}
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {assertion}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := f.cfg.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("channel: lấy access token FCM: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("channel: máy chủ token FCM trả %d", resp.StatusCode)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&out); err != nil || out.AccessToken == "" {
		return "", errors.New("channel: phản hồi token FCM không hợp lệ")
	}
	if out.ExpiresIn <= 0 {
		out.ExpiresIn = 3600
	}
	f.access, f.exp = out.AccessToken, now.Add(time.Duration(out.ExpiresIn)*time.Second)
	return f.access, nil
}
