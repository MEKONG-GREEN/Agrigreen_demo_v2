package kn77_test

// W11.45 — Báo cáo phòng CNTS BM-11 trên module THẬT qua HTTP + PostgreSQL: pf/forms (kỳ native kn_cnts BM-04, BM-17 nộp/chốt)
// + pf/extension (/v1/extension/cnts/bm11/*, sổ pf 20261028034853) + pf/export THẬT (Word, số {n}/BC-CNTS).
// Kiểm: quy tắc 13 (chỉ lập khi đủ tỷ lệ nộp hoặc kỳ đã chốt; "Tổng hợp từ n/N xã" + xã thiếu), một báo cáo mỗi kỳ (kể cả tạo
// đồng thời), gửi lặp cùng Idempotency-Key, khóa lạc quan, máy trạng thái (Đang soạn → Chờ duyệt → Trả lại có ý kiến → Đã gửi
// → Đã nhận từng nơi nhận), quyền (Lãnh đạo phòng duyệt; phòng nhận chỉ thấy báo cáo đã gửi tới mình — AC-02), đã gửi không
// sửa (BR-02), sổ không UPDATE được, Word chỉ cấp số cho báo cáo đã gửi.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export"
	exportport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

type bm11Who struct {
	k     authz.KNContext
	actor uuid.UUID
}

func (f *aggFx) bm11Ctx(dept string, mod func(*authz.KNContext)) bm11Who {
	k := authz.KNContext{Role: authz.KNTinh, Department: dept, OrgRef: f.node, Scope: authz.Scope{Units: []string{"X1", "X2"}}}
	if mod != nil {
		mod(&k)
	}
	return bm11Who{k: k, actor: ids.New()}
}

