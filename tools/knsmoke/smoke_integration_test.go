package main

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/e2e/harness"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// harnessID cấp token bằng issuer giả của harness: quản trị gốc có MFA, tài khoản KN chỉ có vai kn_user.
type harnessID struct {
	s      *harness.Stack
	tenant uuid.UUID
}

func (h harnessID) RootToken(context.Context) (string, error) {
	return h.s.User(harness.UserSpec{Tenant: h.tenant, Roles: []string{"platform_admin"}, MFA: true}).Token, nil
}

func (h harnessID) UserToken(_ context.Context, userID string) (string, error) {
	id, err := uuid.Parse(userID)
	if err != nil {
		return "", err
	}
	return h.s.User(harness.UserSpec{Tenant: h.tenant, Subject: id, Roles: []string{"kn_user"}}).Token, nil
}

// Kiểm khói KN độc lập trên gốc lắp ráp thật của cmd/api-platform và PostgreSQL thật: cùng mã mà job CI kn-standalone
// chạy với Keycloak realm kn.
func TestKNStandaloneSmoke(t *testing.T) {
	s := harness.New(t)
	var out bytes.Buffer
	err := Run(context.Background(), Config{AppURL: s.App.URL, AdminURL: s.Admin.URL, ID: harnessID{s: s, tenant: harness.NewTenant()},
		HTTP: &http.Client{Timeout: 60 * time.Second}, Out: &out})
	t.Log("\n" + out.String())
	if err != nil {
		t.Fatal(err)
	}
}
