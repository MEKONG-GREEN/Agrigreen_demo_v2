package ingest

// Kênh tệp cùng lược đồ (W5.8; R-INT-11, ADR-0033 §3): đối tác không có đội IT tải MỘT tệp NDJSON hoặc CSV theo
// lược đồ bản ghi của hợp đồng nạp. Phần này CHỈ đổi tệp thành phong bì ingest_batch.v1 (chia theo max_batch của
// hợp đồng); mọi kiểm nội dung bản ghi (khóa lạ, kiểu, bắt buộc, entity_key, khóa nghiệp vụ) do đường ống nạp
// theo lô của W5.4 làm — một đường kiểm duy nhất. Tệp hỏng cú pháp (dòng không phải JSON object, CSV lệch số
// cột, tiêu đề trùng) bị từ chối cả tệp vì không tách được bản ghi.

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/integration"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/publication"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Định dạng tệp.
const (
	FormatNDJSON = "ndjson"
	FormatCSV    = "csv"
)

// ChannelFile là kênh "file" của ingest.channels trong hợp đồng (R-INT-11). Hợp đồng không khai → mặc định từ chối.
const ChannelFile = "file"

// Giới hạn mặc định của kênh tệp (ghi đè bằng AG_GATEWAY_INGEST_MAX_FILE_BYTES / _MAX_FILE_RECORDS).
const (
	DefaultMaxFileBytes   = 8 << 20
	DefaultMaxFileRecords = 10000
	// MaxFileBytesLimit, MaxFileRecordsLimit: trần cứng (khớp CHECK của pf_apiclient.ingest_file).
	MaxFileBytesLimit   = 100 << 20
	MaxFileRecordsLimit = 100000
)

// Mã lỗi kênh tệp (docs/errors.md mục "Cổng tích hợp — kênh tệp").
var (
	ErrFileChannelNotAllowed = problem.New(http.StatusForbidden, "integration.file_channel_not_allowed", "Hợp đồng nạp không mở kênh tệp")
	ErrFileFormat            = problem.New(http.StatusUnsupportedMediaType, "integration.file_format_unsupported", "Định dạng tệp không hỗ trợ — dùng application/x-ndjson hoặc text/csv")
	ErrFileInvalid           = problem.New(http.StatusBadRequest, "integration.file_invalid", "Tệp không tách được thành bản ghi")
	ErrFileTooLarge          = problem.New(http.StatusRequestEntityTooLarge, "integration.file_too_large", "Tệp vượt giới hạn kích thước hoặc số bản ghi")
	ErrFileNotFound          = problem.New(http.StatusNotFound, "integration.file_not_found", "Không tìm thấy tệp nạp")
	ErrFileResultsPending    = problem.New(http.StatusConflict, "integration.file_results_pending", "Tệp chưa xử lý xong — tải kết quả sau")
)

// FileChannel: hợp đồng mở kênh tệp (ingest.channels có "file").
func FileChannel(c *integration.Contract) bool {
	if c == nil || c.Ingest == nil {
		return false
	}
	for _, ch := range c.Ingest.Channels {
		if ch == ChannelFile {
			return true
		}
	}
	return false
}

// FormatOf chọn định dạng theo Content-Type (application/x-ndjson, application/jsonl, application/json-seq không
// nhận; text/csv). Tham số charset khác utf-8 → ErrFileFormat.
func FormatOf(contentType string) (string, error) {
	mt, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", ErrFileFormat
	}
	if cs, ok := params["charset"]; ok && !strings.EqualFold(cs, "utf-8") {
		return "", ErrFileFormat.WithDetail("chỉ nhận UTF-8")
	}
	switch strings.ToLower(mt) {
	case "application/x-ndjson", "application/ndjson", "application/jsonl", "application/x-jsonlines":
		return FormatNDJSON, nil
	case "text/csv":
		return FormatCSV, nil
	}
	return "", ErrFileFormat
}

// ContentType trả Content-Type của định dạng (tệp kết quả).
func ContentType(format string) string {
	if format == FormatCSV {
		return "text/csv; charset=utf-8"
	}
	return "application/x-ndjson; charset=utf-8"
}

var bom = []byte{0xEF, 0xBB, 0xBF}