func (f *aggFx) as(w bm11Who, method, path, idem string, body any) (int, []byte) {
	f.t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	kj, _ := json.Marshal(w.k)
	req.Header.Set("X-Test-KN", string(kj))
	req.Header.Set("X-Test-Actor", w.actor.String())
	if method != http.MethodGet {
		if idem == "" {
			idem = "t-" + ids.New().String()
		}
		req.Header.Set(idempotency.Header, idem)
	}
	rec := httptest.NewRecorder()
	f.eng.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

type bm11Resp struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Seq      int64  `json:"seq"`
	Version  int64  `json:"version"`
	Basis    string `json:"basis"`
	Communes string `json:"communes"`
	Replayed bool   `json:"replayed"`
	Coverage []struct {
		Form         string   `json:"form"`
		Missing      []string `json:"missing"`
		MissingNames []string `json:"missing_names"`
	} `json:"coverage"`
	Lines []struct {
		Key, Value string
	} `json:"lines"`
	Fishery struct {
		CatchCur string `json:"catch_cur"`
		CatchYTD string `json:"catch_ytd"`
	} `json:"fishery"`
	Sections []struct{ Key, Text string } `json:"sections"`
	Can      struct {
		Edit, Submit, Approve, Return, Receive bool
	} `json:"can"`
	ReturnComment string `json:"return_comment"`
	RecipientList []struct {
		Code     string `json:"code"`
		Received bool   `json:"received"`
	} `json:"recipient_list"`
	Items []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"items"`
	Ready   bool     `json:"ready"`
	Reasons []string `json:"reasons"`
	Herds   *struct {
		Communes []bm11HerdRow `json:"communes"`
		Total    bm11HerdRow   `json:"total"`
		Heads    []struct {
			Species, Household, Farm string
		} `json:"heads"`
	} `json:"herds"`
}

type bm11HerdRow struct {
	Commune, Name     string
	Households, Farms int
	ByType            map[string]int `json:"by_type"`
}

// household ghi một phiên bản hộ chăn nuôi thẳng vào sổ (pf 20261106164138 — như dịch vụ #251 ghi): hộ Đang dùng có check-in,
// Chờ bổ sung vị trí không có, Vô hiệu hóa có lý do.
func (f *aggFx) household(ref uuid.UUID, code, event, status, commune, typ string, heads int) {
	f.t.Helper()
	checkin, reason := "NULL", "NULL"
	if status == "active" {
		checkin = "'" + ids.New().String() + "'"
	}
	if event == "deactivated" {
		reason = "'Hộ ngừng chăn nuôi'"
	}
	if err := f.admin.exec(fmt.Sprintf(`INSERT INTO pf_extension.cnts_livestock_household (id, tenant_id, stream_key, household_ref, code,
	    event, status, commune_code, person_ref, livestock_type, species_codes, head_counts, checkin_ref, reason, event_time, actor_ref,
	    idem_key, rule_version)
	  VALUES ('%s', '%s', 'lhh:%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', '{heo}', '{%d}', %s, %s, now(), '%s', '%s',
	    'kn.cnts.livestock_household/v1')`, ids.New(), f.tid, ref, ref, code, event, status, commune, ids.New(), typ, heads, checkin, reason,
		ids.New(), "t-"+ids.New().String())); err != nil {
		f.t.Fatal(err)
	}
}

// seedHouseholds (CNTS v4.0 FN-16 AC-05): xã X1 có 12 hộ Đang dùng — 8 nông hộ, 3 trang trại nhỏ, 1 trang trại vừa — cộng
// 1 hộ Chờ bổ sung vị trí và 1 hộ đã vô hiệu hóa (không tính); xã X2 có 1 trang trại công nghiệp. Trả mã một nông hộ của X1.
func (f *aggFx) seedHouseholds() uuid.UUID {
	n := 0
	add := func(event, status, commune, typ string, heads int) uuid.UUID {
		n++
		ref := ids.New()
		f.household(ref, fmt.Sprintf("HCN%07d", n), event, status, commune, typ, heads)
		return ref
	}
	var first uuid.UUID
	for i := range 8 {
		r := add("created", "active", "X1", "nong_ho", 10)
		if i == 0 {
			first = r
		}
	}
	for range 3 {
		add("created", "active", "X1", "trang_trai_nho", 100)
	}
	add("created", "active", "X1", "trang_trai_vua", 500)
	add("created_pending", "pending_location", "X1", "trang_trai_vua", 999)
	gone := add("created", "active", "X1", "trang_trai_nho", 999)
	f.household(gone, fmt.Sprintf("HCN%07d", n), "deactivated", "deactivated", "X1", "trang_trai_nho", 999)
	add("created", "active", "X2", "trang_trai_cong_nghiep", 2000)
	return first
}

func (r bm11Resp) line(key string) string {
	for _, l := range r.Lines {
		if l.Key == key {
			return l.Value
		}
	}
	return "?"
}

func bm04Vals(heo, tiemHeo, hoaChat, ksgm string) map[string]string {
	v := map[string]string{}
	for _, k := range []string{"tong_dan_de", "tong_dan_trau", "tong_dan_bo", "tong_dan_vit", "tong_dan_ga", "tong_dan_cho_meo", "tong_dan_yen",
		"tiem_dich_ta_tht_heo", "tiem_lmlm_heo", "tiem_tai_xanh_heo", "tiem_vdnc_bo", "tiem_tht_bo", "tiem_lmlm_bo", "tiem_cum_gia_cam", "tiem_dai",
		"hoa_chat_gia_cam", "ksgm_gia_cam", "vit_chay_dong_dia_phuong", "vit_chay_dong_noi_khac"} {
		v[k] = "0"
	}
	v["tong_dan_heo"], v["tiem_ta_heo_cp"], v["hoa_chat_gia_suc"], v["ksgm_gia_suc"] = heo, tiemHeo, hoaChat, ksgm
	return v
}

func TestCNTSBM11_W1145(t *testing.T) {
	f := newAggFx(t, facilityDir{}, func(_ *forms.Module, kn *extension.Module) {
		pool := pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform)
		ex, err := export.New(pool, clock.NewFixed(time.Date(2026, 10, 7, 3, 0, 0, 0, time.UTC)), export.Deps{
			Access: exportport.AccessFunc(func(context.Context) bool { return false }), Templates: []exportport.TemplateRegistration{extension.ExportTemplates()}})
		if err != nil {
			t.Fatal(err)
		}
		kn.UseExporter(ex.Exporter())
	})
	c, b := f.call(http.MethodPost, "/v1/extension/cnts/period-forms/install", authz.KNAdmin, nil)
	f.ok(c, b, nil)
	// Tuần CNTS 39 (22/09–28/09, tháng 9) và 40 (29/09–05/10, ngày cuối tháng 10); đồng hồ 10:00 07/10 — sau hạn 12:00 06/10.
	s04 := f.publish("CNTS-BM04", "2026-09-22", "2026-10-05", "X1", "X2")
	s17 := f.publish("CNTS-BM17", "2026-09-22", "2026-10-05", "X1", "X2")
	w39b04, w40b04, w39b17, w40b17 := s04.Periods[0].ID, s04.Periods[1].ID, s17.Periods[0].ID, s17.Periods[1].ID

	writer := f.bm11Ctx(aggDept, nil)
	leader := f.bm11Ctx(aggDept, func(k *authz.KNContext) { k.Manager = true; k.ProvinceGroup = "lanh_dao_phong" })
	hcth := f.bm11Ctx(partyport.DeptHanhChinhTongHop, nil)
	khtc := f.bm11Ctx(partyport.DeptKeHoachTaiChinh, nil)
	trongTrot := f.bm11Ctx(partyport.DeptTrongTrot, nil)
	base := "/v1/extension/cnts/bm11"

	t.Run("quy tắc 13: chưa đủ tỷ lệ nộp và kỳ chưa chốt → không lập được", func(t *testing.T) {
		f.submit(w40b04, "X1", bm04Vals("100", "5", "1.5", "10"))
		var p bm11Resp
		c, b := f.as(writer, http.MethodGet, base+"/preview?type=week&key=2026-W40", "", nil)
		f.ok(c, b, &p)
		if p.Ready || len(p.Reasons) == 0 {
			t.Fatalf("1/2 xã (50%%) < 80%% phải chưa lập được: %s", b)
		}
		c, b = f.as(writer, http.MethodPost, base+"/reports", "", map[string]any{"type": "week", "period_key": "2026-W40"})
		f.fail(c, b, "extension.cnts_bm11_not_ready")
	})

	f.submit(w40b04, "X2", bm04Vals("50", "2", "0.25", "4"))
	f.submit(w39b04, "X1", bm04Vals("90", "1", "1", "1"))
	f.submit(w39b17, "X1", catchVals("2", "1", "0", "0"))
	f.submit(w39b17, "X2", catchVals("3", "0", "0", "0.5"))
	f.submit(w40b17, "X1", catchVals("4", "0", "1", "0"))
	f.submit(w40b17, "X2", catchVals("1", "0", "0", "0"))

	changed := f.seedHouseholds()

	var week bm11Resp
	idem := "t-" + ids.New().String()
	t.Run("lập báo cáo tuần: số tự động từ bài đã nộp, Tổng hợp từ 2/2 xã, gửi lặp trả bản cũ", func(t *testing.T) {
		c, b := f.as(writer, http.MethodPost, base+"/reports", idem, map[string]any{"type": "week", "period_key": "2026-W40"})
		if c != http.StatusCreated {
			t.Fatalf("HTTP %d %s", c, b)
		}
		f.ok(c, b, &week)
		if week.Status != "draft" || week.Communes != "Tổng hợp từ 2/2 xã" || week.Basis != "submitted" {
			t.Fatalf("đầu báo cáo: %s", b)
		}
		// AC-01: tổng mỗi chỉ tiêu = tổng số hợp lệ của kỳ.
		if week.line("tong_dan_heo") != "150" || week.line("tiem_heo") != "7" || week.line("tiem_ta_heo_cp") != "7" ||
			week.line("hoa_chat_gia_suc") != "1.75" || week.line("ksgm_gia_suc") != "14" {
			t.Fatalf("chăn nuôi: %+v", week.Lines)
		}
		if week.Fishery.CatchCur != "6" || week.Fishery.CatchYTD != "12.5" {
			t.Fatalf("khai thác tuần 40 = 6, lũy kế năm = 12,5: %+v", week.Fishery)
		}
		// CNTS v4.0 FN-16 BR-07, AC-05: bảng theo loại chăn nuôi đếm từ danh sách hộ (hộ Đang dùng).
		h := week.Herds
		if h == nil || len(h.Communes) != 2 || h.Communes[0].Commune != "X1" {
			t.Fatalf("bảng theo loại chăn nuôi: %s", b)
		}
		x1 := h.Communes[0]
		if x1.Households != 12 || x1.Farms != 4 || x1.ByType["nong_ho"] != 8 || x1.ByType["trang_trai_nho"] != 3 ||
			x1.ByType["trang_trai_vua"] != 1 || x1.ByType["trang_trai_cong_nghiep"] != 0 {
			t.Fatalf("AC-05 xã X1 = 12 hộ, 4 trang trại; nông hộ 8, nhỏ 3, vừa 1, công nghiệp 0: %+v", x1)
		}
		if h.Total.Households != 13 || h.Total.Farms != 5 || len(h.Heads) != 1 || h.Heads[0].Household != "80" || h.Heads[0].Farm != "2800" {
			t.Fatalf("toàn tỉnh 13 hộ, 5 trang trại; heo nông hộ 80, trang trại 2800: %+v", h)
		}
		var again bm11Resp
		c, b = f.as(writer, http.MethodPost, base+"/reports", idem, map[string]any{"type": "week", "period_key": "2026-W40"})
		f.ok(c, b, &again)
		// Middleware idempotency của nền tảng trả lại đúng phản hồi lần đầu; bên trong app cũng tra khóa idem (đầu báo cáo).
		if again.ID != week.ID {
			t.Fatalf("gửi lặp: HTTP %d %s", c, b)
		}
		c, b = f.as(writer, http.MethodPost, base+"/reports", "", map[string]any{"type": "week", "period_key": "2026-W40"})
		f.fail(c, b, "extension.cnts_bm11_exists")
	})

	// FN-10 BR-06: hộ đổi loại sau khi lập không làm đổi báo cáo đã lập; "Làm mới số liệu" mới đếm lại.
	f.household(changed, "HCN0000001", "updated", "active", "X1", "trang_trai_nho", 10)
	t.Run("BR-06: hộ đổi loại sau khi lập không đổi báo cáo; làm mới đếm lại", func(t *testing.T) {
		var v bm11Resp
		c, b := f.as(writer, http.MethodGet, base+"/reports/"+week.ID, "", nil)
		f.ok(c, b, &v)
		if v.Herds == nil || v.Herds.Communes[0].ByType["nong_ho"] != 8 || v.Herds.Total.Farms != 5 {
			t.Fatalf("báo cáo đã lập phải giữ số cũ: %s", b)
		}
		c, b = f.as(writer, http.MethodPost, base+"/reports/"+week.ID+"/refresh", "", map[string]any{"expected_seq": v.Seq})
		f.ok(c, b, &v)
		if v.Herds.Communes[0].ByType["nong_ho"] != 7 || v.Herds.Communes[0].ByType["trang_trai_nho"] != 4 || v.Herds.Total.Farms != 6 {
			t.Fatalf("làm mới đếm lại theo loại hiện hành: %s", b)
		}
		week = v
	})

	t.Run("soạn: khóa lạc quan, mục lạ, phòng khác không soạn được", func(t *testing.T) {
		c, b := f.as(writer, http.MethodPut, base+"/reports/"+week.ID+"/sections", "", map[string]any{"expected_seq": week.Seq + 5,
			"sections": map[string]string{"ke_hoach": "x"}})
		f.fail(c, b, "extension.cnts_bm11_version_conflict")
		c, _ = f.as(writer, http.MethodPut, base+"/reports/"+week.ID+"/sections", "", map[string]any{"expected_seq": week.Seq,
			"sections": map[string]string{"phuong_huong": "không có ở mẫu tuần"}})
		if c != http.StatusUnprocessableEntity {
			t.Fatalf("mục lạ: HTTP %d", c)
		}
		c, _ = f.as(trongTrot, http.MethodPut, base+"/reports/"+week.ID+"/sections", "", map[string]any{"expected_seq": week.Seq,
			"sections": map[string]string{"ke_hoach": "x"}})
		if c != http.StatusForbidden {
			t.Fatalf("phòng Trồng trọt: HTTP %d", c)
		}
		var v bm11Resp
		c, b = f.as(writer, http.MethodPut, base+"/reports/"+week.ID+"/sections", "", map[string]any{"expected_seq": week.Seq,
			"sections": map[string]string{"ke_hoach": "Tiếp tục điều tra tổng đàn.", "de_xuat": "Không."}})
		f.ok(c, b, &v)
		if v.Seq != week.Seq+1 || v.Version != v.Seq {
			t.Fatalf("phiên bản mới: %s", b)
		}
		week = v
	})

	t.Run("trình → trả lại bắt buộc ý kiến (AC-04) → trình lại → duyệt tự gửi (BR-03)", func(t *testing.T) {
		c, _ := f.as(writer, http.MethodPost, base+"/reports/"+week.ID+"/approve", "", map[string]any{"expected_seq": week.Seq})
		if c != http.StatusForbidden {
			t.Fatalf("cán bộ không duyệt được: HTTP %d", c)
		}
		var v bm11Resp
		c, b := f.as(writer, http.MethodPost, base+"/reports/"+week.ID+"/submit", "", map[string]any{"expected_seq": week.Seq})
		f.ok(c, b, &v)
		if v.Status != "pending" {
			t.Fatalf("trình: %s", b)
		}
		c, b = f.as(writer, http.MethodPut, base+"/reports/"+week.ID+"/sections", "", map[string]any{"expected_seq": v.Seq,
			"sections": map[string]string{"ke_hoach": "sửa khi chờ duyệt"}})
		f.fail(c, b, "extension.cnts_bm11_state")
		// Phòng nhận chưa thấy báo cáo chưa gửi.
		var inbox bm11Resp
		c, b = f.as(hcth, http.MethodGet, base+"/inbox", "", nil)
		f.ok(c, b, &inbox)
		if len(inbox.Items) != 0 {
			t.Fatalf("chưa gửi mà HCTH đã thấy: %s", b)
		}
		c, _ = f.as(leader, http.MethodPost, base+"/reports/"+week.ID+"/return", "", map[string]any{"expected_seq": v.Seq, "comment": "  "})
		if c != http.StatusUnprocessableEntity {
			t.Fatalf("AC-04 trả lại thiếu ý kiến: HTTP %d", c)
		}
		c, b = f.as(leader, http.MethodPost, base+"/reports/"+week.ID+"/return", "", map[string]any{"expected_seq": v.Seq, "comment": "Bổ sung kế hoạch"})
		f.ok(c, b, &v)
		if v.Status != "returned" || v.ReturnComment != "Bổ sung kế hoạch" {
			t.Fatalf("trả lại: %s", b)
		}
		c, b = f.as(writer, http.MethodPost, base+"/reports/"+week.ID+"/submit", "", map[string]any{"expected_seq": v.Seq})
		f.ok(c, b, &v)
		c, b = f.as(leader, http.MethodPost, base+"/reports/"+week.ID+"/approve", "", map[string]any{"expected_seq": v.Seq})
		f.ok(c, b, &v)
		if v.Status != "sent" || v.Can.Edit {
			t.Fatalf("duyệt = đã gửi: %s", b)
		}
		week = v
	})

	t.Run("AC-02: HCTH thấy ngay ở Báo cáo đến, KHTC (không phải nơi nhận báo cáo tuần) không thấy; Đã nhận một lần", func(t *testing.T) {
		var inbox bm11Resp
		c, b := f.as(hcth, http.MethodGet, base+"/inbox", "", nil)
		f.ok(c, b, &inbox)
		if len(inbox.Items) != 1 || inbox.Items[0].ID != week.ID || inbox.Items[0].Status != "sent" {
			t.Fatalf("hộp HCTH: %s", b)
		}
		c, b = f.as(khtc, http.MethodGet, base+"/inbox", "", nil)
		f.ok(c, b, &inbox)
		if len(inbox.Items) != 0 {
			t.Fatalf("hộp KHTC: %s", b)
		}
		c, _ = f.as(khtc, http.MethodGet, base+"/reports/"+week.ID, "", nil)
		if c != http.StatusNotFound {
			t.Fatalf("KHTC đọc báo cáo tuần: HTTP %d", c)
		}
		c, _ = f.as(hcth, http.MethodGet, base+"/reports", "", nil)
		if c != http.StatusForbidden {
			t.Fatalf("HCTH xem danh sách soạn của CNTS: HTTP %d", c)
		}
		var v bm11Resp
		c, b = f.as(hcth, http.MethodPost, base+"/reports/"+week.ID+"/receive", "", nil)
		f.ok(c, b, &v)
		c, b = f.as(hcth, http.MethodPost, base+"/reports/"+week.ID+"/receive", "", nil)
		f.ok(c, b, &v)
		got := 0
		for _, r := range v.RecipientList {
			if r.Received {
				got++
				if r.Code != "hcth" {
					t.Fatalf("nơi nhận: %s", b)
				}
			}
		}
		if got != 1 || v.Can.Receive {
			t.Fatalf("Đã nhận một lần: %s", b)
		}
		c, b = f.as(writer, http.MethodPut, base+"/reports/"+week.ID+"/sections", "", map[string]any{"expected_seq": v.Seq,
			"sections": map[string]string{"ke_hoach": "sửa sau khi gửi"}})
		f.fail(c, b, "extension.cnts_bm11_state")
		if err := f.admin.exec(`UPDATE pf_extension.cnts_bm11_event SET comment = 'x' WHERE report_id = '` + week.ID + `'`); err == nil {
			t.Fatal("sổ BM-11 phải không UPDATE được")
		}
	})

	t.Run("Word: bản đã gửi cấp số BC-CNTS, xuất lại dùng lại số; tải lại đúng băm", func(t *testing.T) {
		var r1, r2 struct {
			ID        string `json:"id"`
			DocNumber string `json:"doc_number"`
		}
		c, b := f.as(writer, http.MethodPost, base+"/reports/"+week.ID+"/word", "", nil)
		f.ok(c, b, &r1)
		c, b = f.as(hcth, http.MethodPost, base+"/reports/"+week.ID+"/word", "", nil)
		f.ok(c, b, &r2)
		if !strings.HasSuffix(r1.DocNumber, "/BC-CNTS") || r1.DocNumber != r2.DocNumber {
			t.Fatalf("số văn bản: %q %q", r1.DocNumber, r2.DocNumber)
		}
		c, b = f.as(writer, http.MethodGet, base+"/reports/"+week.ID+"/words/"+r1.ID+"/download", "", nil)
		if c != http.StatusOK || len(b) < 1000 || !bytes.HasPrefix(b, []byte("PK")) {
			t.Fatalf("tải .docx: HTTP %d, %d byte", c, len(b))
		}
	})

	t.Run("báo cáo tháng 9: kỳ nguồn đã chốt → lập được dù thiếu xã; Tổng hợp từ 1/2 xã + xã thiếu; bản nháp không cấp số", func(t *testing.T) {
		c, b := f.as(writer, http.MethodPost, base+"/reports", "", map[string]any{"type": "month", "period_key": "2026-09"})
		f.fail(c, b, "extension.cnts_bm11_not_ready") // BM-04 tuần 39: 1/2 xã, kỳ chưa chốt
		f.closePeriod(w39b04)
		f.closePeriod(w39b17)
		// Hai lần tạo đồng thời cùng kỳ: đúng một báo cáo.
		var wg sync.WaitGroup
		codes := make([]int, 2)
		bodies := make([][]byte, 2)
		for i := range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				codes[i], bodies[i] = f.as(writer, http.MethodPost, base+"/reports", "", map[string]any{"type": "month", "period_key": "2026-09"})
			}()
		}
		wg.Wait()
		if (codes[0] == http.StatusCreated) == (codes[1] == http.StatusCreated) {
			t.Fatalf("tạo đồng thời: %d %s / %d %s", codes[0], bodies[0], codes[1], bodies[1])
		}
		var m bm11Resp
		for i := range 2 {
			if codes[i] == http.StatusCreated {
				f.ok(codes[i], bodies[i], &m)
			} else {
				f.fail(codes[i], bodies[i], "extension.cnts_bm11_exists")
			}
		}
		if m.Basis != "closed" || m.Communes != "Tổng hợp từ 1/2 xã" || m.line("tong_dan_heo") != "90" || m.Fishery.CatchCur != "6.5" {
			t.Fatalf("tháng 9: %s", bodies)
		}
		found := false
		for _, cv := range m.Coverage {
			if cv.Form == "CNTS-BM04" && len(cv.Missing) == 1 && cv.Missing[0] == "X2" {
				found = true
			}
		}
		if !found {
			t.Fatalf("xã thiếu: %+v", m.Coverage)
		}
		// Làm mới số liệu giữ phần viết.
		var v bm11Resp
		c, b = f.as(writer, http.MethodPut, base+"/reports/"+m.ID+"/sections", "", map[string]any{"expected_seq": m.Seq,
			"sections": map[string]string{"kho_khan": "Thiếu vốn đối ứng."}})
		f.ok(c, b, &v)
		c, b = f.as(writer, http.MethodPost, base+"/reports/"+m.ID+"/refresh", "", map[string]any{"expected_seq": v.Seq})
		f.ok(c, b, &v)
		kept := false
		for _, s := range v.Sections {
			kept = kept || (s.Key == "kho_khan" && s.Text == "Thiếu vốn đối ứng.")
		}
		if !kept || v.Seq != m.Seq+2 {
			t.Fatalf("làm mới: %s", b)
		}
		var r struct {
			DocNumber string `json:"doc_number"`
		}
		c, b = f.as(writer, http.MethodPost, base+"/reports/"+m.ID+"/word", "", nil)
		f.ok(c, b, &r)
		if r.DocNumber != "" {
			t.Fatalf("bản nháp không tiêu số: %q", r.DocNumber)
		}
		var list bm11Resp
		c, b = f.as(writer, http.MethodGet, base+"/reports?type=month", "", nil)
		f.ok(c, b, &list)
		if len(list.Items) != 1 || list.Items[0].ID != m.ID {
			t.Fatalf("danh sách tháng: %s", b)
		}
	})
}
