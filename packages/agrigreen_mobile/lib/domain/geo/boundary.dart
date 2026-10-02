import 'dart:math';

/// Một điểm WGS84 (EPSG:4326).
class GeoPoint {
  const GeoPoint(this.lat, this.lon);
  final double lat;
  final double lon;

  @override
  bool operator ==(Object other) =>
      other is GeoPoint && other.lat == lat && other.lon == lon;
  @override
  int get hashCode => Object.hash(lat, lon);
  @override
  String toString() => 'GeoPoint($lat, $lon)';
}

enum BoundaryError {
  tooFewPoints,
  collinear,
  selfIntersecting,
  outOfRange;

  String get message => switch (this) {
    BoundaryError.tooFewPoints || BoundaryError.collinear =>
      'Cần tối thiểu 4 điểm ranh giới không thẳng hàng để khép vùng.',
    BoundaryError.selfIntersecting =>
      'Ranh giới tự cắt nhau. Vui lòng sắp lại thứ tự các điểm.',
    BoundaryError.outOfRange => 'Tọa độ không hợp lệ.',
  };
}

class BoundaryValidation {
  const BoundaryValidation(this.errors, this.areaHa, this.ring);
  final List<BoundaryError> errors;

  /// Diện tích (ha) theo xấp xỉ trắc địa trên cầu WGS84.
  final double areaHa;

  /// Vòng đã chuẩn hóa (bỏ điểm trùng liên tiếp, bỏ điểm khép cuối).
  final List<GeoPoint> ring;
  bool get ok => errors.isEmpty;
}

/// Bán kính trục lớn WGS84, dùng trong công thức diện tích cầu.
const double _earthRadiusM = 6378137.0;

double _rad(double d) => d * pi / 180.0;

/// Chuẩn hóa: bỏ điểm trùng liên tiếp và điểm khép (điểm cuối = điểm đầu).
List<GeoPoint> normalizeRing(List<GeoPoint> pts) {
  final out = <GeoPoint>[];
  for (final p in pts) {
    if (out.isEmpty || out.last != p) out.add(p);
  }
  while (out.length > 1 && out.first == out.last) {
    out.removeLast();
  }
  return out;
}

/// Diện tích mét vuông của đa giác trên mặt cầu (công thức "spherical excess"
/// dạng gần đúng của Chamberlain & Duquette 2007 — cùng cách với Turf/Leaflet).
/// Sai số < 0,5 % với thửa ruộng vài ha ở vĩ độ Việt Nam.
double sphericalAreaM2(List<GeoPoint> ring) {
  final n = ring.length;
  if (n < 3) return 0;
  var total = 0.0;
  for (var i = 0; i < n; i++) {
    final p1 = ring[i];
    final p2 = ring[(i + 1) % n];
    total +=
        _rad(p2.lon - p1.lon) * (2 + sin(_rad(p1.lat)) + sin(_rad(p2.lat)));
  }
  return (total * _earthRadiusM * _earthRadiusM / 2).abs();
}

double _cross(GeoPoint o, GeoPoint a, GeoPoint b) =>
    (a.lon - o.lon) * (b.lat - o.lat) - (a.lat - o.lat) * (b.lon - o.lon);

bool _onSegment(GeoPoint p, GeoPoint q, GeoPoint r) =>
    min(p.lon, r.lon) <= q.lon &&
    q.lon <= max(p.lon, r.lon) &&
    min(p.lat, r.lat) <= q.lat &&
    q.lat <= max(p.lat, r.lat);

/// Hai đoạn thẳng có cắt nhau không (kể cả chạm) — mặt phẳng lon/lat, đủ
/// chính xác cho quy mô thửa ruộng.
bool segmentsIntersect(GeoPoint p1, GeoPoint p2, GeoPoint p3, GeoPoint p4) {
  const eps = 1e-15;
  final d1 = _cross(p3, p4, p1);
  final d2 = _cross(p3, p4, p2);
  final d3 = _cross(p1, p2, p3);
  final d4 = _cross(p1, p2, p4);
  if (((d1 > eps && d2 < -eps) || (d1 < -eps && d2 > eps)) &&
      ((d3 > eps && d4 < -eps) || (d3 < -eps && d4 > eps))) {
    return true;
  }
  if (d1.abs() <= eps && _onSegment(p3, p1, p4)) return true;
  if (d2.abs() <= eps && _onSegment(p3, p2, p4)) return true;
  if (d3.abs() <= eps && _onSegment(p1, p3, p2)) return true;
  if (d4.abs() <= eps && _onSegment(p1, p4, p2)) return true;
  return false;
}

