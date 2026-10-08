package app

import "math"

// MaxViewportPlots: GIS BR-25 — tối đa 1.000 polygon mỗi khung nhìn.
const MaxViewportPlots = 1000

// SimplifyTolerance trả dung sai đơn giản hóa (độ) cho mức zoom web-mercator:
// nửa điểm ảnh ở xích đạo. Zoom ≥ 17 (cận cảnh 1:5.000 trở xuống) giữ nguyên hình.
func SimplifyTolerance(zoom int) float64 {
	if zoom >= 17 || zoom < 0 {
		return 0
	}
	return 360.0 / (256.0 * math.Pow(2, float64(zoom))) / 2
}

// BBoxInput là tham số đọc theo khung nhìn.
type BBoxInput struct {
	MinLon, MinLat, MaxLon, MaxLat float64
	Zoom                           int
	Limit                          int
}
