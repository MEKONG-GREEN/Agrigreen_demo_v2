import 'dart:async';
import 'dart:convert';
import 'dart:io' show SocketException;
import 'dart:typed_data';

import 'package:http/http.dart' as http;

import '../../domain/outbox_record.dart';
import '../outbox/outbox_repository.dart';

/// Lỗi HTTP từ Application API (problem+json: `status`, `code`, `title`).
class ApiException implements Exception {
  ApiException(this.status, this.code, [this.detail]);
  final int status;
  final String code;
  final String? detail;

  /// 5xx, 429, 408 và 409 idempotency_in_progress → thử lại sau.
  bool get retriable =>
      status >= 500 ||
      status == 429 ||
      status == 408 ||
      (status == 409 && code == 'idempotency_in_progress');

  /// Ngữ cảnh phiên KN (header `X-AG-Context`, KN v7.7 P1) hết hạn / bị thu
  /// hồi / không hợp lệ (403) hoặc thiếu (428) → chọn lại ngữ cảnh, thử lại.
  bool get isContextError =>
      (status == 403 && contextErrorCodes.contains(code)) || status == 428;

  static const contextErrorCodes = {
    'accounts.context_expired',
    'accounts.context_revoked',
    'accounts.context_invalid',
    'accounts.context_required',
  };

  @override
  String toString() => 'ApiException($status, $code)';
}

/// `403 sync.device_revoked` — phải xóa dữ liệu cục bộ và khóa token ngay.
class DeviceRevokedException extends ApiException {
  DeviceRevokedException() : super(403, deviceRevokedCode);
  static const deviceRevokedCode = 'sync.device_revoked';
}

/// 401 — token hết hạn / bị thu hồi. Cần làm mới token hoặc đăng nhập lại.
class UnauthenticatedException extends ApiException {
  UnauthenticatedException(String code) : super(401, code);
}

/// Không có mạng / hết thời gian chờ.
class NetworkException implements Exception {
  NetworkException(this.cause);
  final Object cause;
  @override
  String toString() => 'NetworkException($cause)';
}

class PullChange {
  const PullChange({
    required this.entity,
    required this.id,
    required this.version,
    required this.op,
    this.data,
  });
  final String entity;
  final String id;
  final int version;

  /// upsert | tombstone
  final String op;
  final Map<String, Object?>? data;

  factory PullChange.fromJson(Map<String, Object?> j) => PullChange(
    entity: j['entity']! as String,
    id: j['id']! as String,
    version: (j['version']! as num).toInt(),
    op: j['op']! as String,
    data: (j['data'] as Map?)?.cast<String, Object?>(),
  );
}

class PullPage {
  const PullPage({
    required this.changes,
    required this.nextCursor,
    required this.hasMore,
    this.serverTime,
    this.reset = false,
  });
  final List<PullChange> changes;
  final String? nextCursor;
  final bool hasMore;
  final String? serverTime;

  /// Phạm vi đã đổi: máy thay toàn bộ dữ liệu tham chiếu (sync-contract).
  final bool reset;

  factory PullPage.fromJson(Map<String, Object?> j) => PullPage(
    changes: ((j['changes'] as List?) ?? const [])
        .map((e) => PullChange.fromJson((e as Map).cast<String, Object?>()))
        .toList(),
    nextCursor: j['next_cursor'] as String?,
    hasMore: j['has_more'] == true,
    serverTime: j['server_time'] as String?,
    reset: j['reset'] == true,
  );
}

/// Application API mà app hiện trường nói chuyện (docs/dev/sync-contract.md).
abstract class SyncApi {
  Future<void> registerDevice({
    required String deviceId,
    required String platform,
    required String appVersion,
  });

  Future<List<PushResult>> push({
    required String batchKey,
    required String deviceId,
    required List<OutboxRecord> records,
  });

