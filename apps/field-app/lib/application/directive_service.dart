import '../core/clock.dart';
import '../data/actions/action_queue.dart';
import '../data/reference/reference_repository.dart';

/// Chỉ đạo khẩn/thường tới cán bộ (entity `directive`, pf/extension, KN
/// US-TASK-03).
class FieldDirective {
  const FieldDirective({
    required this.id,
    required this.title,
    required this.body,
    required this.severity,
    required this.isTarget,
    this.issuedAt,
    this.expiresAt,
    this.ackedAt,
    this.pendingAck = false,
  });

  final String id;
  final String title;
  final String body;
  final String severity;

  /// Người dùng là người nhận (chỉ người nhận mới xác nhận được).
  final bool isTarget;
  final DateTime? issuedAt;
  final DateTime? expiresAt;
  final DateTime? ackedAt;

  /// Đã xác nhận đọc trên máy, chưa gửi lên máy chủ.
  final bool pendingAck;

  bool get urgent => severity == 'urgent';
  bool get acked => ackedAt != null || pendingAck;
  bool expiredAt(DateTime now) => expiresAt != null && now.isAfter(expiresAt!);
}

class DirectiveException implements Exception {
  DirectiveException(this.message);
  final String message;
  @override
  String toString() => message;
}

/// Danh sách chỉ đạo + xác nhận đã đọc ngoại tuyến
/// (`POST /v1/extension/directives/{id}/ack`, gọi lặp giữ lần đầu).
class DirectiveService {
  DirectiveService({
    required ReferenceRepository refs,
    required ActionQueue actions,
    required Clock clock,
  }) : _refs = refs,
       _actions = actions,
       _clock = clock;

  final ReferenceRepository _refs;
  final ActionQueue _actions;
  final Clock _clock;

  Future<List<FieldDirective>> list() async {
    final local = {
      for (final a in await _actions.active(kind: ApiActionKind.directiveAck))
        ?a.target,
    };
    DateTime? t(Object? v) => v == null ? null : DateTime.tryParse('$v');
    final out = [
      for (final r in await _refs.list(RefEntity.directive))
        FieldDirective(
          id: r.id,
          title: (r.data['title'] ?? 'Chỉ đạo').toString(),
          body: (r.data['body'] ?? '').toString(),
          severity: (r.data['severity'] ?? 'normal').toString(),
          isTarget: r.data['is_target'] != false,
          issuedAt: t(r.data['issued_at']),
          expiresAt: t(r.data['expires_at']),
          ackedAt: t(r.data['acked_at']),
          pendingAck: r.data['acked_at'] == null && local.contains(r.id),
        ),
    ];
    // Chưa đọc trước, khẩn trước, mới trước.
    out.sort((a, b) {
      if (a.acked != b.acked) return a.acked ? 1 : -1;
      if (a.urgent != b.urgent) return a.urgent ? -1 : 1;
      final ia = a.issuedAt, ib = b.issuedAt;
      if (ia == null || ib == null) return a.id.compareTo(b.id);
      return ib.compareTo(ia);
    });
    return out;
  }

  Future<FieldDirective?> byId(String id) async {
    for (final d in await list()) {
      if (d.id == id) return d;
    }
    return null;
  }

  Future<int> unackedCount() async {
    final now = _clock.now();
    return (await list())
        .where((d) => d.isTarget && !d.acked && !d.expiredAt(now))
        .length;
  }

  Future<void> acknowledge(String id) async {
    final d = await byId(id);
    if (d == null) throw DirectiveException('Không tìm thấy chỉ đạo trên máy.');
    if (!d.isTarget) {
      throw DirectiveException('Bạn không thuộc danh sách nhận chỉ đạo này.');
    }
    if (d.acked) return;
    await _actions.enqueue(ApiActionKind.directiveAck, target: id);
  }
}
