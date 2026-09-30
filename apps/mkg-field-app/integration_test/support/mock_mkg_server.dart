import 'dart:async';
import 'dart:convert';
import 'dart:io';

/// api-internal (mkg/fieldops) giả chạy **trong tiến trình kiểm thử**
/// (dart:io HttpServer trên 127.0.0.1) — cho kiểm thử tích hợp trên emulator
/// và kiểm thử HTTP thật trên Dart VM. Hành vi theo
/// `internal/mkg/fieldops/app/sync.go` + contracts/openapi/mkg-internal.yaml:
/// - `X-Device-Id` bắt buộc, trùng `device_id` của lô (403 fieldops.device_mismatch);
/// - push `roll_count` (create | correct), chống trùng theo `idem_key`,
///   Idempotency-Key theo lô (gửi lại cùng thân → phát lại phản hồi);
/// - pull `collection_order` theo con trỏ mờ gắn thiết bị
///   (400 fieldops.cursor_invalid khi sai);
/// - GET roll-counts (số hiệu lực suy từ sổ), crew-shifts, POST crew-shifts.
///
/// [offline] = true: đóng kết nối ngay (máy thấy lỗi mạng).
class MockMkgServer {
  MockMkgServer._(this._server);
  final HttpServer _server;

  static Future<MockMkgServer> start() async {
    final s = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final m = MockMkgServer._(s);
    s.listen(m._handle);
    return m;
  }

  String get baseUrl => 'http://127.0.0.1:${_server.port}';
  bool offline = false;

  /// idem_key → bản ghi đã ghi sổ (theo thứ tự nhận).
  final ledger = <String, Map<String, Object?>>{};
  final _batchResponses = <String, String>{};
  final _batchBodies = <String, String>{};
  final shifts = <String, List<Map<String, Object?>>>{};
  final _shiftKeys = <String>{};
  final requests = <String>[];
  final orders = <Map<String, Object?>>[];

  Iterable<Map<String, Object?>> get rollCounts => ledger.values;

  void addOrder(Map<String, Object?> data) => orders.add(data);

  String _cursor(String device, int seq) =>
      base64Url.encode(utf8.encode('$device|$seq'));

  int? _decode(String device, String? c) {
    if (c == null || c.isEmpty) return 0;
    try {
      final p = utf8.decode(base64Url.decode(c)).split('|');
      if (p.length != 2 || p[0] != device) return null;
      return int.parse(p[1]);
    } catch (_) {
      return null;
    }
  }

  Future<void> close() => _server.close(force: true);

  Future<void> _json(HttpRequest req, int status, Object body) async {
    req.response
      ..statusCode = status
      ..headers.contentType = ContentType.json
      ..write(jsonEncode(body));
    await req.response.close();
  }

  Future<void> _problem(HttpRequest req, int status, String code) =>
      _json(req, status, {'status': status, 'code': code, 'title': code});

  Future<void> _handle(HttpRequest req) async {
    if (offline) {
      await req.response.detachSocket().then((s) => s.destroy());
      return;
    }
    final path = req.uri.path;
    requests.add('${req.method} $path');
    final device = req.headers.value('X-Device-Id');
    final body = await utf8.decodeStream(req);
    try {
      if (req.method == 'POST' && path == '/v1/fieldops/sync/push') {
        final key = req.headers.value('Idempotency-Key');
        if (key == null) {
          return await _problem(req, 400, 'idempotency_key_required');
        }
        final prev = _batchBodies[key];
        if (prev != null) {
          if (prev != body) {
            return await _problem(req, 422, 'idempotency_key_reused');
          }
          req.response
            ..statusCode = 200
            ..headers.contentType = ContentType.json
            ..write(_batchResponses[key]);
          return await req.response.close();
        }
        final b = jsonDecode(body) as Map<String, Object?>;
        if (device == null || b['device_id'] != device) {
          return await _problem(req, 403, 'fieldops.device_mismatch');
        }
        final results = <Map<String, Object?>>[];
        for (final r in (b['records']! as List).cast<Map<String, Object?>>()) {
          final id = r['id']! as String;
          final idem = r['idem_key']! as String;
          if (!idem.startsWith('dev:$device:')) {
            results.add({
              'id': id,
              'status': 'rejected',
              'code': 'validation_failed',
            });
          } else if (r['entity'] != 'roll_count') {
            results.add({
              'id': id,
              'status': 'rejected',
              'code': 'fieldops.unknown_entity',
            });
          } else if (ledger.containsKey(idem)) {
            results.add({'id': id, 'status': 'duplicate'});
          } else {
            ledger[idem] = r;
            results.add({'id': id, 'status': 'accepted'});
          }
        }
        final out = jsonEncode({'results': results});
        _batchBodies[key] = body;
        _batchResponses[key] = out;
        req.response
          ..statusCode = 200
          ..headers.contentType = ContentType.json
          ..write(out);
        return await req.response.close();
      }
      if (req.method == 'GET' && path == '/v1/fieldops/sync/pull') {
        if (device == null) {
          return await _problem(req, 403, 'fieldops.device_mismatch');
        }
        final seq = _decode(device, req.uri.queryParameters['cursor']);
        if (seq == null) {
          return await _problem(req, 400, 'fieldops.cursor_invalid');
        }
        final changes = [
          for (var i = seq; i < orders.length; i++)
            {
              'entity': 'collection_order',
              'id': orders[i]['order_id'],
              'version': i + 1,
              'op': 'upsert',
              'data': orders[i],
            },
        ];
        return await _json(req, 200, {
          'changes': changes,
          'next_cursor': _cursor(device, orders.length),
          'has_more': false,
          'server_time': DateTime.now().toUtc().toIso8601String(),
          'reset': false,
        });
      }
      final m = RegExp(
        r'^/v1/fieldops/orders/([^/]+)/(roll-counts|crew-shifts)$',
      ).firstMatch(path);
      if (m != null) {
        final order = m.group(1)!;
        if (m.group(2) == 'roll-counts' && req.method == 'GET') {
          final eff = <String, Map<String, Object?>>{};
          for (final w in ledger.values) {
            final p = (w['payload']! as Map).cast<String, Object?>();
            if (p['order_id'] != order) continue;
            eff['${p['plot_ref']}|${p['stage']}'] = {
              'plot_ref': p['plot_ref'],
              'stage': p['stage'],
              'rolls': p['rolls'],
              'loose_kg': p['loose_kg'],
              'discrepancy_reason': p['discrepancy_reason'],
              'roll_count_id': w['id'],
            };
          }
          return await _json(req, 200, {
            'counts': <Object?>[],
            'totals': eff.values.toList(),
          });
        }
        if (req.method == 'GET') {
          return await _json(req, 200, {'items': shifts[order] ?? const []});
        }
        final key = req.headers.value('Idempotency-Key') ?? '';
        final b = jsonDecode(body) as Map<String, Object?>;
        if (_shiftKeys.add(key)) {
          shifts.putIfAbsent(order, () => []).add({...b, 'order_id': order});
        }
        return await _json(req, 201, b);
      }
      return await _problem(req, 404, 'not_found');
    } catch (e) {
      return await _problem(req, 500, 'internal_error');
    }
  }
}
