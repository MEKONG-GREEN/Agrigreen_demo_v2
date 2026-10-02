import 'dart:async';

import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/kn_report_service.dart';
import '../../domain/kn_report.dart';
import 'kn_providers.dart';

/// Nhập báo cáo theo biểu mẫu NGOẠI TUYẾN (FN-XA-03.02), tối ưu điện thoại:
/// mỗi xã một tab; mỗi dòng của xã là một thẻ liệt kê các cột (chữ cái +
/// tiêu đề + ĐVT), "Đi tới" nhóm cột, tóm tắt cột định danh cố định ở đầu.
/// Chỉ ô nhập và cột của kỳ đang mở sửa được; cột cố định / Tỉnh giao / kỳ
/// khác chỉ xem; ô công thức hiện giá trị máy chủ đã tính hoặc "chưa tính".
class KnReportEntryScreen extends ConsumerStatefulWidget {
  const KnReportEntryScreen({super.key, required this.taskId});
  final String taskId;

  @override
  ConsumerState<KnReportEntryScreen> createState() => _EntryState();
}

class _EntryState extends ConsumerState<KnReportEntryScreen> {
  List<KnReportTask> _tabs = const [];
  int _idx = 0;
  KnEntry? _entry;
  bool _noLayout = false;
  bool _loading = true;
  bool _busy = false;
  bool _dirty = false;
  Map<String, String> _values = {};
  Map<String, String> _errors = {};
  final _ctl = <String, TextEditingController>{};
  KnColumnGroup? _group;
  Timer? _debounce;

  KnReportService get _svc => ref.read(knReportServiceProvider);
  KnReportTask? get _task => _tabs.isEmpty ? null : _tabs[_idx];

  @override
  void initState() {
    super.initState();
    _init();
  }

  @override
  void dispose() {
    _debounce?.cancel();
    final t = _task;
    if (_dirty && t != null) unawaited(_svc.saveWork(t.id, Map.of(_values)));
    for (final c in _ctl.values) {
      c.dispose();
    }
    super.dispose();
  }

  Future<void> _init() async {
    final t = await _svc.task(widget.taskId);
    if (t == null) {
      if (mounted) setState(() => _loading = false);
      return;
    }
    final tabs = await _svc.tabsFor(t);
    _tabs = tabs;
    _idx = tabs.indexWhere((x) => x.id == t.id).clamp(0, tabs.length - 1);
    await _loadTab();
  }

  Future<void> _loadTab() async {
    final t = _task!;
    setState(() => _loading = true);
    final e = await _svc.entry(t);
    if (!mounted) return;
    for (final c in _ctl.values) {
      c.dispose();
    }
    _ctl.clear();
    setState(() {
      _entry = e;
      _noLayout = e == null;
      _values = Map.of(e?.values ?? const {});
      _errors = {};
      _dirty = false;
      _loading = false;
      if (_group != null &&
          !(e?.layout.groups.any((g) => g.id == _group!.id) ?? false)) {
        _group = null;
      }
    });
  }

  /// Tự lưu khi chuyển tab: bản đang nhập trên máy; nếu công việc chưa gửi
  /// thì xếp "Lưu nháp" vào hàng đợi (gửi khi có mạng).
  Future<void> _autosave() async {
    final e = _entry;
    if (!_dirty || e == null) return;
    await _svc.saveWork(e.task.id, Map.of(_values));
    final s = e.task.status;
    if (!e.readOnly &&
        (s == KnTaskStatus.notStarted || s == KnTaskStatus.draft) &&
        _svc.validate(e, _values, submit: false).isEmpty) {
      await _svc.queue(e, _values, submit: false);
      if (mounted) {
        showMessage(context, 'Đã tự lưu nháp ${e.task.communeName}.');
      }
    }
    _dirty = false;
  }

  Future<void> _switch(int i) async {
    if (i == _idx || _busy) return;
    _debounce?.cancel();
    await _autosave();
    setState(() => _idx = i);
    await _loadTab();
  }

  void _changed(String key, String v) {
    _values[key] = v;
    if (!_dirty || _errors.containsKey(key)) {
      setState(() {
        _dirty = true;
        _errors.remove(key);
      });
    }
    final t = _task!;
    _debounce?.cancel();
    _debounce = Timer(const Duration(milliseconds: 600), () {
      unawaited(_svc.saveWork(t.id, Map.of(_values)));
    });
  }

