import 'dart:convert';
import 'dart:math';

import 'package:agrigreen_mobile/application/boundary_service.dart';
import 'package:agrigreen_mobile/data/actions/action_queue.dart';
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
        path: '/v1/kn/households/h1',
        idempotencyKey: 'act:2',
        body: const {'cultivated_area_ha': '1.5'},
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

    test('FN-XA-03.03 lệnh PUT xếp hàng được phát lại bằng sendJson', () async {
      final a = await h.actions.enqueue(
        ApiActionKind.knCooperativeUpdate,
        target: 'c1',
        body: const {'name': 'HTX Mới'},
      );
      expect(a.method, 'PUT');
      expect(a.path, '/v1/kn/cooperatives/c1');
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
