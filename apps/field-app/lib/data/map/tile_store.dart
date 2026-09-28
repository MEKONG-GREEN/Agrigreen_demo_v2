import 'dart:typed_data';

import 'package:sqflite_common/sqlite_api.dart';

import '../../domain/geo/tiles.dart';
import '../db/local_db.dart';

class TilePack {
  const TilePack({
    required this.id,
    required this.name,
    required this.bbox,
    required this.minZoom,
    required this.maxZoom,
    required this.tileCount,
    required this.bytes,
    required this.status,
    required this.createdAt,
  });

  final String id;
  final String name;
  final GeoBBox bbox;
  final int minZoom;
  final int maxZoom;
  final int tileCount;
  final int bytes;

  /// downloading | ready | failed
  final String status;
  final DateTime createdAt;

  factory TilePack.fromRow(Map<String, Object?> r) => TilePack(
    id: r['id']! as String,
    name: r['name']! as String,
    bbox: GeoBBox(
      minLat: (r['min_lat']! as num).toDouble(),
      minLon: (r['min_lon']! as num).toDouble(),
      maxLat: (r['max_lat']! as num).toDouble(),
      maxLon: (r['max_lon']! as num).toDouble(),
    ),
    minZoom: r['min_zoom']! as int,
    maxZoom: r['max_zoom']! as int,
    tileCount: r['tile_count']! as int,
    bytes: r['bytes']! as int,
    status: r['status']! as String,
    createdAt: DateTime.fromMillisecondsSinceEpoch(r['created_at']! as int),
  );
}

/// Kho tile ngoại tuyến kiểu MBTiles, nằm **trong CSDL SQLCipher** (mã hóa cùng
/// khóa, bị xóa cùng CSDL khi thu hồi thiết bị).
class TileStore {
  TileStore(this._db);
  final LocalDb _db;
  Database get _sql => _db.db;

  Future<void> createPack({
    required String id,
    required String name,
    required GeoBBox bbox,
    required int minZoom,
    required int maxZoom,
    required DateTime now,
  }) => _sql.insert('tile_pack', {
    'id': id,
    'name': name,
    'min_lat': bbox.minLat,
    'min_lon': bbox.minLon,
    'max_lat': bbox.maxLat,
    'max_lon': bbox.maxLon,
    'min_zoom': minZoom,
    'max_zoom': maxZoom,
    'status': 'downloading',
    'created_at': now.millisecondsSinceEpoch,
  });

  Future<void> putTile(String packId, TileId t, Uint8List data) =>
      _sql.transaction((tx) async {
        final prev = await tx.query(
          'map_tile',
          columns: ['length(data) AS n'],
          where: 'pack_id = ? AND z = ? AND x = ? AND y = ?',
          whereArgs: [packId, t.z, t.x, t.y],
        );
        final old = prev.isEmpty ? 0 : (prev.first['n'] as int? ?? 0);
        await tx.insert('map_tile', {
          'pack_id': packId,
          'z': t.z,
          'x': t.x,
          'y': t.y,
          'data': data,
        }, conflictAlgorithm: ConflictAlgorithm.replace);
        await tx.rawUpdate(
          'UPDATE tile_pack SET tile_count = tile_count + ?, bytes = bytes + ? '
          'WHERE id = ?',
          [prev.isEmpty ? 1 : 0, data.length - old, packId],
        );
      });

  Future<bool> hasTile(String packId, TileId t) async {
    final r = await _sql.query(
      'map_tile',
      columns: ['1'],
      where: 'pack_id = ? AND z = ? AND x = ? AND y = ?',
      whereArgs: [packId, t.z, t.x, t.y],
    );
    return r.isNotEmpty;
  }

  /// Tile ở bất kỳ gói nào đã tải.
  Future<Uint8List?> tile(TileId t) async {
    final r = await _sql.query(
      'map_tile',
      columns: ['data'],
      where: 'z = ? AND x = ? AND y = ?',
      whereArgs: [t.z, t.x, t.y],
      limit: 1,
    );
    if (r.isEmpty) return null;
    return r.first['data']! as Uint8List;
  }

  Future<void> setStatus(String packId, String status) => _sql.update(
    'tile_pack',
    {'status': status},
    where: 'id = ?',
    whereArgs: [packId],
  );

  Future<List<TilePack>> packs() async {
    final rows = await _sql.query('tile_pack', orderBy: 'created_at DESC');
    return rows.map(TilePack.fromRow).toList();
  }

  Future<TilePack?> pack(String id) async {
    final rows = await _sql.query(
      'tile_pack',
      where: 'id = ?',
      whereArgs: [id],
    );
    return rows.isEmpty ? null : TilePack.fromRow(rows.first);
  }

  /// Tổng dung lượng tile đã lưu (byte).
  Future<int> totalBytes() async {
    final r = await _sql.rawQuery(
      'SELECT COALESCE(SUM(bytes), 0) AS n FROM tile_pack',
    );
    return (r.first['n'] as int?) ?? 0;
  }

  Future<void> deletePack(String id) => _sql.transaction((tx) async {
    await tx.delete('map_tile', where: 'pack_id = ?', whereArgs: [id]);
    await tx.delete('tile_pack', where: 'id = ?', whereArgs: [id]);
  });
}
