import 'package:agrigreen_mobile/application/boundary_service.dart';
import 'package:agrigreen_mobile/data/reference/reference_repository.dart';
import 'package:agrigreen_mobile/domain/geo/boundary.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:agrigreen_mobile/ui/map/boundary_editor_screen.dart';
import 'package:agrigreen_mobile/ui/map/map_packs_screen.dart';
import 'package:agrigreen_mobile/ui/map/offline_tile_provider.dart';
import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_map/flutter_map.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:latlong2/latlong.dart';

import '../providers.dart';
import '../widgets.dart';

/// Nhãn lô do cán bộ KN vẽ trước, HTX chưa chỉnh (HTX US-PLOT-01 AC-1).
const msgKnDraft = 'Nháp - KN vẽ trước';

/// Lô nháp chưa gán HTX (pf/geo cho quản lý HTX thấy để nhận/chỉnh).
const msgUnassigned = 'Chưa gán HTX';

/// Ranh giới hiện tại do cán bộ KN vẽ (app hiện trường hoặc web KN).
bool isKnDraft(Map<String, Object?> d) =>
    '${d['source'] ?? ''}'.startsWith('kn_');

class MapPlot {
  const MapPlot(this.row, this.ring, this.status);
  final RefRow row;
  final List<GeoPoint>? ring;
  final PlotBoundaryStatus status;

  bool get knDraft => isKnDraft(row.data);
  bool get unassigned => '${row.data['cooperative_ref'] ?? ''}'.isEmpty;
  String get label => plotRowLabel(row);
}

class PlotMapData {
  const PlotMapData(this.plots, this.localNew);
  final List<MapPlot> plots;

  /// Lô mới vẽ trên máy, máy chủ chưa gửi xuống.
  final List<OutboxRecord> localNew;
}

final plotMapProvider = FutureProvider.autoDispose<PlotMapData>((ref) async {
  ref.watch(syncStatusStreamProvider);
  final b = servicesOf(ref).boundaries;
  final plots = <MapPlot>[];
  for (final p in await servicesOf(ref).refs.list(RefEntity.plot)) {
    // Mỗi lô một HTX: lô của HTX khác không hiện (máy chủ cũng không gửi).
    if (!await b.canEdit(p)) continue;
    plots.add(
      MapPlot(
        p,
        ringFromGeoJson(p.data['boundary'] ?? p.data['geometry']),
        await b.statusOf(p.id),
      ),
    );
  }
  // Lô KN vẽ trước lên đầu để HTX rà soát.
  plots.sort((a, b) {
    if (a.knDraft != b.knDraft) return a.knDraft ? -1 : 1;
    return a.label.compareTo(b.label);
  });
  return PlotMapData(plots, await b.localNewPlots());
});

Color _stateColor(PlotSyncState s) => switch (s) {
  PlotSyncState.accepted => AgColors.g700,
  PlotSyncState.rejected => AgColors.red,
  _ => AgColors.amber,
};

/// "Lô ruộng & Ranh giới GPS" của quản lý HTX (HTX US-PLOT-01): bản đồ lô của
/// HTX mình, kể cả lô cán bộ KN đã vẽ trước (nhãn "Nháp - KN vẽ trước",
/// không phải vẽ lại); sửa ranh giới trên cùng bản ghi lô (AC-2); vẽ lô mới
/// bằng đi GPS (US-PLOT-02) hoặc chọn trên bản đồ (US-PLOT-03), kể cả khi mất
/// sóng trên nền bản đồ đã tải (US-PLOT-05).
class PlotMapScreen extends ConsumerStatefulWidget {
  const PlotMapScreen({super.key});

  @override
  ConsumerState<PlotMapScreen> createState() => _PlotMapScreenState();
}

class _PlotMapScreenState extends ConsumerState<PlotMapScreen> {
  static const _defaultCenter = LatLng(10.0341, 105.7880);

  late final OfflineFirstTileProvider _tiles = htxTileProvider(ref);

