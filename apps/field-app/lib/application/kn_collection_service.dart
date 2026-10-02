import 'dart:convert';

import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/data/actions/action_queue.dart';
import 'package:agrigreen_mobile/data/catalog/catalog_cache.dart';
import 'package:agrigreen_mobile/data/db/local_db.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';

/// Một trang danh sách REST (cursor) — có thể lấy từ bản đệm khi ngoại tuyến.
class KnPage {
  const KnPage(this.items, {this.nextCursor, this.fromCache = false});
  final List<Map<String, Object?>> items;
  final String? nextCursor;
  final bool fromCache;
  bool get hasMore => nextCursor != null && nextCursor!.isNotEmpty;
}

/// Kết quả một lệnh ghi: đã gửi ngay ([response]) hoặc xếp hàng chờ mạng.
class KnCommandResult {
  const KnCommandResult({this.response, this.queued = false});
  final Map<String, Object?>? response;
  final bool queued;
}

/// Máy chủ từ chối lệnh (409/422…) — hiển thị cho người dùng.
class KnCommandError implements Exception {
  KnCommandError(this.code, this.message, {this.status, this.detail});
  final String code;
  final String message;
  final int? status;
  final String? detail;
  @override
  String toString() => message;
}

/// Thông điệp tiếng Việt cho mã lỗi `problem.*` của API thu thập dữ liệu.
String knErrorMessage(String code, [String? detail]) => switch (code) {
  'party.cooperative_name_taken' => 'Tên HTX đã có. Đặt tên khác.',
  'party.out_of_scope' ||
  'mechanization.out_of_scope' ||
  'geo.out_of_scope' ||
  'extension.out_of_scope' => 'Ngoài các xã được phân công.',
  'party.read_only' ||
  'mechanization.read_only' ||
  'geo.read_only' ||
  'extension.read_only' => 'Tài khoản chỉ được xem.',
  'mechanization.machine_type_duplicate' =>
    'Loại máy bị trùng — cập nhật dòng đã có thay vì thêm dòng mới.',
  'mechanization.machine_type_unknown' => 'Loại máy không có trong danh mục.',
  'extension.material_quantity_invalid' =>
    'Số lượng không hợp lệ theo đơn vị tính.',
  'extension.material_superseded' =>
    'Bản ghi này đã được sửa trước đó — mở bản hiện hành để sửa.',
  'geo.plot_overlap' => 'Ranh giới chồng lấn với thửa đã có.',
  'validation_failed' =>
    'Dữ liệu chưa hợp lệ${detail == null ? '' : ': $detail'}.',
  _ => 'Không lưu được ($code).',
};

/// ĐVT đếm được → số lượng phải là số nguyên (hợp đồng §4, "tạm, chờ khách").
const knCountableUnits = {
  'bao',
  'cái',
  'chai',
  'gói',
  'con',
  'chiếc',
  'cây',
  'hộp',
};

/// Kiểm số lượng vật tư theo ĐVT như máy chủ: ≥ 0, ≤ 1e9; ĐVT đếm được → số
/// nguyên; còn lại ≤ 4 chữ số lẻ. null = hợp lệ.
String? validateMaterialQuantity(String raw, String unit) {
  final v = raw.trim().replaceAll(',', '.');
  if (v.isEmpty) return 'Nhập số lượng';
  if (!RegExp(r'^\d+(\.\d+)?$').hasMatch(v)) return 'Số lượng không hợp lệ';
  final n = double.parse(v);
  if (n > 1e9) return 'Tối đa 1.000.000.000';
  final frac = v.contains('.')
      ? v.split('.').last.replaceAll(RegExp(r'0+$'), '')
      : '';
  if (knCountableUnits.contains(unit.trim().toLowerCase()) && frac.isNotEmpty) {
    return 'ĐVT "$unit" phải là số nguyên';
  }
  if (frac.length > 4) return 'Tối đa 4 chữ số thập phân';
  return null;
}

/// Một dòng khai báo máy của HTX.
class KnMachineLine {
  const KnMachineLine(this.machineType, this.quantity, {this.note});
  final String machineType;
  final String quantity;
  final String? note;
}