/// Vòng có tự cắt không (so mọi cặp cạnh không kề nhau).
bool isSelfIntersecting(List<GeoPoint> ring) {
  final n = ring.length;
  if (n < 4) return false;
  for (var i = 0; i < n; i++) {
    final a1 = ring[i];
    final a2 = ring[(i + 1) % n];
    for (var j = i + 1; j < n; j++) {
      // bỏ cạnh kề (chung đỉnh)
      if (j == i || (j + 1) % n == i || (i + 1) % n == j) continue;
      final b1 = ring[j];
      final b2 = ring[(j + 1) % n];
      if (segmentsIntersect(a1, a2, b1, b2)) return true;
    }
  }
  return false;
}

/// Tất cả điểm thẳng hàng (diện tích xấp xỉ 0 so với kích thước vòng).
bool isCollinear(List<GeoPoint> ring) {
  if (ring.length < 3) return true;
  final o = ring.first;
  var maxD2 = 0.0;
  for (final p in ring) {
    final dx = p.lon - o.lon, dy = p.lat - o.lat;
    maxD2 = max(maxD2, dx * dx + dy * dy);
  }
  if (maxD2 == 0) return true;
  for (var i = 1; i < ring.length; i++) {
    for (var j = i + 1; j < ring.length; j++) {
      // |cross| / |d|² > ngưỡng tương đối → có điểm lệch khỏi đường thẳng
      if (_cross(o, ring[i], ring[j]).abs() / maxD2 > 1e-9) return false;
    }
  }
  return true;
}

/// Kiểm tra ranh giới thửa: ≥ 4 điểm không thẳng hàng (US-PLOT-01, HTX
/// US-PLOT-02 AC-2), không tự cắt; tính diện tích ha.
BoundaryValidation validateBoundary(List<GeoPoint> points) {
  final ring = normalizeRing(points);
  final errors = <BoundaryError>[];
  if (ring.any((p) => p.lat.abs() > 90 || p.lon.abs() > 180)) {
    errors.add(BoundaryError.outOfRange);
  }
  if (ring.toSet().length < 4) {
    errors.add(BoundaryError.tooFewPoints);
  } else if (isCollinear(ring)) {
    errors.add(BoundaryError.collinear);
  } else if (isSelfIntersecting(ring)) {
    errors.add(BoundaryError.selfIntersecting);
  }
  final area = errors.isEmpty ? sphericalAreaM2(ring) / 10000.0 : 0.0;
  return BoundaryValidation(errors, area, ring);
}

/// Điểm có nằm trong đa giác không (ray casting).
bool pointInPolygon(GeoPoint p, List<GeoPoint> ring) {
  var inside = false;
  for (var i = 0, j = ring.length - 1; i < ring.length; j = i++) {
    final a = ring[i], b = ring[j];
    if ((a.lat > p.lat) != (b.lat > p.lat) &&
        p.lon < (b.lon - a.lon) * (p.lat - a.lat) / (b.lat - a.lat) + a.lon) {
      inside = !inside;
    }
  }
  return inside;
}

/// Hai đa giác có phần chung (cạnh cắt nhau hoặc một cái nằm trong cái kia).
/// Dùng cho cảnh báo chồng lấn trên máy (US-PLOT-03/04); máy chủ vẫn kiểm
/// lại bằng PostGIS và trả `conflict` khi cần.
bool polygonsOverlap(List<GeoPoint> a, List<GeoPoint> b) {
  if (a.length < 3 || b.length < 3) return false;
  for (var i = 0; i < a.length; i++) {
    for (var j = 0; j < b.length; j++) {
      if (segmentsIntersect(
        a[i],
        a[(i + 1) % a.length],
        b[j],
        b[(j + 1) % b.length],
      )) {
        return true;
      }
    }
  }
  return pointInPolygon(a.first, b) || pointInPolygon(b.first, a);
}

