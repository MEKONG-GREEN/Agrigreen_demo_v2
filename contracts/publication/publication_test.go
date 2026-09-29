package publication

import (
	"errors"
	"testing"
)

func TestEmbeddedContractsApproved(t *testing.T) {
	// hub_*: G2; collection_schedule, htx_collection_statement, mrv_collection_evidence: dự thảo G3-0.
	for _, n := range []string{"hub_facility", "hub_capacity", "collection_schedule", "htx_collection_statement", "mrv_collection_evidence"} {
		c, err := Get(n)
		if err != nil {
			t.Fatalf("%s: %v", n, err)
		}
		if p := c.MetaProblems(); len(p) != 0 {
			t.Fatalf("%s: %v", n, p)
		}
		if _, _, ok := c.EventType(); !ok {
			t.Fatalf("%s phải khai sự kiện", n)
		}
	}
}

func TestValidateDefaultDeny(t *testing.T) {
	c, err := Parse([]byte(`{"name":"x_y","version":1,"owner":"o","proposed_by":"a","approved_by":"b","approved_at":"2026-01-01",
	  "required":["id","n"],
	  "properties":{"id":{"type":"string","format":"uuid"},"n":{"type":"integer"},"k":{"type":"string","enum":["a","b"]},
	    "p":{"type":"string","pattern":"^[0-9]+\\.[0-9]{2}$"},"at":{"type":"string","format":"date-time"},
	    "o":{"type":["object","null"],"properties":{"v":{"type":"number"}}},
	    "xs":{"type":"array","items":{"type":"object","required":["c"],"properties":{"c":{"type":"string"}}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if p := c.MetaProblems(); len(p) != 0 {
		t.Fatal(p)
	}
	ok := `{"id":"0191f2c4-0000-7000-8000-000000000000","n":3,"k":"a","p":"1.25","at":"2026-09-29T10:00:00Z","o":{"v":1.5},"xs":[{"c":"z"}]}`
	if err := c.Validate([]byte(ok)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		`{"id":"0191f2c4-0000-7000-8000-000000000000"}`,                              // thiếu n
		`{"id":"nope","n":1}`,                                                        // uuid
		`{"id":"0191f2c4-0000-7000-8000-000000000000","n":1.5}`,                      // integer
		`{"id":"0191f2c4-0000-7000-8000-000000000000","n":1,"k":"c"}`,                // enum
		`{"id":"0191f2c4-0000-7000-8000-000000000000","n":1,"p":"1.2"}`,              // pattern
		`{"id":"0191f2c4-0000-7000-8000-000000000000","n":1,"at":"hôm qua"}`,         // date-time
		`{"id":"0191f2c4-0000-7000-8000-000000000000","n":1,"extra":true}`,           // khóa lạ
		`{"id":"0191f2c4-0000-7000-8000-000000000000","n":1,"o":{"v":1,"w":2}}`,      // khóa lạ lồng
		`{"id":"0191f2c4-0000-7000-8000-000000000000","n":1,"xs":[{"c":"z"},{}]}`,    // required trong mảng
		`{"id":"0191f2c4-0000-7000-8000-000000000000","n":1,"xs":[{"c":"z","d":1}]}`, // khóa lạ trong mảng
		`[1]`,
	} {
		if err := c.Validate([]byte(bad)); !errors.Is(err, ErrDenied) {
			t.Fatalf("%s phải bị từ chối: %v", bad, err)
		}
	}
	if err := c.Validate([]byte(`{"id":"0191f2c4-0000-7000-8000-000000000000","n":1,"o":null}`)); err != nil {
		t.Fatalf("null được phép khi type có null: %v", err)
	}
}

func TestLegacyFieldsContract(t *testing.T) {
	c, _ := Parse([]byte(`{"name":"old_one","version":1,"owner":"o","proposed_by":"a","approved_by":"b","approved_at":"2026-01-01","fields":["a"]}`))
	if err := c.Validate([]byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := c.Validate([]byte(`{"a":1,"b":2}`)); !errors.Is(err, ErrDenied) {
		t.Fatalf("khóa ngoài fields phải bị từ chối: %v", err)
	}
}