/// Kiểm các dòng máy móc: số nguyên ≥ 0, loại không trùng. Trả chỉ số dòng →
/// thông điệp (kèm gợi ý cập nhật dòng đã có khi trùng loại).
Map<int, String> validateMachineLines(List<KnMachineLine> lines) {
  final errs = <int, String>{};
  final seen = <String, int>{};
  for (var i = 0; i < lines.length; i++) {
    final l = lines[i];
    if (l.machineType.isEmpty) {
      errs[i] = 'Chọn loại máy';
      continue;
    }
    final prev = seen[l.machineType];
    if (prev != null) {
      errs[i] = 'Trùng loại máy ở dòng ${prev + 1} — cập nhật dòng đã có';
      continue;
    }
    seen[l.machineType] = i;
    final q = int.tryParse(l.quantity.trim());
    if (q == null || q < 0 || '$q' != l.quantity.trim()) {
      errs[i] = 'Số lượng phải là số nguyên ≥ 0';
    }
  }
  return errs;
}

/// Thu thập dữ liệu Cấp Xã (FN-XA-03.03 … 03.06) qua Application API.
///
/// Đọc trực tuyến theo trang (cursor) và đệm trang đầu để xem khi ngoại
/// tuyến. Ghi qua [ActionQueue] (Idempotency-Key cố định `act:<id>`): gửi
/// ngay khi có mạng; mất mạng → xếp hàng, `SyncService` phát lại.
class KnCollectionService {
  KnCollectionService({
    required LocalDb db,
    required SyncApi api,
    required ActionQueue actions,
    required CatalogCache catalogs,
    required Clock clock,
    this.pageSize = 50,
  }) : _db = db,
       _api = api,
       _actions = actions,
       _catalogs = catalogs,
       _clock = clock;

  final LocalDb _db;
  final SyncApi _api;
  final ActionQueue _actions;
  final CatalogCache _catalogs;
  final Clock _clock;

  /// Cỡ trang REST (máy yếu, 3G).
  final int pageSize;

  static const _cacheKind = 'kn.cache';

  // -------------------------------------------------------------- đệm

  Future<void> _put(String key, Object json) => _db.db.rawInsert(
    'INSERT OR REPLACE INTO catalog_cache (kind, key, json, fetched_at) '
    'VALUES (?, ?, ?, ?)',
    [_cacheKind, key, jsonEncode(json), _clock.now().millisecondsSinceEpoch],
  );

  Future<Map<String, Object?>?> _cached(String key) async {
    final rows = await _db.db.query(
      'catalog_cache',
      columns: ['json'],
      where: 'kind = ? AND key = ?',
      whereArgs: [_cacheKind, key],
    );
    if (rows.isEmpty) return null;
    return (jsonDecode(rows.first['json']! as String) as Map)
        .cast<String, Object?>();
  }

  /// GET có đệm: thành công → lưu; lỗi mạng/máy chủ → bản đệm (nếu có).
  Future<(Map<String, Object?>, bool)> _getCached(
    String path, {
    Map<String, String>? query,
    bool cache = true,
  }) async {
    final key =
        '$path?${(query ?? const {}).entries.map((e) => '${e.key}=${e.value}').join('&')}';
    try {
      final j = await _api.getJson(path, query: query);
      if (cache) await _put(key, j);
      return (j, false);
    } on DeviceRevokedException {
      rethrow;
    } on ApiException catch (e) {
      if (!e.retriable && e is! UnauthenticatedException) {
        throw KnCommandError(e.code, knErrorMessage(e.code, e.detail));
      }
      final c = cache ? await _cached(key) : null;
      if (c == null) rethrow;
      return (c, true);
    } on NetworkException {
      final c = cache ? await _cached(key) : null;
      if (c == null) rethrow;
      return (c, true);
    }
  }

  Future<KnPage> _page(
    String path,
    Map<String, String> query, {
    String? cursor,
  }) async {
    final q = {...query, 'limit': '$pageSize', 'cursor': ?cursor};
    // Chỉ đệm trang đầu (xem ngoại tuyến); trang sau cần mạng.
    final (j, cached) = await _getCached(path, query: q, cache: cursor == null);
    return KnPage(
      [
        for (final i in (j['items'] as List?) ?? const [])
          if (i is Map) i.cast<String, Object?>(),
      ],
      nextCursor: j['next_cursor'] as String?,
      fromCache: cached,
    );
  }

  // -------------------------------------------------------------- lệnh

