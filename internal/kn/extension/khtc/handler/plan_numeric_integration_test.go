package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

func TestPlanVersionNumericBoundary194(t *testing.T) {
	f := newFix(t)
	admin := f.user(authz.KNAdmin, "", false)
	seedCatalog(t, admin)
	writer := f.user(authz.KNTinh, khtc, false)
	p1 := program(t, writer, "CT194", "Chương trình194", "program")
	p2 := program(t, writer, "TW194", "Dự án194", "central_project")
	path := "/v1/kn/khtc/plans/2027/versions"
	want(t, writer.post(path, planBody(0, p1, p2, nil)), http.StatusCreated, "", "initial positive")
	before := writer.get(path)
	want(t, before, http.StatusOK, "", "native version ledger before")
	for _, field := range []string{"planned_qty", "budget"} {
		normal := `"` + field + `":"120.5"`
		if field == "budget" {
			normal = `"budget":"350000000"`
		}
		for _, value := range []string{`"1e100000"`, `"1e-100000"`, `1e100000`, `1e-100000`, `"` + strings.Repeat("9", 100000) + `"`} {
			for _, suffix := range []string{"", `,"` + field + `":null`, `,"` + field + `":"1"`} {
				body, err := json.Marshal(planBody(1, p1, p2, map[string]any{"effective_from": "2027-02-01"}))
				if err != nil {
					t.Fatal(err)
				}
				replacement := `"` + strings.ToUpper(field) + `":` + value + suffix
				if bytes.Count(body, []byte(normal)) != 1 {
					t.Fatal("numeric fixture not unique")
				}
				body = bytes.Replace(body, []byte(normal), []byte(replacement), 1)
				want(t, writer.send(http.MethodPost, path, key(), "application/json", bytes.NewReader(body)), http.StatusBadRequest, "bad_request", "reject before decimal parser")
			}
		}
	}
	after := writer.get(path)
	if after.status != http.StatusOK || !bytes.Equal(before.raw, after.raw) {
		t.Fatal("rejected numbers changed native plan version ledger")
	}
	adjustment := planBody(1, p1, p2, map[string]any{"effective_from": "2027-02-01"})
	idem := key()
	first := writer.do(http.MethodPost, path, idem, adjustment)
	want(t, first, http.StatusCreated, "", "bounded amount adjustment")
	replay := writer.do(http.MethodPost, path, idem, adjustment)
	want(t, replay, http.StatusCreated, "", "same key replay retains original middleware status")
	if str(first.body["version"], "id") != str(replay.body["version"], "id") {
		t.Fatal("replay allocated a different version")
	}
	want(t, f.user(authz.KNTinh, khtc, true).post(path, adjustment), http.StatusForbidden, "extension.read_only", "readonly cannot apply")
	// Spreadsheet import remains a preview; invalid numeric rows do not reach the
	// draft and no version is committed. Ordinary rows still import successfully.
	importPath := "/v1/kn/khtc/plans/2027/import-draft"
	header := "tab,kind,program,name,qty,budget,sort,points\n"
	csv := func(qty, budget, sort, points string) string {
		return header + "programs,program,CT194,Plan194," + qty + "," + budget + "," + sort + "," + points + "\n"
	}
	valid := writer.send(http.MethodPost, importPath, key(), "text/csv", strings.NewReader(csv("120.5", "350000000", "1", "2")))
	want(t, valid, http.StatusOK, "", "ordinary CSV preview")
	if len(objs(valid.body["entries"])) != 1 || len(objs(valid.body["problems"])) != 0 {
		t.Fatalf("ordinary CSV preview: %s", valid.raw)
	}
	beforeImport := writer.get(path)
	for field := range 4 {
		for _, value := range []string{"1e100000", "1e-100000", strings.Repeat("9", 100000)} {
			values := []string{"120.5", "350000000", "1", "2"}
			values[field] = value
			result := writer.send(http.MethodPost, importPath, key(), "text/csv", strings.NewReader(csv(values[0], values[1], values[2], values[3])))
			if result.status == http.StatusOK {
				if len(objs(result.body["problems"])) == 0 || len(objs(result.body["entries"])) != 0 {
					t.Fatal("hostile CSV row reached draft")
				}
			} else if result.status < 400 || result.status >= 500 {
				t.Fatalf("hostile CSV returned %d", result.status)
			}
		}
	}
	afterImport := writer.get(path)
	if afterImport.status != http.StatusOK || !bytes.Equal(beforeImport.raw, afterImport.raw) {
		t.Fatal("CSV preview changed native version ledger")
	}

}
