package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// ErrNoToken: ctx không có access token của người dùng cuối để chuyển tiếp.
var ErrNoToken = errors.New("vault: thiếu token người dùng để chuyển tiếp")

// maxResponse giới hạn thân phản hồi đọc từ kho.
const maxResponse = 4 << 20

// NewHTTP tạo Client gọi dịch vụ identity-vault qua HTTP(S)/mTLS.
//
// Mỗi lời gọi chuyển tiếp NGUYÊN access token của người dùng cuối lấy từ
// authz.RawToken(ctx) (httpx.Authenticate đã gắn sẵn); kho tự xác minh token,
// lấy tenant và người yêu cầu từ token (R-16, R-54). hc nil → client mặc định
// có timeout 10 giây; cấu hình mTLS đặt trong hc.Transport.
func NewHTTP(baseURL string, hc *http.Client) Client {
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	return &httpClient{base: strings.TrimRight(baseURL, "/"), hc: hc}
}

type httpClient struct {
	base string
	hc   *http.Client
}

var _ Client = (*httpClient)(nil)

// problemBody là phần problem+json client cần đọc.
type problemBody struct {
	Status    int    `json:"status"`
	Code      string `json:"code"`
	Title     string `json:"title"`
	Detail    string `json:"detail"`
	Ref       string `json:"ref"`
	PublicKey []byte `json:"public_key"`
}

type call struct {
	method, path string
	purpose      Purpose
	idemOp       string // khác rỗng → gửi Idempotency-Key suy từ khóa của request gốc
	in, out      any
	dup          *problemBody // nhận thân lỗi (dùng cho 409 duplicate)
}

