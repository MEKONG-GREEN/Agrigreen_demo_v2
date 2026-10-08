package ingest

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/integration"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

func rollContract(t *testing.T) *integration.Contract {
	t.Helper()
	c, err := integration.Get("roll_count_statement", 1)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

const (
	refA = "0199b8af-1000-7a01-8b02-c03d04e05f06"
	refB = "0199b8af-2000-7a01-8b02-c03d04e05f06"
	htx  = "01989e00-1111-7222-8333-944455566677"
	plot = "01989f00-aa11-7b22-8c33-d44e55f66a77"
)

func ndjsonLine(ref string, rolls int) string {
	b, _ := json.Marshal(map[string]any{"statement_ref": ref, "schedule_ref": nil, "htx_ref": htx, "plot_ref": plot, "season_ref": nil,
		"collected_on": "2026-10-09", "rolls": rolls, "revision": 1})
	return string(b)
}

func TestFormatOf(t *testing.T) {
	for ct, want := range map[string]string{"application/x-ndjson": FormatNDJSON, "application/jsonl; charset=utf-8": FormatNDJSON,
		"text/csv": FormatCSV, "text/csv; charset=UTF-8": FormatCSV} {
		if got, err := FormatOf(ct); err != nil || got != want {
			t.Errorf("%s → %s %v", ct, got, err)
		}
	}
	for _, ct := range []string{"", "application/json", "text/csv; charset=windows-1258", "multipart/form-data; boundary=x"} {
		if _, err := FormatOf(ct); code(err) != ErrFileFormat.Code {
			t.Errorf("%q phải bị từ chối 415: %v", ct, err)
		}
	}
}

// NDJSON: BOM, CRLF, dòng trống cuối được bỏ; dòng trống giữa / dòng không phải object → cả tệp 400; vượt số bản
// ghi → 413.
func TestParseNDJSON(t *testing.T) {
	c := rollContract(t)
	raw := "\xEF\xBB\xBF" + ndjsonLine(refA, 46) + "\r\n" + ndjsonLine(refB, 12) + "\r\n\r\n"
	recs, err := ParseFile(c, FormatNDJSON, []byte(raw), 0)
	if err != nil || len(recs) != 2 {
		t.Fatalf("2 bản ghi: %d %v", len(recs), err)
	}
	if err := c.ValidateRecord(recs[0]); err != nil {
		t.Fatalf("bản ghi hợp lệ theo hợp đồng: %v", err)
	}
	for name, bad := range map[string]string{
		"dòng trống giữa":   ndjsonLine(refA, 1) + "\n\n" + ndjsonLine(refB, 1),
		"mảng":              "[1,2]",
		"JSON hỏng":         `{"statement_ref":`,
		"không có bản ghi":  "\n\n",
		"không phải UTF-8":  "{\"a\":\"\xff\"}",
		"chuỗi trần (JSON)": `"x"`,
	} {
		if _, err := ParseFile(c, FormatNDJSON, []byte(bad), 0); code(err) != ErrFileInvalid.Code {
			t.Errorf("%s → 400 integration.file_invalid, nhận %v", name, err)
		}
	}
	if _, err := ParseFile(c, FormatNDJSON, []byte(raw), 1); code(err) != ErrFileTooLarge.Code {
		t.Fatalf("vượt số bản ghi → 413: %v", err)
	}
}

// CSV: ô đổi sang kiểu JSON theo lược đồ (integer, null cho trường nullable rỗng, bỏ trường không nullable rỗng);
// ô sai dạng giữ nguyên chuỗi để ValidateRecord (đường kiểm duy nhất) từ chối bản ghi — tệp vẫn tách được.
func TestParseCSV(t *testing.T) {
	c := rollContract(t)
	raw := "statement_ref,schedule_ref,htx_ref,plot_ref,season_ref,collected_on,rolls,revision\r\n" +
		refA + ",," + htx + "," + plot + ",,2026-10-09,46,1\r\n" +
		refB + ",," + htx + "," + plot + ",,2026-10-09,abc,1\r\n" +
		refB + ",," + htx + "," + plot + ",,,7,2\r\n"
	recs, err := ParseFile(c, FormatCSV, []byte(raw), 0)
	if err != nil || len(recs) != 3 {
		t.Fatalf("3 bản ghi: %d %v", len(recs), err)
	}
	if err := c.ValidateRecord(recs[0]); err != nil {
		t.Fatalf("dòng 1 hợp lệ: %v (%s)", err, recs[0])
	}
	if !strings.Contains(string(recs[0]), `"rolls":46`) || !strings.Contains(string(recs[0]), `"schedule_ref":null`) {
		t.Fatalf("integer + null: %s", recs[0])
	}
	if err := c.ValidateRecord(recs[1]); err == nil || !strings.Contains(string(recs[1]), `"rolls":"abc"`) {
		t.Fatalf("ô sai kiểu giữ chuỗi → hợp đồng từ chối: %v %s", err, recs[1])
	}
	if err := c.ValidateRecord(recs[2]); err == nil || strings.Contains(string(recs[2]), "collected_on") {
		t.Fatalf("ô rỗng không nullable → bỏ trường → thiếu trường bắt buộc: %v %s", err, recs[2])
	}
	// Dấu ';' (Excel vùng VN).
	semi := strings.ReplaceAll(raw, ",", ";")
	if recs2, err := ParseFile(c, FormatCSV, []byte(semi), 0); err != nil || !bytes.Equal(recs2[0], recs[0]) {
		t.Fatalf("CSV ';' cùng kết quả: %v", err)
	}
	for name, bad := range map[string]string{
		"tiêu đề trùng":        "rolls,rolls\n1,2\n",
		"tiêu đề lạ dạng":      "Rolls Count\n1\n",
		"lệch số cột":          "rolls,revision\n1\n",
		"chỉ có tiêu đề":       "rolls,revision\n",
		"ngoặc kép không đóng": "rolls\n\"1\n",
	} {
		if _, err := ParseFile(c, FormatCSV, []byte(bad), 0); code(err) != ErrFileInvalid.Code {
			t.Errorf("%s → 400, nhận %v", name, err)
		}
	}
}

// Phong bì từ tệp: chia theo cỡ lô, tất định (tải lại cùng tệp → cùng thân → Submit coi là gọi lặp), batch_ref khác
// nhau theo (tệp, k), thân qua ParseEnvelope của W5.4 với Idempotency-Key = batch_ref.
func TestFileEnvelopes(t *testing.T) {
	c := rollContract(t)
	var lines []string
	for i := 0; i < 5; i++ {
		lines = append(lines, ndjsonLine(ids.New().String(), i))
	}
	lines[4] = `{"rolls":1}` // thiếu entity_key → "" (lô từ chối entity_key_mismatch)
	recs, err := ParseFile(c, FormatNDJSON, []byte(strings.Join(lines, "\n")), 0)
	if err != nil {
		t.Fatal(err)
	}
	fid := ids.New()
	at := time.Date(2026, 10, 9, 11, 5, 0, 123456000, time.UTC)
	a, err := FileEnvelopes(c, fid, recs, 2, at)
	if err != nil || len(a) != 3 || BatchCount(5, 2) != 3 {
		t.Fatalf("5 bản ghi / cỡ 2 → 3 lô: %d %v", len(a), err)
	}
	b, _ := FileEnvelopes(c, fid, recs, 2, at)
	seen := map[string]bool{}
	for k := range a {
		if !bytes.Equal(a[k].Body, b[k].Body) || a[k].BatchRef != FileBatchRef(fid, k) {
			t.Fatalf("lô %d không tất định", k)
		}
		if seen[a[k].BatchRef.String()] {
			t.Fatal("batch_ref trùng")
		}
		seen[a[k].BatchRef.String()] = true
		env, err := ParseEnvelope(a[k].Body, c.Name, a[k].BatchRef.String())
		if err != nil {
			t.Fatalf("lô %d qua phong bì ingest_batch.v1: %v", k, err)
		}
		if k == 0 && (len(env.Items) != 2 || env.Items[0].EntityKey == "" || !env.SentAt.Equal(at)) {
			t.Fatalf("lô 0: %+v", env)
		}
		if k == 2 && env.Items[0].EntityKey != "" {
			t.Fatalf("bản ghi thiếu khóa → entity_key rỗng: %+v", env.Items[0])
		}
	}
	if FileBatchRef(ids.New(), 0) == FileBatchRef(fid, 0) {
		t.Fatal("batch_ref phụ thuộc mã tệp")
	}
	// Tổng kiểm tệp là khóa idempotent: cùng byte → cùng băm, khác một byte → khác.
	raw := []byte(strings.Join(lines, "\n"))
	if Hash(raw) != Hash(append([]byte(nil), raw...)) || Hash(raw) == Hash(append(raw, '\n')) {
		t.Fatal("tổng kiểm tệp")
	}
}

func TestFileStatusAndResults(t *testing.T) {
	cases := []struct {
		st   []string
		want string
	}{
		{[]string{StatusCompleted, StatusCompleted}, StatusCompleted},
		{[]string{StatusCompleted, StatusQueued}, StatusQueued},
		{[]string{StatusDeadLettered, StatusCompleted}, StatusDeadLettered},
		{[]string{StatusDeadLettered, StatusQueued}, StatusQueued},
		{[]string{StatusQueued, FileIncomplete}, FileIncomplete},
	}
	for _, tc := range cases {
		var bs []FileBatch
		for _, s := range tc.st {
			bs = append(bs, FileBatch{Status: s})
		}
		if got := fileStatus(bs); got != tc.want {
			t.Errorf("%v → %s, muốn %s", tc.st, got, tc.want)
		}
	}
	rows := []FileResult{{Record: 1, EntityKey: "=HYPERLINK(1)", Outcome: OutcomeAccepted},
		{Record: 2, EntityKey: "k2", Outcome: OutcomeRejected, ProblemCode: "integration.record_invalid", Detail: "rolls: kiểu string"}}
	var buf bytes.Buffer
	if err := WriteResults(&buf, FormatCSV, rows); err != nil {
		t.Fatal(err)
	}
	want := "record,entity_key,outcome,problem_code,detail\n1,'=HYPERLINK(1),accepted,,\n2,k2,rejected,integration.record_invalid,rolls: kiểu string\n"
	if buf.String() != want {
		t.Fatalf("CSV kết quả (chặn chèn công thức):\n%s", buf.String())
	}
	buf.Reset()
	_ = WriteResults(&buf, FormatNDJSON, rows)
	if strings.Count(buf.String(), "\n") != 2 || !strings.Contains(buf.String(), `"record":2`) {
		t.Fatalf("NDJSON kết quả: %s", buf.String())
	}
}

func TestFileChannel(t *testing.T) {
	c := rollContract(t)
	if !FileChannel(c) {
		t.Fatal("roll_count_statement mở kênh tệp")
	}
	cp := *c
	ing := *c.Ingest
	ing.Channels = []string{"api"}
	cp.Ingest = &ing
	if FileChannel(&cp) || FileChannel(nil) {
		t.Fatal("không khai file → mặc định từ chối")
	}
}
