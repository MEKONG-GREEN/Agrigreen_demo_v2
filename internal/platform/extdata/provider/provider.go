// Package provider: nhà cung cấp cắm được của pf/extdata (ADR-0007).
//
//   - HTTP: một mẫu URL với chỗ thay {lat} {lon} {cell} (nguồn điểm) hoặc {z} {x} {y}
//     (tile). Yêu cầu đi ra được dựng MỚI: không Authorization, không cookie, không
//     X-Forwarded-For, không header của người gọi; User-Agent cố định; không theo
//     chuyển hướng; giới hạn kích thước và loại nội dung trả về.
//   - Fake: dữ liệu tất định cho Local/Dev và kiểm thử; ghi lại truy vấn đã nhận để
//     kiểm thử chứng minh nhà cung cấp chỉ thấy ô lưới thô. Cấm ở production.
//
// Nhà cung cấp cụ thể (thời tiết, độ mặn, tile vệ tinh trong lãnh thổ) chờ khách chọn
// (tong-hop.md Q2-10); khi chọn chỉ cần cấu hình mẫu URL, không đổi mã.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// UserAgent cố định của yêu cầu đi ra — không mang phiên bản app, thiết bị hay người dùng.
const UserAgent = "agrigreen-extdata/1"

// Giới hạn thân trả về.
const (
	MaxPointBytes = 1 << 20 // 1 MiB
	MaxTileBytes  = 2 << 20 // 2 MiB
)

// ErrUpstream là lỗi gốc khi nhà cung cấp không trả được dữ liệu hợp lệ.
var ErrUpstream = errors.New("extdata: nhà cung cấp lỗi")

var tileTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/webp": true,
	"application/x-protobuf": true, "application/vnd.mapbox-vector-tile": true,
}

// HTTP là nhà cung cấp qua một mẫu URL.
type HTTP struct {
	Template string
	Client   *http.Client
}

// NewHTTP dựng nhà cung cấp HTTP; client nil → client riêng hết hạn 10 giây. Không theo
// chuyển hướng, không cookie.
func NewHTTP(template string, client *http.Client) *HTTP {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	c := *client
	c.Jar = nil
	c.CheckRedirect = noRedirect
	return &HTTP{Template: template, Client: &c}
}

// noRedirect: không theo chuyển hướng nào — nhà cung cấp phải trả thẳng 200. Theo chuyển
// hướng có thể đưa yêu cầu sang máy chủ ngoài danh mục (RB-7) hoặc làm lộ header.
func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

func fixedHeaders(accept string) http.Header {
	h := http.Header{}
	h.Set("User-Agent", UserAgent)
	if accept != "" {
		h.Set("Accept", accept)
	}
	return h
}

// Expand thay chỗ trong mẫu bằng giá trị đã escape. Chỗ lạ giữ nguyên.
func Expand(template string, vals map[string]string) string {
	out := template
	for k, v := range vals {
		out = strings.ReplaceAll(out, "{"+k+"}", url.QueryEscape(v))
	}
	return out
}

func (p *HTTP) get(ctx context.Context, target, accept string, limit int64) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, "", fmt.Errorf("%w: mẫu URL", ErrUpstream)
	}
	req.Header = fixedHeaders(accept)
	resp, err := p.Client.Do(req)
	if err != nil {
		// Không đưa URL vào lỗi: mẫu có thể chứa khóa API.
		return nil, "", fmt.Errorf("%w: không kết nối được", ErrUpstream)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%w: HTTP %d", ErrUpstream, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, "", fmt.Errorf("%w: đọc thân", ErrUpstream)
	}
	if int64(len(body)) > limit {
		return nil, "", fmt.Errorf("%w: thân vượt %d byte", ErrUpstream, limit)
	}
	ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	return body, ct, nil
}

// Point thực hiện port.PointProvider: {lat} {lon} là TÂM ô, {cell} là cạnh ô.
func (p *HTTP) Point(ctx context.Context, q port.PointQuery) (json.RawMessage, error) {
	target := Expand(p.Template, map[string]string{
		"lat": q.Cell.Lat.String(), "lon": q.Cell.Lon.String(), "cell": q.Cell.SizeDeg.String(),
	})
	body, _, err := p.get(ctx, target, "application/json", MaxPointBytes)
	if err != nil {
		return nil, err
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("%w: thân không phải JSON", ErrUpstream)
	}
	return json.RawMessage(bytes.TrimSpace(body)), nil
}

// Tile thực hiện port.TileProvider.
func (p *HTTP) Tile(ctx context.Context, q port.TileQuery) (port.Tile, error) {
	target := Expand(p.Template, map[string]string{"z": strconv.Itoa(q.Z), "x": strconv.Itoa(q.X), "y": strconv.Itoa(q.Y)})
	body, ct, err := p.get(ctx, target, "image/png,image/jpeg,image/webp,application/x-protobuf", MaxTileBytes)
	if err != nil {
		return port.Tile{}, err
	}
	if !tileTypes[ct] {
		return port.Tile{}, fmt.Errorf("%w: loại nội dung tile %q không được phép", ErrUpstream, ct)
	}
	return port.Tile{Body: body, ContentType: ct}, nil
}

// Fake là nhà cung cấp giả tất định (Local/Dev, kiểm thử). An toàn khi dùng đồng thời.
type Fake struct {
	mu     sync.Mutex
	Points []port.PointQuery
	Tiles  []port.TileQuery
	// Identified: có lần gọi mà context còn mang Principal, token hay tenant của người gọi
	// — kiểm thử chứng minh proxy đã gỡ định danh trước khi gọi nhà cung cấp.
	Identified bool
	// Fail: khác nil → mọi lần gọi trả lỗi này.
	Fail error
}

// PNG1x1 là ảnh PNG 1×1 trong suốt mà Fake trả cho tile.
var PNG1x1 = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
	0x42, 0x60, 0x82,
}

// Point trả dữ liệu giả theo ô.
func (f *Fake) Point(ctx context.Context, q port.PointQuery) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Points = append(f.Points, q)
	f.Identified = f.Identified || identified(ctx)
	if f.Fail != nil {
		return nil, f.Fail
	}
	return json.Marshal(map[string]any{
		"provider": "fake", "source": q.Source, "lat": q.Cell.Lat.String(), "lon": q.Cell.Lon.String(),
		"note": "Dữ liệu giả — nhà cung cấp trong lãnh thổ chờ khách chọn (ADR-0007)",
	})
}

// Tile trả PNG 1×1.
func (f *Fake) Tile(ctx context.Context, q port.TileQuery) (port.Tile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Tiles = append(f.Tiles, q)
	f.Identified = f.Identified || identified(ctx)
	if f.Fail != nil {
		return port.Tile{}, f.Fail
	}
	return port.Tile{Body: PNG1x1, ContentType: "image/png"}, nil
}

func identified(ctx context.Context) bool {
	if _, ok := authz.FromContext(ctx); ok {
		return true
	}
	if _, ok := authz.RawToken(ctx); ok {
		return true
	}
	_, err := tenant.FromContext(ctx)
	return err == nil
}

// Calls trả số lần gọi (điểm, tile).
func (f *Fake) Calls() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Points), len(f.Tiles)
}
