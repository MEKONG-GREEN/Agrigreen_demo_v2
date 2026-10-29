package cntsknqg_test

// W11.53b (#158 — BRD CNTS v3.7 FN-19 AC-02, UR-QG-01, UR-TINH-21) trên PostgreSQL thật: (1) sự kiện thông báo
// pf.extension.cnts_knqg.changed ghi outbox cùng giao dịch khi gửi Mẫu 07 (người nhận: Cấp TW phòng CNTS), TW trả lại /
// xác nhận (chủ nhiệm), trình Mẫu 01 (lãnh đạo phòng của tỉnh); (2) ảnh thực địa Mẫu 03 (kho đối tượng, giới hạn cỡ/kiểu/
// số ảnh, quyền, khóa khi trình ký, phát lại theo Idempotency-Key, sổ chỉ ghi thêm).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// holders giả lập pf/accounts RoleHolders (Cấp TW phòng CNTS).
type holders struct {
	mu   sync.Mutex
	tw   []uuid.UUID
	fail bool
}

func (h *holders) RoleHolders(_ context.Context, _ uuid.UUID, role, dept string) ([]uuid.UUID, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.fail {
		return nil, errors.New("giả: danh bạ lỗi")
	}
	if role != authz.KNTW || dept != cnts {
		return nil, nil
	}
	return slices.Clone(h.tw), nil
}

func (h *holders) DutyHolders(context.Context, uuid.UUID, string, string, string) ([]uuid.UUID, error) {
	return nil, nil
}

// directory giả lập pf/accounts KNDirectory (lãnh đạo phòng Cấp Tỉnh của tỉnh 89).
type directory struct{ officers []accport.KNOfficer }

func (d directory) Officers(_ context.Context, _ uuid.UUID, dept, province string) ([]accport.KNOfficer, error) {
	if dept != cnts || province != "89" {
		return nil, nil
	}
	return d.officers, nil
}

type knqgEvent struct {
	DocRef        string   `json:"doc_ref"`
	Form          string   `json:"form"`
	Action        string   `json:"action"`
	Status        string   `json:"status"`
	ProjectCode   string   `json:"project_code"`
	RecipientRefs []string `json:"recipient_refs"`
}

func (f *fix) knqgEvents(t *testing.T) []knqgEvent {
	t.Helper()
	rows, err := f.pool.Query(context.Background(), `SELECT payload FROM shared.outbox WHERE tenant_id = $1 AND event_type = 'pf.extension.cnts_knqg.changed'
	  ORDER BY occurred_at, id`, f.tenant)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []knqgEvent
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var e knqgEvent
		if err := json.Unmarshal(raw, &e); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	return out
}

func photoBody(name string, data []byte, caption string) ([]byte, string) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if caption != "" {
		_ = w.WriteField("caption", caption)
	}
	fw, _ := w.CreateFormFile("file", name)
	_, _ = fw.Write(data)
	_ = w.Close()
	return buf.Bytes(), w.FormDataContentType()
}

func (u user) addPhoto(ref, name string, data []byte, caption string) resp {
	body, ct := photoBody(name, data, caption)
	return u.send(base_+ref+"/photos", key(), ct, body)
}

func png(i int) []byte {
	return append([]byte("\x89PNG\r\n\x1a\n"), []byte(fmt.Sprintf("anh-thuc-dia-%d", i))...)
}
func jpeg(i int) []byte {
	return append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, []byte(fmt.Sprintf("jpeg-%d", i))...)
}

