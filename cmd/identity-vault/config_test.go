package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

func envMap(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestVaultAudience(t *testing.T) {
	if got := vaultAudience(envMap(map[string]string{"AG_OIDC_AUDIENCE": "agrigreen-api"})); got != "agrigreen-api" {
		t.Fatalf("mặc định giữ AG_OIDC_AUDIENCE, nhận %q", got)
	}
	got := vaultAudience(envMap(map[string]string{"AG_OIDC_AUDIENCE": "agrigreen-api", VaultAudienceEnv: " identity-vault "}))
	if got != "identity-vault" {
		t.Fatalf("AG_VAULT_OIDC_AUDIENCE phải thắng, nhận %q", got)
	}
}

func TestRequireMFAFromEnv(t *testing.T) {
	cases := []struct {
		env  map[string]string
		want bool
		err  bool
	}{
		{env: map[string]string{"AG_ENV": "production"}, want: true},
		{env: map[string]string{"AG_ENV": "local"}, want: false},
		{env: map[string]string{"AG_ENV": "production", RequireMFAEnv: "0"}, want: false},
		{env: map[string]string{"AG_ENV": "test", RequireMFAEnv: "1"}, want: true},
		{env: map[string]string{"AG_ENV": "test", RequireMFAEnv: "yes"}, err: true},
	}
	for _, c := range cases {
		got, err := requireMFAFromEnv(envMap(c.env))
		if (err != nil) != c.err || got != c.want {
			t.Fatalf("%v: %v %v", c.env, got, err)
		}
	}
}

type okPinger struct{}

func (okPinger) Ping(context.Context) error { return nil }

type stubModule struct{}

func (stubModule) RegisterApp(*gin.RouterGroup)   {}
func (stubModule) RegisterAdmin(*gin.RouterGroup) {}

// /readyz phơi kms_local để vận hành thấy kho đang dùng KEK phần mềm (M10).
func TestReadyzExposesKMSLocal(t *testing.T) {
	for _, local := range []bool{true, false} {
		r := NewRouter(Deps{Log: telemetry.NewLogger("test", io.Discard), Module: stubModule{}, DB: okPinger{}, KMSLocal: local, KMSProvider: "local"})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		var body struct {
			Status   string `json:"status"`
			KMSLocal int    `json:"kms_local"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 200 {
			t.Fatalf("%d %s %v", w.Code, w.Body, err)
		}
		if want := map[bool]int{true: 1, false: 0}[local]; body.KMSLocal != want {
			t.Fatalf("kms_local = %d, mong %d", body.KMSLocal, want)
		}
	}
}
