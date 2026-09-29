import 'package:agrigreen_mobile/data/outbox/outbox_repository.dart';
import 'package:agrigreen_mobile/data/reference/reference_repository.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';

enum TaskStatus {
  assigned('assigned', 'Mới giao'),
  accepted('accepted', 'Đã tiếp nhận'),
  inProgress('in_progress', 'Đang xử lý'),
  done('done', 'Hoàn thành'),
  escalated('escalated', 'Chuyển cấp trên');

  const TaskStatus(this.code, this.label);
  final String code;
  final String label;

  /// `action` của `TaskUpdatePayload` phía máy chủ (pf/extension):
  /// assigned, accepted, progress, resolved, escalated, reopened.
  String get action => switch (this) {
    TaskStatus.assigned => 'assigned',
    TaskStatus.accepted => 'accepted',
    TaskStatus.inProgress => 'progress',
    TaskStatus.done => 'resolved',
    TaskStatus.escalated => 'escalated',
  };

  static TaskStatus parse(String? c) => values.firstWhere(
    (s) => s.code == c || s.action == c,
    orElse: () => TaskStatus.assigned,
  );

  bool get closed => this == TaskStatus.done;
}

enum SlaLevel { ok, soon, overdue, none }

/// Thời hạn xử lý còn lại của nhiệm vụ (SLA 24 giờ — KN US-TASK-02).
class SlaInfo {
  const SlaInfo(this.level, this.label, [this.remaining]);
  final SlaLevel level;
  final String label;
  final Duration? remaining;
}

/// Đếm ngược SLA theo `due_at`. Còn dưới [soon] → cảnh báo; quá hạn → đỏ.
SlaInfo slaFor(
  String? dueAt,
  DateTime now, {
  bool closed = false,
  Duration soon = const Duration(hours: 4),
}) {
  final due = dueAt == null ? null : DateTime.tryParse(dueAt);
  if (closed) return const SlaInfo(SlaLevel.none, 'Đã hoàn thành');
  if (due == null) return const SlaInfo(SlaLevel.none, 'Không có hạn xử lý');
  final left = due.difference(now);
  String span(Duration d) {
    final m = d.inMinutes.abs();
    final days = m ~/ (24 * 60);
    final h = (m % (24 * 60)) ~/ 60;
    final mm = m % 60;
    if (days > 0) return '$days ngày $h giờ';
    if (h > 0) return '$h giờ $mm phút';
    return '$mm phút';
  }

  if (left.isNegative) {
    return SlaInfo(SlaLevel.overdue, 'Quá hạn ${span(left)}', left);
  }
  return SlaInfo(
    left <= soon ? SlaLevel.soon : SlaLevel.ok,
    'Còn ${span(left)}',
    left,
  );
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
    this.category,
    this.lastNote,
    this.pendingLocal = false,
  });
  final String id;
  final String title;
  final TaskStatus status;
  final String? plotId;
  final String? dueAt;
  final String? priority;
  final String? description;
  final String? category;
  final String? lastNote;

  SlaInfo sla(DateTime now) => slaFor(dueAt, now, closed: status.closed);

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
            title:
                (r.data['title'] ??
                        r.data['description'] ??
                        r.data['category'] ??
                        'Nhiệm vụ')
                    .toString(),
            status: TaskStatus.parse(
              (local?.payload['status'] ?? r.data['status']) as String?,
            ),
            plotId: r.data['plot_id'] as String?,
            dueAt: r.data['due_at'] as String?,
            priority: r.data['priority'] as String?,
            description: r.data['description'] as String?,
            category: r.data['category'] as String?,
            lastNote: r.data['last_note'] as String?,
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
        'action': status.action,
        'status': status.code,
        'note': note.trim(),
        'result': result.trim(),
      },
    );
  }
}