  /// Xếp lệnh rồi thử gửi ngay. Lỗi dữ liệu (4xx) → [KnCommandError] (lệnh bị
  /// bỏ); mất mạng / 5xx → lệnh chờ trong hàng đợi.
  Future<KnCommandResult> run(
    ApiActionKind kind, {
    String? target,
    Map<String, Object?>? body,
  }) async {
    final a = await _actions.enqueue(kind, target: target, body: body);
    try {
      final res = a.method == 'POST'
          ? await _api.postAction(
              path: a.path,
              idempotencyKey: a.idempotencyKey,
              body: body,
            )
          : await _api.sendJson(
              method: a.method,
              path: a.path,
              idempotencyKey: a.idempotencyKey,
              body: body,
            );
      await _actions.markDone(a.id);
      return KnCommandResult(response: res);
    } on NetworkException {
      return const KnCommandResult(queued: true);
    } on ApiException catch (e) {
      if (e is DeviceRevokedException) rethrow;
      if (e.retriable || e is UnauthenticatedException) {
        await _actions.recordAttempt(a.id, e.code);
        return const KnCommandResult(queued: true);
      }
      await _actions.markFailed(a.id, e.code);
      throw KnCommandError(
        e.code,
        knErrorMessage(e.code, e.detail),
        status: e.status,
        detail: e.detail,
      );
    }
  }

  // ------------------------------------------------ HTX & nông hộ (03.03)

  Future<KnPage> cooperatives({
    String? commune,
    String status = 'active',
    String? q,
    String? cursor,
  }) => _page('/v1/kn/cooperatives', {
    'commune': ?commune,
    'status': status,
    if (q != null && q.trim().isNotEmpty) 'q': q.trim(),
  }, cursor: cursor);

  Future<(Map<String, Object?>, bool)> cooperative(String ref) =>
      _getCached('/v1/kn/cooperatives/$ref');

  Future<KnPage> households(String coopRef, {String? cursor}) => _page(
    '/v1/kn/cooperatives/$coopRef/households',
    const {},
    cursor: cursor,
  );

  Future<KnCommandResult> createCooperative({
    required String name,
    required String communeCode,
    String? areaHa,
    String? taxCode,
    String? headOffice,
    String? repName,
    String? repPhone,
  }) => run(
    ApiActionKind.knCooperativeCreate,
    body: {
      'name': name.trim(),
      'commune_code': communeCode,
      if (areaHa != null && areaHa.trim().isNotEmpty)
        'area_ha': areaHa.trim().replaceAll(',', '.'),
      if (taxCode != null && taxCode.trim().isNotEmpty)
        'tax_code': taxCode.trim(),
      if (headOffice != null && headOffice.trim().isNotEmpty)
        'head_office': headOffice.trim(),
      if (repName != null && repName.trim().isNotEmpty)
        'representative': {
          'full_name': repName.trim(),
          'phone': ?repPhone?.trim(),
        },
    },
  );

  Future<KnCommandResult> updateCooperative(
    String ref, {
    String? name,
    String? areaHa,
    String? taxCode,
    String? headOffice,
    String? repName,
    String? repPhone,
    String? reason,
  }) => run(
    ApiActionKind.knCooperativeUpdate,
    target: ref,
    body: {
      'name': ?name?.trim(),
      if (areaHa != null && areaHa.trim().isNotEmpty)
        'area_ha': areaHa.trim().replaceAll(',', '.'),
      'tax_code': ?taxCode?.trim(),
      'head_office': ?headOffice?.trim(),
      if (repName != null && repName.trim().isNotEmpty)
        'representative': {
          'full_name': repName.trim(),
          'phone': ?repPhone?.trim(),
        },
      if (reason != null && reason.trim().isNotEmpty) 'reason': reason.trim(),
    },
  );

  /// Vô hiệu hóa (không xóa cứng). Lý do ≥ 10 ký tự. Trả cảnh báo dữ liệu
  /// liên kết (`warnings: {households, plots, machines}`).
  Future<KnCommandResult> deactivateCooperative(String ref, String reason) {
    if (reason.trim().length < 10) {
      throw KnCommandError('validation_failed', 'Lý do cần ít nhất 10 ký tự.');
    }
    return run(
      ApiActionKind.knCooperativeDeactivate,
      target: ref,
      body: {'reason': reason.trim()},
    );
  }

  Future<KnCommandResult> createHousehold(
    String coopRef, {
    required String fullName,
    required String phone,
    String? areaHa,
  }) => run(
    ApiActionKind.knHouseholdCreate,
    target: coopRef,
    body: {
      'full_name': fullName.trim(),
      'phone': phone.trim(),
      if (areaHa != null && areaHa.trim().isNotEmpty)
        'cultivated_area_ha': areaHa.trim().replaceAll(',', '.'),
    },
  );

  Future<KnCommandResult> updateHousehold(
    String ref, {
    required String areaHa,
    String? reason,
  }) => run(
    ApiActionKind.knHouseholdUpdate,
    target: ref,
    body: {
      'cultivated_area_ha': areaHa.trim().replaceAll(',', '.'),
      if (reason != null && reason.trim().isNotEmpty) 'reason': reason.trim(),
    },
  );

  // ------------------------------------------------- Máy móc HTX (03.04)

