import 'dart:convert';

import 'package:agrigreen_mobile/data/catalog/catalog_cache.dart';
import 'package:agrigreen_mobile/data/db/local_db.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';

import '../domain/roles.dart';

/// Nông hộ liên kết với tài khoản nông dân (`GET /v1/farmer/households`).
class FarmerHousehold {
  const FarmerHousehold(this.ref, this.cooperativeRef, this.communeCode);
  final String ref;
  final String? cooperativeRef;
  final String? communeCode;
}

/// Người dùng và dữ liệu tham chiếu tải qua API (không có entity đồng bộ),
/// đệm trong bảng `meta` của CSDL SQLCipher để dùng khi ngoại tuyến:
/// - `GET /v1/me` → vai, tổ chức (HTX của quản lý);
/// - `GET /v1/farmer/households` (vai farmer) → nông hộ của tài khoản;
/// - `GET /v1/masterdata/catalogs/machine_type` → danh mục máy (HTX US-LOG-01 AC-2);
/// - `GET /v1/cropcycle/variety-bindings` → giống ↔ SOP (HTX US-SEASON-01 AC-1).
///
/// Chưa tải được bao giờ → vai lấy từ access token (`realm_access.roles`).
class ProfileService {
  ProfileService({
    required LocalDb db,
    required CatalogCache catalogs,
    required Future<String?> Function() accessToken,
  }) : _db = db,
       _catalogs = catalogs,
       _token = accessToken;

  final LocalDb _db;
  final CatalogCache _catalogs;
  final Future<String?> Function() _token;

  static const _meKey = 'htx.me';
  static const _householdsKey = 'htx.farmer_households';
  static const _bindingsKey = 'htx.variety_bindings';

  Future<Object?> _get(String key) async {
    final v = await _db.getMeta(key);
    return v == null ? null : jsonDecode(v);
  }

  Future<void> _put(String key, Object v) => _db.setMeta(key, jsonEncode(v));

  Future<List<String>> roles() async {
    final me = await _get(_meKey);
    if (me is Map && me['roles'] is List) {
      return [for (final r in me['roles'] as List) '$r'];
    }
    return rolesFromJwt(await _token());
  }

  Future<HtxMode> mode() async => modeFor(await roles());

  /// HTX của người dùng (`org.ref` của `/v1/me`) — quản lý HTX.
  Future<String?> cooperativeRef() async {
    final me = await _get(_meKey);
    final org = me is Map ? me['org'] : null;
    return org is Map ? org['ref'] as String? : null;
  }

  Future<String?> cooperativeName() async {
    final me = await _get(_meKey);
    final org = me is Map ? me['org'] : null;
    return org is Map ? org['name'] as String? : null;
  }

  Future<List<FarmerHousehold>> households() async {
    final j = await _get(_householdsKey);
    if (j is! List) return const [];
    return [
      for (final e in j)
        if (e is Map)
          FarmerHousehold(
            '${e['household_ref']}',
            e['cooperative_ref'] as String?,
            e['commune_code'] as String?,
          ),
    ];
  }

  /// Mã mẫu SOP mặc định của giống (null nếu chưa gắn / chưa tải).
  Future<String?> templateForVariety(String varietyCode) async {
    final j = await _get(_bindingsKey);
    if (j is! List) return null;
    for (final e in j) {
      if (e is Map && e['variety_code'] == varietyCode) {
        return e['template_ref'] as String?;
      }
    }
    return null;
  }

  /// Chạy sau mỗi lượt pull. Lỗi quyền/không có (4xx) bỏ qua từng mục; thu
  /// hồi, 401, lỗi mạng, 5xx ném tiếp (lượt đồng bộ lùi thử lại).
  Future<void> refresh(SyncApi api) async {
    await _guard(() async {
      final me = await api.getJson('/v1/me');
      await _put(_meKey, {'roles': me['roles'], 'org': me['org']});
    });
    if (modeFor(await roles()) == HtxMode.farmer) {
      await _guard(() async {
        final j = await api.getJson('/v1/farmer/households');
        await _put(_householdsKey, (j['items'] as List?) ?? const []);
      });
    }
    await _guard(() async {
      final items = <Map<String, Object?>>[];
      String? cursor;
      for (var page = 0; page < 20; page++) {
        final j = await api.getJson(
          '/v1/masterdata/catalogs/machine_type',
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
      await _catalogs.putEntries('machine_type', items);
    });
    await _guard(() async {
      final j = await api.getJson('/v1/cropcycle/variety-bindings');
      await _put(_bindingsKey, (j['items'] as List?) ?? const []);
    });
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
    } on TypeError {
      // Dạng dữ liệu lạ: giữ bản đệm cũ.
    }
  }
}
