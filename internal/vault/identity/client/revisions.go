package client

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// CNTS-V4-HOUSEHOLD-LIST (#251, vault 20261106164139, ADR-0056 mục 4): bản sửa họ tên/SĐT cho lịch sử "giá trị cũ → mới"
// (BRD CNTS v4.0 FN-10 HCN-09). Mở rộng TÙY CHỌN của Client như Profiles — module cần thì gọi AsRevisions. Amend của
// Profiles giữ chữ ký cũ (pf/hrm, pf/contact không đổi); AmendRevision là cùng thao tác, trả thêm mã bản sửa.

// MaxRevisionRefs là số bản sửa tối đa của một lần Revisions.
const MaxRevisionRefs = 200

// Revision là nhãn che bớt trước/sau của một lần sửa — KHÔNG có họ tên/SĐT đầy đủ. CHỈ trong bộ nhớ (R-14: không log).
type Revision struct {
	Ref        uuid.UUID
	SubjectRef uuid.UUID
	PrevLabel  string `pii:"true"` // vd "Nguyễn V. A."
	NextLabel  string `pii:"true"`
	PrevPhone  string `pii:"true"` // vd "0912•••123"; rỗng nếu không có SĐT
	NextPhone  string `pii:"true"`
}

// Revisions là phần mở rộng của kho cho lịch sử họ tên/SĐT.
type Revisions interface {
	// AmendRevision như Profiles.Amend; trả mã bản sửa (revision_ref) để bên gọi lưu cùng phiên bản của mình.
	AmendRevision(ctx context.Context, purpose Purpose, ref uuid.UUID, id Identity) (uuid.UUID, error)
	// Revisions trả nhãn che bớt trước/sau của 0–MaxRevisionRefs bản sửa. Mã lạ, của tenant khác hoặc của chủ thể đã xóa
	// định danh bị bỏ qua (không lỗi). Vai như Display; kho ghi sổ op "revision_read" cho từng chủ thể.
	Revisions(ctx context.Context, purpose Purpose, refs []uuid.UUID) ([]Revision, error)
}

// AsRevisions trả phần mở rộng bản sửa của c; ok = false nếu hiện thực không hỗ trợ.
func AsRevisions(c Client) (Revisions, bool) {
	r, ok := c.(Revisions)
	return r, ok
}

// RevisionsRequest là thân POST /v1/subjects/revisions.
type RevisionsRequest struct {
	Purpose string   `json:"purpose"`
	Refs    []string `json:"refs"`
}

// RevisionItem là một bản sửa trên dây.
type RevisionItem struct {
	Ref        string `json:"ref"`
	SubjectRef string `json:"subject_ref"`
	PrevLabel  string `json:"prev_label" pii:"true"`
	NextLabel  string `json:"next_label" pii:"true"`
	PrevPhone  string `json:"prev_masked_phone,omitempty" pii:"true"`
	NextPhone  string `json:"next_masked_phone,omitempty" pii:"true"`
}

// RevisionsResponse là phản hồi đọc bản sửa.
type RevisionsResponse struct {
	Items []RevisionItem `json:"items"`
}

var _ Revisions = (*httpClient)(nil)

func (c *httpClient) AmendRevision(ctx context.Context, purpose Purpose, ref uuid.UUID, id Identity) (uuid.UUID, error) {
	if purpose == "" {
		return uuid.Nil, ProblemPurposeRequired
	}
	var out WriteResponse
	if err := c.do(ctx, call{method: http.MethodPost, path: subjectPath(ref, "/amend"), purpose: purpose, idemOp: "amend",
		in: AmendRequest{Purpose: string(purpose), FullName: id.FullName, Phone: id.Phone, Address: id.Address}, out: &out}); err != nil {
		return uuid.Nil, err
	}
	rev, err := uuid.Parse(out.RevisionRef)
	if err != nil {
		return uuid.Nil, problem.New(http.StatusBadGateway, CodeUnavailable, "Kho định danh không trả mã bản sửa").WithCause(err)
	}
	return rev, nil
}

func (c *httpClient) Revisions(ctx context.Context, purpose Purpose, refs []uuid.UUID) ([]Revision, error) {
	if purpose == "" {
		return nil, ProblemPurposeRequired
	}
	if len(refs) == 0 {
		return []Revision{}, nil
	}
	in := RevisionsRequest{Purpose: string(purpose), Refs: make([]string, len(refs))}
	for i, r := range refs {
		in.Refs[i] = r.String()
	}
	var out RevisionsResponse
	if err := c.do(ctx, call{method: http.MethodPost, path: "/v1/subjects/revisions", purpose: purpose, in: in, out: &out}); err != nil {
		return nil, err
	}
	res := make([]Revision, 0, len(out.Items))
	for _, it := range out.Items {
		ref, err1 := uuid.Parse(it.Ref)
		subj, err2 := uuid.Parse(it.SubjectRef)
		if err1 != nil || err2 != nil {
			continue
		}
		res = append(res, Revision{Ref: ref, SubjectRef: subj, PrevLabel: it.PrevLabel, NextLabel: it.NextLabel,
			PrevPhone: it.PrevPhone, NextPhone: it.NextPhone})
	}
	return res, nil
}

var _ Revisions = local{}

func (l local) revisions() (Revisions, error) {
	r, ok := l.s.(Revisions)
	if !ok {
		return nil, ProblemRevisionsUnsupported
	}
	return r, nil
}

func (l local) AmendRevision(ctx context.Context, purpose Purpose, ref uuid.UUID, id Identity) (uuid.UUID, error) {
	if purpose == "" {
		return uuid.Nil, ProblemPurposeRequired
	}
	r, err := l.revisions()
	if err != nil {
		return uuid.Nil, err
	}
	return r.AmendRevision(ctx, purpose, ref, id)
}

func (l local) Revisions(ctx context.Context, purpose Purpose, refs []uuid.UUID) ([]Revision, error) {
	if purpose == "" {
		return nil, ProblemPurposeRequired
	}
	r, err := l.revisions()
	if err != nil {
		return nil, err
	}
	return r.Revisions(ctx, purpose, refs)
}

// CodeRevisionsUnsupported: hiện thực kho không có phần mở rộng bản sửa — lỗi cấu hình.
const CodeRevisionsUnsupported = "vault.revisions_unsupported"

// ProblemRevisionsUnsupported: 503 — kho được nối không hỗ trợ bản sửa.
var ProblemRevisionsUnsupported = problem.New(http.StatusServiceUnavailable, CodeRevisionsUnsupported, "Kho định danh chưa hỗ trợ lịch sử họ tên, số điện thoại")
