import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/crop_service.dart';
import '../../application/manager_service.dart';
import '../../domain/cropcycle.dart';
import '../providers.dart';
import 'open_season_screen.dart' show parseDisplayDay;

class _BulkData {
  const _BulkData(this.plots, this.seasons, this.varieties);
  final List<PlotItem> plots;
  final List<Choice> seasons;
  final List<Choice> varieties;
}

final _bulkProvider = FutureProvider.autoDispose<_BulkData>((ref) async {
  final s = servicesOf(ref);
  final plots = <PlotItem>[];
  for (final p in await s.crop.plots()) {
    if (await s.crop.openSeasonFor(p.ref) == null) plots.add(p);
  }
  return _BulkData(
    plots,
    await s.crop.seasonsCatalog(),
    await s.crop.varieties(),
  );
});

/// Mở vụ hộ hàng loạt (HTX US-SEASON-02): chọn nhiều lô → một lần xác nhận;
/// lô không hợp lệ bị bỏ qua kèm lý do.
class BulkOpenScreen extends ConsumerStatefulWidget {
  const BulkOpenScreen({super.key});

  @override
  ConsumerState<BulkOpenScreen> createState() => _BulkOpenScreenState();
}

class _BulkOpenScreenState extends ConsumerState<BulkOpenScreen> {
  final _selected = <String>{};
  String? _season;
  String? _variety;
  late int _year;
  final _date = TextEditingController();
  String? _error;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    final today = ref.read(appControllerProvider).services!.crop.today();
    _year = today.year;
    _date.text = displayDay(today);
  }

  @override
  void dispose() {
    _date.dispose();
    super.dispose();
  }

  Future<void> _submit(_BulkData d) async {
    final date = parseDisplayDay(_date.text);
    if (_season == null || _variety == null || date == null) {
      setState(() => _error = 'Chọn vụ, giống và nhập ngày gieo sạ.');
      return;
    }
    setState(() => _busy = true);
    try {
      final r = await ref.services.manager.bulkOpen(
        plots: [
          for (final p in d.plots)
            if (_selected.contains(p.ref)) p,
        ],
        seasonCode: _season!,
        seasonYear: _year,
        varietyCode: _variety!,
        sowingDate: date,
      );
      if (!mounted) return;
      await showDialog<void>(
        context: context,
        builder: (ctx) => AlertDialog(
          title: Text('Đã mở vụ cho ${r.opened} lô'),
          content: Text(
            [
              if (r.offline) 'Đang mất mạng — đã lưu, chờ đồng bộ.',
              if (r.skipped.isNotEmpty) 'Bỏ qua:',
              for (final e in r.skipped.entries) '• Lô ${e.key}: ${e.value}',
            ].join('\n'),
          ),
          actions: [
            FilledButton(
              onPressed: () => Navigator.pop(ctx),
              child: const Text('Đóng'),
            ),
          ],
        ),
      );
      if (mounted) Navigator.of(context).pop(true);
    } on ManagerException catch (e) {
      setState(() => _error = e.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final data = ref.watch(_bulkProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Mở vụ hàng loạt')),
      body: data.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (d) => ListView(
          padding: const EdgeInsets.all(12),
          children: [
            Text(
              'Chọn lô ruộng (${_selected.length})',
              style: Theme.of(context).textTheme.titleMedium,
            ),
            if (d.plots.isEmpty) const Text('Không có lô nào chưa mở vụ.'),
            for (final p in d.plots)
              CheckboxListTile(
                value: _selected.contains(p.ref),
                title: Text(p.title),
                subtitle: p.areaHa == null
                    ? null
                    : Text('${p.areaHa!.replaceAll('.', ',')} ha'),
                onChanged: (v) => setState(
                  () => v == true
                      ? _selected.add(p.ref)
                      : _selected.remove(p.ref),
                ),
              ),
            const SizedBox(height: 8),
            Text('Vụ', style: Theme.of(context).textTheme.titleMedium),
            Wrap(
              spacing: 8,
              children: [
                for (final s in d.seasons)
                  ChoiceChip(
                    label: Text(s.name),
                    selected: _season == s.code,
                    onSelected: (_) => setState(() => _season = s.code),
                  ),
              ],
            ),
            Row(
              children: [
                const Text('Năm: ', style: TextStyle(fontSize: 17)),
                IconButton(
                  tooltip: 'Năm trước',
                  icon: const Icon(Icons.remove_circle_outline),
                  onPressed: () => setState(() => _year--),
                ),
                Text('$_year', style: const TextStyle(fontSize: 20)),
                IconButton(
                  tooltip: 'Năm sau',
                  icon: const Icon(Icons.add_circle_outline),
                  onPressed: () => setState(() => _year++),
                ),
              ],
            ),
            Text('Giống lúa', style: Theme.of(context).textTheme.titleMedium),
            Wrap(
              spacing: 8,
              children: [
                for (final v in d.varieties)
                  ChoiceChip(
                    label: Text(v.name),
                    selected: _variety == v.code,
                    onSelected: (_) => setState(() => _variety = v.code),
                  ),
              ],
            ),
            const SizedBox(height: 12),
            TextField(
              key: const ValueKey('bulk-date'),
              controller: _date,
              decoration: const InputDecoration(
                labelText: 'Ngày gieo sạ (ngày/tháng/năm)',
              ),
            ),
            if (_error != null)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Text(
                  _error!,
                  style: const TextStyle(color: AgColors.red, fontSize: 16),
                ),
              ),
            const SizedBox(height: 16),
            FilledButton.icon(
              onPressed: _busy ? null : () => _submit(d),
              icon: const Icon(Icons.playlist_add_check),
              label: const Text('Mở vụ cho các lô đã chọn'),
            ),
            const SizedBox(height: 8),
            const Text(
              'Nông hộ hoặc lô ruộng ngoài phạm vi HTX của bạn sẽ bị từ chối.',
              style: TextStyle(color: AgColors.mute),
            ),
          ],
        ),
      ),
    );
  }
}
