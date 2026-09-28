import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';

/// Tệp nhận qua `POST /v1/files`.
class ReceivedFile {
  ReceivedFile(this.fileRef, this.meta, this.blob, this.idempotencyKey);
  final String fileRef;
  final Map<String, Object?> meta;
  final Uint8List blob;
  final String idempotencyKey;
}

/// Application API giả chạy **trong tiến trình kiểm thử** (dart:io HttpServer
/// trên 127.0.0.1) — dùng cho kiểm thử tích hợp trên emulator và kiểm thử
/// HTTP thật trên Dart VM. Hành vi theo docs/dev/sync-contract.md:
/// - đăng ký thiết bị, mọi lời gọi đồng bộ bắt buộc `X-Device-Id` đã đăng ký;
/// - push theo lô, chống trùng theo `idem_key`, kết quả từng bản ghi;
/// - pull theo cursor; lệnh idempotent theo `Idempotency-Key`;
/// - `POST /v1/files` multipart (meta, blob, file_ref), kiểm SHA-256.
///
/// [offline] = true: đóng kết nối ngay (máy thấy lỗi mạng).
class MockApiServer {
  MockApiServer._(this._server);
  final HttpServer _server;

  static Future<MockApiServer> start() async {
    final s = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final m = MockApiServer._(s);
    s.listen(m._handle);
    return m;
  }

  String get baseUrl => 'http://127.0.0.1:${_server.port}';

  bool offline = false;

  final registeredDevices = <String>{};
  final pushTokens = <String, String>{};

  /// idem_key → bản ghi đã ghi sổ.
  final ledger = <String, Map<String, Object?>>{};
  final _batchResponses = <String, String>{};
  final files = <String, ReceivedFile>{};
  final actions = <String, String>{}; // Idempotency-Key → path
  final requests = <String>[];
  int pullCalls = 0;

  /// Thay đổi chiều xuống chưa gửi (mỗi thay đổi một version tăng dần).
  final _changes = <Map<String, Object?>>[];

  void addChange(
    String entity,
    String id,
    Map<String, Object?>? data, {
    String op = 'upsert',
    int? version,
  }) => _changes.add({
    'entity': entity,
    'id': id,
    'version': version ?? _changes.length + 1,
    'op': op,
    'data': ?data,
  });

  Iterable<Map<String, Object?>> recordsOf(String entity) =>
      ledger.values.where((r) => r['entity'] == entity);

  Future<void> close() => _server.close(force: true);