  @override
  Widget build(BuildContext context) {
    final data = ref.watch(plotMapProvider);
    Future<void> draw(RefRow? plot) async {
      await Navigator.of(context).push(
        MaterialPageRoute<void>(builder: (_) => htxBoundaryEditor(ref, plot)),
      );
      ref.invalidate(plotMapProvider);
      ref.invalidate(homeSummaryProvider);
    }

    return Scaffold(
      appBar: AppBar(
        title: const Text('Lô ruộng & Ranh giới GPS'),
        actions: [
          IconButton(
            tooltip: 'Bản đồ ngoại tuyến',
            iconSize: 28,
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
        onPressed: () => draw(null),
        icon: const Icon(Icons.add_location_alt),
        label: const Text('Vẽ lô mới', style: TextStyle(fontSize: 17)),
      ),
      // Giữ bản đồ khi làm mới (TileLayer bị tháo sẽ đóng nguồn tile).
      body: data.when(
        skipLoadingOnReload: true,
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (d) {
          final rings = [
            for (final p in d.plots)
              if (p.ring != null && p.ring!.length >= 3) p,
          ];
          final localRings = [
            for (final r in d.localNew) ?ringFromGeoJson(r.payload['geometry']),
          ];
          final all = [
            for (final p in rings)
              for (final g in p.ring!) LatLng(g.lat, g.lon),
            for (final r in localRings)
              for (final g in r) LatLng(g.lat, g.lon),
          ];
          final pf = ref.read(platformProvider);
          return Column(
            children: [
              SizedBox(
                height: 280,
                child: FlutterMap(
                  options: MapOptions(
                    initialCenter: _defaultCenter,
                    initialZoom: 13,
                    initialCameraFit: all.length < 2
                        ? null
                        : CameraFit.bounds(
                            bounds: LatLngBounds.fromPoints(all),
                            padding: const EdgeInsets.all(24),
                          ),
                  ),
                  children: [
                    TileLayer(
                      urlTemplate: pf.config.tileUrlTemplate,
                      userAgentPackageName: 'vn.agrigreen.htx',
                      tileProvider: _tiles,
                    ),
                    PolygonLayer(
                      polygons: [
                        for (final p in rings)
                          Polygon(
                            points: [
                              for (final g in p.ring!) LatLng(g.lat, g.lon),
                            ],
                            color:
                                (p.knDraft || p.unassigned
                                        ? AgColors.amber
                                        : AgColors.g600)
                                    .withValues(alpha: 0.25),
                            borderColor: p.knDraft || p.unassigned
                                ? AgColors.amber
                                : AgColors.g700,
                            borderStrokeWidth: 2,
                          ),
                        for (final r in localRings)
                          Polygon(
                            points: [for (final g in r) LatLng(g.lat, g.lon)],
                            color: AgColors.blue.withValues(alpha: 0.2),
                            borderColor: AgColors.blue,
                            borderStrokeWidth: 2,
                          ),
                      ],
                    ),
                    const SimpleAttributionWidget(
                      source: Text('© OpenStreetMap contributors'),
                    ),
                  ],
                ),
              ),
              Expanded(
                child: ListView(
                  padding: const EdgeInsets.fromLTRB(8, 8, 8, 88),
                  children: [
                    if (d.plots.isEmpty && d.localNew.isEmpty)
                      const Padding(
                        padding: EdgeInsets.all(16),
                        child: Text(
                          'Chưa có lô ruộng của HTX trên máy. Đồng bộ khi có '
                          'mạng, hoặc bấm "Vẽ lô mới".',
                          style: TextStyle(fontSize: 17),
                        ),
                      ),
                    for (final r in d.localNew) _LocalPlotCard(record: r),
                    for (final p in d.plots)
                      _PlotCard(plot: p, onEdit: () => draw(p.row)),
                  ],
                ),
              ),
            ],
          );
        },
      ),
    );
  }
}

/// Nguồn tile của App HTX: gói ngoại tuyến trong CSDL mã hóa trước, rồi máy
/// chủ tile cấu hình (`AG_TILE_URL`).
OfflineFirstTileProvider htxTileProvider(WidgetRef ref) {
  final pf = ref.read(platformProvider);
  return OfflineFirstTileProvider(
    store: ref.read(appControllerProvider).services?.tiles.store,
    urlTemplate: pf.config.tileUrlTemplate,
    userAgent: 'vn.agrigreen.htx',
    fetchRemote: pf.tileFetcher,
  );
}

/// Màn vẽ ranh giới của App HTX: hai chế độ đi GPS / chọn trên bản đồ, nền
/// bản đồ ngoại tuyến trước, nút tải gói bản đồ vùng đang xem.
Widget htxBoundaryEditor(WidgetRef ref, RefRow? plot) {
  final s = ref.services;
  final pf = ref.read(platformProvider);
  return Builder(
    builder: (context) => BoundaryEditorScreen(
      boundaries: s.boundaries,
      location: pf.location,
      tileUrlTemplate: pf.config.tileUrlTemplate,
      tileStore: s.tiles.store,
      tileProvider: htxTileProvider(ref),
      plot: plot,
      userAgent: 'vn.agrigreen.htx',
      modeToggle: true,
      onDownloadView: (bbox, minZoom, maxZoom) => Navigator.of(context).push(
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
  );
}

class _PlotCard extends ConsumerWidget {
  const _PlotCard({required this.plot, required this.onEdit});
  final MapPlot plot;
  final VoidCallback onEdit;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final p = plot;
    final st = p.status;
    final area = p.row.data['area_ha'] as String?;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(
                  p.ring == null ? Icons.crop_square : Icons.map_outlined,
                  color: p.knDraft ? AgColors.amber : AgColors.g700,
                  size: 32,
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    p.label,
                    style: const TextStyle(
                      fontSize: 17,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ),
                TextButton.icon(
                  onPressed: onEdit,
                  icon: const Icon(Icons.edit_location_alt),
                  label: Text(
                    p.ring == null ? 'Vẽ ranh giới' : 'Sửa ranh giới',
                  ),
                ),
              ],
            ),
            Wrap(
              spacing: 6,
              runSpacing: 4,
              children: [
                if (p.knDraft) const Tag(msgKnDraft),
                if (p.unassigned)
                  const Tag(msgUnassigned, color: AgColors.mute),
                if (area != null)
                  Tag('${area.replaceAll('.', ',')} ha', color: AgColors.g700),
                if (st.state != PlotSyncState.none)
                  Tag(st.state.label, color: _stateColor(st.state)),
              ],
            ),
            if (st.canConfirm && st.record?.replacedBy == null)
              Align(
                alignment: Alignment.centerRight,
                child: FilledButton.icon(
                  onPressed: () => confirmBoundaryAfterServer(context, ref, st),
                  icon: const Icon(Icons.fact_check),
                  label: const Text('Xác nhận giữ nguyên'),
                ),
              ),
          ],
        ),
      ),
    );
  }
}

