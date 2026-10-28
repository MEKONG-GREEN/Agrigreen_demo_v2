package domain

import (
	"strings"
	"testing"
	"time"
)

func day(s string) *time.Time {
	t, _ := time.Parse(time.DateOnly, s)
	return &t
}

func TestValidity(t *testing.T) {
	today := *day("2027-10-13")
	for _, c := range []struct {
		from, to *time.Time
		want     string
	}{
		{nil, nil, ValidityActive},
		{day("2027-01-01"), day("2027-10-13"), ValidityActive},
		{day("2027-01-01"), day("2027-10-12"), ValidityExpired},
		{day("2027-11-01"), nil, ValidityPending},
	} {
		if got := Validity(c.from, c.to, today); got != c.want {
			t.Errorf("Validity(%v, %v) = %s, muốn %s", c.from, c.to, got, c.want)
		}
	}
}

func TestDocMetaValidate(t *testing.T) {
	m := DocMeta{Title: "  Quy   trình  ", Kind: KindQuyTrinh, Audience: []string{"xa", "xa", "nong_ho"}}
	if es := m.Validate(); len(es) != 0 || m.Title != "Quy trình" || strings.Join(m.Audience, ",") != "nong_ho,xa" {
		t.Fatalf("chuẩn hóa: %v %+v", es, m)
	}
	bad := DocMeta{Title: "", Kind: "x", Audience: []string{"tw"}, EffectiveFrom: day("2027-02-01"), EffectiveTo: day("2027-01-01")}
	fields := map[string]bool{}
	for _, e := range bad.Validate() {
		fields[e.Field] = true
	}
	for _, f := range []string{"title", "kind", "audience", "effective_to"} {
		if !fields[f] {
			t.Errorf("thiếu lỗi trường %s: %v", f, fields)
		}
	}
}

func TestFileType(t *testing.T) {
	if ct, name, ok := FileType(`C:\tmp\a"b.pdf`, []byte("%PDF-1.7")); !ok || ct != "application/pdf" || name != "ab.pdf" {
		t.Fatalf("pdf: %s %s %v", ct, name, ok)
	}
	if _, _, ok := FileType("a.docx", []byte("%PDF")); ok {
		t.Fatal("docx với nội dung pdf phải bị chặn")
	}
	if _, _, ok := FileType("a.exe", []byte("MZ")); ok {
		t.Fatal("exe phải bị chặn")
	}
	long := strings.Repeat("a", 300) + ".pdf"
	if _, name, ok := FileType(long, []byte("%PDF-")); !ok || len([]rune(name)) != MaxFileName || !strings.HasSuffix(name, ".pdf") {
		t.Fatalf("cắt tên dài: %d %v", len([]rune(name)), ok)
	}
}

func TestProfileValidate(t *testing.T) {
	p := Profile{Intro: " ", Duties: ""}
	if len(p.Validate()) == 0 {
		t.Fatal("giới thiệu rỗng phải lỗi")
	}
	p = Profile{Intro: "Giới thiệu", Duties: "1.\n2."}
	if es := p.Validate(); len(es) != 0 {
		t.Fatal(es)
	}
}
