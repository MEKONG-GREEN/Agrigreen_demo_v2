package channel_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/channel"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
)

// fakeGoogle giả máy chủ token OAuth2 và FCM HTTP v1.
type fakeGoogle struct {
	mu       sync.Mutex
	tokens   int
	bodies   [][]byte
	auth     []string
	response func(w http.ResponseWriter)
}

func (g *fakeGoogle) server(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" || r.Form.Get("assertion") == "" {
			http.Error(w, "bad grant", http.StatusBadRequest)
			return
		}
		g.mu.Lock()
		g.tokens++
		g.mu.Unlock()
		_, _ = w.Write([]byte(`{"access_token":"token-gia-cho-kiem-thu","expires_in":3600,"token_type":"Bearer"}`))
	})
	mux.HandleFunc("/v1/projects/agrigreen-test/messages:send", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		g.mu.Lock()
		g.bodies = append(g.bodies, b)
		g.auth = append(g.auth, r.Header.Get("Authorization"))
		resp := g.response
		g.mu.Unlock()
		if resp != nil {
			resp(w)
			return
		}
		_, _ = w.Write([]byte(`{"name":"projects/agrigreen-test/messages/1"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newFCM(t *testing.T, srv *httptest.Server) *channel.FCM {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	sa, _ := json.Marshal(map[string]string{
		"type": "service_account", "project_id": "agrigreen-test", "client_email": "push@agrigreen-test.iam.gserviceaccount.com",
		"private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), "token_uri": srv.URL + "/token",
	})
	cfg, err := channel.ParseServiceAccount(sa)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Endpoint = srv.URL
	f, err := channel.NewFCM(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// RB-7 (ADR-0015): thân gửi FCM CHỈ có token + data {kind, notification_id} + cờ
// ưu tiên/nền — không khối notification, không tiêu đề/nội dung, không PII.
func TestFCMPayloadIsDataOnly(t *testing.T) {
	g := &fakeGoogle{}
	srv := g.server(t)
	f := newFCM(t, srv)
	nid := uuid.MustParse("0191a0e8-9281-7111-97db-bc8458a6af17")
	for _, m := range []domain.PushMessage{
		{Token: "fcm-token-thu-nghiem-0123456789abcdef", Platform: "android", Kind: "directive", NotificationID: &nid, Urgent: true},
		{Token: "fcm-token-thu-nghiem-0123456789abcdef", Platform: "ios", Kind: "sync_now"},
	} {
		if err := f.Send(context.Background(), m); err != nil {
			t.Fatal(err)
		}
	}
	if g.tokens != 1 {
		t.Fatalf("access token phải được dùng lại, lấy %d lần", g.tokens)
	}
	for i, b := range g.bodies {
		if g.auth[i] != "Bearer token-gia-cho-kiem-thu" {
			t.Fatalf("Authorization: %q", g.auth[i])
		}
		var req map[string]map[string]json.RawMessage
		if err := json.Unmarshal(b, &req); err != nil {
			t.Fatal(err)
		}
		msg := req["message"]
		for k := range msg {
			if k != "token" && k != "data" && k != "android" && k != "apns" {
				t.Fatalf("khóa ngoài danh sách cho phép trong message: %q (%s)", k, b)
			}
		}
		var data map[string]string
		_ = json.Unmarshal(msg["data"], &data)
		want := map[string]string{"kind": "directive", "notification_id": nid.String()}
		if i == 1 {
			want = map[string]string{"kind": "sync_now"}
		}
		if len(data) != len(want) {
			t.Fatalf("data %v, muốn %v", data, want)
		}
		for k, v := range want {
			if data[k] != v {
				t.Fatalf("data %v, muốn %v", data, want)
			}
		}
		s := strings.ToLower(string(b))
		for _, bad := range []string{`"notification"`, `"title"`, `"body"`, `"alert"`} {
			if strings.Contains(s, bad) {
				t.Fatalf("thân chứa %s: %s", bad, b)
			}
		}
		if !strings.Contains(s, `"content-available":1`) {
			t.Fatalf("iOS phải là thông điệp nền: %s", b)
		}
	}
}

// Token đã gỡ (UNREGISTERED) → ErrTokenInvalid; lỗi khác (5xx, sai project) là lỗi tạm.
func TestFCMErrors(t *testing.T) {
	g := &fakeGoogle{}
	srv := g.server(t)
	f := newFCM(t, srv)
	m := domain.PushMessage{Token: "fcm-token-thu-nghiem-0123456789abcdef", Kind: "alert"}
	g.response = func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":404,"status":"NOT_FOUND","details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"UNREGISTERED"}]}}`))
	}
	if err := f.Send(context.Background(), m); !errors.Is(err, channel.ErrTokenInvalid) {
		t.Fatalf("UNREGISTERED: %v", err)
	}
	g.response = func(w http.ResponseWriter) { w.WriteHeader(http.StatusServiceUnavailable) }
	if err := f.Send(context.Background(), m); err == nil || errors.Is(err, channel.ErrTokenInvalid) {
		t.Fatalf("503 phải là lỗi tạm: %v", err)
	}
	if err := (channel.DisabledZNS{}).SendTemplate(context.Background(), uuid.New(), "t", nil); !errors.Is(err, channel.ErrZNSDisabled) {
		t.Fatal("ZNS phải bị tắt")
	}
}

func TestBuildMessageEncodesSubject(t *testing.T) {
	msg := string(channel.BuildMessage("noreply@agrigreen.vn", []string{"a@b.vn"}, "Chỉ đạo khẩn", "dòng 1\ndòng 2"))
	if !strings.Contains(msg, "Subject: =?utf-8?q?") || !strings.Contains(msg, "dòng 1\r\ndòng 2") {
		t.Fatalf("thư: %q", msg)
	}
}
