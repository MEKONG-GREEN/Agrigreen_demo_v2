import 'package:agrigreen_mobile/application/inbox_service.dart';
import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../navigation.dart';
import '../providers.dart';

final inboxProvider = FutureProvider.autoDispose<List<InboxItem>>((ref) {
  ref.watch(syncStatusStreamProvider);
  return servicesOf(ref).inbox.list();
});

/// Hộp thư trong app (pf/notify, 30 ngày gần nhất). Đọc được khi ngoại tuyến;
/// đánh dấu đã đọc được xếp hàng và gửi khi có mạng.
class NotificationsScreen extends ConsumerWidget {
  const NotificationsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final items = ref.watch(inboxProvider);
    final hasUnread = items.value?.any((i) => !i.read) ?? false;
    return Scaffold(
      appBar: AppBar(
        title: const Text('Thông báo'),
        actions: [
          if (hasUnread)
            TextButton(
              onPressed: () async {
                await ref.services.inbox.markAllRead();
                ref.invalidate(inboxProvider);
                ref.invalidate(homeSummaryProvider);
                unawaitedSync(ref);
              },
              child: const Text(
                'Đọc hết',
                style: TextStyle(color: Colors.white, fontSize: 16),
              ),
            ),
        ],
      ),
      body: items.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (list) => list.isEmpty
            ? const Center(
                child: Padding(
                  padding: EdgeInsets.all(24),
                  child: Text(
                    'Chưa có thông báo nào trong 30 ngày gần đây.',
                    textAlign: TextAlign.center,
                    style: TextStyle(fontSize: 17),
                  ),
                ),
              )
            : ListView.separated(
                itemCount: list.length,
                separatorBuilder: (_, _) => const Divider(height: 1),
                itemBuilder: (_, i) => _InboxTile(item: list[i]),
              ),
      ),
    );
  }
}

/// Đồng bộ nền để gửi lệnh vừa xếp hàng (không chờ).
void unawaitedSync(WidgetRef ref) {
  final s = ref.read(appControllerProvider).services;
  s?.sync.syncOnce(force: true).ignore();
}

class _InboxTile extends ConsumerWidget {
  const _InboxTile({required this.item});
  final InboxItem item;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final color = item.urgent ? AgColors.red : AgColors.g700;
    return ListTile(
      tileColor: item.read ? Colors.white : AgColors.g100,
      leading: Icon(
        switch (item.kind) {
          'directive' => Icons.campaign,
          'alert' => Icons.warning_amber,
          'document' => Icons.menu_book,
          'task_escalated' => Icons.assignment_late,
          'sync_conflict' => Icons.sync_problem,
          _ => Icons.notifications,
        },
        color: color,
        size: 32,
      ),
      title: Text(
        item.title,
        style: TextStyle(
          fontWeight: item.read ? FontWeight.w500 : FontWeight.w800,
        ),
      ),
      subtitle: Text(
        [
          item.kindLabel,
          if (item.urgent) 'Khẩn',
          formatLocal(item.createdAt),
          if (item.pendingRead) 'Chờ đồng bộ',
        ].join(' · '),
      ),
      trailing: item.read
          ? null
          : const Icon(Icons.circle, size: 12, color: AgColors.red),
      onTap: () async {
        await ref.services.inbox.markRead(item.id);
        ref.invalidate(inboxProvider);
        ref.invalidate(homeSummaryProvider);
        unawaitedSync(ref);
        if (context.mounted) await openDeepLink(context, item.deepLink);
      },
    );
  }
}

/// Chuông + số chưa đọc trên thanh tiêu đề.
class InboxBell extends StatelessWidget {
  const InboxBell({super.key, required this.unread, required this.onTap});
  final int unread;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) => IconButton(
    tooltip: unread > 0 ? 'Thông báo ($unread chưa đọc)' : 'Thông báo',
    iconSize: 30,
    onPressed: onTap,
    icon: Badge(
      isLabelVisible: unread > 0,
      label: Text(unread > 99 ? '99+' : '$unread'),
      backgroundColor: AgColors.red,
      child: const Icon(Icons.notifications),
    ),
  );
}
