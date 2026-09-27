import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/field_report_service.dart';
import '../../data/photo/location_source.dart';
import '../../data/reference/reference_repository.dart';
import '../../domain/outbox_record.dart';
import '../providers.dart';
import '../theme.dart';
import '../widgets/common.dart';
import 'photo_capture_screen.dart';
import 'plots_screen.dart';

const _activityIcons = {
  FarmActivity.lamDat: Icons.agriculture,
  FarmActivity.gieoSa: Icons.grass,
  FarmActivity.tuoiNuoc: Icons.water_drop,
  FarmActivity.bonPhan: Icons.scatter_plot,
  FarmActivity.phunThuoc: Icons.sanitizer,
  FarmActivity.kiemTra: Icons.fact_check,
  FarmActivity.thuHoach: Icons.inventory_2,
};

/// Báo cáo hiện trường / nhật ký canh tác. GPS + thời gian gắn tự động khi lưu
/// và không sửa được về sau (HTX US-LOG-01).
class FieldReportScreen extends ConsumerStatefulWidget {
  const FieldReportScreen({super.key});

  @override
  ConsumerState<FieldReportScreen> createState() => _FieldReportScreenState();
}

class _FieldReportScreenState extends ConsumerState<FieldReportScreen> {
  RefRow? _plot;
  FarmActivity? _activity;
  final _note = TextEditingController();
  bool _busy = false;

  @override
  void dispose() {
    _note.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    final s = ref.read(appControllerProvider).services;
    if (s == null || _plot == null || _activity == null) return;
    setState(() => _busy = true);
    try {
      final loc = await s.platform.location.current();
      final rec = await s.reports.create(
        plotId: _plot!.id,
        householdRef: _plot!.data['household_ref'] as String?,
        activity: _activity!,
        note: _note.text,
        location: loc,
      );
      if (!mounted) return;
      showMessage(
        context,
        loc.fix == null
            ? 'Đã lưu (${loc.status.label}). Chờ đồng bộ.'
            : 'Đã lưu kèm GPS và thời gian. Chờ đồng bộ.',
      );
      final household = _plot!.data['household_ref'] as String?;
      if (household != null) {
        await Navigator.of(context).pushReplacement(
          MaterialPageRoute<void>(
            builder: (_) => PhotoCaptureScreen(
              subjectRef: household,
              parentEntity: PushEntity.fieldReport,
              parentId: rec.id,
            ),
          ),
        );
      } else {
        Navigator.of(context).pop();
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final plots = ref.watch(plotsProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Báo cáo hiện trường')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          const Text(
            'Thửa ruộng',
            style: TextStyle(fontSize: 17, fontWeight: FontWeight.w600),
          ),
          const SizedBox(height: 8),
          plots.when(
            loading: () => const LinearProgressIndicator(),
            error: (e, _) => Text('Lỗi: $e'),
            data: (list) => DropdownButtonFormField<String>(
              initialValue: _plot?.id,
              isExpanded: true,
              itemHeight: 56,
              hint: const Text('Chọn thửa'),
              items: [
                for (final p in list)
                  DropdownMenuItem(value: p.id, child: Text(plotLabel(p))),
              ],
              onChanged: (id) =>
                  setState(() => _plot = list.firstWhere((p) => p.id == id)),
            ),
          ),
          const SizedBox(height: 20),
          const Text(
            'Hoạt động',
            style: TextStyle(fontSize: 17, fontWeight: FontWeight.w600),
          ),
          const SizedBox(height: 8),
          Wrap(
            spacing: 10,
            runSpacing: 10,
            children: [
              for (final a in FarmActivity.values)
                ChoiceChip(
                  avatar: Icon(_activityIcons[a], size: 24),
                  label: Text(a.label, style: const TextStyle(fontSize: 17)),
                  padding: const EdgeInsets.symmetric(
                    horizontal: 10,
                    vertical: 10,
                  ),
                  selected: _activity == a,
                  selectedColor: AgColors.g100,
                  onSelected: (_) => setState(() => _activity = a),
                ),
            ],
          ),
          const SizedBox(height: 20),
          TextField(
            controller: _note,
            maxLines: 3,
            style: const TextStyle(fontSize: 17),
            decoration: const InputDecoration(
              labelText: 'Ghi chú (không bắt buộc)',
            ),
          ),
          const SizedBox(height: 12),
          const Text(
            'Tọa độ GPS và thời gian được gắn tự động khi lưu và không thể chỉnh sửa sau đó.',
            style: TextStyle(color: AgColors.mute, fontSize: 15),
          ),
          const SizedBox(height: 20),
          FilledButton.icon(
            onPressed: _busy || _plot == null || _activity == null
                ? null
                : _save,
            icon: const Icon(Icons.save),
            label: Text(_busy ? 'Đang lấy GPS…' : 'Lưu báo cáo'),
          ),
          const SizedBox(height: 28),
          const _RecentReports(),
        ],
      ),
    );
  }
}

final recentReportsProvider = FutureProvider.autoDispose<List<OutboxRecord>>(
  (ref) => servicesOf(ref).reports.recent(),
);

class _RecentReports extends ConsumerWidget {
  const _RecentReports();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final list = ref.watch(recentReportsProvider);
    return list.when(
      loading: () => const SizedBox.shrink(),
      error: (e, _) => Text('Lỗi: $e'),
      data: (items) => Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          if (items.isNotEmpty)
            const Text(
              'Báo cáo gần đây trên máy',
              style: TextStyle(fontSize: 17, fontWeight: FontWeight.w600),
            ),
          for (final r in items)
            Card(
              child: ListTile(
                title: Text(
                  FarmActivity.byCode(r.payload['activity'] as String?)
                          ?.label ??
                      'Báo cáo',
                ),
                subtitle: Text(
                  '${r.eventTime}\n${GpsStatus.values.where((g) => g.name == r.payload['gps_status']).map((g) => g.label).firstOrNull ?? ''}'
                  '${r.op == RecordOp.correct ? ' · Bản ghi bù' : ''}',
                ),
                isThreeLine: true,
                trailing: StatusChip(r.status),
              ),
            ),
        ],
      ),
    );
  }
}
