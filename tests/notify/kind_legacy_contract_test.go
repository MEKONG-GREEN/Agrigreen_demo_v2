package notify_test

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestLegacyNotificationKindContract(t *testing.T) {
	f := newFixture(t)
	var definition string
	if err := f.pool.QueryRow(context.Background(), "SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='pf_notify.notification'::regclass AND conname='notification_kind_check'").Scan(&definition); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(definition, "CHECK (") || !strings.HasSuffix(definition, ")") || !strings.Contains(definition, "kind = ANY") {
		t.Fatal("expected legacy finite kind constraint")
	}
	seen := map[string]bool{}
	var kinds []string
	for _, match := range regexp.MustCompile("'([a-z_]+)'::text").FindAllStringSubmatch(definition, -1) {
		if !seen[match[1]] {
			kinds = append(kinds, match[1])
			seen[match[1]] = true
		}
	}
	if len(kinds) == 0 {
		t.Fatal("no current kind contract")
	}
	slices.Sort(kinds)
	expression := strings.TrimSuffix(strings.TrimPrefix(definition, "CHECK ("), ")")
	check := func(kind string) bool {
		t.Helper()
		var allowed bool
		if err := f.pool.QueryRow(context.Background(), "SELECT ("+expression+") FROM (SELECT $1::text AS kind) input", kind).Scan(&allowed); err != nil {
			t.Fatal(err)
		}
		return allowed
	}
	for _, kind := range kinds {
		if !check(kind) {
			t.Fatalf("captured kind rejected: %s", kind)
		}
	}
	rejected := []string{"", "fixture_kind", "UPPER", "kind_v1", "a.b", "has space", strings.Repeat("a", 41)}
	for _, kind := range rejected {
		if check(kind) {
			t.Fatalf("legacy constraint unexpectedly accepts %q", kind)
		}
	}
	data, err := json.Marshal(struct{ Kinds, Rejected []string }{kinds, rejected})
	if err != nil {
		t.Fatal(err)
	}
	t.Log("OLD_NOTIFICATION_KINDS=" + string(data))
}