  Future<void> _handle(HttpRequest req) async {
    requests.add('${req.method} ${req.uri.path}');
    if (offline) {
      final socket = await req.response.detachSocket(writeHeaders: false);
      socket.destroy();
      return;
    }
    try {
      final body = await _readAll(req);
      final path = req.uri.path;
      final device = req.headers.value('X-Device-Id');
      final idem = req.headers.value('Idempotency-Key');
      if (req.headers.value('Authorization') == null) {
        return await _problem(req, 401, 'unauthenticated');
      }
      if (req.method == 'POST' && path == '/v1/devices') {
        final j = jsonDecode(utf8.decode(body)) as Map;
        registeredDevices.add(j['device_id']! as String);
        return await _json(req, 201, {
          'device_id': j['device_id'],
          'revoked': false,
        });
      }
      final pt = RegExp(r'^/v1/devices/([^/]+)/push-token$').firstMatch(path);
      if (req.method == 'POST' && pt != null) {
        final id = pt.group(1)!;
        if (!registeredDevices.contains(id)) {
          return await _problem(req, 403, 'notify.device_unregistered');
        }
        final j = jsonDecode(utf8.decode(body)) as Map;
        if (j['provider'] != 'fcm') {
          return await _problem(req, 422, 'validation');
        }
        pushTokens[id] = j['token']! as String;
        return await _json(req, 200, {'device_id': id, 'active': true});
      }
      if (path.startsWith('/v1/sync/') || path == '/v1/files') {
        if (device == null || !registeredDevices.contains(device)) {
          return await _problem(req, 403, 'sync.device_unregistered');
        }
      }
      if (req.method == 'GET' && path == '/v1/sync/pull') {
        pullCalls++;
        final cur = int.tryParse(req.uri.queryParameters['cursor'] ?? '') ?? 0;
        final limit =
            int.tryParse(req.uri.queryParameters['limit'] ?? '') ?? 500;
        final page = _changes.skip(cur).take(limit).toList();
        final next = cur + page.length;
        return await _json(req, 200, {
          'changes': page,
          'next_cursor': '$next',
          'has_more': next < _changes.length,
          'server_time': DateTime.now().toUtc().toIso8601String(),
          'reset': false,
        });
      }
      if (req.method == 'POST' && path == '/v1/sync/push') {
        if (idem == null) {
          return await _problem(req, 400, 'idempotency_required');
        }
        final text = utf8.decode(body);
        final prev = _batchResponses[idem];
        if (prev != null) {
          return await _raw(req, 200, prev);
        }
        final j = jsonDecode(text) as Map;
        final results = <Map<String, Object?>>[];
        for (final r in (j['records']! as List).cast<Map<String, Object?>>()) {
          final key = r['idem_key']! as String;
          if (!key.startsWith('dev:$device:')) {
            results.add({
              'id': r['id'],
              'status': 'rejected',
              'code': 'validation_failed',
              'errors': [
                {'field': 'idem_key', 'reason': 'không phải của thiết bị này'},
              ],
            });
            continue;
          }
          if (ledger.containsKey(key)) {
            results.add({'id': r['id'], 'status': 'duplicate'});
            continue;
          }
          if (r['entity'] == 'evidence') {
            final p = r['payload']! as Map;
            if (!files.containsKey(p['file_ref'])) {
              results.add({
                'id': r['id'],
                'status': 'rejected',
                'code': 'mrv.file_not_found',
              });
              continue;
            }
          }
          ledger[key] = r;
          results.add({'id': r['id'], 'status': 'accepted'});
        }
        final out = jsonEncode({'results': results});
        _batchResponses[idem] = out;
        return await _raw(req, 200, out);
      }
      if (req.method == 'POST' && path == '/v1/files') {
        final ct = req.headers.contentType;
        final boundary = ct?.parameters['boundary'];
        if (ct?.mimeType != 'multipart/form-data' || boundary == null) {
          return await _problem(req, 415, 'unsupported_media_type');
        }
        final parts = parseMultipart(body, boundary);
        final meta = jsonDecode(utf8.decode(parts['meta']!)) as Map;
        final blob = parts['blob']!;
        final sha = meta['sha256_ciphertext'];
        if (sha != _sha256Hex(blob)) {
          return await _problem(req, 422, 'sync.file_checksum_mismatch');
        }
        final ref = parts['file_ref'] == null
            ? 'srv-${files.length + 1}'
            : utf8.decode(parts['file_ref']!);
        files[ref] = ReceivedFile(
          ref,
          meta.cast<String, Object?>(),
          blob,
          idem ?? '',
        );
        return await _json(req, 201, {
          'file_ref': ref,
          'sha256_ciphertext': sha,
          'size': blob.length,
        });
      }
      if (req.method == 'POST' &&
          (path.startsWith('/v1/notifications') ||
              path.startsWith('/v1/extension/directives/') ||
              path.startsWith('/v1/documents/'))) {
        if (idem == null) {
          return await _problem(req, 400, 'idempotency_required');
        }
        actions.putIfAbsent(idem, () => path);
        return await _json(req, 200, {'ok': true});
      }
      return await _problem(req, 404, 'not_found');
    } catch (e) {
      return await _problem(req, 500, 'internal_error');
    }
  }

  static Future<Uint8List> _readAll(HttpRequest req) async {
    final b = BytesBuilder(copy: false);
    await for (final c in req) {
      b.add(c);
    }
    return b.takeBytes();
  }

  static Future<void> _raw(HttpRequest req, int status, String body) async {
    req.response
      ..statusCode = status
      ..headers.contentType = ContentType.json
      ..write(body);
    await req.response.close();
  }

  static Future<void> _json(HttpRequest req, int status, Object body) =>
      _raw(req, status, jsonEncode(body));

  static Future<void> _problem(HttpRequest req, int status, String code) async {
    req.response
      ..statusCode = status
      ..headers.contentType = ContentType('application', 'problem+json')
      ..write(jsonEncode({'status': status, 'code': code, 'title': code}));
    await req.response.close();
  }
}

/// Tách multipart/form-data thành tên phần → bytes (đủ cho kiểm thử).
Map<String, Uint8List> parseMultipart(Uint8List body, String boundary) {
  final s = latin1.decode(body);
  final out = <String, Uint8List>{};
  for (final raw in s.split('--$boundary')) {
    if (raw.isEmpty || raw.startsWith('--')) continue;
    final part = raw.startsWith('\r\n') ? raw.substring(2) : raw;
    final sep = part.indexOf('\r\n\r\n');
    if (sep < 0) continue;
    final headers = part.substring(0, sep);
    var content = part.substring(sep + 4);
    if (content.endsWith('\r\n')) {
      content = content.substring(0, content.length - 2);
    }
    final name = RegExp(r'name="([^"]+)"').firstMatch(headers)?.group(1);
    if (name != null) out[name] = Uint8List.fromList(latin1.encode(content));
  }
  return out;
}

String _sha256Hex(Uint8List data) => sha256.convert(data).toString();
