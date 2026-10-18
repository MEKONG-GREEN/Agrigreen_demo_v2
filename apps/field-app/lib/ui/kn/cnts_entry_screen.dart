import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/cnts_period_service.dart';
import '../../domain/cnts_period.dart';
import 'cnts_tasks_screen.dart';
import 'kn_providers.dart';

/// Màn 2 — nhập một công việc biểu kỳ CNTS (BM-01 theo cơ sở/ngày, BM-04,
/// BM-17 theo xã/tuần, BM-05 kết quả/kế hoạch tuần) ngoại tuyến: trường vẽ
/// theo định nghĩa biểu đã pull, tự lưu bản đang nhập trên máy, "Sao chép kỳ
/// trước", "Báo không phát sinh", ghi chú (H08), lý do trễ (H09) bắt buộc khi
/// quá hạn. "Gửi báo cáo" xếp vào hàng đợi; kỳ đã chốt lúc đồng bộ → máy chủ
/// giữ "Chờ mở kỳ"; phiên bản đã đổi ở nơi khác → báo tải bản mới.
class CntsEntryScreen extends ConsumerStatefulWidget {
  const CntsEntryScreen({super.key, required this.taskId});
  final String taskId;

  @override
  ConsumerState<CntsEntryScreen> createState() => _CntsEntryScreenState();
}

class _CntsEntryScreenState extends ConsumerState<CntsEntryScreen> {
  FormsTask? _task;
  FormsWork? _work;
  FormsRecordStatus? _sync;
  Map<String, String> _errs = {};
  final _ctl = <String, TextEditingController>{};
  bool _busy = false;

