import 'package:agrigreen_mobile/data/reference/reference_repository.dart';
import 'package:agrigreen_mobile/ui/map/boundary_editor_screen.dart';
import 'package:agrigreen_mobile/ui/map/map_packs_screen.dart';
import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers.dart';
import 'kn_common.dart';
import 'kn_providers.dart';

/// Mở màn vẽ thửa của lõi với công cụ v7.7: chấm điểm / hình tròn / hình
/// vuông và trường HTX, tên nông dân, tên lô, ghi chú. Lưu được khi ngoại
/// tuyến (hàng đợi `plot_boundary`).
Future<bool?> openKnPlotEditor(
  BuildContext context,
  WidgetRef ref, {
  RefRow? plot,
  String? cooperativeRef,
}) async {
  final s = ref.services;
  final pf = ref.read(platformProvider);
  var coops = <(String, String)>[];
  try {
    final page = await ref.read(knCollectionServiceProvider).cooperatives();
    coops = [for (final c in page.items) ('${c['ref']}', '${c['name']}')];
  } on Object {
    // ngoại tuyến không có bản đệm: bỏ chọn HTX
  }
  if (!context.mounted) return null;
  return Navigator.of(context).push<bool>(
    MaterialPageRoute(
      builder: (ctx) => BoundaryEditorScreen(
        boundaries: s.boundaries,
        location: pf.location,
        tileUrlTemplate: pf.config.tileUrlTemplate,
        tileStore: s.tiles.store,
        plot: plot,
        shapeTools: true,
        plotAttributes: true,
        cooperatives: coops,
        onDownloadView: (bbox, minZoom, maxZoom) => Navigator.of(ctx).push(
          MaterialPageRoute<void>(
            builder: (_) => MapPacksScreen(
              service: s.tiles,
              proposal: bbox,
              minZoom: minZoom,
              maxZoom: maxZoom,
            ),
          ),
        ),
      ),
    ),
  );
}

/// "Vẽ thửa ruộng" (FN-XA-03.05).
class KnPlotsScreen extends ConsumerStatefulWidget {
  const KnPlotsScreen({super.key});

  @override
  ConsumerState<KnPlotsScreen> createState() => _KnPlotsScreenState();
}

class _KnPlotsScreenState extends ConsumerState<KnPlotsScreen> {
  final list = GlobalKey<KnPlotsListState>();

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Vẽ thửa ruộng'),
        actions: [
          IconButton(
            tooltip: 'Bản đồ ngoại tuyến',
            icon: const Icon(Icons.offline_pin),
            onPressed: () => Navigator.of(context).push(
              MaterialPageRoute<void>(
                builder: (_) => MapPacksScreen(service: ref.services.tiles),
              ),
            ),
          ),
        ],
      ),
      floatingActionButton: FloatingActionButton.extended(
        key: const ValueKey('kn-plot-new'),
        onPressed: () async {
          final ok = await openKnPlotEditor(context, ref);
          if (ok == true) await list.currentState?.reload();
        },
        icon: const Icon(Icons.add_location_alt),
        label: const Text('Vẽ lô mới'),
      ),
      body: KnPlotsList(key: list),
    );
  }
}

/// Danh sách thửa (REST, phân trang; trang đầu đệm ngoại tuyến) + thửa mới
/// vẽ trên máy chưa đồng bộ.
class KnPlotsList extends ConsumerStatefulWidget {
  const KnPlotsList({super.key, this.cooperativeRef});
  final String? cooperativeRef;
  @override
  ConsumerState<KnPlotsList> createState() => KnPlotsListState();
}

class KnPlotsListState extends ConsumerState<KnPlotsList> {
  final _list = GlobalKey<KnPagedListState>();
  List<(String, String)> _local = const [];

  Future<void> reload() async {
    await _loadLocal();
    await _list.currentState?.reload();
  }

  @override
  void initState() {
    super.initState();
    _loadLocal();
  }

  Future<void> _loadLocal() async {
    final s = ref.read(appControllerProvider).services;
    if (s == null) return;
    final recs = await s.boundaries.localNewPlots();
    if (!mounted) return;
    setState(
      () => _local = [
        for (final r in recs)
          (
            '${r.payload['lot_name'] ?? r.payload['name'] ?? 'Lô mới'}',
            s.boundaries.statusOfRecord(r).state.label,
          ),
      ],
    );
  }

  @override
  Widget build(BuildContext context) {
    final svc = ref.read(knCollectionServiceProvider);
    return KnPagedList(
      key: _list,
      empty: 'Chưa có thửa.',
      header: Column(
        children: [
          for (final (name, st) in _local)
            ListTile(
              tileColor: AgColors.amberBg,
              leading: const Icon(Icons.cloud_upload_outlined),
              title: Text(name),
              subtitle: Text('Vẽ trên máy · $st'),
            ),
        ],
      ),
      load: (cur) => svc.plots(coopRef: widget.cooperativeRef, cursor: cur),
      itemBuilder: (ctx, p) {
        final dev = p['area_deviation_pct'];
        return ListTile(
          key: ValueKey('kn-plot-${p['ref']}'),
          title: Text('${p['lot_name'] ?? p['code'] ?? 'Thửa'}'),
          subtitle: Text(
            [
              if (p['farmer'] is Map)
                'Nông dân: ${(p['farmer'] as Map)['label']}',
              'Diện tích ${p['area_ha'] ?? '—'} ha'
                  '${p['declared_area_ha'] == null ? '' : ' (khai báo ${p['declared_area_ha']} ha'
                            '${dev == null ? '' : ', lệch $dev %'})'}',
              if ((p['note'] ?? '') != '') '${p['note']}',
            ].join('\n'),
          ),
          trailing: const Icon(Icons.edit_location_alt),
          onTap: () async {
            final s = ref.services;
            final row = await s.refs.get(RefEntity.plot, '${p['ref']}');
            if (!ctx.mounted) return;
            if (row == null) {
              showMessage(
                ctx,
                'Thửa chưa đồng bộ về máy — đồng bộ rồi mở lại để sửa ranh giới.',
                error: true,
              );
              return;
            }
            final ok = await openKnPlotEditor(ctx, ref, plot: row);
            if (ok == true) await reload();
          },
        );
      },
    );
  }
}
