import 'dart:async';

import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/field_service.dart';
import '../../domain/fieldops.dart';
import '../providers.dart';
import '../widgets.dart';
import 'count_form_screen.dart';

class _Detail {
  const _Detail(this.board, this.shift);
  final OrderBoard board;
  final ShiftState shift;
}

final _detailProvider = FutureProvider.autoDispose.family<_Detail?, String>((
  ref,
  id,
) async {
  final s = servicesOf(ref).field;
  final o = await s.order(id);
  if (o == null) return null;
  return _Detail(await s.board(o), await s.shifts(id));
});

/// Chi tiết lệnh gom: tổng theo công đoạn, ca tổ gom, danh sách thửa với số
/// từng công đoạn (bale → gather → boat_load).
class OrderDetailScreen extends ConsumerStatefulWidget {
  const OrderDetailScreen({super.key, required this.orderId});
  final String orderId;

  @override
  ConsumerState<OrderDetailScreen> createState() => _OrderDetailScreenState();
}

class _OrderDetailScreenState extends ConsumerState<OrderDetailScreen> {
  @override
  void initState() {
    super.initState();
    unawaited(_refresh(quiet: true));
  }

  /// Tải số và ca mới nhất từ máy chủ; mất mạng thì dùng bản trên máy.
  Future<void> _refresh({bool quiet = false}) async {
    final f = ref.read(appControllerProvider).services?.field;
    if (f == null) return;
    try {
      await f.refreshCounts(widget.orderId);
      await f.refreshShifts(widget.orderId);
    } on NetworkException {
      if (!quiet && mounted) {
        showMessage(context, 'Mất kết nối — đang xem số trên máy.');
      }
    } on ApiException catch (e) {
      if (!quiet && mounted) {
        showMessage(context, rejectMessage(e.code), error: true);
      }
    }
    if (mounted) ref.invalidate(_detailProvider(widget.orderId));
  }

  @override
  Widget build(BuildContext context) {
    final d = ref.watch(_detailProvider(widget.orderId));
    return Scaffold(
      appBar: AppBar(
        title: const Text('Lệnh gom'),
        actions: [
          IconButton(
            tooltip: 'Làm mới số',
            icon: const Icon(Icons.refresh),
            onPressed: _refresh,
          ),
        ],
      ),
      body: d.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (d) => d == null
            ? const Center(
                child: Padding(
                  padding: EdgeInsets.all(24),
                  child: Text(
                    'Lệnh gom không còn trên máy (đã đóng, hủy hoặc hết phạm vi).',
                    style: TextStyle(fontSize: 17),
                  ),
                ),
              )
            : _body(context, d),
      ),
    );
  }

  Widget _body(BuildContext context, _Detail d) {
    final b = d.board;
    final o = b.order;
    return ListView(
      padding: const EdgeInsets.all(12),
      children: [
        Card(
          child: Padding(
            padding: const EdgeInsets.all(14),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  'Lệnh ${o.shortCode} · ${OrderStatus.label(o.status)}',
                  style: Theme.of(context).textTheme.titleMedium,
                ),
                const SizedBox(height: 4),
                Text('Khung gom: ${o.windowLabel} · xã ${o.communeCode}'),
                Text('HTX ${shortRef(o.htxRef)} · ${o.plotRefs.length} thửa'),
                const SizedBox(height: 8),
                for (final s in Stage.values)
                  Text(
                    '${s.label}: ${b.totalRolls(s)} cuộn '
                    '(${b.plotsDone(s)}/${o.plotRefs.length} thửa)',
                    key: ValueKey('total-${s.code}'),
                  ),
                if (b.fetchedAt != null)
                  Text(
                    'Số máy chủ lúc ${formatLocal(b.fetchedAt)}',
                    style: const TextStyle(color: AgColors.mute, fontSize: 14),
                  ),
              ],
            ),
          ),
        ),
        _ShiftCard(order: o, shift: d.shift, onChanged: _changed),
        const Padding(
          padding: EdgeInsets.fromLTRB(4, 12, 4, 4),
          child: Text(
            'Thửa',
            style: TextStyle(fontSize: 18, fontWeight: FontWeight.w700),
          ),
        ),
        for (final p in o.plotRefs)
          _PlotCard(board: b, plotRef: p, onChanged: _changed),
      ],
    );
  }

  void _changed() {
    ref.invalidate(_detailProvider(widget.orderId));
    unawaited(
      ref.services.sync.syncOnce(force: true).then((_) {
        if (mounted) ref.invalidate(_detailProvider(widget.orderId));
      }),
    );
  }
}

