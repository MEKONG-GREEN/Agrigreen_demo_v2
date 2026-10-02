import 'package:agrigreen_mobile/data/catalog/catalog_cache.dart';
import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/kn_collection_service.dart';
import 'kn_common.dart';
import 'kn_providers.dart';

/// Máy móc HTX (FN-XA-03.04): tổng theo loại của xã = Σ các HTX; mở HTX để
/// khai báo số lượng từng loại máy.
class KnMachinesScreen extends ConsumerStatefulWidget {
  const KnMachinesScreen({super.key});
  @override
  ConsumerState<KnMachinesScreen> createState() => _MachinesState();
}

class _MachinesState extends ConsumerState<KnMachinesScreen> {
  Map<String, Object?>? _data;
  bool _cached = false;
  String? _error;
  String? _commune;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final (j, cached) = await ref
          .read(knCollectionServiceProvider)
          .machineCounts(commune: _commune);
      if (mounted) setState(() => (_data = j, _cached = cached, _error = null));
    } on KnCommandError catch (e) {
      if (mounted) setState(() => _error = e.message);
    } on Object {
      if (mounted) {
        setState(() => _error = 'Không tải được — cần kết nối mạng.');
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final communes = ref.watch(knCommunesProvider);
    final d = _data;
    final coops = [
      for (final c in (d?['cooperatives'] as List?) ?? const [])
        if (c is Map) c.cast<String, Object?>(),
    ];
    final totals = [
      for (final t in (d?['commune_totals'] as List?) ?? const [])
        if (t is Map) t.cast<String, Object?>(),
    ];
    return Scaffold(
      appBar: AppBar(title: const Text('Máy móc HTX')),
      body: RefreshIndicator(
        onRefresh: _load,
        child: ListView(
          padding: const EdgeInsets.only(bottom: 32),
          children: [
            if (_cached) const KnOfflineBanner(),
            Padding(
              padding: const EdgeInsets.all(12),
              child: DropdownButtonFormField<String?>(
                isExpanded: true,
                initialValue: _commune,
                decoration: const InputDecoration(labelText: 'Xã'),
                items: [
                  const DropdownMenuItem<String?>(
                    value: null,
                    child: Text('Tất cả xã'),
                  ),
                  for (final c in communes)
                    DropdownMenuItem<String?>(
                      value: c.code,
                      child: Text(c.name),
                    ),
                ],
                onChanged: (v) {
                  _commune = v;
                  _load();
                },
              ),
            ),
            if (_error != null)
              Padding(padding: const EdgeInsets.all(12), child: Text(_error!)),
            if (totals.isNotEmpty)
              Card(
                margin: const EdgeInsets.all(12),
                child: Padding(
                  padding: const EdgeInsets.all(12),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      const Text(
                        'Tổng theo loại máy của xã',
                        style: TextStyle(fontWeight: FontWeight.w700),
                      ),
                      for (final t in totals)
                        Text(
                          '${knCommuneName(communes, t['commune_code'] as String?)} · '
                          '${t['machine_type_name'] ?? t['machine_type']}: ${t['quantity']}',
                        ),
                    ],
                  ),
                ),
              ),
            if (coops.isEmpty && _error == null && d != null)
              const Padding(
                padding: EdgeInsets.all(24),
                child: Text('Chưa có HTX.', textAlign: TextAlign.center),
              ),
            for (final c in coops)
              Card(
                margin: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
                child: ListTile(
                  key: ValueKey('kn-mc-${c['cooperative_ref']}'),
                  title: Text('${c['name']}'),
                  subtitle: Text(
                    [
                      for (final l in (c['lines'] as List?) ?? const [])
                        if (l is Map)
                          '${l['machine_type_name'] ?? l['machine_type']}: ${l['quantity']}',
                    ].join(' · ').ifEmpty('Chưa khai báo'),
                  ),
                  trailing: Text('Tổng ${c['total'] ?? 0}'),
                  onTap: () async {
                    await Navigator.of(context).push(
                      MaterialPageRoute<void>(
                        builder: (_) => Scaffold(
                          appBar: AppBar(title: Text('Máy móc · ${c['name']}')),
                          body: KnMachineEditor(
                            cooperativeRef: '${c['cooperative_ref']}',
                          ),
                        ),
                      ),
                    );
                    await _load();
                  },
                ),
              ),
          ],
        ),
      ),
    );
  }
}

extension on String {
  String ifEmpty(String v) => isEmpty ? v : this;
}

class _Line {
  _Line(this.type, String qty, String note)
    : qty = TextEditingController(text: qty),
      note = TextEditingController(text: note);
  String? type;
  final TextEditingController qty;
  final TextEditingController note;
  void dispose() {
    qty.dispose();
    note.dispose();
  }
}

