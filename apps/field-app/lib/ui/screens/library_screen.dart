import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/library_service.dart';
import '../providers.dart';
import 'notifications_screen.dart' show unawaitedSync;

final _documentsProvider = FutureProvider.autoDispose<List<LibraryDocument>>((
  ref,
) {
  ref.watch(syncStatusStreamProvider);
  return servicesOf(ref).library.documents();
});

final _savedProvider = FutureProvider.autoDispose<List<LibraryDocument>>((ref) {
  ref.watch(syncStatusStreamProvider);
  return servicesOf(ref).library.saved();
});

/// Thư viện kỹ thuật ngoại tuyến: "Tài liệu" (theo phạm vi) và "Đã lưu" (danh
/// mục cá nhân — KN US-LIB-05).
class LibraryScreen extends ConsumerWidget {
  const LibraryScreen({super.key, this.initialTab = 0});
  final int initialTab;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return DefaultTabController(
      length: 2,
      initialIndex: initialTab,
      child: Scaffold(
        appBar: AppBar(
          title: const Text('Thư viện kỹ thuật'),
          bottom: const TabBar(
            labelStyle: TextStyle(fontSize: 17, fontWeight: FontWeight.w700),
            labelColor: Colors.white,
            unselectedLabelColor: Colors.white70,
            indicatorColor: Colors.white,
            tabs: [
              Tab(text: 'Tài liệu'),
              Tab(text: 'Đã lưu'),
            ],
          ),
        ),
        body: TabBarView(
          children: [
            _DocList(provider: _documentsProvider, empty: 'Chưa có tài liệu.'),
            _DocList(
              provider: _savedProvider,
              empty: 'Chưa lưu tài liệu nào. Mở một tài liệu và chạm "Lưu" để đọc ngoại tuyến.',
            ),
          ],
        ),
      ),
    );
  }
}

class _DocList extends ConsumerWidget {
  const _DocList({required this.provider, required this.empty});
  final FutureProvider<List<LibraryDocument>> provider;
  final String empty;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final docs = ref.watch(provider);
    return docs.when(
      loading: () => const Center(child: CircularProgressIndicator()),
      error: (e, _) => Center(child: Text('Lỗi: $e')),
      data: (list) => list.isEmpty
          ? Center(
              child: Padding(
                padding: const EdgeInsets.all(24),
                child: Text(
                  empty,
                  textAlign: TextAlign.center,
                  style: const TextStyle(fontSize: 17),
                ),
              ),
            )
          : ListView.separated(
              itemCount: list.length,
              separatorBuilder: (_, _) => const Divider(height: 1),
              itemBuilder: (_, i) {
                final d = list[i];
                return ListTile(
                  tileColor: Colors.white,
                  leading: Icon(
                    d.available ? Icons.menu_book : Icons.block,
                    color: d.available ? AgColors.g700 : AgColors.mute,
                    size: 32,
                  ),
                  title: Text(d.title),
                  subtitle: Text(_docSubtitle(d)),
                  trailing: Icon(
                    d.saved ? Icons.bookmark : Icons.bookmark_border,
                    color: AgColors.g700,
                  ),
                  onTap: () async {
                    await Navigator.of(context).push(
                      MaterialPageRoute<void>(
                        builder: (_) => DocumentScreen(documentRef: d.ref),
                      ),
                    );
                    ref.invalidate(_documentsProvider);
                    ref.invalidate(_savedProvider);
                  },
                );
              },
            ),
    );
  }
}

String _docSubtitle(LibraryDocument d) => [
  if (!d.available) 'Đã gỡ — bản đã tải trên máy',
  ?d.category,
  if (d.publishedAt != null)
    'Xuất bản ${formatLocal(DateTime.tryParse(d.publishedAt!))}',
  if (d.pending) 'Chờ đồng bộ',
].join(' · ');

final _docProvider = FutureProvider.autoDispose
    .family<LibraryDocument?, String>(
      (ref, id) => servicesOf(ref).library.byRef(id),
    );

class DocumentScreen extends ConsumerWidget {
  const DocumentScreen({super.key, required this.documentRef});
  final String documentRef;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final d = ref.watch(_docProvider(documentRef));
    Future<void> toggle(LibraryDocument x) async {
      final lib = ref.services.library;
      if (x.saved) {
        await lib.unsave(x.ref);
      } else {
        await lib.save(x.ref);
      }
      ref.invalidate(_docProvider(documentRef));
      unawaitedSync(ref);
      if (context.mounted) {
        showMessage(
          context,
          x.saved ? 'Đã bỏ lưu.' : 'Đã lưu vào "Đã lưu" để đọc ngoại tuyến.',
        );
      }
    }

    return Scaffold(
      appBar: AppBar(
        title: const Text('Tài liệu'),
        actions: [
          if (d.value != null)
            IconButton(
              iconSize: 30,
              tooltip: d.value!.saved ? 'Bỏ lưu' : 'Lưu',
              icon: Icon(
                d.value!.saved ? Icons.bookmark : Icons.bookmark_border,
              ),
              onPressed: () => toggle(d.value!),
            ),
        ],
      ),
      body: d.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (x) => x == null
            ? const Center(
                child: Padding(
                  padding: EdgeInsets.all(24),
                  child: Text(
                    'Tài liệu chưa có trên máy. Hãy đồng bộ khi có mạng.',
                    textAlign: TextAlign.center,
                    style: TextStyle(fontSize: 17),
                  ),
                ),
              )
            : ListView(
                padding: const EdgeInsets.all(16),
                children: [
                  Text(x.title, style: Theme.of(context).textTheme.titleLarge),
                  const SizedBox(height: 6),
                  Text(
                    _docSubtitle(x),
                    style: const TextStyle(color: AgColors.mute),
                  ),
                  if (x.summary.isNotEmpty) ...[
                    const SizedBox(height: 12),
                    Text(
                      x.summary,
                      style: const TextStyle(
                        fontSize: 17,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                  ],
                  const SizedBox(height: 12),
                  SelectableText(x.body, style: const TextStyle(fontSize: 17)),
                  const SizedBox(height: 24),
                  OutlinedButton.icon(
                    onPressed: () => toggle(x),
                    icon: Icon(
                      x.saved ? Icons.bookmark_remove : Icons.bookmark_add,
                    ),
                    label: Text(x.saved ? 'Bỏ lưu' : 'Lưu để đọc ngoại tuyến'),
                  ),
                ],
              ),
      ),
    );
  }
}
