import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/kn_collection_service.dart';
import 'kn_common.dart';
import 'kn_machines_screen.dart';
import 'kn_plots_screen.dart';
import 'kn_providers.dart';

String _rep(Map<String, Object?> c) {
  final r = c['representative'];
  if (r is! Map) return '—';
  return [r['label'], r['masked_phone']].whereType<String>().join(' · ');
}

/// Danh sách HTX (FN-XA-03.03): lọc xã, trạng thái, tìm tên; SĐT người đại
/// diện chỉ hiển thị dạng che (máy chủ trả `masked_phone`).
class KnCooperativesScreen extends ConsumerStatefulWidget {
  const KnCooperativesScreen({super.key});
  @override
  ConsumerState<KnCooperativesScreen> createState() => _CoopsState();
}

class _CoopsState extends ConsumerState<KnCooperativesScreen> {
  final _list = GlobalKey<KnPagedListState>();
  final _q = TextEditingController();
  String? _commune;
  String _status = 'active';

  @override
  void dispose() {
    _q.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final svc = ref.read(knCollectionServiceProvider);
    final communes = ref.watch(knCommunesProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Danh sách HTX')),
      floatingActionButton: FloatingActionButton.extended(
        key: const ValueKey('kn-coop-add'),
        onPressed: () async {
          final ok = await Navigator.of(context).push<bool>(
            MaterialPageRoute(builder: (_) => const KnCooperativeFormScreen()),
          );
          if (ok == true) await _list.currentState?.reload();
        },
        icon: const Icon(Icons.add),
        label: const Text('Thêm HTX'),
      ),
      body: KnPagedList(
        key: _list,
        empty: 'Chưa có HTX trong các xã phụ trách.',
        header: Padding(
          padding: const EdgeInsets.all(12),
          child: Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              SizedBox(
                width: 220,
                child: TextField(
                  key: const ValueKey('kn-coop-q'),
                  controller: _q,
                  decoration: const InputDecoration(
                    labelText: 'Tìm tên HTX',
                    isDense: true,
                    prefixIcon: Icon(Icons.search),
                  ),
                  onSubmitted: (_) => _list.currentState?.reload(),
                ),
              ),
              SizedBox(
                width: 160,
                child: DropdownButtonFormField<String?>(
                  isExpanded: true,
                  initialValue: _commune,
                  decoration: const InputDecoration(
                    labelText: 'Xã',
                    isDense: true,
                  ),
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
                    _list.currentState?.reload();
                  },
                ),
              ),
              SizedBox(
                width: 150,
                child: DropdownButtonFormField<String>(
                  isExpanded: true,

                  initialValue: _status,
                  decoration: const InputDecoration(
                    labelText: 'Trạng thái',
                    isDense: true,
                  ),
                  items: const [
                    DropdownMenuItem(
                      value: 'active',
                      child: Text('Đang hoạt động'),
                    ),
                    DropdownMenuItem(
                      value: 'inactive',
                      child: Text('Vô hiệu hóa'),
                    ),
                    DropdownMenuItem(value: 'all', child: Text('Tất cả')),
                  ],
                  onChanged: (v) {
                    _status = v ?? 'active';
                    _list.currentState?.reload();
                  },
                ),
              ),
            ],
          ),
        ),
        load: (cursor) => svc.cooperatives(
          commune: _commune,
          status: _status,
          q: _q.text,
          cursor: cursor,
        ),
        itemBuilder: (ctx, c) => Card(
          margin: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
          child: ListTile(
            key: ValueKey('kn-coop-${c['ref']}'),
            title: Text('${c['name']}'),
            subtitle: Text(
              [
                knCommuneName(communes, c['commune_code'] as String?),
                'Đại diện: ${_rep(c)}',
                'Diện tích: ${c['area_ha'] ?? '—'} ha · ${c['household_count'] ?? 0} nông hộ',
              ].join('\n'),
            ),
            isThreeLine: true,
            trailing: c['active'] == false
                ? const KnChip('Vô hiệu hóa', AgColors.mute)
                : const Icon(Icons.chevron_right),
            onTap: () async {
              await Navigator.of(ctx).push(
                MaterialPageRoute<void>(
                  builder: (_) => KnCooperativeDetailScreen(ref: '${c['ref']}'),
                ),
              );
              await _list.currentState?.reload();
            },
          ),
        ),
      ),
    );
  }
}

