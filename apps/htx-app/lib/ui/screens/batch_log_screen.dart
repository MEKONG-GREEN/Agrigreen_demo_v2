import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/crop_service.dart';
import '../../domain/cropcycle.dart';
import '../providers.dart';
import '../widgets.dart';
import 'farm_log_screen.dart' show ActivityTile;
import 'plots_screen.dart' show PlotRow;

class _BatchData {
  const _BatchData(this.rows, this.machines, this.history);
  final List<PlotRow> rows;
  final List<Choice> machines;
  final List<BatchSummary> history;
}

final _batchDataProvider = FutureProvider.autoDispose<_BatchData>((ref) async {
  final s = servicesOf(ref);
  return _BatchData(
    [
      for (final p in await s.crop.plots())
        PlotRow(p, await s.crop.openSeasonFor(p.ref)),
    ],
    await s.crop.machines(),
    await s.crop.batches(),
  );
});

/// Ghi nhật ký hàng loạt (HTX US-LOG-03): chọn nhiều lô cùng một hoạt động
/// trong ngày → MỘT bản ghi `farm_log_batch` (một đầu lô, máy chủ tạo một dòng
/// mỗi lô — AC-1). Lô chưa mở vụ bị loại và liệt kê kèm lý do trước khi lưu;
/// lô máy chủ loại thêm (ngoài phạm vi, vụ đã đóng) hiện ở "Lượt đã ghi" sau
/// khi đồng bộ (AC-2). Làm được khi ngoại tuyến.
class BatchLogScreen extends ConsumerStatefulWidget {
  const BatchLogScreen({super.key});

  @override
  ConsumerState<BatchLogScreen> createState() => _BatchLogScreenState();
}

class _BatchLogScreenState extends ConsumerState<BatchLogScreen> {
  final _selected = <String>{};
  FarmActivity? _activity;
  final _machines = <String>{};
  final _note = TextEditingController();
  String? _error;
  bool _busy = false;

  @override
  void dispose() {
    _note.dispose();
    super.dispose();
  }

