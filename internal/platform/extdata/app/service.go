// Package app: use case của pf/extdata — proxy khử định danh (ADR-0007, RB-7).
//
// Thứ tự luôn là: kiểm người gọi đã xác thực → tra nguồn trong danh mục đã bật
// (mặc định chặn) → làm thô tọa độ / kiểm zoom và vùng → tra bộ nhớ đệm theo ô →
// gọi nhà cung cấp bằng context ĐÃ GỠ mọi giá trị (không Principal, token, tenant).
// Nhật ký chỉ ghi mã nguồn và kết quả, không ghi tọa độ của người gọi.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"time"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extdata/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Mã lỗi của module (docs/errors.md, mục pf/extdata).
var (
	ErrUnknownSource       = problem.New(404, "extdata.unknown_source", "Nguồn dữ liệu ngoài chưa được bật")
	ErrOutsideTerritory    = problem.New(422, "extdata.outside_territory", "Tọa độ ngoài vùng phục vụ")
	ErrZoomTooHigh         = problem.New(422, "extdata.zoom_too_high", "Zoom vượt giới hạn của nguồn")
	ErrProviderUnavailable = problem.New(502, "extdata.provider_unavailable", "Nhà cung cấp dữ liệu ngoài không phản hồi")
)

// UpstreamTimeout là hạn chờ mỗi lần gọi nhà cung cấp.
const UpstreamTimeout = 8 * time.Second

// Source là một nguồn đã bật: mô tả + nhà cung cấp theo loại.
type Source struct {
	Info  port.SourceInfo
	Point port.PointProvider // loại weather/salinity
	Tile  port.TileProvider  // loại tile
}

// Service là proxy.
type Service struct {
	Clock   clock.Clock
	Log     *slog.Logger
	sources map[string]Source
	cache   *Cache
}

// NewService dựng proxy với danh mục nguồn đã kiểm (Config.Sources).
func NewService(clk clock.Clock, log *slog.Logger, sources []Source, cacheEntries int) (*Service, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	m := map[string]Source{}
	for _, s := range sources {
		if _, dup := m[s.Info.ID]; dup {
			return nil, fmt.Errorf("extdata: nguồn %q khai hai lần", s.Info.ID)
		}
		switch {
		case s.Info.Kind.Point() && s.Point == nil, s.Info.Kind == port.KindTile && s.Tile == nil, !s.Info.Kind.Valid():
			return nil, fmt.Errorf("extdata: nguồn %q thiếu nhà cung cấp cho loại %q", s.Info.ID, s.Info.Kind)
		}
		m[s.Info.ID] = s
	}
	return &Service{Clock: clk, Log: log, sources: m, cache: NewCache(cacheEntries)}, nil
}

