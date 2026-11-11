import 'package:agrigreen_mobile/application/inbox_service.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/domain/deep_link.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/harness.dart';

PullChange notif(
  String id, {
  int version = 1,
  String? readAt,
  String createdAt = '2026-12-01T08:00:00Z',
  String kind = 'directive',
  String link = '/extension/directives/d1',
}) => PullChange(
  entity: 'notification',
  id: id,
  version: version,
  op: 'upsert',
  data: {
    'id': id,
    'kind': kind,
    'severity': 'urgent',
    'title': 'Phòng rầy nâu $id',
    'body': 'Kiểm tra ruộng trong 48 giờ',
    'deep_link': link,
    'subject_ref': null,
    'created_at': createdAt,
    'read_at': readAt,
  },
);

void main() {
  late Harness h;
  setUp(() async => h = await Harness.create());
  tearDown(() => h.dispose());

  InboxService inbox() =>
      InboxService(refs: h.refs, actions: h.actions, clock: h.clock);

  Future<void> pull(List<PullChange> changes, {String cursor = 'c1'}) =>
      h.refs.applyPage(
        PullPage(changes: changes, nextCursor: cursor, hasMore: false),
      );

  group('hộp thư (entity notification, US-LIB-06)', () {
    test('unread, đánh dấu đã đọc ngoại tuyến → xếp hàng, phát lại đúng một lần qua crash', () async {
      await pull([
        notif('n1'),
        notif('n2', createdAt: '2026-12-01T09:00:00Z'),
        notif('n3', readAt: '2026-12-01T10:00:00Z'),
      ]);
      final list = await inbox().list();
      expect(list.map((i) => i.id), ['n2', 'n1', 'n3']); // mới nhất trước
      expect(await inbox().unreadCount(), 2);

      // Ngoại tuyến.
      h.fake.failNextActions = 1;
      await inbox().markRead('n1');
      await inbox().markRead('n1'); // lặp: không xếp thêm
      expect(await inbox().unreadCount(), 1);
      expect((await inbox().byId('n1'))!.pendingRead, isTrue);
      await h.sync.syncOnce();
      expect(h.fake.actionCalls, hasLength(1));
      expect(await h.actions.pendingCount(), 1);

      // App chết, mở lại: phát lại cùng khóa.
      await h.reopen();
      await h.sync.syncOnce(force: true);
      expect(h.fake.actionCalls, hasLength(2));
      expect(h.fake.actionCalls[0], h.fake.actionCalls[1]);
      expect(h.fake.actionCalls[0].$1, '/v1/notifications/n1/read');
      expect(h.fake.actionCalls[0].$2, startsWith('act:'));
      expect(h.fake.actionEffects['/v1/notifications/n1/read'], 1);
      expect(await h.actions.pendingCount(), 0);

      // Pull mang read_at xuống.
      await pull([notif('n1', version: 2, readAt: '2026-12-02T01:00:00Z')]);
      final n1 = (await inbox().byId('n1'))!;
      expect(n1.read, isTrue);
      expect(n1.pendingRead, isFalse);
    });

    test('đọc hết: một lệnh read-all phủ mọi thông báo tới lúc bấm', () async {
      await pull([notif('n1'), notif('n2')]);
      await inbox().markAllRead();
      expect(await inbox().unreadCount(), 0);
      // Thông báo mới hơn sau khi bấm vẫn chưa đọc.
      await pull([
        notif('n9', createdAt: '2026-12-03T00:00:00Z'),
      ], cursor: 'c2');
      h.clock.advance(const Duration(minutes: 1));
      expect(await inbox().unreadCount(), 1);
      await h.sync.syncOnce();
      expect(h.fake.actionCalls.single.$1, '/v1/notifications/read-all');
    });

    test(
      '404 (không còn/ngoài phạm vi) → bỏ lệnh, không chặn lệnh sau',
      () async {
        await pull([notif('n1'), notif('n2')]);
        h.fake.actionErrors['/v1/notifications/n1/read'] = (
          404,
          'notify.notification_not_found',
        );
        await inbox().markRead('n1');
        await inbox().markRead('n2');
        await h.sync.syncOnce();
        expect(h.fake.actionCalls.map((c) => c.$1), [
          '/v1/notifications/n1/read',
          '/v1/notifications/n2/read',
        ]);
        expect(await h.actions.pendingCount(), 0);
        final failed = await h.db.db.query(
          'api_action',
          where: "status = 'failed'",
        );
        expect(failed.single['last_error'], 'notify.notification_not_found');
      },
    );

    test('máy bỏ thông báo cũ hơn 30 ngày', () async {
      await pull([
        notif('old', createdAt: '2026-10-01T00:00:00Z'),
        notif('new', createdAt: '2026-11-30T00:00:00Z'),
      ]);
      expect((await inbox().list()).map((i) => i.id), ['new']);
      await h.sync.syncOnce();
      expect(await h.refs.get('notification', 'old'), isNull);
      expect(await h.refs.get('notification', 'new'), isNotNull);
    });
  });

  group('deep link', () {
    test('đường dẫn hợp đồng → màn hình; lạ → hộp thư', () {
      expect(
        DeepLink.parse('/extension/directives/0191-a'),
        const DirectiveLink('0191-a'),
      );
      expect(DeepLink.parse('/extension/tasks/t1'), const TaskLink('t1'));
      expect(DeepLink.parse('/documents/d9'), const DocumentLink('d9'));
      expect(DeepLink.parse('/sync/conflicts/c1'), const ConflictLink('c1'));
      expect(DeepLink.parse('/orgs/o1'), const InboxLink());
      expect(DeepLink.parse(null), const InboxLink());
      expect(DeepLink.parse('javascript:alert(1)'), const InboxLink());
      expect(DeepLink.parse('/documents/../../etc'), const InboxLink());
    });
  });
}