  Future<void> _save(_BatchData d) async {
    if (_activity == null) {
      setState(() => _error = 'Chọn hoạt động canh tác.');
      return;
    }
    final crop = ref.services.crop;
    final plots = [
      for (final r in d.rows)
        if (_selected.contains(r.plot.ref)) r.plot,
    ];
    setState(() => _busy = true);
    try {
      if (plots.isEmpty) {
        throw CropError('Chọn ít nhất 1 lô ruộng để ghi nhật ký hàng loạt.');
      }
      final plan = await crop.planBatch(plots);
      if (!mounted) return;
      final ok = await confirmDialog(
        context,
        title: 'Ghi nhật ký cho ${plan.included.length} lô',
        message: [
          '${_activity!.label} — lưu chung một bản ghi.',
          if (plan.skipped.isNotEmpty) 'Bỏ qua:',
          for (final e in plan.skipped.entries) '• Lô ${e.key}: ${e.value}',
        ].join('\n'),
        ok: 'Ghi hàng loạt',
      );
      if (!ok) return;
      final (_, done) = await crop.recordBatch(
        plots: plots,
        activity: _activity!,
        machineCodes: _machines.toList(),
        note: _note.text,
      );
      if (!mounted) return;
      showMessage(
        context,
        'Đã lưu nhật ký hàng loạt cho ${done.included.length} lô — Chờ đồng bộ.',
      );
      setState(() {
        _selected.clear();
        _activity = null;
        _machines.clear();
        _note.clear();
        _error = null;
      });
      ref.invalidate(_batchDataProvider);
    } on CropError catch (e) {
      setState(() => _error = e.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final data = ref.watch(_batchDataProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Nhật ký hàng loạt')),
      body: data.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (d) => ListView(
          padding: const EdgeInsets.all(12),
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    'Chọn lô ruộng (${_selected.length})',
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                ),
                TextButton(
                  onPressed: () => setState(
                    () => _selected.addAll([
                      for (final r in d.rows)
                        if (r.season != null) r.plot.ref,
                    ]),
                  ),
                  child: const Text('Chọn lô đang có vụ'),
                ),
              ],
            ),
            if (d.rows.isEmpty) const Text('Chưa có lô ruộng trên máy.'),
            for (final r in d.rows)
              CheckboxListTile(
                key: ValueKey('batch-plot-${r.plot.code}'),
                value: _selected.contains(r.plot.ref),
                title: Text(r.plot.title),
                subtitle: Text(
                  r.season == null
                      ? skipNotOpen
                      : 'Vụ ${r.season!.seasonCode} ${r.season!.seasonYear} · ${r.season!.varietyCode}',
                  style: TextStyle(
                    color: r.season == null ? AgColors.amber : null,
                  ),
                ),
                onChanged: (v) => setState(
                  () => v == true
                      ? _selected.add(r.plot.ref)
                      : _selected.remove(r.plot.ref),
                ),
              ),
            const SizedBox(height: 8),
            Text('Hoạt động', style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 8),
            GridView.count(
              crossAxisCount: 3,
              shrinkWrap: true,
              physics: const NeverScrollableScrollPhysics(),
              mainAxisSpacing: 8,
              crossAxisSpacing: 8,
              childAspectRatio: 1.05,
              children: [
                for (final a in FarmActivity.values)
                  ActivityTile(
                    activity: a,
                    selected: _activity == a,
                    onTap: () => setState(() => _activity = a),
                  ),
              ],
            ),
            if (d.machines.isNotEmpty) ...[
              const SizedBox(height: 12),
              Text(
                'Máy móc đã dùng (không bắt buộc)',
                style: Theme.of(context).textTheme.titleMedium,
              ),
              Wrap(
                spacing: 8,
                runSpacing: 4,
                children: [
                  for (final m in d.machines)
                    FilterChip(
                      label: Text(m.name),
                      selected: _machines.contains(m.code),
                      onSelected: (on) => setState(
                        () => on
                            ? _machines.add(m.code)
                            : _machines.remove(m.code),
                      ),
                    ),
                ],
              ),
            ],
            const SizedBox(height: 12),
            TextField(
              key: const ValueKey('batch-note'),
              controller: _note,
              maxLines: 2,
              decoration: const InputDecoration(
                labelText: 'Ghi chú (không bắt buộc)',
              ),
            ),
            const SizedBox(height: 8),
            const ListTile(
              contentPadding: EdgeInsets.zero,
              leading: Icon(Icons.list_alt, color: AgColors.g700),
              title: Text('Vị trí: chọn lô từ danh sách'),
              subtitle: Text(
                'Thời gian ghi tự đính kèm khi lưu, không sửa được.',
              ),
            ),
            if (_error != null)
              Padding(
                padding: const EdgeInsets.only(bottom: 8),
                child: Text(
                  _error!,
                  key: const ValueKey('batch-error'),
                  style: const TextStyle(color: AgColors.red, fontSize: 16),
                ),
              ),
            FilledButton.icon(
              onPressed: _busy ? null : () => _save(d),
              icon: const Icon(Icons.library_add_check),
              label: const Text('Ghi cho các lô đã chọn'),
            ),
            if (d.history.isNotEmpty) ...[
              const SizedBox(height: 20),
              Text(
                'Lượt đã ghi',
                style: Theme.of(context).textTheme.titleMedium,
              ),
              for (final b in d.history) _BatchCard(summary: b),
            ],
          ],
        ),
      ),
    );
  }
}

class _BatchCard extends StatelessWidget {
  const _BatchCard({required this.summary});
  final BatchSummary summary;

  @override
  Widget build(BuildContext context) {
    final b = summary;
    final r = b.record;
    final sent = r.status == OutboxStatus.sent;
    final result = r.status == OutboxStatus.rejected
        ? rejectMessage(r.errorCode)
        : !sent
        ? r.status.label
        : b.accepted == 0
        ? 'Đã gửi — chờ máy chủ gửi kết quả về'
        : 'Đã ghi ${b.accepted}/${b.plotCodes.length} lô';
    return Card(
      child: ListTile(
        title: Text(
          '${activityLabel(b.activityCode)} · ${b.plotCodes.length} lô',
        ),
        subtitle: Text(
          [
            'Lô: ${b.plotCodes.join(', ')}',
            result,
            if (b.serverSkipped.isNotEmpty)
              'Máy chủ bỏ qua: ${b.serverSkipped.join(', ')} '
                  '($skipNotOpen hoặc $skipOutOfScope)',
            formatLocal(r.recordedAt),
          ].join('\n'),
        ),
      ),
    );
  }
}