  /// `GET /v1/sync/pull`. [entities] (G2, KN US-SYNC-02): chỉ đọc các entity
  /// này (`?entities=plot,task`); máy chủ giữ vị trí con trỏ của entity khác.
  /// null/rỗng = mọi entity (như G1).
  Future<PullPage> pull({
    String? cursor,
    required int limit,
    List<String>? entities,
  });

  /// `POST /v1/files` multipart: phần `meta` (JSON phong bì) + `blob`
  /// (ciphertext) + `file_ref` (UUIDv7 máy sinh, tùy chọn).
  Future<String> uploadFile({
    required String idempotencyKey,
    required Map<String, Object?> meta,
    required Uint8List ciphertext,
    String? fileRef,
  });

  /// `POST /v1/devices/{device_id}/push-token {provider: fcm, token}`.
  Future<void> registerPushToken({
    required String deviceId,
    required String token,
    required String idempotencyKey,
  });

  /// GET JSON chỉ đọc (danh mục masterdata, danh mục trường MRV).
  Future<Map<String, Object?>> getJson(
    String path, {
    Map<String, String>? query,
  });

  /// Lệnh API idempotent (POST + Idempotency-Key) xếp hàng khi ngoại tuyến:
  /// đánh dấu đã đọc, xác nhận chỉ đạo, lưu/bỏ lưu tài liệu.
  Future<Map<String, Object?>> postAction({
    required String path,
    required String idempotencyKey,
    Map<String, Object?>? body,
  });

  /// Lệnh JSON với phương thức bất kỳ (KN v7.7 P7: `PUT /v1/kn/...` sửa HTX,
  /// nông hộ, khai báo máy móc). [idempotencyKey] cố định cho mọi lần phát lại.
  Future<Map<String, Object?>> sendJson({
    required String method,
    required String path,
    String? idempotencyKey,
    Map<String, Object?>? body,
  });
}

typedef AccessTokenProvider = Future<String?> Function();

/// Mã ngữ cảnh phiên KN hiện hành (null = không gửi header `X-AG-Context`).
typedef ContextIdProvider = Future<String?> Function();

/// Máy chủ từ chối ngữ cảnh ([ApiException.isContextError]): chọn lại ngữ
/// cảnh (cùng phân công) và trả mã mới để thử lại một lần; null = không chọn
/// lại được (người dùng phải chọn ở màn "Chọn vai trò và phòng ban").
typedef ContextRecovery = Future<String?> Function(ApiException rejected);

class HttpSyncApi implements SyncApi {
  HttpSyncApi({
    required this.baseUrl,
    required this.accessToken,
    this.deviceId,
    http.Client? client,
    this.timeout = const Duration(seconds: 30),
    this.pushPath = defaultPushPath,
    this.pullPath = defaultPullPath,
    this.contextId,
    this.onContextRejected,
  }) : _client = client ?? http.Client();

  static const defaultPushPath = '/v1/sync/push';
  static const defaultPullPath = '/v1/sync/pull';

  /// Header ngữ cảnh phiên KN v7.7 (P1) — gửi ở MỌI lời gọi khi [contextId]
  /// trả mã (trừ chính các lời gọi chọn ngữ cảnh [contextFreePaths]).
  static const contextHeader = 'X-AG-Context';

  /// `GET /v1/me/contexts`, `POST /v1/me/context`: không mang ngữ cảnh cũ và
  /// không tự chọn lại khi bị từ chối (tránh vòng lặp).
  static const contextFreePaths = {'/v1/me/contexts', '/v1/me/context'};

  /// Đường dẫn đồng bộ (cùng hình dạng hợp đồng sync-contract). Mặc định của
  /// nền tảng; App hiện trường MKG dùng `/v1/fieldops/sync/push|pull` của
  /// api-internal (vùng mkg).
  final String pushPath;
  final String pullPath;

  final String baseUrl;
  final AccessTokenProvider accessToken;

  /// Gửi trong header `X-Device-Id` của mọi lời gọi (sync-contract).
  final String? deviceId;
  final http.Client _client;
  final Duration timeout;

