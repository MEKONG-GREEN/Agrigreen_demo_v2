import 'dart:convert';
import 'dart:io';

import 'package:agrigreen_field/application/app_services.dart';
import 'package:agrigreen_field/application/kn_context_service.dart';
import 'package:agrigreen_field/application/kn_report_service.dart';
import 'package:agrigreen_field/domain/kn_report.dart';
import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/core/config.dart';
import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/photo/photo_pipeline.dart';
import 'package:agrigreen_mobile/data/secure/secure_store.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'support/ffi_opener.dart';
import 'support/kn_fixtures.dart';

class _NoCamera implements CameraSource {
  @override
  Future<CapturedImage?> capture() async => null;
}

/// Application API giả (P1 ngữ cảnh + pull/push KN) sau `MockClient`: kiểm
/// header `X-AG-Context` của từng lời gọi qua `HttpSyncApi` thật.
class _Backend {
  final calls = <(String method, String path, String? ctx)>[];
  final expired = <String>{'ctx-1'};
  int _ctx = 0;
  final _changes = <Map<String, Object?>>[];
  final pushed = <Map<String, Object?>>[];

  void add(String entity, String id, Map<String, Object?> data) =>
      _changes.add({
        'entity': entity,
        'id': id,
        'version': _changes.length + 1,
        'op': 'upsert',
        'data': data,
      });

  http.Response _j(int status, Object body) => http.Response(
    jsonEncode(body),
    status,
    headers: {'content-type': 'application/json; charset=utf-8'},
  );

  Future<http.Response> handle(http.Request req) async {
    final path = req.url.path;
    final ctx = req.headers['X-AG-Context'];
    calls.add((req.method, path, ctx));
    if (path == '/v1/me/contexts') {
      return _j(200, {
        'assignments': [assignment('a-1')],
      });
    }
    if (path == '/v1/me/context') {
      final b = jsonDecode(req.body) as Map;
      _ctx++;
      return _j(201, {
        'context_id': 'ctx-$_ctx',
        'assignment_id': b['assignment_id'],
        'role': 'kn_xa',
        'department_code': b['department_code'],
        'department_name': 'Trồng trọt',
        'org_ref': null,
        'org_name': 'TTKN Cần Thơ',
        'communes': [
          {'code': xaA, 'name': 'Xã A'},
        ],
        'default_route': '/kn/nhiem-vu/cong-viec',
        'expires_at': '2099-01-01T00:00:00Z',
        'support_session_id': null,
        'on_behalf_of': null,
      });
    }
    if (ctx == null) return _j(428, {'code': 'accounts.context_required'});
    if (expired.contains(ctx)) {
      return _j(403, {'status': 403, 'code': 'accounts.context_expired'});
    }
    if (req.method == 'POST' && path == '/v1/devices') {
      return _j(201, {'device_id': 'd', 'revoked': false});
    }
    if (req.method == 'GET' && path == '/v1/sync/pull') {
      final cur = int.tryParse(req.url.queryParameters['cursor'] ?? '') ?? 0;
      final page = _changes.skip(cur).toList();
      return _j(200, {
        'changes': page,
        'next_cursor': '${cur + page.length}',
        'has_more': false,
        'reset': false,
      });
    }
    if (req.method == 'POST' && path == '/v1/sync/push') {
      final b = jsonDecode(req.body) as Map;
      final results = <Map<String, Object?>>[];
      for (final r in (b['records'] as List).cast<Map<String, Object?>>()) {
        pushed.add(r);
        final p = (r['payload']! as Map).cast<String, Object?>();
        final input = ((p['rows']! as List).first as Map)['input'] as Map;
        final e = double.parse('${input['E'] ?? 0}');
        // Máy chủ tính ô công thức G = D + E (+F) rồi ghi bài nộp mới nhất.
        add(
          KnEntity.reportSubmission,
          '${p['task_id']}',
          submissionData(
            taskId: '${p['task_id']}',
            submissionId: '${r['id']}',
            input: {for (final x in input.entries) '${x.key}': '${x.value}'},
            cells: {'G': '${12 + e}'},
          ),
        );
        results.add({'id': r['id'], 'status': 'accepted'});
      }
      return _j(200, {'results': results});
    }
    return _j(404, {'status': 404, 'code': 'not_found'});
  }
}

