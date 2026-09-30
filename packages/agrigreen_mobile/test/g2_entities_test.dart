import 'package:agrigreen_mobile/core/config.dart';
import 'package:agrigreen_mobile/data/actions/action_queue.dart';
import 'package:agrigreen_mobile/data/outbox/outbox_repository.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/harness.dart';

/// Mở rộng G2 của lõi cho App HTX — tương thích ngược với app hiện trường.
void main() {
  late Harness h;
  setUp(() async => h = await Harness.create());
  tearDown(() => h.dispose());

  test(
    'entity chiều lên G2 (pf/cropcycle, pf/extension) vào hàng đợi và push',
    () async {
      for (final e in PushEntity.g2) {
        await h.outbox.enqueue(entity: e, payload: {'x': e});
      }
      expect(
        () => h.outbox.enqueue(entity: 'khong_co', payload: const {}),
        throwsA(isA<OutboxException>()),
      );
      await h.sync.syncOnce(force: true);
      expect(
        h.fake.ledger.values.map((w) => w['entity']).toSet(),
        PushEntity.g2,
      );
      expect(PushEntity.all.containsAll(PushEntity.g1), isTrue);
    },
  );

  test('entity chiều lên G3-2 htx_collection_response (RB-6 chiều mua) vào hàng đợi và push', () async {
    await h.outbox.enqueue(
      entity: PushEntity.htxCollectionResponse,
      payload: {'statement_ref': 's-1', 'revision': 1, 'outcome': 'confirmed'},
    );
    await h.sync.syncOnce(force: true);
    expect(h.fake.ledger.values.map((w) => w['entity']).toSet(), {
      PushEntity.htxCollectionResponse,
    });
    expect(PushEntity.all.containsAll(PushEntity.g3), isTrue);
  });

  test(
    'lệnh duyệt nhật ký xếp hàng → POST /v1/cropcycle/farm-logs/{id}/reviews',
    () async {
      await h.actions.enqueue(
        ApiActionKind.farmLogReview,
        target: 'log-1',
        body: const {'decision': 'approved'},
      );
      await h.sync.syncOnce(force: true);
      expect(
        h.fake.actionCalls.single.$1,
        '/v1/cropcycle/farm-logs/log-1/reviews',
      );
      expect(
        (await h.actions.ofKind(ApiActionKind.farmLogReview)).single.status,
        ApiActionStatus.done,
      );
    },
  );

  test(
    'FakeServer.onGet / onPost có tham số truy vấn và phát lại theo khóa',
    () async {
      h.fake.onGet = (path, q) async =>
          path == '/v1/farmer/coop-staff' ? {'q': q?['q']} : null;
      expect(
        await h.fake.getJson('/v1/farmer/coop-staff', query: {'q': 'kế toán'}),
        {'q': 'kế toán'},
      );
      h.fake.onPost = (path, body) async => {'n': body?['n']};
      final a = await h.fake.postAction(
        path: '/p',
        idempotencyKey: 'k1',
        body: {'n': 1},
      );
      final b = await h.fake.postAction(
        path: '/p',
        idempotencyKey: 'k1',
        body: {'n': 2},
      );
      expect(a, {'n': 1});
      expect(b, {'n': 1});
    },
  );

  test('AppConfig.fromEnvironment: mặc định app hiện trường, App HTX truyền client riêng', () {
    final field = AppConfig.fromEnvironment();
    expect(field.oidcClientId, 'agrigreen-field-app');
    expect(field.oidcRedirectUrl, 'vn.agrigreen.field:/oauth2redirect');
    final htx = AppConfig.fromEnvironment(
      oidcClientId: 'agrigreen-htx-app',
      oidcRedirectUrl: 'vn.agrigreen.htx:/oauth2redirect',
    );
    expect(htx.oidcClientId, 'agrigreen-htx-app');
    expect(htx.oidcRedirectUrl, 'vn.agrigreen.htx:/oauth2redirect');
  });
}