  /// Nguồn mã ngữ cảnh KN (tùy chọn; App HTX / App MKG không dùng). Gán được
  /// sau khi tạo (app hiện trường nối sau khi mở CSDL).
  ContextIdProvider? contextId;

  /// Tự chọn lại ngữ cảnh khi máy chủ báo hết hạn/thu hồi rồi thử lại một lần.
  ContextRecovery? onContextRejected;

  Uri _u(String path, [Map<String, String>? q]) {
    final b = Uri.parse(baseUrl);
    return b.replace(path: '${b.path}$path', queryParameters: q);
  }

  Future<Map<String, String>> _headers(
    String path, {
    String? idem,
    bool json = true,
  }) async {
    final token = await accessToken();
    final ctx = contextFreePaths.contains(path)
        ? null
        : await contextId?.call();
    return {
      if (token != null) 'Authorization': 'Bearer $token',
      'Idempotency-Key': ?idem,
      'X-Device-Id': ?deviceId,
      if (ctx != null && ctx.isNotEmpty) contextHeader: ctx,
      if (json) 'Content-Type': 'application/json',
      'Accept': 'application/json, application/problem+json',
    };
  }

  /// Một lời gọi: dựng header (kèm `X-AG-Context`), đổi lỗi mạng thành
  /// [NetworkException], lỗi HTTP thành [ApiException]. Ngữ cảnh bị từ chối →
  /// [onContextRejected] chọn lại, dựng lại header và thử lại đúng một lần.
  Future<http.Response> _call(
    String path,
    Future<http.Response> Function(Map<String, String> headers) f, {
    String? idem,
    bool json = true,
  }) async {
    var retried = false;
    while (true) {
      final h = await _headers(path, idem: idem, json: json);
      try {
        return await _send(() => f(h));
      } on ApiException catch (e) {
        final recover = onContextRejected;
        if (retried ||
            recover == null ||
            !e.isContextError ||
            contextFreePaths.contains(path)) {
          rethrow;
        }
        retried = true;
        final next = await recover(e);
        if (next == null || next.isEmpty) rethrow;
      }
    }
  }

  Future<http.Response> _send(Future<http.Response> Function() f) async {
    http.Response res;
    try {
      res = await f().timeout(timeout);
    } on SocketException catch (e) {
      throw NetworkException(e);
    } on TimeoutException catch (e) {
      throw NetworkException(e);
    } on http.ClientException catch (e) {
      throw NetworkException(e);
    }
    if (res.statusCode >= 200 && res.statusCode < 300) return res;
    throw _problem(res);
  }

  static ApiException _problem(http.Response res) {
    var code = 'http_${res.statusCode}';
    String? detail;
    try {
      final j = jsonDecode(utf8.decode(res.bodyBytes));
      if (j is Map) {
        final c = j['code'];
        if (c is String && c.isNotEmpty) code = c;
        final d = j['detail'] ?? j['title'];
        if (d is String) detail = d;
      }
    } catch (_) {
      // thân không phải JSON
    }
    if (res.statusCode == 403 &&
        code == DeviceRevokedException.deviceRevokedCode) {
      return DeviceRevokedException();
    }
    if (res.statusCode == 401) return UnauthenticatedException(code);
    return ApiException(res.statusCode, code, detail);
  }

  Map<String, Object?> _json(http.Response r) =>
      (jsonDecode(utf8.decode(r.bodyBytes)) as Map).cast<String, Object?>();

  @override
  Future<void> registerDevice({
    required String deviceId,
    required String platform,
    required String appVersion,
  }) async {
    await _call(
      '/v1/devices',
      idem: 'devreg:$deviceId',
      (h) => _client.post(
        _u('/v1/devices'),
        headers: h,
        body: jsonEncode({
          'device_id': deviceId,
          'platform': platform,
          'app_version': appVersion,
        }),
      ),
    );
  }

