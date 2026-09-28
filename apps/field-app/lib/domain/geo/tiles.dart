import 'dart:math';

/// Ô tile XYZ (Web Mercator, sơ đồ "slippy map" của OSM).
class TileId {
  const TileId(this.z, this.x, this.y);
  final int z;
  final int x;
  final int y;

  @override
  bool operator ==(Object other) =>
      other is TileId && other.z == z && other.x == x && other.y == y;
  @override
  int get hashCode => Object.hash(z, x, y);
  @override
  String toString() => '$z/$x/$y';
}

/// Khung chữ nhật WGS84.
class GeoBBox {
  const GeoBBox({
    required this.minLat,
    required this.minLon,
    required this.maxLat,
    required this.maxLon,
  });
  final double minLat;
  final double minLon;
  final double maxLat;
  final double maxLon;

  bool get valid =>
      minLat < maxLat &&
      minLon < maxLon &&
      minLat >= -maxMercatorLat &&
      maxLat <= maxMercatorLat &&
      minLon >= -180 &&
      maxLon <= 180;

  /// Diện tích gần đúng (km²) — dùng để giới hạn kích thước gói.
  double get areaKm2 {
    const r = 6371.0088;
    final dLon = (maxLon - minLon) * pi / 180;
    return r *
        r *
        dLon *
        (sin(maxLat * pi / 180) - sin(minLat * pi / 180)).abs();
  }
}

const maxMercatorLat = 85.05112878;

int lonToTileX(double lon, int z) {
  final n = 1 << z;
  final x = ((lon + 180) / 360 * n).floor();
  return x.clamp(0, n - 1);
}

int latToTileY(double lat, int z) {
  final n = 1 << z;
  final l = lat.clamp(-maxMercatorLat, maxMercatorLat) * pi / 180;
  final y = ((1 - log(tan(l) + 1 / cos(l)) / pi) / 2 * n).floor();
  return y.clamp(0, n - 1);
}

/// Dải x/y của khung ở mức [z] (bao gồm cả hai đầu).
({int x0, int x1, int y0, int y1}) tileRange(GeoBBox b, int z) => (
  x0: lonToTileX(b.minLon, z),
  x1: lonToTileX(b.maxLon, z),
  // y tăng về phía nam.
  y0: latToTileY(b.maxLat, z),
  y1: latToTileY(b.minLat, z),
);

/// Số tile của khung trong dải mức zoom.
int tileCount(GeoBBox b, int minZoom, int maxZoom) {
  var n = 0;
  for (var z = minZoom; z <= maxZoom; z++) {
    final r = tileRange(b, z);
    n += (r.x1 - r.x0 + 1) * (r.y1 - r.y0 + 1);
  }
  return n;
}

/// Liệt kê tile theo thứ tự zoom tăng dần (tải mức thấp trước).
Iterable<TileId> tilesFor(GeoBBox b, int minZoom, int maxZoom) sync* {
  for (var z = minZoom; z <= maxZoom; z++) {
    final r = tileRange(b, z);
    for (var x = r.x0; x <= r.x1; x++) {
      for (var y = r.y0; y <= r.y1; y++) {
        yield TileId(z, x, y);
      }
    }
  }
}

/// URL tile từ mẫu `{z}/{x}/{y}` (có thể có `{s}` → `a`).
String tileUrl(String template, TileId t) => template
    .replaceAll('{z}', '${t.z}')
    .replaceAll('{x}', '${t.x}')
    .replaceAll('{y}', '${t.y}')
    .replaceAll('{s}', 'a');
