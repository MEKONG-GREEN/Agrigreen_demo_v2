package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestSpecsWellFormed(t *testing.T) {
	for _, s := range Specs() {
		if s.Form == "" || s.Code == "" || s.Lifecycle == "" || len(s.Sections) == 0 {
			t.Fatalf("đặc tả %q thiếu thông tin", s.Form)
		}
		seen := map[string]bool{}
		for _, sec := range s.Sections {
			for _, k := range sec.Fields {
				if _, ok := s.FieldByKey(k); !ok || seen["f:"+k] {
					t.Fatalf("%s: ô %q không có hoặc lặp ở mục", s.Form, k)
				}
				seen["f:"+k] = true
			}
			for _, k := range sec.Tables {
				if _, ok := s.TableByKey(k); !ok || seen["t:"+k] {
					t.Fatalf("%s: bảng %q không có hoặc lặp ở mục", s.Form, k)
				}
				seen["t:"+k] = true
			}
		}
		for _, f := range s.Fields {
			if !seen["f:"+f.Key] {
				t.Fatalf("%s: ô %q không thuộc mục nào", s.Form, f.Key)
			}
		}
		for _, tb := range s.Tables {
			if !seen["t:"+tb.Key] || tb.MaxRows <= 0 {
				t.Fatalf("%s: bảng %q không thuộc mục nào hoặc thiếu trần dòng", s.Form, tb.Key)
			}
		}
	}
}

func TestNormalize(t *testing.T) {
	s, _ := Lookup(Form01)
	c := Content{Fields: map[string]string{"danh_gia": "  Tốt  ", "ten_du_an": "giả mạo", "la": "x"},
		Tables: map[string][]Row{"vat_tu": {{"ten": "Gà giống", "ke_hoach": "100", "da_cap": "120.50"}, {}}}}
	es := s.Normalize(&c)
	if len(es) != 1 || es[0].Field != "content.fields.la" {
		t.Fatalf("khóa lạ phải bị từ chối: %v", es)
	}
	if c.Fields["danh_gia"] != "Tốt" || c.Fields["ten_du_an"] != "" || len(c.Tables["vat_tu"]) != 1 || c.Tables["vat_tu"][0]["da_cap"] != "120.5" {
		t.Fatalf("chuẩn hóa sai: %+v", c)
	}
	if w := s.Warnings(c); len(w) != 1 || !strings.Contains(w[0], "vượt kế hoạch") {
		t.Fatalf("quy tắc 11: %v", w)
	}
	// Số âm, ngày sai, quá số dòng.
	bad := Content{Tables: map[string][]Row{"vat_tu": {{"ke_hoach": "-1"}}, "mo_hinh": make([]Row, 101)}}
	if es := s.Normalize(&bad); len(es) != 2 {
		t.Fatalf("số âm + quá 100 dòng: %v", es)
	}
	s4, _ := Lookup(Form04)
	if es := s4.Normalize(&Content{Fields: map[string]string{"ngay": "2027-02-30", "so_hoc_vien": "2.5"}}); len(es) != 2 {
		t.Fatalf("ngày sai + số nguyên: %v", es)
	}
	big := Content{Fields: map[string]string{"danh_gia": strings.Repeat("a", MaxLong+1)}}
	if es := s.Normalize(&big); len(es) != 1 {
		t.Fatalf("quá dài: %v", es)
	}
}

func TestTransitions(t *testing.T) {
	cases := []struct {
		lc, status, act, want string
		ok                    bool
	}{
		{LifecycleApprove, StatusDraft, ActSubmit, StatusSubmitted, true},
		{LifecycleApprove, StatusSubmitted, ActApprove, StatusApproved, true},
		{LifecycleApprove, StatusSubmitted, ActReturn, StatusReturned, true},
		{LifecycleApprove, StatusReturned, ActSubmit, StatusSubmitted, true},
		{LifecycleApprove, StatusApproved, ActAdjust, StatusDraft, true},
		{LifecycleApprove, StatusApproved, ActSubmit, "", false},
		{LifecycleSign, StatusDraft, ActRequestSign, StatusAwaitingSign, true},
		{LifecycleSign, StatusAwaitingSign, ActSign, StatusSigned, true},
		{LifecycleSign, StatusAwaitingSign, ActUnrequest, StatusDraft, true},
		{LifecycleSign, StatusSigned, ActAdjust, StatusDraft, true},
		{LifecycleSign, StatusDraft, ActSign, "", false},
		{LifecycleNational, StatusDraft, ActSend, StatusSent, true},
		{LifecycleNational, StatusSent, ActConfirm, StatusConfirmed, true},
		{LifecycleNational, StatusSent, ActReturn, StatusReturned, true},
		{LifecycleNational, StatusReturned, ActSend, StatusSent, true},
		{LifecycleNational, StatusConfirmed, ActAdjust, "", false}, // BR-02: đã xác nhận khóa sửa
		{LifecycleNational, StatusConfirmed, ActReturn, "", false},
	}
	for _, c := range cases {
		st, ok := Transition(c.lc, c.status, c.act)
		if ok != c.ok || st.Status != c.want {
			t.Fatalf("%s %s %s: %+v %v", c.lc, c.status, c.act, st, ok)
		}
	}
	if st, _ := Transition(LifecycleApprove, StatusSubmitted, ActReturn); !st.Reason {
		t.Fatal("trả lại Mẫu 01 bắt buộc ý kiến")
	}
	if st, _ := Transition(LifecycleSign, StatusSigned, ActAdjust); !st.NewRev || !st.Reason {
		t.Fatal("bản điều chỉnh: phiên bản mới, có lý do")
	}
}

