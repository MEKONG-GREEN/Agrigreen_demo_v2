package integration

// Luồng song phương (ADR-0034; W5.6): phần dùng CHUNG cho hai bên của cặp — nền tảng (sổ song phương pf_coop,
// cổng tích hợp) và đối tác (bộ điều hợp của MKG) — để cùng một công thức cho khóa nghiệp vụ, băm bản ghi và tổng
// kiểm theo ngày (R-INT-10). Chỉ thư viện chuẩn: gói này được cả vùng nền tảng lẫn vùng mkg import.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// BilateralDayAlgorithm định danh công thức tổng kiểm theo ngày của luồng song phương (GET
// /v1/feeds/{hợp đồng}/checksums trả trong trường algorithm).
const BilateralDayAlgorithm = "bilateral_day.v1"

// PairFeed là tên feed của cặp (bilateral.pair_feed của mọi hợp đồng song phương): GET /v1/feeds/bilateral?pair=…
const PairFeed = "bilateral"

// CanonicalJSON chuẩn hóa JSON (khóa sắp xếp, số giữ nguyên chữ số, không khoảng trắng) để so và băm nội dung không
// phụ thuộc định dạng của bên gửi.
func CanonicalJSON(raw []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

// PayloadSHA256 là SHA-256 hex của JSON chuẩn hóa — payload_sha256 của sổ song phương và của tổng kiểm ngày.
func PayloadSHA256(raw []byte) (string, error) {
	c, err := CanonicalJSON(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(c)
	return hex.EncodeToString(sum[:]), nil
}

// BusinessKeyOf dựng khóa nghiệp vụ của bản ghi nạp: giá trị các trường ingest.business_key (rỗng → entity_key) nối
// bằng EntityKeySep.
func (c *Contract) BusinessKeyOf(raw []byte) (string, error) {
	fields := c.EntityKey
	if c.Ingest != nil && len(c.Ingest.BusinessKey) > 0 {
		fields = c.Ingest.BusinessKey
	}
	return keyOf(raw, fields, "business_key")
}

// PairKeyOf trả giá trị trường bilateral.pair_key (một trường — htx_ref) của bản ghi song phương.
func (c *Contract) PairKeyOf(raw []byte) (string, error) {
	if c.Bilateral == nil || len(c.Bilateral.PairKey) != 1 {
		return "", fmt.Errorf("hợp đồng %s không phải song phương một khóa cặp", c.ID())
	}
	return keyOf(raw, c.Bilateral.PairKey, "pair_key")
}

func keyOf(raw []byte, fields []string, what string) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return "", err
	}
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		switch v := m[f].(type) {
		case string:
			parts = append(parts, v)
		case json.Number:
			parts = append(parts, v.String())
		default:
			return "", fmt.Errorf("%s %s thiếu hoặc không phải string/integer", what, f)
		}
	}
	return strings.Join(parts, EntityKeySep), nil
}

// BilateralDayRecord là một bản ghi song phương tham gia tổng kiểm một ngày.
type BilateralDayRecord struct {
	BusinessKey     string
	ContractVersion int
	PayloadSHA256   string
}

// BilateralDayDigest tính (số bản ghi, sha256 hex) của một ngày theo BilateralDayAlgorithm: SHA-256 của các dòng
// "<business_key>\t<contract_version>\t<payload_sha256>\n" sắp theo (business_key theo byte, contract_version). Ngày
// không có bản ghi: 0 và SHA-256 của chuỗi rỗng. Ngày = ngày nghiệp vụ (giờ Việt Nam) của recorded_at mà sổ song
// phương ghi (trả trong mục feed của cặp) — hai bên tính cùng công thức trên bản ghi mình giữ.
func BilateralDayDigest(recs []BilateralDayRecord) (int64, string) {
	rs := append([]BilateralDayRecord(nil), recs...)
	sort.Slice(rs, func(i, j int) bool {
		if rs[i].BusinessKey != rs[j].BusinessKey {
			return rs[i].BusinessKey < rs[j].BusinessKey
		}
		return rs[i].ContractVersion < rs[j].ContractVersion
	})
	h := sha256.New()
	for _, r := range rs {
		h.Write([]byte(r.BusinessKey + "\t" + strconv.Itoa(r.ContractVersion) + "\t" + r.PayloadSHA256 + "\n"))
	}
	return int64(len(rs)), hex.EncodeToString(h.Sum(nil))
}

// BilateralContracts trả tên các hợp đồng nạp song phương hợp lệ (mọi trạng thái), sắp theo tên.
func BilateralContracts() []string {
	all, err := All()
	if err != nil {
		return nil
	}
	var out []string
	for _, c := range all {
		if c.Kind == KindIngest && c.Flow == FlowBilateral && (len(out) == 0 || out[len(out)-1] != c.Name) {
			out = append(out, c.Name)
		}
	}
	sort.Strings(out)
	return out
}
