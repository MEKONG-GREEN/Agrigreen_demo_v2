package kn77_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// The owner registry is real; the fixture replaces selected-account middleware.
func TestCNTS275DefinitionReplayCurrentAuthority(t *testing.T) {
	f := newAggFx(t, facilityDir{})
	actor, current := ids.New(), cn275Context(f)
	const createPath = "/v1/extension/cnts/form-designs"
	create := map[string]any{"code_suffix": "AUTH275", "title": "Biểu gốc", "fields": []designEditField{{Key: "so_con", Label: "Số con", Type: "integer", Required: true}}}
	c, b := cn275Call(f, createPath, "create275-authority", actor, current, create)
	var first designSaved
	f.ok(c, b, &first)
	savePath := createPath + "/" + first.Code + "/versions"
	fields := append(append([]designEditField{}, first.Version.Fields...), designEditField{Key: "ghi_chu", Label: "Ghi chú", Type: "text"})
	save := map[string]any{"base_version": 1, "title": "Biểu v2", "fields": fields}
	c, b = cn275Call(f, savePath, "save275-authority", actor, current, save)
	var second designSaved
	f.ok(c, b, &second)
	thirdFields := append(append([]designEditField{}, fields...), designEditField{Key: "ngay", Label: "Ngày", Type: "date"})
	c, b = cn275Call(f, savePath, "save275-head3", actor, current, map[string]any{"base_version": 2, "title": "Biểu v3", "fields": thirdFields})
	f.ok(c, b, nil)
	operations := []struct {
		name, path, key string
		body            map[string]any
		original        designSaved
	}{
		{"create", createPath, "create275-authority", create, first},
		{"save", savePath, "save275-authority", save, second},
	}
	evict := func() {
		t.Helper()
		if _, err := f.adminExec("DELETE FROM shared.idempotency_key WHERE tenant_id='" + f.tid.String() + "'"); err != nil {
			t.Fatal(err)
		}
	}
	for _, op := range operations {
		t.Run(op.name+" durable original after v3", func(t *testing.T) {
			evict()
			c, b := cn275Call(f, op.path, op.key, actor, current, op.body)
			var replay designSaved
			if c != http.StatusCreated || json.Unmarshal(b, &replay) != nil {
				t.Fatalf("durable replay failed: %d %s", c, b)
			}
			if replay.Version.TemplateID != op.original.Version.TemplateID || replay.Version.Version != op.original.Version.Version {
				t.Fatalf("head advanced: original receipt changed: %s", b)
			}
		})
		for _, change := range []string{"payload", "actor"} {
			t.Run(op.name+" canonical conflict "+change, func(t *testing.T) {
				evict()
				body := map[string]any{}
				for key, value := range op.body {
					body[key] = value
				}
				by := actor
				if change == "actor" {
					by = ids.New()
				} else {
					body["title"] = "Đã đổi payload"
				}
				c, b := cn275Call(f, op.path, op.key, by, current, body)
				if c != http.StatusUnprocessableEntity || !bytes.Contains(b, []byte("idempotency_key_reused")) {
					t.Fatalf("canonical actor/payload must conflict after HTTP eviction: %d %s", c, b)
				}
			})
		}
	}
	evict()
	changedTarget := map[string]any{}
	for key, value := range create {
		changedTarget[key] = value
	}
	changedTarget["code_suffix"] = "OTHER275"
	c, b = cn275Call(f, createPath, "create275-authority", actor, current, changedTarget)
	if c != http.StatusUnprocessableEntity || !bytes.Contains(b, []byte("idempotency_key_reused")) {
		t.Fatalf("canonical target must conflict after HTTP eviction: %d %s", c, b)
	}
	// Fill the HTTP cache with the original receipts again. Every denial below
	// must occur before that cache can replay either create or save.
	for _, op := range operations {
		c, b = cn275Call(f, op.path, op.key, actor, current, op.body)
		f.ok(c, b, nil)
	}
	beforeAttempts := len(f.admin.query("SELECT idem_key FROM shared.idempotency_key WHERE tenant_id=$1", f.tid))
	for _, reason := range []string{"read-only", "expired", "wrong-group", "empty-group", "wrong-department", "commune"} {
		denied := current
		switch reason {
		case "read-only":
			denied.ReadOnly = true
		case "expired":
			denied.ExpiresAt = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
		case "wrong-group":
			denied.ProvinceGroup = "lanh_dao"
		case "empty-group":
			denied.ProvinceGroup = ""
		case "wrong-department":
			denied.Department = "ke_hoach_tai_chinh"
		case "commune":
			denied.Role = authz.KNXa
		}
		for _, op := range operations {
			t.Run(op.name+" current "+reason, func(t *testing.T) {
				c, b := cn275Call(f, op.path, op.key, actor, denied, op.body)
				if c != http.StatusForbidden {
					t.Fatalf("current authority must precede HTTP replay: %d %s", c, b)
				}
			})
		}
	}
	if after := len(f.admin.query("SELECT idem_key FROM shared.idempotency_key WHERE tenant_id=$1", f.tid)); after != beforeAttempts {
		t.Fatalf("denied replay consumed attempts: %d -> %d", beforeAttempts, after)
	}
	if n := len(f.admin.query("SELECT id FROM pf_forms.template WHERE tenant_id=$1", f.tid)); n != 3 {
		t.Fatalf("definition replay/conflict added versions: %d", n)
	}
}
