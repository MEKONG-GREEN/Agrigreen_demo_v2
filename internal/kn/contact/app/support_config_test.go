package app

import (
	"context"
	"encoding/base64"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/contact/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/envelope"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

func TestSupport274TrustedConfiguration(t *testing.T) {
	values := map[string]string{}
	get := func(k string) string { return values[k] }
	if cfg, err := SupportConfigFromEnv(get); err != nil || len(cfg.PublicKey) != 0 {
		t.Fatalf("absent: %v", err)
	}
	values["AG_KN_SUPPORT_TENANT_ID"] = ids.New().String()
	if _, err := SupportConfigFromEnv(get); err == nil {
		t.Fatal("partial configuration accepted")
	}
	values["AG_KN_SUPPORT_SUBJECT_REF"] = ids.New().String()
	values["AG_KN_SUPPORT_PUBLIC_KEY"] = base64.StdEncoding.EncodeToString(make([]byte, 32))
	if _, err := SupportConfigFromEnv(get); err != nil {
		t.Fatal(err)
	}
	values["AG_KN_SUPPORT_PUBLIC_KEY"] = "not-base64"
	if _, err := SupportConfigFromEnv(get); err == nil {
		t.Fatal("bad key accepted")
	}
}

// Canonical JSON can expand safe text; this must be rejected before a database transaction, not become a storage 500.
func TestSupport274EncodedBudgetRejectedBeforeDatabase(t *testing.T) {
	pub, _, err := envelope.GenerateKeyPair(nil)
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{Support: SupportConfig{TenantID: ids.New(), SubjectRef: ids.New(), PublicKey: pub[:]}}
	err = svc.SubmitSupport(context.Background(), domain.SupportInput{Name: strings.Repeat(">", 100), Contact: "274@example.test", Message: strings.Repeat("<", 2000)}, ids.New().String())
	var p *problem.Problem
	if !errors.As(err, &p) || p.Code != "validation_failed" {
		t.Fatalf("encoded limit must fail validation before DB: %v", err)
	}
}
