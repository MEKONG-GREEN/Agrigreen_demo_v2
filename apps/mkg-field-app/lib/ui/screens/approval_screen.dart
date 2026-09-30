import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/field_service.dart';
import '../../domain/fieldops.dart';
import '../providers.dart';
import '../widgets.dart';

class _Planned {
  const _Planned(this.list, this.me);
  final Cached<List<CollectionOrder>> list;
  final String? me;
}

final _plannedProvider = FutureProvider.autoDispose<_Planned>((ref) async {
  final f = servicesOf(ref).field;
  return _Planned(await f.plannedOrders(), await f.me());
});

/// Giám sát gom: duyệt / từ chối lệnh gom chờ duyệt. Người lập không được tự
/// duyệt (R-56) — nút bị khóa, máy chủ chặn lần hai.
class ApprovalScreen extends ConsumerWidget {
  const ApprovalScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final q = ref.watch(_plannedProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Duyệt lệnh gom')),
      body: q.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(
          child: Padding(
            padding: const EdgeInsets.all(24),
            child: Text('$e', style: const TextStyle(fontSize: 17)),
          ),
        ),
        data: (d) => ListView(
          padding: const EdgeInsets.all(12),
          children: [
            if (d.list.stale)
              Text(
                'Mất kết nối — danh sách lúc ${formatLocal(d.list.fetchedAt)}.',
                style: const TextStyle(color: AgColors.amber),
              ),
            if (d.list.value.isEmpty)
              const Padding(
                padding: EdgeInsets.all(24),
                child: Text(
                  'Không có lệnh gom chờ duyệt.',
                  style: TextStyle(fontSize: 17),
                ),
              ),
            for (final o in d.list.value) _PlannedCard(order: o, me: d.me),
          ],
        ),
      ),
    );
  }
}

class _PlannedCard extends ConsumerWidget {
  const _PlannedCard({required this.order, required this.me});
  final CollectionOrder order;
  final String? me;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final own = me != null && order.createdBy == me;
    Future<void> decide(bool approve) async {
      String? reason;
      if (!approve) {
        reason = await _askReason(context);
        if (reason == null) return;
      }
      try {
        await ref.services.field.decide(
          order,
          approve: approve,
          reason: reason,
        );
        if (context.mounted) {
          showMessage(context, approve ? 'Đã duyệt lệnh gom.' : 'Đã từ chối.');
        }
        ref.invalidate(_plannedProvider);
        await ref.services.sync.syncOnce(force: true);
      } on FieldError catch (e) {
        if (context.mounted) showMessage(context, e.message, error: true);
      }
    }

    return Card(
      key: ValueKey('planned-${order.orderId}'),
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              'Lệnh ${order.shortCode} · bản ${order.revision}',
              style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w700),
            ),
            Text('${order.plotRefs.length} thửa · ${order.windowLabel}'),
            if (own)
              const Text(
                'Bạn lập lệnh này — cần giám sát khác duyệt (R-56).',
                key: ValueKey('own-order'),
                style: TextStyle(color: AgColors.amber),
              ),
            const SizedBox(height: 8),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              alignment: WrapAlignment.end,
              children: [
                OutlinedButton(
                  onPressed: own ? null : () => decide(false),
                  child: const Text('Từ chối'),
                ),
                FilledButton(
                  key: ValueKey('approve-${order.orderId}'),
                  onPressed: own ? null : () => decide(true),
                  child: const Text('Duyệt'),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

Future<String?> _askReason(BuildContext context) => promptText(
  context,
  title: 'Lý do từ chối',
  label: 'Ít nhất 10 ký tự',
  ok: 'Từ chối',
  maxLines: 3,
);