// ParseFile tách tệp thành các bản ghi JSON object (thứ tự trong tệp). Không kiểm nội dung bản ghi theo hợp đồng
// (việc của đường ống lô); CSV đổi ô sang kiểu JSON theo lược đồ bản ghi (chỉ đổi khi ô đúng dạng kiểu đó —
// ô sai dạng giữ là chuỗi để ValidateRecord từ chối bản ghi). maxRecords ≤ 0 → DefaultMaxFileRecords.
func ParseFile(c *integration.Contract, format string, raw []byte, maxRecords int) ([]json.RawMessage, error) {
	if maxRecords <= 0 {
		maxRecords = DefaultMaxFileRecords
	}
	raw = bytes.TrimPrefix(raw, bom)
	if !utf8.Valid(raw) {
		return nil, ErrFileInvalid.WithDetail("tệp phải là UTF-8")
	}
	var (
		recs []json.RawMessage
		err  error
	)
	switch format {
	case FormatNDJSON:
		recs, err = parseNDJSON(raw, maxRecords)
	case FormatCSV:
		recs, err = parseCSV(c, raw, maxRecords)
	default:
		return nil, ErrFileFormat
	}
	if err != nil {
		return nil, err
	}
	if len(recs) == 0 {
		return nil, ErrFileInvalid.WithDetail("tệp không có bản ghi")
	}
	return recs, nil
}

func tooMany(limit int) error {
	return ErrFileTooLarge.WithDetail(fmt.Sprintf("tối đa %d bản ghi mỗi tệp", limit))
}

func parseNDJSON(raw []byte, maxRecords int) ([]json.RawMessage, error) {
	lines := bytes.Split(raw, []byte("\n"))
	// Một dòng trống cuối tệp (xuống dòng kết thúc) được bỏ qua; dòng trống giữa tệp → tệp hỏng.
	for len(lines) > 0 && len(bytes.TrimSpace(lines[len(lines)-1])) == 0 {
		lines = lines[:len(lines)-1]
	}
	out := make([]json.RawMessage, 0, len(lines))
	for i, ln := range lines {
		ln = bytes.TrimSpace(ln)
		if len(ln) == 0 {
			return nil, ErrFileInvalid.WithDetail(fmt.Sprintf("dòng %d trống", i+1))
		}
		if ln[0] != '{' || !json.Valid(ln) {
			return nil, ErrFileInvalid.WithDetail(fmt.Sprintf("dòng %d không phải một JSON object", i+1))
		}
		if len(out) == maxRecords {
			return nil, tooMany(maxRecords)
		}
		var buf bytes.Buffer
		if err := json.Compact(&buf, ln); err != nil {
			return nil, ErrFileInvalid.WithDetail(fmt.Sprintf("dòng %d không phải một JSON object", i+1))
		}
		out = append(out, buf.Bytes())
	}
	return out, nil
}

var headerRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func parseCSV(c *integration.Contract, raw []byte, maxRecords int) ([]json.RawMessage, error) {
	r := csv.NewReader(bytes.NewReader(raw))
	r.Comma = delimiter(raw)
	r.ReuseRecord = false
	header, err := r.Read()
	if err != nil {
		return nil, ErrFileInvalid.WithDetail("thiếu dòng tiêu đề CSV")
	}
	seen := map[string]bool{}
	for i, h := range header {
		h = strings.TrimSpace(h)
		if !headerRe.MatchString(h) || seen[h] {
			return nil, ErrFileInvalid.WithDetail(fmt.Sprintf("tiêu đề cột %d không hợp lệ hoặc trùng", i+1))
		}
		seen[h], header[i] = true, h
	}
	var schema *publication.Node
	if c != nil {
		schema = c.Record()
	}
	var out []json.RawMessage
	for {
		row, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			line := 0
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				line = pe.Line
			}
			return nil, ErrFileInvalid.WithDetail(fmt.Sprintf("CSV hỏng ở dòng %d (số cột phải bằng tiêu đề, ngoặc kép phải đóng)", line))
		}
		if len(out) == maxRecords {
			return nil, tooMany(maxRecords)
		}
		rec := make(map[string]json.RawMessage, len(row))
		for i, cell := range row {
			if v, ok := cellValue(field(schema, header[i]), cell); ok {
				rec[header[i]] = v
			}
		}
		b, err := json.Marshal(rec) // khóa theo thứ tự chữ cái → tất định
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

// delimiter: ',' mặc định; ';' khi dòng tiêu đề không có ',' nhưng có ';' (Excel theo vùng Việt Nam).
func delimiter(raw []byte) rune {
	first := raw
	if i := bytes.IndexByte(raw, '\n'); i >= 0 {
		first = raw[:i]
	}
	if !bytes.Contains(first, []byte(",")) && bytes.Contains(first, []byte(";")) {
		return ';'
	}
	return ','
}

func field(schema *publication.Node, name string) *publication.Node {
	if schema == nil {
		return nil
	}
	return schema.Properties[name]
}

var numberRe = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

