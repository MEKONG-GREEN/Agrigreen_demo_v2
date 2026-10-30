import 'dart:convert';

import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/data/actions/action_queue.dart';
import 'package:agrigreen_mobile/data/catalog/catalog_cache.dart';
import 'package:agrigreen_mobile/data/db/local_db.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';

/// Lý do vô hiệu hóa / kích hoạt lại hồ sơ KN (FN-XA-03.03): 10–1000 ký tự —
/// bằng `knrecords/domain.MinReason`, `MaxReason` (KNSPLIT-2-KN-RECORDS #67).
const kReasonMin = 10;
const kReasonMax = 1000;

/// Loại hồ sơ KN riêng (`/v1/kn/producer-records`, #67) — không đổi sau khi tạo.
abstract final class KnRecordKind {
  static const cooperative = 'cooperative';
  static const household = 'household';
  static const coopGroup = 'coop_group';
}

/// Diện tích (ha) như máy chủ (`knrecords/domain.ParseArea`): số thập phân
/// thường (dấu phẩy được đổi thành chấm), không số mũ, không dấu, ≤ 16 ký
/// tự, ≤ 4 chữ số lẻ, 0–1.000.000. Kiểm chuỗi TRƯỚC khi đọc thành số. Trả
/// (giá trị chuẩn hóa, lỗi); rỗng → (null, null).
(String?, String?) normalizeAreaHa(String? raw) {
  final v = (raw ?? '').trim().replaceAll(',', '.');
  if (v.isEmpty) return (null, null);
  if (v.length > 16 || !RegExp(r'^[0-9]{1,7}(\.[0-9]{1,4})?$').hasMatch(v)) {
    return (
      null,
      'Diện tích: số ≥ 0, tối đa 4 chữ số lẻ, không quá 1.000.000 ha',
    );
  }
  final whole = int.parse(v.split('.').first);
  final frac = v.contains('.') ? v.split('.').last : '';
  if (whole > 1000000 || (whole == 1000000 && frac.replaceAll('0', '') != '')) {
    return (null, 'Diện tích không quá 1.000.000 ha');
  }
  return (v, null);
}

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
  'extension.kn_record_name_taken' =>
    'Tên đã có trong xã (cùng loại, đang hoạt động). Đặt tên khác.',
  'extension.kn_record_household_exists' =>
    'Chủ hộ đã có hồ sơ Nông hộ đang hoạt động.',
  'extension.kn_record_version_conflict' =>
    'Hồ sơ đã được người khác cập nhật — tải lại rồi sửa.',
  'extension.kn_record_state_conflict' =>
    'Trạng thái hồ sơ không cho phép thao tác này${detail == null ? '' : ': $detail'}.',
  'extension.kn_record_not_found' =>
    'Không tìm thấy hồ sơ trong phạm vi phụ trách.',
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
  //
  // KNSPLIT-2-KN-RECORDS (#67, #161): HTX / Nông hộ / Tổ hợp tác của màn KN là
  // hồ sơ KN riêng (`/v1/kn/producer-records`, sổ `pf_extension.kn_producer`).
  // KN KHÔNG tạo/sửa HTX nền tảng: không còn lệnh ghi `/v1/kn/cooperatives…`.

  static const _records = '/v1/kn/producer-records';

  /// Một trang hồ sơ KN theo loại (`kind` bắt buộc). Nông hộ không lọc theo
  /// tên (tên chủ hộ ở kho định danh) — lọc theo HTX/THT cha ([parentRef]).
  Future<KnPage> records({
    required String kind,
    String? commune,
    String status = 'active',
    String? q,
    String? parentRef,
    String? cursor,
  }) => _page(_records, {
    'kind': kind,
    'commune': ?commune,
    'status': status,
    if (kind != KnRecordKind.household && q != null && q.trim().isNotEmpty)
      'q': q.trim(),
    if (kind == KnRecordKind.household) 'parent_ref': ?parentRef,
  }, cursor: cursor);

  /// Chi tiết hồ sơ (kèm `version`, `log`) — đệm để xem ngoại tuyến.
  Future<(Map<String, Object?>, bool)> record(String ref) =>
      _getCached('$_records/$ref');

  /// Nông hộ (đang hoạt động) thuộc một hồ sơ HTX/THT.
  Future<KnPage> households(String parentRef, {String? cursor}) => records(
    kind: KnRecordKind.household,
    parentRef: parentRef,
    cursor: cursor,
  );

  /// HTX đã công bố trong phạm vi (chỉ đọc) — chọn để liên kết đối chiếu
  /// (`htx_ref`) khi thêm/sửa hồ sơ HTX; máy móc, thửa đọc theo `htx_ref`.
  Future<List<Map<String, Object?>>> htxCandidates(String commune) async {
    final (j, _) = await _getCached(
      '/v1/kn/producer-htx-candidates',
      query: {'commune': commune},
    );
    return [
      for (final i in (j['items'] as List?) ?? const [])
        if (i is Map) i.cast<String, Object?>(),
    ];
  }

  /// HTX nền tảng (legacy GET, CHỈ ĐỌC) — bộ chọn HTX của màn Thửa (khai báo
  /// thửa, máy vẫn ở module nền, #162). Không dùng để ghi.
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

  static String? _opt(String? v) {
    final t = v?.trim();
    return t == null || t.isEmpty ? null : t;
  }

  static Map<String, Object?>? _person(String? name, String? phone) {
    final n = _opt(name);
    if (n == null) return null;
    return {'full_name': n, 'phone': _opt(phone) ?? ''};
  }

  static String _area(String? raw) {
    final (v, err) = normalizeAreaHa(raw);
    if (err != null) throw KnCommandError('validation_failed', err);
    return v ?? '';
  }

  /// Thêm hồ sơ KN. HTX, THT: [name], người đại diện ([personName],
  /// [personPhone]) bắt buộc; Nông hộ: chủ hộ bắt buộc, thuộc [parentRef]
  /// (HTX/THT), không tên/địa chỉ/MST. [htxRef] chỉ cho HTX.
  Future<KnCommandResult> createRecord({
    required String kind,
    required String communeCode,
    String? name,
    String? areaHa,
    String? taxCode,
    String? address,
    String? personName,
    String? personPhone,
    String? parentRef,
    String? htxRef,
  }) {
    final household = kind == KnRecordKind.household;
    final area = _area(areaHa);
    final person = _person(personName, personPhone);
    if (person == null) {
      throw KnCommandError(
        'validation_failed',
        household ? 'Nhập chủ hộ và SĐT.' : 'Nhập người đại diện và SĐT.',
      );
    }
    return run(
      ApiActionKind.knRecordCreate,
      body: {
        'kind': kind,
        'commune_code': communeCode,
        if (!household) 'name': ?_opt(name),
        if (area.isNotEmpty) 'area_ha': area,
        if (kind == KnRecordKind.cooperative) 'tax_code': ?_opt(taxCode),
        if (!household) 'address': ?_opt(address),
        (household ? 'holder' : 'representative'): person,
        if (household) 'parent_ref': ?_opt(parentRef),
        if (kind == KnRecordKind.cooperative) 'htx_ref': ?_opt(htxRef),
      },
    );
  }

  /// Sửa hồ sơ [existing] (bản chi tiết từ máy chủ): máy chủ THAY toàn bộ
  /// trường nhập được (trường vắng = xóa), nên thân gửi đủ giá trị hiện có,
  /// đè bởi tham số. Người đại diện/chủ hộ chỉ gửi khi nhập mới. Khóa lạc
  /// quan `base_version` = `existing['version']` (409 nếu đã có bản mới).
  Future<KnCommandResult> updateRecord(
    Map<String, Object?> existing, {
    String? name,
    String? areaHa,
    String? taxCode,
    String? address,
    String? personName,
    String? personPhone,
    String? htxRef,
  }) {
    final ref = existing['ref'] as String?;
    final version = existing['version'];
    final kind = existing['kind'] as String?;
    if (ref == null || version is! int || kind == null) {
      throw KnCommandError('validation_failed', 'Tải lại hồ sơ rồi sửa.');
    }
    String? cur(String k) => existing[k] as String?;
    final household = kind == KnRecordKind.household;
    final area = _area(areaHa ?? cur('area_ha'));
    final person = _person(personName, personPhone);
    return run(
      ApiActionKind.knRecordUpdate,
      target: ref,
      body: {
        'kind': kind,
        'commune_code': cur('commune_code'),
        if (!household) 'name': ?_opt(name ?? cur('name')),
        if (area.isNotEmpty) 'area_ha': area,
        if (kind == KnRecordKind.cooperative)
          'tax_code': ?_opt(taxCode ?? cur('tax_code')),
        if (!household) 'address': ?_opt(address ?? cur('address')),
        (household ? 'holder' : 'representative'): ?person,
        if (household) 'parent_ref': ?cur('parent_ref'),
        if (kind == KnRecordKind.cooperative)
          'htx_ref': ?_opt(htxRef ?? cur('htx_ref')),
        'base_version': version,
      },
    );
  }

  /// Vô hiệu hóa (không xóa) hoặc kích hoạt lại hồ sơ [existing]; lý do
  /// 10–1000 ký tự, kiểm trên máy trước khi xếp hàng.
  Future<KnCommandResult> setRecordStatus(
    Map<String, Object?> existing, {
    required bool deactivate,
    required String reason,
  }) {
    final ref = existing['ref'] as String?;
    final version = existing['version'];
    if (ref == null || version is! int) {
      throw KnCommandError('validation_failed', 'Tải lại hồ sơ rồi thử lại.');
    }
    final r = reason.trim();
    final n = r.runes.length;
    if (n < kReasonMin || n > kReasonMax) {
      throw KnCommandError(
        'validation_failed',
        'Lý do cần $kReasonMin–$kReasonMax ký tự.',
      );
    }
    return run(
      deactivate
          ? ApiActionKind.knRecordDeactivate
          : ApiActionKind.knRecordReactivate,
      target: ref,
      body: {'reason': r, 'base_version': version},
    );
  }

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
