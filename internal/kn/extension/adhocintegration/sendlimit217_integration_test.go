package adhocintegration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	forms "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

type lostFinalSubmit217 struct {
	forms.Entry
	lost bool
}

func (e *lostFinalSubmit217) Submit(ctx context.Context, profile string, period uuid.UUID, unit string, values map[string]string, base int64, key string) (forms.SubmissionRef, error) {
	ack, err := e.Entry.Submit(ctx, profile, period, unit, values, base, key)
	if err == nil && base == 2 && !e.lost {
		e.lost = true
		return forms.SubmissionRef{}, errors.New("lost final native ACK217")
	}
	return ack, err
}

func TestKHTCFixedThreeAndFinalLostACK217(t *testing.T) {
	f := newKHTCFormFxWith(t, nil)
	wrapper := &lostFinalSubmit217{Entry: f.nativeEntry}
	f.ext.UseKHTCPeriods(extension.KHTCDeps{Forms: f.nativeForms, Control: f.nativeControl, Entry: wrapper, MaxSubmissions: 9})
	p := f.open(41, dTT)
	if p.MaxSubmissions != 3 {
		t.Fatalf("legacy wiring selected max%d", p.MaxSubmissions)
	}

	// Exercise the actual native HTTP owner as well as the KHTC facade. This
	// fails if the real startup profile ever stops registering its fixed rule.
	var native struct {
		Period struct {
			TemplateID string `json:"template_id"`
		} `json:"period"`
	}
	code, body := f.do(http.MethodGet, "/v1/forms/periods/"+p.FormsPeriodID+"?profile=kn_khtc", authz.KNTinh, khtcDept, f.khtcUser, "", nil)
	f.ok(code, body, &native)
	if native.Period.TemplateID == "" {
		t.Fatalf("missing public native template identity: %s", body)
	}
	for _, maximum := range []any{0, 1, 9, "1e100000", "1e-100000", strings.Repeat("9", 100000)} {
		code, body = f.do(http.MethodPost, "/v1/forms/periods", authz.KNTinh, khtcDept, f.khtcUser, "", map[string]any{
			"profile": "kn_khtc", "template_id": native.Period.TemplateID, "label": "Forbidden cap override", "due_at": p.DueAt, "units": []string{dTT}, "unit_kind": "department", "max_submissions": maximum})
		if code != http.StatusUnprocessableEntity && code != http.StatusBadRequest {
			t.Fatalf("native cap override must reject: HTTP %d %s", code, body)
		}
	}
	var nativeList struct {
		Items []json.RawMessage `json:"items"`
	}
	code, body = f.do(http.MethodGet, "/v1/forms/periods?profile=kn_khtc", authz.KNTinh, khtcDept, f.khtcUser, "", nil)
	f.ok(code, body, &nativeList)
	if len(nativeList.Items) != 1 {
		t.Fatalf("rejected native publications created periods: %d", len(nativeList.Items))
	}
	actor := ids.New()
	var current struct {
		State      string `json:"state"`
		Attempt    int64  `json:"attempt"`
		CanConfirm bool   `json:"can_confirm"`
		CanRequest bool   `json:"can_request_supplement"`
		Editable   bool   `json:"editable"`
	}
	for n := int64(0); n < 3; n++ {
		key := fmt.Sprintf("send217-%d", n)
		body := map[string]any{"base_attempt": n, "sheet": ttSheet(f.item, "2")}
		code, raw := f.do(http.MethodPost, formPath(p.ID, dTT)+"/submit", authz.KNTinh, dTT, actor, key, body)
		if n == 2 {
			if code < 500 {
				t.Fatalf("lost ACK must be uncertain, got %d %s", code, raw)
			}
			code, raw = f.do(http.MethodPost, formPath(p.ID, dTT)+"/submit", authz.KNTinh, dTT, actor, key, body)
		}
		f.ok(code, raw, &current)
		if n < 2 {
			code, raw = f.reviewForm241(p.ID, dTT, "returned", "Bổ sung số liệu")
			f.ok(code, raw, nil)
		}
	}
	if current.State != "locked" || current.Attempt != 3 || current.CanConfirm || current.CanRequest || current.Editable {
		t.Fatalf("final form: %+v", current)
	}
	for _, decision := range []string{"accepted", "returned"} {
		code, raw := f.reviewForm241(p.ID, dTT, decision, "Không được rà soát sau lần3")
		if code != http.StatusConflict {
			t.Fatalf("review %s: %d %s", decision, code, raw)
		}
	}
	code, raw := f.do(http.MethodPost, formPath(p.ID, dTT)+"/submit", authz.KNTinh, dTT, actor, "fourth217", map[string]any{"base_attempt": 3, "sheet": ttSheet(f.item, "3")})
	if code != http.StatusConflict {
		t.Fatalf("fourth: %d %s", code, raw)
	}
	code, raw = f.khtc(http.MethodGet, "/periods/"+p.ID, f.khtcUser, "", nil)
	f.ok(code, raw, &p)
	if p.Forms[0].State != "locked" || p.Forms[0].CanConfirm || p.Forms[0].CanRequestSupplement {
		t.Fatalf("period projection: %s", raw)
	}
	var dbCounts struct {
		Submissions int
		Receipts    int
		Seals       int
	}
	admin := pgtest.Get(t).AdminPool(t, migrations.PF)
	err := admin.QueryRow(context.Background(), `SELECT
	 (SELECT count(*) FROM pf_forms.submission WHERE tenant_id=$1 AND period_id=$2),
	 (SELECT count(*) FROM pf_forms.submission_receipt WHERE tenant_id=$1 AND period_id=$2),
	 (SELECT count(*) FROM pf_extension.khtc_form_sheet WHERE tenant_id=$1 AND period_id=$3 AND kind='sealed')`, f.tid, p.FormsPeriodID, p.ID).Scan(&dbCounts.Submissions, &dbCounts.Receipts, &dbCounts.Seals)
	if err != nil {
		t.Fatal(err)
	}
	if dbCounts.Submissions != 3 || dbCounts.Receipts != 3 || dbCounts.Seals != 3 {
		raw, _ := json.Marshal(dbCounts)
		t.Fatalf("lost ACK/reject created side effects: %s", raw)
	}
}
