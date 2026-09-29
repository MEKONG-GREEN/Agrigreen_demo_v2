import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/crop_service.dart';
import '../../domain/cropcycle.dart';
import '../providers.dart';

class _OpenData {
  const _OpenData(this.plots, this.seasons, this.varieties);
  final List<PlotItem> plots;
  final List<Choice> seasons;
  final List<Choice> varieties;
}

final _openDataProvider = FutureProvider.autoDispose<_OpenData>((ref) async {
  final s = servicesOf(ref);
  final plots = <PlotItem>[];
  for (final p in await s.crop.plots()) {
    if (await s.crop.openSeasonFor(p.ref) == null) plots.add(p);
  }
  return _OpenData(
    plots,
    await s.crop.seasonsCatalog(),
    await s.crop.varieties(),
  );
});

/// Ô nhập ngày `dd/mm/yyyy` → ngày (null nếu sai).
DateTime? parseDisplayDay(String s) {
  final m = RegExp(r'^\s*(\d{1,2})/(\d{1,2})/(\d{4})\s*$').firstMatch(s);
  if (m == null) return null;
  final d = DateTime.utc(
    int.parse(m.group(3)!),
    int.parse(m.group(2)!),
    int.parse(m.group(1)!),
  );
  return d.day == int.parse(m.group(1)!) && d.month == int.parse(m.group(2)!)
      ? d
      : null;
}

/// Mở vụ 4 bước (HTX US-SEASON-01): lô + vụ/năm → giống (SOP tự gán) → ngày
/// gieo sạ trong [−15, +30] ngày → xác nhận (GPS tự lấy, không bắt buộc).
/// Lưu vào hàng đợi — làm được khi ngoại tuyến.
class OpenSeasonScreen extends ConsumerStatefulWidget {
  const OpenSeasonScreen({super.key, this.plotRef});
  final String? plotRef;

  @override
  ConsumerState<OpenSeasonScreen> createState() => _OpenSeasonScreenState();
}

class _OpenSeasonScreenState extends ConsumerState<OpenSeasonScreen> {
  int _step = 0;
  String? _plot;
  String? _season;
  late int _year;
  String? _variety;
  String? _sopName;
  bool _sopResolved = false;

  /// Cấu hình vụ trước đã sao chép (HTX US-SEASON-03); đổi giống → bỏ.
  PreviousConfig? _copied;
  bool _copying = false;
  final _date = TextEditingController();
  String? _error;
  bool _saving = false;

  @override
  void initState() {
    super.initState();
    _plot = widget.plotRef;
    final today = ref.read(appControllerProvider).services!.crop.today();
    _year = today.year;
    _date.text = displayDay(today);
  }

  @override
  void dispose() {
    _date.dispose();
    super.dispose();
  }

  /// HTX US-SEASON-03: lấy giống + SOP của vụ vừa hoàn thành trên lô.
  Future<void> _copyPrevious(List<PlotItem> plots) async {
    final plot = plots.where((p) => p.ref == _plot).firstOrNull;
    if (plot == null) return;
    final crop = ref.services.crop;
    setState(() => _copying = true);
    try {
      final c = await crop.previousConfig(plot);
      final sop = await crop.sopById(c.sopVersionId);
      if (!mounted) return;
      setState(() {
        _copied = c;
        _variety = c.varietyCode;
        _sopName = sop?.name;
        _sopResolved = true;
        _error = null;
      });
      showMessage(
        context,
        'Đã sao chép giống ${c.varietyCode} và quy trình của vụ '
        '${c.seasonCode} ${c.seasonYear}.',
      );
    } on CropError catch (e) {
      setState(() => _error = e.message);
    } finally {
      if (mounted) setState(() => _copying = false);
    }
  }

  Future<void> _pickVariety(String code) async {
    setState(() {
      _variety = code;
      _sopResolved = false;
      _copied = null;
    });
    final sop = await ref.services.crop.sopForVariety(code);
    if (!mounted) return;
    setState(() {
      _sopName = sop?.name;
      _sopResolved = true;
    });
  }

  Future<void> _pickDate() async {
    final crop = ref.services.crop;
    final today = crop.today();
    final d = await showDatePicker(
      context: context,
      initialDate: parseDisplayDay(_date.text) ?? today,
      firstDate: addDays(today, -sowingPastDays),
      lastDate: addDays(today, sowingFutureDays),
    );
    if (d != null) {
      setState(
        () => _date.text = displayDay(DateTime.utc(d.year, d.month, d.day)),
      );
    }
  }