// Sources liệt kê nguồn đã bật (không URL), sắp theo mã.
func (s *Service) Sources(ctx context.Context) ([]port.SourceInfo, error) {
	if _, ok := authz.FromContext(ctx); !ok {
		return nil, problem.Unauthenticated
	}
	out := make([]port.SourceInfo, 0, len(s.sources))
	for _, src := range s.sources {
		out = append(out, src.Info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// PointAnswer là kết quả dạng điểm.
type PointAnswer struct {
	Source    port.SourceInfo
	Cell      port.Cell
	Data      json.RawMessage
	FetchedAt time.Time
	Expires   time.Time
	Cached    bool
}

// TileAnswer là kết quả tile.
type TileAnswer struct {
	Tile      port.Tile
	FetchedAt time.Time
	Expires   time.Time
	Cached    bool
}

func (s *Service) source(id string, point bool) (Source, error) {
	src, ok := s.sources[id]
	if !ok || src.Info.Kind.Point() != point {
		return Source{}, ErrUnknownSource
	}
	return src, nil
}

// Point trả dữ liệu của ô lưới chứa (lat, lon). lat/lon là chuỗi thập phân.
func (s *Service) Point(ctx context.Context, sourceID, lat, lon string) (PointAnswer, error) {
	if _, ok := authz.FromContext(ctx); !ok {
		return PointAnswer{}, problem.Unauthenticated
	}
	src, err := s.source(sourceID, true)
	if err != nil {
		return PointAnswer{}, err
	}
	la, e1 := decimal.NewFromString(lat)
	lo, e2 := decimal.NewFromString(lon)
	if e1 != nil || e2 != nil {
		p := problem.Validation
		if e1 != nil {
			p = p.WithField("lat", "phải là số thập phân")
		}
		if e2 != nil {
			p = p.WithField("lon", "phải là số thập phân")
		}
		return PointAnswer{}, p
	}
	cell, err := domain.Snap(la, lo, src.Info.CellDeg)
	if errors.Is(err, domain.ErrOutsideTerritory) {
		return PointAnswer{}, ErrOutsideTerritory
	}
	if err != nil {
		return PointAnswer{}, err
	}
	key := "p|" + src.Info.ID + "|" + cell.Lat.String() + "|" + cell.Lon.String()
	now := s.Clock.Now()
	if e, ok := s.cache.Get(key, now); ok {
		return PointAnswer{Source: src.Info, Cell: cell, Data: e.Data, FetchedAt: e.FetchedAt, Expires: e.Expires, Cached: true}, nil
	}
	uctx, cancel := context.WithTimeout(Detached(ctx), UpstreamTimeout)
	defer cancel()
	data, err := src.Point.Point(uctx, port.PointQuery{Source: src.Info.ID, Cell: cell})
	if err != nil {
		s.Log.Warn("nguồn ngoài lỗi", "event", "extdata.point", "source", src.Info.ID, "outcome", "error", "error", err.Error())
		return PointAnswer{}, ErrProviderUnavailable
	}
	e := Entry{Data: data, FetchedAt: now, Expires: now.Add(src.Info.TTL)}
	s.cache.Put(key, e)
	s.Log.Info("lấy dữ liệu ngoài", "event", "extdata.point", "source", src.Info.ID, "outcome", "ok")
	return PointAnswer{Source: src.Info, Cell: cell, Data: data, FetchedAt: now, Expires: e.Expires}, nil
}

// Tile trả tile z/x/y của nguồn tile.
func (s *Service) Tile(ctx context.Context, sourceID string, z, x, y int) (TileAnswer, error) {
	if _, ok := authz.FromContext(ctx); !ok {
		return TileAnswer{}, problem.Unauthenticated
	}
	src, err := s.source(sourceID, false)
	if err != nil {
		return TileAnswer{}, err
	}
	switch err := domain.CheckTile(z, x, y, src.Info.MaxZoom); {
	case errors.Is(err, domain.ErrZoomTooHigh):
		return TileAnswer{}, ErrZoomTooHigh.With("max_zoom", src.Info.MaxZoom)
	case errors.Is(err, domain.ErrOutsideTerritory):
		return TileAnswer{}, ErrOutsideTerritory
	case err != nil:
		return TileAnswer{}, problem.Validation.WithField("tile", "z/x/y ngoài lưới")
	}
	key := "t|" + src.Info.ID + "|" + strconv.Itoa(z) + "/" + strconv.Itoa(x) + "/" + strconv.Itoa(y)
	now := s.Clock.Now()
	if e, ok := s.cache.Get(key, now); ok {
		return TileAnswer{Tile: port.Tile{Body: e.Data, ContentType: e.ContentType}, FetchedAt: e.FetchedAt, Expires: e.Expires, Cached: true}, nil
	}
	uctx, cancel := context.WithTimeout(Detached(ctx), UpstreamTimeout)
	defer cancel()
	t, err := src.Tile.Tile(uctx, port.TileQuery{Source: src.Info.ID, Z: z, X: x, Y: y})
	if err != nil {
		s.Log.Warn("nguồn tile lỗi", "event", "extdata.tile", "source", src.Info.ID, "outcome", "error", "error", err.Error())
		return TileAnswer{}, ErrProviderUnavailable
	}
	e := Entry{Data: t.Body, ContentType: t.ContentType, FetchedAt: now, Expires: now.Add(src.Info.TTL)}
	s.cache.Put(key, e)
	return TileAnswer{Tile: t, FetchedAt: now, Expires: e.Expires}, nil
}

// Detached trả context giữ hạn chờ/hủy của ctx nhưng KHÔNG mang giá trị nào (Principal,
// token thô, tenant, trace…) — để nhà cung cấp ngoài không thể đọc định danh (ADR-0007).
func Detached(ctx context.Context) context.Context { return bare{ctx} }

type bare struct{ parent context.Context }

func (b bare) Deadline() (time.Time, bool) { return b.parent.Deadline() }
func (b bare) Done() <-chan struct{}       { return b.parent.Done() }
func (b bare) Err() error                  { return b.parent.Err() }
func (b bare) Value(any) any               { return nil }
