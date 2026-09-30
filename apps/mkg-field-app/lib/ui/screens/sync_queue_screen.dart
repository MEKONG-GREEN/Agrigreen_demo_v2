import 'package:agrigreen_mobile/data/actions/action_queue.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../domain/fieldops.dart';
import '../../domain/trips.dart';
import '../providers.dart';

class _Queue {
  const _Queue(
    this.records,
    this.pendingActions,
    this.failedActions,
    this.milestoneConflicts,
    this.tripCodes,
  );
  final List<OutboxRecord> records;
  final int pendingActions;
  final List<ApiAction> failedActions;

  /// Mốc chuyến bị máy chủ từ chối (409 `logistics.*`…).
  final List<LocalMilestone> milestoneConflicts;
  final Map<String, String> tripCodes;
}

final _queueProvider = FutureProvider.autoDispose<_Queue>((ref) async {
  final s = servicesOf(ref);
  return _Queue(
    await s.outbox.list(limit: 300),
    await s.actions.pendingCount(),
    await s.field.failedShiftActions(),
    await s.trips.conflicts(),
    {for (final v in await s.trips.trips()) v.trip.tripId: v.trip.code},
  );
});

String countSummary(OutboxRecord r) {
  final p = r.payload;
  final stage = Stage.tryParse(p['stage'] as String?);
  final kg = p['loose_kg'];
  return [
    stage?.label ?? '',
    'thửa ${shortRef('${p['plot_ref'] ?? ''}')}',
    kg != null ? '$kg kg rơm rời' : '${p['rolls']} cuộn',
  ].join(' · ');
}

/// Hàng đợi đồng bộ: chờ gửi, đã gửi (giữ 48 giờ), bị từ chối kèm lý do
/// (mã `fieldops.*`), lệnh ca bị từ chối.
class SyncQueueScreen extends ConsumerWidget {
  const SyncQueueScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final q = ref.watch(_queueProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('Hàng đợi đồng bộ'),
        actions: [
          IconButton(
            tooltip: 'Đồng bộ ngay',
            icon: const Icon(Icons.sync),
            onPressed: () async {
              await ref.services.sync.syncOnce(force: true);
              ref.invalidate(_queueProvider);
            },
          ),
        ],
      ),
      body: q.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (d) => ListView(
          padding: const EdgeInsets.all(8),
          children: [
            if (d.pendingActions > 0)
              Card(
                child: ListTile(
                  leading: const Icon(
                    Icons.cloud_upload,
                    color: AgColors.amber,
                  ),
                  title: Text(
                    '${d.pendingActions} thao tác (ca / mốc chuyến) chờ gửi',
                  ),
                ),
              ),
            for (final a in d.failedActions)
              Card(
                child: ListTile(
                  leading: const Icon(Icons.error_outline, color: AgColors.red),
                  title: Text(
                    '${a.body?['kind'] == 'opened' ? 'Mở' : 'Đóng'} ca · lệnh ${shortRef(a.target ?? '')}',
                  ),
                  subtitle: Text(rejectMessage(a.lastError)),
                ),
              ),
            for (final m in d.milestoneConflicts)
              Card(
                key: ValueKey('ms-conflict-${m.actionId}'),
                color: AgColors.redBg,
                child: ListTile(
                  leading: const Icon(Icons.error_outline, color: AgColors.red),
                  title: Text(
                    '${m.milestone.label} · chuyến '
                    '${d.tripCodes[m.tripId] ?? shortRef(m.tripId)}',
                  ),
                  subtitle: Text(
                    'Lúc ${formatLocal(m.eventTime)} — '
                    '${logisticsMessage(m.errorCode)}',
                  ),
                ),
              ),
            if (d.records.isEmpty)
              const Padding(
                padding: EdgeInsets.all(24),
                child: Text(
                  'Không có bản ghi nào trên máy.',
                  style: TextStyle(fontSize: 17),
                ),
              ),
            for (final r in d.records)
              Card(
                key: ValueKey('queue-${r.id}'),
                child: Padding(
                  padding: const EdgeInsets.all(12),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Row(
                        children: [
                          Expanded(
                            child: Text(
                              '${entityLabel(r.entity)} · ${countSummary(r)}',
                              style: const TextStyle(
                                fontSize: 17,
                                fontWeight: FontWeight.w700,
                              ),
                            ),
                          ),
                          StatusChip(r.status),
                        ],
                      ),
                      Text(
                        'Lúc ${r.eventTime}'
                        '${r.op == RecordOp.correct ? ' · Bản ghi bù' : ''}'
                        '${r.attempts > 1 ? ' · đã thử ${r.attempts} lần' : ''}',
                        style: const TextStyle(
                          color: AgColors.mute,
                          fontSize: 14,
                        ),
                      ),
                      if (r.correctionReason != null)
                        Text('Lý do sửa: ${r.correctionReason}'),
                      if (r.status == OutboxStatus.rejected)
                        Text(
                          rejectMessage(r.errorCode),
                          key: ValueKey('reject-${r.id}'),
                          style: const TextStyle(
                            color: AgColors.red,
                            fontSize: 15,
                          ),
                        ),
                      if (r.status == OutboxStatus.rejected &&
                          r.errorSummary.isNotEmpty)
                        Text(
                          r.errorSummary,
                          style: const TextStyle(
                            color: AgColors.mute,
                            fontSize: 13,
                          ),
                        ),
                      if (r.replacedBy != null)
                        const Text(
                          'Đã ghi lại số thay thế.',
                          style: TextStyle(color: AgColors.g700, fontSize: 15),
                        ),
                    ],
                  ),
                ),
              ),
          ],
        ),
      ),
    );
  }
}
