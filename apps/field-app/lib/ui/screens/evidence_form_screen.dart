import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/evidence_service.dart';
import '../../data/catalog/catalog_cache.dart';
import '../../data/files/evidence_repository.dart';
import '../../data/photo/photo_pipeline.dart';
import '../../data/reference/reference_repository.dart';
import '../../domain/consent.dart';
import '../../domain/field_report.dart' show normalizeDecimal;
import '../../domain/mrv.dart';
import '../providers.dart';
import '../theme.dart';
import '../widgets/common.dart';
import 'consent_screen.dart';
import 'plots_screen.dart' show plotLabel;

class _FormData {
  const _FormData(this.catalogs, this.plots, this.seasons);
  final List<MrvCatalog> catalogs;
  final List<RefRow> plots;
  final List<CatalogEntry> seasons;
}

final _formDataProvider = FutureProvider.autoDispose<_FormData>((ref) async {
  final s = servicesOf(ref);
  return _FormData(
    await s.mrv.catalogs(),
    await s.refs.list(RefEntity.plot),
    await s.catalogs.entries('season'),
  );
});

/// Thu minh chứng MRV: biểu mẫu dựng từ phiên bản danh mục đã duyệt (ghim vào
/// bản ghi), ảnh mã hóa ngay, bắt buộc đồng ý `mrv.evidence` của hộ.
class EvidenceFormScreen extends ConsumerStatefulWidget {
  const EvidenceFormScreen({super.key});

  @override
  ConsumerState<EvidenceFormScreen> createState() => _EvidenceFormState();
}

class _EvidenceFormState extends ConsumerState<EvidenceFormScreen> {
  late final String _formId = ref
      .read(appControllerProvider)
      .services!
      .mrv
      .newFormId();
  MrvCatalog? _cat;
  RefRow? _plot;
  String? _season;
  MrvGps? _gps;
  String? _block;
  final _values = <String, Object>{};
  final _text = <String, TextEditingController>{};
  List<EvidenceFile> _photos = const [];
  List<MrvFieldError> _errors = const [];
  bool _busy = false;

  String? get _household => _plot?.data['household_ref'] as String?;

  @override
  void dispose() {
    for (final c in _text.values) {
      c.dispose();
    }
    super.dispose();
  }

  TextEditingController _ctl(String key) =>
      _text.putIfAbsent(key, TextEditingController.new);

  Future<void> _selectPlot(RefRow p) async {
    setState(() => _plot = p);
    final hh = _household;
    final r = hh == null
        ? 'Thửa chưa gắn nông hộ — không chụp được ảnh (cần khóa công khai của hộ).'
        : await ref.services.mrv.blockReason(hh);
    if (mounted) setState(() => _block = r);
  }