  Future<(Map<String, Object?>, bool)> machineCounts({String? commune}) =>
      _getCached('/v1/kn/machine-counts', query: {'commune': ?commune});

  Future<(Map<String, Object?>, bool)> machineCountsOf(String coopRef) =>
      _getCached('/v1/kn/machine-counts/$coopRef');

  Future<KnCommandResult> saveMachineCounts(
    String coopRef,
    List<KnMachineLine> lines, {
    String? reason,
  }) {
    final errs = validateMachineLines(lines);
    if (errs.isNotEmpty) {
      throw KnCommandError('validation_failed', errs.values.first);
    }
    return run(
      ApiActionKind.knMachineCountsSave,
      target: coopRef,
      body: {
        'lines': [
          for (final l in lines)
            {
              'machine_type': l.machineType,
              'quantity': int.parse(l.quantity.trim()),
              if (l.note != null && l.note!.trim().isNotEmpty)
                'note': l.note!.trim(),
            },
        ],
        if (reason != null && reason.trim().isNotEmpty) 'reason': reason.trim(),
      },
    );
  }

  // ----------------------------------------------------- Thửa (03.05)

  Future<KnPage> plots({String? commune, String? coopRef, String? cursor}) =>
      _page('/v1/kn/plots', {
        'commune': ?commune,
        'cooperative_ref': ?coopRef,
      }, cursor: cursor);

  // ----------------------------------------------------- Vật tư (03.06)

  Future<KnPage> materials({
    String? commune,
    String? from,
    String? to,
    String? cursor,
  }) => _page('/v1/kn/materials', {
    'commune': ?commune,
    'from': ?from,
    'to': ?to,
  }, cursor: cursor);

  Future<KnCommandResult> createMaterials({
    required String communeCode,
    required String recordedOn,
    required List<({String code, String quantity, String unit, String? note})>
    lines,
  }) {
    if (lines.isEmpty || lines.length > 200) {
      throw KnCommandError('validation_failed', 'Cần 1–200 dòng vật tư.');
    }
    for (final l in lines) {
      final m = validateMaterialQuantity(l.quantity, l.unit);
      if (m != null) throw KnCommandError('validation_failed', m);
    }
    final today = _clock.now().toUtc().add(const Duration(hours: 7));
    final d = DateTime.tryParse(recordedOn);
    if (d == null ||
        d.isAfter(DateTime.utc(today.year, today.month, today.day))) {
      throw KnCommandError(
        'validation_failed',
        'Ngày ghi nhận không được ở tương lai.',
      );
    }
    return run(
      ApiActionKind.knMaterialsCreate,
      body: {
        'commune_code': communeCode,
        'recorded_on': recordedOn,
        'lines': [
          for (final l in lines)
            {
              'material_code': l.code,
              'quantity': l.quantity.trim().replaceAll(',', '.'),
              if (l.note != null && l.note!.trim().isNotEmpty)
                'note': l.note!.trim(),
            },
        ],
      },
    );
  }

  Future<KnCommandResult> correctMaterial(
    String id, {
    required String quantity,
    required String unit,
    required String reason,
    String? note,
  }) {
    final m = validateMaterialQuantity(quantity, unit);
    if (m != null) throw KnCommandError('validation_failed', m);
    if (reason.trim().length < 10) {
      throw KnCommandError('validation_failed', 'Lý do cần ít nhất 10 ký tự.');
    }
    return run(
      ApiActionKind.knMaterialCorrect,
      target: id,
      body: {
        'quantity': quantity.trim().replaceAll(',', '.'),
        'reason': reason.trim(),
        if (note != null && note.trim().isNotEmpty) 'note': note.trim(),
      },
    );
  }

  // ------------------------------------------------------- danh mục

  /// Danh mục masterdata (`material`, `machine_type`) — tải khi có mạng, dùng
  /// bản đệm khi ngoại tuyến.
  Future<List<CatalogEntry>> catalog(String name) async {
    try {
      final items = <Map<String, Object?>>[];
      String? cursor;
      for (var p = 0; p < 20; p++) {
        final j = await _api.getJson(
          '/v1/masterdata/catalogs/$name',
          query: {'limit': '200', 'cursor': ?cursor},
        );
        items.addAll([
          for (final i in (j['items'] as List?) ?? const [])
            if (i is Map) i.cast<String, Object?>(),
        ]);
        cursor = j['next_cursor'] as String?;
        if (cursor == null || cursor.isEmpty) break;
      }
      await _catalogs.putEntries(name, items);
    } on DeviceRevokedException {
      rethrow;
    } on Object {
      // dùng bản đệm
    }
    return _catalogs.entries(name);
  }
}