  /// Thân lô phải **ổn định từng byte** giữa các lần gửi lại cùng
  /// Idempotency-Key (máy chủ băm thân, khác thân → 422 idempotency_key_reused).
  static String encodePushBody(String deviceId, List<OutboxRecord> records) =>
      jsonEncode({
        'device_id': deviceId,
        'records': [for (final r in records) r.toWire()],
      });

  @override
  Future<List<PushResult>> push({
    required String batchKey,
    required String deviceId,
    required List<OutboxRecord> records,
  }) async {
    final res = await _call(
      pushPath,
      idem: batchKey,
      (h) => _client.post(
        _u(pushPath),
        headers: h,
        body: encodePushBody(deviceId, records),
      ),
    );
    final list = (_json(res)['results'] as List?) ?? const [];
    return list
        .map((e) => PushResult.fromJson((e as Map).cast<String, Object?>()))
        .toList();
  }

  @override
  Future<PullPage> pull({
    String? cursor,
    required int limit,
    List<String>? entities,
  }) async {
    final res = await _call(
      pullPath,
      json: false,
      (h) => _client.get(
        _u(pullPath, {
          'cursor': ?cursor,
          'limit': '$limit',
          if (entities != null && entities.isNotEmpty)
            'entities': entities.join(','),
        }),
        headers: h,
      ),
    );
    return PullPage.fromJson(_json(res));
  }

  @override
  Future<String> uploadFile({
    required String idempotencyKey,
    required Map<String, Object?> meta,
    required Uint8List ciphertext,
    String? fileRef,
  }) async {
    final res = await _call('/v1/files', idem: idempotencyKey, json: false, (
      h,
    ) async {
      final req = http.MultipartRequest('POST', _u('/v1/files'))
        ..headers.addAll(h)
        // `meta` là trường văn bản (không filename), `blob` là phần tệp.
        ..fields['meta'] = jsonEncode(meta)
        ..fields.addAll({'file_ref': ?fileRef})
        ..files.add(
          http.MultipartFile.fromBytes(
            'blob',
            ciphertext,
            filename: 'blob.bin',
          ),
        );
      final streamed = await _client.send(req);
      return http.Response.fromStream(streamed);
    });
    final ref = _json(res)['file_ref'];
    if (ref is! String || ref.isEmpty) {
      throw ApiException(res.statusCode, 'files.missing_file_ref');
    }
    return ref;
  }

  @override
  Future<Map<String, Object?>> getJson(
    String path, {
    Map<String, String>? query,
  }) async {
    final res = await _call(
      path,
      json: false,
      (h) => _client.get(_u(path, query), headers: h),
    );
    return _json(res);
  }

  @override
  Future<void> registerPushToken({
    required String deviceId,
    required String token,
    required String idempotencyKey,
  }) async {
    final path = '/v1/devices/$deviceId/push-token';
    await _call(
      path,
      idem: idempotencyKey,
      (h) => _client.post(
        _u(path),
        headers: h,
        body: jsonEncode({'provider': 'fcm', 'token': token}),
      ),
    );
  }

  @override
  Future<Map<String, Object?>> postAction({
    required String path,
    required String idempotencyKey,
    Map<String, Object?>? body,
  }) => sendJson(
    method: 'POST',
    path: path,
    idempotencyKey: idempotencyKey,
    body: body,
  );

  @override
  Future<Map<String, Object?>> sendJson({
    required String method,
    required String path,
    String? idempotencyKey,
    Map<String, Object?>? body,
  }) async {
    final res = await _call(path, idem: idempotencyKey, (h) async {
      final req = http.Request(method, _u(path))..headers.addAll(h);
      if (body != null) req.body = jsonEncode(body);
      return http.Response.fromStream(await _client.send(req));
    });
    if (res.bodyBytes.isEmpty) return const {};
    try {
      return _json(res);
    } catch (_) {
      return const {};
    }
  }
}