func (c *httpClient) do(ctx context.Context, k call) error {
	raw, ok := authz.RawToken(ctx)
	if !ok {
		return problem.Unauthenticated.WithCause(ErrNoToken)
	}
	var body []byte
	if k.in != nil {
		var err error
		if body, err = json.Marshal(k.in); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, k.method, c.base+k.path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+raw)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if k.purpose != "" {
		req.Header.Set(HeaderPurpose, string(k.purpose))
	}
	if tid := telemetry.TraceID(ctx); tid != "" {
		req.Header.Set("traceparent", "00-"+tid+"-"+randHex(8)+"-01")
	}
	if k.idemOp != "" {
		req.Header.Set(idempotency.Header, deriveKey(ctx, k.idemOp, k.path, body))
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return problem.New(http.StatusServiceUnavailable, CodeUnavailable, "Kho định danh không phản hồi").WithCause(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return problem.New(http.StatusBadGateway, CodeUnavailable, "Đọc phản hồi kho định danh thất bại").WithCause(err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if k.out == nil {
			return nil
		}
		if err := json.Unmarshal(data, k.out); err != nil {
			return problem.New(http.StatusBadGateway, CodeUnavailable, "Phản hồi kho định danh sai dạng").WithCause(err)
		}
		return nil
	}
	var pb problemBody
	_ = json.Unmarshal(data, &pb)
	if k.dup != nil {
		*k.dup = pb
	}
	return toError(resp.StatusCode, pb)
}

// toError quy problem của kho về lỗi có errors.Is tới ErrX của package này.
func toError(status int, pb problemBody) error {
	switch pb.Code {
	case CodePurposeRequired:
		return ProblemPurposeRequired
	case CodeNotFound:
		return ProblemNotFound
	case CodeErased:
		return ProblemErased
	case CodeDuplicate:
		return ProblemDuplicate
	case "":
		if status >= 500 {
			return problem.New(http.StatusBadGateway, CodeUnavailable, "Kho định danh lỗi")
		}
		return problem.New(status, "vault.error", "Kho định danh từ chối yêu cầu")
	}
	p := problem.New(status, pb.Code, pb.Title)
	if pb.Detail != "" {
		p = p.WithDetail(pb.Detail)
	}
	if status >= 500 {
		// Lỗi hệ thống của kho: không đẩy mã nội bộ của kho sang client của module gọi.
		return problem.New(http.StatusBadGateway, CodeUnavailable, "Kho định danh lỗi").WithCause(p)
	}
	return p
}

// deriveKey suy Idempotency-Key cho lời gọi kho từ khóa của request gốc: gọi
// lặp request gốc (vd sau khi tiến trình chết) → gọi lặp kho với cùng khóa →
// kho trả lại đúng kết quả lần đầu. Không có khóa gốc → khóa ngẫu nhiên.
func deriveKey(ctx context.Context, op, path string, body []byte) string {
	orig := idempotency.KeyFromContext(ctx)
	if orig == "" {
		return "vault-" + op + "-" + randHex(16)
	}
	h := sha256.New()
	h.Write([]byte(orig + "\n" + op + "\n" + path + "\n"))
	h.Write(body)
	return "vault-" + op + "-" + hex.EncodeToString(h.Sum(nil))[:40]
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func subjectPath(ref uuid.UUID, tail string) string {
	return "/v1/subjects/" + ref.String() + tail
}

func (c *httpClient) Create(ctx context.Context, purpose Purpose, id Identity) (Created, error) {
	if purpose == "" {
		return Created{}, ProblemPurposeRequired
	}
	var out CreatedResponse
	var pb problemBody
	err := c.do(ctx, call{method: http.MethodPost, path: "/v1/subjects", purpose: purpose, idemOp: "create", dup: &pb,
		in: CreateRequest{
			Purpose: string(purpose), Kind: string(id.Kind), FullName: id.FullName, Phone: id.Phone,
			NationalID: id.NationalID, Address: id.Address, AreaCode: id.AreaCode,
		}, out: &out})
	if errors.Is(err, ErrDuplicate) {
		ref, _ := uuid.Parse(pb.Ref)
		return Created{Ref: ref, PublicKey: pb.PublicKey}, err
	}
	if err != nil {
		return Created{}, err
	}
	ref, err := uuid.Parse(out.Ref)
	if err != nil {
		return Created{}, problem.New(http.StatusBadGateway, CodeUnavailable, "Phản hồi kho định danh sai dạng").WithCause(err)
	}
	return Created{Ref: ref, PublicKey: out.PublicKey}, nil
}

func (c *httpClient) Display(ctx context.Context, purpose Purpose, ref uuid.UUID) (Display, error) {
	if purpose == "" {
		return Display{}, ProblemPurposeRequired
	}
	var out DisplayResponse
	err := c.do(ctx, call{method: http.MethodGet, path: subjectPath(ref, "/display"), purpose: purpose, out: &out})
	if errors.Is(err, ErrErased) {
		return Display{Ref: ref, Label: ErasedLabel}, err
	}
	if err != nil {
		return Display{}, err
	}
	return Display{Ref: ref, Label: out.Label, MaskedPhone: out.MaskedPhone}, nil
}

// ErasedLabel là nhãn hiển thị Display trả kèm ErrErased.
const ErasedLabel = "(đã xóa định danh)"

func (c *httpClient) Resolve(ctx context.Context, purpose Purpose, ref uuid.UUID) (Identity, error) {
	if purpose == "" {
		return Identity{}, ProblemPurposeRequired
	}
	var out IdentityResponse
	if err := c.do(ctx, call{method: http.MethodPost, path: subjectPath(ref, "/resolve"), purpose: purpose,
		in: PurposeRequest{Purpose: string(purpose)}, out: &out}); err != nil {
		return Identity{}, err
	}
	return Identity{
		Kind: SubjectKind(out.Kind), FullName: out.FullName, Phone: out.Phone, NationalID: out.NationalID,
		Address: out.Address, AreaCode: out.AreaCode,
	}, nil
}

func (c *httpClient) Lookup(ctx context.Context, purpose Purpose, phoneOrNationalID string) (uuid.UUID, error) {
	if purpose == "" {
		return uuid.Nil, ProblemPurposeRequired
	}
	var out LookupResponse
	if err := c.do(ctx, call{method: http.MethodPost, path: "/v1/subjects/lookup", purpose: purpose,
		in: LookupRequest{Purpose: string(purpose), Value: phoneOrNationalID}, out: &out}); err != nil {
		return uuid.Nil, err
	}
	ref, err := uuid.Parse(out.Ref)
	if err != nil {
		return uuid.Nil, problem.New(http.StatusBadGateway, CodeUnavailable, "Phản hồi kho định danh sai dạng").WithCause(err)
	}
	return ref, nil
}

func (c *httpClient) Unwrap(ctx context.Context, purpose Purpose, ref uuid.UUID, wrappedDEK []byte) ([]byte, error) {
	if purpose == "" {
		return nil, ProblemPurposeRequired
	}
	var out UnwrapResponse
	if err := c.do(ctx, call{method: http.MethodPost, path: subjectPath(ref, "/unwrap"), purpose: purpose,
		in: UnwrapRequest{Purpose: string(purpose), WrappedDEK: wrappedDEK}, out: &out}); err != nil {
		return nil, err
	}
	return out.DEK, nil
}

func (c *httpClient) Erase(ctx context.Context, ref uuid.UUID, reason string) error {
	return c.do(ctx, call{method: http.MethodPost, path: subjectPath(ref, "/erasure"), idemOp: "erase",
		in: ErasureRequest{Reason: reason}, out: &ErasureResponse{}})
}

func (c *httpClient) PubKeys(ctx context.Context, areaCodes []string) ([]PubKey, error) {
	var all []PubKey
	cursor := ""
	for {
		q := url.Values{}
		for _, a := range areaCodes {
			q.Add("area", a)
		}
		q.Set("limit", "1000")
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		var out PubKeysResponse
		if err := c.do(ctx, call{method: http.MethodGet, path: "/v1/pubkeys?" + q.Encode(), out: &out}); err != nil {
			return nil, err
		}
		for _, it := range out.Items {
			ref, err := uuid.Parse(it.Ref)
			if err != nil {
				return nil, problem.New(http.StatusBadGateway, CodeUnavailable, "Phản hồi kho định danh sai dạng").WithCause(err)
			}
			all = append(all, PubKey{Ref: ref, AreaCode: it.AreaCode, PublicKey: it.PublicKey})
		}
		if out.NextCursor == nil || *out.NextCursor == "" {
			return all, nil
		}
		cursor = *out.NextCursor
	}
}
