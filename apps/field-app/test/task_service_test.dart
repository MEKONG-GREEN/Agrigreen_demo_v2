import 'package:agrigreen_field/application/task_service.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/harness.dart';

// Tách từ photo_pipeline_test.dart khi chuyển lõi sang packages/agrigreen_mobile.
void main() {
  late Harness h;

  setUp(() async => h = await Harness.create());
  tearDown(() => h.dispose());

  group('nhiệm vụ hỗ trợ (US-TASK-02)', () {
    test('chỉ đóng khi có kết quả; chuyển tiếp cần ghi chú; trạng thái cục bộ phủ lên', () async {
      await h.refs.applyPage(
        PullPage(
          changes: [
            PullChange(
              entity: 'task',
              id: 't1',
              version: 1,
              op: 'upsert',
              data: {'title': 'Hỗ trợ phòng rầy nâu', 'status': 'assigned'},
            ),
          ],
          nextCursor: 'c2',
          hasMore: false,
        ),
      );
      final svc = TaskService(outbox: h.outbox, refs: h.refs);
      expect(await svc.openCount(), 1);
      expect(
        () => svc.update(taskId: 't1', status: TaskStatus.done),
        throwsA(isA<TaskException>()),
      );
      expect(
        () => svc.update(taskId: 't1', status: TaskStatus.escalated),
        throwsA(isA<TaskException>()),
      );
      await svc.update(taskId: 't1', status: TaskStatus.accepted);
      await svc.update(
        taskId: 't1',
        status: TaskStatus.done,
        result: 'Đã hướng dẫn phun thuốc',
      );
      final t = (await svc.list()).single;
      expect(t.status, TaskStatus.done);
      expect(t.pendingLocal, isTrue);
      expect(await svc.openCount(), 0);
    });
  });
}
