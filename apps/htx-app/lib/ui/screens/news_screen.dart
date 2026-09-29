import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/news_service.dart';
import '../providers.dart';
import '../widgets.dart';

final _newsProvider = FutureProvider.autoDispose<List<NewsArticle>>(
  (ref) => servicesOf(ref).news.articles(),
);

/// Tin tức khuyến nông + lưu đọc ngoại tuyến (HTX US-NEWS-02). Nội dung đồng
/// bộ về máy (entity `document`), đọc được khi mất mạng.
class NewsScreen extends ConsumerStatefulWidget {
  const NewsScreen({super.key});

  @override
  ConsumerState<NewsScreen> createState() => _NewsScreenState();
}

class _NewsScreenState extends ConsumerState<NewsScreen> {
  bool _savedOnly = false;

  /// Chuyên mục đang lọc; null = tất cả, chia nhóm theo chuyên mục.
  String? _category;

  Widget _card(NewsArticle a, {bool showCategory = false}) => Card(
    color: a.urgent ? AgColors.redBg : null,
    child: ListTile(
      leading: Icon(
        a.urgent ? Icons.warning_amber : Icons.article,
        color: a.urgent ? AgColors.red : AgColors.g700,
      ),
      title: Text(a.title),
      subtitle: Text(
        [
          if (showCategory) newsCategoryLabel(a.category),
          if (!a.available) 'Đã gỡ',
          if (a.saved) 'Đã lưu',
          a.summary,
        ].where((x) => x.isNotEmpty).join(' · '),
        maxLines: 2,
        overflow: TextOverflow.ellipsis,
      ),
      onTap: () => Navigator.of(context)
          .push(
            MaterialPageRoute<void>(builder: (_) => ArticleScreen(article: a)),
          )
          .then((_) => ref.invalidate(_newsProvider)),
    ),
  );

  @override
  Widget build(BuildContext context) {
    final news = ref.watch(_newsProvider);
    final present = {
      for (final a in news.value ?? const <NewsArticle>[]) newsCategoryKey(a),
    };
    return Scaffold(
      appBar: AppBar(title: const Text('Tin tức & Khuyến nông')),
      body: Column(
        children: [
          Padding(
            padding: const EdgeInsets.all(8),
            child: SegmentedButton<bool>(
              segments: const [
                ButtonSegment(value: false, label: Text('Tất cả')),
                ButtonSegment(value: true, label: Text('Đã lưu')),
              ],
              selected: {_savedOnly},
              onSelectionChanged: (s) => setState(() => _savedOnly = s.first),
            ),
          ),
          // Lọc theo chuyên mục (HTX US-NEWS-01 AC-2).
          SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            padding: const EdgeInsets.symmetric(horizontal: 8),
            child: Row(
              children: [
                Padding(
                  padding: const EdgeInsets.only(right: 6),
                  child: ChoiceChip(
                    label: const Text('Mọi chuyên mục'),
                    selected: _category == null,
                    onSelected: (_) => setState(() => _category = null),
                  ),
                ),
                for (final k in [...newsCategories.keys, newsOtherCategory])
                  if (present.contains(k))
                    Padding(
                      padding: const EdgeInsets.only(right: 6),
                      child: ChoiceChip(
                        label: Text(newsCategoryLabel(k)),
                        selected: _category == k,
                        onSelected: (on) =>
                            setState(() => _category = on ? k : null),
                      ),
                    ),
              ],
            ),
          ),
          Expanded(
            child: news.when(
              loading: () => const Center(child: CircularProgressIndicator()),
              error: (e, _) => Center(child: Text('Lỗi: $e')),
              data: (all) {
                final list = [
                  for (final a in all)
                    if ((!_savedOnly || a.saved) &&
                        (_category == null || newsCategoryKey(a) == _category))
                      a,
                ];
                if (list.isEmpty) {
                  return const Center(
                    child: Text(
                      'Chưa có bài viết trên máy.',
                      style: TextStyle(fontSize: 17),
                    ),
                  );
                }
                if (_category != null) {
                  return ListView(
                    padding: const EdgeInsets.all(8),
                    children: [for (final a in list) _card(a)],
                  );
                }
                // Cảnh báo khẩn cấp luôn trên cùng (HTX US-NEWS-03), rồi
                // từng chuyên mục riêng.
                final urgent = [
                  for (final a in list)
                    if (a.urgent) a,
                ];
                return ListView(
                  padding: const EdgeInsets.all(8),
                  children: [
                    if (urgent.isNotEmpty) ...[
                      Padding(
                        padding: const EdgeInsets.fromLTRB(4, 12, 4, 4),
                        child: Text(
                          'Cảnh báo khẩn (${urgent.length})',
                          style: Theme.of(context).textTheme.titleMedium,
                        ),
                      ),
                      for (final a in urgent) _card(a, showCategory: true),
                    ],
                    for (final (k, items) in groupByCategory([
                      for (final a in list)
                        if (!a.urgent) a,
                    ])) ...[
                      Padding(
                        padding: const EdgeInsets.fromLTRB(4, 12, 4, 4),
                        child: Text(
                          '${newsCategoryLabel(k)} (${items.length})',
                          style: Theme.of(context).textTheme.titleMedium,
                        ),
                      ),
                      for (final a in items) _card(a),
                    ],
                  ],
                );
              },
            ),
          ),
        ],
      ),
    );
  }
}

class ArticleScreen extends ConsumerStatefulWidget {
  const ArticleScreen({super.key, required this.article});
  final NewsArticle article;

  @override
  ConsumerState<ArticleScreen> createState() => _ArticleScreenState();
}

class _ArticleScreenState extends ConsumerState<ArticleScreen> {
  late bool _saved = widget.article.saved;

  Future<void> _toggle() async {
    final n = ref.services.news;
    if (_saved) {
      await n.unsave(widget.article.ref);
    } else {
      await n.save(widget.article.ref);
    }
    setState(() => _saved = !_saved);
    if (mounted) {
      showMessage(
        context,
        _saved ? 'Đã lưu để đọc ngoại tuyến.' : 'Đã bỏ lưu.',
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    final a = widget.article;
    return Scaffold(
      appBar: AppBar(
        title: const Text('Bài viết'),
        actions: [
          IconButton(
            tooltip: _saved ? 'Bỏ lưu' : 'Lưu đọc ngoại tuyến',
            iconSize: 28,
            icon: Icon(_saved ? Icons.bookmark : Icons.bookmark_border),
            onPressed: _toggle,
          ),
        ],
      ),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          if (!a.available)
            const Padding(
              padding: EdgeInsets.only(bottom: 8),
              child: Tag(msgArticleWithdrawn, color: AgColors.red),
            ),
          Text(a.title, style: Theme.of(context).textTheme.titleLarge),
          if (a.publishedAt != null)
            Text(a.publishedAt!, style: const TextStyle(color: AgColors.mute)),
          const SizedBox(height: 12),
          if (a.summary.isNotEmpty)
            Text(
              a.summary,
              style: const TextStyle(fontWeight: FontWeight.w600),
            ),
          const SizedBox(height: 8),
          Text(a.body),
        ],
      ),
    );
  }
}
