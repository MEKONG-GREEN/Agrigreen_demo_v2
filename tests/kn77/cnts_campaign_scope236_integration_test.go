package kn77_test

// CNTS-CAMPAIGN-SCOPE (#236) — BRD CNTS v4.0 FN-21 (ma trận quyền "Chiến dịch điều tra mở rộng", luồng chính B1 "người phụ
// trách", luồng phụ "sao chép đợt cũ") trên module THẬT qua HTTP và PostgreSQL: giao người phụ trách đợt; chỉ cán bộ tổng
// hợp hoặc người phụ trách (viên chức, chủ nhiệm) sửa, phát hành, nhắc, gia hạn, đóng — người khác 403; Lãnh đạo phòng chỉ
// xem và mở lại; sao chép đợt cũ ra đợt Nháp mới mang biểu, phạm vi, người phụ trách nhưng KHÔNG mang số liệu.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// campOfficers là danh bạ cán bộ KN giả (pf/accounts KNDirectory) theo phòng × tỉnh.
type campOfficers []accport.KNOfficer

func (d campOfficers) Officers(_ context.Context, _ uuid.UUID, dept, province string) ([]accport.KNOfficer, error) {
	if dept != aggDept || province != "89" {
		return nil, nil
	}
	return d, nil
}

// campaignAs gọi API với phiên Cấp Tỉnh CNTS của người actor thuộc nhóm group (manager = Lãnh đạo phòng).
func (f *aggFx) campaignAs(method, path string, actor uuid.UUID, group string, manager bool, body any) (int, []byte) {
	f.t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	k := authz.KNContext{Role: authz.KNTinh, Department: aggDept, OrgRef: f.node, Scope: authz.Scope{Units: []string{"X1", "X2"}},
		Manager: manager, ProvinceGroup: group}
	kj, _ := json.Marshal(k)
	req.Header.Set("X-Test-KN", string(kj))
	req.Header.Set("X-Test-Actor", actor.String())
	if method == http.MethodPost {
		req.Header.Set(idempotency.Header, "t-"+ids.New().String())
	}
	w := httptest.NewRecorder()
	f.eng.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

func TestCNTSCampaignOwnerAndCopy_236(t *testing.T) {
	tongHop, vien, vienKhac, lanhDao, xa := ids.New(), ids.New(), ids.New(), ids.New(), ids.New()
	dir := campOfficers{
		{ActorRef: tongHop, Level: accport.ScheduleLevelProvince, ProvinceCode: "89"},
		{ActorRef: vien, Level: accport.ScheduleLevelProvince, ProvinceCode: "89"},
		{ActorRef: vienKhac, Level: accport.ScheduleLevelProvince, ProvinceCode: "89"},
		{ActorRef: lanhDao, Level: accport.ScheduleLevelProvince, ProvinceCode: "89"},
		{ActorRef: xa, Level: accport.ScheduleLevelCommune, ProvinceCode: "89", Communes: []string{"X1"}},
	}
	f := newAggFx(t, facilityDir{}, func(fm *forms.Module, kn *extension.Module) {
		kn.UseCNTSCampaigns(extension.CNTSCampaignDeps{Forms: fm.Publisher(), Control: fm.Control(), Conditional: fm.ConditionalControl(), Figures: fm.Figures()})
		kn.UseKNContext(nil, dir)
	})
	c, b := f.call(http.MethodPost, "/v1/extension/cnts/period-forms/install", authz.KNAdmin, nil)
	f.ok(c, b, nil)

	const base = "/v1/extension/cnts/campaigns"
	type view struct {
		campaignView
		CanManage    bool    `json:"can_manage"`
		TemplateCode string  `json:"template_code"`
		Goal         *string `json:"goal"`
		Communes     []string
		FromDate     string `json:"from_date"`
		Owner        *struct {
			Ref string `json:"ref"`
		} `json:"owner"`
	}
	input := func(owner uuid.UUID) map[string]any {
		return map[string]any{"name": "Điều tra tổng đàn 01/10/2026", "goal": "Tổng đàn sau lũ", "field": "chan_nuoi",
			"template_code": "CNTS-BM17", "scope_mode": "communes", "communes": []string{"X1", "X2"}, "from_date": "2026-10-01",
			"to_date": "2026-10-05", "due_at": "2026-10-09T03:00:00Z", "owner_ref": owner.String()}
	}
	var cv view

	t.Run("B1 giao người phụ trách: bắt buộc, phải là cán bộ Cấp Tỉnh CNTS của tỉnh", func(t *testing.T) {
		in := input(vien)
		delete(in, "owner_ref")
		c, b := f.campaignAs(http.MethodPost, base, tongHop, accport.GroupTongHop, false, in)
		f.fail(c, b, "validation_failed")
		c, b = f.campaignAs(http.MethodPost, base, tongHop, accport.GroupTongHop, false, input(xa))
		f.fail(c, b, "extension.cnts_campaign_owner")
		c, b = f.campaignAs(http.MethodPost, base, tongHop, accport.GroupTongHop, false, input(ids.New()))
		f.fail(c, b, "extension.cnts_campaign_owner")
		// Viên chức chỉ tạo đợt mình phụ trách; Lãnh đạo phòng không tạo.
		c, b = f.campaignAs(http.MethodPost, base, vienKhac, accport.GroupVienChuc, false, input(vien))
		f.fail(c, b, "forbidden")
		c, b = f.campaignAs(http.MethodPost, base, lanhDao, accport.GroupLanhDaoPhong, true, input(lanhDao))
		f.fail(c, b, "forbidden")
		c, b = f.campaignAs(http.MethodPost, base, tongHop, accport.GroupTongHop, false, input(vien))
		f.ok(c, b, &cv)
		if c != http.StatusCreated || cv.Owner == nil || cv.Owner.Ref != vien.String() || !cv.CanManage {
			t.Fatalf("tạo đợt giao người phụ trách: %d %s", c, b)
		}
	})

	t.Run("Danh sách người phụ trách: cán bộ Cấp Tỉnh của tỉnh; viên chức chỉ thấy mình", func(t *testing.T) {
		var out struct {
			Items []struct {
				Ref string `json:"ref"`
			} `json:"items"`
		}
		c, b := f.campaignAs(http.MethodGet, "/v1/extension/cnts/campaign-owners", tongHop, accport.GroupTongHop, false, nil)
		f.ok(c, b, &out)
		if len(out.Items) != 4 {
			t.Fatalf("cán bộ tổng hợp: %s", b)
		}
		for _, it := range out.Items {
			if it.Ref == xa.String() {
				t.Fatalf("cán bộ Cấp Xã không được làm người phụ trách đợt: %s", b)
			}
		}
		c, b = f.campaignAs(http.MethodGet, "/v1/extension/cnts/campaign-owners", vien, accport.GroupVienChuc, false, nil)
		f.ok(c, b, &out)
		if len(out.Items) != 1 || out.Items[0].Ref != vien.String() {
			t.Fatalf("viên chức: %s", b)
		}
	})

	t.Run("Người không phụ trách đợt bị từ chối 403 khi sửa, phát hành, nhắc, đóng", func(t *testing.T) {
		in := input(vien)
		in["name"] = "Điều tra tổng đàn (người khác sửa)"
		c, b := f.campaignAs(http.MethodPost, base+"/"+cv.ID+"/draft", vienKhac, accport.GroupVienChuc, false, in)
		f.fail(c, b, "forbidden")
		c, b = f.campaignAs(http.MethodPost, base+"/"+cv.ID+"/publish", vienKhac, accport.GroupChuNhiem, false, nil)
		f.fail(c, b, "forbidden")
		c, b = f.campaignAs(http.MethodPost, base+"/"+cv.ID+"/publish", lanhDao, accport.GroupLanhDaoPhong, true, nil)
		f.fail(c, b, "forbidden")
		var other view
		c, b = f.campaignAs(http.MethodGet, base+"/"+cv.ID, vienKhac, accport.GroupVienChuc, false, nil)
		f.ok(c, b, &other)
		if other.CanManage || other.State != "draft" {
			t.Fatalf("người khác chỉ xem: %s", b)
		}
		// Người phụ trách sửa nháp (giữ mình là người phụ trách) và phát hành.
		in["name"] = "Điều tra tổng đàn 01/10/2026 (bản sửa)"
		c, b = f.campaignAs(http.MethodPost, base+"/"+cv.ID+"/draft", vien, accport.GroupVienChuc, false, in)
		f.ok(c, b, &cv)
		if cv.Version != 2 || !cv.CanManage {
			t.Fatalf("người phụ trách sửa nháp: %s", b)
		}
		in["owner_ref"] = vienKhac.String()
		c, b = f.campaignAs(http.MethodPost, base+"/"+cv.ID+"/draft", vien, accport.GroupVienChuc, false, in)
		f.fail(c, b, "forbidden")
		c, b = f.campaignAs(http.MethodPost, base+"/"+cv.ID+"/publish", vien, accport.GroupVienChuc, false, nil)
		f.ok(c, b, &cv)
		if cv.State != "collecting" || cv.FormsPeriodID == nil {
			t.Fatalf("người phụ trách phát hành: %s", b)
		}
		f.submit(*cv.FormsPeriodID, "X1", catchVals("100", "200", "0", "0"))
		c, b = f.campaignAs(http.MethodPost, base+"/"+cv.ID+"/remind", vienKhac, accport.GroupVienChuc, false, nil)
		f.fail(c, b, "forbidden")
		c, b = f.campaignAs(http.MethodPost, base+"/"+cv.ID+"/extend", vienKhac, accport.GroupVienChuc, false,
			map[string]any{"due_at": "2026-10-12T03:00:00Z", "reason": "Xã X2 mất điện nhiều ngày"})
		f.fail(c, b, "forbidden")
		c, b = f.campaignAs(http.MethodPost, base+"/"+cv.ID+"/close", vienKhac, accport.GroupVienChuc, false, nil)
		f.fail(c, b, "forbidden")
		// Cán bộ tổng hợp quản lý mọi đợt của Nút; Lãnh đạo phòng mở lại.
		c, b = f.campaignAs(http.MethodPost, base+"/"+cv.ID+"/close", tongHop, accport.GroupTongHop, false, nil)
		f.ok(c, b, &cv)
		if cv.State != "closed" {
			t.Fatalf("đóng đợt: %s", b)
		}
		c, b = f.campaignAs(http.MethodPost, base+"/"+cv.ID+"/reopen", lanhDao, accport.GroupLanhDaoPhong, true,
			map[string]any{"due_at": "2026-10-15T03:00:00Z", "reason": "Bổ sung số liệu xã X2 theo chỉ đạo"})
		f.ok(c, b, &cv)
		if cv.State != "collecting" {
			t.Fatalf("Lãnh đạo phòng mở lại: %s", b)
		}
		c, b = f.campaignAs(http.MethodPost, base+"/"+cv.ID+"/close", vien, accport.GroupVienChuc, false, nil)
		f.ok(c, b, &cv)
	})

	t.Run("Sao chép đợt cũ: đợt Nháp mới mang biểu, phạm vi, người phụ trách; không mang số liệu", func(t *testing.T) {
		body := map[string]any{"name": "Điều tra tổng đàn 01/04/2027", "from_date": "2026-10-20", "to_date": "2026-10-25",
			"due_at": "2026-10-28T03:00:00Z"}
		c, b := f.campaignAs(http.MethodPost, base+"/"+cv.ID+"/copy", vienKhac, accport.GroupVienChuc, false, body)
		f.fail(c, b, "forbidden") // viên chức khác sao chép mà giữ người phụ trách cũ
		var cp view
		c, b = f.campaignAs(http.MethodPost, base+"/"+cv.ID+"/copy", tongHop, accport.GroupTongHop, false, body)
		f.ok(c, b, &cp)
		if c != http.StatusCreated || cp.ID == cv.ID || cp.State != "draft" || cp.Version != 1 || cp.Name != body["name"] ||
			cp.TemplateCode != "CNTS-BM17" || cp.Goal == nil || *cp.Goal != "Tổng đàn sau lũ" || cp.Tasks != 2 ||
			cp.FromDate != "2026-10-20" || cp.Owner == nil || cp.Owner.Ref != vien.String() ||
			cp.FormsPeriodID != nil || cp.Progress != nil || len(cp.Events) != 0 {
			t.Fatalf("sao chép: %d %s", c, b)
		}
		// Viên chức sao chép và nhận mình làm người phụ trách.
		body["owner_ref"] = vienKhac.String()
		var mine view
		c, b = f.campaignAs(http.MethodPost, base+"/"+cv.ID+"/copy", vienKhac, accport.GroupVienChuc, false, body)
		f.ok(c, b, &mine)
		if mine.Owner == nil || mine.Owner.Ref != vienKhac.String() || !mine.CanManage {
			t.Fatalf("viên chức sao chép: %s", b)
		}
		// Đợt sao chép phát hành thành kỳ native riêng: không có bài, kết quả rỗng.
		c, b = f.campaignAs(http.MethodPost, base+"/"+cp.ID+"/publish", vien, accport.GroupVienChuc, false, nil)
		f.ok(c, b, &cp)
		if cp.FormsPeriodID == nil || *cp.FormsPeriodID == *cv.FormsPeriodID || cp.Progress.Submitted != 0 || cp.Progress.Pending != 2 {
			t.Fatalf("phát hành đợt sao chép: %s", b)
		}
		var r struct {
			Reported int               `json:"reported"`
			Total    map[string]string `json:"total"`
		}
		c, b = f.campaignAs(http.MethodGet, base+"/"+cp.ID+"/results", vien, accport.GroupVienChuc, false, nil)
		f.ok(c, b, &r)
		if r.Reported != 0 || (r.Total["ca"] != "" && r.Total["ca"] != "0") {
			t.Fatalf("đợt sao chép mang số liệu cũ: %s", b)
		}
		c, b = f.campaignAs(http.MethodPost, base+"/"+ids.New().String()+"/copy", tongHop, accport.GroupTongHop, false, body)
		f.fail(c, b, "extension.cnts_campaign_not_found")
	})
}

// lhh ghi một phiên bản hộ chăn nuôi (sổ #251) thẳng vào DB (DB tính seq, băm) — đầu vào cho phạm vi theo hộ.
func (f *aggFx) lhh(ref uuid.UUID, code, event, status, commune string) {
	f.t.Helper()
	checkin := "NULL"
	if status == "active" {
		checkin = "'" + ids.New().String() + "'"
	}
	reason := "NULL"
	if event == "deactivated" {
		reason = "'Hộ ngừng chăn nuôi'"
	}
	sql := fmt.Sprintf(`INSERT INTO pf_extension.cnts_livestock_household (id, tenant_id, stream_key, household_ref, code, event, status,
	    commune_code, person_ref, livestock_type, species_codes, head_counts, checkin_ref, reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ('%s', '%s', 'lhh:%s', '%s', '%s', '%s', '%s', '%s', '%s', 'nong_ho', '{heo}', '{10}', %s, %s, now(), '%s', '%s',
	    'kn.cnts.livestock_household/v1')`, ids.New(), f.tid, ref, ref, code, event, status, commune, ids.New(), checkin, reason, ids.New(),
		"t-"+ids.New().String())
	if _, err := f.adminExec(sql); err != nil {
		f.t.Fatal(err)
	}
}

// FN-21 B1 phạm vi "hộ" (ADR-0056 mục 9): chỉ hộ Đang dùng, xã trong phạm vi Nút; phát hành mỗi hộ một việc của cán bộ xã chứa
// hộ (kỳ native đơn vị lá hộ, cha = xã); tiến độ, kết quả theo hộ; sao chép mang danh sách hộ; hộ vô hiệu hóa sau đó → 422.
func TestCNTSCampaignHouseholdScope_236(t *testing.T) {
	tongHop := ids.New()
	f := newAggFx(t, facilityDir{}, func(fm *forms.Module, kn *extension.Module) {
		kn.UseCNTSCampaigns(extension.CNTSCampaignDeps{Forms: fm.Publisher(), Control: fm.Control(), Conditional: fm.ConditionalControl(), Figures: fm.Figures()})
	})
	c, b := f.call(http.MethodPost, "/v1/extension/cnts/period-forms/install", authz.KNAdmin, nil)
	f.ok(c, b, nil)
	h1, h2, pending, outside := ids.New(), ids.New(), ids.New(), ids.New()
	f.lhh(h1, "HCN0000001", "created", "active", "X1")
	f.lhh(h2, "HCN0000002", "created", "active", "X2")
	f.lhh(pending, "HCN0000003", "created_pending", "pending_location", "X1")
	f.lhh(outside, "HCN0000004", "created", "active", "X9")

	const base = "/v1/extension/cnts/campaigns"
	type hh struct {
		Ref, Code, Commune, Status string
	}
	type view struct {
		campaignView
		ScopeMode  string `json:"scope_mode"`
		Communes   []string
		Households []hh
		Progress2  *struct {
			Units []struct {
				Unit          string `json:"unit"`
				HouseholdCode string `json:"household_code"`
				Commune       string `json:"commune"`
			} `json:"units"`
		} `json:"progress"`
	}
	input := func(refs ...uuid.UUID) map[string]any {
		hs := []string{}
		for _, r := range refs {
			hs = append(hs, r.String())
		}
		return map[string]any{"name": "Điều tra hộ nuôi heo 10/2026", "field": "chan_nuoi", "template_code": "CNTS-BM17",
			"scope_mode": "households", "households": hs, "from_date": "2026-10-01", "to_date": "2026-10-05",
			"due_at": "2026-10-09T03:00:00Z", "owner_ref": tongHop.String()}
	}
	var cv view
	t.Run("chỉ hộ Đang dùng, trong phạm vi Nút", func(t *testing.T) {
		for _, bad := range [][]uuid.UUID{{h1, pending}, {h1, outside}, {ids.New()}} {
			c, b := f.campaignAs(http.MethodPost, base, tongHop, accport.GroupTongHop, false, input(bad...))
			f.fail(c, b, "extension.cnts_campaign_household")
		}
		c, b := f.campaignAs(http.MethodPost, base, tongHop, accport.GroupTongHop, false, map[string]any{"name": "x", "field": "chan_nuoi",
			"template_code": "CNTS-BM17", "scope_mode": "households", "households": []string{}, "from_date": "2026-10-01",
			"to_date": "2026-10-05", "due_at": "2026-10-09T03:00:00Z", "owner_ref": tongHop.String()})
		f.fail(c, b, "validation_failed")
		c, b = f.campaignAs(http.MethodPost, base, tongHop, accport.GroupTongHop, false, input(h1, h2, h1))
		f.ok(c, b, &cv)
		if c != http.StatusCreated || cv.ScopeMode != "households" || cv.Tasks != 2 || len(cv.Communes) != 2 || len(cv.Households) != 2 ||
			cv.Households[0].Code != "HCN0000001" || cv.Households[1].Commune != "X2" {
			t.Fatalf("đợt theo hộ: %d %s", c, b)
		}
	})

	t.Run("phát hành: mỗi hộ một việc ở xã chứa hộ; tiến độ, kết quả theo hộ", func(t *testing.T) {
		c, b := f.campaignAs(http.MethodPost, base+"/"+cv.ID+"/publish", tongHop, accport.GroupTongHop, false, nil)
		f.ok(c, b, &cv)
		if cv.FormsPeriodID == nil || cv.Progress2 == nil || len(cv.Progress2.Units) != 2 ||
			cv.Progress2.Units[0].HouseholdCode != "HCN0000001" || cv.Progress2.Units[0].Commune != "X1" || cv.Progress2.Units[0].Unit != h1.String() {
			t.Fatalf("phát hành theo hộ: %s", b)
		}
		var tasks struct {
			Items []struct {
				PeriodID string `json:"period_id"`
				Unit     string `json:"unit"`
				UnitKind string `json:"unit_kind"`
				Parent   string `json:"parent"`
			} `json:"items"`
		}
		c, b = f.call(http.MethodGet, "/v1/forms/tasks?profile=kn_cnts&code=CNTS-BM17", authz.KNXa, nil)
		f.ok(c, b, &tasks)
		got := map[string]string{}
		for _, it := range tasks.Items {
			if it.PeriodID == *cv.FormsPeriodID {
				if it.UnitKind != "household" {
					t.Fatalf("việc theo hộ: %s", b)
				}
				got[it.Unit] = it.Parent
			}
		}
		if len(got) != 2 || got[h1.String()] != "X1" || got[h2.String()] != "X2" {
			t.Fatalf("việc của cán bộ xã: %v — %s", got, b)
		}
		f.submit(*cv.FormsPeriodID, h1.String(), catchVals("12", "0", "0", "0"))
		var r struct {
			Reported int `json:"reported"`
			Rows     []struct {
				HouseholdCode string            `json:"household_code"`
				Commune       string            `json:"commune"`
				Values        map[string]string `json:"values"`
			} `json:"rows"`
			Total map[string]string `json:"total"`
		}
		c, b = f.campaignAs(http.MethodGet, base+"/"+cv.ID+"/results", tongHop, accport.GroupTongHop, false, nil)
		f.ok(c, b, &r)
		if r.Reported != 1 || len(r.Rows) != 2 || r.Total["ca"] != "12" {
			t.Fatalf("kết quả theo hộ: %s", b)
		}
		for _, row := range r.Rows {
			if row.HouseholdCode == "HCN0000001" && (row.Commune != "X1" || row.Values["ca"] != "12") {
				t.Fatalf("dòng hộ: %s", b)
			}
		}
		c, _ = f.campaignAs(http.MethodGet, base+"/"+cv.ID+"/results/xlsx", tongHop, accport.GroupTongHop, false, nil)
		if c != http.StatusOK {
			t.Fatalf("xuất Excel theo hộ: %d", c)
		}
	})

	t.Run("sao chép mang danh sách hộ; hộ vô hiệu hóa sau đó không phát hành được", func(t *testing.T) {
		var cp view
		c, b := f.campaignAs(http.MethodPost, base+"/"+cv.ID+"/copy", tongHop, accport.GroupTongHop, false,
			map[string]any{"from_date": "2026-10-20", "to_date": "2026-10-25", "due_at": "2026-10-28T03:00:00Z"})
		f.ok(c, b, &cp)
		if cp.ScopeMode != "households" || len(cp.Households) != 2 || cp.Tasks != 2 || cp.FormsPeriodID != nil {
			t.Fatalf("sao chép theo hộ: %s", b)
		}
		f.lhh(h2, "HCN0000002", "deactivated", "deactivated", "X2")
		c, b = f.campaignAs(http.MethodPost, base+"/"+cp.ID+"/publish", tongHop, accport.GroupTongHop, false, nil)
		f.fail(c, b, "extension.cnts_campaign_household")
		c, b = f.campaignAs(http.MethodPost, base+"/"+cv.ID+"/copy", tongHop, accport.GroupTongHop, false,
			map[string]any{"from_date": "2026-10-20", "to_date": "2026-10-25", "due_at": "2026-10-28T03:00:00Z"})
		f.fail(c, b, "extension.cnts_campaign_household")
	})
}
