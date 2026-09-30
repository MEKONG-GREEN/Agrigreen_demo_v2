import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/trip_service.dart';
import '../../domain/trips.dart';
import '../providers.dart';
import 'sync_queue_screen.dart';
import 'trip_detail_screen.dart';

class TripListData {
  const TripListData(this.trips, this.fetchedAt, this.pendingActions);
  final List<TripView> trips;
  final DateTime? fetchedAt;
  final int pendingActions;
}

final tripListProvider = FutureProvider<TripListData>((ref) async {
  ref.watch(syncStatusStreamProvider);
  final s = servicesOf(ref);
  return TripListData(
    await s.trips.trips(),
    await s.trips.fetchedAt(),
    await s.actions.pendingCount(),
  );
});

/// Chuyến của tôi (vai `driver`): chuyến chờ xuất phát / đang chạy / đã đến
/// trên máy (tải khi đồng bộ). [asHome] = trang chủ của tài khoản chỉ có vai
/// tài xế (có nút khóa, thẻ đồng bộ).
class TripListScreen extends ConsumerWidget {
  const TripListScreen({super.key, this.asHome = false});
  final bool asHome;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final data = ref.watch(tripListProvider);
    final status = ref.watch(syncStatusStreamProvider).value;
    Future<void> syncNow() async {
      await ref.services.sync.syncOnce(force: true);
      ref.invalidate(tripListProvider);
    }

    return Scaffold(
      appBar: AppBar(
        title: const Text('Chuyến của tôi'),
        actions: [
          IconButton(
            tooltip: 'Đồng bộ ngay',
            icon: const Icon(Icons.sync),
            onPressed: syncNow,
          ),
          if (asHome)
            IconButton(
              tooltip: 'Khóa',
              icon: const Icon(Icons.lock_outline),
              onPressed: () => ref.read(appControllerProvider.notifier).lock(),
            ),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: syncNow,
        child: data.when(
          loading: () => const Center(child: CircularProgressIndicator()),
          error: (e, _) => Center(child: Text('Lỗi: $e')),
          data: (d) => ListView(
            padding: const EdgeInsets.all(12),
            children: [
              Card(
                child: ListTile(
                  key: const ValueKey('trip-sync-card'),
                  leading: Icon(
                    d.pendingActions > 0
                        ? Icons.cloud_upload
                        : Icons.cloud_done,
                    color: d.pendingActions > 0
                        ? AgColors.amber
                        : AgColors.g700,
                  ),
                  title: Text(status?.label ?? 'Đồng bộ'),
                  subtitle: Text(
                    [
                      if (d.pendingActions > 0)
                        '${d.pendingActions} thao tác chờ gửi',
                      'Tải chuyến lúc: ${formatLocal(d.fetchedAt)}',
                    ].join(' · '),
                  ),
                  onTap: () async {
                    await Navigator.of(context).push(
                      MaterialPageRoute<void>(
                        builder: (_) => const SyncQueueScreen(),
                      ),
                    );
                    ref.invalidate(tripListProvider);
                  },
                ),
              ),
              const SizedBox(height: 8),
              if (d.trips.isEmpty)
                const Padding(
                  padding: EdgeInsets.all(24),
                  child: Text(
                    'Chưa có chuyến nào giao cho bạn trên máy. Kéo xuống để đồng bộ.',
                    style: TextStyle(fontSize: 17),
                  ),
                ),
              for (final v in d.trips) _TripCard(view: v),
            ],
          ),
        ),
      ),
    );
  }
}

class TripStatusChip extends StatelessWidget {
  const TripStatusChip(this.status, {super.key});
  final String status;

  @override
  Widget build(BuildContext context) {
    final (bg, fg) = switch (status) {
      TripStatus.planned => (AgColors.amberBg, AgColors.amber),
      TripStatus.departed => (const Color(0xFFE6F4FA), AgColors.blue),
      TripStatus.arrived => (AgColors.g100, AgColors.g700),
      _ => (const Color(0xFFF3F4F6), AgColors.mute),
    };
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
      decoration: BoxDecoration(
        color: bg,
        borderRadius: BorderRadius.circular(12),
      ),
      child: Text(
        TripStatus.label(status),
        style: TextStyle(color: fg, fontWeight: FontWeight.w700),
      ),
    );
  }
}

class _TripCard extends ConsumerWidget {
  const _TripCard({required this.view});
  final TripView view;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final t = view.trip;
    final dest = t.destination;
    return Card(
      child: InkWell(
        key: ValueKey('trip-${t.tripId}'),
        onTap: () async {
          await Navigator.of(context).push(
            MaterialPageRoute<void>(
              builder: (_) => TripDetailScreen(tripId: t.tripId),
            ),
          );
          ref.invalidate(tripListProvider);
        },
        child: Padding(
          padding: const EdgeInsets.all(12),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Icon(
                    t.mode == 'waterway'
                        ? Icons.directions_boat
                        : Icons.local_shipping,
                    color: AgColors.g700,
                    size: 30,
                  ),
                  const SizedBox(width: 8),
                  // Màn hình hẹp: mã chuyến và nhãn trạng thái tự xuống dòng.
                  Expanded(
                    child: Wrap(
                      spacing: 8,
                      runSpacing: 4,
                      crossAxisAlignment: WrapCrossAlignment.center,
                      children: [
                        Text(
                          'Chuyến ${t.code}',
                          style: const TextStyle(
                            fontSize: 18,
                            fontWeight: FontWeight.w700,
                          ),
                        ),
                        TripStatusChip(view.status),
                      ],
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 4),
              Text(
                [
                  modeLabel(t.mode),
                  'ước ${tonnesLabel(t.plannedKg)}',
                  if (dest != null) 'đến: ${dest.label}',
                ].join(' · '),
                style: const TextStyle(fontSize: 15),
              ),
              if (view.pending)
                const Text(
                  'Có mốc chờ đồng bộ',
                  style: TextStyle(color: AgColors.amber, fontSize: 15),
                ),
              if (view.conflicts.isNotEmpty)
                Text(
                  '${view.conflicts.length} mốc bị máy chủ từ chối — mở để xem',
                  key: ValueKey('trip-conflict-${t.tripId}'),
                  style: const TextStyle(color: AgColors.red, fontSize: 15),
                ),
            ],
          ),
        ),
      ),
    );
  }
}
