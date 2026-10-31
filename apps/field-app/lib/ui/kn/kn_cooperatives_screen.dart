import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/kn_collection_service.dart';
import 'kn_common.dart';
import 'kn_machines_screen.dart';
import 'kn_plots_screen.dart';
import 'kn_providers.dart';

// FN-XA-03.03 HTX / Tổ hợp tác / Nông hộ — KNSPLIT-2-KN-RECORDS (#67, #161):
// mọi màn chạy trên hồ sơ KN riêng `/v1/kn/producer-records`. KN không
// tạo/sửa HTX nền tảng; HTX đã công bố chỉ để liên kết đối chiếu (`htx_ref`),
// máy móc và thửa ruộng đọc theo `htx_ref`.

String _person(Map<String, Object?> c, String key) {
  final r = c[key];
  if (r is! Map) return '—';
  final s = [r['label'], r['masked_phone']].whereType<String>().join(' · ');
  return s.isEmpty ? '—' : s;
}

String _kindLabel(String kind) =>
    kind == KnRecordKind.coopGroup ? 'Tổ hợp tác' : 'HTX';

bool _inactive(Map<String, Object?> c) => c['status'] == 'inactive';

/// Danh sách HTX / Tổ hợp tác (hồ sơ KN): lọc loại, xã, trạng thái, tìm tên;
/// SĐT người đại diện chỉ hiển thị dạng che (máy chủ trả `masked_phone`).
class KnCooperativesScreen extends ConsumerStatefulWidget {
  const KnCooperativesScreen({super.key});
  @override
  ConsumerState<KnCooperativesScreen> createState() => _CoopsState();
}

