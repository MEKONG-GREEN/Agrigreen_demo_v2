package kn77_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/xuri/excelize/v2"
)

type pn215View struct {
	Department string `json:"department"`
	Status     string `json:"status"`
	Snapshot   *struct {
		ID      string `json:"id"`
		Version int64  `json:"version"`
	} `json:"snapshot"`
	Rows []struct {
		Ref        string  `json:"ref"`
		Kind       string  `json:"kind"`
		Name       string  `json:"name"`
		Department string  `json:"department"`
		YearToDate string  `json:"year_to_date"`
		Percent    *string `json:"percent"`
	} `json:"rows"`
}

func TestPN215SnapshotReaderMatchesM5AndExcel(t *testing.T) {
	f := newReportFx(t)
	week := f.open(41, dTT, dTTTT)
	f.submit(week.ID, "25")
	var p khtcPeriod
	c, b := f.khtc(http.MethodPost, "/periods", f.khtcUser, "", map[string]any{
		"report_type": "program", "year": 2026, "as_of_date": "2026-10-13",
		"due_at": "2026-12-31T03:00:00Z", "departments": []string{dTT, dTTTT},
	})
	f.ok(c, b, &p)
	d := f.refresh(p.ID, 0)
	c, ownerBody := f.call(http.MethodGet, p.ID, "", nil)
	f.ok(c, ownerBody, nil)
	var rawOwner struct {
		Content struct {
			Data struct {
				Rows []struct {
					Ref        string  `json:"ref"`
					Name       string  `json:"name"`
					YearToDate string  `json:"year_to_date"`
					Percent    *string `json:"percent"`
				} `json:"rows"`
			} `json:"data"`
		} `json:"content"`
	}
	if err := json.Unmarshal(ownerBody, &rawOwner); err != nil {
		t.Fatal(err)
	}
	expectedName, expectedQty := "", ""
	var expectedPercent *string
	for _, r := range rawOwner.Content.Data.Rows {
		if r.Ref == f.item {
			expectedName, expectedQty, expectedPercent = r.Name, r.YearToDate, r.Percent
		}
	}
	if expectedName == "" || expectedQty != "25" {
		t.Fatalf("native owner M5 fixture missing submitted quantity: %s %s", expectedName, expectedQty)
	}
	base := "/v1/extension/khtc/departments/" + dTT + "/program-progress"
	actor := ids.New()
	c, b = f.do(http.MethodGet, base+"/periods?year=2026", authz.KNTinh, dTT, actor, "", nil)
	f.ok(c, b, nil)
	if !bytes.Contains(b, []byte(p.ID)) || bytes.Contains(b, []byte(week.ID)) {
		t.Fatalf("PN choices must select real M5 periods only: %s", b)
	}
	c, b = f.do(http.MethodGet, base+"/"+p.ID, authz.KNTinh, dTT, actor, "", nil)
	var view pn215View
	f.ok(c, b, &view)
	if view.Department != dTT || view.Status != "snapshot" || view.Snapshot == nil || view.Snapshot.ID == "" || view.Snapshot.Version != d.Version {
		t.Fatalf("missing real source/version: %+v", view)
	}
	if bytes.Contains(b, []byte(f.news)) || bytes.Contains(b, []byte(f.price)) || bytes.Contains(b, []byte("submissions")) || bytes.Contains(b, []byte("actor_ref")) {
		t.Fatalf("PN response exposed other-department/private report data: %s", b)
	}
	found := false
	for _, row := range view.Rows {
		if row.Kind != "item" {
			continue
		}
		if row.Department != dTT {
			t.Fatalf("foreign row reached PN: %+v", row)
		}
		if row.Ref == f.item {
			found = true
			if row.YearToDate != expectedQty || row.Percent == nil || expectedPercent == nil || *row.Percent != *expectedPercent {
				t.Fatalf("PN differs from actual M5: %+v", row)
			}
		}
	}
	if !found {
		t.Fatal("own item absent")
	}
	c, b = f.do(http.MethodGet, base+"/"+p.ID+"/excel?snapshot_id="+view.Snapshot.ID, authz.KNTinh, dTT, actor, "", nil)
	f.ok(c, b, nil)
	x, err := excelize.OpenReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = x.Close() }()
	rows, err := x.GetRows("Tiến độ phòng")
	if err != nil {
		t.Fatal(err)
	}
	matched := false
	for _, row := range rows {
		if len(row) >= 6 && row[1] == expectedName {
			matched = true
			if row[4] != expectedQty {
				t.Fatalf("Excel quantity differs from M5: %v", row)
			}
		}
		if strings.Contains(strings.Join(row, "|"), "Tin bài") {
			t.Fatalf("Excel exposed another department: %v", row)
		}
	}
	if !matched {
		t.Fatal("Excel missing own item")
	}
	for _, suffix := range []string{"/periods?year=2026", "/" + p.ID, "/" + p.ID + "/excel?snapshot_id=" + view.Snapshot.ID} {
		c, b = f.do(http.MethodGet, base+suffix, authz.KNTinh, dTTTT, actor, "", nil)
		if c != http.StatusForbidden {
			t.Fatalf("other department must be403 for %s, got%d body=%s", suffix, c, b)
		}
		c, b = f.do(http.MethodGet, base+suffix, authz.KNXa, dTT, actor, "", nil)
		if c != http.StatusForbidden {
			t.Fatalf("commune must be403 for %s, got%d body=%s", suffix, c, b)
		}
	}
}
