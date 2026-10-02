import 'package:agrigreen_mobile/application/inbox_service.dart';
import 'package:agrigreen_mobile/data/actions/action_queue.dart';
import 'package:agrigreen_mobile/testing.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/harness.dart';

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
