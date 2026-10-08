import 'package:agrigreen_mobile/application/inbox_service.dart';
import 'package:agrigreen_mobile/data/actions/action_queue.dart';
import 'package:agrigreen_mobile/testing.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/harness.dart';

/// Loại lệnh do app khai báo (thử cơ chế đăng ký `extraKinds`).
enum _AppKind implements ActionKind {
  ping('app_ping'),

  /// Trùng mã với lệnh của lõi → ActionQueue phải từ chối.
  dup('notification_read');

  const _AppKind(this.code);

  @override
  final String code;

  @override
  String get method => 'POST';

  @override
  String pathFor(String? target) => '/v1/app/$target/ping';
}

void main() {
  test('KN P5: lệnh lưu lịch công tác tới đúng đường dẫn', () {
    expect(
      ApiActionKind.workScheduleSave.pathFor('2026-10-05'),
      '/v1/extension/work-schedules',
    );
    expect(
      ApiActionKind.parse('work_schedule_save'),
      ApiActionKind.workScheduleSave,
    );
  });

  test('phân vùng mạng (§29, R-SEC-01): lõi không chứa lệnh vùng mkg', () {
    for (final k in ApiActionKind.values) {
      final path = k.pathFor('x');
      expect(path, isNot(startsWith('/v1/fieldops')), reason: k.code);
      expect(path, isNot(startsWith('/v1/logistics')), reason: k.code);
    }
    expect(() => ApiActionKind.parse('fieldops_crew_shift'), throwsStateError);
    expect(() => ApiActionKind.parse('logistics_milestone'), throwsStateError);
  });

  test('lệnh riêng của app: phải đăng ký mới xếp hàng / đọc được', () async {
    final h = await Harness.create();
    addTearDown(h.dispose);
    // Hàng đợi mặc định (như App hiện trường KN) từ chối lệnh chưa đăng ký.
    await expectLater(
      h.actions.enqueue(_AppKind.ping, target: 't1'),
      throwsArgumentError,
    );
    expect(await h.actions.pendingCount(), 0);

    final q = ActionQueue(
      db: h.db,
      clock: h.clock,
      ids: h.ids,
      extraKinds: [_AppKind.ping],
    );
    final a = await q.enqueue(_AppKind.ping, target: 't1');
    expect(a.path, '/v1/app/t1/ping');
    final back = await q.byId(a.id);
    expect(back!.kind, _AppKind.ping);
    expect(q.kinds, containsAll([...ApiActionKind.values, _AppKind.ping]));
    // Hàng đợi không đăng ký gặp bản ghi lạ → lỗi rõ ràng, không phát nhầm.
    await expectLater(h.actions.byId(a.id), throwsStateError);

    expect(
      () => ActionQueue(
        db: h.db,
        clock: h.clock,
        ids: h.ids,
        extraKinds: [_AppKind.dup],
      ),
      throwsArgumentError,
    );
  });

  test(
    'cancelPending chỉ bỏ lệnh còn chờ; lệnh bị bỏ không phát lại',
    () async {
      final h = await Harness.create();
      addTearDown(h.dispose);
      final a = await h.actions.enqueue(
        ApiActionKind.workScheduleSave,
        target: '2026-10-05',
        body: {'action': 'draft'},
      );
      final b = await h.actions.enqueue(
        ApiActionKind.workScheduleSave,
        target: '2026-10-05',
        body: {'action': 'submit'},
      );
      expect(await h.actions.cancelPending(a.id), isTrue);
      expect(await h.actions.cancelPending(a.id), isFalse);
      final ca = await h.actions.byId(a.id);
      expect(ca!.status, ApiActionStatus.failed);
      expect(ca.lastError, ActionQueue.supersededCode);
      expect(await h.actions.byId('khong-co'), isNull);

      await h.sync.syncOnce(force: true);
      final fake = h.server as FakeServer;
      expect(fake.actionCalls.map((c) => c.$2), ['act:${b.id}']);
      expect((await h.actions.byId(b.id))!.status, ApiActionStatus.done);
      // Đã gửi xong → không bỏ được nữa.
      expect(await h.actions.cancelPending(b.id), isFalse);
    },
  );

  test('nhãn thông báo lịch công tác', () {
    InboxItem item(String kind) => InboxItem(
      id: 'n',
      kind: kind,
      severity: 'normal',
      title: 't',
      body: 'b',
      deepLink: null,
      createdAt: null,
      read: false,
    );
    expect(
      item('work_schedule_reviewed').kindLabel,
      'Kết quả duyệt lịch công tác',
    );
    expect(item('work_schedule_due').kindLabel, 'Nhắc đăng ký lịch công tác');
  });
}
