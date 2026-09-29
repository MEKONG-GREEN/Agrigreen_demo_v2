import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/manager_service.dart';
import '../../domain/cropcycle.dart';
import '../providers.dart';
import '../widgets.dart';

final _reviewProvider = FutureProvider.autoDispose<List<ReviewItem>>(
  (ref) => servicesOf(ref).manager.reviewQueue(),
);

/// Duyệt nhật ký (HTX US-LOG-05): nhãn "Vị trí không khớp"/"Thiếu GPS" hiển
/// thị rõ (AC-1); Phê duyệt → "Đã duyệt" (AC-2); Yêu cầu bổ sung → trả nông
/// dân kèm ghi chú (AC-3). Lệnh xếp hàng khi ngoại tuyến.
class ReviewScreen extends ConsumerWidget {
  const ReviewScreen({super.key, this.highlightLogId});

  /// Mở từ thông báo `farm_log_pending`: nhật ký này lên đầu, viền nổi bật.
  final String? highlightLogId;

  Future<void> _needsSupplement(
    BuildContext context,
    WidgetRef ref,
    FarmLogView log,
  ) async {
    final note = TextEditingController();
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Yêu cầu bổ sung'),
        content: TextField(
          key: const ValueKey('review-note'),
          controller: note,
          maxLines: 3,
          decoration: const InputDecoration(
            labelText: 'Cần bổ sung gì? (gửi cho nông dân)',
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Hủy'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Gửi yêu cầu'),
          ),
        ],
      ),
    );
    if (ok != true || !context.mounted) return;
    await _run(
      context,
      ref,
      () => ref.services.manager.review(log, approve: false, note: note.text),
      'Đã yêu cầu bổ sung — chờ gửi.',
    );
  }

  static Future<void> _run(
    BuildContext context,
    WidgetRef ref,
    Future<void> Function() f,
    String done,
  ) async {
    try {
      await f();
      if (context.mounted) showMessage(context, done);
      ref.invalidate(_reviewProvider);
      await ref.services.sync.syncOnce(force: true);
      ref.invalidate(_reviewProvider);
    } on ManagerException catch (e) {
      if (context.mounted) showMessage(context, e.message, error: true);
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final q = ref.watch(_reviewProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Duyệt nhật ký')),
      body: q.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (all) {
          final hl = highlightLogId;
          final items = [
            ...all.where((i) => i.log.id == hl),
            ...all.where((i) => i.log.id != hl),
          ];
          final missing = hl != null && items.every((i) => i.log.id != hl);
          if (items.isEmpty && !missing) {
            return const Center(
              child: Text(
                'Không có nhật ký chờ duyệt.',
                style: TextStyle(fontSize: 17),
              ),
            );
          }
          return ListView(
            padding: const EdgeInsets.all(8),
            children: [
              if (missing)
                const Card(
                  color: AgColors.amberBg,
                  child: ListTile(
                    leading: Icon(Icons.info_outline),
                    title: Text(
                      'Nhật ký trong thông báo đã được xử lý hoặc chưa đồng bộ về máy.',
                    ),
                  ),
                ),
              for (final it in items)
                _ReviewCard(
                  highlight: it.log.id == hl,
                  item: it,
                  onApprove: () => _run(
                    context,
                    ref,
                    () => ref.services.manager.review(it.log, approve: true),
                    'Đã phê duyệt — chờ gửi.',
                  ),
                  onSupplement: () => _needsSupplement(context, ref, it.log),
                ),
            ],
          );
        },
      ),
    );
  }
}

class _ReviewCard extends StatelessWidget {
  const _ReviewCard({
    required this.item,
    required this.onApprove,
    required this.onSupplement,
    this.highlight = false,
  });
  final ReviewItem item;
  final bool highlight;
  final VoidCallback onApprove;
  final VoidCallback onSupplement;

  @override
  Widget build(BuildContext context) {
    final l = item.log;
    final loc = locationLabel(l.locationStatus, l.gpsSource);
    final act = FarmActivity.parse(l.activityCode);
    return Card(
      key: highlight ? const ValueKey('review-highlight') : null,
      shape: highlight
          ? RoundedRectangleBorder(
              side: const BorderSide(color: AgColors.g700, width: 2),
              borderRadius: BorderRadius.circular(12),
            )
          : null,
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(
                  act == null ? Icons.note : activityIcon(act),
                  color: AgColors.g700,
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    activityLabel(l.activityCode),
                    style: const TextStyle(
                      fontSize: 17,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ),
                if (l.onBehalf) const Tag('Ghi hộ', color: AgColors.blue),
              ],
            ),
            Text(
              'Lúc ${l.eventTime}',
              style: const TextStyle(color: AgColors.mute),
            ),
            if (l.note.isNotEmpty) Text(l.note),
            if (l.fileRefs.isNotEmpty)
              Text('${l.fileRefs.length} ảnh minh chứng'),
            if (loc != null)
              Padding(
                padding: const EdgeInsets.only(top: 6),
                child: Tag(loc, color: AgColors.red),
              ),
            // HTX US-GPS-02 AC-2: luôn ghi rõ nguồn xác thực vị trí.
            if (l.gpsSource.isNotEmpty)
              Text(
                gpsSourceLabel(l.gpsSource),
                style: const TextStyle(color: AgColors.mute, fontSize: 14),
              ),
            const SizedBox(height: 8),
            if (item.failed != null)
              Text(
                rejectMessage(item.failed),
                style: const TextStyle(color: AgColors.red),
              )
            else if (item.decision != null)
              Tag(
                item.decision == 'approved'
                    ? 'Đã phê duyệt — chờ đồng bộ'
                    : 'Đã yêu cầu bổ sung — chờ đồng bộ',
                color: AgColors.g700,
              )
            else
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  FilledButton.icon(
                    key: ValueKey('approve-${l.id}'),
                    onPressed: onApprove,
                    icon: const Icon(Icons.check),
                    label: const Text('Phê duyệt'),
                  ),
                  OutlinedButton.icon(
                    key: ValueKey('supplement-${l.id}'),
                    onPressed: onSupplement,
                    icon: const Icon(Icons.undo),
                    label: const Text('Yêu cầu bổ sung'),
                  ),
                ],
              ),
          ],
        ),
      ),
    );
  }
}
