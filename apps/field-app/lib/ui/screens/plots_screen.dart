import 'package:agrigreen_mobile/data/reference/reference_repository.dart';
import 'package:agrigreen_mobile/domain/geo/boundary.dart';
import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/boundary_service.dart';
import '../providers.dart';
import 'boundary_screen.dart';
import 'map_packs_screen.dart';
import 'notifications_screen.dart' show unawaitedSync;

final plotsProvider = FutureProvider.autoDispose<List<RefRow>>(
  (ref) => servicesOf(ref).refs.list(RefEntity.plot),
);

class PlotRowView {
  const PlotRowView(this.plot, this.status);
  final RefRow plot;
  final PlotBoundaryStatus status;
}

final _plotRowsProvider = FutureProvider.autoDispose<List<PlotRowView>>((
  ref,
) async {
  ref.watch(syncStatusStreamProvider);
  final s = servicesOf(ref);
  return [
    for (final p in await s.refs.list(RefEntity.plot))
      PlotRowView(p, await s.boundaries.statusOf(p.id)),
  ];
});

/// Ranh giới xung đột/bị từ chối chưa xử lý (kể cả thửa mới chưa có trên máy chủ).
final boundaryAttentionProvider =
    FutureProvider.autoDispose<List<PlotBoundaryStatus>>((ref) {
      ref.watch(syncStatusStreamProvider);
      return servicesOf(ref).boundaries.needingAttention();
    });

String plotLabel(RefRow p) =>
    (p.data['label'] ?? p.data['name'] ?? p.data['code'] ?? p.id).toString();

Color plotStateColor(PlotSyncState s) => switch (s) {
  PlotSyncState.pending => AgColors.amber,
  PlotSyncState.accepted => AgColors.g700,
  PlotSyncState.needsConfirm => AgColors.amber,
  PlotSyncState.conflict => AgColors.amber,
  PlotSyncState.rejected => AgColors.red,
  PlotSyncState.none => AgColors.mute,
};

/// Xác nhận giữ nguyên chồng lấn/lệch diện tích sau phản hồi của máy chủ.
Future<void> confirmBoundary(
  BuildContext context,
  WidgetRef ref,
  PlotBoundaryStatus st,
) async {
  final r = st.record;
  if (r == null) return;
  final ok = await showDialog<bool>(
    context: context,
    builder: (ctx) => AlertDialog(
      title: const Text('Xác nhận giữ nguyên ranh giới'),
      content: Text(
        'Máy chủ báo ranh giới chồng lấn với thửa cùng HTX/thửa nháp hoặc lệch '
        'diện tích khai báo${st.detail == null ? '' : ' (${st.detail})'}. '
        'Bạn đã kiểm tra thực địa và xác nhận giữ nguyên?',
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(ctx, false),
          child: const Text('Để sau'),
        ),
        FilledButton(
          onPressed: () => Navigator.pop(ctx, true),
          child: const Text('Xác nhận'),
        ),
      ],
    ),
  );
  if (ok != true) return;
  try {
    await ref.services.boundaries.confirmAfterServer(r);
    unawaitedSync(ref);
    if (context.mounted) {
      showMessage(context, 'Đã gửi xác nhận. Chờ đồng bộ.');
    }
  } on BoundaryException catch (e) {
    if (context.mounted) showMessage(context, e.message, error: true);
  }
  ref.invalidate(_plotRowsProvider);
  ref.invalidate(boundaryAttentionProvider);
  ref.invalidate(homeSummaryProvider);
}

class PlotsScreen extends ConsumerWidget {
  const PlotsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final rows = ref.watch(_plotRowsProvider);
    final attention = ref.watch(boundaryAttentionProvider).value ?? const [];
    Future<void> draw(RefRow? plot) async {
      await Navigator.of(context).push(
        MaterialPageRoute<void>(builder: (_) => BoundaryScreen(plot: plot)),
      );
      ref.invalidate(_plotRowsProvider);
      ref.invalidate(boundaryAttentionProvider);
    }

    return Scaffold(
      appBar: AppBar(
        title: const Text('Thửa ruộng'),
        actions: [
          IconButton(
            tooltip: 'Bản đồ ngoại tuyến',
            iconSize: 28,
            icon: const Icon(Icons.offline_pin),
            onPressed: () => Navigator.of(context).push(
              MaterialPageRoute<void>(builder: (_) => const MapPacksScreen()),
            ),
          ),
        ],
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: () => draw(null),
        icon: const Icon(Icons.add_location_alt),
        label: const Text('Vẽ thửa mới', style: TextStyle(fontSize: 17)),
      ),
      body: rows.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (list) => ListView(
          padding: const EdgeInsets.only(bottom: 96),
          children: [
            for (final st in attention)
              Card(
                color: st.state == PlotSyncState.rejected
                    ? AgColors.redBg
                    : AgColors.amberBg,
                child: ListTile(
                  leading: Icon(
                    Icons.report_problem,
                    color: plotStateColor(st.state),
                  ),
                  title: Text(
                    '${st.record?.payload['name'] ?? 'Ranh giới'} · ${st.state.label}',
                  ),
                  subtitle: Text(
                    [
                      'Lưu lúc ${st.record?.eventTime ?? ''}',
                      ?st.detail,
                    ].join('\n'),
                  ),
                  isThreeLine: st.detail != null,
                  trailing: st.canConfirm
                      ? FilledButton(
                          onPressed: () => confirmBoundary(context, ref, st),
                          child: const Text('Xác nhận'),
                        )
                      : null,
                ),
              ),
            if (list.isEmpty)
              const Padding(
                padding: EdgeInsets.all(24),
                child: Text(
                  'Chưa có thửa nào trong địa bàn. Hãy đồng bộ khi có mạng.',
                  textAlign: TextAlign.center,
                  style: TextStyle(fontSize: 17),
                ),
              ),
            for (final row in list) _plotTile(row, draw),
          ],
        ),
      ),
    );
  }

  Widget _plotTile(PlotRowView row, Future<void> Function(RefRow?) draw) {
    final p = row.plot;
    final ring = ringFromGeoJson(p.data['boundary'] ?? p.data['geometry']);
    final hasBoundary = ring != null && ring.length >= 3;
    final area = p.data['area_ha'];
    final st = row.status;
    final review = p.data['needs_review'] == true;
    return Column(
      children: [
        ListTile(
          tileColor: Colors.white,
          leading: Icon(
            hasBoundary ? Icons.check_circle : Icons.pending,
            color: hasBoundary ? AgColors.g700 : AgColors.amber,
            size: 32,
          ),
          title: Text(plotLabel(p)),
          subtitle: Text(
            [
              if (hasBoundary)
                'Đã có ranh giới${area != null ? ' · $area ha' : ''}'
              else
                'Chưa vẽ ranh giới',
              if (review) 'Cần xem xét',
              if (st.state != PlotSyncState.none) st.state.label,
            ].join(' · '),
            style: TextStyle(
              color: st.state == PlotSyncState.none || !hasBoundary
                  ? null
                  : plotStateColor(st.state),
            ),
          ),
          trailing: const Icon(Icons.chevron_right, size: 28),
          onTap: () => draw(p),
        ),
        const Divider(height: 1),
      ],
    );
  }
}
