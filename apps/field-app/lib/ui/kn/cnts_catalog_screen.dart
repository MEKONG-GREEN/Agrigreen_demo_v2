import 'package:agrigreen_mobile/data/reference/reference_repository.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers.dart';

/// Tên các danh mục CNTS (khớp masterdata/domain.CNTSCatalogs, W11.41).
const cntsCatalogNames = {
  'cnts_species': 'Vật nuôi',
  'cnts_breed': 'Giống',
  'cnts_vaccine': 'Vaccine',
  'cnts_vaccination_schedule': 'Quy trình tiêm',
  'cnts_disease': 'Bệnh',
  'cnts_chemical': 'Hóa chất',
  'cnts_slaughter_facility': 'Cơ sở giết mổ',
  'cnts_aquaculture_object': 'Đối tượng nuôi thủy sản',
  'cnts_fishing_group': 'Nhóm khai thác',
  'cnts_indicator': 'Chỉ tiêu theo dõi',
  'cnts_model_type': 'Loại mô hình',
  'cnts_document_type': 'Loại văn bản',
};

/// Danh mục CNTS trên máy, theo danh mục.
final cntsCatalogProvider = FutureProvider<Map<String, List<RefRow>>>((
  ref,
) async {
  ref.watch(syncStatusStreamProvider);
  final rows = await servicesOf(ref).refs.list(RefEntity.catalogItem);
  final out = <String, List<RefRow>>{};
  for (final r in rows) {
    final c = '${r.data['catalog']}';
    if (cntsCatalogNames.containsKey(c)) out.putIfAbsent(c, () => []).add(r);
  }
  for (final l in out.values) {
    l.sort((a, b) => '${a.data['name']}'.compareTo('${b.data['name']}'));
  }
  return out;
});

/// Màn 3 — Danh mục CNTS ngoại tuyến (W11.42): 12 danh mục CNTS đồng bộ xuống
/// máy qua entity `catalog_item` (W11.41 để lại `SyncCatalogs` chưa gồm CNTS):
/// cơ sở giết mổ (mã mộc dấu GS/GC, loài), vật nuôi, vaccine… — tra cứu khi
/// nhập BM-01/BM-04 không có mạng. Chỉ xem.
class CntsCatalogScreen extends ConsumerWidget {
  const CntsCatalogScreen({super.key});

  String _attrs(Map<String, Object?> d) {
    final a = (d['attrs'] as Map?) ?? const {};
    final parts = <String>[];
    for (final k in const [
      'commune_code',
      'facility_kind',
      'stamp_codes_gs',
      'stamp_codes_gc',
      'species',
      'unit',
      'report_group',
    ]) {
      final v = a[k];
      if (v != null && '$v'.isNotEmpty) parts.add('$k: $v');
    }
    return parts.join(' · ');
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final s = ref.watch(cntsCatalogProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Danh mục CNTS')),
      body: s.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (m) => m.isEmpty
            ? const Center(
                child: Text('Chưa có danh mục trên máy — đồng bộ khi có mạng.'),
              )
            : ListView(
                children: [
                  for (final c in cntsCatalogNames.keys)
                    if (m[c] != null)
                      ExpansionTile(
                        key: Key('cnts-catalog-$c'),
                        title: Text('${cntsCatalogNames[c]} (${m[c]!.length})'),
                        children: [
                          for (final r in m[c]!)
                            ListTile(
                              dense: true,
                              title: Text('${r.data['name']}'),
                              subtitle: Text(
                                '${r.data['code']} ${_attrs(r.data)}'.trim(),
                              ),
                            ),
                        ],
                      ),
                ],
              ),
      ),
    );
  }
}