  Future<void> _capture() async {
    final hh = _household;
    if (hh == null) return;
    setState(() => _busy = true);
    try {
      await ref.services.mrv.capturePhoto(formId: _formId, householdRef: hh);
      final list = await ref.services.mrv.photosOf(_formId);
      _gps ??= await ref.services.mrv.currentGps();
      if (mounted) setState(() => _photos = list);
    } on PhotoPipelineException catch (e) {
      if (mounted) showMessage(context, e.message, error: true);
    } on ConsentRequiredException catch (e) {
      if (mounted) showMessage(context, e.message, error: true);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  MrvEvidenceDraft? _draft() {
    final c = _cat;
    final p = _plot;
    if (c == null || p == null) return null;
    final values = <String, Object>{};
    for (final f in c.fields.where((f) => !f.reserved)) {
      switch (f.type) {
        case MrvDataType.decimal:
          final v = normalizeDecimal(_ctl(f.key).text);
          if (v != null) values[f.key] = v;
        case MrvDataType.integer:
          final t = _ctl(f.key).text.trim();
          if (t.isNotEmpty) values[f.key] = int.tryParse(t) ?? t;
        case MrvDataType.date || MrvDataType.text:
          final t = _ctl(f.key).text.trim();
          if (t.isNotEmpty) values[f.key] = t;
        case MrvDataType.boolean || MrvDataType.enumType || MrvDataType.photo:
          final v = _values[f.key];
          if (v != null) values[f.key] = v;
        case MrvDataType.geoPoint:
          break;
      }
    }
    return MrvEvidenceDraft(
      evidenceType: c.evidenceType,
      catalogVersion: c.version,
      adminUnitCode: '${p.data['commune_code'] ?? ''}',
      plotRef: p.id,
      householdRef: _household,
      seasonCode: _season,
      values: values,
      gps: _gps,
    );
  }

  Future<void> _submit() async {
    final d = _draft();
    if (d == null) return;
    setState(() => _busy = true);
    try {
      await ref.services.mrv.submit(formId: _formId, draft: d);
      ref.read(appControllerProvider).services?.sync.syncOnce().ignore();
      if (!mounted) return;
      showMessage(
        context,
        'Đã lưu minh chứng. Ảnh và bản ghi sẽ gửi khi có mạng.',
      );
      Navigator.of(context).pop();
    } on EvidenceFormException catch (e) {
      if (mounted) {
        setState(() => _errors = e.errors);
        showMessage(context, e.message, error: true);
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  String? _err(String field) {
    for (final e in _errors) {
      if (e.field == field) return e.reason;
    }
    return null;
  }

  Widget _fieldWidget(MrvField f) {
    final label =
        '${f.labelVi}${f.unit == null ? '' : ' (${f.unit})'}'
        '${f.required ? ' *' : ''}';
    final err = _err('values.${f.key}');
    switch (f.type) {
      case MrvDataType.decimal ||
          MrvDataType.integer ||
          MrvDataType.date ||
          MrvDataType.text:
        return TextField(
          controller: _ctl(f.key),
          keyboardType: f.type == MrvDataType.text
              ? TextInputType.text
              : const TextInputType.numberWithOptions(
                  decimal: true,
                  signed: true,
                ),
          decoration: InputDecoration(
            labelText: label,
            helperText: f.type == MrvDataType.date ? 'YYYY-MM-DD' : f.helpVi,
            errorText: err,
          ),
        );
      case MrvDataType.boolean:
        return SwitchListTile(
          title: Text(label),
          subtitle: err == null ? null : Text(err),
          value: _values[f.key] == true,
          onChanged: (v) => setState(() => _values[f.key] = v),
        );
      case MrvDataType.enumType:
        return DropdownButtonFormField<String>(
          initialValue: _values[f.key] as String?,
          isExpanded: true,
          decoration: InputDecoration(labelText: label, errorText: err),
          items: [
            for (final v in f.enumValues)
              DropdownMenuItem(value: v, child: Text(v)),
          ],
          onChanged: (v) => setState(() {
            if (v != null) _values[f.key] = v;
          }),
        );
      case MrvDataType.photo:
        final sel = [...(_values[f.key] as List<String>?) ?? const <String>[]];
        return Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(label),
            for (final p in _photos)
              CheckboxListTile(
                value: sel.contains(p.id),
                title: Text('Ảnh ${p.meta['captured_at']}'),
                onChanged: (v) => setState(() {
                  v == true ? sel.add(p.id) : sel.remove(p.id);
                  _values[f.key] = sel;
                }),
              ),
            if (err != null)
              Text(err, style: const TextStyle(color: AgColors.red)),
          ],
        );
      case MrvDataType.geoPoint:
        return const SizedBox.shrink();
    }
  }

  @override
  Widget build(BuildContext context) {
    final data = ref.watch(_formDataProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Minh chứng MRV')),
      body: data.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (d) {
          if (d.catalogs.isEmpty) {
            return const Center(
              child: Padding(
                padding: EdgeInsets.all(24),
                child: Text(
                  'Chưa có danh mục trường minh chứng đã duyệt trên máy. '
                  'Hãy đồng bộ khi có mạng (cần vai thu minh chứng).',
                  textAlign: TextAlign.center,
                  style: TextStyle(fontSize: 17),
                ),
              ),
            );
          }
          final c = _cat;
          return ListView(
            padding: const EdgeInsets.all(16),
            children: [
              DropdownButtonFormField<String>(
                initialValue: c?.evidenceType,
                isExpanded: true,
                decoration: const InputDecoration(labelText: 'Loại minh chứng'),
                items: [
                  for (final k in d.catalogs)
                    DropdownMenuItem(
                      value: k.evidenceType,
                      child: Text(
                        '${mrvEvidenceTypes[k.evidenceType] ?? k.evidenceType}'
                        ' · danh mục v${k.version}${k.provisional ? ' (tạm)' : ''}',
                      ),
                    ),
                ],
                onChanged: (v) => setState(() {
                  _cat = d.catalogs.firstWhere((k) => k.evidenceType == v);
                  _values.clear();
                  _errors = const [];
                }),
              ),
              const SizedBox(height: 8),
              DropdownButtonFormField<String>(
                initialValue: _plot?.id,
                isExpanded: true,
                decoration: InputDecoration(
                  labelText: 'Thửa ruộng / SPOT',
                  errorText: _err('plot_ref'),
                ),
                items: [
                  for (final p in d.plots)
                    DropdownMenuItem(value: p.id, child: Text(plotLabel(p))),
                ],
                onChanged: (v) =>
                    _selectPlot(d.plots.firstWhere((p) => p.id == v)),
              ),
              if (c?.field('season') != null) ...[
                const SizedBox(height: 8),
                DropdownButtonFormField<String>(
                  initialValue: _season,
                  isExpanded: true,
                  decoration: InputDecoration(
                    labelText: 'Vụ',
                    errorText: _err('season_code'),
                  ),
                  items: [
                    for (final e in d.seasons)
                      DropdownMenuItem(value: e.code, child: Text(e.name)),
                  ],
                  onChanged: (v) => setState(() => _season = v),
                ),
              ],
              if (_block != null)
                Card(
                  color: AgColors.amberBg,
                  child: Padding(
                    padding: const EdgeInsets.all(12),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        Text(_block!, style: const TextStyle(fontSize: 16)),
                        if (_household != null &&
                            _block ==
                                ConsentRequiredException(_household!).message)
                          FilledButton.icon(
                            icon: const Icon(Icons.how_to_reg),
                            label: const Text('Ghi nhận đồng ý của nông hộ'),
                            onPressed: () async {
                              await Navigator.of(context).push(
                                MaterialPageRoute<bool>(
                                  builder: (_) =>
                                      ConsentScreen(subjectRef: _household!),
                                ),
                              );
                              if (_plot != null) await _selectPlot(_plot!);
                            },
                          ),
                      ],
                    ),
                  ),
                ),
              const SizedBox(height: 8),
              FilledButton.icon(
                onPressed: _busy || _plot == null || _block != null
                    ? null
                    : _capture,
                icon: const Icon(Icons.photo_camera),
                label: Text('Chụp ảnh (${_photos.length})'),
              ),
              if (_err('file_refs') != null)
                Text(
                  _err('file_refs')!,
                  style: const TextStyle(color: AgColors.red),
                ),
              Text(
                _gps == null
                    ? 'Toạ độ: chưa có (lấy khi chụp ảnh)'
                    : 'Toạ độ đã ghi nhận (GPS)',
                style: TextStyle(
                  color: _err('gps') != null ? AgColors.red : AgColors.mute,
                ),
              ),
              const SizedBox(height: 8),
              if (c != null)
                for (final f in c.fields.where((f) => !f.reserved))
                  Padding(
                    padding: const EdgeInsets.only(bottom: 8),
                    child: _fieldWidget(f),
                  ),
              const SizedBox(height: 8),
              FilledButton.icon(
                onPressed: _busy || c == null || _plot == null ? null : _submit,
                icon: const Icon(Icons.save),
                label: const Text('Lưu minh chứng'),
              ),
              const SizedBox(height: 8),
              const Text(
                'Ảnh được đóng dấu thời gian, toạ độ và mã hóa ngay trong bộ nhớ. '
                'Bản ghi chỉ gửi khi mọi ảnh đã tải lên.',
                style: TextStyle(color: AgColors.mute, fontSize: 14),
              ),
            ],
          );
        },
      ),
    );
  }
}
