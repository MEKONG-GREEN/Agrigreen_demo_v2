// Test-only native fixture; production interfaces remain unchanged.
package adhocintegration_test

import (
	"bytes"
	"context"
	"encoding/json"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// crosswalk giả của gói PFPUB: một kỳ nguồn CNTS cho mỗi tuần KHTC, trạng thái chốt điều khiển được.
type crosswalk struct{ closed bool }

func (c *crosswalk) Sources(_ context.Context, kind, key string, _, _ time.Time, depts []string) ([]extport.KHTCSourceRef, error) {
	for _, d := range depts {
		if d == partyport.DeptChanNuoiThuySan && kind == "week" {
			return []extport.KHTCSourceRef{{Department: d, Source: "cnts.bm04", CalendarID: "cnts.week", CalendarVersion: "v1",
				NativeKey: key, Label: "BM-04 " + key}}, nil
		}
	}
	return nil, nil
}

func (c *crosswalk) States(_ context.Context, refs []extport.KHTCSourceRef) ([]extport.KHTCSourceState, error) {
	out := make([]extport.KHTCSourceState, len(refs))
	for i := range refs {
		out[i].Closed = c.closed
	}
	return out, nil
}

type khtcPeriod struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	State          string    `json:"state"`
	FormsPeriodID  string    `json:"forms_period_id"`
	FormsStatus    string    `json:"forms_status"`
	DueAt          time.Time `json:"due_at"`
	MaxSubmissions int       `json:"max_submissions"`
	CanClose       bool      `json:"can_close"`
	CloseForced    bool      `json:"close_forced"`
	Counts         struct {
		Sources int `json:"sources"`
	} `json:"counts"`
	Forms []struct {
		Department           string `json:"department"`
		State                string `json:"state"`
		Attempt              int64  `json:"attempt"`
		LimitReached         bool   `json:"limit_reached"`
		CanRequestSupplement bool   `json:"can_request_supplement"`
		CanConfirm           bool   `json:"can_confirm"`
		Late                 bool   `json:"late"`
	} `json:"forms"`
	Events []struct {
		Action string `json:"action"`
		Forced bool   `json:"forced"`
	} `json:"events"`
	// nháp
	Duplicate       *struct{ ID string } `json:"duplicate"`
	Sources         []json.RawMessage    `json:"sources"`
	TemplateVersion int64                `json:"template_version"`
}

type khtcFx struct {
	t    *testing.T
	clk  *clock.Fixed
	cw   *crosswalk
	eng  *gin.Engine
	tid  uuid.UUID
	node uuid.UUID
}

// do gọi API như phiên (vai, phòng ban, người); idem rỗng → khóa mới. Trả mã HTTP và thân.
func (f *khtcFx) do(method, path, role, dept string, actor uuid.UUID, idem string, body any) (int, []byte) {
	f.t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	requestCtx, cancel := context.WithTimeout(req.Context(), 30*time.Second)
	defer cancel()
	req = req.WithContext(requestCtx)
	req.Header.Set("Content-Type", "application/json")
	k := authz.KNContext{Role: role, Department: dept, OrgRef: f.node, Scope: authz.Scope{Units: []string{"X1"}}}
	if role == authz.KNAdmin {
		k = authz.KNContext{Role: role, Scope: authz.Scope{All: true}}
	}
	kj, _ := json.Marshal(k)
	req.Header.Set("X-Test-KN", string(kj))
	if actor == uuid.Nil {
		actor = ids.New()
	}
	req.Header.Set("X-Test-Actor", actor.String())
	if method == http.MethodPost {
		if idem == "" {
			idem = "t-" + ids.New().String()
		}
		req.Header.Set(idempotency.Header, idem)
	}
	w := httptest.NewRecorder()
	f.eng.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

func (f *khtcFx) ok(code int, b []byte, out any) {
	f.t.Helper()
	if code >= 300 {
		f.t.Fatalf("HTTP %d: %s", code, b)
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			f.t.Fatal(err)
		}
	}
}

const khtcDept = partyport.DeptKeHoachTaiChinh

func (f *khtcFx) khtc(method, path string, actor uuid.UUID, idem string, body any) (int, []byte) {
	return f.do(method, "/v1/extension/khtc"+path, authz.KNTinh, khtcDept, actor, idem, body)
}
