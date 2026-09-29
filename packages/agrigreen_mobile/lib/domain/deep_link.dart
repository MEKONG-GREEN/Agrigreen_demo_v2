/// Đích điều hướng trong app từ `deep_link` của thông báo (schema
/// `Notification.deep_link`): `/extension/directives/{id}`, `/documents/{ref}`,
/// `/extension/tasks/{id}`, `/sync/conflicts/{id}`, `/orgs/{ref}`; G2 (App
/// HTX, pf/notify `cropcycle.go`): `/cropcycle/farm-logs/{id}` (kind
/// `farm_log_pending` / `farm_log_reviewed`), `/cropcycle/seasons/{ref}` (kind
/// `season_opened`). Cảnh báo khẩn cấp (`alert`) dùng `/documents/{ref}`.
sealed class DeepLink {
  const DeepLink();

  /// Phân tích đường dẫn. Đường dẫn lạ hoặc sai dạng → [InboxLink] (mở hộp thư),
  /// không bao giờ ném lỗi.
  static DeepLink parse(String? raw) {
    if (raw == null) return const InboxLink();
    final uri = Uri.tryParse(raw.trim());
    if (uri == null) return const InboxLink();
    final seg = uri.pathSegments.where((s) => s.isNotEmpty).toList();
    bool isId(String s) => RegExp(r'^[0-9A-Za-z-]{1,64}$').hasMatch(s);
    if (seg.length == 3 && seg[0] == 'extension' && isId(seg[2])) {
      if (seg[1] == 'directives') return DirectiveLink(seg[2]);
      if (seg[1] == 'tasks') return TaskLink(seg[2]);
    }
    if (seg.length == 2 && seg[0] == 'documents' && isId(seg[1])) {
      return DocumentLink(seg[1]);
    }
    if (seg.length == 3 &&
        seg[0] == 'sync' &&
        seg[1] == 'conflicts' &&
        isId(seg[2])) {
      return ConflictLink(seg[2]);
    }
    if (seg.length == 3 && seg[0] == 'cropcycle' && isId(seg[2])) {
      if (seg[1] == 'farm-logs') return FarmLogLink(seg[2]);
      if (seg[1] == 'seasons') return SeasonLink(seg[2]);
    }
    if (seg.length == 1 && seg[0] == 'notifications') return const InboxLink();
    return const InboxLink();
  }
}

class InboxLink extends DeepLink {
  const InboxLink();
  @override
  bool operator ==(Object other) => other is InboxLink;
  @override
  int get hashCode => 0;
}

class DirectiveLink extends DeepLink {
  const DirectiveLink(this.id);
  final String id;
  @override
  bool operator ==(Object other) => other is DirectiveLink && other.id == id;
  @override
  int get hashCode => Object.hash('d', id);
}

class TaskLink extends DeepLink {
  const TaskLink(this.id);
  final String id;
  @override
  bool operator ==(Object other) => other is TaskLink && other.id == id;
  @override
  int get hashCode => Object.hash('t', id);
}

class DocumentLink extends DeepLink {
  const DocumentLink(this.ref);
  final String ref;
  @override
  bool operator ==(Object other) => other is DocumentLink && other.ref == ref;
  @override
  int get hashCode => Object.hash('doc', ref);
}

class ConflictLink extends DeepLink {
  const ConflictLink(this.id);
  final String id;
  @override
  bool operator ==(Object other) => other is ConflictLink && other.id == id;
  @override
  int get hashCode => Object.hash('c', id);
}

/// Nhật ký canh tác (id phiên bản) — G2 App HTX.
class FarmLogLink extends DeepLink {
  const FarmLogLink(this.id);
  final String id;
  @override
  bool operator ==(Object other) => other is FarmLogLink && other.id == id;
  @override
  int get hashCode => Object.hash('fl', id);
}

/// Vụ (`season_ref`) — G2 App HTX.
class SeasonLink extends DeepLink {
  const SeasonLink(this.ref);
  final String ref;
  @override
  bool operator ==(Object other) => other is SeasonLink && other.ref == ref;
  @override
  int get hashCode => Object.hash('se', ref);
}
