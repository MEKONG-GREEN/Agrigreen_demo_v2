package kn77_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// An acknowledged save and a later current draft are different facts. Keep the
// first operation identity while successors change the report, including ABA.
func TestKHTCReport244_SaveReceiptSurvivesSuccessorAndABA(t *testing.T) {
	f := newReportFx(t)
	p := f.open(41, dTT)
	initial := f.refresh(p.ID, 0)
	input := map[string]any{"base_version": initial.Version, "texts": map[string]string{"B": "Nội dung đã lưu"}}
	key := ids.New().String()
	code, body := f.do(http.MethodPost, reportPath(p.ID)+"/draft", authz.KNTinh, khtcDept, f.khtcUser, key, input)
	var saved struct {
		Version int64 `json:"version"`
		Receipt struct {
			OperationID string `json:"operation_id"`
			Outcome     string `json:"outcome"`
			Applied     struct {
				DraftID string `json:"draft_id"`
				Version int64  `json:"version"`
				Actor   string `json:"actor_ref"`
			} `json:"applied"`
		} `json:"save_receipt"`
	}
	f.ok(code, body, &saved)
	if saved.Receipt.OperationID == "" || saved.Receipt.Outcome != "applied" || saved.Receipt.Applied.Version != saved.Version || saved.Receipt.Applied.Actor != f.khtcUser.String() {
		t.Fatalf("save has no exact atomic operation receipt: %s", body)
	}
	for _, text := range []string{"Bản thay thế", "Nội dung đã lưu"} {
		code, body = f.call(http.MethodPost, p.ID, "/draft", map[string]any{"base_version": saved.Version, "texts": map[string]string{"B": text}})
		var next struct {
			Version int64 `json:"version"`
		}
		f.ok(code, body, &next)
		saved.Version = next.Version
	}
	code, body = f.call(http.MethodGet, p.ID, "/draft-operations/"+saved.Receipt.OperationID, nil)
	var receipt struct {
		OperationID string `json:"operation_id"`
		Outcome     string `json:"outcome"`
		Applied     struct {
			DraftID string `json:"draft_id"`
			Version int64  `json:"version"`
			Actor   string `json:"actor_ref"`
		} `json:"applied"`
	}
	f.ok(code, body, &receipt)
	if receipt.OperationID != saved.Receipt.OperationID || receipt.Outcome != "applied" || receipt.Applied != saved.Receipt.Applied || receipt.Applied.Version >= saved.Version {
		t.Fatalf("receipt was reconstructed from successor/ABA head: %s", body)
	}
	code, body = f.call(http.MethodGet, p.ID, "/draft-audit", nil)
	var audit struct {
		Items []json.RawMessage `json:"items"`
	}
	f.ok(code, body, &audit)
	if len(audit.Items) != 3 {
		t.Fatalf("want three actual save audits, got %s", body)
	}
}