  Future<void> _queue({required bool submit}) async {
    final e = _entry;
    if (e == null) return;
    setState(() => _busy = true);
    try {
      await _svc.queue(e, _values, submit: submit);
      if (!mounted) return;
      showMessage(
        context,
        submit
            ? 'Đã gửi báo cáo ${e.task.communeName} vào hàng đợi — tự đồng bộ khi có mạng.'
            : 'Đã lưu nháp ${e.task.communeName}.',
      );
      await _loadTab();
    } on KnValidationException catch (x) {
      if (!mounted) return;
      setState(() => _errors = x.errors);
      showMessage(context, x.toString(), error: true);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _history() async {
    final t = _task;
    if (t == null) return;
    List<Map<String, Object?>> items;
    try {
      items = await _svc.history(t.id);
    } on Object {
      if (mounted) {
        showMessage(
          context,
          'Cần kết nối mạng để xem lịch sử sửa.',
          error: true,
        );
      }
      return;
    }
    if (!mounted) return;
    await showDialog<void>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('Lịch sử sửa · ${t.communeName}'),
        content: SizedBox(
          width: double.maxFinite,
          child: items.isEmpty
              ? const Text('Chưa có phiên bản nào.')
              : ListView(
                  shrinkWrap: true,
                  children: [
                    for (final i in items)
                      ListTile(
                        dense: true,
                        title: Text(
                          'Phiên bản ${i['version_no'] ?? ''} · '
                          '${i['kind'] == 'submitted' ? 'Đã gửi' : 'Lưu nháp'}',
                        ),
                        subtitle: Text(
                          '${i['actor_display'] ?? ''} · '
                          '${knDate(DateTime.tryParse('${i['recorded_at']}'))}',
                        ),
                      ),
                  ],
                ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: const Text('Đóng'),
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final e = _entry;
    final t = _task;
    return Scaffold(
      appBar: AppBar(
        title: Text(
          t == null ? 'Nhập báo cáo' : '${t.formCode} · ${t.formName}',
        ),
        actions: [
          IconButton(
            key: const ValueKey('kn-history'),
            tooltip: 'Lịch sử sửa',
            icon: const Icon(Icons.history),
            onPressed: t == null ? null : _history,
          ),
        ],
      ),
      body: t == null && !_loading
          ? const Center(child: Text('Không tìm thấy công việc trên máy.'))
          : Column(
              children: [
                if (_tabs.length > 1) _tabBar(),
                if (_loading)
                  const Expanded(
                    child: Center(child: CircularProgressIndicator()),
                  )
                else if (_noLayout)
                  const Expanded(
                    child: Center(
                      child: Padding(
                        padding: EdgeInsets.all(24),
                        child: Text(
                          'Máy chưa có bố cục biểu mẫu. Kết nối mạng và đồng bộ.',
                          textAlign: TextAlign.center,
                        ),
                      ),
                    ),
                  )
                else
                  Expanded(child: _body(e!)),
                if (e != null && !e.readOnly && !_loading) _actions(),
              ],
            ),
    );
  }

  // Ít tab (số xã phụ trách) → dựng hết trong một hàng cuộn ngang.
  Widget _tabBar() => SingleChildScrollView(
    scrollDirection: Axis.horizontal,
    padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 8),
    child: Row(
      children: [
        for (var i = 0; i < _tabs.length; i++)
          Padding(
            padding: const EdgeInsets.only(right: 6),
            child: ChoiceChip(
              key: ValueKey('kn-tab-${_tabs[i].communeCode}'),
              selected: i == _idx,
              label: Text(
                '${_tabs[i].communeName} · '
                '${KnTaskStatus.label(_tabs[i].displayAt(_svc.now))}',
              ),
              onSelected: (_) => _switch(i),
            ),
          ),
      ],
    ),
  );

  Widget _actions() => SafeArea(
    child: Padding(
      padding: const EdgeInsets.fromLTRB(12, 4, 12, 8),
      child: Row(
        children: [
          Expanded(
            child: OutlinedButton.icon(
              key: const ValueKey('kn-save-draft'),
              onPressed: _busy ? null : () => _queue(submit: false),
              icon: const Icon(Icons.save_outlined),
              label: const Text('Lưu nháp'),
            ),
          ),
          const SizedBox(width: 8),
          Expanded(
            child: FilledButton.icon(
              key: const ValueKey('kn-submit'),
              onPressed: _busy ? null : () => _queue(submit: true),
              icon: const Icon(Icons.send),
              label: const Text('Gửi báo cáo'),
            ),
          ),
        ],
      ),
    ),
  );

  Widget _body(KnEntry e) {
    final cols = [
      for (final c in e.layout.columnsOf(_group))
        if (!c.inactive && !e.layout.identifying.contains(c)) c,
    ];
    return ListView.builder(
      // Dòng 0 = đầu biểu; sau đó mỗi dòng của xã một thẻ.
      itemCount: e.rows.length + 1,
      itemBuilder: (_, i) =>
          i == 0 ? _header(e) : _rowCard(e, e.rows[i - 1], cols),
    );
  }

  Widget _header(KnEntry e) {
    final t = e.task;
    final l = e.layout;
    final ident = [
      for (final c in l.identifying)
        if ((e.displayValue(e.rows.first, c) ?? '').isNotEmpty)
          '${c.title.isEmpty ? c.letter : c.title}: ${e.displayValue(e.rows.first, c)}',
    ];
    final generalErr = e.sync.fieldErrors['_'];
    return Padding(
      padding: const EdgeInsets.fromLTRB(12, 8, 12, 4),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            l.title.isEmpty ? l.name : l.title,
            style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w700),
          ),
          Text(
            [
              if (t.seasonLabel.isNotEmpty) 'Vụ ${t.seasonLabel}',
              if (t.periodLabel.isNotEmpty) t.periodLabel,
              'Hạn nộp ${knDate(t.dueAt)}',
            ].join(' · '),
          ),
          if (l.sheetName.isNotEmpty)
            Text(
              'Sheet mẫu: ${l.sheetName}',
              style: const TextStyle(color: AgColors.mute, fontSize: 13),
            ),
          if (ident.isNotEmpty)
            Container(
              margin: const EdgeInsets.only(top: 6),
              padding: const EdgeInsets.all(8),
              color: Colors.grey.shade200,
              child: Text(ident.join(' · ')),
            ),
          if (e.readOnly)
            Container(
              key: const ValueKey('kn-closed'),
              margin: const EdgeInsets.only(top: 6),
              padding: const EdgeInsets.all(8),
              color: Colors.grey.shade300,
              child: const Text('Kỳ đã chốt — chỉ xem.'),
            ),
          if (t.adjustmentReason != null &&
              t.status == KnTaskStatus.adjustmentRequested)
            Padding(
              padding: const EdgeInsets.only(top: 6),
              child: Text(
                'Yêu cầu điều chỉnh: ${t.adjustmentReason}',
                style: const TextStyle(color: AgColors.amber),
              ),
            ),
          if (e.sync.state != KnSyncState.none)
            Padding(
              key: const ValueKey('kn-sync-status'),
              padding: const EdgeInsets.only(top: 6),
              child: Text(
                e.sync.label,
                style: TextStyle(
                  color: knSyncColor(e.sync.state),
                  fontWeight: FontWeight.w600,
                ),
              ),
            ),
          if (generalErr != null)
            Text(generalErr, style: const TextStyle(color: AgColors.red)),
          if (l.requirements.isNotEmpty)
            ExpansionTile(
              tilePadding: EdgeInsets.zero,
              title: const Text('Yêu cầu biểu mẫu'),
              children: [Text(l.requirements)],
            ),
          if (l.groups.isNotEmpty)
            DropdownButtonFormField<String?>(
              isExpanded: true,
              key: const ValueKey('kn-goto'),
              initialValue: _group?.id,
              decoration: const InputDecoration(labelText: 'Đi tới nhóm cột'),
              items: [
                const DropdownMenuItem<String?>(
                  value: null,
                  child: Text('Tất cả cột'),
                ),
                for (final g in l.groups)
                  DropdownMenuItem<String?>(
                    value: g.id,
                    child: Text('${g.title} (${g.from}–${g.to})'),
                  ),
              ],
              onChanged: (v) => setState(
                () => _group = l.groups.where((g) => g.id == v).firstOrNull,
              ),
            ),
        ],
      ),
    );
  }

  Widget _rowCard(KnEntry e, KnRow r, List<KnColumn> cols) {
    return Card(
      margin: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(
              '${r.index}. ${r.label}',
              style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w700),
            ),
            for (final c in cols) _cell(e, r, c),
          ],
        ),
      ),
    );
  }

  Widget _cell(KnEntry e, KnRow r, KnColumn c) {
    final key = knCellKey(r.key, c.letter);
    final label =
        '${c.letter} · ${c.title}${c.unit == null ? '' : ' (${c.unit})'}';
    final err = _errors[key] ?? e.errorAt(r, c);
    if (e.editable(c)) {
      final ctl = _ctl.putIfAbsent(
        key,
        () => TextEditingController(text: _values[key] ?? ''),
      );
      final open = e.role(c) == KnCellRole.openPeriod;
      return Padding(
        padding: const EdgeInsets.only(top: 8),
        child: TextField(
          key: ValueKey('cell-${r.key}-${c.letter}'),
          controller: ctl,
          keyboardType: c.isNumeric
              ? const TextInputType.numberWithOptions(decimal: true)
              : TextInputType.text,
          decoration: InputDecoration(
            labelText: c.required ? '$label *' : label,
            errorText: err,
            isDense: true,
            enabledBorder: open
                ? const OutlineInputBorder(
                    borderSide: BorderSide(color: AgColors.g900, width: 2),
                  )
                : null,
          ),
          onChanged: (v) => _changed(key, v),
        ),
      );
    }
    final role = e.role(c);
    final v = e.displayValue(r, c);
    final color = switch (role) {
      KnCellRole.formula => AgColors.g100,
      KnCellRole.otherPeriod => Colors.grey.shade100,
      _ => Colors.grey.shade300,
    };
    final shown = role == KnCellRole.formula && (v == null || v.isEmpty)
        ? 'chưa tính'
        : (v == null || v.isEmpty ? '—' : v);
    return Container(
      key: ValueKey('ro-${r.key}-${c.letter}'),
      margin: const EdgeInsets.only(top: 8),
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
      decoration: BoxDecoration(
        color: color,
        borderRadius: BorderRadius.circular(6),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            label,
            style: const TextStyle(fontSize: 12, color: AgColors.mute),
          ),
          Text(
            shown,
            style: TextStyle(
              fontSize: 16,
              fontStyle: shown == 'chưa tính' ? FontStyle.italic : null,
            ),
          ),
          if (err != null)
            Text(
              err,
              style: const TextStyle(color: AgColors.red, fontSize: 12),
            ),
        ],
      ),
    );
  }
}
