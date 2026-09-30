import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../domain/fieldops.dart';
import '../../domain/roles.dart';
import '../providers.dart';
import 'approval_screen.dart';
import 'order_detail_screen.dart';
import 'reconciliation_screen.dart';
import 'sync_queue_screen.dart';

/// Trang chủ: trạng thái đồng bộ + lệnh gom của tôi (pull `collection_order`);
/// giám sát gom có thêm "Duyệt lệnh gom" và "Đối soát chiều mua".
class HomeScreen extends ConsumerWidget {
  const HomeScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final mode = ref.watch(appControllerProvider.select((a) => a.mode));
    final home = ref.watch(homeProvider);
    Future<void> syncNow() async {
      await ref.services.sync.syncOnce(force: true);
      ref.invalidate(homeProvider);
    }

    return Scaffold(
      appBar: AppBar(
        title: const Text('Lệnh gom của tôi'),
        actions: [
          IconButton(
            tooltip: 'Khóa',
            icon: const Icon(Icons.lock_outline),
            onPressed: () => ref.read(appControllerProvider.notifier).lock(),
          ),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: syncNow,
        child: ListView(
          padding: const EdgeInsets.all(12),
          children: [
            if (mode == FieldMode.none)
              const Card(
                color: AgColors.amberBg,
                child: Padding(
                  padding: EdgeInsets.all(14),
                  child: Text(
                    'Tài khoản chưa có vai tổ gom / giám sát gom. Liên hệ quản trị Mekong Green.',
                    style: TextStyle(fontSize: 16),
                  ),
                ),
              ),
            home.when(
              loading: () => const LinearProgressIndicator(),
              error: (e, _) => Text('Lỗi: $e'),
              data: (d) => Card(
                child: ListTile(
                  key: const ValueKey('sync-card'),
                  leading: Icon(
                    d.waiting > 0 ? Icons.cloud_upload : Icons.cloud_done,
                    color: d.waiting > 0 ? AgColors.amber : AgColors.g700,
                  ),
                  title: Text(d.status.label),
                  subtitle: Text(
                    [
                      if (d.waiting > 0) '${d.waiting} chờ gửi',
                      if (d.counts.rejected > 0)
                        '${d.counts.rejected} bị từ chối',
                      'Lần cuối: ${formatLocal(d.status.lastSuccessAt)}',
                    ].join(' · '),
                  ),
                  onTap: () async {
                    await Navigator.of(context).push(
                      MaterialPageRoute<void>(
                        builder: (_) => const SyncQueueScreen(),
                      ),
                    );
                    ref.invalidate(homeProvider);
                  },
                  trailing: IconButton(
                    tooltip: 'Đồng bộ ngay',
                    icon: const Icon(Icons.sync),
                    onPressed: syncNow,
                  ),
                ),
              ),
            ),
            if (mode == FieldMode.supervisor) ...[
              const SizedBox(height: 8),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  OutlinedButton.icon(
                    onPressed: () => Navigator.of(context).push(
                      MaterialPageRoute<void>(
                        builder: (_) => const ApprovalScreen(),
                      ),
                    ),
                    icon: const Icon(Icons.fact_check),
                    label: const Text('Duyệt lệnh gom'),
                  ),
                  OutlinedButton.icon(
                    onPressed: () => Navigator.of(context).push(
                      MaterialPageRoute<void>(
                        builder: (_) => const ReconciliationScreen(),
                      ),
                    ),
                    icon: const Icon(Icons.balance),
                    label: const Text('Đối soát chiều mua'),
                  ),
                ],
              ),
            ],
            const SizedBox(height: 8),
            ...home.maybeWhen(
              data: (d) => d.orders.isEmpty
                  ? [
                      const Padding(
                        padding: EdgeInsets.all(24),
                        child: Text(
                          'Chưa có lệnh gom đã duyệt trên máy. Kéo xuống để đồng bộ.',
                          style: TextStyle(fontSize: 17),
                        ),
                      ),
                    ]
                  : [for (final o in d.orders) _OrderCard(order: o)],
              orElse: () => const <Widget>[],
            ),
          ],
        ),
      ),
    );
  }
}

class _OrderCard extends ConsumerWidget {
  const _OrderCard({required this.order});
  final CollectionOrder order;

  @override
  Widget build(BuildContext context, WidgetRef ref) => Card(
    child: ListTile(
      key: ValueKey('order-${order.orderId}'),
      leading: const Icon(Icons.assignment, color: AgColors.g700, size: 32),
      title: Text('Lệnh ${order.shortCode}'),
      subtitle: Text(
        '${OrderStatus.label(order.status)} · ${order.plotRefs.length} thửa · '
        '${order.windowLabel}',
      ),
      trailing: const Icon(Icons.chevron_right),
      onTap: () async {
        await Navigator.of(context).push(
          MaterialPageRoute<void>(
            builder: (_) => OrderDetailScreen(orderId: order.orderId),
          ),
        );
        ref.invalidate(homeProvider);
      },
    ),
  );
}
