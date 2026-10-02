import 'package:agrigreen_mobile/data/catalog/catalog_cache.dart';
import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/kn_collection_service.dart';
import 'kn_common.dart';
import 'kn_providers.dart';

class _MLine {
  String? code;
  final qty = TextEditingController();
  final note = TextEditingController();
  void dispose() {
    qty.dispose();
    note.dispose();
  }
}

String _ymd(DateTime d) =>
    '${d.year.toString().padLeft(4, '0')}-${d.month.toString().padLeft(2, '0')}-${d.day.toString().padLeft(2, '0')}';

/// Nhập thông tin vật tư (FN-XA-03.06): chọn xã + ngày ghi nhận, thêm dòng
/// (vật tư theo danh mục `material`, số lượng, ĐVT theo danh mục, ghi chú),
/// Lưu; lịch sử ghi nhận và "Sửa" = bản ghi bù có lý do.
class KnMaterialsScreen extends ConsumerStatefulWidget {
  const KnMaterialsScreen({super.key});
  @override
  ConsumerState<KnMaterialsScreen> createState() => _MaterialsState();
}

class _MaterialsState extends ConsumerState<KnMaterialsScreen> {
  final _history = GlobalKey<KnPagedListState>();
  final _lines = <_MLine>[_MLine()];
  List<CatalogEntry> _catalog = const [];
  String? _commune;
  late DateTime _date = _today();
  Map<int, String> _errors = {};
  bool _busy = false;

  KnCollectionService get _svc => ref.read(knCollectionServiceProvider);

  static DateTime _today() {
    final v = DateTime.now().toUtc().add(const Duration(hours: 7));
    return DateTime(v.year, v.month, v.day);
  }

  @override
  void initState() {
    super.initState();
    _svc.catalog('material').then((c) {
      if (mounted) setState(() => _catalog = c);
    });
  }

  @override
  void dispose() {
    for (final l in _lines) {
      l.dispose();
    }
    super.dispose();
  }

  String _unitOf(String? code) =>
      _catalog.where((c) => c.code == code).firstOrNull?.attrs['unit'] ?? '';

  Future<void> _save() async {
    final communes = ref.read(knCommunesProvider);
    final commune =
        _commune ?? (communes.length == 1 ? communes.first.code : null);
    final errs = <int, String>{};
    for (var i = 0; i < _lines.length; i++) {
      final l = _lines[i];
      if (l.code == null) {
        errs[i] = 'Chọn vật tư';
        continue;
      }
      final m = validateMaterialQuantity(l.qty.text, _unitOf(l.code));
      if (m != null) errs[i] = m;
    }
    setState(() => _errors = errs);
    if (errs.isNotEmpty) return;
    if (commune == null) {
      ScaffoldMessenger.of(context)
          .showSnackBar(const SnackBar(content: Text('Chọn xã.')));
      return;
    }
    setState(() => _busy = true);
    final r = await knRunCommand(
      context,
      () => _svc.createMaterials(
        communeCode: commune,
        recordedOn: _ymd(_date),
        lines: [
          for (final l in _lines)
            (
              code: l.code!,
              quantity: l.qty.text,
              unit: _unitOf(l.code),
              note: l.note.text,
            ),
        ],
      ),
      done: 'Đã ghi nhận vật tư.',
    );
    if (!mounted) return;
    setState(() => _busy = false);
    if (r != null) {
      setState(() {
        for (final l in _lines) {
          l.dispose();
        }
        _lines
          ..clear()
          ..add(_MLine());
      });
      await _history.currentState?.reload();
    }
  }

