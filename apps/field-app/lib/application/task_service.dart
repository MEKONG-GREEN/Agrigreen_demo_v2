import '../data/outbox/outbox_repository.dart';
import '../data/reference/reference_repository.dart';
import '../domain/outbox_record.dart';

enum TaskStatus {
  assigned('assigned', 'Mới giao'),
  accepted('accepted', 'Đã tiếp nhận'),
  inProgress('in_progress', 'Đang xử lý'),
  done('done', 'Hoàn thành'),
  escalated('escalated', 'Chuyển cấp trên');

  const TaskStatus(this.code, this.label);
  final String code;
  final String label;

  static TaskStatus parse(String? c) =>
      values.firstWhere((s) => s.code == c, orElse: () => TaskStatus.assigned);
}

class FieldTask {
  const FieldTask({
    required this.id,
    required this.title,
    required this.status,
    this.plotId,
    this.dueAt,
    this.priority,
    this.description,
    this.pendingLocal = false,
  });
  final String id;
  final String title;
  final TaskStatus status;
  final String? plotId;
  final String? dueAt;
  final String? priority;
  final String? description;

  /// Có cập nhật trên máy chưa đồng bộ.
  final bool pendingLocal;
}

class TaskException implements Exception {
  TaskException(this.message);
  final String message;
  @override
  String toString() => message;
}

/// Nhiệm vụ hỗ trợ (KN epic J): tiếp nhận, xử lý, cập nhật kết quả, chuyển tiếp.
class TaskService {
  TaskService({
    required OutboxRepository outbox,
    required ReferenceRepository refs,
  }) : _outbox = outbox,
       _refs = refs;
  final OutboxRepository _outbox;
  final ReferenceRepository _refs;

  Future<List<FieldTask>> list() async {
    final updates = await _outbox.list(entity: PushEntity.taskUpdate);
    // updates sắp theo seq giảm dần → bản đầu tiên là mới nhất.
    final latest = <String, OutboxRecord>{};
    for (final u in updates) {
      if (u.status == OutboxStatus.rejected ||
          u.status == OutboxStatus.superseded) {
        continue;
      }
      latest.putIfAbsent(u.payload['task_id']! as String, () => u);
    }
    final rows = await _refs.list(RefEntity.task);
    return [
      for (final r in rows)
        () {
          final local = latest[r.id];
          return FieldTask(
            id: r.id,
            title: (r.data['title'] ?? 'Nhiệm vụ').toString(),
            status: TaskStatus.parse(
              (local?.payload['status'] ?? r.data['status']) as String?,
            ),
            plotId: r.data['plot_id'] as String?,
            dueAt: r.data['due_at'] as String?,
            priority: r.data['priority'] as String?,
            description: r.data['description'] as String?,
            pendingLocal: local?.status == OutboxStatus.pending,
          );
        }(),
    ];
  }

  Future<int> openCount() async => (await list())
      .where(
        (t) => t.status != TaskStatus.done && t.status != TaskStatus.escalated,
      )
      .length;

  Future<OutboxRecord> update({
    required String taskId,
    required TaskStatus status,
    String note = '',
    String result = '',
  }) {
    if (status == TaskStatus.done && result.trim().isEmpty) {
      // US-TASK-02: chỉ đóng khi có kết quả xử lý cụ thể.
      throw TaskException(
        'Cần ghi kết quả xử lý trước khi hoàn thành nhiệm vụ.',
      );
    }
    if (status == TaskStatus.escalated && note.trim().isEmpty) {
      throw TaskException('Cần ghi chú lý do khi chuyển tiếp lên cấp trên.');
    }
    return _outbox.enqueue(
      entity: PushEntity.taskUpdate,
      payload: {
        'task_id': taskId,
        'status': status.code,
        'note': note.trim(),
        'result': result.trim(),
      },
    );
  }
}
