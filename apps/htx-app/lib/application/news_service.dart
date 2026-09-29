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
    this.category,
    this.urgent = false,
    this.pending = false,
  });
  final String ref;
  final String title;
  final String summary;
  final String body;
  final String? publishedAt;

  /// Chuyên mục (`document.category` của pf/content): technique, pest_warning,
  /// policy, market, event; null = chưa phân loại (HTX US-NEWS-01 AC-2).
  final String? category;

  /// Cảnh báo khẩn cấp (HTX US-NEWS-03) — hiện trên cùng, màu đỏ.
  final bool urgent;

  /// Đã lưu đọc ngoại tuyến (HTX US-NEWS-02).
  final bool saved;

  /// false = bài đã gỡ; máy vẫn giữ bản đã tải (US-NEWS-02 AC-2).
  final bool available;

  /// Lưu/bỏ lưu trên máy, chưa gửi lên máy chủ.
  final bool pending;
}

/// Chuyên mục tin tức theo thứ tự hiển thị (pf/content `domain.Categories`).
const newsCategories = <String, String>{
  'technique': 'Kỹ thuật',
  'pest_warning': 'Cảnh báo sâu bệnh',
  'policy': 'Chính sách',
  'market': 'Thị trường',
  'event': 'Sự kiện',
};

/// Khóa nhóm của bài chưa có / có chuyên mục lạ.
const newsOtherCategory = 'other';

String newsCategoryLabel(String? c) =>
    newsCategories[c] ?? (c == null || c == newsOtherCategory ? 'Khác' : c);

/// Khóa nhóm của bài: chuyên mục đã biết, còn lại gộp "Khác".
String newsCategoryKey(NewsArticle a) =>
    newsCategories.containsKey(a.category) ? a.category! : newsOtherCategory;

/// Chia bài theo chuyên mục (thứ tự [newsCategories], "Khác" cuối), giữ thứ tự
/// trong nhóm (khẩn cấp trước, mới trước) — không trộn chuyên mục (AC-2).
List<(String, List<NewsArticle>)> groupByCategory(List<NewsArticle> list) {
  final m = <String, List<NewsArticle>>{};
  for (final a in list) {
    (m[newsCategoryKey(a)] ??= []).add(a);
  }
  return [
    for (final k in [...newsCategories.keys, newsOtherCategory])
      if (m[k] != null) (k, m[k]!),
  ];
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
      category: (doc?['category'] ?? item?['category']) as String?,
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

  /// Bài theo `document_ref` (deep link `/documents/{ref}` của cảnh báo khẩn
  /// cấp — HTX US-NEWS-03). null = chưa đồng bộ về máy.
  Future<NewsArticle?> article(String ref) async {
    for (final a in await articles()) {
      if (a.ref == ref) return a;
    }
    return null;
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