class _ShiftCard extends ConsumerWidget {
  const _ShiftCard({
    required this.order,
    required this.shift,
    required this.onChanged,
  });
  final CollectionOrder order;
  final ShiftState shift;
  final VoidCallback onChanged;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final last = shift.last;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              shift.open ? 'Ca đang mở' : 'Chưa mở ca',
              key: const ValueKey('shift-state'),
              style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w700),
            ),
            if (last != null)
              Text(
                '${last.kind == 'opened' ? 'Mở' : 'Đóng'} lúc ${formatLocal(last.eventTime)}'
                '${last.headcount != null ? ' · ${last.headcount} người' : ''}'
                '${last.pending ? ' · Chờ đồng bộ' : ''}',
              ),
            if (order.crewRef == null)
              const Text(
                'Lệnh chưa giao tổ gom.',
                style: TextStyle(color: AgColors.mute),
              ),
            const SizedBox(height: 8),
            Align(
              alignment: Alignment.centerRight,
              child: shift.open
                  ? OutlinedButton.icon(
                      onPressed: order.crewRef == null
                          ? null
                          : () async {
                              try {
                                await ref.services.field.closeShift(order);
                                onChanged();
                              } on FieldError catch (e) {
                                if (context.mounted) {
                                  showMessage(context, e.message, error: true);
                                }
                              }
                            },
                      icon: const Icon(Icons.stop_circle_outlined),
                      label: const Text('Đóng ca'),
                    )
                  : FilledButton.icon(
                      onPressed: order.crewRef == null
                          ? null
                          : () => _open(context, ref),
                      icon: const Icon(Icons.play_circle_outline),
                      label: const Text('Mở ca'),
                    ),
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _open(BuildContext context, WidgetRef ref) async {
    final n = await promptText(
      context,
      title: 'Mở ca tổ gom',
      label: 'Số người của tổ',
      ok: 'Mở ca',
      fieldKey: const ValueKey('headcount'),
      keyboardType: TextInputType.number,
    );
    if (n == null) return;
    try {
      await ref.services.field.openShift(order, n);
      onChanged();
      if (context.mounted) showMessage(context, 'Đã mở ca — Chờ đồng bộ.');
    } on FieldError catch (e) {
      if (context.mounted) showMessage(context, e.message, error: true);
    }
  }
}

class _PlotCard extends ConsumerWidget {
  const _PlotCard({
    required this.board,
    required this.plotRef,
    required this.onChanged,
  });
  final OrderBoard board;
  final String plotRef;
  final VoidCallback onChanged;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final o = board.order;
    return Card(
      key: ValueKey('plot-$plotRef'),
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              'Thửa ${shortRef(plotRef)}',
              style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w700),
            ),
            for (final s in Stage.values)
              _StageRow(
                board: board,
                plotRef: plotRef,
                stage: s,
                enabled: o.countable,
                onChanged: onChanged,
              ),
          ],
        ),
      ),
    );
  }
}

class _StageRow extends StatelessWidget {
  const _StageRow({
    required this.board,
    required this.plotRef,
    required this.stage,
    required this.enabled,
    required this.onChanged,
  });
  final OrderBoard board;
  final String plotRef;
  final Stage stage;
  final bool enabled;
  final VoidCallback onChanged;

  @override
  Widget build(BuildContext context) {
    final c = board.count(plotRef, stage);
    final rej = board.rejectedOf(plotRef, stage);
    final prev = stage.previous;
    final prevMissing = prev != null && board.count(plotRef, prev) == null;
    Future<void> open({required bool correction}) async {
      final saved = await Navigator.of(context).push<bool>(
        MaterialPageRoute(
          builder: (_) => CountFormScreen(
            order: board.order,
            plotRef: plotRef,
            stage: stage,
            correction: correction,
          ),
        ),
      );
      if (saved == true) onChanged();
    }

    return Padding(
      padding: const EdgeInsets.only(top: 6),
      child: Row(
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  '${stage.label}: ${c?.label ?? '—'}',
                  key: ValueKey('count-$plotRef-${stage.code}'),
                  style: const TextStyle(fontSize: 16),
                ),
                if (c?.pending ?? false)
                  const Text(
                    'Chờ đồng bộ',
                    style: TextStyle(color: AgColors.amber, fontSize: 14),
                  ),
                if (c?.discrepancyReason != null)
                  Text(
                    'Lý do chênh: ${c!.discrepancyReason}',
                    style: const TextStyle(color: AgColors.mute, fontSize: 14),
                  ),
                if (rej != null)
                  Text(
                    'Bị từ chối: ${rejectMessage(rej.errorCode)}',
                    style: const TextStyle(color: AgColors.red, fontSize: 14),
                  ),
              ],
            ),
          ),
          if (enabled && c == null)
            TextButton(
              key: ValueKey('record-$plotRef-${stage.code}'),
              onPressed: prevMissing ? null : () => open(correction: false),
              child: const Text('Ghi số'),
            ),
          if (enabled && c != null)
            TextButton(
              key: ValueKey('correct-$plotRef-${stage.code}'),
              onPressed: () => open(correction: true),
              child: const Text('Sửa số'),
            ),
        ],
      ),
    );
  }
}
