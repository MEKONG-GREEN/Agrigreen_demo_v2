import 'dart:convert';
import 'dart:math';

import 'package:agrigreen_mobile/application/boundary_service.dart';
import 'package:agrigreen_mobile/data/actions/action_queue.dart';
import 'package:agrigreen_mobile/data/db/schema.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/domain/geo/boundary.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'support/harness.dart';

/// Máy chủ giả ghi lại header; [expired] = mã ngữ cảnh bị máy chủ từ chối.
class _Srv {
  final seen = <(String, String, String?, String?)>[];
  final expired = <String>{};
  int status = 403;

  Future<http.Response> handle(http.Request r) async {
    final ctx = r.headers['X-AG-Context'];
    seen.add((r.method, r.url.path, ctx, r.headers['Idempotency-Key']));
    if (ctx != null && expired.contains(ctx)) {
      return http.Response(
        jsonEncode({
          'status': status,
          'code': status == 428
              ? 'accounts.context_required'
              : 'accounts.context_expired',
        }),
        status,
      );
    }
    if (r.url.path == '/v1/sync/pull') {
      return http.Response(
        jsonEncode({'changes': [], 'next_cursor': 'c', 'has_more': false}),
        200,
      );
    }
    if (r.url.path == '/v1/sync/push') {
      return http.Response(jsonEncode({'results': []}), 200);
    }
    return http.Response(jsonEncode({'ok': r.method, 'body': r.body}), 200);
  }
}

