package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

const sample = "# T\n" +
	"```sql\n" + // dòng 2
	"SELECT 1;\n" +
	"```\n" +
	"text\n" +
	"```go\n" +
	"x := 1\n" +
	"```\n" +
	"````markdown\n" +
	"```sql\n" + // trong khối khác: không tính
	"SELECT 99;\n" +
	"```\n" +
	"````\n" +
	"  ```SQL  extra info\n" + // dòng 14, thụt 2, chữ hoa
	"CREATE TABLE t (a int);\n" +
	"\n" +
	"SELECT 2;\n" +
	"~~~~\n" + // khác ký tự: không đóng
	"```\n" +
	"~~~sql\n" + // dòng 20
	"SELECT 3;\n" // chưa đóng: kéo tới hết tệp

func TestExtract(t *testing.T) {
	bs := Extract(sample)
	if len(bs) != 3 {
		t.Fatalf("muốn 3 khối, có %d: %+v", len(bs), bs)
	}
	want := []struct {
		line int
		sql  string
	}{
		{3, "SELECT 1;\n"},
		{15, "CREATE TABLE t (a int);\n\nSELECT 2;\n~~~~\n"},
		{21, "SELECT 3;\n"},
	}
	for i, w := range want {
		if bs[i].Index != i+1 || bs[i].Line != w.line || bs[i].SQL != w.sql {
			t.Errorf("khối %d = %+v, muốn dòng %d sql %q", i+1, bs[i], w.line, w.sql)
		}
	}
}

func TestExtractCRLFAndNoBlocks(t *testing.T) {
	bs := Extract("```sql\r\nSELECT 1;\r\n```\r\n")
	if len(bs) != 1 || bs[0].SQL != "SELECT 1;\n" {
		t.Fatalf("CRLF: %+v", bs)
	}
	if bs := Extract("không có mã\n``not fence``\n"); len(bs) != 0 {
		t.Fatalf("muốn 0 khối: %+v", bs)
	}
}

func TestParseSkipsValidation(t *testing.T) {
	cases := map[string]string{
		"thiếu doc":    "skips:\n  - {block: 1, hash: 0123456789ab, reason: r}\n",
		"hash ngắn":    "skips:\n  - {doc: a.md, block: 1, hash: 0123, reason: r}\n",
		"thiếu reason": "skips:\n  - {doc: a.md, block: 1, hash: 0123456789ab, reason: ' '}\n",
	}
	for name, y := range cases {
		if _, err := ParseSkips([]byte(y)); err == nil {
			t.Errorf("%s: muốn lỗi", name)
		}
	}
	s, err := ParseSkips([]byte("skips: []\n"))
	if err != nil || len(s) != 0 {
		t.Fatalf("rỗng: %v %v", s, err)
	}
}

func TestPlan(t *testing.T) {
	bs := Extract(sample)
	h2 := bs[1].Hash()
	skips := []SkipEntry{
		{Doc: "a.md", Block: 2, Hash: h2, Reason: "minh họa"},
		{Doc: "a.md", Block: 3, Hash: strings.ToUpper(bs[2].Hash()[:8]), Reason: "tiền tố hoa"},
		{Doc: "a.md", Block: 1, Hash: "ffffffffffff", Reason: "thừa"},
		{Doc: "khac.md", Block: 1, Hash: "ffffffffffff", Reason: "tài liệu khác: bỏ qua"},
		{Doc: "a.md", Block: 9, Hash: bs[0].Hash(), Reason: "sai số"},
	}
	reasons, problems := Plan("docs/x/a.md", bs, skips)
	if reasons[2] != "minh họa" || reasons[3] != "tiền tố hoa" || reasons[1] != "sai số" {
		t.Errorf("reasons = %v", reasons)
	}
	if len(problems) != 2 || !strings.Contains(problems[0], "mục thừa") || !strings.Contains(problems[1], "không phải #9") {
		t.Errorf("problems = %q", problems)
	}
}

func TestReportExitCode(t *testing.T) {
	b := Block{Index: 1, Line: 3, SQL: "x"}
	var buf bytes.Buffer
	if c := report(&buf, []Result{{Block: b, Status: Pass}, {Block: b, Status: Skip, Detail: "r"}}, nil); c != 0 {
		t.Errorf("pass+skip: mã %d", c)
	}
	if c := report(&buf, []Result{{Block: b, Status: Fail, Detail: "ERROR"}}, nil); c != 1 {
		t.Errorf("fail: mã %d", c)
	}
	if c := report(&buf, nil, []string{"mục thừa"}); c != 1 {
		t.Errorf("mục thừa: mã %d", c)
	}
}

func adminURL(t *testing.T) string {
	u := os.Getenv("AG_TEST_PG_ADMIN_URL")
	if u == "" {
		if os.Getenv("CI") == "true" {
			t.Fatal("docsql: CI bắt buộc AG_TEST_PG_ADMIN_URL")
		}
		t.Skip("docsql: chưa đặt AG_TEST_PG_ADMIN_URL — bỏ qua kiểm thử tích hợp")
	}
	return u
}

// Khối lỗi được báo kèm dòng tài liệu, khối sau vẫn chạy, vai tạo trong lượt không còn.
func TestExecuteReportsFailures(t *testing.T) {
	admin := adminURL(t)
	md := "```sql\nCREATE ROLE ag_docsql_probe NOLOGIN;\nCREATE TABLE k_ref.t (a int);\n```\n" +
		"```sql\nSELECT 1;\nSELEC 2;\n```\n" +
		"```sql\nINSERT INTO k_ref.t VALUES (1);\n```\n" +
		"```sql\nnot run\n```\n"
	bs := Extract(md)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	res, err := Execute(ctx, admin, defaultPreamble, bs, map[int]string{4: "minh họa"})
	if err != nil {
		t.Fatal(err)
	}
	got := []Status{res[0].Status, res[1].Status, res[2].Status, res[3].Status}
	want := []Status{Pass, Fail, Pass, Skip}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("trạng thái = %v, muốn %v (%+v)", got, want, res)
		}
	}
	if !strings.Contains(res[1].Detail, "42601") || res[1].ErrAt != bs[1].Line+1 {
		t.Errorf("lỗi khối 2: %q tại dòng %d, muốn 42601 tại %d", res[1].Detail, res[1].ErrAt, bs[1].Line+1)
	}
	// Lượt sau tạo lại cùng vai: được, vì lượt trước đã ROLLBACK.
	res, err = Execute(ctx, admin, defaultPreamble, bs[:1], nil)
	if err != nil || res[0].Status != Pass {
		t.Fatalf("lượt hai: %v %+v", err, res)
	}
}

// R-OPS-04: mọi khối sql của bản v4.1 chạy được (trừ mục trong skip.yaml).
func TestArchitectureDocV41(t *testing.T) {
	admin := adminURL(t)
	var out, errb bytes.Buffer
	code := run([]string{"-admin", admin, "-skip", "skip.yaml",
		"../../docs/kien-truc/AgriGreen-Thiet-ke-kien-truc-he-thong-v4_1.md"}, &out, &errb)
	t.Log(out.String())
	if code != 0 {
		t.Fatalf("docsql mã %d\n%s%s", code, out.String(), errb.String())
	}
}