  String? _validate() {
    switch (_step) {
      case 0:
        if (_plot == null) return 'Chọn lô ruộng.';
        if (_season == null) return 'Chọn vụ.';
        return null;
      case 1:
        return _variety == null ? 'Chọn giống lúa.' : null;
      case 2:
        final d = parseDisplayDay(_date.text);
        if (d == null) {
          return 'Nhập ngày theo dạng ngày/tháng/năm, ví dụ 05/10/2026.';
        }
        return checkSowingDate(d, ref.services.crop.today());
    }
    return null;
  }

  void _next() {
    final e = _validate();
    setState(() {
      _error = e;
      if (e == null && _step < 3) _step++;
    });
  }

  Future<void> _save(List<PlotItem> plots) async {
    final plot = plots.firstWhere((p) => p.ref == _plot);
    setState(() => _saving = true);
    try {
      await ref.services.crop.openSeason(
        plot: plot,
        seasonCode: _season!,
        seasonYear: _year,
        varietyCode: _variety!,
        sowingDate: parseDisplayDay(_date.text)!,
        copied: _copied,
      );
      if (!mounted) return;
      showMessage(context, 'Đã mở vụ cho lô ${plot.code} — Chờ đồng bộ.');
      Navigator.of(context).pop(true);
    } on CropError catch (e) {
      setState(() => _error = e.message);
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final data = ref.watch(_openDataProvider);
    return Scaffold(
      appBar: AppBar(title: Text('Mở vụ mới · Bước ${_step + 1}/4')),
      body: data.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (d) => ListView(
          padding: const EdgeInsets.all(16),
          children: [
            LinearProgressIndicator(value: (_step + 1) / 4),
            const SizedBox(height: 16),
            ...switch (_step) {
              0 => _stepPlot(d),
              1 => _stepVariety(d),
              2 => _stepDate(),
              _ => _stepConfirm(d),
            },
            if (_error != null)
              Padding(
                padding: const EdgeInsets.only(top: 12),
                child: Text(
                  _error!,
                  key: const ValueKey('open-season-error'),
                  style: const TextStyle(color: AgColors.red, fontSize: 16),
                ),
              ),
            const SizedBox(height: 16),
            Row(
              children: [
                if (_step > 0)
                  Expanded(
                    child: OutlinedButton(
                      onPressed: () => setState(() {
                        _step--;
                        _error = null;
                      }),
                      child: const Text('Quay lại'),
                    ),
                  ),
                if (_step > 0) const SizedBox(width: 12),
                Expanded(
                  child: _step < 3
                      ? FilledButton(
                          onPressed: _next,
                          child: const Text('Tiếp'),
                        )
                      : FilledButton(
                          onPressed: _saving ? null : () => _save(d.plots),
                          child: const Text('Lưu mở vụ'),
                        ),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }

  List<Widget> _stepPlot(_OpenData d) => [
    Text('Chọn lô ruộng', style: Theme.of(context).textTheme.titleMedium),
    if (d.plots.isEmpty)
      const Text('Mọi lô đều đang có vụ, hoặc chưa đồng bộ lô ruộng.'),
    RadioGroup<String>(
      groupValue: _plot,
      onChanged: (v) => setState(() => _plot = v),
      child: Column(
        children: [
          for (final p in d.plots)
            RadioListTile<String>(
              value: p.ref,
              title: Text(p.title),
              subtitle: p.areaHa == null
                  ? null
                  : Text('${p.areaHa!.replaceAll('.', ',')} ha'),
            ),
        ],
      ),
    ),
    const SizedBox(height: 12),
    Text('Vụ', style: Theme.of(context).textTheme.titleMedium),
    if (d.seasons.isEmpty)
      const Text('Chưa có danh mục vụ trên máy — đồng bộ khi có mạng.'),
    Wrap(
      spacing: 8,
      runSpacing: 8,
      children: [
        for (final s in d.seasons)
          ChoiceChip(
            label: Text(s.name, style: const TextStyle(fontSize: 17)),
            selected: _season == s.code,
            onSelected: (_) => setState(() => _season = s.code),
          ),
      ],
    ),
    const SizedBox(height: 12),
    Row(
      children: [
        const Text('Năm: ', style: TextStyle(fontSize: 17)),
        IconButton(
          tooltip: 'Năm trước',
          icon: const Icon(Icons.remove_circle_outline),
          onPressed: () => setState(() => _year--),
        ),
        Text(
          '$_year',
          style: const TextStyle(fontSize: 20, fontWeight: FontWeight.w700),
        ),
        IconButton(
          tooltip: 'Năm sau',
          icon: const Icon(Icons.add_circle_outline),
          onPressed: () => setState(() => _year++),
        ),
      ],
    ),
  ];

  List<Widget> _stepVariety(_OpenData d) => [
    OutlinedButton.icon(
      key: const ValueKey('copy-previous'),
      onPressed: _copying ? null : () => _copyPrevious(d.plots),
      icon: const Icon(Icons.content_copy),
      label: const Text('Sao chép cấu hình vụ trước'),
    ),
    if (_copied != null)
      Padding(
        padding: const EdgeInsets.only(top: 6),
        child: Text(
          'Theo vụ ${_copied!.seasonCode} ${_copied!.seasonYear}'
          '${_copied!.offline ? ' (dữ liệu trên máy — máy chủ kiểm lại khi đồng bộ)' : ''}',
          style: const TextStyle(color: AgColors.g700),
        ),
      ),
    const SizedBox(height: 12),
    Text('Chọn giống lúa', style: Theme.of(context).textTheme.titleMedium),
    if (d.varieties.isEmpty)
      const Text('Chưa có danh mục giống trên máy — đồng bộ khi có mạng.'),
    RadioGroup<String>(
      groupValue: _variety,
      onChanged: (v) {
        if (v != null) _pickVariety(v);
      },
      child: Column(
        children: [
          for (final v in d.varieties)
            RadioListTile<String>(value: v.code, title: Text(v.name)),
        ],
      ),
    ),
    if (_variety != null && _sopResolved)
      Card(
        color: AgColors.g100,
        child: ListTile(
          leading: const Icon(Icons.rule, color: AgColors.g700),
          title: const Text('Quy trình SOP (tự gán theo giống)'),
          subtitle: Text(
            _sopName ?? 'Máy chủ sẽ gán quy trình chuẩn của giống khi đồng bộ.',
          ),
        ),
      ),
  ];

  List<Widget> _stepDate() {
    final today = ref.read(appControllerProvider).services!.crop.today();
    return [
      Text('Ngày gieo sạ', style: Theme.of(context).textTheme.titleMedium),
      const SizedBox(height: 8),
      TextField(
        key: const ValueKey('sowing-date'),
        controller: _date,
        keyboardType: TextInputType.datetime,
        decoration: InputDecoration(
          labelText: 'Ngày/tháng/năm',
          helperText:
              'Từ ${displayDay(addDays(today, -sowingPastDays))} đến ${displayDay(addDays(today, sowingFutureDays))}',
          suffixIcon: IconButton(
            tooltip: 'Chọn ngày',
            icon: const Icon(Icons.calendar_month),
            onPressed: _pickDate,
          ),
        ),
      ),
    ];
  }

  List<Widget> _stepConfirm(_OpenData d) {
    final plot = d.plots.where((p) => p.ref == _plot).firstOrNull;
    String name(List<Choice> l, String? c) =>
        l.where((x) => x.code == c).firstOrNull?.name ?? '$c';
    return [
      Text('Xác nhận mở vụ', style: Theme.of(context).textTheme.titleMedium),
      Card(
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('Lô: ${plot?.title ?? _plot}'),
              Text('Vụ: ${name(d.seasons, _season)} $_year'),
              Text('Giống: ${name(d.varieties, _variety)}'),
              Text('Quy trình: ${_sopName ?? 'tự gán khi đồng bộ'}'),
              Text('Ngày sạ: ${_date.text}'),
            ],
          ),
        ),
      ),
      const ListTile(
        leading: Icon(Icons.my_location, color: AgColors.g700),
        title: Text('Vị trí GPS tự lấy khi lưu'),
        subtitle: Text(
          'Không lấy được vị trí vẫn lưu được (gắn nhãn "Thiếu GPS").',
        ),
      ),
    ];
  }
}
