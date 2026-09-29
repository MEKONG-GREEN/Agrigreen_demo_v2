import 'package:agrigreen_mobile/application/inbox_service.dart';
import 'package:agrigreen_mobile/application/push_coordinator.dart';
import 'package:agrigreen_mobile/data/push/push_messaging.dart';
import 'package:agrigreen_mobile/data/reference/reference_repository.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/domain/deep_link.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/harness.dart';

PullChange _up(String entity, String id, int v) => PullChange(
  entity: entity,
  id: id,
  version: v,
  op: 'upsert',
  data: {'label': '$entity $id'},
);

/// Host đẩy nối thẳng vào SyncService của harness (như app thật).
class _SyncHost implements PushHost {
  _SyncHost(this.h);
  final Harness h;
  @override
  Future<InboxItem?> notification(String id) async => null;
  @override
  Future<void> setPushToken(String token) async {}
  @override
  Future<void> syncNow({Set<String>? entities}) async {
    await h.sync.syncOnce(force: true, entities: entities);
  }
}

void main() {
  group('KN US-SYNC-02 / HTX: sync_now có entities[] → chỉ pull entity đó', () {
    test('PushMessage.entities: tách dấu phẩy, bỏ khoảng trắng; sai dạng / '
        'quá 30 / rỗng → null (pull toàn bộ — tương thích ngược)', () {
      expect(
        const PushMessage({
          'kind': 'sync_now',
          'entities': 'plot, task,,farm_log',
        }).entities,
        {'plot', 'task', 'farm_log'},
      );
      expect(const PushMessage({'kind': 'sync_now'}).entities, isNull);
      expect(
        const PushMessage({'kind': 'sync_now', 'entities': ' , '}).entities,
        isNull,
      );
      expect(
        const PushMessage({'kind': 'sync_now', 'entities': 'plot,DROP TABLE'})
            .entities,
        isNull,
      );
      expect(
        PushMessage({
          'kind': 'sync_now',
          'entities': [for (var i = 0; i < 31; i++) 'e$i'].join(','),
        }).entities,
        isNull,
      );
    });

    late Harness h;
    setUp(() async => h = await Harness.create());
    tearDown(() => h.dispose());

    test('syncOnce(entities) gửi ?entities (đã sắp xếp), vẫn push hàng đợi, '
        'áp đúng entity và lưu con trỏ mới; lượt sau không có entities → '
        'pull đầy đủ', () async {
      await h.outbox.enqueue(
        entity: PushEntity.fieldReport,
        payload: const {'kind': 'sowing_progress'},
      );
      h.fake.pages[null] = PullPage(
        changes: [_up('plot', 'p1', 1), _up('task', 't1', 1)],
        nextCursor: 'c1',
        hasMore: false,
      );
      await h.sync.syncOnce(force: true, entities: {'task', 'plot'});
      expect(h.fake.pullEntities.single, ['plot', 'task']);
      expect(h.fake.pushCalls, 1, reason: 'chiều lên vẫn gửi đủ');
      expect(await h.refs.count(RefEntity.plot), 1);
      expect(await h.refs.cursor(), 'c1');

      await h.sync.syncOnce(force: true);
      expect(h.fake.pullEntities.last, isNull);
      expect(h.fake.pullCursors.last, 'c1');
    });

    test('lọc chỉ plot: task trong trang không được áp', () async {
      h.fake.pages[null] = PullPage(
        changes: [_up('plot', 'p1', 1), _up('task', 't1', 1)],
        nextCursor: 'c1',
        hasMore: false,
      );
      await h.sync.syncOnce(force: true, entities: {'plot'});
      expect(await h.refs.count(RefEntity.plot), 1);
      expect(await h.refs.count(RefEntity.task), 0);
    });

    test('trang reset khi pull một phần → bỏ trang, pull đầy đủ (không xóa '
        'dữ liệu entity khác)', () async {
      await h.refs.applyPage(
        PullPage(
          changes: [_up('task', 't0', 1)],
          nextCursor: 'c0',
          hasMore: false,
        ),
      );
      h.fake.pages['c0'] = PullPage(
        changes: [_up('plot', 'p1', 1), _up('task', 't0', 2)],
        nextCursor: 'c1',
        hasMore: false,
        reset: true,
      );
      await h.sync.syncOnce(force: true, entities: {'plot'});
      expect(h.fake.pullEntities, [
        ['plot'],
        null,
      ]);
      expect(await h.refs.count(RefEntity.task), 1);
      expect(await h.refs.count(RefEntity.plot), 1);
      expect(await h.refs.cursor(), 'c1');
    });

    test('PushCoordinator: sync_now + entities → host.syncNow(entities); '
        'không có entities → null', () async {
      final fcm = FakePushMessaging();
      final c = PushCoordinator(
        messaging: fcm,
        notifier: RecordingLocalNotifier(),
      );
      await c.start();
      await c.attach(_SyncHost(h));
      h.fake.pages[null] = PullPage(
        changes: [_up('farm_log', 'l1', 1), _up('plot', 'p1', 1)],
        nextCursor: 'c1',
        hasMore: false,
      );
      fcm.deliver({'kind': 'sync_now', 'entities': 'farm_log,season'});
      await Future<void>.delayed(const Duration(milliseconds: 50));
      expect(h.fake.pullEntities.single, ['farm_log', 'season']);
      expect(await h.refs.count(RefEntity.farmLog), 1);
      expect(await h.refs.count(RefEntity.plot), 0);
      fcm.deliver({'kind': 'sync_now'});
      await Future<void>.delayed(const Duration(milliseconds: 50));
      expect(h.fake.pullEntities.last, isNull);
      expect(c.syncNowHandled, 2);
      await c.dispose();
    });
  });

  test('DeepLink G2: /cropcycle/farm-logs/{id}, /cropcycle/seasons/{ref}', () {
    expect(
      DeepLink.parse('/cropcycle/farm-logs/0192-a'),
      const FarmLogLink('0192-a'),
    );
    expect(
      DeepLink.parse('/cropcycle/seasons/0192-s'),
      const SeasonLink('0192-s'),
    );
    expect(DeepLink.parse('/cropcycle/other/x'), const InboxLink());
    expect(DeepLink.parse('/cropcycle/farm-logs/a b'), const InboxLink());
  });
}
