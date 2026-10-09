package adapter

// GatewayBilateral là port.BilateralGateway qua cổng tích hợp (W5.6 — ADR-0034, ADR-0033 §3): khóa API của khách hàng
// API tenant MKG (header X-API-Key; bí mật chỉ ở cấu hình, không log). Chỉ thư viện chuẩn HTTP — không import vùng nền
// tảng (R-03).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
)

// GatewayBilateral gọi nạp theo lô, feed của cặp và tổng kiểm ngày của cổng tích hợp.
type GatewayBilateral struct {
	cfg    GatewayConfig
	client *http.Client
	now    func() time.Time
}

var _ port.BilateralGateway = (*GatewayBilateral)(nil)

// NewGatewayBilateral tạo client luồng song phương (cùng cấu hình cổng với feed danh bạ W5.5).
func NewGatewayBilateral(cfg GatewayConfig, now func() time.Time) *GatewayBilateral {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	c := cfg.Client
	if c == nil {
		c = &http.Client{Timeout: cfg.Timeout}
	}
	if now == nil {
		now = time.Now
	}
	return &GatewayBilateral{cfg: cfg, client: c, now: now}
}

func retry(format string, a ...any) error {
	return fmt.Errorf("%w: %s", port.ErrBilateralRetry, fmt.Sprintf(format, a...))
}

// do gửi request, đọc thân (≤ 16 MiB); lỗi mạng → ErrBilateralRetry.
func (g *GatewayBilateral) do(ctx context.Context, method, path string, q url.Values, body []byte, hdr map[string]string) (int, []byte, error) {
	u := strings.TrimRight(g.cfg.BaseURL, "/") + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return 0, nil, retry("%v", err)
	}
	req.Header.Set("X-API-Key", g.cfg.APIKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return 0, nil, retry("%v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return 0, nil, retry("đọc thân: %v", err)
	}
	return resp.StatusCode, raw, nil
}

// classify quy mã HTTP không thành công về lỗi của port.
func classify(status int, body []byte) error {
	code := problemCode(body)
	switch {
	case status == http.StatusForbidden && code == "integration.pair_missing":
		return port.ErrPairMissing
	case status == http.StatusGone:
		return port.ErrFeedCursorExpired
	case status == http.StatusTooManyRequests || status >= 500:
		return retry("HTTP %d %s", status, code)
	}
	return fmt.Errorf("fieldops: cổng tích hợp từ chối HTTP %d %s", status, code)
}

type batchDTO struct {
	ID      uuid.UUID `json:"id"`
	Status  string    `json:"status"`
	Results []struct {
		Index       int    `json:"index"`
		Outcome     string `json:"outcome"`
		ProblemCode string `json:"problem_code"`
	} `json:"results"`
}

// SubmitBatch cài đặt port.BilateralGateway: Idempotency-Key = batch_ref (gửi lại cùng lô là no-op ở cổng).
func (g *GatewayBilateral) SubmitBatch(ctx context.Context, contract string, version int, batchRef uuid.UUID, items []port.BilateralItem) (uuid.UUID, error) {
	type item struct {
		EntityKey string          `json:"entity_key"`
		Record    json.RawMessage `json:"record"`
	}
	env := struct {
		Contract        string    `json:"contract"`
		ContractVersion int       `json:"contract_version"`
		BatchRef        uuid.UUID `json:"batch_ref"`
		SentAt          string    `json:"sent_at"`
		Items           []item    `json:"items"`
	}{Contract: contract, ContractVersion: version, BatchRef: batchRef, SentAt: g.now().UTC().Format(time.RFC3339Nano)}
	for _, it := range items {
		env.Items = append(env.Items, item{EntityKey: it.EntityKey, Record: it.Record})
	}
	body, err := json.Marshal(env)
	if err != nil {
		return uuid.Nil, err
	}
	status, raw, err := g.do(ctx, http.MethodPost, "/v1/batches/"+url.PathEscape(contract), nil, body, map[string]string{"Idempotency-Key": batchRef.String()})
	if err != nil {
		return uuid.Nil, err
	}
	if status != http.StatusAccepted && status != http.StatusOK {
		return uuid.Nil, classify(status, raw)
	}
	var b batchDTO
	if err := json.Unmarshal(raw, &b); err != nil || b.ID == uuid.Nil {
		return uuid.Nil, retry("phản hồi lô không hợp lệ")
	}
	return b.ID, nil
}

