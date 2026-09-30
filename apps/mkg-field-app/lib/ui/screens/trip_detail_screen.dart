import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/trip_service.dart';
import '../../domain/fieldops.dart' show shortRef;
import '../../domain/trips.dart';
import '../providers.dart';
import 'trip_list_screen.dart';

final tripDetailProvider = FutureProvider.autoDispose.family<TripView?, String>(
  (ref, id) async {
    ref.watch(syncStatusStreamProvider);
    return servicesOf(ref).trips.trip(id);
  },
);

/// Chi tiết chuyến: chặng, điểm đến, tấn ước, mốc xuất phát / đến. Chỉ nút
/// của mốc kế tiếp được bấm (chỉ tiến từng bậc — phản chiếu máy chủ, L-02).
class TripDetailScreen extends ConsumerStatefulWidget {
  const TripDetailScreen({super.key, required this.tripId});
  final String tripId;

  @override
  ConsumerState<TripDetailScreen> createState() => _TripDetailState();
}

class _TripDetailState extends ConsumerState<TripDetailScreen> {
  bool _busy = false;

  Future<void> _record(TripView v, Milestone m) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (c) => AlertDialog(
        title: Text(m.action),
        content: Text(
          'Ghi mốc "${m.label}" cho chuyến ${v.trip.code} lúc này, kèm vị trí '
          'GPS? Mốc đã ghi không sửa lùi được.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(c).pop(false),
            child: const Text('Hủy'),
          ),
          FilledButton(
            key: const ValueKey('milestone-confirm'),
            onPressed: () => Navigator.of(c).pop(true),
            child: const Text('Ghi mốc'),
          ),
        ],
      ),
    );
    if (ok != true || !mounted) return;
    setState(() => _busy = true);
    final messenger = ScaffoldMessenger.of(context);
    try {
      final l = await ref.services.trips.record(widget.tripId, m);
      messenger.showSnackBar(
        SnackBar(
          content: Text('Đã ghi "${m.label}" (${l.gpsLabel}) — chờ đồng bộ.'),
        ),
      );
      // Có mạng thì gửi ngay; không có thì nằm trong hàng đợi.
      await ref.services.sync.syncOnce(force: true);
    } on TripError catch (e) {
      messenger.showSnackBar(
        SnackBar(content: Text(e.message), backgroundColor: AgColors.red),
      );
    } finally {
      if (mounted) setState(() => _busy = false);
      ref.invalidate(tripDetailProvider(widget.tripId));
      ref.invalidate(tripListProvider);
    }
  }

  @override
  Widget build(BuildContext context) {
    final async = ref.watch(tripDetailProvider(widget.tripId));
    return Scaffold(
      appBar: AppBar(title: const Text('Chi tiết chuyến')),
      body: async.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (v) => v == null
            ? const Padding(
                padding: EdgeInsets.all(24),
                child: Text(
                  'Chuyến không còn trên máy (đã hoàn tất, bị hủy hoặc giao cho người khác).',
                  style: TextStyle(fontSize: 17),
                ),
              )
            : _body(v),
      ),
    );
  }

  Widget _body(TripView v) {
    final t = v.trip;
    final next = v.next;
    final dest = t.destination;
    return ListView(
      padding: const EdgeInsets.all(12),
      children: [
        Card(
          child: Padding(
            padding: const EdgeInsets.all(12),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Wrap(
                  spacing: 8,
                  runSpacing: 4,
                  crossAxisAlignment: WrapCrossAlignment.center,
                  children: [
                    Text(
                      'Chuyến ${t.code}',
                      style: const TextStyle(
                        fontSize: 20,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    TripStatusChip(
                      v.status,
                      key: const ValueKey('trip-status'),
                    ),
                  ],
                ),
                const SizedBox(height: 6),
                Text(
                  '${modeLabel(t.mode)} · ${t.vehicleLabel}',
                  style: const TextStyle(fontSize: 16),
                ),
                Text(
                  'Tấn ước: ${tonnesLabel(t.plannedKg)}',
                  key: const ValueKey('trip-tonnes'),
                  style: const TextStyle(fontSize: 16),
                ),
                Text(
                  dest == null
                      ? 'Điểm đến: chưa có chặng (điều phối chưa lập tuyến)'
                      : 'Điểm đến: ${dest.label} (${dest.coords})',
                  key: const ValueKey('trip-destination'),
                  style: const TextStyle(fontSize: 16),
                ),
                if (t.flags.contains('payload_exceeded'))
                  const Text(
                    'Cảnh báo: tấn ước vượt 1,1 × tải của phương tiện.',
                    style: TextStyle(color: AgColors.amber),
                  ),
              ],
            ),
          ),
        ),
        for (final c in v.conflicts)
          Card(
            key: ValueKey('conflict-${c.actionId}'),
            color: AgColors.redBg,
            child: ListTile(
              leading: const Icon(Icons.error_outline, color: AgColors.red),
              title: Text('Mốc "${c.milestone.label}" bị máy chủ từ chối'),
              subtitle: Text(
                'Ghi lúc ${formatLocal(c.eventTime)} — '
                '${logisticsMessage(c.errorCode)}',
              ),
            ),
          ),
        const SizedBox(height: 4),
        const Text(
          'Mốc chuyến',
          style: TextStyle(fontSize: 18, fontWeight: FontWeight.w700),
        ),
        for (final m in Milestone.values) _milestoneRow(v, m),
        const SizedBox(height: 8),
        if (next != null)
          FilledButton.icon(
            key: ValueKey('record-${next.code}'),
            onPressed: _busy ? null : () => _record(v, next),
            icon: Icon(
              next == Milestone.departed ? Icons.play_arrow : Icons.flag,
            ),
            label: Text(next.action),
          )
        else
          Text(
            switch (v.status) {
              TripStatus.arrived =>
                'Đã đến nơi — chờ cân nhà máy / điều phối hoàn tất chuyến.',
              TripStatus.completed => 'Chuyến đã hoàn tất.',
              TripStatus.cancelled => 'Chuyến đã bị hủy.',
              _ => '',
            },
            key: const ValueKey('no-next-milestone'),
            style: const TextStyle(fontSize: 16, color: AgColors.mute),
          ),
        const SizedBox(height: 12),
        const Text(
          'Chặng',
          style: TextStyle(fontSize: 18, fontWeight: FontWeight.w700),
        ),
        if (t.legs.isEmpty)
          const Padding(
            padding: EdgeInsets.symmetric(vertical: 8),
            child: Text('Chưa có chặng trên máy.'),
          ),
        for (final l in [...t.legs]..sort((a, b) => a.seq.compareTo(b.seq)))
          Card(
            key: ValueKey('leg-${l.seq}'),
            child: ListTile(
              leading: CircleAvatar(child: Text('${l.seq}')),
              title: Text('${l.from.label} → ${l.to.label}'),
              subtitle: Text(
                [
                  modeLabel(l.mode),
                  if (l.distanceLabel.isNotEmpty) l.distanceLabel,
                  if (l.to.ref != null) 'điểm ${shortRef(l.to.ref!)}',
                ].join(' · '),
              ),
            ),
          ),
      ],
    );
  }

  Widget _milestoneRow(TripView v, Milestone m) {
    final at = v.timeOf(m);
    final local = v.localOf(m);
    final done = at != null;
    return ListTile(
      key: ValueKey('milestone-${m.code}'),
      contentPadding: EdgeInsets.zero,
      leading: Icon(
        done ? Icons.check_circle : Icons.radio_button_unchecked,
        color: done ? AgColors.g700 : AgColors.mute,
      ),
      title: Text(m.label),
      subtitle: Text(
        !done
            ? 'Chưa ghi'
            : [
                formatLocal(at),
                if (local != null) local.gpsLabel,
                if (local?.state == MilestoneState.pending) 'Chờ đồng bộ',
              ].join(' · '),
      ),
    );
  }
}
