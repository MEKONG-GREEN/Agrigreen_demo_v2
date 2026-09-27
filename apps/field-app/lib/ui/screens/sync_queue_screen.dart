import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/field_report_service.dart';
import '../../data/outbox/outbox_repository.dart';
import '../../domain/outbox_record.dart';
import '../providers.dart';
import '../theme.dart';
import '../widgets/common.dart';

final queueProvider = FutureProvider.autoDispose<List<OutboxRecord>>(
  (ref) => servicesOf(ref).outbox.list(limit: 300),
);

/// Hàng đợi đồng bộ: bản ghi chờ gửi, đã gửi (giữ 48 giờ), xung đột, bị từ chối.
class SyncQueueScreen extends ConsumerWidget {
  const SyncQueueScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final q = ref.watch(queueProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Hàng đợi đồng bộ')),
      body: q.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (list) => list.isEmpty
            ? const Center(
                child: Text(
                  'Không có bản ghi nào trên máy.',
                  style: TextStyle(fontSize: 17),
                ),
              )
            : ListView.builder(
                padding: const EdgeInsets.all(8),
                itemCount: list.length,
                itemBuilder: (_, i) => _RecordCard(record: list[i]),
              ),
      ),
    );
  }
}

class _RecordCard extends ConsumerWidget {
  const _RecordCard({required this.record});
  final OutboxRecord record;

  Future<void> _correct(BuildContext context, WidgetRef ref) async {
    final s = ref.read(appControllerProvider).services;
    if (s == null) return;
    final reason = TextEditingController();
    final note = TextEditingController(
      text: record.payload['note'] as String? ?? '',
    );
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Tạo bản ghi bù'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Text(
              'Bản ghi đã lưu không sửa được. Bản ghi bù sẽ thay thế, giữ nguyên GPS và thời gian.',
            ),
            const SizedBox(height: 12),
            TextField(
              controller: reason,
              decoration: const InputDecoration(
                labelText: 'Lý do sửa (bắt buộc)',
              ),
            ),
            const SizedBox(height: 8),
            TextField(
              controller: note,
              maxLines: 2,
              decoration: const InputDecoration(labelText: 'Ghi chú mới'),
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Hủy'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Lưu bản ghi bù'),
          ),
        ],
      ),
    );
    if (ok != true) return;
    try {
      if (record.entity == PushEntity.fieldReport) {
        await s.reports.correct(
          original: record,
          reason: reason.text,
          note: note.text,
        );
      } else {
        await s.outbox.correct(
          originalId: record.id,
          reason: reason.text,
          payload: {...record.payload, 'note': note.text},
        );
      }
      if (context.mounted) showMessage(context, 'Đã tạo bản ghi bù.');
      ref.invalidate(queueProvider);
    } on OutboxException catch (e) {
      if (context.mounted) showMessage(context, e.message, error: true);
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final r = record;
    final canCorrect =
        r.status == OutboxStatus.rejected ||
        r.status == OutboxStatus.sent ||
        r.status == OutboxStatus.conflict;
    final activity = FarmActivity.byCode(r.payload['activity'] as String?)
        ?.label;
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
                    '${entityLabel(r.entity)}${activity != null ? ' · $activity' : ''}',
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
              'Lúc ${r.eventTime} · #${r.seq}'
              '${r.op == RecordOp.correct ? ' · Bản ghi bù' : ''}'
              '${r.attempts > 1 ? ' · đã thử ${r.attempts} lần' : ''}',
              style: const TextStyle(color: AgColors.mute, fontSize: 14),
            ),
            if (r.correctionReason != null)
              Text(
                'Lý do sửa: ${r.correctionReason}',
                style: const TextStyle(fontSize: 14),
              ),
            if (r.status == OutboxStatus.conflict)
              const Padding(
                padding: EdgeInsets.only(top: 6),
                child: Text(
                  'Máy chủ đã nhận nhưng có bản ghi khác cùng thực thể. Cả hai được giữ lại và chờ cán bộ phụ trách xử lý.',
                  style: TextStyle(color: AgColors.amber, fontSize: 15),
                ),
              ),
            if (r.status == OutboxStatus.rejected)
              Padding(
                padding: const EdgeInsets.only(top: 6),
                child: Text(
                  'Bị từ chối: ${r.errorCode ?? ''}'
                  '${(r.errors ?? const []).isNotEmpty ? ' — ${r.errors!.map(_fmtErr).join('; ')}' : ''}',
                  style: const TextStyle(color: AgColors.red, fontSize: 15),
                ),
              ),
            if (canCorrect)
              Align(
                alignment: Alignment.centerRight,
                child: TextButton.icon(
                  onPressed: () => _correct(context, ref),
                  icon: const Icon(Icons.edit_note),
                  label: const Text(
                    'Tạo bản ghi bù',
                    style: TextStyle(fontSize: 16),
                  ),
                ),
              ),
          ],
        ),
      ),
    );
  }

  static String _fmtErr(Object? e) {
    if (e is Map) return '${e['field'] ?? ''}: ${e['reason'] ?? ''}';
    return '$e';
  }
}