  Future<void> _correct(Map<String, Object?> m) async {
    final qty = TextEditingController(text: '${m['quantity'] ?? ''}');
    final reason = TextEditingController();
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('Sửa ${m['material_name'] ?? m['material_code']}'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextField(
              key: const ValueKey('kn-mat-c-qty'),
              controller: qty,
              keyboardType: const TextInputType.numberWithOptions(
                decimal: true,
              ),
              decoration: InputDecoration(
                labelText: 'Số lượng (${m['unit'] ?? ''})',
              ),
            ),
            TextField(
              key: const ValueKey('kn-mat-c-reason'),
              controller: reason,
              decoration: const InputDecoration(
                labelText: 'Lý do sửa (≥ 10 ký tự)',
              ),
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Hủy'),
          ),
          FilledButton(
            key: const ValueKey('kn-mat-c-ok'),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Lưu'),
          ),
        ],
      ),
    );
    if (ok == true && mounted) {
      await knRunCommand(
        context,
        () => _svc.correctMaterial(
          '${m['id']}',
          quantity: qty.text,
          unit: '${m['unit'] ?? ''}',
          reason: reason.text,
        ),
        done: 'Đã lưu bản sửa.',
      );
      await _history.currentState?.reload();
    }
    qty.dispose();
    reason.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final communes = ref.watch(knCommunesProvider);
    final form = Padding(
      padding: const EdgeInsets.all(12),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              Expanded(
                child: DropdownButtonFormField<String>(
                  isExpanded: true,
                  key: const ValueKey('kn-mat-commune'),
                  initialValue:
                      _commune ??
                      (communes.length == 1 ? communes.first.code : null),
                  decoration: const InputDecoration(labelText: 'Xã'),
                  items: [
                    for (final c in communes)
                      DropdownMenuItem(value: c.code, child: Text(c.name)),
                  ],
                  onChanged: (v) => setState(() => _commune = v),
                ),
              ),
              const SizedBox(width: 8),
              TextButton.icon(
                key: const ValueKey('kn-mat-date'),
                icon: const Icon(Icons.event),
                label: Text(_ymd(_date)),
                onPressed: () async {
                  final d = await showDatePicker(
                    context: context,
                    initialDate: _date,
                    firstDate: DateTime(2020),
                    lastDate: _today(),
                  );
                  if (d != null) setState(() => _date = d);
                },
              ),
            ],
          ),
          for (var i = 0; i < _lines.length; i++)
            Card(
              child: Padding(
                padding: const EdgeInsets.all(8),
                child: Column(
                  children: [
                    Row(
                      children: [
                        Expanded(
                          child: DropdownButtonFormField<String>(
                            key: ValueKey('kn-mat-code-$i'),
                            initialValue: _lines[i].code,
                            isExpanded: true,
                            decoration: const InputDecoration(
                              labelText: 'Vật tư',
                            ),
                            items: [
                              for (final c in _catalog)
                                DropdownMenuItem(
                                  value: c.code,
                                  child: Text(c.name),
                                ),
                            ],
                            onChanged: (v) =>
                                setState(() => _lines[i].code = v),
                          ),
                        ),
                        if (_lines.length > 1)
                          IconButton(
                            icon: const Icon(Icons.delete_outline),
                            onPressed: () =>
                                setState(() => _lines.removeAt(i).dispose()),
                          ),
                      ],
                    ),
                    Row(
                      children: [
                        Expanded(
                          child: TextField(
                            key: ValueKey('kn-mat-qty-$i'),
                            controller: _lines[i].qty,
                            keyboardType: const TextInputType.numberWithOptions(
                              decimal: true,
                            ),
                            decoration: InputDecoration(
                              labelText: 'Số lượng',
                              suffixText: _unitOf(_lines[i].code),
                            ),
                          ),
                        ),
                        const SizedBox(width: 8),
                        Expanded(
                          child: TextField(
                            controller: _lines[i].note,
                            decoration: const InputDecoration(
                              labelText: 'Ghi chú',
                            ),
                          ),
                        ),
                      ],
                    ),
                    if (_errors[i] != null)
                      Text(
                        _errors[i]!,
                        key: ValueKey('kn-mat-err-$i'),
                        style: const TextStyle(color: AgColors.red),
                      ),
                  ],
                ),
              ),
            ),
          Row(
            children: [
              OutlinedButton.icon(
                onPressed: () => setState(() => _lines.add(_MLine())),
                icon: const Icon(Icons.add),
                label: const Text('Thêm dòng'),
              ),
              const Spacer(),
              FilledButton(
                key: const ValueKey('kn-mat-save'),
                onPressed: _busy ? null : _save,
                child: const Text('Lưu'),
              ),
            ],
          ),
          const Divider(height: 24),
          const Text(
            'Lịch sử ghi nhận',
            style: TextStyle(fontWeight: FontWeight.w700, fontSize: 16),
          ),
        ],
      ),
    );
    return Scaffold(
      appBar: AppBar(title: const Text('Vật tư')),
      body: KnPagedList(
        key: _history,
        header: form,
        empty: 'Chưa có ghi nhận.',
        load: (cur) => _svc.materials(commune: _commune, cursor: cur),
        itemBuilder: (ctx, m) => ListTile(
          key: ValueKey('kn-mat-${m['id']}'),
          title: Text(
            '${m['material_name'] ?? m['material_code']}: ${m['quantity']} ${m['unit'] ?? ''}',
          ),
          subtitle: Text(
            '${knCommuneName(communes, m['commune_code'] as String?)} · ${m['recorded_on']} · ${m['actor_display'] ?? ''}'
            '${m['supersedes_id'] == null ? '' : ' · bản sửa'}',
          ),
          trailing: TextButton(
            onPressed: () => _correct(m),
            child: const Text('Sửa'),
          ),
        ),
      ),
    );
  }
}