/// Thêm / sửa HTX.
class KnCooperativeFormScreen extends ConsumerStatefulWidget {
  const KnCooperativeFormScreen({super.key, this.existing});
  final Map<String, Object?>? existing;
  @override
  ConsumerState<KnCooperativeFormScreen> createState() => _CoopFormState();
}

class _CoopFormState extends ConsumerState<KnCooperativeFormScreen> {
  late final _name = TextEditingController(text: _s('name'));
  late final _area = TextEditingController(text: _s('area_ha'));
  late final _tax = TextEditingController(text: _s('tax_code'));
  late final _office = TextEditingController(text: _s('head_office'));
  final _repName = TextEditingController();
  final _repPhone = TextEditingController();
  final _reason = TextEditingController();
  String? _commune;
  bool _busy = false;

  String _s(String k) {
    final v = widget.existing?[k];
    return v == null ? '' : '$v';
  }

  @override
  void initState() {
    super.initState();
    _commune = widget.existing?['commune_code'] as String?;
  }

  @override
  void dispose() {
    for (final c in [
      _name,
      _area,
      _tax,
      _office,
      _repName,
      _repPhone,
      _reason,
    ]) {
      c.dispose();
    }
    super.dispose();
  }

  Future<void> _save() async {
    final svc = ref.read(knCollectionServiceProvider);
    final communes = ref.read(knCommunesProvider);
    final commune =
        _commune ?? (communes.length == 1 ? communes.first.code : null);
    if (_name.text.trim().length < 3) {
      _msg('Tên HTX tối thiểu 3 ký tự.');
      return;
    }
    if (widget.existing == null && commune == null) {
      _msg('Chọn xã của HTX.');
      return;
    }
    setState(() => _busy = true);
    final ex = widget.existing;
    final r = await knRunCommand(
      context,
      () => ex == null
          ? svc.createCooperative(
              name: _name.text,
              communeCode: commune!,
              areaHa: _area.text,
              taxCode: _tax.text,
              headOffice: _office.text,
              repName: _repName.text,
              repPhone: _repPhone.text,
            )
          : svc.updateCooperative(
              '${ex['ref']}',
              name: _name.text,
              areaHa: _area.text,
              taxCode: _tax.text,
              headOffice: _office.text,
              repName: _repName.text,
              repPhone: _repPhone.text,
              reason: _reason.text,
            ),
      done: ex == null ? 'Đã thêm HTX.' : 'Đã lưu thay đổi.',
    );
    if (!mounted) return;
    setState(() => _busy = false);
    if (r != null) Navigator.of(context).pop(true);
  }

  void _msg(String m) =>
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(m)));

  @override
  Widget build(BuildContext context) {
    final communes = ref.watch(knCommunesProvider);
    final edit = widget.existing != null;
    Widget f(
      String key,
      TextEditingController c,
      String label, {
      TextInputType? kb,
    }) => Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: TextField(
        key: ValueKey(key),
        controller: c,
        keyboardType: kb,
        decoration: InputDecoration(labelText: label),
      ),
    );
    return Scaffold(
      appBar: AppBar(title: Text(edit ? 'Sửa HTX' : 'Thêm HTX')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          f('kn-coop-f-name', _name, 'Tên HTX *'),
          if (!edit)
            Padding(
              padding: const EdgeInsets.only(bottom: 8),
              child: DropdownButtonFormField<String>(
                isExpanded: true,
                key: const ValueKey('kn-coop-f-commune'),
                initialValue: _commune,
                decoration: const InputDecoration(labelText: 'Xã *'),
                items: [
                  for (final c in communes)
                    DropdownMenuItem(value: c.code, child: Text(c.name)),
                ],
                onChanged: (v) => setState(() => _commune = v),
              ),
            ),
          f(
            'kn-coop-f-area',
            _area,
            'Diện tích (ha)',
            kb: const TextInputType.numberWithOptions(decimal: true),
          ),
          f('kn-coop-f-tax', _tax, 'Mã số thuế'),
          f('kn-coop-f-office', _office, 'Trụ sở'),
          f(
            'kn-coop-f-rep',
            _repName,
            edit
                ? 'Người đại diện mới (để trống = giữ nguyên)'
                : 'Người đại diện',
          ),
          f(
            'kn-coop-f-phone',
            _repPhone,
            'SĐT người đại diện',
            kb: TextInputType.phone,
          ),
          if (edit) f('kn-coop-f-reason', _reason, 'Lý do sửa'),
          const SizedBox(height: 8),
          FilledButton(
            key: const ValueKey('kn-coop-f-save'),
            onPressed: _busy ? null : _save,
            child: const Text('Lưu'),
          ),
        ],
      ),
    );
  }
}

