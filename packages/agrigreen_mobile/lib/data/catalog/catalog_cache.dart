import 'dart:convert';

import 'package:sqflite_common/sqlite_api.dart';

import '../../core/clock.dart';
import '../../domain/mrv.dart';
import '../db/local_db.dart';
import '../sync/sync_api.dart';

/// Mục danh mục dùng chung (`CatalogItem` của pf/masterdata).
class CatalogEntry {
  const CatalogEntry(this.code, this.name, [this.attrs = const {}]);
  final String code;
  final String name;
  final Map<String, String> attrs;
}

/// Danh mục masterdata cần cho báo cáo hiện trường (máy chủ tra khi nhận):
/// giống lúa, vụ, sâu bệnh (`masterdata/port`: rice_variety, season, pest).
const reportCatalogs = ['rice_variety', 'season', 'pest'];

/// Bộ đệm danh mục tải qua Application API (bảng `catalog_cache`, SQLCipher).
///
/// Hợp đồng đồng bộ liệt kê entity `catalog_item`, nhưng máy chủ **chưa có**
/// nguồn pull cho nó (không module nào khai báo `catalog_item` trong
/// `Entities()`), và danh mục trường MRV không có entity đồng bộ. Vì vậy app
/// tải khi có mạng (sau mỗi lượt pull) và dùng bản đệm khi ngoại tuyến:
/// - `GET /v1/masterdata/catalogs/{catalog}` (phân trang cursor);
/// - `GET /v1/mrv/catalog?evidence_type=` (bản đã duyệt mới nhất).
class CatalogCache {
  CatalogCache(this._db, this._clock);
  final LocalDb _db;
  final Clock _clock;

  static const _md = 'masterdata';
  static const _mrv = 'mrv';

  Future<void> _put(String kind, String key, Object json) =>
      _db.db.insert('catalog_cache', {
        'kind': kind,
        'key': key,
        'json': jsonEncode(json),
        'fetched_at': _clock.now().millisecondsSinceEpoch,
      }, conflictAlgorithm: ConflictAlgorithm.replace);

  Future<Object?> _get(String kind, String key) async {
    final rows = await _db.db.query(
      'catalog_cache',
      columns: ['json'],
      where: 'kind = ? AND key = ?',
      whereArgs: [kind, key],
    );
    return rows.isEmpty ? null : jsonDecode(rows.first['json']! as String);
  }

  Future<List<CatalogEntry>> entries(String catalog) async {
    final j = await _get(_md, catalog);
    if (j is! List) return const [];
    return [
      for (final e in j.cast<Map<String, Object?>>())
        CatalogEntry('${e['code']}', '${e['name'] ?? e['code']}', {
          for (final a in ((e['attrs'] as Map?) ?? const {}).entries)
            '${a.key}': '${a.value}',
        }),
    ];
  }

  Future<MrvCatalog?> mrvCatalog(String evidenceType) async {
    final j = await _get(_mrv, evidenceType);
    return j is Map ? MrvCatalog.fromJson(j.cast<String, Object?>()) : null;
  }

  Future<List<MrvCatalog>> mrvCatalogs() async => [
    for (final t in mrvEvidenceTypes.keys) ?await mrvCatalog(t),
  ];

  /// Lưu trực tiếp (kiểm thử, hoặc khi đã có JSON).
  Future<void> putMrv(Map<String, Object?> catalogJson) =>
      _put(_mrv, catalogJson['evidence_type']! as String, catalogJson);

  Future<void> putEntries(String catalog, List<Map<String, Object?>> items) =>
      _put(_md, catalog, items);

  /// Làm mới từ máy chủ. Lỗi quyền/không có danh mục (403/404/422) bỏ qua từng
  /// danh mục (người không có vai `mrv_collector` vẫn đồng bộ bình thường);
  /// thu hồi, 401, lỗi mạng ném tiếp.
  Future<void> refresh(SyncApi api) async {
    for (final c in reportCatalogs) {
      await _guard(() async {
        final items = <Map<String, Object?>>[];
        String? cursor;
        for (var page = 0; page < 50; page++) {
          final j = await api.getJson(
            '/v1/masterdata/catalogs/$c',
            query: {'limit': '200', 'cursor': ?cursor},
          );
          items.addAll(
            ((j['items'] as List?) ?? const []).map(
              (e) => (e as Map).cast<String, Object?>(),
            ),
          );
          cursor = j['next_cursor'] as String?;
          if (cursor == null || cursor.isEmpty) break;
        }
        await putEntries(c, items);
      });
    }
    for (final t in mrvEvidenceTypes.keys) {
      await _guard(() async {
        final j = await api.getJson(
          '/v1/mrv/catalog',
          query: {'evidence_type': t},
        );
        if (j['evidence_type'] == t && j['status'] == 'approved') {
          MrvCatalog.fromJson(j); // kiểm dạng trước khi lưu
          await putMrv(j);
        }
      });
    }
  }

  static Future<void> _guard(Future<void> Function() f) async {
    try {
      await f();
    } on DeviceRevokedException {
      rethrow;
    } on UnauthenticatedException {
      rethrow;
    } on ApiException catch (e) {
      if (e.retriable) rethrow;
    } on FormatException {
      // JSON lạ: giữ bản đệm cũ.
    }
  }
}