  CntsPeriodService get _svc => ref.read(cntsPeriodServiceProvider);

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    for (final c in _ctl.values) {
      c.dispose();
    }
    super.dispose();
  }

  Future<void> _load() async {
    final t = await _svc.task(widget.taskId);
    if (t == null) {
      if (mounted) Navigator.of(context).pop();
      return;
    }
    final w = await _svc.work(t);
    final s = await _svc.recordStatus(t.id);
    if (!mounted) return;
    setState(() {
      _task = t;
      _work = w;
      _sync = s;
      _errs = {...s.fieldErrors};
      for (final c in _ctl.values) {
        c.dispose();
      }
      _ctl
        ..clear()
        ..addAll({
          for (final f in t.fields)
            if (f.type != 'choice')
              f.key: TextEditingController(text: w.values[f.key] ?? ''),
          '_note': TextEditingController(text: w.note),
          '_explanation': TextEditingController(text: w.explanation),
        });
    });
  }

  void _changed() {
    final t = _task, w = _work;
    if (t == null || w == null) return;
    for (final e in _ctl.entries) {
      if (e.key == '_note') {
        w.note = e.value.text;
      } else if (e.key == '_explanation') {
        w.explanation = e.value.text;
      } else {
        w.values[e.key] = e.value.text;
      }
    }
    _svc.saveWork(t, w);
  }

  Future<void> _copyPrevious() async {
    final t = _task!;
    final p = await _svc.previous(t);
    if (!mounted) return;
    if (p == null) {
      showMessage(context, 'Chưa có kỳ trước trên máy để sao chép.');
      return;
    }
    setState(() {
      _work!.values = {...p.values};
      _work!.nilReport = p.nilReport;
      for (final f in t.fields) {
        _ctl[f.key]?.text = p.values[f.key] ?? '';
      }
    });
    _changed();
    showMessage(context, 'Đã sao chép từ ${p.label}.');
  }

  Future<void> _submit() async {
    _changed();
    setState(() => _busy = true);
    try {
      await _svc.submit(_task!, _work!);
      if (!mounted) return;
      showMessage(context, 'Đã xếp gửi — tự đồng bộ khi có mạng.');
      Navigator.of(context).pop();
    } on FormsValidationException catch (e) {
      if (!mounted) return;
      setState(() => _errs = e.errors);
      showMessage(context, e.toString(), error: true);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Widget _field(FormsField f, bool readOnly) {
    final w = _work!;
    final locked = readOnly || (w.nilReport && f.numeric);
    final label =
        '${f.label}${f.unit != null ? ' (${f.unit})' : ''}${f.required ? ' *' : ''}';
    if (f.type == 'choice') {
      return DropdownButtonFormField<String>(
        key: Key('cnts-field-${f.key}'),
        initialValue: f.options.contains(w.values[f.key])
            ? w.values[f.key]
            : null,
        isExpanded: true,
        decoration: InputDecoration(labelText: label, errorText: _errs[f.key]),
        items: [
          for (final o in f.options)
            DropdownMenuItem(
              value: o,
              child: Text(o, overflow: TextOverflow.ellipsis),
            ),
        ],
        onChanged: locked
            ? null
            : (v) {
                setState(() => w.values[f.key] = v ?? '');
                _svc.saveWork(_task!, w);
              },
      );
    }
    final c = _ctl[f.key]!;
    if (w.nilReport && f.numeric) c.text = '0';
    return TextField(
      key: Key('cnts-field-${f.key}'),
      controller: c,
      enabled: !locked,
      keyboardType: f.numeric
          ? const TextInputType.numberWithOptions(decimal: true, signed: false)
          : TextInputType.multiline,
      maxLines: f.numeric ? 1 : null,
      decoration: InputDecoration(labelText: label, errorText: _errs[f.key]),
      onChanged: (_) => _changed(),
    );
  }

  @override
  Widget build(BuildContext context) {
    final t = _task, w = _work;
    if (t == null || w == null) {
      return const Scaffold(body: Center(child: CircularProgressIndicator()));
    }
    final now = _svc.now;
    final status = t.statusAt(now);
    final readOnly = t.periodStatus == 'cancelled';
    final nilLabel = CntsForms.nilLabel(t.templateCode);
    final late = t.isLateAt(now);
    final groups = <String, List<FormsField>>{};
    for (final f in t.fields) {
      groups.putIfAbsent(f.group, () => []).add(f);
    }
    return Scaffold(
      appBar: AppBar(title: Text(CntsForms.short(t.templateCode))),
      body: ListView(
        padding: const EdgeInsets.all(12),
        children: [
          Text(t.templateTitle, style: Theme.of(context).textTheme.titleMedium),
          Text('${t.label} · hạn ${knDate(t.dueAt)}'),
          const SizedBox(height: 6),
          Wrap(
            spacing: 6,
            children: [
              KnChip(FormsStatus.label(status), cntsStatusColor(status)),
              if (t.version > 0)
                KnChip('Phiên bản ${t.version}', AgColors.mute),
              if (_sync != null && _sync!.state != FormsSyncState.none)
                KnChip(_sync!.label, AgColors.amber),
            ],
          ),
          if (!t.open && t.periodStatus != 'cancelled')
            const KnOfflineBanner(
              text: 'Kỳ đã chốt — bản gửi lúc này được giữ "Chờ mở kỳ" và báo Cấp Tỉnh.',
            ),
          if (t.returnNote != null)
            KnOfflineBanner(text: 'Yêu cầu điều chỉnh: ${t.returnNote}'),
          if (_sync?.state == FormsSyncState.conflict)
            const KnOfflineBanner(
              text: 'Đã có người cập nhật báo cáo này — kéo đồng bộ để tải bản mới rồi sửa lại (không ghi đè).',
            ),
          if (!readOnly)
            Wrap(
              spacing: 8,
              children: [
                OutlinedButton.icon(
                  key: const Key('cnts-copy-previous'),
                  onPressed: _copyPrevious,
                  icon: const Icon(Icons.copy_all_outlined),
                  label: const Text('Sao chép kỳ trước'),
                ),
                if (nilLabel != null)
                  FilterChip(
                    key: const Key('cnts-nil'),
                    label: Text(nilLabel),
                    selected: w.nilReport,
                    onSelected: (v) {
                      setState(() => w.nilReport = v);
                      _svc.saveWork(t, w);
                    },
                  ),
              ],
            ),
          for (final g in groups.entries) ...[
            if (g.key.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 12),
                child: Text(
                  g.key,
                  style: const TextStyle(fontWeight: FontWeight.w600),
                ),
              ),
            for (final f in g.value)
              Padding(
                padding: const EdgeInsets.only(top: 6),
                child: _field(f, readOnly),
              ),
          ],
          const SizedBox(height: 12),
          TextField(
            key: const Key('cnts-note'),
            controller: _ctl['_note'],
            enabled: !readOnly,
            maxLength: 1000,
            decoration: const InputDecoration(labelText: 'H08 Ghi chú chung'),
            onChanged: (_) => _changed(),
          ),
          TextField(
            key: const Key('cnts-explanation'),
            controller: _ctl['_explanation'],
            enabled: !readOnly,
            maxLength: 1000,
            decoration: InputDecoration(
              labelText: 'H09 Giải trình / lý do trễ${late ? ' *' : ''}',
              errorText: _errs['explanation'],
            ),
            onChanged: (_) => _changed(),
          ),
          const SizedBox(height: 12),
          if (!readOnly)
            FilledButton(
              key: const Key('cnts-submit'),
              onPressed: _busy ? null : _submit,
              child: const Text('Gửi báo cáo'),
            ),
        ],
      ),
    );
  }
}
