import 'dart:convert';

import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/data/db/local_db.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';

/// Khóa menu Cấp Xã mà app hiện trường có màn hình (hợp đồng P7 §6). Khóa
/// khác trong `GET /v1/menu` bị ẩn.
abstract final class KnMenuKey {
  static const congViec = 'xa.nhiem_vu.cong_viec';
  static const lichCongTac = 'xa.nhiem_vu.lich_cong_tac';
  static const baoCaoTuan = 'xa.bao_cao.tuan';
  static const baoCaoThang = 'xa.bao_cao.thang';
  static const baoCaoVu = 'xa.bao_cao.hang_vu';
  static const baoCaoCuaXa = 'xa.bao_cao.cua_xa';
  static const baoCaoDotXuat = 'xa.bao_cao.dot_xuat';
  static const htx = 'xa.thu_thap.htx';
  static const mayMoc = 'xa.thu_thap.may_moc';
  static const thua = 'xa.thu_thap.thua';
  static const vatTu = 'xa.thu_thap.vat_tu';

  static const known = {
    congViec,
    lichCongTac,
    baoCaoTuan,
    baoCaoThang,
    baoCaoVu,
    baoCaoCuaXa,
    baoCaoDotXuat,
    htx,
    mayMoc,
    thua,
    vatTu,
  };
}

/// Badge "việc cần làm" (badge_source của `xa.nhiem_vu.cong_viec`).
const knBadgeTasksTodo = 'extension.report_tasks.todo';

class KnMenuEntry {
  const KnMenuEntry({
    required this.key,
    required this.label,
    required this.group,
    this.badgeSource,
    this.enabled = true,
  });
  final String key;
  final String label;

  /// Nhãn nhóm (Điều hành / Báo cáo / Dữ liệu).
  final String group;
  final String? badgeSource;
  final bool enabled;
}

class KnMenu {
  const KnMenu(this.entries, {this.version, this.fromCache = false});
  final List<KnMenuEntry> entries;
  final String? version;

  /// Lấy từ bản đệm (ngoại tuyến).
  final bool fromCache;

  bool has(String key) => entries.any((e) => e.key == key);

  /// Mở `KNMenu` (nhóm → mục → mục con) thành danh sách phẳng, chỉ giữ khóa
  /// app biết ([KnMenuKey.known]).
  factory KnMenu.fromJson(Map<String, Object?> j, {bool fromCache = false}) {
    final out = <KnMenuEntry>[];
    void walk(Object? items, String group) {
      for (final it in (items is List ? items : const [])) {
        if (it is! Map) continue;
        final key = '${it['key']}';
        if (KnMenuKey.known.contains(key)) {
          out.add(
            KnMenuEntry(
              key: key,
              label: '${it['label'] ?? key}',
              group: group,
              badgeSource: it['badge_source'] as String?,
              enabled: it['enabled'] != false,
            ),
          );
        }
        walk(it['children'], group);
      }
    }

    for (final g in (j['groups'] as List?) ?? const []) {
      if (g is Map) walk(g['items'], '${g['label'] ?? g['key'] ?? ''}');
    }
    return KnMenu(
      out,
      version: j['menu_version'] as String?,
      fromCache: fromCache,
    );
  }
}

/// Menu Cấp Xã từ `GET /v1/menu` (P2), đệm trong `catalog_cache` để dùng khi
/// ngoại tuyến; số đếm từ `GET /v1/extension/report-badges` khi có mạng.
class KnMenuService {
  KnMenuService({
    required LocalDb db,
    required SyncApi api,
    required Clock clock,
    required this.localTodoCount,
  }) : _db = db,
       _api = api,
       _clock = clock;

  final LocalDb _db;
  final SyncApi _api;
  final Clock _clock;

  /// Số việc cần làm tính trên máy (khi ngoại tuyến).
  final Future<int> Function() localTodoCount;

  static const _kind = 'kn.menu';

  Future<void> _put(String key, Object json) => _db.db.rawInsert(
    'INSERT OR REPLACE INTO catalog_cache (kind, key, json, fetched_at) '
    'VALUES (?, ?, ?, ?)',
    [_kind, key, jsonEncode(json), _clock.now().millisecondsSinceEpoch],
  );

  Future<Map<String, Object?>?> _get(String key) async {
    final rows = await _db.db.query(
      'catalog_cache',
      columns: ['json'],
      where: 'kind = ? AND key = ?',
      whereArgs: [_kind, key],
    );
    if (rows.isEmpty) return null;
    return (jsonDecode(rows.first['json']! as String) as Map)
        .cast<String, Object?>();
  }

  /// Menu hiện hành; lỗi mạng → bản đệm; không có bản đệm → menu Cấp Xã mặc
  /// định (chỉ "Công việc cần làm" và "Lịch công tác").
  Future<KnMenu> load({bool refresh = true}) async {
    if (refresh) {
      try {
        final j = await _api.getJson('/v1/menu');
        await _put('menu', j);
        return KnMenu.fromJson(j);
      } on DeviceRevokedException {
        rethrow;
      } on NetworkException {
        // dùng bản đệm
      } on ApiException {
        // dùng bản đệm
      }
    }
    final c = await _get('menu');
    if (c != null) return KnMenu.fromJson(c, fromCache: true);
    return const KnMenu([
      KnMenuEntry(
        key: KnMenuKey.congViec,
        label: 'Công việc cần làm',
        group: 'Điều hành',
        badgeSource: knBadgeTasksTodo,
      ),
      KnMenuEntry(
        key: KnMenuKey.lichCongTac,
        label: 'Lịch công tác',
        group: 'Điều hành',
      ),
    ], fromCache: true);
  }

  /// Số đếm theo `badge_source`. Trực tuyến: máy chủ; ngoại tuyến: đếm trên
  /// máy cho "việc cần làm".
  Future<Map<String, int>> badges() async {
    try {
      final j = await _api.getJson('/v1/extension/report-badges');
      final counts = (j['counts'] as Map?) ?? const {};
      final out = <String, int>{
        for (final e in counts.entries)
          if (e.value is num) '${e.key}': (e.value as num).toInt(),
      };
      return out;
    } on DeviceRevokedException {
      rethrow;
    } on Object {
      return {knBadgeTasksTodo: await localTodoCount()};
    }
  }
}
