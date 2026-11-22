package app_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/guestlink/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/guestlink/port"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func TestDisabledCapabilityNeverNeedsOrMintsIdentity(t *testing.T) {
	s, err := app.New(nil, nil, app.Config{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Issue(context.Background(), port.Binding{}, time.Time{}, "request"); err == nil {
		t.Fatal("disabled issue accepted")
	}
	if _, err = s.Open(context.Background(), port.Secret{}, "request", "127.0.0.1"); err == nil {
		t.Fatal("disabled open accepted")
	}
	if _, err = s.Verify(context.Background(), port.Secret{}, uuid.Nil, port.Secret{}, "127.0.0.1"); err == nil {
		t.Fatal("disabled verify accepted")
	}
	if _, err = s.Authorize(context.Background(), port.Secret{}, "127.0.0.1"); err == nil {
		t.Fatal("disabled authorization accepted")
	}
}
func TestConfigurationRejectsUnsafeBoundsAndRedactsKey(t *testing.T) {
	marker := "synthetic-key-184-1234567890123456"
	c := app.Config{Enabled: true, Tenant: uuid.New(), Key: []byte(marker)}
	raw, _ := json.Marshal(c)
	if strings.Contains(string(raw), marker) || strings.Contains(string(raw), base64.StdEncoding.EncodeToString([]byte(marker))) || strings.Contains(fmt.Sprintf("%+v %#v", c, c), marker) {
		t.Fatal("config leaked key")
	}
	for _, cfg := range []app.Config{{Enabled: true}, {Enabled: true, Tenant: uuid.New(), Key: make([]byte, 32)}, {Enabled: true, Tenant: uuid.New(), Key: []byte(strings.Repeat("k", 32)), MaxAttempts: 6}, {Enabled: true, Tenant: uuid.New(), Key: []byte(strings.Repeat("k", 32)), MaxOpens: 11}, {Enabled: true, Tenant: uuid.New(), Key: []byte(strings.Repeat("k", 32)), LinkTTL: 25 * time.Hour}} {
		if _, err := app.New(nil, nil, cfg, nil, nil); err == nil {
			t.Fatal("unsafe or incomplete enabled config accepted")
		}
	}
}