func TestPrefillAndComplete05(t *testing.T) {
	s, _ := Lookup(Form05)
	out, profit := decimal.RequireFromString("1250.5"), decimal.RequireFromString("3200000")
	from, to := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC), time.Date(2027, 6, 30, 0, 0, 0, 0, time.UTC)
	f := Facts{ProjectName: "Gà an toàn sinh học", Basis: "HĐ 12/HĐ-KNQG", Site: &SiteFacts{Name: "Điểm A", Commune: "Xã X", Scale: "1500 con", Households: 3},
		Diary:      &DiaryFacts{Households: 2, Heads: 1400, OutputKg: &out, Profit: &profit, From: &from, To: &to, Verified: 5},
		Households: []Row{{"chu_ho": "N*** A", "quy_mo": "500 con"}}}
	c := s.Prefill(f)
	if c.Fields["ten_mo_hinh"] != "Gà an toàn sinh học" || c.Fields["dia_diem"] != "Điểm A, Xã X" || len(c.Tables["phu_luc_1"]) != 1 {
		t.Fatalf("điền sẵn: %+v", c.Fields)
	}
	row := func(c Content, ma string) Row {
		for _, r := range c.Tables["chi_tieu"] {
			if r["ma"] == ma {
				return r
			}
		}
		return nil
	}
	if r := row(c, "so_ho"); r["yeu_cau"] != "3" || r["thuc_te_nhat_ky"] != "2" || r["thuc_te"] != "2" {
		t.Fatalf("số hộ: %v", r)
	}
	if r := row(c, "nang_suat"); r["thuc_te_nhat_ky"] != "1250.5 kg" {
		t.Fatalf("năng suất từ nhật ký: %v", r)
	}
	// Máy khách sửa thực tế khác nhật ký mà không ghi chú → chặn khi trình ký (quy tắc 9); cột tự điền gửi lên bị bỏ.
	edited := c.Clone()
	row(edited, "so_ho")["thuc_te"] = "3"
	row(edited, "so_ho")["thuc_te_nhat_ky"] = "3"
	if es := s.Normalize(&edited); len(es) != 0 {
		t.Fatal(es)
	}
	s.Fill(&edited, f)
	if row(edited, "so_ho")["thuc_te_nhat_ky"] != "2" {
		t.Fatal("cột tự điền phải do máy chủ đặt lại")
	}
	edited.Fields["danh_gia"], edited.Fields["don_vi_thuc_hien"] = "Đạt yêu cầu", "Trung tâm Khuyến nông"
	es := s.Complete(edited)
	if len(es) != 1 || !strings.Contains(es[0].Reason, "khác số từ nhật ký") {
		t.Fatalf("quy tắc 9: %v", es)
	}
	row(edited, "so_ho")["ghi_chu"] = "Một hộ nhận bổ sung giống"
	row(edited, "nang_suat")["dat"] = "khong"
	if es := s.Complete(edited); len(es) != 1 || !strings.Contains(es[0].Reason, "không đạt") {
		t.Fatalf("không đạt phải ghi chú: %v", es)
	}
	// Mẫu 07 lấy dòng điểm từ Mẫu 05 đã ký.
	sum := Summary05(c)
	if sum["dia_diem"] != "Điểm A, Xã X" || sum["so_ho"] != "2" || !strings.Contains(sum["ket_qua"], "1250.5 kg") {
		t.Fatalf("tóm tắt Mẫu 05: %v", sum)
	}
	s7, _ := Lookup(Form07)
	c7 := s7.Prefill(Facts{ProjectName: "X", Signed05: []Row{sum}})
	if len(c7.Tables["diem"]) != 1 {
		t.Fatal("Mẫu 07: bảng điểm tự dựng")
	}
	if es := s7.Normalize(&Content{Tables: map[string][]Row{"diem": {{"dia_diem": "giả"}}}}); len(es) != 0 {
		t.Fatal("bảng tự dựng gửi lên bị bỏ, không lỗi")
	}
}

func TestPeriodAndScan(t *testing.T) {
	s1, _ := Lookup(Form01)
	if _, bad := s1.NormalizePeriod("2027-13"); bad == "" {
		t.Fatal("tháng 13")
	}
	if p, bad := s1.NormalizePeriod("2027-10"); bad != "" || PeriodLabel(p) != "tháng 10/2027" {
		t.Fatal(p, bad)
	}
	s3, _ := Lookup(Form03)
	if _, bad := s3.NormalizePeriod("2027"); bad == "" {
		t.Fatal("Mẫu 03 không theo kỳ")
	}
	if ct, name, ok := ScanType(`C:\a\bien ban.PDF`, []byte("%PDF-1.7 ...")); !ok || ct != "application/pdf" || name != "bien ban.PDF" {
		t.Fatal(ct, name, ok)
	}
	if _, _, ok := ScanType("x.pdf", []byte("MZ")); ok {
		t.Fatal("chữ ký tệp sai phải bị chặn")
	}
	if _, _, ok := ScanType("x.exe", []byte("%PDF-")); ok {
		t.Fatal("đuôi lạ")
	}
}
