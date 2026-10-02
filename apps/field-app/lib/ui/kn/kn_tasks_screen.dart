import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/kn_report_service.dart';
import '../../domain/kn_report.dart';
import '../providers.dart';
import 'kn_providers.dart';
import 'kn_report_entry_screen.dart';

/// "Công việc cần làm" (FN-XA-03.01) đứng riêng (mở từ menu Báo cáo tuần /
/// tháng / hàng vụ / đột xuất với bộ lọc sẵn).
class KnTasksScreen extends StatelessWidget {
  const KnTasksScreen({super.key, this.title, this.initial});
  final String? title;
  final KnTaskFilter? initial;

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: Text(title ?? 'Công việc cần làm')),
    body: KnTasksView(initial: initial ?? const KnTaskFilter()),
  );
}

/// Danh sách công việc của mọi xã phụ trách, từ entity `report_task` đã pull
/// (xem được ngoại tuyến): lọc xã / loại kỳ / trạng thái / biểu, nhãn đếm Cần
/// làm / Trễ hạn / Yêu cầu điều chỉnh, sắp theo hạn gần nhất.
class KnTasksView extends ConsumerStatefulWidget {
  const KnTasksView({super.key, this.initial = const KnTaskFilter()});
  final KnTaskFilter initial;

  @override
  ConsumerState<KnTasksView> createState() => _KnTasksViewState();
}

class _KnTasksViewState extends ConsumerState<KnTasksView> {
  late KnTaskFilter _f = widget.initial;
  KnTaskList? _list;
  Object? _error;