void main() {
  test('KN P1/P7 qua HTTP: X-AG-Context ở mọi lời gọi; hết hạn → chọn lại + gọi lại; nhập → push → pull ô đã tính', () async {
    final be = _Backend()
      ..add(KnEntity.formLayout, ver1, layoutData())
      ..add(KnEntity.reportTask, taskA2, taskData(id: taskA2, commune: xaA))
      ..add(
        KnEntity.reportTask,
        taskA1,
        taskData(id: taskA1, period: period1, slot: 1, status: 'submitted'),
      )
      ..add(
        KnEntity.reportSubmission,
        taskA1,
        submissionData(
          taskId: taskA1,
          submissionId: 'sub-a1',
          input: const {'D': '12'},
        ),
      );
    final dir = await Directory.systemTemp.createTemp('ag_kn_http_');
    final pf = AppPlatform(
      config: const AppConfig(
        apiBaseUrl: 'https://api.test',
        oidcIssuer: 'https://id.test',
      ),
      store: MemorySecureStore(),
      clock: const SystemClock(),
      ids: UuidV7Generator(),
      opener: FfiOpener(),
      dataDir: dir,
      apiFactory: (token, deviceId) => HttpSyncApi(
        baseUrl: 'https://api.test',
        accessToken: token,
        deviceId: deviceId,
        client: MockClient(be.handle),
      ),
      camera: _NoCamera(),
      location: const FixedLocationSource(LocationResult(GpsStatus.denied)),
    );
    final s = await AppServices.open(pf, accessToken: () async => 'tok');
    try {
      final st = await s.knContext.resolve();
      expect(st.stage, KnContextStage.ready);
      expect(await s.knContext.contextId(), 'ctx-1');

      // ctx-1 đã hết hạn phía máy chủ: lời gọi đầu nhận 403 → chọn lại → ctx-2.
      await s.sync.syncOnce(force: true);
      expect(await s.knContext.contextId(), 'ctx-2');
      expect(await s.refs.count(KnEntity.reportTask), 2);

      final t = (await s.knReports.task(taskA2))!;
      final e = (await s.knReports.entry(t))!;
      await s.knReports.queue(e, {
        knCellKey(xaA, 'E'): '3.5',
        knCellKey(xaA, 'H'): '7',
      }, submit: true);
      await s.sync.syncOnce(
        force: true,
        entities: KnReportService.focusedEntities,
      );
      final after = (await s.knReports.entry(t))!;
      expect(be.pushed.single['entity'], 'report_submission');
      expect(after.formulaPending, isFalse);
      expect(
        after.displayValue(after.rows.single, after.layout.column('G')!),
        '15.5',
      );

      // Header: mọi lời gọi (trừ chọn ngữ cảnh) mang X-AG-Context.
      for (final (m, p, ctx) in be.calls) {
        if (p == '/v1/me/contexts' || p == '/v1/me/context') {
          expect(ctx, isNull, reason: '$m $p');
        } else {
          expect(ctx, isNotNull, reason: '$m $p');
        }
      }
      final first = be.calls.indexWhere((c) => c.$3 == 'ctx-1');
      expect(be.calls[first + 1].$2, '/v1/me/contexts'); // recover
      expect(be.calls[first + 2].$2, '/v1/me/context');
      expect(be.calls[first + 3], (
        be.calls[first].$1,
        be.calls[first].$2,
        'ctx-2',
      ));
      expect(
        be.calls.where((c) => c.$3 == 'ctx-1'),
        hasLength(1),
        reason: 'thử lại một lần với ngữ cảnh mới',
      );
    } finally {
      await s.close();
      await dir.delete(recursive: true);
    }
  });
}
