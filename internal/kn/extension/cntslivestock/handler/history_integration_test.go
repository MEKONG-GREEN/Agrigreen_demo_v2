package handler_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntslivestock/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntslivestock/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/google/uuid"
)

// Supplemental module fixture: PG ledger + instance owner projection, synthetic vault.
// The native account/current-scope/vault HTTP workflow is TestHCN09NativeCurrentSession_276.
func TestLivestockHistoryVersionsAndUnavailable_276(t *testing.T) {
	f := newFix(t)
	writer := f.user("Cán bộ kiểm thử", xa("30001"))
	reader := f.user("Người xem kiểm thử", tinh("vien_chuc"))
	h := want(t, writer.post(base, household("Hộ kiểm thử An", "30001", "Ấp kiểm thử")), 201, "", "create")
	path := base + "/" + str(h, "id")
	in := household("Hộ kiểm thử Bình", "30001", "Ấp kiểm thử")
	delete(in, "checkin")
	in["base_version"], in["owner_phone"] = 1, "0991000456"
	want(t, writer.post(path, in), 200, "", "first rename")
	in["base_version"], in["owner_name"], in["owner_phone"] = 2, "Hộ kiểm thử Cường", "0992000789"
	want(t, writer.post(path, in), 200, "", "later rename")
	exact := "owner_name:Hộ kiểm thử An→Hộ kiểm thử Bình|owner_phone:0912•••123→0991•••456"
	for _, u := range []user{writer, reader} {
		got := want(t, u.get(path), 200, "", "read historical version")
		if changes(got, "2") != exact {
			t.Fatalf("historical version followed latest identity: %q", changes(got, "2"))
		}
		if strings.Contains(string(mustJSON(got)), "0991000456") || strings.Contains(string(mustJSON(got)), "0992000789") {
			t.Fatal("plaintext phone leaked")
		}
	}
	view := xa("30001")
	view.ViewOnly = true
	readOnly := f.user("Chỉ xem", view)
	if got := want(t, readOnly.get(path), 200, "", "read-only history"); changes(got, "2") != exact || got["can_edit"] != false || got["owner_name"] != nil {
		t.Fatal("read-only changed current-detail or history permissions")
	}
	want(t, readOnly.post(path, in), 403, "extension.read_only", "read-only mutation")
	before := len(f.vault.Accesses())
	badDept := xa("30001")
	badDept.Department = "trong_trot"
	want(t, f.user("Sai phòng", badDept).get(path), 403, "forbidden", "wrong department")
	want(t, f.user("Sai xã", xa("30002")).get(path), 404, "extension.cnts_livestock_household_not_found", "wrong scope")
	if len(f.vault.Accesses()) != before {
		t.Fatal("unauthorized request reached vault")
	}
	f.vault.Fail = func(op string) error {
		if op == "resolve" {
			return errors.New("synthetic vault unavailable")
		}
		return nil
	}
	got := want(t, reader.get(path), 200, "", "vault failure")
	if c := changes(got, "2"); !strings.Contains(c, "owner_name:(không đọc được lịch sử định danh)→(không đọc được lịch sử định danh)") || strings.Contains(c, "Hộ kiểm thử") || !strings.Contains(c, "0912•••123→0991•••456") {
		t.Fatal("vault failure exposed or guessed names")
	}
	f.vault.Fail = nil
	// Append a synthetic old-format event with no revision reference; never rewrite an old ledger row.
	ctx := context.Background()
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", f.tenant.String()); err != nil {
		t.Fatal(err)
	}
	latest, err := store.Latest(ctx, tx, uuid.MustParse(str(h, "id")))
	if err != nil {
		t.Fatal(err)
	}
	latest.Meta = store.Meta{ID: ids.New(), ActorRef: writer.ref, EventTime: t0, IdemKey: key()}
	latest.ChangedFields = []string{domain.FieldOwnerName}
	latest.PIIRevisionRef = nil
	latest.Event = "updated"
	if err := store.Insert(ctx, tx, f.tenant, latest); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	got = want(t, reader.get(path), 200, "", "missing historical ref")
	if c := changes(got, "4"); strings.Contains(c, "Hộ kiểm thử") || !strings.Contains(c, "không đọc được lịch sử định danh") {
		t.Fatal("missing ref fell back to current name")
	}
	// SQL assertions about the KN ledger stay in the owning KN module.
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_livestock_household WHERE tenant_id=$1 AND row_to_json(cnts_livestock_household)::text LIKE '%Hộ kiểm thử%'`, f.tenant); n != 0 {
		t.Fatal("KN ledger contains plaintext names")
	}

}
