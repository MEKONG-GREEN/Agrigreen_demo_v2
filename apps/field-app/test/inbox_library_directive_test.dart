import 'package:agrigreen_field/application/directive_service.dart';
import 'package:agrigreen_field/application/library_service.dart';
import 'package:agrigreen_mobile/application/inbox_service.dart';
import 'package:agrigreen_mobile/data/actions/action_queue.dart';
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

  group('thư viện "Đã lưu" (library_item, US-LIB-05)', () {
    LibraryService lib() => LibraryService(refs: h.refs, actions: h.actions);

    PullChange doc(String id, String title, {String? published}) => PullChange(
      entity: 'document',
      id: id,
      version: 1,
      op: 'upsert',
      data: {
        'ref': id,
        'title': title,
        'summary': 'Tóm tắt',
        'body': 'Nội dung',
        'published_at': published ?? '2026-11-01T00:00:00Z',
      },
    );

    test(
      'lưu/bỏ lưu ngoại tuyến → lớp phủ ngay; phát lại save/unsave theo thứ tự',
      () async {
        await pull([
          doc('d1', 'Quy trình 1 phải 5 giảm'),
          doc('d2', 'Phòng rầy'),
        ]);
        h.fake.failNextActions = 5;
        await lib().save('d1', note: 'mang ra ruộng');
        expect((await lib().saved()).single.ref, 'd1');
        expect((await lib().saved()).single.pending, isTrue);
        expect((await lib().saved()).single.note, 'mang ra ruộng');
        await lib().save('d2');
        await lib().unsave('d2');
        expect((await lib().saved()).map((d) => d.ref), ['d1']);

        await h.sync.syncOnce(); // lỗi mạng
        h.fake.failNextActions = 0;
        await h.sync.syncOnce(force: true);
        final effective = [for (final c in h.fake.actionCalls.skip(1)) c.$1];
        expect(effective, [
          '/v1/documents/d1/save',
          '/v1/documents/d2/save',
          '/v1/documents/d2/unsave',
        ]);
        // Chiều xuống: library_item cho d1.
        await pull([
          PullChange(
            entity: 'library_item',
            id: 'd1',
            version: 1,
            op: 'upsert',
            data: {
              'document_ref': 'd1',
              'saved_at': '2026-12-02T01:00:00Z',
              'note': 'mang ra ruộng',
              'title': 'Quy trình 1 phải 5 giảm',
              'available': true,
            },
          ),
        ], cursor: 'c2');
        final saved = await lib().saved();
        expect(saved.single.pending, isFalse);
        expect(saved.single.available, isTrue);
      },
    );

    test(
      'tài liệu đã gỡ vẫn trong "Đã lưu" với nhãn đã gỡ; bỏ lưu = tombstone',
      () async {
        await pull([
          doc('d1', 'Cũ'),
          PullChange(
            entity: 'library_item',
            id: 'd1',
            version: 1,
            op: 'upsert',
            data: {'document_ref': 'd1', 'title': 'Cũ', 'available': false},
          ),
        ]);
        final s = (await lib().saved()).single;
        expect(s.available, isFalse);
        expect(s.title, 'Cũ');
        await pull([
          const PullChange(
            entity: 'library_item',
            id: 'd1',
            version: 2,
            op: 'tombstone',
          ),
        ], cursor: 'c2');
        expect(await lib().saved(), isEmpty);
        expect((await lib().documents()).single.saved, isFalse);
      },
    );
  });

  group('chỉ đạo (directive, US-TASK-03)', () {
    DirectiveService svc() =>
        DirectiveService(refs: h.refs, actions: h.actions, clock: h.clock);

    PullChange dir(String id, {bool target = true, String? acked}) =>
        PullChange(
          entity: 'directive',
          id: id,
          version: 1,
          op: 'upsert',
          data: {
            'directive_id': id,
            'title': 'Chỉ đạo $id',
            'body': 'Phun thuốc phòng đạo ôn',
            'severity': 'urgent',
            'issued_at': '2026-12-01T00:00:00Z',
            'expires_at': null,
            'acked_at': acked,
            'is_target': target,
          },
        );

    test('xác nhận đã đọc ngoại tuyến → xếp hàng → POST /ack; không phải người nhận → chặn', () async {
      await pull([
        dir('d1'),
        dir('d2', target: false),
        dir('d3', acked: '2026-12-01T05:00:00Z'),
      ]);
      expect(await svc().unackedCount(), 1);
      await expectLater(
        svc().acknowledge('d2'),
        throwsA(isA<DirectiveException>()),
      );
      await svc().acknowledge('d1');
      await svc().acknowledge('d1');
      final d1 = (await svc().byId('d1'))!;
      expect(d1.acked, isTrue);
      expect(d1.pendingAck, isTrue);
      expect(await svc().unackedCount(), 0);
      await h.sync.syncOnce();
      expect(h.fake.actionCalls.map((c) => c.$1), [
        '/v1/extension/directives/d1/ack',
      ]);
      expect(
        (await h.actions.active(kind: ApiActionKind.directiveAck))
            .single
            .status,
        ApiActionStatus.done,
      );
    });
  });
}