/// Chi tiết HTX: thông tin chung, Sửa / Thêm nông hộ / Vô hiệu hóa; tab Nông
/// hộ / Máy móc / Thửa ruộng.
class KnCooperativeDetailScreen extends ConsumerStatefulWidget {
  const KnCooperativeDetailScreen({super.key, required this.ref});
  final String ref;
  @override
  ConsumerState<KnCooperativeDetailScreen> createState() => _CoopDetailState();
}

class _CoopDetailState extends ConsumerState<KnCooperativeDetailScreen> {
  Map<String, Object?>? _c;
  bool _cached = false;
  String? _error;
  final _households = GlobalKey<KnPagedListState>();

  KnCollectionService get _svc => ref.read(knCollectionServiceProvider);

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final (c, cached) = await _svc.cooperative(widget.ref);
      if (mounted) setState(() => (_c = c, _cached = cached, _error = null));
    } on KnCommandError catch (e) {
      if (mounted) setState(() => _error = e.message);
    } on Object {
      if (mounted) {
        setState(() => _error = 'Không tải được — cần kết nối mạng.');
      }
    }
  }

  Future<void> _deactivate() async {
    final reason = await knAskReason(
      context,
      title: 'Vô hiệu hóa HTX',
      body: 'HTX không bị xóa; dữ liệu liên kết được giữ nguyên.',
    );
    if (reason == null || !mounted) return;
    final r = await knRunCommand(
      context,
      () => _svc.deactivateCooperative(widget.ref, reason),
      done: 'Đã vô hiệu hóa HTX.',
    );
    final w = r?.response?['warnings'];
    if (w is Map && mounted) {
      final parts = [
        if ((w['households'] as num? ?? 0) > 0) '${w['households']} nông hộ',
        if ((w['plots'] as num? ?? 0) > 0) '${w['plots']} thửa',
        if ((w['machines'] as num? ?? 0) > 0) '${w['machines']} máy móc',
      ];
      if (parts.isNotEmpty) {
        await showDialog<void>(
          context: context,
          builder: (ctx) => AlertDialog(
            title: const Text('Cảnh báo dữ liệu liên kết'),
            content: Text('HTX còn ${parts.join(', ')} liên kết.'),
            actions: [
              TextButton(
                onPressed: () => Navigator.pop(ctx),
                child: const Text('Đã hiểu'),
              ),
            ],
          ),
        );
      }
    }
    await _load();
  }

  Future<void> _household({Map<String, Object?>? existing}) async {
    final name = TextEditingController();
    final phone = TextEditingController();
    final area = TextEditingController(
      text: existing == null ? '' : '${existing['cultivated_area_ha'] ?? ''}',
    );
    final reason = TextEditingController();
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(
          existing == null ? 'Thêm nông hộ' : 'Sửa diện tích nông hộ',
        ),
        content: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              if (existing == null) ...[
                TextField(
                  key: const ValueKey('kn-hh-name'),
                  controller: name,
                  decoration: const InputDecoration(labelText: 'Chủ hộ *'),
                ),
                TextField(
                  key: const ValueKey('kn-hh-phone'),
                  controller: phone,
                  keyboardType: TextInputType.phone,
                  decoration: const InputDecoration(labelText: 'SĐT *'),
                ),
              ],
              TextField(
                key: const ValueKey('kn-hh-area'),
                controller: area,
                keyboardType: const TextInputType.numberWithOptions(
                  decimal: true,
                ),
                decoration: const InputDecoration(
                  labelText: 'Diện tích canh tác (ha)',
                ),
              ),
              if (existing != null)
                TextField(
                  controller: reason,
                  decoration: const InputDecoration(labelText: 'Lý do sửa'),
                ),
            ],
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Hủy'),
          ),
          FilledButton(
            key: const ValueKey('kn-hh-save'),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Lưu'),
          ),
        ],
      ),
    );
    if (ok == true && mounted) {
      await knRunCommand(
        context,
        () => existing == null
            ? _svc.createHousehold(
                widget.ref,
                fullName: name.text,
                phone: phone.text,
                areaHa: area.text,
              )
            : _svc.updateHousehold(
                '${existing['ref']}',
                areaHa: area.text,
                reason: reason.text,
              ),
        done: existing == null ? 'Đã thêm nông hộ.' : 'Đã lưu.',
      );
      await _households.currentState?.reload();
    }
    for (final c in [name, phone, area, reason]) {
      c.dispose();
    }
  }

  @override
  Widget build(BuildContext context) {
    final c = _c;
    final communes = ref.watch(knCommunesProvider);
    return DefaultTabController(
      length: 3,
      child: Scaffold(
        appBar: AppBar(
          title: Text(c == null ? 'HTX' : '${c['name']}'),
          actions: [
            if (c != null)
              IconButton(
                key: const ValueKey('kn-coop-edit'),
                tooltip: 'Sửa',
                icon: const Icon(Icons.edit),
                onPressed: () async {
                  final ok = await Navigator.of(context).push<bool>(
                    MaterialPageRoute(
                      builder: (_) => KnCooperativeFormScreen(existing: c),
                    ),
                  );
                  if (ok == true) await _load();
                },
              ),
            if (c != null && c['active'] != false)
              IconButton(
                key: const ValueKey('kn-coop-deactivate'),
                tooltip: 'Vô hiệu hóa',
                icon: const Icon(Icons.block),
                onPressed: _deactivate,
              ),
          ],
          bottom: const TabBar(
            tabs: [
              Tab(text: 'Nông hộ'),
              Tab(text: 'Máy móc'),
              Tab(text: 'Thửa ruộng'),
            ],
          ),
        ),
        floatingActionButton: c == null || c['active'] == false
            ? null
            : FloatingActionButton.extended(
                key: const ValueKey('kn-hh-add'),
                onPressed: _household,
                icon: const Icon(Icons.person_add),
                label: const Text('Thêm nông hộ'),
              ),
        body: Column(
          children: [
            if (_cached) const KnOfflineBanner(),
            if (_error != null)
              Padding(padding: const EdgeInsets.all(12), child: Text(_error!)),
            if (c != null)
              ListTile(
                title: Text(
                  '${knCommuneName(communes, c['commune_code'] as String?)} · '
                  '${c['area_ha'] ?? '—'} ha',
                ),
                subtitle: Text(
                  [
                    'Đại diện: ${_rep(c)}',
                    if (c['tax_code'] != null) 'MST: ${c['tax_code']}',
                    if (c['head_office'] != null) 'Trụ sở: ${c['head_office']}',
                    if (c['active'] == false)
                      'Vô hiệu hóa: ${c['status_reason'] ?? ''}',
                  ].join('\n'),
                ),
              ),
            Expanded(
              child: TabBarView(
                children: [
                  KnPagedList(
                    key: _households,
                    empty: 'Chưa có nông hộ.',
                    load: (cur) => _svc.households(widget.ref, cursor: cur),
                    itemBuilder: (ctx, h) {
                      final holder = h['holder'];
                      return ListTile(
                        key: ValueKey('kn-hh-${h['ref']}'),
                        title: Text(
                          holder is Map ? '${holder['label'] ?? ''}' : '—',
                        ),
                        subtitle: Text(
                          '${holder is Map ? holder['masked_phone'] ?? '' : ''}'
                          ' · ${h['cultivated_area_ha'] ?? '—'} ha',
                        ),
                        trailing: IconButton(
                          icon: const Icon(Icons.edit),
                          onPressed: () => _household(existing: h),
                        ),
                      );
                    },
                  ),
                  KnMachineEditor(cooperativeRef: widget.ref, embedded: true),
                  KnPlotsList(cooperativeRef: widget.ref),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}
