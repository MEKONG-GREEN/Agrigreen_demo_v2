import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/field_report_service.dart';
import '../../data/catalog/catalog_cache.dart';
import '../../data/reference/reference_repository.dart';
import '../../domain/outbox_record.dart';
import '../providers.dart';
import '../theme.dart';
import '../widgets/common.dart';

/// Lựa chọn cho biểu mẫu: xã/HTX trong phạm vi (từ dữ liệu đã đồng bộ) và
/// danh mục giống/vụ/sâu bệnh (đệm từ API masterdata).
class ReportOptions {
  const ReportOptions({
    required this.communes,
    required this.cooperatives,
    required this.varieties,
    required this.seasons,
    required this.pests,
  });
  final Map<String, String> communes;
  final Map<String, String> cooperatives;
  final List<CatalogEntry> varieties;
  final List<CatalogEntry> seasons;
  final List<CatalogEntry> pests;
}

final reportOptionsProvider = FutureProvider.autoDispose<ReportOptions>((
  ref,
) async {
  final s = servicesOf(ref);
  final communes = <String, String>{};
  final coops = <String, String>{};
  for (final u in await s.refs.list(RefEntity.adminUnit)) {
    final code = '${u.data['code'] ?? u.id}';
    communes[code] = '${u.data['name'] ?? code}';
  }
  for (final e in [
    ...await s.refs.list(RefEntity.plot),
    ...await s.refs.list(RefEntity.task),
  ]) {
    final c = e.data['commune_code'];
    if (c is String && c.isNotEmpty) communes.putIfAbsent(c, () => c);
    final coop = e.data['cooperative_ref'];
    if (coop is String && coop.isNotEmpty) {
      coops.putIfAbsent(coop, () => 'HTX ${coop.substring(0, 8)}');
    }
  }
  for (final c in await s.refs.list(RefEntity.cooperative)) {
    coops[c.id] = '${c.data['name'] ?? c.data['label'] ?? c.id}';
  }
  return ReportOptions(
    communes: communes,
    cooperatives: coops,
    varieties: await s.catalogs.entries('rice_variety'),
    seasons: await s.catalogs.entries('season'),
    pests: await s.catalogs.entries('pest'),
  );
});

/// Báo cáo hiện trường KN: tiến độ gieo sạ (FR-01) hoặc sinh trưởng – sâu bệnh
/// (FR-02). Payload đúng `ReportPayload` của pf/extension; lưu được ngoại tuyến.
class FieldReportScreen extends ConsumerStatefulWidget {
  const FieldReportScreen({super.key});

  @override
  ConsumerState<FieldReportScreen> createState() => _FieldReportScreenState();
}

class _FieldReportScreenState extends ConsumerState<FieldReportScreen> {
  FieldReportKind _kind = FieldReportKind.sowing;
  String? _commune;
  String? _coop;
  String? _season;
  String? _variety;
  String? _pest;
  PestSeverity? _severity;
  bool _urgent = false;
  final _communeText = TextEditingController();
  final _area = TextEditingController();
  final _sownOn = TextEditingController();
  final _stage = TextEditingController();
  final _note = TextEditingController();
  bool _busy = false;

  @override
  void dispose() {
    for (final c in [_communeText, _area, _sownOn, _stage, _note]) {
      c.dispose();
    }
    super.dispose();
  }