/// Hai định dạng số trong payload: diện tích gửi dạng chuỗi 4 chữ số thập
/// phân (máy chủ lưu numeric(14,4), không float — g1-conventions §3).
String formatHa(double ha) => ha.toStringAsFixed(4);

/// GeoJSON Polygon (vòng khép kín, thứ tự [lon, lat]).
Map<String, Object?> toGeoJsonPolygon(List<GeoPoint> ring) {
  final r = normalizeRing(ring);
  return {
    'type': 'Polygon',
    'coordinates': [
      [
        for (final p in [...r, r.first]) [p.lon, p.lat],
      ],
    ],
  };
}

/// Đọc vòng ngoài từ GeoJSON Polygon/MultiPolygon (dữ liệu thửa từ pull).
List<GeoPoint>? ringFromGeoJson(Object? g) {
  if (g is! Map) return null;
  final type = g['type'];
  final coords = g['coordinates'];
  if (coords is! List || coords.isEmpty) return null;
  List<Object?>? outer;
  if (type == 'Polygon') {
    outer = coords.first as List<Object?>?;
  } else if (type == 'MultiPolygon') {
    final first = coords.first;
    if (first is List && first.isNotEmpty) {
      outer = first.first as List<Object?>?;
    }
  }
  if (outer == null) return null;
  final pts = <GeoPoint>[];
  for (final c in outer) {
    if (c is List && c.length >= 2) {
      pts.add(GeoPoint((c[1]! as num).toDouble(), (c[0]! as num).toDouble()));
    }
  }
  return normalizeRing(pts);
}

// ---- KN v7.7 P7 (FN-XA-03.05): vẽ bằng hình tròn / hình vuông ----

/// Cách vẽ thửa (`shape` của pf/geo). Hình tròn / vuông được đổi thành đa
/// giác trên máy trước khi gửi (máy chủ chỉ nhận Polygon).
enum PlotShape {
  polygon('Chấm điểm'),
  circle('Hình tròn'),
  square('Hình vuông');

  const PlotShape(this.label);
  final String label;
}

/// Khoảng cách (m) giữa hai điểm (haversine trên cầu bán kính WGS84).
double distanceM(GeoPoint a, GeoPoint b) {
  final dLat = _rad(b.lat - a.lat);
  final dLon = _rad(b.lon - a.lon);
  final h =
      pow(sin(dLat / 2), 2) +
      cos(_rad(a.lat)) * cos(_rad(b.lat)) * pow(sin(dLon / 2), 2);
  return 2 * _earthRadiusM * asin(min(1.0, sqrt(h)));
}

/// Điểm cách [c] một đoạn ([northM], [eastM]) mét (xấp xỉ mặt phẳng cục bộ —
/// đủ chính xác ở quy mô thửa ruộng).
GeoPoint offsetM(GeoPoint c, double northM, double eastM) {
  final dLat = northM / _earthRadiusM * 180 / pi;
  final dLon = eastM / (_earthRadiusM * cos(_rad(c.lat))) * 180 / pi;
  return GeoPoint(c.lat + dLat, c.lon + dLon);
}

/// Đa giác xấp xỉ hình tròn tâm [center], bán kính [radiusM] ([segments] đỉnh).
List<GeoPoint> circlePolygon(
  GeoPoint center,
  double radiusM, {
  int segments = 32,
}) => [
  for (var i = 0; i < segments; i++)
    offsetM(
      center,
      radiusM * cos(2 * pi * i / segments),
      radiusM * sin(2 * pi * i / segments),
    ),
];

/// Hình vuông tâm [center], một đỉnh tại [corner] (xoay theo hướng đỉnh đó).
List<GeoPoint> squarePolygon(GeoPoint center, GeoPoint corner) {
  // Véc-tơ tâm → đỉnh theo mét (bắc, đông), xoay 90° ba lần.
  final n = (corner.lat - center.lat) * pi / 180 * _earthRadiusM;
  final e =
      (corner.lon - center.lon) *
      pi /
      180 *
      _earthRadiusM *
      cos(_rad(center.lat));
  return [
    offsetM(center, n, e),
    offsetM(center, -e, n),
    offsetM(center, -n, -e),
    offsetM(center, e, -n),
  ];
}
