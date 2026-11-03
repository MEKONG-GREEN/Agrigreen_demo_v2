package handler

import (
	"encoding/json"
	"strings"
	"testing"
)

// The check runs at the JSON boundary, before decimal rounding/comparison.
func TestPlanSaveRejectsUnboundedDecimalJSON(t *testing.T) {
	for _, field := range []string{"planned_qty", "budget", "PLANNED_QTY", "Budget"} {
		for _, value := range []string{`"1e100000"`, `"1e-100000"`, `1e100000`, `1e-100000`, `"` + strings.Repeat("9", 100000) + `"`} {
			t.Run(field+"/"+value[:min(len(value), 16)], func(t *testing.T) {
				for _, suffix := range []string{"", `,"` + field + `":null`, `,"` + field + `":"1"`} {
					var req saveReq
					if err := json.Unmarshal([]byte(`{"entries":[{"`+field+`":`+value+suffix+`}]}`), &req); err == nil {
						t.Fatal("unbounded decimal accepted before arithmetic")
					}
				}
			})
		}
	}
}

func TestPlanSaveKeepsBoundedDecimalJSON(t *testing.T) {
	for _, value := range []string{`null`, `"0"`, `"120.5000"`, `120.5`, `"999999999999999999.99"`, `"-1"`} {
		var req saveReq
		if err := json.Unmarshal([]byte(`{"entries":[{"planned_qty":`+value+`,"budget":`+value+`}]}`), &req); err != nil {
			t.Fatalf("bounded decimal %s: %v", value, err)
		}
	}
}
