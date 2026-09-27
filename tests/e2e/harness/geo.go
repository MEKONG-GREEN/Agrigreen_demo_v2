package harness

import "math"

// metersPerDeg là số mét của một độ vĩ (gần đúng, đủ cho thửa vài trăm mét).
const metersPerDeg = 111_320.0

// Rect trả geometry GeoJSON Polygon ([lon, lat], khép kín) của hình chữ nhật
// w × h mét, góc tây nam lệch (east, north) mét so với tâm (lat, lon).
// Rect(10.03, 105.77, 0, 0, 100, 100) ≈ 1 ha.
func Rect(lat, lon, east, north, w, h float64) map[string]any {
	dLon := 1 / (metersPerDeg * math.Cos(lat*math.Pi/180))
	dLat := 1 / metersPerDeg
	x0, y0 := lon+east*dLon, lat+north*dLat
	x1, y1 := x0+w*dLon, y0+h*dLat
	ring := [][]float64{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}, {x0, y0}}
	return map[string]any{"type": "Polygon", "coordinates": [][][]float64{ring}}
}