// cellValue đổi một ô CSV sang giá trị JSON theo kiểu của trường. Ô rỗng: trường cho phép null → null, ngược lại
// bỏ trường (thiếu trường bắt buộc do ValidateRecord báo). Không đổi được → chuỗi nguyên văn (để kiểm từ chối).
func cellValue(n *publication.Node, cell string) (json.RawMessage, bool) {
	if cell == "" {
		if n != nil && n.Type.Has("null") {
			return json.RawMessage("null"), true
		}
		return nil, false
	}
	if n != nil {
		t := strings.TrimSpace(cell)
		switch {
		case (n.Type.Has("integer") || n.Type.Has("number")) && numberRe.MatchString(t):
			return json.RawMessage(t), true
		case n.Type.Has("boolean") && (t == "true" || t == "false"):
			return json.RawMessage(t), true
		case (n.Type.Has("object") && strings.HasPrefix(t, "{") || n.Type.Has("array") && strings.HasPrefix(t, "[")) && json.Valid([]byte(t)):
			var buf bytes.Buffer
			_ = json.Compact(&buf, []byte(t))
			return buf.Bytes(), true
		}
	}
	b, _ := json.Marshal(cell)
	return b, true
}

// fileNamespace là không gian tên UUIDv5 của batch_ref sinh từ tệp.
var fileNamespace = uuid.MustParse("0199c9a0-58f0-7000-8000-000000000058")

// FileBatchRef là batch_ref (= Idempotency-Key) tất định của lô thứ k (từ 0) của tệp.
func FileBatchRef(fileID uuid.UUID, k int) uuid.UUID {
	return uuid.NewSHA1(fileNamespace, []byte(fmt.Sprintf("%s:%d", fileID, k)))
}

// BatchCount là số lô của n bản ghi chia theo chunk.
func BatchCount(n, chunk int) int {
	if chunk < 1 {
		return 0
	}
	return (n + chunk - 1) / chunk
}

// FileChunk là một lô (phong bì ingest_batch.v1) sinh từ tệp.
type FileChunk struct {
	Index    int
	BatchRef uuid.UUID
	Body     []byte
}

// FileEnvelopes chia bản ghi thành các phong bì ingest_batch.v1 (cỡ chunk). Thân tất định theo (mã tệp, bản ghi,
// sentAt) — tải lại cùng tệp sinh đúng cùng thân, nên Submit của W5.4 coi là gọi lặp idempotent. entity_key tính
// từ bản ghi; không tính được → "" (đường ống lô từ chối integration.entity_key_mismatch).
func FileEnvelopes(c *integration.Contract, fileID uuid.UUID, records []json.RawMessage, chunk int, sentAt time.Time) ([]FileChunk, error) {
	if chunk < 1 {
		return nil, ErrBatchTooLarge
	}
	out := make([]FileChunk, 0, BatchCount(len(records), chunk))
	for k := 0; k*chunk < len(records); k++ {
		part := records[k*chunk : min(len(records), (k+1)*chunk)]
		items := make([]Item, 0, len(part))
		for _, rec := range part {
			key, err := c.EntityKeyOf(rec)
			if err != nil || len(key) > 300 {
				key = ""
			}
			items = append(items, Item{EntityKey: key, Record: rec})
		}
		ref := FileBatchRef(fileID, k)
		body, err := json.Marshal(struct {
			Contract        string `json:"contract"`
			ContractVersion int    `json:"contract_version"`
			BatchRef        string `json:"batch_ref"`
			SentAt          string `json:"sent_at"`
			Items           []Item `json:"items"`
		}{c.Name, c.Version, ref.String(), sentAt.UTC().Format(time.RFC3339Nano), items})
		if err != nil {
			return nil, err
		}
		out = append(out, FileChunk{Index: k, BatchRef: ref, Body: body})
	}
	return out, nil
}

// FileResult là một dòng của tệp kết quả: bản ghi thứ Record (từ 1, theo thứ tự trong tệp — CSV không tính dòng
// tiêu đề). Không có giá trị bản ghi (chỉ khóa thực thể và mã problem).
type FileResult struct {
	Record      int    `json:"record"`
	EntityKey   string `json:"entity_key"`
	Outcome     string `json:"outcome"`
	ProblemCode string `json:"problem_code,omitempty"`
	Detail      string `json:"detail,omitempty"`
}

// WriteResults ghi tệp kết quả theo định dạng (cùng định dạng với kênh tải lên).
func WriteResults(w io.Writer, format string, rows []FileResult) error {
	if format == FormatCSV {
		cw := csv.NewWriter(w)
		if err := cw.Write([]string{"record", "entity_key", "outcome", "problem_code", "detail"}); err != nil {
			return err
		}
		for _, r := range rows {
			if err := cw.Write([]string{fmt.Sprint(r.Record), cellSafe(r.EntityKey), r.Outcome, r.ProblemCode, cellSafe(r.Detail)}); err != nil {
				return err
			}
		}
		cw.Flush()
		return cw.Error()
	}
	enc := json.NewEncoder(w)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return nil
}

// cellSafe chặn chèn công thức khi mở tệp kết quả bằng bảng tính (ô bắt đầu bằng = + - @ tab CR).
func cellSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}
