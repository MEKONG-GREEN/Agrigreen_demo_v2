package extension

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestDigestRecipientsStrictConfigurationAndIsolation(t *testing.T) {
	a := uuid.MustParse("01990000-0000-7000-8000-000000000001")
	b := uuid.MustParse("01990000-0000-7000-8000-000000000002")
	raw := fmt.Sprintf(`[{"tenant_id":%q,"to":["center-a@example.vn"]},{"tenant_id":%q,"to":["center-b@example.vn"]}]`, a, b)
	resolve, err := NewDigestRecipients(raw)
	if err != nil {
		t.Fatal(err)
	}
	got, err := resolve(context.Background(), a)
	if err != nil || !slices.Equal(got, []string{"center-a@example.vn"}) {
		t.Fatal("tenant A mapping")
	}
	got[0] = "mutated@example.vn"
	got, _ = resolve(context.Background(), a)
	if got[0] != "center-a@example.vn" {
		t.Fatal("resolver returned mutable startup state")
	}
	got, err = resolve(context.Background(), b)
	if err != nil || !slices.Equal(got, []string{"center-b@example.vn"}) {
		t.Fatal("tenant B mapping")
	}
	for _, tid := range []uuid.UUID{uuid.Nil, uuid.New()} {
		if got, err := resolve(context.Background(), tid); len(got) != 0 || !errors.Is(err, ErrDigestRecipientsMissing) {
			t.Fatal("unknown tenant accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := resolve(ctx, a); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored")
	}
	for _, raw := range []string{"", "  ", "[]"} {
		empty, err := NewDigestRecipients(raw)
		if err != nil || empty == nil {
			t.Fatal("empty config must create a deny resolver")
		}
		if _, err := empty(context.Background(), a); !errors.Is(err, ErrDigestRecipientsMissing) {
			t.Fatal("empty mapping allowed delivery")
		}
	}
	cases := map[string]string{
		"object": `{}`, "null": `null`, "malformed": `[{`, "trailing": raw + ` {}`, "scalar-entry": `[1]`,
		"unknown-field":          fmt.Sprintf(`[{"tenant_id":%q,"to":["center-a@example.vn"],"secret":"do-not-print"}]`, a),
		"duplicate-key":          fmt.Sprintf(`[{"tenant_id":%q,"tenant_id":%q,"to":["center-a@example.vn"]}]`, a, b),
		"duplicate-tenant":       fmt.Sprintf(`[{"tenant_id":%q,"to":["one@example.vn"]},{"tenant_id":%q,"to":["two@example.vn"]}]`, a, a),
		"zero-tenant":            `[{"tenant_id":"00000000-0000-0000-0000-000000000000","to":["one@example.vn"]}]`,
		"bad-tenant":             `[{"tenant_id":"bad-id","to":["one@example.vn"]}]`,
		"missing-tenant":         `[{"to":["one@example.vn"]}]`,
		"missing-to":             fmt.Sprintf(`[{"tenant_id":%q}]`, a),
		"empty-to":               fmt.Sprintf(`[{"tenant_id":%q,"to":[]}]`, a),
		"null-to":                fmt.Sprintf(`[{"tenant_id":%q,"to":null}]`, a),
		"scalar-to":              fmt.Sprintf(`[{"tenant_id":%q,"to":"one@example.vn"}]`, a),
		"non-string-to":          fmt.Sprintf(`[{"tenant_id":%q,"to":[1]}]`, a),
		"bad-email":              fmt.Sprintf(`[{"tenant_id":%q,"to":["not-an-address"]}]`, a),
		"display-name":           fmt.Sprintf(`[{"tenant_id":%q,"to":["Person <one@example.vn>"]}]`, a),
		"whitespace":             fmt.Sprintf(`[{"tenant_id":%q,"to":[" one@example.vn"]}]`, a),
		"header-injection":       fmt.Sprintf(`[{"tenant_id":%q,"to":["one@example.vn\r\nBcc: leak@example.vn"]}]`, a),
		"duplicate-mailbox":      fmt.Sprintf(`[{"tenant_id":%q,"to":["One@example.vn","one@example.vn"]}]`, a),
		"cross-tenant-collision": fmt.Sprintf(`[{"tenant_id":%q,"to":["One@example.vn"]},{"tenant_id":%q,"to":["one@example.vn"]}]`, a, b),
	}
	for name, invalid := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := NewDigestRecipients(invalid)
			if got != nil || !errors.Is(err, ErrDigestRecipientsConfig) {
				t.Fatal("invalid configuration accepted")
			}
			if strings.Contains(err.Error(), "example.vn") || strings.Contains(err.Error(), "do-not-print") || err.Error() != ErrDigestRecipientsConfig.Error() {
				t.Fatal("configuration value leaked in error")
			}
		})
	}
}
