import '../data/actions/action_queue.dart';
import '../data/reference/reference_repository.dart';

/// Tài liệu kỹ thuật đã tải về máy (entity `document`, pf/content).
class LibraryDocument {
  const LibraryDocument({
    required this.ref,
    required this.title,
    required this.summary,
    required this.body,
    required this.saved,
    required this.available,
    this.kind,
    this.category,
    this.publishedAt,
    this.note,
    this.savedAt,
    this.pending = false,
  });

  final String ref;
  final String title;
  final String summary;
  final String body;
  final String? kind;
  final String? category;
  final String? publishedAt;

  /// Trong "Đã lưu" (danh mục cá nhân, KN US-LIB-05).
  final bool saved;

  /// false = tài liệu đã gỡ/ra khỏi phạm vi; máy giữ bản đã tải, nhãn "đã gỡ".
  final bool available;
  final String? note;
  final String? savedAt;

  /// Lưu/bỏ lưu trên máy, chưa gửi lên máy chủ.
  final bool pending;
}

/// Thư viện kỹ thuật ngoại tuyến + danh mục cá nhân "Đã lưu".
///
/// - Chiều xuống: `document` (nội dung) và `library_item` (đã lưu của chính
///   người dùng; `id = document_ref`; bỏ lưu → tombstone).
/// - Chiều lên: `POST /v1/documents/{ref}/save|unsave` xếp hàng khi ngoại tuyến.
///   Lệnh mới nhất của mỗi tài liệu phủ lên trạng thái máy chủ.
class LibraryService {
  LibraryService({
    required ReferenceRepository refs,
    required ActionQueue actions,
  }) : _refs = refs,
       _actions = actions;

  final ReferenceRepository _refs;
  final ActionQueue _actions;

  Future<Map<String, ApiAction>> _latestLocal() async {
    final m = <String, ApiAction>{};
    for (final a in await _actions.pending()) {
      if ((a.kind == ApiActionKind.librarySave ||
              a.kind == ApiActionKind.libraryUnsave) &&
          a.target != null) {
        m[a.target!] = a; // theo seq tăng dần → bản cuối là mới nhất
      }
    }
    return m;
  }

  LibraryDocument _build(
    String ref,
    Map<String, Object?>? doc,
    Map<String, Object?>? item,
    ApiAction? local,
  ) {
    var saved = item != null;
    var pending = false;
    // Chỉ lệnh CHỜ GỬI phủ lên dữ liệu máy chủ: lệnh đã xong được phát lại
    // trước bước pull của cùng lượt đồng bộ, nên pull đã phản ánh nó.
    if (local != null && local.status == ApiActionStatus.pending) {
      pending = true;
      saved = local.kind == ApiActionKind.librarySave;
    }
    String s(Object? v) => v == null ? '' : v.toString();
    return LibraryDocument(
      ref: ref,
      title: s(doc?['title'] ?? item?['title']).isEmpty
          ? 'Tài liệu'
          : s(doc?['title'] ?? item?['title']),
      summary: s(doc?['summary']),
      body: s(doc?['body']),
      kind: doc?['kind'] as String?,
      category: doc?['category'] as String?,
      publishedAt: doc?['published_at'] as String?,
      saved: saved,
      available: doc != null && item?['available'] != false,
      note: (local?.body?['note'] ?? item?['note']) as String?,
      savedAt: item?['saved_at'] as String?,
      pending: pending,
    );
  }

  /// Mọi tài liệu có trên máy (mới xuất bản trước).
  Future<List<LibraryDocument>> documents() async {
    final items = {
      for (final r in await _refs.list(RefEntity.libraryItem)) r.id: r.data,
    };
    final local = await _latestLocal();
    final out = [
      for (final d in await _refs.list(RefEntity.document))
        _build(d.id, d.data, items[d.id], local[d.id]),
    ];
    out.sort((a, b) => (b.publishedAt ?? '').compareTo(a.publishedAt ?? ''));
    return out;
  }

  /// "Đã lưu": gồm cả tài liệu đã gỡ (giữ bản đã tải, nhãn "đã gỡ").
  Future<List<LibraryDocument>> saved() async {
    final docs = {
      for (final d in await _refs.list(RefEntity.document)) d.id: d.data,
    };
    final items = {
      for (final r in await _refs.list(RefEntity.libraryItem)) r.id: r.data,
    };
    final local = await _latestLocal();
    final refs = {...items.keys, ...local.keys};
    final out = <LibraryDocument>[];
    for (final ref in refs) {
      final d = _build(ref, docs[ref], items[ref], local[ref]);
      if (d.saved) out.add(d);
    }
    out.sort((a, b) => (b.savedAt ?? '~').compareTo(a.savedAt ?? '~'));
    return out;
  }

  Future<LibraryDocument?> byRef(String ref) async {
    final doc = await _refs.get(RefEntity.document, ref);
    final item = await _refs.get(RefEntity.libraryItem, ref);
    if (doc == null && item == null) return null;
    return _build(ref, doc?.data, item?.data, (await _latestLocal())[ref]);
  }

  Future<void> save(String ref, {String? note}) async {
    final cur = await byRef(ref);
    if (cur != null && cur.saved && cur.note == note) return;
    await _actions.enqueue(
      ApiActionKind.librarySave,
      target: ref,
      body: {'note': ?note},
    );
  }

  Future<void> unsave(String ref) async {
    final cur = await byRef(ref);
    if (cur != null && !cur.saved) return;
    await _actions.enqueue(ApiActionKind.libraryUnsave, target: ref);
  }
}