  KnReportService get _svc => ref.read(knReportServiceProvider);

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final l = await _svc.list(_f);
      if (mounted) setState(() => (_list = l, _error = null));
    } on Object catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  Future<void> _open(KnTaskRow r) async {
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => KnReportEntryScreen(taskId: r.task.id),
      ),
    );
    await _load();
  }

  Future<void> _refresh() async {
    await ref
        .read(appControllerProvider)
        .services
        ?.sync
        .syncOnce(
          force: true,
          entities: KnReportService.focusedEntities.union({
            KnEntity.formLayout,
          }),
        );
    await _load();
  }

  Widget _dropdown<T>({
    required String label,
    required Key key,
    required T? value,
    required Map<T, String> items,
    required void Function(T?) onChanged,
  }) => SizedBox(
    width: 160,
    child: DropdownButtonFormField<T?>(
      isExpanded: true,
      key: key,
      initialValue: value,
      decoration: InputDecoration(labelText: label, isDense: true),
      items: [
        DropdownMenuItem<T?>(value: null, child: const Text('Tất cả')),
        for (final e in items.entries)
          DropdownMenuItem<T?>(
            value: e.key,
            child: Text(e.value, overflow: TextOverflow.ellipsis),
          ),
      ],
      onChanged: (v) {
        onChanged(v);
        _load();
      },
    ),
  );

  @override
  Widget build(BuildContext context) {
    ref.listen(syncStatusStreamProvider, (_, _) => _load());
    final l = _list;
    if (_error != null && l == null) {
      return Center(child: Text('Lỗi: $_error'));
    }
    if (l == null) return const Center(child: CircularProgressIndicator());
    final rows = l.rows;
    return RefreshIndicator(
      onRefresh: _refresh,
      child: CustomScrollView(
        slivers: [
          SliverToBoxAdapter(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(12, 12, 12, 4),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Wrap(
                    spacing: 8,
                    runSpacing: 8,
                    children: [
                      _counter('Cần làm', l.todo, AgColors.blue, null),
                      _counter(
                        'Trễ hạn',
                        l.overdue,
                        AgColors.red,
                        KnTaskStatus.overdue,
                      ),
                      _counter(
                        'Yêu cầu điều chỉnh',
                        l.adjustment,
                        AgColors.amber,
                        KnTaskStatus.adjustmentRequested,
                      ),
                    ],
                  ),
                  const SizedBox(height: 8),
                  Wrap(
                    spacing: 8,
                    runSpacing: 8,
                    children: [
                      _dropdown<String>(
                        label: 'Xã/phường',
                        key: const ValueKey('kn-f-commune'),
                        value: _f.commune,
                        items: l.communes,
                        onChanged: (v) => _f = _f.copyWith(commune: () => v),
                      ),
                      _dropdown<String>(
                        label: 'Loại kỳ',
                        key: const ValueKey('kn-f-period'),
                        value: _f.periodType,
                        items: {
                          for (final p in [
                            KnPeriodType.week,
                            KnPeriodType.month,
                            KnPeriodType.season,
                          ])
                            p: KnPeriodType.label(p),
                        },
                        onChanged: (v) => _f = _f.copyWith(periodType: () => v),
                      ),
                      _dropdown<String>(
                        label: 'Trạng thái',
                        key: const ValueKey('kn-f-status'),
                        value: _f.status,
                        items: {
                          for (final s in KnTaskStatus.all)
                            s: KnTaskStatus.label(s),
                        },
                        onChanged: (v) => _f = _f.copyWith(status: () => v),
                      ),
                      _dropdown<String>(
                        label: 'Biểu',
                        key: const ValueKey('kn-f-form'),
                        value: _f.formCode,
                        items: {
                          for (final e in l.forms.entries)
                            e.key: '${e.key} · ${e.value}',
                        },
                        onChanged: (v) => _f = _f.copyWith(formCode: () => v),
                      ),
                    ],
                  ),
                ],
              ),
            ),
          ),
          if (rows.isEmpty)
            const SliverFillRemaining(
              hasScrollBody: false,
              child: Center(
                child: Padding(
                  padding: EdgeInsets.all(24),
                  child: Text(
                    'Chưa có công việc',
                    style: TextStyle(fontSize: 18, color: AgColors.mute),
                  ),
                ),
              ),
            )
          else
            SliverList.builder(
              itemCount: rows.length,
              itemBuilder: (_, i) => _tile(i + 1, rows[i]),
            ),
          const SliverToBoxAdapter(child: SizedBox(height: 24)),
        ],
      ),
    );
  }

  Widget _counter(String label, int n, Color color, String? status) =>
      ActionChip(
        key: ValueKey('kn-count-$label'),
        avatar: CircleAvatar(
          backgroundColor: color,
          child: Text(
            '$n',
            style: const TextStyle(color: Colors.white, fontSize: 12),
          ),
        ),
        label: Text(label),
        onPressed: () {
          setState(() => _f = _f.copyWith(status: () => status));
          _load();
        },
      );

  Widget _tile(int no, KnTaskRow r) {
    final t = r.task;
    return Card(
      margin: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Text('$no. ', style: const TextStyle(color: AgColors.mute)),
                Expanded(
                  child: Text(
                    '${t.formCode} · ${t.formName}',
                    style: const TextStyle(
                      fontSize: 16,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ),
                KnChip(KnTaskStatus.label(r.display), knStatusColor(r.display)),
              ],
            ),
            const SizedBox(height: 4),
            Text(
              '${t.communeName} · ${t.periodLabel.isEmpty ? KnPeriodType.label(t.periodType) : t.periodLabel}'
              '${t.seasonLabel.isEmpty ? '' : ' · ${t.seasonLabel}'}',
            ),
            Text(
              'Hạn nộp: ${knDate(t.dueAt)} · Cập nhật: ${knDate(t.updatedAt)}',
              style: const TextStyle(color: AgColors.mute, fontSize: 13),
            ),
            if (r.display == KnTaskStatus.adjustmentRequested &&
                (t.adjustmentReason ?? '').isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 4),
                child: Text(
                  'Lý do điều chỉnh: ${t.adjustmentReason}',
                  style: const TextStyle(color: AgColors.amber),
                ),
              ),
            if (r.display == KnTaskStatus.overdue)
              const Text(
                'Trễ hạn — vẫn nộp được đến khi chốt kỳ.',
                style: TextStyle(color: AgColors.red, fontSize: 13),
              ),
            if (r.sync.state != KnSyncState.none)
              Padding(
                padding: const EdgeInsets.only(top: 4),
                child: Text(
                  r.sync.label,
                  style: TextStyle(
                    color: knSyncColor(r.sync.state),
                    fontSize: 13,
                  ),
                ),
              ),
            Align(
              alignment: Alignment.centerRight,
              child: FilledButton(
                key: ValueKey('kn-task-action-${t.id}'),
                onPressed: () => _open(r),
                child: Text(r.action),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
