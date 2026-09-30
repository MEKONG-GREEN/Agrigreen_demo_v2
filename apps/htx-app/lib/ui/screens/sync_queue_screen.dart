import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/crop_service.dart';
import '../../domain/cropcycle.dart';
import '../../domain/support.dart';
import '../providers.dart';
import '../widgets.dart';
import 'plot_map_screen.dart' show confirmBoundaryAfterServer;

class _Queue {
  const _Queue(this.records, this.files, this.actions);
  final List<OutboxRecord> records;
  final int files;
  final int actions;
}

final _queueProvider = FutureProvider.autoDispose<_Queue>((ref) async {
  final s = servicesOf(ref);
  return _Queue(
    await s.outbox.list(limit: 300),
    (await s.evidence.pending()).length,
    await s.actions.pendingCount(),
  );
});

/// Tóm tắt bản ghi cho người dùng.
String recordSummary(OutboxRecord r) {
  final p = r.payload;
  return switch (r.entity) {
    PushEntity.seasonOpen when p['cancel'] == true => 'Hủy vụ',
    PushEntity.seasonOpen when r.op == RecordOp.correct => [
      'Sửa vụ',
      if (p['variety_code'] != null) 'giống ${p['variety_code']}',
      if (p['sowing_date'] != null) 'sạ ${p['sowing_date']}',
    ].join(' · '),
    PushEntity.seasonOpen =>
      'Vụ ${p['season_code'] ?? ''} ${p['season_year'] ?? ''} · sạ ${p['sowing_date'] ?? ''}',
    PushEntity.farmLog => activityLabel(p['activity_code'] as String?),
    PushEntity.farmLogBatch =>
      '${activityLabel(p['activity_code'] as String?)} · ${((p['plot_refs'] as List?) ?? const []).length} lô',
    PushEntity.harvest => '${p['mass_kg']} kg',
    PushEntity.plotBoundary =>
      '${p['point_count'] ?? '?'} điểm · ≈ ${'${p['area_ha_client'] ?? ''}'.replaceAll('.', ',')} ha',
    PushEntity.htxCollectionResponse =>
      '${p['outcome'] == 'confirmed' ? 'Xác nhận' : 'Tranh chấp'} bảng kê · bản ${p['revision']}',
    PushEntity.supportRequest =>
      supportCategories[p['category']] ?? '${p['category']}',
    _ => '',
  };
}

/// Hàng đợi đồng bộ: chờ gửi, đã gửi (giữ 48 giờ), xung đột, bị từ chối.
class SyncQueueScreen extends ConsumerWidget {
  const SyncQueueScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final q = ref.watch(_queueProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Hàng đợi đồng bộ')),
      body: q.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (d) => ListView(
          padding: const EdgeInsets.all(8),
          children: [
            if (d.files > 0 || d.actions > 0)
              Card(
                child: ListTile(
                  leading: const Icon(
                    Icons.cloud_upload,
                    color: AgColors.amber,
                  ),
                  title: Text(
                    [
                      if (d.files > 0) '${d.files} ảnh chờ tải lên',
                      if (d.actions > 0) '${d.actions} thao tác chờ gửi',
                    ].join(' · '),
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
            for (final r in d.records) _RecordCard(record: r),
          ],
        ),
      ),
    );
  }
}

class _RecordCard extends ConsumerWidget {
  const _RecordCard({required this.record});
  final OutboxRecord record;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final r = record;
    final canConfirmYield =
        r.status == OutboxStatus.rejected &&
        r.entity == PushEntity.harvest &&
        r.errorCode == 'cropcycle.yield_anomaly' &&
        r.replacedBy == null;
    final summary = recordSummary(r);
    final boundary = r.entity == PushEntity.plotBoundary
        ? ref.read(appControllerProvider).services?.boundaries.statusOfRecord(r)
        : null;
    final canConfirmBoundary =
        (boundary?.canConfirm ?? false) && r.replacedBy == null;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    '${entityLabel(r.entity)}${summary.isEmpty ? '' : ' · $summary'}',
                    style: const TextStyle(
                      fontSize: 17,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ),
                StatusChip(r.status),
              ],
            ),
            const SizedBox(height: 4),
            Text(
              'Lúc ${r.eventTime}'
              '${r.op == RecordOp.correct ? ' · Bản bổ sung' : ''}'
              '${r.attempts > 1 ? ' · đã thử ${r.attempts} lần' : ''}',
              style: const TextStyle(color: AgColors.mute, fontSize: 14),
            ),
            if (boundary != null &&
                (r.status == OutboxStatus.conflict ||
                    r.status == OutboxStatus.rejected))
              Padding(
                padding: const EdgeInsets.only(top: 6),
                child: Text(
                  [
                    boundary.state.label,
                    if (r.status == OutboxStatus.rejected)
                      rejectMessage(r.errorCode),
                    ?boundary.detail,
                  ].join(' · '),
                  style: const TextStyle(color: AgColors.amber, fontSize: 15),
                ),
              ),
            if (canConfirmBoundary)
              Align(
                alignment: Alignment.centerRight,
                child: FilledButton.icon(
                  onPressed: () async {
                    await confirmBoundaryAfterServer(context, ref, boundary!);
                    ref.invalidate(_queueProvider);
                  },
                  icon: const Icon(Icons.fact_check),
                  label: const Text('Xác nhận giữ nguyên'),
                ),
              ),
            if (r.status == OutboxStatus.conflict && boundary == null)
              const Padding(
                padding: EdgeInsets.only(top: 6),
                child: Text(
                  'Máy chủ đã nhận nhưng có bản khác cùng lúc (sửa ở hai máy). '
                  'Cả hai được giữ lại, chờ quản lý HTX xử lý.',
                  style: TextStyle(color: AgColors.amber, fontSize: 15),
                ),
              ),
            if (r.status == OutboxStatus.rejected && boundary == null)
              Padding(
                padding: const EdgeInsets.only(top: 6),
                child: Text(
                  rejectMessage(r.errorCode),
                  style: const TextStyle(color: AgColors.red, fontSize: 15),
                ),
              ),
            if (r.replacedBy != null)
              const Padding(
                padding: EdgeInsets.only(top: 6),
                child: Text(
                  'Đã tạo bản ghi thay thế.',
                  style: TextStyle(color: AgColors.g700, fontSize: 15),
                ),
              ),
            if (canConfirmYield)
              Align(
                alignment: Alignment.centerRight,
                child: FilledButton.icon(
                  onPressed: () async {
                    final ok = await confirmDialog(
                      context,
                      title: 'Sản lượng bất thường',
                      message:
                          'Máy chủ báo sản lượng ${r.payload['mass_kg']} kg vượt ngưỡng '
                          'bất thường. Bạn xác nhận số liệu đúng?',
                      ok: 'Xác nhận đúng',
                    );
                    if (!ok) return;
                    try {
                      await ref.services.crop.confirmRejectedHarvest(r);
                      if (context.mounted) {
                        showMessage(context, 'Đã xác nhận — Chờ đồng bộ.');
                      }
                    } on CropError catch (e) {
                      if (context.mounted) {
                        showMessage(context, e.message, error: true);
                      }
                    }
                    ref.invalidate(_queueProvider);
                  },
                  icon: const Icon(Icons.fact_check),
                  label: const Text('Xác nhận sản lượng'),
                ),
              ),
          ],
        ),
      ),
    );
  }
}
