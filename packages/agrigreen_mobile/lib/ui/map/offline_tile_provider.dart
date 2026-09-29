import 'dart:ui' as ui;

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_map/flutter_map.dart';
import 'package:http/http.dart' as http;

import '../../data/map/tile_store.dart';
import '../../domain/geo/tiles.dart';

/// Nguồn tile "ngoại tuyến trước": đọc gói đã tải trong CSDL SQLCipher; không
/// có thì tải từ máy chủ tile cấu hình (khi có mạng). Tile tải trực tuyến
/// không được ghi đệm xuống đĩa.
class OfflineFirstTileProvider extends TileProvider {
  OfflineFirstTileProvider({
    required this.store,
    required this.urlTemplate,
    http.Client? client,
    this.userAgent = 'vn.agrigreen.field',
    this.fetchRemote,
  }) : _client = client ?? http.Client();

  final TileStore? store;
  final String urlTemplate;
  final String userAgent;
  final http.Client _client;

  /// Thay cách tải tile trực tuyến (kiểm thử); null = HTTP theo [urlTemplate].
  final Future<Uint8List> Function(TileId t)? fetchRemote;

  @override
  ImageProvider getImage(TileCoordinates coordinates, TileLayer options) =>
      StoredTileImage(
        TileId(coordinates.z, coordinates.x, coordinates.y),
        load: _bytes,
      );

  Future<Uint8List> _bytes(TileId t) async {
    final local = await store?.tile(t);
    if (local != null) return local;
    final remote = fetchRemote;
    if (remote != null) return remote(t);
    final res = await _client
        .get(
          Uri.parse(tileUrl(urlTemplate, t)),
          headers: {'User-Agent': userAgent},
        )
        .timeout(const Duration(seconds: 20));
    if (res.statusCode != 200 || res.bodyBytes.isEmpty) {
      throw StateError('tile ${res.statusCode}');
    }
    return res.bodyBytes;
  }

  @override
  void dispose() {
    _client.close();
    super.dispose();
  }
}

/// ImageProvider cho một tile, bytes lấy bất đồng bộ từ [load].
@immutable
class StoredTileImage extends ImageProvider<StoredTileImage> {
  const StoredTileImage(this.tile, {required this.load});
  final TileId tile;
  final Future<Uint8List> Function(TileId t) load;

  @override
  Future<StoredTileImage> obtainKey(ImageConfiguration configuration) =>
      SynchronousFuture<StoredTileImage>(this);

  @override
  ImageStreamCompleter loadImage(
    StoredTileImage key,
    ImageDecoderCallback decode,
  ) => MultiFrameImageStreamCompleter(codec: _codec(decode), scale: 1);

  Future<ui.Codec> _codec(ImageDecoderCallback decode) async {
    final bytes = await load(tile);
    return decode(await ui.ImmutableBuffer.fromUint8List(bytes));
  }

  @override
  bool operator ==(Object other) =>
      other is StoredTileImage && other.tile == tile;

  @override
  int get hashCode => tile.hashCode;
}
