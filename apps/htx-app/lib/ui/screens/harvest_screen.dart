import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/crop_service.dart';
import '../../domain/cropcycle.dart';
import '../providers.dart';
import '../widgets.dart';
import 'open_season_screen.dart' show parseDisplayDay;

class _HarvestRow {
  const _HarvestRow(this.season, this.block, this.queued);
  final Season season;
  final String? block;
  final bool queued;
}

final _harvestRowsProvider = FutureProvider.autoDispose<List<_HarvestRow>>((
  ref,
) async {
  final s = servicesOf(ref);
  return [
    for (final se in await s.crop.seasons())
      if (se.isOpen)
        _HarvestRow(
          se,
          await s.crop.harvestBlock(se),
          await s.crop.hasLocalHarvest(se.seasonRef),
        ),
  ];
});

/// Khai sản lượng (HTX US-YIELD-01): nhập **kg**, hiện t/ha; chặn trước cổng
/// thu hoạch của SOP đã ghim; bất thường → hỏi xác nhận (US-YIELD-03); mất
/// mạng → "Chờ đồng bộ".
class HarvestScreen extends ConsumerStatefulWidget {
  const HarvestScreen({super.key, this.seasonRef});
  final String? seasonRef;

  @override
  ConsumerState<HarvestScreen> createState() => _HarvestScreenState();
}

class _HarvestScreenState extends ConsumerState<HarvestScreen> {
  String? _season;
  final _kg = TextEditingController();
  final _date = TextEditingController();
  String? _error;
  bool _saving = false;

  @override
  void initState() {
    super.initState();
    _season = widget.seasonRef;
    _date.text = displayDay(
      ref.read(appControllerProvider).services!.crop.today(),
    );
    _kg.addListener(() => setState(() {}));
  }

  @override
  void dispose() {
    _kg.dispose();
    _date.dispose();
    super.dispose();
  }

  Future<void> _save(Season season) async {
    final date = parseDisplayDay(_date.text);
    if (date == null) {
      setState(() => _error = 'Ngày thu hoạch theo dạng ngày/tháng/năm.');
      return;
    }
    setState(() => _saving = true);
    final crop = ref.services.crop;
    try {
      try {
        await crop.declareHarvest(
          season: season,
          massKgInput: _kg.text,
          harvestDate: date,
        );
      } on YieldNeedsConfirm catch (e) {
        if (!mounted) return;
        final ok = await confirmDialog(
          context,
          title: 'Sản lượng bất thường',
          message: e.message,
          ok: 'Vẫn lưu',
          cancel: 'Sửa lại',
        );
        if (!ok) return;
        await crop.declareHarvest(
          season: season,
          massKgInput: _kg.text,
          harvestDate: date,
          confirmAnomaly: true,
        );
      }
      if (!mounted) return;
      showMessage(context, 'Đã lưu sản lượng — Chờ đồng bộ.');
      Navigator.of(context).pop(true);
    } on CropError catch (e) {
      setState(() => _error = e.message);
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final rows = ref.watch(_harvestRowsProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Khai sản lượng thu hoạch')),
      body: rows.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (list) {
          final row = list
              .where((r) => r.season.seasonRef == _season)
              .firstOrNull;
          return ListView(
            padding: const EdgeInsets.all(16),
            children: [
              if (widget.seasonRef == null) ...[
                Text('Chọn lô', style: Theme.of(context).textTheme.titleMedium),
                if (list.isEmpty)
                  const Text(
                    'Chưa có vụ đang mở.',
                    style: TextStyle(fontSize: 16),
                  ),
                RadioGroup<String>(
                  groupValue: _season,
                  onChanged: (v) => setState(() {
                    _season = v;
                    _error = null;
                  }),
                  child: Column(
                    children: [
                      for (final r in list)
                        RadioListTile<String>(
                          value: r.season.seasonRef,
                          title: Text('Lô ${r.season.plotCode}'),
                          subtitle: Text(
                            'Vụ ${r.season.seasonCode} ${r.season.seasonYear}'
                            '${r.block != null ? ' · chưa đến kỳ thu hoạch' : ''}',
                          ),
                        ),
                    ],
                  ),
                ),
              ],
              if (row != null) ..._form(row),
            ],
          );
        },
      ),
    );
  }

  List<Widget> _form(_HarvestRow row) {
    final s = row.season;
    if (row.queued) {
      return const [
        Card(
          child: ListTile(
            leading: Icon(Icons.cloud_upload, color: AgColors.amber),
            title: Text('Đã khai sản lượng — Chờ đồng bộ'),
          ),
        ),
      ];
    }
    if (row.block != null) {
      return [
        Card(
          key: const ValueKey('harvest-blocked'),
          color: AgColors.amberBg,
          child: ListTile(
            leading: const Icon(Icons.block, color: AgColors.amber),
            title: Text(row.block!),
          ),
        ),
      ];
    }
    String? yieldText;
    try {
      yieldText = formatYield(MassKg.parse(_kg.text), s.areaHa);
    } on YieldError {
      yieldText = null;
    }
    return [
      Text(
        'Lô ${s.plotCode}${s.areaHa == null ? '' : ' · ${s.areaHa!.replaceAll('.', ',')} ha'}',
        style: Theme.of(context).textTheme.titleMedium,
      ),
      const SizedBox(height: 12),
      TextField(
        key: const ValueKey('harvest-kg'),
        controller: _kg,
        keyboardType: const TextInputType.numberWithOptions(decimal: true),
        decoration: InputDecoration(
          labelText: 'Sản lượng (kg)',
          helperText: yieldText == null ? null : 'Năng suất: $yieldText',
        ),
      ),
      const SizedBox(height: 12),
      TextField(
        key: const ValueKey('harvest-date'),
        controller: _date,
        keyboardType: TextInputType.datetime,
        decoration: const InputDecoration(
          labelText: 'Ngày thu hoạch (ngày/tháng/năm)',
        ),
      ),
      if (_error != null)
        Padding(
          padding: const EdgeInsets.only(top: 8),
          child: Text(
            _error!,
            key: const ValueKey('harvest-error'),
            style: const TextStyle(color: AgColors.red, fontSize: 16),
          ),
        ),
      const SizedBox(height: 16),
      FilledButton.icon(
        onPressed: _saving ? null : () => _save(s),
        icon: const Icon(Icons.save),
        label: const Text('Lưu sản lượng'),
      ),
    ];
  }
}
