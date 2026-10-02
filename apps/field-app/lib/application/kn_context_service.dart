import 'dart:async';
import 'dart:convert';

import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/db/local_db.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';

import '../domain/kn_context.dart';
import '../domain/kn_report.dart';

enum KnContextStage {
  /// Tài khoản không có phân công KN v7.7 (hoặc máy chủ chưa có P1) → màn
  /// hình chủ cũ của app hiện trường.
  none,

  /// Nhiều cặp (vai trò, phòng ban), chưa chọn → màn "Chọn vai trò và phòng
  /// ban".
  picker,

  /// Đã có ngữ cảnh (kể cả ngữ cảnh lưu trên máy khi ngoại tuyến).
  ready,
}

class KnContextState {
  const KnContextState(
    this.stage, {
    this.context,
    this.choices = const [],
    this.message,
    this.offline = false,
  });
  final KnContextStage stage;
  final KnSessionContext? context;
  final List<KnContextChoice> choices;
  final String? message;

  /// Đang dùng ngữ cảnh lưu trên máy (không gọi được máy chủ).
  final bool offline;
}

class KnContextException implements Exception {
  KnContextException(this.message);
  final String message;
  @override
  String toString() => message;
}

/// Ngữ cảnh phiên KN v7.7 (P1) của app hiện trường.
///
/// - Sau đăng nhập + PIN: `GET /v1/me/contexts`; một cặp → tự chọn, nhiều →
///   màn chọn; `POST /v1/me/context` → lưu `KNSessionContext` và danh sách
///   cặp trong CSDL SQLCipher (bảng meta) để làm việc NGOẠI TUYẾN trong cửa sổ
///   72 giờ (ngữ cảnh cũ vẫn gửi kèm hàng đợi).
/// - Mọi lời gọi API mang `X-AG-Context` = [contextId].
/// - Máy chủ báo hết hạn/thu hồi/không hợp lệ (403) hoặc thiếu (428) →
///   [recover] chọn lại đúng phân công cũ rồi gọi lại; phân công không còn →
///   chuyển sang màn chọn ([changes]).
/// - Đổi vai trò/phòng ban (không đăng nhập lại): phạm vi đổi → xóa dữ liệu KN
///   đã pull và bản đệm KN (máy chủ trả `reset: true` ở lượt pull kế tiếp).
class KnContextService {
  KnContextService({
    required LocalDb db,
    required SyncApi api,
    required Clock clock,
    required IdGenerator ids,
  }) : _db = db,
       _api = api,
       _clock = clock,
       _ids = ids;

  final LocalDb _db;
  final SyncApi _api;
  final Clock _clock;
  final IdGenerator _ids;

  static const _ctxKey = 'kn.context';
  static const _choiceKey = 'kn.context.choice';
  static const _choicesKey = 'kn.context.choices';
  static const contextsPath = '/v1/me/contexts';
  static const contextPath = '/v1/me/context';

  KnSessionContext? _current;
  KnContextChoice? _choice;
  bool _loaded = false;
  Future<String?>? _recovering;

  final _changes = StreamController<KnContextState>.broadcast();

  /// Thay đổi cần màn hình phản ứng (đã chọn lại, cần chọn ở màn chọn).
  Stream<KnContextState> get changes => _changes.stream;

  KnSessionContext? get current => _current;

  Future<void> _load() async {
    if (_loaded) return;
    _loaded = true;
    _current = _decode(await _db.getMeta(_ctxKey), KnSessionContext.fromJson);
    _choice = _decode(await _db.getMeta(_choiceKey), KnContextChoice.fromJson);
  }

  static T? _decode<T>(String? s, T Function(Map<String, Object?>) f) {
    if (s == null) return null;
    try {
      return f((jsonDecode(s) as Map).cast<String, Object?>());
    } on Object {
      return null;
    }
  }

  /// Mã ngữ cảnh cho header `X-AG-Context` (ngữ cảnh lưu trên máy — kể cả đã
  /// hết hạn: máy chủ trả 403 và [recover] chọn lại).
  Future<String?> contextId() async {
    await _load();
    return _current?.contextId;
  }

  Future<KnSessionContext?> stored() async {
    await _load();
    return _current;
  }

  Future<List<KnContextChoice>> storedChoices() async {
    final s = await _db.getMeta(_choicesKey);
    if (s == null) return const [];
    try {
      return [
        for (final e in jsonDecode(s) as List)
          KnContextChoice.fromJson((e as Map).cast<String, Object?>()),
      ];
    } on Object {
      return const [];
    }
  }

  /// Lấy các cặp chọn được từ máy chủ và lưu lại. Ném [NetworkException] khi
  /// mất mạng; 403/404 (không phải người dùng KN, máy chủ chưa có P1) → rỗng.
  Future<List<KnContextChoice>> fetchChoices() async {
    Map<String, Object?> body;
    try {
      body = await _api.getJson(contextsPath);
    } on ApiException catch (e) {
      if (e is DeviceRevokedException || e is UnauthenticatedException) {
        rethrow;
      }
      if (e.status == 403 || e.status == 404) return const [];
      rethrow;
    }
    final list = KnContextChoice.expand(body);
    await _db.setMeta(
      _choicesKey,
      jsonEncode([for (final c in list) c.toJson()]),
    );
    return list;
  }

