package app

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extdata/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/residency"
)

// Biến môi trường (danh mục nhà cung cấp, ADR-0007):
//
//	AG_EXTDATA_SOURCES          mã nguồn bật, phân tách dấu phẩy (rỗng → không nguồn nào; mặc định chặn)
//	AG_EXTDATA_<ID>_URL         mẫu URL ({lat} {lon} {cell} | {z} {x} {y}) hoặc "fake" (chỉ ngoài production)
//	AG_EXTDATA_<ID>_KIND        weather | salinity | tile
//	AG_EXTDATA_<ID>_ADR         ADR riêng của nguồn ngoài lãnh thổ (vd ADR-0021) — bắt buộc ở production
//	AG_EXTDATA_<ID>_CELL_DEG    cạnh ô lưới (độ), 0.05–1, mặc định 0.1
//	AG_EXTDATA_<ID>_MAX_ZOOM    trần zoom tile, 0–18, mặc định 16
//	AG_EXTDATA_<ID>_TTL         hạn đệm (Go duration, ≥ 1m), mặc định 30m (điểm) / 24h (tile)
//
// Tên biến _URL kết thúc bằng _URL nên residency.Check (RB-7) kiểm cả đích của nguồn.
const (
	EnvSources = "AG_EXTDATA_SOURCES"
	envPrefix  = "AG_EXTDATA_"
	// FakeURL là giá trị _URL chọn nhà cung cấp giả.
	FakeURL = "fake"
)

var (
	sourceID = regexp.MustCompile(`^[a-z][a-z0-9_]{1,30}$`)
	adrID    = regexp.MustCompile(`^ADR-\d{4}$`)
)

// ErrConfig là lỗi gốc của cấu hình sai.
var ErrConfig = errors.New("extdata: cấu hình nguồn ngoài sai")

// SourceConfig là cấu hình đã kiểm của một nguồn.
type SourceConfig struct {
	Info port.SourceInfo
	URL  string // mẫu URL; rỗng khi Info.Fake
}

// LoadConfig đọc danh mục nguồn. Ở production (AG_ENV=production):
//   - không nhận nhà cung cấp giả;
//   - đích phải thuộc AG_RESIDENCY_ALLOWLIST (trong lãnh thổ), HOẶC thuộc
//     AG_NONPII_ALLOWLIST và có AG_EXTDATA_<ID>_ADR (nguồn ngoài lãnh thổ cần ADR riêng).
func LoadConfig(getenv func(string) string) ([]SourceConfig, error) {
	prod := residency.Production(getenv)
	inTerr, err := residency.ParseList(getenv(residency.EnvAllowlist))
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrConfig, residency.EnvAllowlist, err)
	}
	nonPII, err := residency.ParseList(getenv(residency.EnvNonPIIAllow))
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrConfig, residency.EnvNonPIIAllow, err)
	}
	var out []SourceConfig
	seen := map[string]bool{}
	for _, raw := range strings.Split(getenv(EnvSources), ",") {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if !sourceID.MatchString(id) {
			return nil, fmt.Errorf("%w: mã nguồn %q phải dạng [a-z][a-z0-9_]{1,30}", ErrConfig, id)
		}
		if seen[id] {
			return nil, fmt.Errorf("%w: nguồn %q khai hai lần", ErrConfig, id)
		}
		seen[id] = true
		c, err := loadOne(getenv, id, prod, inTerr, nonPII)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func loadOne(getenv func(string) string, id string, prod bool, inTerr, nonPII residency.List) (SourceConfig, error) {
	up := envPrefix + strings.ToUpper(id) + "_"
	get := func(k string) string { return strings.TrimSpace(getenv(up + k)) }
	info := port.SourceInfo{ID: id, Kind: port.Kind(get("KIND")), ADR: get("ADR"), CellDeg: domain.DefaultCellDeg, MaxZoom: domain.DefaultTileZoom}
	if !info.Kind.Valid() {
		return SourceConfig{}, fmt.Errorf("%w: %sKIND phải là weather, salinity hoặc tile", ErrConfig, up)
	}
	if info.ADR != "" && !adrID.MatchString(info.ADR) {
		return SourceConfig{}, fmt.Errorf("%w: %sADR phải dạng ADR-NNNN", ErrConfig, up)
	}
	if v := get("CELL_DEG"); v != "" {
		d, err := decimal.NewFromString(v)
		if err != nil || domain.ValidCellDeg(d) != nil {
			return SourceConfig{}, fmt.Errorf("%w: %sCELL_DEG phải trong [0.05, 1]", ErrConfig, up)
		}
		info.CellDeg = d
	}
	if v := get("MAX_ZOOM"); v != "" {
		z, err := strconv.Atoi(v)
		if err != nil || z < 0 || z > domain.MaxTileZoom {
			return SourceConfig{}, fmt.Errorf("%w: %sMAX_ZOOM phải trong [0, %d]", ErrConfig, up, domain.MaxTileZoom)
		}
		info.MaxZoom = z
	}
	info.TTL = 30 * time.Minute
	if info.Kind == port.KindTile {
		info.TTL = 24 * time.Hour
	}
	if v := get("TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < time.Minute {
			return SourceConfig{}, fmt.Errorf("%w: %sTTL phải là thời lượng ≥ 1m", ErrConfig, up)
		}
		info.TTL = d
	}
	u := get("URL")
	if u == "" {
		return SourceConfig{}, fmt.Errorf("%w: thiếu %sURL", ErrConfig, up)
	}
	if u == FakeURL {
		if prod {
			return SourceConfig{}, fmt.Errorf("%w: RB-7: nhà cung cấp giả không dùng ở production (%sURL)", ErrConfig, up)
		}
		info.Fake = true
		return SourceConfig{Info: info}, nil
	}
	if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
		return SourceConfig{}, fmt.Errorf("%w: %sURL phải là http(s)://", ErrConfig, up)
	}
	if prod && !strings.HasPrefix(u, "https://") {
		return SourceConfig{}, fmt.Errorf("%w: %sURL phải là https ở production", ErrConfig, up)
	}
	hosts, err := residency.Hosts(u)
	if err != nil || len(hosts) != 1 {
		return SourceConfig{}, fmt.Errorf("%w: %sURL không đọc được máy chủ", ErrConfig, up)
	}
	info.InTerritory = inTerr.Match(hosts[0])
	if prod && !info.InTerritory {
		switch {
		case !nonPII.Match(hosts[0]):
			return SourceConfig{}, fmt.Errorf("%w: RB-7: %sURL → %s ngoài danh mục lãnh thổ và AG_NONPII_ALLOWLIST", ErrConfig, up, hosts[0])
		case info.ADR == "":
			return SourceConfig{}, fmt.Errorf("%w: ADR-0007: nguồn ngoài lãnh thổ %s cần ADR riêng (%sADR)", ErrConfig, hosts[0], up)
		}
	}
	return SourceConfig{Info: info, URL: u}, nil
}