  Future<void> _save() async {
    final s = ref.read(appControllerProvider).services;
    if (s == null) return;
    final sowing = _kind == FieldReportKind.sowing;
    final draft = FieldReportDraft(
      kind: _kind,
      communeCode: (_commune ?? _communeText.text).trim(),
      cooperativeRef: _coop,
      period: periodOf(s.platform.clock.now()),
      seasonCode: _season ?? '',
      varietyCode: _variety ?? '',
      sownAreaHa: sowing ? normalizeDecimal(_area.text) : null,
      sownOn: _sownOn.text.trim().isEmpty ? null : _sownOn.text.trim(),
      growthStage: _stage.text.trim(),
      pestCode: _pest ?? '',
      severity: _severity,
      affectedAreaHa: sowing ? null : normalizeDecimal(_area.text),
      urgent: _urgent,
      note: _note.text,
    );
    setState(() => _busy = true);
    try {
      await s.reports.create(draft);
      if (!mounted) return;
      showMessage(context, 'Đã lưu báo cáo. Chờ đồng bộ.');
      Navigator.of(context).pop();
    } on FieldReportError catch (e) {
      if (mounted) showMessage(context, e.reason, error: true);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Widget _pick(
    String label,
    String? value,
    Map<String, String> items,
    void Function(String?) onChanged,
  ) => DropdownButtonFormField<String>(
    initialValue: value,
    isExpanded: true,
    decoration: InputDecoration(labelText: label),
    items: [
      for (final e in items.entries)
        DropdownMenuItem(value: e.key, child: Text(e.value)),
    ],
    onChanged: onChanged,
  );

  Map<String, String> _catalog(List<CatalogEntry> l) => {
    for (final e in l) e.code: '${e.name} (${e.code})',
  };

  @override
  Widget build(BuildContext context) {
    final opts = ref.watch(reportOptionsProvider);
    final sowing = _kind == FieldReportKind.sowing;
    return Scaffold(
      appBar: AppBar(title: const Text('Báo cáo hiện trường')),
      body: opts.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (o) => ListView(
          padding: const EdgeInsets.all(16),
          children: [
            SegmentedButton<FieldReportKind>(
              segments: [
                for (final k in FieldReportKind.values)
                  ButtonSegment(value: k, label: Text(k.label)),
              ],
              selected: {_kind},
              onSelectionChanged: (v) => setState(() => _kind = v.first),
            ),
            const SizedBox(height: 12),
            if (o.communes.isNotEmpty)
              _pick('Xã', _commune, o.communes, (v) {
                setState(() => _commune = v);
              })
            else
              TextField(
                controller: _communeText,
                decoration: const InputDecoration(labelText: 'Mã xã'),
              ),
            const SizedBox(height: 8),
            _pick(
              sowing ? 'Hợp tác xã (bắt buộc)' : 'Hợp tác xã (không bắt buộc)',
              _coop,
              o.cooperatives,
              (v) => setState(() => _coop = v),
            ),
            const SizedBox(height: 8),
            if (sowing) ...[
              _pick(
                'Vụ',
                _season,
                _catalog(o.seasons),
                (v) => setState(() => _season = v),
              ),
              const SizedBox(height: 8),
              _pick(
                'Giống lúa',
                _variety,
                _catalog(o.varieties),
                (v) => setState(() => _variety = v),
              ),
              const SizedBox(height: 8),
              TextField(
                controller: _area,
                keyboardType: const TextInputType.numberWithOptions(
                  decimal: true,
                ),
                decoration: const InputDecoration(
                  labelText: 'Diện tích đã sạ (ha)',
                ),
              ),
              const SizedBox(height: 8),
              TextField(
                controller: _sownOn,
                decoration: const InputDecoration(
                  labelText: 'Ngày sạ (YYYY-MM-DD, không bắt buộc)',
                ),
              ),
            ] else ...[
              TextField(
                controller: _stage,
                decoration: const InputDecoration(
                  labelText: 'Giai đoạn sinh trưởng (mã, vd de_nhanh)',
                ),
              ),
              const SizedBox(height: 8),
              _pick(
                'Sâu bệnh',
                _pest,
                _catalog(o.pests),
                (v) => setState(() => _pest = v),
              ),
              const SizedBox(height: 8),
              Wrap(
                spacing: 8,
                children: [
                  for (final sv in PestSeverity.values)
                    ChoiceChip(
                      label: Text(sv.label),
                      selected: _severity == sv,
                      onSelected: (_) => setState(() => _severity = sv),
                    ),
                ],
              ),
              const SizedBox(height: 8),
              TextField(
                controller: _area,
                keyboardType: const TextInputType.numberWithOptions(
                  decimal: true,
                ),
                decoration: const InputDecoration(
                  labelText: 'Diện tích nhiễm (ha, không bắt buộc)',
                ),
              ),
              SwitchListTile(
                value: _urgent,
                onChanged: (v) => setState(() => _urgent = v),
                title: const Text('Khẩn cấp'),
              ),
            ],
            if (o.varieties.isEmpty && o.pests.isEmpty)
              const Padding(
                padding: EdgeInsets.only(top: 8),
                child: Text(
                  'Chưa có danh mục giống/vụ/sâu bệnh trên máy. Hãy đồng bộ khi có mạng.',
                  style: TextStyle(color: AgColors.amber),
                ),
              ),
            const SizedBox(height: 8),
            TextField(
              controller: _note,
              maxLines: 3,
              decoration: const InputDecoration(
                labelText: 'Ghi chú (không ghi SĐT, CCCD)',
              ),
            ),
            const SizedBox(height: 16),
            FilledButton.icon(
              onPressed: _busy ? null : _save,
              icon: const Icon(Icons.save),
              label: const Text('Lưu báo cáo'),
            ),
            const SizedBox(height: 24),
            const _RecentReports(),
          ],
        ),
      ),
    );
  }
}

final recentReportsProvider = FutureProvider.autoDispose<List<OutboxRecord>>(
  (ref) => servicesOf(ref).reports.recent(),
);

String reportSummary(Map<String, Object?> p) {
  final k = FieldReportKind.parse(p['kind'] as String?);
  return switch (k) {
    FieldReportKind.sowing =>
      'Gieo sạ · ${p['variety_code']} · ${p['sown_area_ha']} ha',
    FieldReportKind.growthPest =>
      'Sâu bệnh · ${p['pest_code']} · ${p['severity']}',
    null => 'Báo cáo',
  };
}

class _RecentReports extends ConsumerWidget {
  const _RecentReports();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final list = ref.watch(recentReportsProvider);
    return list.when(
      loading: () => const SizedBox.shrink(),
      error: (e, _) => Text('Lỗi: $e'),
      data: (items) => Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          if (items.isNotEmpty)
            const Text(
              'Báo cáo gần đây trên máy',
              style: TextStyle(fontSize: 17, fontWeight: FontWeight.w600),
            ),
          for (final r in items)
            Card(
              child: ListTile(
                title: Text(reportSummary(r.payload)),
                subtitle: Text(
                  'Kỳ ${r.payload['period']} · ${r.eventTime}'
                  '${r.op == RecordOp.correct ? ' · Bản ghi bù' : ''}',
                ),
                trailing: StatusChip(r.status),
              ),
            ),
        ],
      ),
    );
  }
}
