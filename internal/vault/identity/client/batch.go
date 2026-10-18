package client

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// W11.28: đọc hồ sơ nhân sự THEO LÔ cho báo cáo Sở NS01–NS06 và dashboard nhân sự (BRD HCTH v3.0 FN-12, FN-06). Mở rộng
// TÙY CHỌN của Client như Profiles — module cần thì gọi AsProfileBatch; hiện thực không hỗ trợ → module tự rơi về đọc
// từng chủ thể (Resolve + GetSection).
//
// Khác Resolve/GetSection: MỘT lần gọi đọc tối đa MaxBatchRefs chủ thể, chịu MỘT lượt hạn mức (khóa hạn mức riêng
// "batch_read") — báo cáo 1.000 người không còn chạm hạn mức 600 lượt/giờ của Resolve. Kho vẫn ghi sổ truy cập TỪNG chủ
// thể với thao tác thật đã làm (resolve khi đọc định danh, section_read khi đọc ngăn) — không cần thêm giá trị op mới.

// MaxBatchRefs là số chủ thể tối đa của một lần ReadBatch.
const MaxBatchRefs = 200

// BatchRequest là yêu cầu đọc theo lô.
//   - Purpose PurposeHRMProfile: Section = SectionHRMProfile (hoặc rỗng = không đọc ngăn); Identity = true đọc họ tên,
//     SĐT, địa chỉ (KHÔNG BAO GIỜ CCCD).
//   - Purpose PurposeHRMSensitive: Section = SectionHRMSensitive; Identity phải false.
type BatchRequest struct {
	Purpose  Purpose
	Refs     []uuid.UUID
	Identity bool
	Section  string
}

// BatchItem là kết quả của một chủ thể — CHỈ trong bộ nhớ (R-14: không log).
type BatchItem struct {
	Ref      uuid.UUID
	FullName string `pii:"true"`
	Phone    string `pii:"true"`
	Address  string `pii:"true"`
	Fields   Fields // ngăn được hỏi (chưa ghi → rỗng)
	Erased   bool   // chủ thể đã xóa định danh (mọi trường rỗng)
}

// ProfileBatch là phần mở rộng đọc theo lô.
type ProfileBatch interface {
	// ReadBatch đọc 1–MaxBatchRefs chủ thể. Ref không có trong tenant bị bỏ qua (không lỗi, không ghi sổ). Vai
	// RoleRegistrar; MFA khi đọc định danh hoặc ngăn nhạy cảm (như Resolve/GetSection). Thứ tự kết quả theo Refs.
	ReadBatch(ctx context.Context, req BatchRequest) ([]BatchItem, error)
}

// AsProfileBatch trả phần mở rộng đọc theo lô của c; ok = false nếu hiện thực không hỗ trợ.
func AsProfileBatch(c Client) (ProfileBatch, bool) {
	b, ok := c.(ProfileBatch)
	return b, ok
}

// --- Dạng trên dây (contracts/openapi/identity-vault.yaml, POST /v1/subjects/batch-read). ---

// BatchReadRequest là thân POST /v1/subjects/batch-read.
type BatchReadRequest struct {
	Purpose  string   `json:"purpose"`
	Refs     []string `json:"refs"`
	Identity bool     `json:"identity,omitempty"`
	Section  string   `json:"section,omitempty"`
}

// BatchReadItem là một chủ thể trên dây.
type BatchReadItem struct {
	Ref      string            `json:"ref"`
	FullName string            `json:"full_name,omitempty" pii:"true"`
	Phone    string            `json:"phone,omitempty" pii:"true"`
	Address  string            `json:"address,omitempty" pii:"true"`
	Fields   map[string]string `json:"fields,omitempty" pii:"true"`
	Erased   bool              `json:"erased,omitempty"`
}

// BatchReadResponse là phản hồi đọc theo lô.
type BatchReadResponse struct {
	Items []BatchReadItem `json:"items"`
}

// CodeBatchUnsupported: hiện thực kho không có phần mở rộng đọc theo lô.
const CodeBatchUnsupported = "vault.batch_unsupported"

// ProblemBatchUnsupported: 503 — kho được nối không hỗ trợ đọc theo lô.
var ProblemBatchUnsupported = problem.New(http.StatusServiceUnavailable, CodeBatchUnsupported, "Kho định danh chưa hỗ trợ đọc theo lô")

var _ ProfileBatch = (*httpClient)(nil)

func (c *httpClient) ReadBatch(ctx context.Context, req BatchRequest) ([]BatchItem, error) {
	if req.Purpose == "" {
		return nil, ProblemPurposeRequired
	}
	if len(req.Refs) == 0 {
		return nil, nil
	}
	in := BatchReadRequest{Purpose: string(req.Purpose), Identity: req.Identity, Section: req.Section, Refs: make([]string, len(req.Refs))}
	for i, r := range req.Refs {
		in.Refs[i] = r.String()
	}
	var out BatchReadResponse
	if err := c.do(ctx, call{method: http.MethodPost, path: "/v1/subjects/batch-read", purpose: req.Purpose, in: in, out: &out}); err != nil {
		return nil, err
	}
	res := make([]BatchItem, 0, len(out.Items))
	for _, it := range out.Items {
		ref, err := uuid.Parse(it.Ref)
		if err != nil {
			continue
		}
		f := Fields(it.Fields)
		if f == nil {
			f = Fields{}
		}
		res = append(res, BatchItem{Ref: ref, FullName: it.FullName, Phone: it.Phone, Address: it.Address, Fields: f, Erased: it.Erased})
	}
	return res, nil
}

var _ ProfileBatch = local{}

func (l local) ReadBatch(ctx context.Context, req BatchRequest) ([]BatchItem, error) {
	if req.Purpose == "" {
		return nil, ProblemPurposeRequired
	}
	b, ok := l.s.(ProfileBatch)
	if !ok {
		return nil, ProblemBatchUnsupported
	}
	return b.ReadBatch(ctx, req)
}
