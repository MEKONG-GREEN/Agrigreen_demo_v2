import 'dart:async';
import 'dart:typed_data';

import 'package:http/http.dart' as http;

import '../core/clock.dart';
import '../core/ids.dart';
import '../data/map/tile_store.dart';
import '../domain/geo/tiles.dart';

/// Giới hạn gói bản đồ ngoại tuyến (US-MAP-03) — giữ máy cấu hình thấp an toàn
/// và tôn trọng chính sách máy chủ tile.
class TilePackLimits {
  const TilePackLimits({
    this.maxTilesPerPack = 4000,
    this.maxZoom = 18,
    this.minZoom = 8,
    this.maxAreaKm2 = 400,
    this.maxTileBytes = 512 * 1024,
    this.maxTotalBytes = 150 * 1024 * 1024,
  });

  final int maxTilesPerPack;
  final int maxZoom;
  final int minZoom;

  /// Diện tích khung tối đa (một xã ĐBSCL thường 20–100 km²).
  final double maxAreaKm2;
  final int maxTileBytes;

  /// Tổng dung lượng mọi gói trên máy.
  final int maxTotalBytes;
}

class TilePackException implements Exception {
  TilePackException(this.message);
  final String message;
  @override
  String toString() => message;
}

class TilePackPlan {
  const TilePackPlan(this.bbox, this.minZoom, this.maxZoom, this.tiles);
  final GeoBBox bbox;
  final int minZoom;
  final int maxZoom;
  final int tiles;
}

class TileDownloadProgress {
  const TileDownloadProgress(this.done, this.total, this.failed);
  final int done;
  final int total;
  final int failed;
}

/// Tải gói tile theo khung + dải zoom từ mẫu URL cấu hình
/// (`AG_TILE_URL`). OSM `tile.openstreetmap.org` **chỉ cho dev** (chính sách
/// OSM cấm tải hàng loạt); production dùng máy chủ tile trong lãnh thổ
/// (ADR-0007). Tile lưu vào [TileStore] (SQLCipher).
class TilePackService {
  TilePackService({
    required TileStore store,
    required String urlTemplate,
    required Clock clock,
    required IdGenerator ids,
    http.Client? client,
    this.limits = const TilePackLimits(),
    this.userAgent = 'vn.agrigreen.field (offline-pack)',
  }) : _store = store,
       _template = urlTemplate,
       _clock = clock,
       _ids = ids,
       _client = client ?? http.Client();

  final TileStore _store;
  final String _template;
  final Clock _clock;
  final IdGenerator _ids;
  final http.Client _client;
  final TilePackLimits limits;
  final String userAgent;

  TileStore get store => _store;

  /// Kiểm khung/zoom theo [limits]; ném [TilePackException] kèm lý do.
  TilePackPlan plan(GeoBBox bbox, int minZoom, int maxZoom) {
    if (!bbox.valid) throw TilePackException('Khung bản đồ không hợp lệ.');
    if (minZoom > maxZoom) {
      throw TilePackException('Mức phóng to nhỏ nhất lớn hơn lớn nhất.');
    }
    if (minZoom < limits.minZoom || maxZoom > limits.maxZoom) {
      throw TilePackException(
        'Mức phóng to phải trong khoảng ${limits.minZoom}–${limits.maxZoom}.',
      );
    }
    if (bbox.areaKm2 > limits.maxAreaKm2) {
      throw TilePackException(
        'Vùng quá rộng (${bbox.areaKm2.round()} km²). Tối đa '
        '${limits.maxAreaKm2.round()} km² mỗi gói — hãy phóng to bản đồ.',
      );
    }
    final n = tileCount(bbox, minZoom, maxZoom);
    if (n > limits.maxTilesPerPack) {
      throw TilePackException(
        'Gói có $n ô bản đồ, vượt giới hạn ${limits.maxTilesPerPack}. '
        'Hãy thu nhỏ vùng hoặc giảm mức phóng to.',
      );
    }
    return TilePackPlan(bbox, minZoom, maxZoom, n);
  }

  /// Tải gói. Tile đã có được bỏ qua (tải tiếp khi gọi lại cùng [packId]).
  Future<TilePack> download(
    TilePackPlan plan, {
    required String name,
    String? packId,
    void Function(TileDownloadProgress p)? onProgress,
    bool Function()? cancelled,
  }) async {
    final used = await _store.totalBytes();
    if (used >= limits.maxTotalBytes) {
      throw TilePackException(
        'Bộ nhớ bản đồ ngoại tuyến đã đầy. Xóa bớt gói cũ trước khi tải.',
      );
    }
    final id = packId ?? _ids.newId();
    if (await _store.pack(id) == null) {
      await _store.createPack(
        id: id,
        name: name,
        bbox: plan.bbox,
        minZoom: plan.minZoom,
        maxZoom: plan.maxZoom,
        now: _clock.now(),
      );
    } else {
      await _store.setStatus(id, 'downloading');
    }
    var done = 0, failed = 0, bytes = used;
    for (final t in tilesFor(plan.bbox, plan.minZoom, plan.maxZoom)) {
      if (cancelled?.call() ?? false) break;
      if (await _store.hasTile(id, t)) {
        done++;
        continue;
      }
      final data = await _fetch(t);
      if (data == null) {
        failed++;
      } else {
        if (bytes + data.length > limits.maxTotalBytes) {
          await _store.setStatus(id, 'failed');
          throw TilePackException(
            'Vượt dung lượng bản đồ ngoại tuyến cho phép '
            '(${limits.maxTotalBytes ~/ (1024 * 1024)} MB).',
          );
        }
        await _store.putTile(id, t, data);
        bytes += data.length;
      }
      done++;
      onProgress?.call(TileDownloadProgress(done, plan.tiles, failed));
    }
    final complete = done == plan.tiles && failed == 0;
    await _store.setStatus(id, complete ? 'ready' : 'failed');
    return (await _store.pack(id))!;
  }

  Future<Uint8List?> _fetch(TileId t) async {
    try {
      final res = await _client
          .get(
            Uri.parse(tileUrl(_template, t)),
            headers: {'User-Agent': userAgent},
          )
          .timeout(const Duration(seconds: 20));
      if (res.statusCode != 200) return null;
      final b = res.bodyBytes;
      if (b.isEmpty || b.length > limits.maxTileBytes) return null;
      if (!_looksLikeImage(b)) return null;
      return b;
    } catch (_) {
      return null;
    }
  }

  static bool _looksLikeImage(Uint8List b) {
    if (b.length < 4) return false;
    final png = b[0] == 0x89 && b[1] == 0x50 && b[2] == 0x4E && b[3] == 0x47;
    final jpg = b[0] == 0xFF && b[1] == 0xD8;
    final webp =
        b.length >= 12 &&
        b[0] == 0x52 &&
        b[1] == 0x49 &&
        b[2] == 0x46 &&
        b[3] == 0x46;
    return png || jpg || webp;
  }
}