// Batch cài đặt port.BilateralGateway.
func (g *GatewayBilateral) Batch(ctx context.Context, id uuid.UUID) (port.BilateralBatch, error) {
	status, raw, err := g.do(ctx, http.MethodGet, "/v1/batches/"+id.String(), nil, nil, nil)
	if err != nil {
		return port.BilateralBatch{}, err
	}
	if status != http.StatusOK {
		return port.BilateralBatch{}, classify(status, raw)
	}
	var b batchDTO
	if err := json.Unmarshal(raw, &b); err != nil {
		return port.BilateralBatch{}, retry("phản hồi lô không hợp lệ")
	}
	out := port.BilateralBatch{ID: b.ID, Status: b.Status}
	for _, r := range b.Results {
		out.Results = append(out.Results, port.BilateralResult{Index: r.Index, Outcome: r.Outcome, ProblemCode: r.ProblemCode})
	}
	return out, nil
}

// PairPage cài đặt port.BilateralGateway: GET /v1/feeds/bilateral?pair=<htx_ref>.
func (g *GatewayBilateral) PairPage(ctx context.Context, htxRef uuid.UUID, cursor string, limit int) (port.PairFeedPage, error) {
	q := url.Values{"pair": {htxRef.String()}}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	status, raw, err := g.do(ctx, http.MethodGet, "/v1/feeds/bilateral", q, nil, nil)
	if err != nil {
		return port.PairFeedPage{}, err
	}
	if status != http.StatusOK {
		return port.PairFeedPage{}, classify(status, raw)
	}
	var fp struct {
		Feed       string `json:"feed"`
		NextCursor string `json:"next_cursor"`
		HasMore    bool   `json:"has_more"`
		AsOf       string `json:"as_of"`
		Items      []struct {
			Seq        int64           `json:"seq"`
			EventID    uuid.UUID       `json:"event_id"`
			EntityKey  string          `json:"entity_key"`
			Op         string          `json:"op"`
			Contract   string          `json:"contract"`
			RecordedAt string          `json:"recorded_at"`
			Record     json.RawMessage `json:"record"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &fp); err != nil || fp.Feed != "bilateral" || fp.NextCursor == "" {
		return port.PairFeedPage{}, retry("phong bì feed cặp không hợp lệ")
	}
	out := port.PairFeedPage{NextCursor: fp.NextCursor, HasMore: fp.HasMore}
	if out.AsOf, err = time.Parse(time.RFC3339Nano, fp.AsOf); err != nil {
		return port.PairFeedPage{}, retry("as_of không hợp lệ")
	}
	for _, it := range fp.Items {
		name, ver, ok := strings.Cut(it.Contract, ".v")
		v, verr := strconv.Atoi(ver)
		at, terr := time.Parse(time.RFC3339Nano, it.RecordedAt)
		if !ok || verr != nil || v < 1 || terr != nil || it.Seq < 1 || it.EventID == uuid.Nil || it.Op != "upsert" || len(it.Record) == 0 {
			return port.PairFeedPage{}, retry("mục feed cặp không hợp lệ (seq %d)", it.Seq)
		}
		out.Items = append(out.Items, port.PairFeedItem{Seq: it.Seq, EventID: it.EventID, Contract: name, ContractVersion: v,
			EntityKey: it.EntityKey, Record: it.Record, RecordedAt: at})
	}
	return out, nil
}

// DayChecksums cài đặt port.BilateralGateway: GET /v1/feeds/{hợp đồng}/checksums?from=&to=&pair=.
func (g *GatewayBilateral) DayChecksums(ctx context.Context, contract string, htxRef uuid.UUID, from, to string) ([]port.DayChecksum, error) {
	q := url.Values{"from": {from}, "to": {to}}
	if htxRef != uuid.Nil {
		q.Set("pair", htxRef.String())
	}
	status, raw, err := g.do(ctx, http.MethodGet, "/v1/feeds/"+url.PathEscape(contract)+"/checksums", q, nil, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, classify(status, raw)
	}
	var b struct {
		Algorithm string `json:"algorithm"`
		Days      []struct {
			Date        string `json:"date"`
			RecordCount int64  `json:"record_count"`
			SHA256      string `json:"sha256"`
		} `json:"days"`
	}
	if err := json.Unmarshal(raw, &b); err != nil || b.Algorithm != "bilateral_day.v1" {
		return nil, retry("phản hồi tổng kiểm không hợp lệ")
	}
	out := make([]port.DayChecksum, 0, len(b.Days))
	for _, d := range b.Days {
		out = append(out, port.DayChecksum{Date: d.Date, RecordCount: d.RecordCount, SHA256: d.SHA256})
	}
	return out, nil
}
