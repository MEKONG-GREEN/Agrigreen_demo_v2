import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/crop_service.dart';
import '../../domain/cropcycle.dart';
import '../providers.dart';
import '../widgets.dart';
import 'batch_log_screen.dart';
import 'plots_screen.dart' show LogCard;

class _LogData {
  const _LogData(this.seasons, this.machines, this.supplement);
  final List<Season> seasons;
  final List<Choice> machines;
  final List<LogEntry> supplement;
}

final _logDataProvider = FutureProvider.autoDispose<_LogData>((ref) async {
  final s = servicesOf(ref);
  return _LogData(
    [
      for (final x in await s.crop.seasons())
        if (x.isOpen) x,
    ],
    await s.crop.machines(),
    await s.crop.needingSupplement(),
  );
});

final _sopProvider = FutureProvider.autoDispose.family<SopVersion?, String>((
  ref,
  seasonRef,
) async {
  final s = servicesOf(ref);
  final se = await s.crop.seasonByRef(seasonRef);
  return se == null ? null : s.crop.sopFor(se);
});

/// Ghi nhật ký bằng biểu tượng (HTX US-LOG-01; quản lý ghi hộ US-LOG-02).
/// GPS và thời gian tự đính kèm lúc lưu và bị khóa sau đó; thiếu GPS không
/// chặn lưu; mất mạng → "Chờ đồng bộ" (US-LOG-04).
class FarmLogScreen extends ConsumerStatefulWidget {
  const FarmLogScreen({super.key, this.seasonRef});
  final String? seasonRef;

  @override
  ConsumerState<FarmLogScreen> createState() => _FarmLogScreenState();
}

class _FarmLogScreenState extends ConsumerState<FarmLogScreen> {
  String? _season;
  FarmActivity? _activity;
  int? _step;
  final _machines = <String>{};
  final _note = TextEditingController();
  final _inName = TextEditingController();
  final _inQty = TextEditingController();
  final _inUnit = TextEditingController(text: 'kg');
  final _inputs = <Map<String, Object?>>[];
  List<String> _photos = const [];
  String? _error;
  bool _saving = false;

  @override
  void initState() {
    super.initState();
    _season = widget.seasonRef;
  }

  @override
  void dispose() {
    _note.dispose();
    _inName.dispose();
    _inQty.dispose();
    _inUnit.dispose();
    super.dispose();
  }

  void _addInput() {
    final name = _inName.text.trim();
    final qty = _inQty.text.trim().replaceAll(',', '.');
    if (name.isEmpty || parseScaled(qty, 3) == null) {
      setState(
        () => _error = 'Vật tư cần tên và số lượng (tối đa 3 chữ số lẻ).',
      );
      return;
    }
    setState(() {
      _inputs.add({'name': name, 'qty': qty, 'unit': _inUnit.text.trim()});
      _inName.clear();
      _inQty.clear();
      _error = null;
    });
  }