  /// Xác định ngữ cảnh sau khi mở khóa (đăng nhập trực tuyến hoặc PIN).
  Future<KnContextState> resolve() async {
    await _load();
    final cur = _current;
    List<KnContextChoice> choices;
    try {
      choices = await fetchChoices();
    } on NetworkException {
      return _offline(cur);
    } on UnauthenticatedException {
      return _offline(cur);
    } on DeviceRevokedException {
      rethrow;
    } on ApiException catch (e) {
      if (e.retriable) return _offline(cur);
      return KnContextState(KnContextStage.none, message: e.code);
    }
    if (choices.isEmpty) {
      await _clear();
      return const KnContextState(KnContextStage.none);
    }
    final prev = _choice;
    final same = prev == null
        ? null
        : choices.where((c) => c.key == prev.key).firstOrNull;
    try {
      if (cur != null && same != null) {
        if (!cur.expiredAt(_clock.now())) {
          return KnContextState(
            KnContextStage.ready,
            context: cur,
            choices: choices,
          );
        }
        // Hết hạn: chọn lại đúng cặp cũ.
        return KnContextState(
          KnContextStage.ready,
          context: await select(same),
          choices: choices,
        );
      }
      if (choices.length == 1) {
        return KnContextState(
          KnContextStage.ready,
          context: await select(choices.single),
          choices: choices,
        );
      }
    } on NetworkException {
      return _offline(cur);
    }
    return KnContextState(KnContextStage.picker, choices: choices);
  }

  KnContextState _offline(KnSessionContext? cur) => cur == null
      ? const KnContextState(
          KnContextStage.none,
          message: 'Chưa chọn được vai trò khi ngoại tuyến.',
          offline: true,
        )
      : KnContextState(KnContextStage.ready, context: cur, offline: true);

  /// `POST /v1/me/context` cho cặp [c] và lưu kết quả. Phạm vi khác ngữ cảnh
  /// cũ → xóa dữ liệu KN đã pull và bản đệm KN.
  Future<KnSessionContext> select(KnContextChoice c) async {
    await _load();
    final Map<String, Object?> body;
    try {
      body = await _api.postAction(
        path: contextPath,
        idempotencyKey: 'ctx:${_ids.newId()}',
        body: {
          'assignment_id': c.assignmentId,
          'department_code': c.departmentCode,
        },
      );
    } on ApiException catch (e) {
      if (e is DeviceRevokedException ||
          e is UnauthenticatedException ||
          e.retriable) {
        rethrow;
      }
      throw KnContextException(
        'Không chọn được vai trò/phòng ban (${e.code}).',
      );
    }
    final ctx = KnSessionContext.fromJson(body);
    if (ctx.contextId.isEmpty || ctx.contextId == 'null') {
      throw KnContextException('Máy chủ không cấp ngữ cảnh phiên.');
    }
    final prev = _current;
    if (prev != null && prev.scopeKey != ctx.scopeKey) {
      await resetKnData();
    }
    _current = ctx;
    _choice = c;
    await _db.setMeta(_ctxKey, jsonEncode(ctx.toJson()));
    await _db.setMeta(_choiceKey, jsonEncode(c.toJson()));
    return ctx;
  }

  /// Xóa dữ liệu KN đã pull (công việc, bố cục, bài nộp) và bản đệm KN —
  /// thuộc phạm vi cũ. Hàng đợi chiều lên giữ nguyên (không mất dữ liệu nhập).
  Future<void> resetKnData() async {
    await _db.db.transaction((tx) async {
      for (final e in KnEntity.all) {
        await tx.delete('ref_entity', where: 'entity = ?', whereArgs: [e]);
      }
      await tx.delete('catalog_cache', where: "kind LIKE 'kn.%'");
    });
  }

  /// [ContextRecovery] cho `HttpSyncApi`: chọn lại cùng phân công; không còn →
  /// phát trạng thái màn chọn. Gọi chồng nhau dùng chung một lần chọn lại.
  Future<String?> recover(ApiException rejected) {
    final r = _recovering;
    if (r != null) return r;
    final f = _recover().whenComplete(() => _recovering = null);
    _recovering = f;
    return f;
  }

  Future<String?> _recover() async {
    await _load();
    final prev = _choice;
    try {
      final choices = await fetchChoices();
      final same = prev == null
          ? null
          : choices.where((c) => c.key == prev.key).firstOrNull;
      if (same == null) {
        if (choices.length == 1) {
          final ctx = await select(choices.single);
          _emit(KnContextState(KnContextStage.ready, context: ctx));
          return ctx.contextId;
        }
        await _clearContextOnly();
        _emit(
          KnContextState(
            choices.isEmpty ? KnContextStage.none : KnContextStage.picker,
            choices: choices,
            message: 'Phân công cũ không còn hiệu lực. Chọn lại vai trò.',
          ),
        );
        return null;
      }
      final ctx = await select(same);
      return ctx.contextId;
    } on NetworkException {
      return null;
    } on KnContextException {
      return null;
    }
  }

  void _emit(KnContextState s) {
    if (!_changes.isClosed) _changes.add(s);
  }

  /// "Đổi vai trò/phòng ban": chọn cặp khác ngay (cần mạng).
  Future<KnSessionContext> switchTo(KnContextChoice c) async {
    final ctx = await select(c);
    _emit(KnContextState(KnContextStage.ready, context: ctx));
    return ctx;
  }

  Future<void> _clearContextOnly() async {
    _current = null;
    _choice = null;
    await _db.setMeta(_ctxKey, null);
    await _db.setMeta(_choiceKey, null);
  }

  Future<void> _clear() async {
    final had = _current != null;
    await _clearContextOnly();
    await _db.setMeta(_choicesKey, null);
    if (had) await resetKnData();
  }

  Future<void> dispose() => _changes.close();
}