class _CoopsState extends ConsumerState<KnCooperativesScreen> {
  final _list = GlobalKey<KnPagedListState>();
  final _q = TextEditingController();
  String _kind = KnRecordKind.cooperative;
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
    final label = _kindLabel(_kind);
    return Scaffold(
      appBar: AppBar(title: Text('Danh sách $label')),
      floatingActionButton: FloatingActionButton.extended(
        key: const ValueKey('kn-coop-add'),
        onPressed: () async {
          final ok = await Navigator.of(context).push<bool>(
            MaterialPageRoute(
              builder: (_) => KnCooperativeFormScreen(kind: _kind),
            ),
          );
          if (ok == true) await _list.currentState?.reload();
        },
        icon: const Icon(Icons.add),
        label: Text('Thêm $label'),
      ),
      body: KnPagedList(
        key: _list,
        empty: 'Chưa có $label trong các xã phụ trách.',
        header: Padding(
          padding: const EdgeInsets.all(12),
          child: Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              SizedBox(
                width: 150,
                child: DropdownButtonFormField<String>(
                  key: const ValueKey('kn-coop-kind'),
                  isExpanded: true,
                  initialValue: _kind,
                  decoration: const InputDecoration(
                    labelText: 'Loại',
                    isDense: true,
                  ),
                  items: const [
                    DropdownMenuItem(
                      value: KnRecordKind.cooperative,
                      child: Text('HTX'),
                    ),
                    DropdownMenuItem(
                      value: KnRecordKind.coopGroup,
                      child: Text('Tổ hợp tác'),
                    ),
                  ],
                  onChanged: (v) {
                    setState(() => _kind = v ?? KnRecordKind.cooperative);
                    _list.currentState?.reload();
                  },
                ),
              ),
              SizedBox(
                width: 220,
                child: TextField(
                  key: const ValueKey('kn-coop-q'),
                  controller: _q,
                  decoration: const InputDecoration(
                    labelText: 'Tìm tên',
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
        load: (cursor) => svc.records(
          kind: _kind,
          commune: _commune,
          status: _status,
          q: _q.text,
          cursor: cursor,
        ),
        itemBuilder: (ctx, c) => Card(
          margin: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
          child: ListTile(
            key: ValueKey('kn-coop-${c['ref']}'),
            title: Text('${c['display_name'] ?? c['name'] ?? '—'}'),
            subtitle: Text(
              [
                knCommuneName(communes, c['commune_code'] as String?),
                'Đại diện: ${_person(c, 'representative')}',
                'Diện tích: ${c['area_ha'] ?? '—'} ha · ${c['member_count'] ?? 0} nông hộ',
              ].join('\n'),
            ),
            isThreeLine: true,
            trailing: _inactive(c)
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

/// Thêm / sửa hồ sơ HTX hoặc Tổ hợp tác. Sửa gửi đủ trường (máy chủ thay
/// toàn bộ) kèm `base_version` của bản đang xem.
class KnCooperativeFormScreen extends ConsumerStatefulWidget {
  const KnCooperativeFormScreen({
    super.key,
    this.existing,
    this.kind = KnRecordKind.cooperative,
  });
  final Map<String, Object?>? existing;

  /// Loại khi thêm mới (sửa lấy theo [existing]).
  final String kind;
  @override
  ConsumerState<KnCooperativeFormScreen> createState() => _CoopFormState();
}

class _CoopFormState extends ConsumerState<KnCooperativeFormScreen> {
  late final _name = TextEditingController(text: _s('name'));
  late final _area = TextEditingController(text: _s('area_ha'));
  late final _tax = TextEditingController(text: _s('tax_code'));
  late final _address = TextEditingController(text: _s('address'));
  final _repName = TextEditingController();
  final _repPhone = TextEditingController();
  String? _commune;
  String? _htxRef;
  List<Map<String, Object?>>? _candidates;
  String? _candidatesFor;
  bool _busy = false;

  String get _kind => (widget.existing?['kind'] as String?) ?? widget.kind;

  String _s(String k) {
    final v = widget.existing?[k];
    return v == null ? '' : '$v';
  }

  @override
  void initState() {
    super.initState();
    _commune = widget.existing?['commune_code'] as String?;
    _htxRef = widget.existing?['htx_ref'] as String?;
  }

  @override
  void dispose() {
    for (final c in [_name, _area, _tax, _address, _repName, _repPhone]) {
      c.dispose();
    }
    super.dispose();
  }

  /// Danh sách HTX đã công bố của xã (liên kết đối chiếu) — cần mạng; lỗi thì
  /// bỏ qua ô chọn (liên kết là tùy chọn).
  Future<void> _loadCandidates(String commune) async {
    if (_kind != KnRecordKind.cooperative || _candidatesFor == commune) return;
    _candidatesFor = commune;
    List<Map<String, Object?>>? items;
    try {
      items = await ref
          .read(knCollectionServiceProvider)
          .htxCandidates(commune);
    } on Object {
      items = null;
    }
    if (!mounted || _candidatesFor != commune) return;
    setState(() => _candidates = items);
  }

  Future<void> _save() async {
    final svc = ref.read(knCollectionServiceProvider);
    final communes = ref.read(knCommunesProvider);
    final commune =
        _commune ?? (communes.length == 1 ? communes.first.code : null);
    final label = _kindLabel(_kind);
    if (_name.text.trim().length < 3) {
      _msg('Tên $label tối thiểu 3 ký tự.');
      return;
    }
    if (widget.existing == null && commune == null) {
      _msg('Chọn xã của $label.');
      return;
    }
    setState(() => _busy = true);
    final ex = widget.existing;
    final r = await knRunCommand(
      context,
      () => ex == null
          ? svc.createRecord(
              kind: _kind,
              communeCode: commune!,
              name: _name.text,
              areaHa: _area.text,
              taxCode: _tax.text,
              address: _address.text,
              personName: _repName.text,
              personPhone: _repPhone.text,
              htxRef: _htxRef,
            )
          : svc.updateRecord(
              ex,
              name: _name.text,
              areaHa: _area.text,
              taxCode: _tax.text,
              address: _address.text,
              personName: _repName.text,
              personPhone: _repPhone.text,
              htxRef: _htxRef ?? '',
            ),
      done: ex == null ? 'Đã thêm $label.' : 'Đã lưu thay đổi.',
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
    final label = _kindLabel(_kind);
    final coop = _kind == KnRecordKind.cooperative;
    final commune =
        _commune ?? (communes.length == 1 ? communes.first.code : null);
    if (coop && commune != null) _loadCandidates(commune);
    final cands = _candidates ?? const <Map<String, Object?>>[];
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
      appBar: AppBar(title: Text(edit ? 'Sửa $label' : 'Thêm $label')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          f('kn-coop-f-name', _name, 'Tên $label *'),
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
                onChanged: (v) => setState(() {
                  _commune = v;
                  _htxRef = null;
                }),
              ),
            ),
          f(
            'kn-coop-f-area',
            _area,
            'Diện tích (ha)',
            kb: const TextInputType.numberWithOptions(decimal: true),
          ),
          if (coop) f('kn-coop-f-tax', _tax, 'Mã số thuế'),
          f('kn-coop-f-address', _address, 'Địa chỉ'),
          f(
            'kn-coop-f-rep',
            _repName,
            edit
                ? 'Người đại diện mới (để trống = giữ nguyên)'
                : 'Người đại diện *',
          ),
          f(
            'kn-coop-f-phone',
            _repPhone,
            edit ? 'SĐT người đại diện mới' : 'SĐT người đại diện *',
            kb: TextInputType.phone,
          ),
          if (coop && (cands.isNotEmpty || _htxRef != null))
            Padding(
              padding: const EdgeInsets.only(bottom: 8),
              child: DropdownButtonFormField<String?>(
                key: const ValueKey('kn-coop-f-htx'),
                isExpanded: true,
                initialValue: _htxRef,
                decoration: const InputDecoration(
                  labelText: 'Liên kết HTX đã công bố (tùy chọn)',
                ),
                items: [
                  const DropdownMenuItem<String?>(
                    value: null,
                    child: Text('Không liên kết'),
                  ),
                  if (_htxRef != null &&
                      !cands.any((c) => c['htx_ref'] == _htxRef))
                    DropdownMenuItem<String?>(
                      value: _htxRef,
                      child: Text(
                        '${widget.existing?['htx_name'] ?? 'HTX đã liên kết'}',
                      ),
                    ),
                  for (final c in cands)
                    if (c['linked_ref'] == null ||
                        c['linked_ref'] == widget.existing?['ref'])
                      DropdownMenuItem<String?>(
                        value: c['htx_ref'] as String?,
                        child: Text('${c['name']}'),
                      ),
                ],
                onChanged: (v) => setState(() => _htxRef = v),
              ),
            ),
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

/// Chi tiết hồ sơ HTX / Tổ hợp tác: thông tin chung, Sửa / Thêm nông hộ /
/// Vô hiệu hóa / Kích hoạt lại; tab Nông hộ (+ Máy móc / Thửa ruộng của HTX
/// đã công bố được liên kết, chỉ với HTX).
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
      final (c, cached) = await _svc.record(widget.ref);
      if (mounted) setState(() => (_c = c, _cached = cached, _error = null));
    } on KnCommandError catch (e) {
      if (mounted) setState(() => _error = e.message);
    } on Object {
      if (mounted) {
        setState(() => _error = 'Không tải được — cần kết nối mạng.');
      }
    }
  }

  /// Vô hiệu hóa (không xóa) / kích hoạt lại [rec] — lý do ≥ 10 ký tự.
  Future<bool> _status(Map<String, Object?> rec, String what) async {
    final off = !_inactive(rec);
    final reason = await knAskReason(
      context,
      title: off ? 'Vô hiệu hóa $what' : 'Kích hoạt lại $what',
      body: off
          ? 'Hồ sơ không bị xóa; dữ liệu liên kết được giữ nguyên.'
          : null,
      min: kReasonMin,
    );
    if (reason == null || !mounted) return false;
    final r = await knRunCommand(
      context,
      () => _svc.setRecordStatus(rec, deactivate: off, reason: reason),
      done: off ? 'Đã vô hiệu hóa (không xóa).' : 'Đã kích hoạt lại.',
    );
    return r != null;
  }

  Future<void> _household({Map<String, Object?>? existing}) async {
    final c = _c;
    if (c == null) return;
    final name = TextEditingController();
    final phone = TextEditingController();
    final area = TextEditingController(
      text: existing == null ? '' : '${existing['area_ha'] ?? ''}',
    );
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
            ? _svc.createRecord(
                kind: KnRecordKind.household,
                communeCode: '${c['commune_code']}',
                parentRef: widget.ref,
                personName: name.text,
                personPhone: phone.text,
                areaHa: area.text,
              )
            : _svc.updateRecord(existing, areaHa: area.text),
        done: existing == null ? 'Đã thêm nông hộ.' : 'Đã lưu.',
      );
      await _households.currentState?.reload();
    }
    for (final c in [name, phone, area]) {
      c.dispose();
    }
  }

  @override
  Widget build(BuildContext context) {
    final c = _c;
    final communes = ref.watch(knCommunesProvider);
    final kind = (c?['kind'] as String?) ?? KnRecordKind.cooperative;
    final label = _kindLabel(kind);
    final coop = kind == KnRecordKind.cooperative;
    final htxRef = c?['htx_ref'] as String?;
    final inactive = c != null && _inactive(c);
    Widget noHtx() => const Padding(
      padding: EdgeInsets.all(16),
      child: Text(
        'Chưa liên kết HTX đã công bố — máy móc, thửa ruộng theo HTX đã '
        'công bố. Sửa hồ sơ để chọn liên kết.',
      ),
    );
    return DefaultTabController(
      length: coop ? 3 : 1,
      child: Scaffold(
        appBar: AppBar(
          title: Text(c == null ? label : '${c['display_name'] ?? c['name']}'),
          actions: [
            if (c != null && !inactive && c['can_edit'] != false)
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
            if (c != null && c['can_edit'] != false)
              IconButton(
                key: ValueKey(
                  inactive ? 'kn-coop-reactivate' : 'kn-coop-deactivate',
                ),
                tooltip: inactive ? 'Kích hoạt lại' : 'Vô hiệu hóa',
                icon: Icon(inactive ? Icons.restore : Icons.block),
                onPressed: () async {
                  if (await _status(c, label)) await _load();
                },
              ),
          ],
          bottom: TabBar(
            tabs: [
              const Tab(text: 'Nông hộ'),
              if (coop) ...const [
                Tab(text: 'Máy móc'),
                Tab(text: 'Thửa ruộng'),
              ],
            ],
          ),
        ),
        floatingActionButton: c == null || inactive
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
                    'Đại diện: ${_person(c, 'representative')}',
                    if (c['tax_code'] != null) 'MST: ${c['tax_code']}',
                    if (c['address'] != null) 'Địa chỉ: ${c['address']}',
                    if (coop)
                      'HTX đã công bố: ${c['htx_name'] ?? 'chưa liên kết'}',
                    if (inactive) 'Vô hiệu hóa: ${c['status_reason'] ?? ''}',
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
                    itemBuilder: (ctx, h) => ListTile(
                      key: ValueKey('kn-hh-${h['ref']}'),
                      title: Text('${h['display_name'] ?? '—'}'),
                      subtitle: Text(
                        '${_person(h, 'holder')} · ${h['area_ha'] ?? '—'} ha',
                      ),
                      trailing: h['can_edit'] == false
                          ? null
                          : PopupMenuButton<String>(
                              key: ValueKey('kn-hh-menu-${h['ref']}'),
                              onSelected: (v) async {
                                if (v == 'edit') {
                                  await _household(existing: h);
                                } else if (await _status(h, 'nông hộ')) {
                                  await _households.currentState?.reload();
                                }
                              },
                              itemBuilder: (_) => const [
                                PopupMenuItem(
                                  value: 'edit',
                                  child: Text('Sửa diện tích'),
                                ),
                                PopupMenuItem(
                                  value: 'deactivate',
                                  child: Text('Vô hiệu hóa'),
                                ),
                              ],
                            ),
                    ),
                  ),
                  if (coop)
                    htxRef == null
                        ? noHtx()
                        : KnMachineEditor(
                            cooperativeRef: htxRef,
                            embedded: true,
                          ),
                  if (coop)
                    htxRef == null
                        ? noHtx()
                        : KnPlotsList(cooperativeRef: htxRef),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}
