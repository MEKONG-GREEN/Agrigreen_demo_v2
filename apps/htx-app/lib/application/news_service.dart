import 'package:agrigreen_mobile/data/actions/action_queue.dart';
import 'package:agrigreen_mobile/data/reference/reference_repository.dart';

/// Bài viết khuyến nông (entity `document` của pf/content).
class NewsArticle {
  const NewsArticle({
    required this.ref,
    required this.title,
    required this.summary,
    required this.body,
    required this.saved,
    required this.available,
    this.publishedAt,
    this.urgent = false,
    this.pending = false,
  });
  final String ref;
  final String title;
  final String summary;
  final String body;
  final String? publishedAt;

  /// Cảnh báo khẩn cấp (HTX US-NEWS-03) — hiện trên cùng, màu đỏ.
  final bool urgent;

  /// Đã lưu đọc ngoại tuyến (HTX US-NEWS-02).
  final bool saved;

  /// false = bài đã gỡ; máy vẫn giữ bản đã tải (US-NEWS-02 AC-2).
  final bool available;

  /// Lưu/bỏ lưu trên máy, chưa gửi lên máy chủ.
  final bool pending;
}

/// Nhãn của bài đã lưu bị gỡ (HTX US-NEWS-02 AC-2).
const msgArticleWithdrawn =
    'Bài viết đã gỡ, nội dung có thể không còn cập nhật';

/// Tin tức + lưu đọc ngoại tuyến. Dùng lại nguồn pull của lõi (`document`,
/// `library_item`) và lệnh `POST /v1/documents/{ref}/save|unsave` xếp hàng
/// (ActionQueue) — cùng cơ chế thư viện của app hiện trường.
class NewsService {
  NewsService({required ReferenceRepository refs, required ActionQueue actions})
    : _refs = refs,
      _actions = actions;

  final ReferenceRepository _refs;
  final ActionQueue _actions;

  Future<Map<String, ApiAction>> _pendingLocal() async {
    final m = <String, ApiAction>{};
    for (final a in await _actions.pending()) {
      if ((a.kind == ApiActionKind.librarySave ||
              a.kind == ApiActionKind.libraryUnsave) &&
          a.target != null) {
        m[a.target!] = a;
      }
    }
    return m;
  }

  NewsArticle _build(
    String ref,
    Map<String, Object?>? doc,
    Map<String, Object?>? item,
    ApiAction? local,
  ) {
    var saved = item != null;
    if (local != null) saved = local.kind == ApiActionKind.librarySave;
    String s(Object? v) => v == null ? '' : '$v';
    final title = s(doc?['title'] ?? item?['title']);
    return NewsArticle(
      ref: ref,
      title: title.isEmpty ? 'Bài viết' : title,
      summary: s(doc?['summary']),
      body: s(doc?['body']),
      publishedAt: doc?['published_at'] as String?,
      urgent:
          doc?['severity'] == 'urgent' ||
          doc?['kind'] == 'urgent_alert' ||
          doc?['urgent'] == true,
      saved: saved,
      available: doc != null && item?['available'] != false,
      pending: local != null,
    );
  }

  /// Mọi bài trên máy: khẩn cấp trước, rồi mới xuất bản trước.
  Future<List<NewsArticle>> articles() async {
    final items = {
      for (final r in await _refs.list(RefEntity.libraryItem)) r.id: r.data,
    };
    final local = await _pendingLocal();
    final out = [
      for (final d in await _refs.list(RefEntity.document))
        _build(d.id, d.data, items[d.id], local[d.id]),
    ];
    // Bài đã lưu nhưng đã gỡ khỏi phạm vi (tombstone `document`) vẫn đọc được.
    for (final e in items.entries) {
      if (out.every((a) => a.ref != e.key)) {
        out.add(_build(e.key, null, e.value, local[e.key]));
      }
    }
    out.sort((a, b) {
      if (a.urgent != b.urgent) return a.urgent ? -1 : 1;
      return (b.publishedAt ?? '').compareTo(a.publishedAt ?? '');
    });
    return out;
  }

  Future<List<NewsArticle>> saved() async => [
    for (final a in await articles())
      if (a.saved) a,
  ];

  Future<void> save(String ref) =>
      _actions.enqueue(ApiActionKind.librarySave, target: ref, body: const {});

  Future<void> unsave(String ref) =>
      _actions.enqueue(ApiActionKind.libraryUnsave, target: ref);
}
