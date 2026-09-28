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

  Future<PullPage> pull({String? cursor, required int limit});

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
}

typedef AccessTokenProvider = Future<String?> Function();

class HttpSyncApi implements SyncApi {
  HttpSyncApi({
    required this.baseUrl,
    required this.accessToken,
    this.deviceId,
    http.Client? client,
    this.timeout = const Duration(seconds: 30),
  }) : _client = client ?? http.Client();

  final String baseUrl;
  final AccessTokenProvider accessToken;

  /// Gửi trong header `X-Device-Id` của mọi lời gọi (sync-contract).
  final String? deviceId;
  final http.Client _client;
  final Duration timeout;

  Uri _u(String path, [Map<String, String>? q]) {
    final b = Uri.parse(baseUrl);
    return b.replace(path: '${b.path}$path', queryParameters: q);
  }

  Future<Map<String, String>> _headers({String? idem, bool json = true}) async {
    final token = await accessToken();
    return {
      if (token != null) 'Authorization': 'Bearer $token',
      'Idempotency-Key': ?idem,
      'X-Device-Id': ?deviceId,
      if (json) 'Content-Type': 'application/json',
      'Accept': 'application/json, application/problem+json',
    };
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
    final h = await _headers(idem: 'devreg:$deviceId');
    await _send(
      () => _client.post(
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
    final h = await _headers(idem: batchKey);
    final res = await _send(
      () => _client.post(
        _u('/v1/sync/push'),
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
  Future<PullPage> pull({String? cursor, required int limit}) async {
    final h = await _headers(json: false);
    final res = await _send(
      () => _client.get(
        _u('/v1/sync/pull', {'cursor': ?cursor, 'limit': '$limit'}),
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
    final h = await _headers(idem: idempotencyKey, json: false);
    final res = await _send(() async {
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
    final h = await _headers(json: false);
    final res = await _send(() => _client.get(_u(path, query), headers: h));
    return _json(res);
  }

  @override
  Future<void> registerPushToken({
    required String deviceId,
    required String token,
    required String idempotencyKey,
  }) async {
    final h = await _headers(idem: idempotencyKey);
    await _send(
      () => _client.post(
        _u('/v1/devices/$deviceId/push-token'),
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
  }) async {
    final h = await _headers(idem: idempotencyKey);
    final res = await _send(
      () => _client.post(
        _u(path),
        headers: h,
        body: body == null ? null : jsonEncode(body),
      ),
    );
    if (res.bodyBytes.isEmpty) return const {};
    try {
      return _json(res);
    } catch (_) {
      return const {};
    }
  }
}
