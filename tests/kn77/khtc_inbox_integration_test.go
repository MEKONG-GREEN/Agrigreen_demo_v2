package kn77_test

// W11.11 — "Báo cáo cho KHTC" phía phòng (FN-PN-03.01) trên module THẬT qua HTTP: KHTC mở kỳ (W11.62), phòng thấy phiếu
// của mình ở /v1/extension/khtc/department-forms, gửi qua /v1/forms (kn_khtc), KHTC yêu cầu bổ sung; kỳ hủy bị gỡ.

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

type khtcInbox struct {
	Department string `json:"department"`
	View       string `json:"view"`
	ReadOnly   bool   `json:"read_only"`
	Counts     struct {
		Total       int `json:"total"`
		NeedsAction int `json:"needs_action"`
		Supplement  int `json:"supplement"`
		Sent        int `json:"sent"`
	} `json:"counts"`
	Items []struct {
		PeriodID       string  `json:"period_id"`
		FormsPeriodID  string  `json:"forms_period_id"`
		ReportType     string  `json:"report_type"`
		State          string  `json:"state"`
		Attempt        int64   `json:"attempt"`
		NeedsAction    bool    `json:"needs_action"`
		Action         string  `json:"action"`
		SupplementNote *string `json:"supplement_note"`
		EntryRoute     string  `json:"entry_route"`
	} `json:"items"`
}

func (f *khtcFx) inbox(role, dept, query string) khtcInbox {
	f.t.Helper()
	var in khtcInbox
	c, b := f.do(http.MethodGet, "/v1/extension/khtc/department-forms"+query, role, dept, uuid.Nil, "", nil)
	f.ok(c, b, &in)
	return in
}

func TestKHTCDepartmentForms_W1111(t *testing.T) {
	f := newKHTCFx(t)
	me := ids.New()
	tt, cn, tttt := partyport.DeptTrongTrot, partyport.DeptChanNuoiThuySan, partyport.DeptThongTinTruyenThong
	c, b := f.do(http.MethodPost, "/v1/extension/khtc/form-template/install", authz.KNAdmin, "", uuid.Nil, "", nil)
	f.ok(c, b, nil)
	var w41, m10 khtcPeriod
	c, b = f.khtc(http.MethodPost, "/periods", me, "", map[string]any{"report_type": "week", "year": 2026, "no": 41, "departments": []string{tt, cn}})
	f.ok(c, b, &w41)
	c, b = f.khtc(http.MethodPost, "/periods", me, "", map[string]any{"report_type": "month", "year": 2026, "no": 10, "departments": []string{tt}})
	f.ok(c, b, &m10)

	t.Run("FN-PN-03.01 phiếu kỳ này: chỉ phiếu của phòng, Cần xử lý, hạn gần nhất trước", func(t *testing.T) {
		in := f.inbox(authz.KNTinh, tt, "")
		if in.Department != tt || in.View != "current" || in.Counts.Total != 2 || in.Counts.NeedsAction != 2 {
			t.Fatalf("Trồng trọt: %+v", in)
		}
		if in.Items[0].PeriodID != w41.ID || in.Items[0].Action != "enter" || in.Items[0].State != "not_started" ||
			in.Items[0].EntryRoute != "/kn/khtc/phieu/"+w41.ID {
			t.Fatalf("thứ tự/nút: %+v", in.Items)
		}
		if got := f.inbox(authz.KNTinh, cn, ""); got.Counts.Total != 1 || got.Items[0].PeriodID != w41.ID {
			t.Fatalf("CNTS chỉ có tuần 41: %+v", got)
		}
		if got := f.inbox(authz.KNTinh, tttt, ""); got.Counts.Total != 0 || got.Items == nil {
			t.Fatalf("TTTT không tham gia: %+v", got)
		}
	})

	t.Run("FN-PN-03.01 quyền: phòng khác, Cấp Xã, Admin phải chọn phòng", func(t *testing.T) {
		c, b := f.do(http.MethodGet, "/v1/extension/khtc/department-forms?department="+cn, authz.KNTinh, tt, uuid.Nil, "", nil)
		f.fail(c, b, "forbidden")
		c, b = f.do(http.MethodGet, "/v1/extension/khtc/department-forms", authz.KNXa, tt, uuid.Nil, "", nil)
		f.fail(c, b, "forbidden")
		c, b = f.do(http.MethodGet, "/v1/extension/khtc/department-forms", authz.KNAdmin, "", uuid.Nil, "", nil)
		f.fail(c, b, "validation_failed")
		in := f.inbox(authz.KNAdmin, "", "?department="+tt)
		if in.Counts.Total != 2 || in.Items[0].EntryRoute != "/kn/khtc/phieu/"+w41.ID+"?department="+tt {
			t.Fatalf("Admin nhập thay: %+v", in)
		}
		c, b = f.do(http.MethodGet, "/v1/extension/khtc/department-forms?view=all", authz.KNTinh, tt, uuid.Nil, "", nil)
		f.fail(c, b, "validation_failed")
	})

	t.Run("FN-PN-03.01 gửi → Xem; yêu cầu bổ sung → Sửa, gửi lại kèm nội dung; phiếu đã gửi", func(t *testing.T) {
		sub := f.submit(w41.FormsPeriodID, tt)
		in := f.inbox(authz.KNTinh, tt, "")
		if in.Counts.NeedsAction != 1 || in.Items[0].PeriodID != m10.ID || in.Items[1].Action != "view" || in.Items[1].State != "submitted" {
			t.Fatalf("sau gửi: %+v", in.Items)
		}
		f.review(sub, "returned", me)
		in = f.inbox(authz.KNTinh, tt, "")
		var found bool
		for _, it := range in.Items {
			if it.PeriodID == w41.ID {
				found = true
				if it.State != "supplement" || it.Action != "resubmit" || it.SupplementNote == nil || *it.SupplementNote == "" || it.Attempt != 1 {
					t.Fatalf("yêu cầu bổ sung: %+v", it)
				}
			}
		}
		if !found || in.Counts.Supplement != 1 {
			t.Fatalf("yêu cầu bổ sung: %+v", in)
		}
		sent := f.inbox(authz.KNTinh, tt, "?view=sent")
		if sent.Counts.Total != 1 || sent.Items[0].PeriodID != w41.ID {
			t.Fatalf("phiếu đã gửi: %+v", sent)
		}
	})

	t.Run("BR-DL-18 kỳ hủy gỡ khỏi module phòng", func(t *testing.T) {
		c, b := f.khtc(http.MethodPost, "/periods/"+m10.ID+"/cancel", me, "", map[string]any{"reason": "Mở nhầm kỳ báo cáo tháng"})
		f.ok(c, b, nil)
		in := f.inbox(authz.KNTinh, tt, "")
		for _, it := range in.Items {
			if it.PeriodID == m10.ID {
				t.Fatalf("kỳ đã hủy còn hiện: %+v", in.Items)
			}
		}
		raw, _ := json.Marshal(in)
		if in.Counts.Total != 1 {
			t.Fatalf("sau hủy: %s", raw)
		}
	})
}