/// Bảng khai báo máy của một HTX: loại máy (danh mục `machine_type`), số lượng
/// (số nguyên ≥ 0), ghi chú, xóa dòng, Thêm dòng, Lưu (thay toàn bộ khai báo).
class KnMachineEditor extends ConsumerStatefulWidget {
  const KnMachineEditor({
    super.key,
    required this.cooperativeRef,
    this.embedded = false,
  });
  final String cooperativeRef;
  final bool embedded;
  @override
  ConsumerState<KnMachineEditor> createState() => _EditorState();
}

class _EditorState extends ConsumerState<KnMachineEditor> {
  final _lines = <_Line>[];
  List<CatalogEntry> _types = const [];
  Map<int, String> _errors = {};
  bool _loading = true;
  bool _cached = false;
  String? _error;

  KnCollectionService get _svc => ref.read(knCollectionServiceProvider);

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    for (final l in _lines) {
      l.dispose();
    }
    super.dispose();
  }

  Future<void> _load() async {
    final types = await _svc.catalog('machine_type');
    try {
      final (j, cached) = await _svc.machineCountsOf(widget.cooperativeRef);
      if (!mounted) return;
      for (final l in _lines) {
        l.dispose();
      }
      _lines
        ..clear()
        ..addAll([
          for (final l in (j['lines'] as List?) ?? const [])
            if (l is Map)
              _Line(
                l['machine_type'] as String?,
                '${l['quantity'] ?? 0}',
                '${l['note'] ?? ''}',
              ),
        ]);
      setState(() {
        _types = types;
        _cached = cached;
        _loading = false;
        _error = null;
      });
    } on Object catch (e) {
      if (mounted) {
        setState(() {
          _types = types;
          _loading = false;
          _error = e is KnCommandError
              ? e.message
              : 'Không tải được — cần kết nối mạng.';
        });
      }
    }
  }

  List<KnMachineLine> get _values => [
    for (final l in _lines)
      KnMachineLine(l.type ?? '', l.qty.text, note: l.note.text),
  ];

  Future<void> _save() async {
    final errs = validateMachineLines(_values);
    setState(() => _errors = errs);
    if (errs.isNotEmpty) return;
    await knRunCommand(
      context,
      () => _svc.saveMachineCounts(widget.cooperativeRef, _values),
      done: 'Đã lưu khai báo máy móc.',
    );
  }

  @override
  Widget build(BuildContext context) {
    if (_loading) return const Center(child: CircularProgressIndicator());
    return ListView(
      padding: const EdgeInsets.all(12),
      children: [
        if (_cached) const KnOfflineBanner(),
        if (_error != null) Text(_error!),
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
                          key: ValueKey('kn-mc-type-$i'),
                          initialValue:
                              _types.any((t) => t.code == _lines[i].type)
                              ? _lines[i].type
                              : null,
                          isExpanded: true,
                          decoration: const InputDecoration(
                            labelText: 'Loại máy',
                          ),
                          items: [
                            for (final t in _types)
                              DropdownMenuItem(
                                value: t.code,
                                child: Text(t.name),
                              ),
                          ],
                          onChanged: (v) => setState(() {
                            _lines[i].type = v;
                            _errors = validateMachineLines(_values);
                          }),
                        ),
                      ),
                      IconButton(
                        tooltip: 'Xóa dòng',
                        icon: const Icon(Icons.delete_outline),
                        onPressed: () => setState(() {
                          _lines.removeAt(i).dispose();
                          _errors = {};
                        }),
                      ),
                    ],
                  ),
                  Row(
                    children: [
                      SizedBox(
                        width: 110,
                        child: TextField(
                          key: ValueKey('kn-mc-qty-$i'),
                          controller: _lines[i].qty,
                          keyboardType: TextInputType.number,
                          decoration: const InputDecoration(
                            labelText: 'Số lượng',
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
                      key: ValueKey('kn-mc-err-$i'),
                      style: const TextStyle(color: AgColors.red),
                    ),
                ],
              ),
            ),
          ),
        Row(
          children: [
            OutlinedButton.icon(
              key: const ValueKey('kn-mc-add'),
              onPressed: () => setState(() => _lines.add(_Line(null, '0', ''))),
              icon: const Icon(Icons.add),
              label: const Text('Thêm dòng'),
            ),
            const Spacer(),
            FilledButton(
              key: const ValueKey('kn-mc-save'),
              onPressed: _save,
              child: const Text('Lưu'),
            ),
          ],
        ),
        if (widget.embedded) const SizedBox(height: 80),
      ],
    );
  }
}
