package hc30_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/xuri/excelize/v2"
)

func expectIDs(t *testing.T, r response, want ...string) {
	t.Helper()
	items, ok := r.body["items"].([]any)
	if !ok {
		t.Fatalf("missing list: %v", r.body)
	}
	got := make([]string, 0, len(items))
	for _, item := range items {
		got = append(got, item.(map[string]any)["id"].(string))
	}
	slices.Sort(got)
	want = slices.Clone(want)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("visible IDs=%v want %v", got, want)
	}
}

func reportUnit(t *testing.T, r response, id string) string {
	t.Helper()
	unitColumn := -1
	for i, v := range r.body["columns"].([]any) {
		if v.(map[string]any)["key"] == "phong_ban" {
			unitColumn = i
			break
		}
	}
	if unitColumn < 0 {
		t.Fatal("NS01 lacks the unit column")
	}
	for _, v := range r.body["rows"].([]any) {
		row := v.(map[string]any)
		if row["staff_id"] == id {
			cells := row["cells"].([]any)
			if unitColumn >= len(cells) {
				t.Fatal("NS01 staff row lacks a unit cell")
			}
			return cells[unitColumn].(string)
		}
	}
	t.Fatalf("report lacks staff %s", id)
	return ""
}
func TestHCTHStaffAsOfPermissionsContact_W1131(t *testing.T) {
	f := newFixture(t)
	f.request("admin", http.MethodPost, "/v1/hrm/defaults", "hc30-defaults", nil, http.StatusCreated)
	body := staff("Trần Thị An", "0912000101", "vien_chuc", "trong_trot")
	body["account_ref"] = f.people["self"].ActorRef.String()
	a := f.request("hcth", http.MethodPost, "/v1/hrm/staff", "hc30-create-a", body, http.StatusCreated)
	aid := a.body["id"].(string)
	replay := f.request("hcth", http.MethodPost, "/v1/hrm/staff", "hc30-create-a", body, http.StatusCreated)
	if replay.body["id"] != aid || replay.header.Get(idempotency.ReplayedHeader) != "true" {
		t.Fatal("create retry did not replay the original staff")
	}
	changed := staff("Tên Khác", "0912000109", "vien_chuc", "trong_trot")
	changedKey := f.request("hcth", http.MethodPost, "/v1/hrm/staff", "hc30-create-a", changed, http.StatusUnprocessableEntity)
	if changedKey.body["code"] != "idempotency_key_reused" {
		t.Fatal("changed request did not report the public idempotency error")
	}
	b := f.request("hcth", http.MethodPost, "/v1/hrm/staff", "hc30-create-b", staff("Lê Văn Bình", "0912000102", "vien_chuc", "chan_nuoi"), http.StatusCreated).body["id"].(string)
	x := f.request("hcth", http.MethodPost, "/v1/hrm/staff", "hc30-create-x", staff("Nguyễn Văn Xã", "0912000103", "can_bo_xa", "trong_trot"), http.StatusCreated).body["id"].(string)
	path := "/v1/hrm/staff/" + aid

	t.Run("owner and tenant boundaries", func(t *testing.T) {
		f := f.withTest(t)
		expectIDs(t, f.request("hcth", http.MethodGet, "/v1/hrm/staff", "", nil, http.StatusOK), aid, b, x)
		expectIDs(t, f.request("manager_tt", http.MethodGet, "/v1/hrm/staff", "", nil, http.StatusOK), aid, x)
		expectIDs(t, f.request("manager_cn", http.MethodGet, "/v1/hrm/staff", "", nil, http.StatusOK), b)
		f.request("manager_cn", http.MethodGet, path, "", nil, http.StatusNotFound)
		f.request("", http.MethodGet, "/v1/hrm/staff", "", nil, http.StatusUnauthorized)
		for _, who := range []string{"national", "commune", "self"} {
			f.request(who, http.MethodGet, "/v1/hrm/staff", "", nil, http.StatusForbidden)
		}
		missing := f.request("realm_only", http.MethodGet, "/v1/hrm/staff", "", nil, http.StatusForbidden)
		if missing.body["code"] != "forbidden" {
			t.Fatal("realm roles must not replace the selected owner session")
		}
		f.request("wrong_owner", http.MethodGet, path, "", nil, http.StatusForbidden)
		expectIDs(t, f.request("other_tenant", http.MethodGet, "/v1/hrm/staff", "", nil, http.StatusOK))
		f.request("other_tenant", http.MethodGet, path, "", nil, http.StatusNotFound)
	})

	t.Run("self override cannot widen to colleagues", func(t *testing.T) {
		f := f.withTest(t)
		f.request("admin", http.MethodPost, "/v1/hrm/permissions", "hc30-self-permission", map[string]any{
			"grantee_kind": "user", "grantee": f.people["self"].ActorRef.String(), "perms": map[string]any{"profile.view": "self"},
		}, http.StatusCreated)
		expectIDs(t, f.request("self", http.MethodGet, "/v1/hrm/staff", "", nil, http.StatusOK), aid)
		f.request("self", http.MethodGet, "/v1/hrm/staff/"+b, "", nil, http.StatusNotFound)
	})

	t.Run("as-of transfer preserves July report", func(t *testing.T) {
		f := f.withTest(t)
		july := "/v1/hrm/reports/NS01?at=2026-07-31"
		before := f.request("hcth", http.MethodGet, july, "", nil, http.StatusOK)
		if !strings.Contains(reportUnit(t, before, aid), "Phòng Trồng trọt") {
			t.Fatal("July report has wrong original unit")
		}
		ex := f.request("hcth", http.MethodPost, "/v1/hrm/reports/NS01/export", "hc30-july-export", map[string]any{"as_of": "2026-07-31"}, http.StatusCreated)
		filePath := "/v1/hrm/report-exports/" + ex.body["id"].(string) + "/file"
		fileBefore := f.request("hcth", http.MethodGet, filePath, "", nil, http.StatusOK)
		transfer := event("unit", "2026-08-01", map[string]any{"unit": "chan_nuoi"})
		f.request("hcth", http.MethodPost, path+"/events", "hc30-unit-transfer", transfer, http.StatusCreated)
		past := f.request("hcth", http.MethodGet, path+"?at=2026-07-31", "", nil, http.StatusOK)
		present := f.request("hcth", http.MethodGet, path+"?at=2026-08-31", "", nil, http.StatusOK)
		if past.body["current"].(map[string]any)["unit"] != "trong_trot" || present.body["current"].(map[string]any)["unit"] != "chan_nuoi" {
			t.Fatal("effective-date boundary changed")
		}
		julyAgain := f.request("hcth", http.MethodGet, july, "", nil, http.StatusOK)
		august := f.request("hcth", http.MethodGet, "/v1/hrm/reports/NS01?at=2026-08-31", "", nil, http.StatusOK)
		if reportUnit(t, julyAgain, aid) != reportUnit(t, before, aid) || !strings.Contains(reportUnit(t, august, aid), "Phòng Chăn nuôi") {
			t.Fatal("NS01 changed the historical unit or missed the effective unit")
		}
		fileAfter := f.request("hcth", http.MethodGet, filePath, "", nil, http.StatusOK)
		sum := sha256.Sum256(fileAfter.raw)
		if !bytes.Equal(fileBefore.raw, fileAfter.raw) || hex.EncodeToString(sum[:]) != ex.body["sha256"] || fileAfter.header.Get("Cache-Control") != "no-store" {
			t.Fatal("historical export changed or lost its SHA/cache policy")
		}
		book, err := excelize.OpenReader(bytes.NewReader(fileAfter.raw))
		if err != nil {
			t.Fatal(err)
		}
		rows, err := book.GetRows(book.GetSheetName(0))
		_ = book.Close()
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(rows)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(encoded), "Trần Thị An") || !strings.Contains(string(encoded), "THÁNG 7/2026") {
			t.Fatal("download does not contain the July staff report")
		}
		expectIDs(t, f.request("manager_tt", http.MethodGet, "/v1/hrm/staff", "", nil, http.StatusOK), x)
		expectIDs(t, f.request("manager_cn", http.MethodGet, "/v1/hrm/staff", "", nil, http.StatusOK), aid, b)
	})

	t.Run("readonly write is denied and leaves profile unchanged", func(t *testing.T) {
		f := f.withTest(t)
		f.request("readonly", http.MethodGet, path, "", nil, http.StatusOK)
		f.request("readonly", http.MethodPost, path+"/profile", "hc30-readonly-write", map[string]any{"phone": "0912000999"}, http.StatusForbidden)
		detail := f.request("hcth", http.MethodGet, path, "", nil, http.StatusOK)
		if detail.body["profile"].(map[string]any)["phone"] != "0912000101" {
			t.Fatal("readonly request changed the profile")
		}
	})

	t.Run("HRM phone update reaches minimal Contact response", func(t *testing.T) {
		f := f.withTest(t)
		f.request("hcth", http.MethodPost, path+"/profile", "hc30-phone-update", map[string]any{"phone": "0912000199"}, http.StatusOK)
		for _, who := range []string{"commune", "national"} {
			r := f.request(who, http.MethodGet, "/v1/contacts/persons?q=0912000199", "", nil, http.StatusOK)
			items := r.body["items"].([]any)
			if len(items) != 1 || r.header.Get("Cache-Control") != "no-store" {
				t.Fatalf("%s Contact count/cache", who)
			}
			person := items[0].(map[string]any)
			keys := make([]string, 0, len(person))
			for key := range person {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			if !slices.Equal(keys, []string{"birth_year", "full_name", "id", "phone"}) || person["phone"] != "0912000199" || person["birth_year"] != "1990" || person["full_name"] != "Trần Thị An" {
				t.Fatalf("%s Contact leaked fields or missed HRM update: %v", who, person)
			}
			access := f.request(who, http.MethodGet, "/v1/contacts/access", "", nil, http.StatusOK)
			if access.body["can_manage"] != false || access.body["can_detail"] != false {
				t.Fatal("basic Contact advertised write/detail access")
			}
		}
		expectIDs(t, f.request("other_tenant", http.MethodGet, "/v1/contacts/persons", "", nil, http.StatusOK))
	})
}