  Future<void> _save(List<Season> seasons) async {
    if (_activity == null) {
      setState(() => _error = 'Chọn hoạt động canh tác.');
      return;
    }
    final season = seasons.where((s) => s.seasonRef == _season).firstOrNull;
    setState(() => _saving = true);
    try {
      await ref.services.crop.recordLog(
        season: season,
        activity: _activity!,
        sopStepSeq: _step,
        machineCodes: _machines.toList(),
        inputs: _inputs,
        note: _note.text,
        fileRefs: _photos,
      );
      if (!mounted) return;
      showMessage(context, 'Đã lưu nhật ký — Chờ đồng bộ.');
      Navigator.of(context).pop(true);
    } on CropError catch (e) {
      setState(() => _error = e.message);
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final data = ref.watch(_logDataProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('Ghi nhật ký canh tác'),
        actions: [
          // HTX US-LOG-03: nông dân có nhiều lô cùng hoạt động trong ngày.
          if (widget.seasonRef == null)
            IconButton(
              tooltip: 'Ghi hàng loạt nhiều lô',
              iconSize: 28,
              icon: const Icon(Icons.library_add_check_outlined),
              onPressed: () => Navigator.of(context)
                  .push(
                    MaterialPageRoute<void>(
                      builder: (_) => const BatchLogScreen(),
                    ),
                  )
                  .then((_) => ref.invalidate(_logDataProvider)),
            ),
        ],
      ),
      body: data.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (d) {
          final season = d.seasons
              .where((s) => s.seasonRef == _season)
              .firstOrNull;
          final sop = _season == null
              ? null
              : ref.watch(_sopProvider(_season!)).value;
          return ListView(
            padding: const EdgeInsets.all(12),
            children: [
              if (widget.seasonRef == null && d.supplement.isNotEmpty) ...[
                Text(
                  'Cần bổ sung',
                  style: Theme.of(context).textTheme.titleMedium,
                ),
                for (final l in d.supplement)
                  LogCard(
                    entry: l,
                    onSupplement: (w) =>
                        Navigator.of(context)
                            .push(MaterialPageRoute<void>(builder: (_) => w))
                            .then((_) => ref.invalidate(_logDataProvider)),
                  ),
                const Divider(height: 24),
              ],
              if (widget.seasonRef == null) ...[
                Text('Lô / vụ', style: Theme.of(context).textTheme.titleMedium),
                if (d.seasons.isEmpty)
                  const Padding(
                    padding: EdgeInsets.symmetric(vertical: 8),
                    child: Text(
                      msgSeasonNotOpen,
                      style: TextStyle(fontSize: 16),
                    ),
                  ),
                RadioGroup<String>(
                  groupValue: _season,
                  onChanged: (v) => setState(() {
                    _season = v;
                    _step = null;
                  }),
                  child: Column(
                    children: [
                      for (final s in d.seasons)
                        RadioListTile<String>(
                          value: s.seasonRef,
                          title: Text('Lô ${s.plotCode}'),
                          subtitle: Text(
                            'Vụ ${s.seasonCode} ${s.seasonYear} · ${s.varietyCode}',
                          ),
                        ),
                    ],
                  ),
                ),
              ] else if (season != null)
                Text(
                  'Lô ${season.plotCode} · vụ ${season.seasonCode} ${season.seasonYear}',
                  style: Theme.of(context).textTheme.titleMedium,
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
              if (sop != null && sop.steps.isNotEmpty) ...[
                const SizedBox(height: 12),
                DropdownButtonFormField<int?>(
                  initialValue: _step,
                  isExpanded: true,
                  decoration: const InputDecoration(
                    labelText: 'Bước quy trình (không bắt buộc)',
                  ),
                  items: [
                    const DropdownMenuItem<int?>(child: Text('— Không chọn —')),
                    for (final st in sop.steps)
                      DropdownMenuItem<int?>(
                        value: st.seq,
                        child: Text('${st.seq}. ${st.name}'),
                      ),
                  ],
                  onChanged: (v) => setState(() => _step = v),
                ),
              ],
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
              ExpansionTile(
                tilePadding: EdgeInsets.zero,
                title: Text(
                  'Vật tư đã dùng (${_inputs.length}) — không bắt buộc',
                ),
                children: [
                  for (final i in _inputs)
                    ListTile(
                      dense: true,
                      title: Text('${i['name']}: ${i['qty']} ${i['unit']}'),
                      trailing: IconButton(
                        tooltip: 'Bỏ',
                        icon: const Icon(Icons.close),
                        onPressed: () => setState(() => _inputs.remove(i)),
                      ),
                    ),
                  TextField(
                    controller: _inName,
                    decoration: const InputDecoration(labelText: 'Tên vật tư'),
                  ),
                  const SizedBox(height: 8),
                  Row(
                    children: [
                      Expanded(
                        child: TextField(
                          controller: _inQty,
                          keyboardType: const TextInputType.numberWithOptions(
                            decimal: true,
                          ),
                          decoration: const InputDecoration(
                            labelText: 'Số lượng',
                          ),
                        ),
                      ),
                      const SizedBox(width: 8),
                      SizedBox(
                        width: 90,
                        child: TextField(
                          controller: _inUnit,
                          decoration: const InputDecoration(
                            labelText: 'Đơn vị',
                          ),
                        ),
                      ),
                    ],
                  ),
                  Align(
                    alignment: Alignment.centerRight,
                    child: TextButton.icon(
                      onPressed: _addInput,
                      icon: const Icon(Icons.add),
                      label: const Text('Thêm vật tư'),
                    ),
                  ),
                ],
              ),
              TextField(
                key: const ValueKey('log-note'),
                controller: _note,
                maxLines: 2,
                decoration: const InputDecoration(
                  labelText: 'Ghi chú (không bắt buộc)',
                ),
              ),
              const SizedBox(height: 12),
              PhotoCapture(
                householdRef: season?.householdRef,
                photoIds: _photos,
                onChanged: (l) => setState(() => _photos = l),
                parentEntity: 'farm_log',
              ),
              const SizedBox(height: 8),
              const ListTile(
                contentPadding: EdgeInsets.zero,
                leading: Icon(Icons.lock_clock, color: AgColors.g700),
                title: Text('GPS và thời gian tự đính kèm khi lưu'),
                subtitle: Text(
                  'Sau khi lưu không sửa được. Không có GPS vẫn lưu (nhãn "Thiếu GPS").',
                ),
              ),
              if (_error != null)
                Padding(
                  padding: const EdgeInsets.only(bottom: 8),
                  child: Text(
                    _error!,
                    key: const ValueKey('log-error'),
                    style: const TextStyle(color: AgColors.red, fontSize: 16),
                  ),
                ),
              FilledButton.icon(
                onPressed: _saving ? null : () => _save(d.seasons),
                icon: const Icon(Icons.save),
                label: const Text('Lưu nhật ký'),
              ),
            ],
          );
        },
      ),
    );
  }
}

/// Ô chọn hoạt động bằng biểu tượng (dùng chung cho ghi hàng loạt).
class ActivityTile extends StatelessWidget {
  const ActivityTile({
    super.key,
    required this.activity,
    required this.selected,
    required this.onTap,
  });
  final FarmActivity activity;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) => Semantics(
    selected: selected,
    button: true,
    label: activity.label,
    child: Material(
      color: selected ? AgColors.g700 : Colors.white,
      borderRadius: BorderRadius.circular(14),
      child: InkWell(
        key: ValueKey('act-${activity.code}'),
        borderRadius: BorderRadius.circular(14),
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.all(6),
          child: FittedBox(
            fit: BoxFit.scaleDown,
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Icon(
                  activityIcon(activity),
                  size: 36,
                  color: selected ? Colors.white : AgColors.g700,
                ),
                const SizedBox(height: 4),
                Text(
                  activity.label,
                  style: TextStyle(
                    fontSize: 16,
                    fontWeight: FontWeight.w600,
                    color: selected ? Colors.white : AgColors.ink,
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    ),
  );
}
