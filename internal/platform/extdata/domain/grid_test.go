package domain

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// ADR-0007: tọa độ làm thô theo lưới — mọi điểm trong cùng ô 0,1° cho cùng tâm ô.
func TestSnapToCoarseGrid_ADR0007(t *testing.T) {
	a, err := Snap(d("10.0452"), d("105.7701"), DefaultCellDeg)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Snap(d("10.0999"), d("105.7002"), DefaultCellDeg)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Lat.Equal(d("10.05")) || !a.Lon.Equal(d("105.75")) {
		t.Fatalf("tâm ô = %s,%s; muốn 10.05,105.75", a.Lat, a.Lon)
	}
	if !a.Lat.Equal(b.Lat) || !a.Lon.Equal(b.Lon) {
		t.Fatalf("hai điểm cùng ô phải cho cùng tâm: %v vs %v", a, b)
	}
	if c, _ := Snap(d("10.1"), d("105.8"), DefaultCellDeg); c.Lat.Equal(a.Lat) || c.Lon.Equal(a.Lon) {
		t.Fatalf("điểm ở ô kế bên phải khác tâm: %v", c)
	}
}

// ADR-0007: ô lưới không nhỏ hơn 0,05° (không lộ vị trí thửa); ngoài vùng phục vụ → từ chối.
func TestSnapRejectsFineGridAndOutsideTerritory_ADR0007(t *testing.T) {
	if _, err := Snap(d("10.0"), d("105.7"), d("0.01")); !errors.Is(err, ErrInvalidCell) {
		t.Fatalf("ô 0,01° phải bị từ chối: %v", err)
	}
	if _, err := Snap(d("48.85"), d("2.35"), DefaultCellDeg); !errors.Is(err, ErrOutsideTerritory) {
		t.Fatalf("Paris phải ngoài vùng phục vụ: %v", err)
	}
}

// ADR-0007: tile qua proxy bị giới hạn zoom theo nguồn và phải giao vùng phục vụ.
func TestCheckTileZoomAndTerritory_ADR0007(t *testing.T) {
	// z=10 tile chứa Cần Thơ (10.03, 105.78): x = 812, y = 483.
	if err := CheckTile(10, 812, 483, 16); err != nil {
		t.Fatalf("tile Cần Thơ phải hợp lệ: %v", err)
	}
	if err := CheckTile(17, 103993, 61837, 16); !errors.Is(err, ErrZoomTooHigh) {
		t.Fatalf("zoom 17 > trần 16 phải bị chặn: %v", err)
	}
	if err := CheckTile(10, 0, 0, 16); !errors.Is(err, ErrOutsideTerritory) {
		t.Fatalf("tile ngoài vùng phải bị chặn: %v", err)
	}
	if err := CheckTile(3, 8, 0, 16); !errors.Is(err, ErrInvalidTile) {
		t.Fatalf("x ngoài lưới phải bị chặn: %v", err)
	}
	if err := CheckTile(0, 0, 0, 16); err != nil {
		t.Fatalf("tile z=0 phủ toàn cầu, giao vùng phục vụ: %v", err)
	}
}