void main() {
  group('KN P1 X-AG-Context (HttpSyncApi)', () {
    test('gửi ở mọi lời gọi; không gửi khi chọn ngữ cảnh; không cấu hình → không gửi (App HTX/MKG)', () async {
      final s = _Srv();
      var ctx = 'ctx-1';
      final api = HttpSyncApi(
        baseUrl: 'https://api.test',
        accessToken: () async => 'tok',
        deviceId: 'dev1',
        client: MockClient(s.handle),
        contextId: () async => ctx,
      );
      await api.pull(limit: 10);
      await api.push(batchKey: 'b1', deviceId: 'dev1', records: const []);
      await api.getJson('/v1/menu');
      await api.postAction(path: '/v1/x', idempotencyKey: 'act:1');
      await api.sendJson(
        method: 'PUT',
        path: '/v1/kn/plots/p1',
        idempotencyKey: 'act:2',
        body: const {'note': 'p1'},
      );
      await api.registerDevice(
        deviceId: 'dev1',
        platform: 'android',
        appVersion: '1',
      );
      await api.getJson('/v1/me/contexts');
      await api.postAction(path: '/v1/me/context', idempotencyKey: 'ctx:1');
      expect(
        [for (final c in s.seen) c.$3],
        ['ctx-1', 'ctx-1', 'ctx-1', 'ctx-1', 'ctx-1', 'ctx-1', null, null],
      );
      expect(s.seen[4].$1, 'PUT');
      ctx = 'ctx-9';
      await api.getJson('/v1/menu');
      expect(s.seen.last.$3, 'ctx-9');

      final plain = HttpSyncApi(
        baseUrl: 'https://api.test',
        accessToken: () async => 'tok',
        client: MockClient(s.handle),
      );
      await plain.pull(limit: 10);
      expect(s.seen.last.$3, isNull);
    });

    test(
      '403 context_expired → chọn lại → thử lại MỘT lần cùng Idempotency-Key',
      () async {
        final s = _Srv()..expired.add('ctx-1');
        var ctx = 'ctx-1';
        final rejected = <String>[];
        final api = HttpSyncApi(
          baseUrl: 'https://api.test',
          accessToken: () async => 'tok',
          client: MockClient(s.handle),
          contextId: () async => ctx,
          onContextRejected: (e) async {
            rejected.add(e.code);
            ctx = 'ctx-2';
            return ctx;
          },
        );
        final r = await api.postAction(path: '/v1/x', idempotencyKey: 'act:7');
        expect(r['ok'], 'POST');
        expect(rejected, ['accounts.context_expired']);
        expect(
          [for (final c in s.seen) (c.$3, c.$4)],
          [('ctx-1', 'act:7'), ('ctx-2', 'act:7')],
        );

        // Ngữ cảnh mới cũng bị từ chối → không lặp vô hạn.
        s.expired.add('ctx-2');
        ctx = 'ctx-2';
        final api2 = HttpSyncApi(
          baseUrl: 'https://api.test',
          accessToken: () async => 'tok',
          client: MockClient(s.handle),
          contextId: () async => ctx,
          onContextRejected: (_) async => ctx,
        );
        await expectLater(
          api2.pull(limit: 1),
          throwsA(isA<ApiException>().having((e) => e.status, 'status', 403)),
        );
      },
    );

    test(
      '428 context_required cũng chọn lại; không chọn được → ném lỗi gốc',
      () async {
        final s = _Srv()
          ..expired.add('ctx-1')
          ..status = 428;
        final api = HttpSyncApi(
          baseUrl: 'https://api.test',
          accessToken: () async => 'tok',
          client: MockClient(s.handle),
          contextId: () async => 'ctx-1',
          onContextRejected: (_) async => null,
        );
        await expectLater(
          api.getJson('/v1/menu'),
          throwsA(
            isA<ApiException>()
                .having((e) => e.isContextError, 'isContextError', isTrue)
                .having((e) => e.code, 'code', 'accounts.context_required'),
          ),
        );
        expect(
          ApiException(403, 'sync.device_revoked').isContextError,
          isFalse,
        );
      },
    );
  });

  group('KN P7 lõi', () {
    late Harness h;
    setUp(() async => h = await Harness.create());
    tearDown(() => h.dispose());

    test('FN-XA-03.04 lệnh PUT xếp hàng được phát lại bằng sendJson', () async {
      final a = await h.actions.enqueue(
        ApiActionKind.knPlotUpdate,
        target: 'p1',
        body: const {'name': 'Thửa 1'},
      );
      expect(a.method, 'PUT');
      expect(a.path, '/v1/kn/plots/p1');
      await h.sync.syncOnce(force: true);
      expect(h.fake.sendCalls.single.$1, 'PUT');
      expect(h.fake.sendCalls.single.$3, a.idempotencyKey);
      expect(h.fake.actionCalls, isEmpty);
      expect(await h.actions.pendingCount(), 0);
      expect(
        ApiActionKind.knMaterialCorrect.pathFor('m1'),
        '/v1/kn/materials/m1/correct',
      );
    });

    test('#161 hồ sơ KN riêng: mọi lệnh là POST /v1/kn/producer-records…, không còn route ghi HTX nền tảng', () async {
      expect(
        ApiActionKind.knRecordCreate.pathFor(null),
        '/v1/kn/producer-records',
      );
      expect(
        ApiActionKind.knRecordUpdate.pathFor('r1'),
        '/v1/kn/producer-records/r1',
      );
      expect(
        ApiActionKind.knRecordDeactivate.pathFor('r1'),
        '/v1/kn/producer-records/r1/deactivate',
      );
      expect(
        ApiActionKind.knRecordReactivate.pathFor('r1'),
        '/v1/kn/producer-records/r1/reactivate',
      );
      for (final k in ApiActionKind.values) {
        final path = k.pathFor('x');
        expect(path, isNot(contains('/v1/kn/cooperatives')), reason: k.code);
        expect(path, isNot(contains('/v1/kn/households')), reason: k.code);
        if (k.name.startsWith('knRecord')) expect(k.method, 'POST');
      }
      final a = await h.actions.enqueue(
        ApiActionKind.knRecordUpdate,
        target: 'r1',
        body: const {'kind': 'cooperative', 'base_version': 2},
      );
      await h.sync.syncOnce(force: true);
      expect(h.fake.actionCalls.single.$1, '/v1/kn/producer-records/r1');
      expect(h.fake.actionCalls.single.$2, a.idempotencyKey);
      expect(h.fake.sendCalls, isEmpty);
    });

    test('#161 lệnh ghi HTX cũ còn trong máy: không phát lại, migration v4 bỏ (retired), không xếp hàng mới', () async {
      Future<void> legacy(String id, int seq, String kind) =>
          h.db.db.insert('api_action', {
            'id': id,
            'seq': seq,
            'kind': kind,
            'target': 'c1',
            'body': jsonEncode({'name': 'HTX'}),
            'status': 'pending',
            'created_at': h.clock.now().millisecondsSinceEpoch,
          });
      await legacy('old-1', 900, 'kn_cooperative_update');
      await legacy('old-2', 901, 'kn_household_create');
      // Dòng cũ đọc được (lớp phủ) nhưng không vào danh sách phát lại.
      expect(await h.actions.active(), hasLength(2));
      expect(await h.actions.pending(), isEmpty);
      await h.sync.syncOnce(force: true);
      expect(h.fake.actionCalls, isEmpty);
      expect(h.fake.sendCalls, isEmpty);
      // Máy nâng cấp từ lược đồ v3: lệnh cũ còn chờ → failed 'retired'.
      await Schema.upgrade(h.db.db, 3, Schema.version);
      expect(await h.actions.pendingCount(), 0);
      final old = (await h.actions.byId('old-1'))!;
      expect(old.status, ApiActionStatus.failed);
      expect(old.lastError, RetiredActionKind.retiredCode);
      expect(() => old.path, throwsStateError);
      await expectLater(
        h.actions.enqueue(RetiredActionKind.values.first, target: 'c1'),
        throwsArgumentError,
      );
    });

    test(
      'FN-XA-03.02 report_submission là thực thể chiều lên hợp lệ',
      () async {
        final r = await h.outbox.enqueue(
          entity: PushEntity.reportSubmission,
          payload: const {'task_id': 't', 'kind': 'draft', 'rows': []},
        );
        expect(r.entity, 'report_submission');
      },
    );

    test('FN-XA-03.05 hình tròn / hình vuông → đa giác; diện tích tự tính', () {
      const c = GeoPoint(10.04, 105.77);
      final circle = circlePolygon(c, 50);
      expect(circle, hasLength(32));
      final v = validateBoundary(circle);
      expect(v.ok, isTrue);
      // π·50² ≈ 7854 m² → 0,785 ha (đa giác 32 cạnh hơi nhỏ hơn).
      expect(v.areaHa, closeTo(pi * 2500 / 10000, 0.01));
      expect(distanceM(c, circle.first), closeTo(50, 0.5));

      final corner = offsetM(c, 30, 30);
      final sq = squarePolygon(c, corner);
      expect(sq, hasLength(4));
      final vs = validateBoundary(sq);
      expect(vs.ok, isTrue);
      // Cạnh = 60 m → 3600 m².
      expect(vs.areaHa, closeTo(0.36, 0.005));
    });

    test('FN-XA-03.05 (A2 03/10/2026): tam giác 3 điểm hợp lệ; trùng/thẳng hàng chặn', () {
      expect(kMinBoundaryPoints, 3);
      const a = GeoPoint(10.04, 105.77);
      final tri = validateBoundary([a, offsetM(a, 60, 0), offsetM(a, 0, 60)]);
      expect(tri.ok, isTrue);
      // Tam giác vuông cạnh 60 m → 1800 m².
      expect(tri.areaHa, closeTo(0.18, 0.005));
      expect(
        validateBoundary([a, offsetM(a, 60, 0), offsetM(a, 60, 0)]).errors,
        [BoundaryError.tooFewPoints],
      );
      expect(
        validateBoundary([a, offsetM(a, 30, 0), offsetM(a, 60, 0)]).errors,
        [BoundaryError.collinear],
      );
    });

    test('FN-XA-03.05 payload plot_boundary mang lot_name, note, farmer_name, shape', () async {
      final svc = BoundaryService(outbox: h.outbox, refs: h.refs, ids: h.ids);
      final rec = await svc.save(
        points: squarePolygon(
          const GeoPoint(10.1, 105.8),
          offsetM(const GeoPoint(10.1, 105.8), 40, 40),
        ),
        method: BoundaryMethod.map,
        communeCode: '31117',
        lotName: ' Lô bờ kênh ',
        farmerName: 'Nguyễn Văn B',
        note: 'gần cống',
        shape: PlotShape.square.name,
      );
      expect(rec.payload['lot_name'], 'Lô bờ kênh');
      expect(rec.payload['farmer_name'], 'Nguyễn Văn B');
      expect(rec.payload['note'], 'gần cống');
      expect(rec.payload['shape'], 'square');
      expect((rec.payload['geometry']! as Map)['type'], 'Polygon');
      final old = await svc.save(
        points: squarePolygon(
          const GeoPoint(10.2, 105.8),
          offsetM(const GeoPoint(10.2, 105.8), 40, 40),
        ),
        method: BoundaryMethod.map,
        communeCode: '31117',
      );
      expect(old.payload.containsKey('lot_name'), isFalse);
      expect(old.payload.containsKey('shape'), isFalse);
    });
  });
}
