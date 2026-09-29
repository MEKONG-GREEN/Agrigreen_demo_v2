import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../domain/roles.dart';
import '../navigation.dart';
import '../providers.dart';
import '../widgets.dart';
import 'batch_log_screen.dart';
import 'dashboard_screen.dart';
import 'farm_log_screen.dart';
import 'harvest_screen.dart';
import 'households_screen.dart';
import 'bulk_open_screen.dart';
import 'news_screen.dart';
import 'notifications_screen.dart';
import 'plot_map_screen.dart';
import 'plots_screen.dart';
import 'review_screen.dart';
import 'staff_screen.dart';
import 'support_screen.dart';
import 'sync_queue_screen.dart';

/// Trang chủ theo vai (HTX US-LOGIN-01 AC-1): nông dân hoặc quản lý HTX.
class HomeScreen extends ConsumerWidget {
  const HomeScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final mode = ref.watch(appControllerProvider.select((s) => s.mode));
    final summary = ref.watch(homeSummaryProvider);
    void open(Widget w) =>
        Navigator.of(context)
            .push(MaterialPageRoute<void>(builder: (_) => w))
            .then((_) => ref.invalidate(homeSummaryProvider));
    final s = summary.value;
    final today = ref.read(appControllerProvider).services?.crop.today();
    // Thông báo được chạm (kể cả trước khi mở khóa) → mở đúng màn hình.
    void consumeLink(String? link) {
      if (link == null) return;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        final l = ref.read(pendingDeepLinkProvider.notifier).take();
        if (l != null && context.mounted) {
          openDeepLink(
            context,
            ref,
            l,
          ).then((_) => ref.invalidate(homeSummaryProvider));
        }
      });
    }

    ref.listen(pendingDeepLinkProvider, (_, next) => consumeLink(next));
    consumeLink(ref.read(pendingDeepLinkProvider));

    final tiles = switch (mode) {
      HtxMode.farmer => [
        BigTile(
          icon: Icons.grid_view,
          label: 'Ruộng của tôi',
          badge: s == null || s.plots == 0 ? null : '${s.plots}',
          onTap: () => open(const PlotsScreen()),
        ),
        BigTile(
          icon: Icons.edit_note,
          label: 'Ghi nhật ký',
          badge: (s?.needsSupplement ?? 0) > 0
              ? '!${s!.needsSupplement}'
              : null,
          onTap: () => open(const FarmLogScreen()),
        ),
        BigTile(
          icon: Icons.scale,
          label: 'Khai sản lượng',
          onTap: () => open(const HarvestScreen()),
        ),
        BigTile(
          icon: Icons.support_agent,
          label: 'Yêu cầu hỗ trợ',
          onTap: () => open(const SupportScreen()),
        ),
        BigTile(
          icon: Icons.contact_phone_outlined,
          label: 'Danh bạ cán bộ',
          onTap: () => open(const StaffScreen()),
        ),
        BigTile(
          icon: Icons.newspaper,
          label: 'Tin tức',
          onTap: () => open(const NewsScreen()),
        ),
        _queueTile(s, () => open(const SyncQueueScreen())),
      ],
      HtxMode.manager => [
        BigTile(
          icon: Icons.groups,
          label: 'Nông hộ',
          onTap: () => open(const HouseholdsScreen()),
        ),
        BigTile(
          icon: Icons.map_outlined,
          label: 'Bản đồ lô ruộng',
          onTap: () => open(const PlotMapScreen()),
        ),
        BigTile(
          icon: Icons.playlist_add,
          label: 'Mở vụ hàng loạt',
          onTap: () => open(const BulkOpenScreen()),
        ),
        BigTile(
          icon: Icons.edit_note,
          label: 'Ghi nhật ký hộ',
          onTap: () => open(const PlotsScreen(manager: true)),
        ),
        BigTile(
          icon: Icons.library_add_check_outlined,
          label: 'Nhật ký hàng loạt',
          onTap: () => open(const BatchLogScreen()),
        ),
        BigTile(
          icon: Icons.fact_check_outlined,
          label: 'Duyệt nhật ký',
          badge: (s?.reviewPending ?? 0) > 0 ? '${s!.reviewPending}' : null,
          onTap: () => open(const ReviewScreen()),
        ),
        BigTile(
          icon: Icons.bar_chart,
          label: 'Dashboard',
          onTap: () => open(const DashboardScreen()),
        ),
        BigTile(
          icon: Icons.newspaper,
          label: 'Tin tức',
          onTap: () => open(const NewsScreen()),
        ),
        _queueTile(s, () => open(const SyncQueueScreen())),
      ],
      HtxMode.none => [_queueTile(s, () => open(const SyncQueueScreen()))],
    };

    return Scaffold(
      appBar: AppBar(
        title: Text(
          mode == HtxMode.manager ? 'AgriGreen HTX · Quản lý' : 'AgriGreen HTX',
        ),
        actions: [
          IconButton(
            tooltip: 'Thông báo',
            iconSize: 28,
            icon: const Icon(Icons.notifications),
            onPressed: () => open(const NotificationsScreen()),
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
          padding: const EdgeInsets.all(12),
          children: [
            if (mode == HtxMode.none)
              const Card(
                color: AgColors.amberBg,
                child: Padding(
                  padding: EdgeInsets.all(16),
                  child: Text(
                    'Tài khoản chưa có vai Nông dân hoặc Quản lý HTX. '
                    'Đồng bộ khi có mạng, hoặc liên hệ quản lý HTX.',
                    style: TextStyle(fontSize: 16),
                  ),
                ),
              ),
            summary.when(
              data: (s) => SyncCard(summary: s),
              loading: () => const Card(
                child: Padding(
                  padding: EdgeInsets.all(24),
                  child: Center(child: CircularProgressIndicator()),
                ),
              ),
              error: (e, _) => Card(child: ListTile(title: Text('Lỗi: $e'))),
            ),
            const WeatherCard(),
            if (s?.advice != null && today != null)
              AdviceCard(
                season: s!.advice!.$1,
                step: s.advice!.$2,
                today: today,
              ),
            if ((s?.needsSupplement ?? 0) > 0)
              Card(
                color: AgColors.amberBg,
                child: ListTile(
                  leading: const Icon(
                    Icons.assignment_late,
                    color: AgColors.amber,
                  ),
                  title: Text('${s!.needsSupplement} nhật ký cần bổ sung'),
                  subtitle: const Text(
                    'Quản lý HTX yêu cầu bổ sung — chạm để sửa.',
                  ),
                  onTap: () => open(const FarmLogScreen()),
                ),
              ),
            const SizedBox(height: 8),
            GridView.count(
              crossAxisCount: 2,
              shrinkWrap: true,
              physics: const NeverScrollableScrollPhysics(),
              mainAxisSpacing: 10,
              crossAxisSpacing: 10,
              childAspectRatio: 1.3,
              children: tiles,
            ),
          ],
        ),
      ),
    );
  }

  static Widget _queueTile(HomeSummary? s, VoidCallback onTap) => BigTile(
    icon: Icons.cloud_sync_outlined,
    label: 'Hàng đợi đồng bộ',
    badge: s == null
        ? null
        : s.counts.rejected > 0
        ? '!${s.counts.rejected}'
        : s.waiting > 0
        ? '${s.waiting}'
        : null,
    onTap: onTap,
  );
}
