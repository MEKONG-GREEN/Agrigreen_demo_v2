package client

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

// RevisionNames là họ tên trước/sau ĐÚNG bản sửa ở kho, chỉ trong bộ nhớ (R-14).
// Không chứa SĐT hoặc định danh khác; Revisions vẫn là hợp đồng nhãn che bớt.
type RevisionNames struct {
	Ref        uuid.UUID
	SubjectRef uuid.UUID
	PrevName   string `pii:"true"`
	NextName   string `pii:"true"`
}

// RevisionResolver là phần mở rộng tùy chọn cho HCN-09 (#276, Q9).
type RevisionResolver interface {
	// ResolveRevisions đọc tối đa MaxRevisionRefs bản sửa với PurposeFieldSupport.
	// Quyền, MFA và quota như Resolve (một lượt mỗi ref); audit resolve từng chủ thể
	// phải commit trước khi trả tên. Ref lạ/khác tenant/đã xóa bị bỏ qua.
	ResolveRevisions(context.Context, Purpose, []uuid.UUID) ([]RevisionNames, error)
}

func AsRevisionResolver(c Client) (RevisionResolver, bool) {
	r, ok := c.(RevisionResolver)
	return r, ok
}

// RevisionNamesItem là bản sửa trên dây; không cache hoặc ghi vào idempotency.
type RevisionNamesItem struct {
	Ref        string `json:"ref"`
	SubjectRef string `json:"subject_ref"`
	PrevName   string `json:"prev_name" pii:"true"`
	NextName   string `json:"next_name" pii:"true"`
}

type RevisionNamesResponse struct {
	Items []RevisionNamesItem `json:"items"`
}

var _ RevisionResolver = (*httpClient)(nil)
var _ RevisionResolver = local{}

func (c *httpClient) ResolveRevisions(ctx context.Context, purpose Purpose, refs []uuid.UUID) ([]RevisionNames, error) {
	if purpose == "" {
		return nil, ProblemPurposeRequired
	}
	in := RevisionsRequest{Purpose: string(purpose), Refs: make([]string, len(refs))}
	for i, r := range refs {
		in.Refs[i] = r.String()
	}
	var out RevisionNamesResponse
	if err := c.do(ctx, call{method: http.MethodPost, path: "/v1/subjects/revisions/resolve", purpose: purpose, in: in, out: &out}); err != nil {
		return nil, err
	}
	res := make([]RevisionNames, 0, len(out.Items))
	for _, it := range out.Items {
		ref, e1 := uuid.Parse(it.Ref)
		subject, e2 := uuid.Parse(it.SubjectRef)
		if e1 != nil || e2 != nil {
			continue
		}
		res = append(res, RevisionNames{Ref: ref, SubjectRef: subject, PrevName: it.PrevName, NextName: it.NextName})
	}
	return res, nil
}

func (l local) ResolveRevisions(ctx context.Context, purpose Purpose, refs []uuid.UUID) ([]RevisionNames, error) {
	if purpose == "" {
		return nil, ProblemPurposeRequired
	}
	r, ok := l.s.(RevisionResolver)
	if !ok {
		return nil, ProblemRevisionsUnsupported
	}
	return r.ResolveRevisions(ctx, purpose, refs)
}
