import '../core/clock.dart';
import '../data/actions/action_queue.dart';
import '../data/reference/reference_repository.dart';
import '../domain/deep_link.dart';

/// Một thông báo trong hộp thư cục bộ (entity đồng bộ `notification`, pf/notify).
/// Schema không chứa dữ liệu cá nhân.
class InboxItem {
  const InboxItem({
    required this.id,
    required this.kind,
    required this.severity,
    required this.title,
    required this.body,
    required this.deepLink,
    required this.createdAt,
    required this.read,
    this.subjectRef,
    this.pendingRead = false,
  });

  final String id;

  /// directive | alert | document | task_escalated | sync_conflict | org_deactivated
  final String kind;

  /// urgent | normal
  final String severity;
  final String title;
  final String body;
  final String? deepLink;
  final DateTime? createdAt;
  final String? subjectRef;
  final bool read;

  /// Đã đánh dấu đọc trên máy, chưa gửi được lên máy chủ.
  final bool pendingRead;

  bool get urgent => severity == 'urgent';
  DeepLink get link => DeepLink.parse(deepLink);

  String get kindLabel => switch (kind) {
    'directive' => 'Chỉ đạo',
    'alert' => 'Cảnh báo',
    'document' => 'Tài liệu mới',
    'task_escalated' => 'Nhiệm vụ leo thang',
    'sync_conflict' => 'Xung đột đồng bộ',
    'org_deactivated' => 'Tổ chức',
    'farm_log_pending' => 'Nhật ký chờ duyệt',
    'farm_log_reviewed' => 'Kết quả duyệt nhật ký',
    'season_opened' => 'Mở vụ',
    'task_assigned' => 'Nhiệm vụ mới',
    _ => 'Thông báo',
  };
}

/// Hộp thư trong app: đọc từ dữ liệu đồng bộ, đánh dấu đã đọc ngoại tuyến bằng
/// lệnh xếp hàng (`POST /v1/notifications/{id}/read`, `/read-all`), phát lại
/// khi có mạng. Trạng thái đọc cục bộ phủ lên dữ liệu máy chủ cho tới khi pull
/// mang `read_at` xuống.
class InboxService {
  InboxService({
    required ReferenceRepository refs,
    required ActionQueue actions,
    required Clock clock,
  }) : _refs = refs,
       _actions = actions,
       _clock = clock;

  final ReferenceRepository _refs;
  final ActionQueue _actions;
  final Clock _clock;

  Future<List<InboxItem>> list() async {
    final reads = <String>{};
    DateTime? readAllAt;
    for (final a in await _actions.active()) {
      if (a.kind == ApiActionKind.notificationRead && a.target != null) {
        reads.add(a.target!);
      } else if (a.kind == ApiActionKind.notificationReadAll) {
        if (readAllAt == null || a.createdAt.isAfter(readAllAt)) {
          readAllAt = a.createdAt;
        }
      }
    }
    final cutoff = _clock.now().toUtc().subtract(notificationRetention);
    final out = <InboxItem>[];
    for (final r in await _refs.list(RefEntity.notification)) {
      final d = r.data;
      final created = DateTime.tryParse('${d['created_at']}');
      if (created != null && created.isBefore(cutoff)) continue;
      final serverRead = d['read_at'] != null;
      final localRead =
          reads.contains(r.id) ||
          (readAllAt != null &&
              (created == null || !created.isAfter(readAllAt)));
      out.add(
        InboxItem(
          id: r.id,
          kind: (d['kind'] ?? '').toString(),
          severity: (d['severity'] ?? 'normal').toString(),
          title: (d['title'] ?? '').toString(),
          body: (d['body'] ?? '').toString(),
          deepLink: d['deep_link'] as String?,
          subjectRef: d['subject_ref'] as String?,
          createdAt: created,
          read: serverRead || localRead,
          pendingRead: !serverRead && localRead,
        ),
      );
    }
    out.sort((a, b) {
      final ta = a.createdAt, tb = b.createdAt;
      if (ta == null || tb == null) return b.id.compareTo(a.id);
      return tb.compareTo(ta);
    });
    return out;
  }

  Future<InboxItem?> byId(String id) async {
    for (final i in await list()) {
      if (i.id == id) return i;
    }
    return null;
  }

  Future<int> unreadCount() async =>
      (await list()).where((i) => !i.read).length;

  /// Đánh dấu đã đọc (idempotent: đã có lệnh/đã đọc thì không xếp thêm).
  Future<void> markRead(String id) async {
    final item = await byId(id);
    if (item == null || item.read) return;
    await _actions.enqueue(ApiActionKind.notificationRead, target: id);
  }

  Future<void> markAllRead() async {
    if (await unreadCount() == 0) return;
    await _actions.enqueue(ApiActionKind.notificationReadAll);
  }
}