class _LocalPlotCard extends ConsumerWidget {
  const _LocalPlotCard({required this.record});
  final OutboxRecord record;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final st = ref.services.boundaries.statusOfRecord(record);
    final pl = record.payload;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '${pl['name'] ?? 'Lô mới'} · ${pl['point_count']} điểm · '
              '≈ ${'${pl['area_ha_client'] ?? ''}'.replaceAll('.', ',')} ha',
              style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w700),
            ),
            const SizedBox(height: 4),
            Tag(st.state.label, color: _stateColor(st.state)),
            if (st.detail != null)
              Text(
                st.detail!,
                style: const TextStyle(color: AgColors.mute, fontSize: 14),
              ),
            if (st.canConfirm && record.replacedBy == null)
              Align(
                alignment: Alignment.centerRight,
                child: FilledButton.icon(
                  onPressed: () => confirmBoundaryAfterServer(context, ref, st),
                  icon: const Icon(Icons.fact_check),
                  label: const Text('Xác nhận giữ nguyên'),
                ),
              ),
          ],
        ),
      ),
    );
  }
}

/// Máy chủ báo chồng lấn cùng HTX/lô nháp hoặc lệch diện tích (bản ghi
/// `conflict`/`rejected geo.*_needs_confirm`): quản lý HTX xác nhận giữ nguyên
/// → bản ghi mới/bản bù có `confirm_overlap: true` (HTX US-PLOT-04 AC-2).
Future<void> confirmBoundaryAfterServer(
  BuildContext context,
  WidgetRef ref,
  PlotBoundaryStatus st,
) async {
  final r = st.record;
  if (r == null) return;
  final ok = await confirmDialog(
    context,
    title: 'Xác nhận giữ nguyên ranh giới',
    message:
        'Máy chủ báo ranh giới chồng lấn với lô cùng HTX/lô nháp hoặc lệch '
        'diện tích khai báo${st.detail == null ? '' : ' (${st.detail})'}. '
        'Bạn đã kiểm tra thực địa và xác nhận giữ nguyên?',
  );
  if (!ok) return;
  try {
    await ref.services.boundaries.confirmAfterServer(r);
    if (context.mounted) showMessage(context, 'Đã gửi xác nhận. Chờ đồng bộ.');
  } on BoundaryException catch (e) {
    if (context.mounted) showMessage(context, e.message, error: true);
  }
  ref.invalidate(plotMapProvider);
}
