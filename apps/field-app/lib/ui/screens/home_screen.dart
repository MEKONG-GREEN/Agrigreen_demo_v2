import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../data/sync/sync_service.dart';
import '../navigation.dart';
import '../providers.dart';
import '../theme.dart';
import '../widgets/common.dart';
import 'directives_screen.dart';
import 'field_report_screen.dart';
import 'library_screen.dart';
import 'notifications_screen.dart';
import 'plots_screen.dart';
import 'sync_queue_screen.dart';
import 'tasks_screen.dart';

class HomeScreen extends ConsumerWidget {
  const HomeScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final summary = ref.watch(homeSummaryProvider);
    void open(Widget w) =>
        Navigator.of(context)
            .push(MaterialPageRoute<void>(builder: (_) => w))
            .then((_) => ref.invalidate(homeSummaryProvider));
    // Thông báo được chạm (kể cả trước khi mở khóa) → mở đúng màn hình.
    void consumeLink(String? link) {
      if (link == null) return;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        final l = ref.read(pendingDeepLinkProvider.notifier).take();
        if (l != null && context.mounted) {
          openDeepLink(
            context,
            l,
          ).then((_) => ref.invalidate(homeSummaryProvider));
        }
      });
    }

    ref.listen(pendingDeepLinkProvider, (_, next) => consumeLink(next));
    consumeLink(ref.read(pendingDeepLinkProvider));
    return Scaffold(
      appBar: AppBar(
        title: const Text('AgriGreen Hiện trường'),
        actions: [
          InboxBell(
            unread: summary.value?.unreadNotifications ?? 0,
            onTap: () => open(const NotificationsScreen()),
          ),
          IconButton(
            tooltip: 'Khóa',
            iconSize: 28,
            icon: const Icon(Icons.lock),
            onPressed: () => ref.read(appControllerProvider.notifier).lock(),
          ),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: () async {
          await ref
              .read(appControllerProvider)
              .services
              ?.sync
              .syncOnce(force: true);
          ref.invalidate(homeSummaryProvider);
        },
        child: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            summary.when(
              data: (s) => _SyncCard(summary: s),
              loading: () => const Card(
                child: Padding(
                  padding: EdgeInsets.all(24),
                  child: Center(child: CircularProgressIndicator()),
                ),
              ),
              error: (e, _) => Card(child: ListTile(title: Text('Lỗi: $e'))),
            ),
            const SizedBox(height: 16),
            GridView.count(
              crossAxisCount: 2,
              shrinkWrap: true,
              physics: const NeverScrollableScrollPhysics(),
              mainAxisSpacing: 12,
              crossAxisSpacing: 12,
              childAspectRatio: 1.3,
              children: [
                BigTile(
                  icon: Icons.map_outlined,
                  label: 'Thửa ruộng',
                  badge: (summary.value?.boundaryAttention ?? 0) > 0
                      ? '!${summary.value!.boundaryAttention}'
                      : summary.value == null
                      ? null
                      : '${summary.value!.plots}',
                  onTap: () => open(const PlotsScreen()),
                ),
                BigTile(
                  icon: Icons.assignment_outlined,
                  label: 'Nhiệm vụ',
                  badge: (summary.value?.openTasks ?? 0) > 0
                      ? '${summary.value!.openTasks}'
                      : null,
                  onTap: () => open(const TasksScreen()),
                ),
                BigTile(
                  icon: Icons.edit_note,
                  label: 'Báo cáo hiện trường',
                  onTap: () => open(const FieldReportScreen()),
                ),
                BigTile(
                  icon: Icons.campaign_outlined,
                  label: 'Chỉ đạo',
                  badge: (summary.value?.unackedDirectives ?? 0) > 0
                      ? '${summary.value!.unackedDirectives}'
                      : null,
                  onTap: () => open(const DirectivesScreen()),
                ),
                BigTile(
                  icon: Icons.menu_book_outlined,
                  label: 'Thư viện · Đã lưu',
                  onTap: () => open(const LibraryScreen()),
                ),
                BigTile(
                  icon: Icons.cloud_sync_outlined,
                  label: 'Hàng đợi đồng bộ',
                  badge: (summary.value?.counts.rejected ?? 0) > 0
                      ? '${summary.value!.counts.rejected}'
                      : null,
                  onTap: () => open(const SyncQueueScreen()),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _SyncCard extends ConsumerWidget {
  const _SyncCard({required this.summary});
  final HomeSummary summary;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final s = summary.status;
    final c = summary.counts;
    final color = switch (s.state) {
      SyncState.idle => AgColors.g700,
      SyncState.running => AgColors.blue,
      SyncState.revoked || SyncState.needsLogin => AgColors.red,
      _ => AgColors.amber,
    };
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.circle, size: 14, color: color),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    s.label,
                    style: const TextStyle(
                      fontSize: 18,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 8),
            Text(
              'Lần đồng bộ cuối: ${formatLocal(s.lastSuccessAt)}',
              style: const TextStyle(color: AgColors.mute),
            ),
            if (s.nextAttemptAt != null && s.state != SyncState.idle)
              Text(
                'Thử lại lúc: ${formatLocal(s.nextAttemptAt)}',
                style: const TextStyle(color: AgColors.mute),
              ),
            const SizedBox(height: 12),
            Wrap(
              spacing: 16,
              runSpacing: 8,
              children: [
                _Count(
                  'Chờ đồng bộ',
                  c.pending + summary.pendingFiles + summary.pendingActions,
                  AgColors.amber,
                ),
                _Count('Bị từ chối', c.rejected, AgColors.red),
                _Count('Xung đột', c.conflict, AgColors.amber),
                _Count('Đã gửi', c.sent + c.superseded, AgColors.g700),
              ],
            ),
            const SizedBox(height: 12),
            SizedBox(
              width: double.infinity,
              child: FilledButton.icon(
                onPressed: s.state == SyncState.running
                    ? null
                    : () async {
                        await ref
                            .read(appControllerProvider)
                            .services
                            ?.sync
                            .syncOnce(force: true);
                        ref.invalidate(homeSummaryProvider);
                      },
                icon: const Icon(Icons.sync),
                label: const Text('Đồng bộ ngay'),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _Count extends StatelessWidget {
  const _Count(this.label, this.n, this.color);
  final String label;
  final int n;
  final Color color;
  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      Text(
        '$n',
        style: TextStyle(
          fontSize: 24,
          fontWeight: FontWeight.w800,
          color: color,
        ),
      ),
      Text(label, style: const TextStyle(fontSize: 14)),
    ],
  );
}