func TestCNTSKNQGNotifyPhotos_W1153b(t *testing.T) {
	f := newFix(t)
	chu := f.user(tinh("chu_nhiem"))
	chu2 := f.user(tinh("chu_nhiem"))
	ld := f.user(tinh("lanh_dao_phong"))
	tw := f.user(authz.KNContext{Role: authz.KNTW, Department: cnts, Scope: authz.Scope{Units: []string{"89", "30001", "30002"}}})
	officer := f.user(xa("30001"))
	f.xa.list = []accport.XaScope{{ActorRef: officer.ref, Communes: []string{"30001"}}}
	tw2 := uuid.New()
	hs := &holders{tw: []uuid.UUID{tw.ref, tw2, uuid.Nil, tw2}}
	f.mod.UseRoleHolders(hs)
	f.mod.UseKNContext(nil, directory{officers: []accport.KNOfficer{
		{ActorRef: ld.ref, Level: accport.ScheduleLevelProvince, ProvinceCode: "89", MgmtLevel: "truong_phong"},
		{ActorRef: chu.ref, Level: accport.ScheduleLevelProvince, ProvinceCode: "89"},
		{ActorRef: officer.ref, Level: accport.ScheduleLevelCommune, ProvinceCode: "89", Communes: []string{"30001"}},
	}})

	p := want(t, chu.post(base+"projects", map[string]any{"code": "KNQG-VIT-2027", "name": "Mô hình vịt an toàn sinh học", "kind": "knqg",
		"start_date": "2027-03-01", "end_date": "2027-12-31", "budget": "300000000"}), 201, "", "dự án KNQG")
	pref := str(p, "ref")
	sA := str(want(t, chu.post(base+"demo-sites", map[string]any{"project_ref": pref, "commune_code": "30001", "name": "Điểm A",
		"model_type_code": "GA_TP", "scale": "1000", "scale_unit": "con", "officer_ref": officer.ref.String(), "diary_form": "BM-08",
		"location": map[string]any{"lat": 10.38, "lng": 105.43, "source": "map"}}), 201, "", "điểm A"), "ref")
	newDoc := func(u user, form string, extra map[string]any) map[string]any {
		body := map[string]any{"form": form, "project_ref": pref}
		for k, v := range extra {
			body[k] = v
		}
		return want(t, u.post(base+"knqg-docs", body), 201, "", "lập Mẫu "+form)
	}
	upd := func(u user, d map[string]any, fields map[string]any) map[string]any {
		return want(t, u.post(base_+str(d, "ref"), map[string]any{"base_version": ver(d), "content": withFields(d, fields)}), 200, "", "sửa")
	}
	act := func(u user, d map[string]any, a, reason string) resp {
		return u.post(base_+str(d, "ref")+"/actions/"+a, map[string]any{"base_version": ver(d), "reason": reason})
	}

	// ----- (1) Thông báo -----
	// Mẫu 01 trình → lãnh đạo phòng của tỉnh (không phải chủ nhiệm, không phải Cấp Xã).
	d1 := newDoc(chu, "01", map[string]any{"period": "2027-10"})
	d1 = upd(chu, d1, map[string]any{"danh_gia": "Đúng tiến độ"})
	s1 := want(t, act(chu, d1, "submit", ""), 200, "", "trình Mẫu 01")
	ev := f.knqgEvents(t)
	if len(ev) != 1 || ev[0].Action != "submit" || ev[0].Form != "01" || ev[0].Status != "submitted" || ev[0].DocRef != str(d1, "ref") ||
		!slices.Equal(ev[0].RecipientRefs, []string{ld.ref.String()}) || ev[0].ProjectCode != "KNQG-VIT-2027" {
		t.Fatalf("trình Mẫu 01 → lãnh đạo phòng: %+v", ev)
	}
	// Duyệt / sửa nội dung không phát thông báo.
	want(t, act(ld, s1, "approve", ""), 200, "", "duyệt Mẫu 01")
	if n := len(f.knqgEvents(t)); n != 1 {
		t.Fatalf("duyệt không thông báo: %d", n)
	}

	// Mẫu 05 điểm A ký (điều kiện gửi Mẫu 07).
	d5 := newDoc(chu, "05", map[string]any{"site_ref": sA})
	d5 = upd(chu, d5, map[string]any{"don_vi_thuc_hien": "TTKN", "danh_gia": "Đạt"})
	w5 := want(t, act(chu, d5, "request_sign", ""), 200, "", "trình ký 05")
	want(t, chu.upload(base_+str(d5, "ref")+"/signed", ver(w5), "bb.pdf", pdf), 200, "", "ký 05")

	// Mẫu 07: gửi → Cấp TW phòng CNTS (bỏ trùng, bỏ Nil), một sự kiện cùng giao dịch.
	d7 := newDoc(chu, "07", map[string]any{"period": "2027"})
	d7 = upd(chu, d7, map[string]any{"hop_dong": "05/HĐ-KN", "nhan_xet": "Đạt yêu cầu"})
	sendKey := key()
	sent := want(t, chu.do(http.MethodPost, base_+str(d7, "ref")+"/actions/send", sendKey, map[string]any{"base_version": ver(d7)}), 200, "", "gửi Mẫu 07")
	ev = f.knqgEvents(t)
	twWant := []string{tw.ref.String(), tw2.String()}
	slices.Sort(twWant)
	if len(ev) != 2 || ev[1].Action != "send" || ev[1].Status != "sent" || ev[1].Form != "07" || !slices.Equal(ev[1].RecipientRefs, twWant) {
		t.Fatalf("AC-02: gửi Mẫu 07 → Cấp TW: %+v", ev)
	}
	// Bấm lại cùng khóa: phát lại, không thêm sự kiện.
	want(t, chu.do(http.MethodPost, base_+str(d7, "ref")+"/actions/send", sendKey, map[string]any{"base_version": ver(d7)}), 200, "", "phát lại gửi")
	if n := len(f.knqgEvents(t)); n != 2 {
		t.Fatalf("phát lại không thêm thông báo: %d", n)
	}
	// TW trả lại → chủ nhiệm; gửi lại khi danh bạ lỗi → thao tác vẫn thành công, không thông báo; TW xác nhận → chủ nhiệm.
	rr := want(t, act(tw, sent, "return", "Bổ sung hồ sơ quyết toán"), 200, "", "TW trả lại")
	ev = f.knqgEvents(t)
	if len(ev) != 3 || ev[2].Action != "return" || ev[2].Status != "returned" || !slices.Equal(ev[2].RecipientRefs, []string{chu.ref.String()}) {
		t.Fatalf("trả lại → chủ nhiệm: %+v", ev)
	}
	hs.mu.Lock()
	hs.fail = true
	hs.mu.Unlock()
	sent = want(t, act(chu, rr, "send", ""), 200, "", "gửi lại khi danh bạ lỗi")
	if n := len(f.knqgEvents(t)); n != 3 {
		t.Fatalf("danh bạ lỗi → không thông báo (không mở rộng theo vai): %d", n)
	}
	want(t, act(tw, sent, "confirm", ""), 200, "", "TW xác nhận")
	ev = f.knqgEvents(t)
	if len(ev) != 4 || ev[3].Action != "confirm" || ev[3].Status != "confirmed" || !slices.Equal(ev[3].RecipientRefs, []string{chu.ref.String()}) {
		t.Fatalf("xác nhận → chủ nhiệm: %+v", ev)
	}

	// ----- (2) Ảnh thực địa Mẫu 03 -----
	d3 := newDoc(chu, "03", nil)
	r3 := str(d3, "ref")
	l := want(t, chu.get(base_+r3+"/photos"), 200, "", "danh sách ảnh rỗng")
	if len(list(l, "items")) != 0 || l["can_edit"] != true || str(l, "max_photos") != "20" {
		t.Fatalf("ảnh rỗng: %v", l)
	}
	want(t, chu.addPhoto(r3, "a.gif", []byte("GIF89a...."), ""), 422, "extension.cnts_knqg_photo_invalid", "GIF")
	want(t, chu.addPhoto(r3, "a.png", pdf, ""), 422, "extension.cnts_knqg_photo_invalid", "đuôi PNG nhưng nội dung PDF")
	want(t, chu.addPhoto(r3, "a.pdf", pdf, ""), 422, "extension.cnts_knqg_photo_invalid", "PDF không phải ảnh")
	want(t, chu.addPhoto(r3, "a.png", append(png(0), make([]byte, 5<<20)...), ""), 422, "extension.cnts_knqg_photo_invalid", "quá 5 MB")
	want(t, chu.addPhoto(r3, "a.png", png(0), "Gọi 0912345678"), 422, "extension.cnts_knqg_photo_invalid", "SĐT trong chú thích")
	want(t, chu2.addPhoto(r3, "a.png", png(0), ""), 403, "", "chủ nhiệm dự án khác")
	want(t, tw.addPhoto(r3, "a.png", png(0), ""), 403, "extension.read_only", "TW chỉ xem")
	want(t, chu.addPhoto(str(d1, "ref"), "a.png", png(0), ""), 422, "extension.cnts_knqg_photo_invalid", "Mẫu 01 không có ảnh thực địa")
	// Tải lên, bấm lại sau mất phản hồi (cùng khóa, cùng thân) → phát lại, không thêm ảnh.
	pk := key()
	pb, pct := photoBody("C:\\Anh\\chuong-nuoi.png", png(1), "Chuồng nuôi điểm A")
	a1 := want(t, chu.send(base_+r3+"/photos", pk, pct, pb), 200, "", "tải ảnh 1")
	items := list(a1, "items")
	if len(items) != 1 || str(items[0].(map[string]any), "name") != "chuong-nuoi.png" || str(items[0].(map[string]any), "content_type") != "image/png" ||
		str(items[0].(map[string]any), "caption") != "Chuồng nuôi điểm A" {
		t.Fatalf("ảnh 1: %v", a1)
	}
	if again := want(t, chu.send(base_+r3+"/photos", pk, pct, pb), 200, "", "phát lại tải ảnh"); len(list(again, "items")) != 1 {
		t.Fatalf("phát lại không thêm ảnh: %v", again)
	}
	a2 := want(t, ld.addPhoto(r3, "dan-vit.jpg", jpeg(2), ""), 200, "", "lãnh đạo phòng tải ảnh 2")
	if len(list(a2, "items")) != 2 {
		t.Fatalf("2 ảnh: %v", a2)
	}
	p1 := str(items[0].(map[string]any), "ref")
	code, raw, hdr := tw.req(http.MethodGet, base_+r3+"/photos/"+p1, "", "", nil)
	if code != 200 || !bytes.Equal(raw, png(1)) || hdr.Get("Content-Type") != "image/png" || hdr.Get("X-Content-SHA256") == "" {
		t.Fatalf("TW xem ảnh: %d %q", code, raw)
	}
	if l := want(t, tw.get(base_+r3+"/photos"), 200, "", "TW danh sách"); l["can_edit"] != false || len(list(l, "items")) != 2 {
		t.Fatalf("TW chỉ xem: %v", l)
	}
	want(t, officer.get(base_+r3+"/photos"), 404, "", "Cấp Xã không thấy Mẫu 03")
	if code, _, _ := officer.req(http.MethodGet, base_+r3+"/photos/"+p1, "", "", nil); code != 404 {
		t.Fatalf("Cấp Xã không tải ảnh Mẫu 03: %d", code)
	}
	want(t, chu.get(base_+r3+"/photos/"+uuid.NewString()), 404, "extension.cnts_knqg_photo_not_found", "ảnh lạ")
	// Gỡ ảnh 1: còn 1 ảnh; gỡ lại → không có; ảnh đã gỡ không tải được.
	rm := want(t, chu.post(base_+r3+"/photos/"+p1+"/remove", map[string]any{}), 200, "", "gỡ ảnh 1")
	if len(list(rm, "items")) != 1 {
		t.Fatalf("gỡ: %v", rm)
	}
	want(t, chu.post(base_+r3+"/photos/"+p1+"/remove", map[string]any{}), 404, "extension.cnts_knqg_photo_not_found", "gỡ lại")
	want(t, chu.get(base_+r3+"/photos/"+p1), 404, "extension.cnts_knqg_photo_not_found", "ảnh đã gỡ")
	// Tối đa 20 ảnh hiện hành.
	for i := 3; i <= 21; i++ {
		want(t, chu.addPhoto(r3, fmt.Sprintf("a%d.png", i), png(i), ""), 200, "", fmt.Sprintf("ảnh %d", i))
	}
	want(t, chu.addPhoto(r3, "a22.png", png(22), ""), 422, "extension.cnts_knqg_photo_invalid", "quá 20 ảnh")
	// Trình ký → khóa ảnh.
	d3c := want(t, chu.get(base_+r3), 200, "", "Mẫu 03")
	d3c = upd(chu, d3c, map[string]any{"can_cu": "Kế hoạch kiểm tra năm 2027", "thuc_dia": "Đã triển khai đủ điểm"})
	want(t, act(chu, d3c, "request_sign", ""), 200, "", "trình ký Mẫu 03")
	want(t, chu.addPhoto(r3, "late.png", png(99), ""), 409, "extension.cnts_knqg_locked", "chờ ký không thêm ảnh")
	if l := want(t, chu.get(base_+r3+"/photos"), 200, "", "sau trình ký"); l["can_edit"] != false || len(list(l, "items")) != 20 {
		t.Fatalf("chờ ký: can_edit=false, ảnh giữ nguyên: %v", l)
	}
	// Sổ chỉ ghi thêm.
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM pf_extension.cnts_knqg_photo WHERE tenant_id = $1`, f.tenant).Scan(&n); err != nil || n != 22 {
		t.Fatalf("sổ ảnh: %d %v", n, err)
	}
	if _, err := f.pool.Exec(context.Background(), `UPDATE pf_extension.cnts_knqg_photo SET caption = 'x' WHERE tenant_id = $1`, f.tenant); err == nil {
		t.Fatal("sổ ảnh chỉ ghi thêm: UPDATE phải bị chặn")
	}
}
