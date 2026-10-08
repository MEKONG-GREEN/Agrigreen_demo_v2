package main

// Cấp đối tác TỔNG HỢP cho kịch bản cổng (W5.9) qua Admin API của pf/apiclient — đúng thủ tục runbook
// public-api-onboarding (bốn mắt: người đề xuất api_client_admin ≠ người duyệt api_client_approver, MFA):
// khách hàng API → đăng ký feed (feed:<tên>:read) → đăng ký hợp đồng nạp (ingest:<hợp đồng>:write) → gói hạn mức
// → phát hành khóa. Khóa chỉ hiện một lần và được ghi vào tệp TSV 0600; KHÔNG BAO GIỜ dùng khóa của đối tác thật.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// GWProvision là tham số cấp đối tác.
type GWProvision struct {
	Admin         string // URL gốc Admin API (api-platform), vd https://admin.staging.<miền>
	AdminToken    string // bearer MFA, vai api_client_admin, tenant đơn vị vận hành
	ApproverToken string // bearer MFA, vai api_client_approver (người khác)
	Partners      int
	Noisy         int // số đối tác cuối danh sách đánh dấu class=noisy
	Feed          string
	Contract      string
	QuotaRPM      int64
	DailyUnits    int64
	Label         string // tiền tố display_name, vd loadgen-w59-20261009
	Parallel      int
	HTTP          *http.Client
}

func (p GWProvision) call(ctx context.Context, tok, method, path string, body any) (int, map[string]any, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(p.Admin, "/")+path, rd)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method == http.MethodPost {
		req.Header.Set("Idempotency-Key", ids.New().String())
	}
	resp, err := p.HTTP.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	out := map[string]any{}
	if len(raw) > 0 && json.Unmarshal(raw, &out) != nil {
		return resp.StatusCode, nil, fmt.Errorf("%s %s: phản hồi không phải JSON (HTTP %d)", method, path, resp.StatusCode)
	}
	return resp.StatusCode, out, nil
}

// one cấp một đối tác, trả mã khách hàng + bí mật khóa.
func (p GWProvision) one(ctx context.Context, i int) (*GWPartner, error) {
	step := func(tok, what, method, path string, body any, wantStatus int) (map[string]any, error) {
		st, out, err := p.call(ctx, tok, method, path, body)
		if err != nil {
			return nil, fmt.Errorf("đối tác %d, %s: %w", i, what, err)
		}
		if st != wantStatus {
			code, _ := out["code"].(string)
			return nil, fmt.Errorf("đối tác %d, %s: HTTP %d %s", i, what, st, code)
		}
		return out, nil
	}
	out, err := step(p.AdminToken, "đề xuất khách hàng API", http.MethodPost, "/v1/api-clients", map[string]any{
		"client_tenant_id": ids.New().String(), "display_name": fmt.Sprintf("%s-%03d", p.Label, i),
		"purpose": "Kiểm thử tải cổng tích hợp W5.9 (§42) — đối tác TỔNG HỢP, không phải khách hàng thật", "contact_ref": ids.New().String()}, http.StatusCreated)
	if err != nil {
		return nil, err
	}
	cid, _ := out["id"].(string)
	if _, err := step(p.ApproverToken, "duyệt khách hàng", http.MethodPost, "/v1/api-clients/"+cid+"/approve", nil, http.StatusOK); err != nil {
		return nil, err
	}
	for _, sub := range []struct{ product, scope string }{{p.Feed, apiclientport.FeedScope(p.Feed)}, {p.Contract, apiclientport.IngestScope(p.Contract)}} {
		out, err := step(p.AdminToken, "đăng ký "+sub.product, http.MethodPost, "/v1/api-clients/"+cid+"/subscriptions",
			map[string]any{"product": sub.product, "scopes": []string{sub.scope}}, http.StatusCreated)
		if err != nil {
			return nil, err
		}
		sid, _ := out["id"].(string)
		if _, err := step(p.ApproverToken, "duyệt đăng ký "+sub.product, http.MethodPost, "/v1/api-clients/"+cid+"/subscriptions/"+sid+"/approve", nil,
			http.StatusOK); err != nil {
			return nil, err
		}
	}
	out, err = step(p.AdminToken, "đề xuất gói hạn mức", http.MethodPost, "/v1/api-clients/"+cid+"/plans", map[string]any{
		"requests_per_minute": p.QuotaRPM, "daily_units": p.DailyUnits, "monthly_units": p.DailyUnits * 31, "max_export_rows": 1000}, http.StatusCreated)
	if err != nil {
		return nil, err
	}
	if _, err := step(p.ApproverToken, "duyệt gói", http.MethodPost, fmt.Sprintf("/v1/api-clients/%s/plans/%v/approve", cid, out["version"]), nil,
		http.StatusOK); err != nil {
		return nil, err
	}
	out, err = step(p.AdminToken, "phát hành khóa", http.MethodPost, "/v1/api-clients/"+cid+"/keys", nil, http.StatusCreated)
	if err != nil {
		return nil, err
	}
	key, _ := out["api_key"].(string)
	if _, ok := apiclientport.ParseKey(key); !ok {
		return nil, fmt.Errorf("đối tác %d: phát hành khóa không trả bí mật đúng dạng", i)
	}
	class := ClassNormal
	if i >= p.Partners-p.Noisy {
		class = ClassNoisy
	}
	return &GWPartner{ID: cid, Key: key, Class: class}, nil
}

// Provision cấp p.Partners đối tác (song song p.Parallel), thứ tự ổn định theo chỉ số.
func Provision(ctx context.Context, p GWProvision) ([]*GWPartner, error) {
	if p.Partners < 1 || p.Noisy < 0 || p.Noisy >= p.Partners {
		return nil, fmt.Errorf("cần ≥ 1 đối tác và 0 ≤ số đối tác ồn < số đối tác")
	}
	if p.Admin == "" || p.AdminToken == "" || p.ApproverToken == "" {
		return nil, fmt.Errorf("cần URL Admin API và hai token MFA (người đề xuất ≠ người duyệt)")
	}
	if p.HTTP == nil {
		p.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	if p.Parallel < 1 {
		p.Parallel = 4
	}
	if p.Label == "" {
		p.Label = "loadgen-w59-" + time.Now().UTC().Format("20060102T150405")
	}
	out := make([]*GWPartner, p.Partners)
	errs := make([]error, p.Partners)
	work := make(chan int)
	var wg sync.WaitGroup
	for range p.Parallel {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				out[i], errs[i] = p.one(ctx, i)
			}
		}()
	}
	for i := range p.Partners {
		if ctx.Err() != nil {
			break
		}
		work <- i
	}
	close(work)
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
