import 'package:agrigreen_mobile/application/tile_pack_service.dart';
import 'package:agrigreen_mobile/data/map/tile_store.dart';
import 'package:agrigreen_mobile/domain/geo/tiles.dart';
import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers.dart';

final tilePacksProvider = FutureProvider.autoDispose<List<TilePack>>(
  (ref) => servicesOf(ref).tiles.store.packs(),
);

String _mb(int bytes) => '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';

/// Gói bản đồ ngoại tuyến theo địa bàn (US-MAP-03). Tile lưu trong CSDL mã hóa.
class MapPacksScreen extends ConsumerStatefulWidget {
  const MapPacksScreen({
    super.key,
    this.proposal,
    this.minZoom = 12,
    this.maxZoom = 17,
  });

  /// Khung đang xem trên bản đồ (đề xuất tải).
  final GeoBBox? proposal;
  final int minZoom;
  final int maxZoom;

  @override
  ConsumerState<MapPacksScreen> createState() => _MapPacksScreenState();
}

class _MapPacksScreenState extends ConsumerState<MapPacksScreen> {
  late int _maxZoom = widget.maxZoom;
  TileDownloadProgress? _progress;
  bool _cancel = false;
  bool _running = false;

  Future<void> _download(TilePackPlan plan) async {
    final svc = ref.services.tiles;
    setState(() {
      _running = true;
      _cancel = false;
      _progress = TileDownloadProgress(0, plan.tiles, 0);
    });
    try {
      final p = await svc.download(
        plan,
        name: 'Vùng ${formatLocal(DateTime.now())}',
        onProgress: (p) {
          if (mounted) setState(() => _progress = p);
        },
        cancelled: () => _cancel,
      );
      if (mounted) {
        showMessage(
          context,
          p.status == 'ready'
              ? 'Đã tải ${p.tileCount} ô bản đồ (${_mb(p.bytes)}).'
              : 'Tải chưa đủ (${p.tileCount}/${plan.tiles}). Có thể tải tiếp khi có mạng.',
          error: p.status != 'ready',
        );
      }
    } on TilePackException catch (e) {
      if (mounted) showMessage(context, e.message, error: true);
    } finally {
      if (mounted) setState(() => _running = false);
      ref.invalidate(tilePacksProvider);
    }
  }

  @override
  Widget build(BuildContext context) {
    final packs = ref.watch(tilePacksProvider);
    final svc = ref.services.tiles;
    TilePackPlan? plan;
    String? planError;
    final b = widget.proposal;
    if (b != null) {
      try {
        plan = svc.plan(b, widget.minZoom, _maxZoom);
      } on TilePackException catch (e) {
        planError = e.message;
      }
    }
    return Scaffold(
      appBar: AppBar(title: const Text('Bản đồ ngoại tuyến')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          if (b != null)
            Card(
              child: Padding(
                padding: const EdgeInsets.all(16),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    const Text(
                      'Tải vùng đang xem',
                      style: TextStyle(
                        fontSize: 18,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    const SizedBox(height: 8),
                    Text(
                      'Diện tích ≈ ${b.areaKm2.toStringAsFixed(1)} km² · '
                      'mức phóng to ${widget.minZoom}–$_maxZoom',
                    ),
                    Slider(
                      value: _maxZoom.toDouble(),
                      min: widget.minZoom.toDouble(),
                      max: svc.limits.maxZoom.toDouble(),
                      divisions: svc.limits.maxZoom - widget.minZoom,
                      label: 'Chi tiết tới mức $_maxZoom',
                      onChanged: _running
                          ? null
                          : (v) => setState(() => _maxZoom = v.round()),
                    ),
                    if (plan != null) Text('${plan.tiles} ô bản đồ'),
                    if (planError != null)
                      Text(
                        planError,
                        style: const TextStyle(color: AgColors.red),
                      ),
                    const SizedBox(height: 8),
                    if (_progress != null && _running) ...[
                      LinearProgressIndicator(
                        value: _progress!.total == 0
                            ? null
                            : _progress!.done / _progress!.total,
                      ),
                      Text(
                        '${_progress!.done}/${_progress!.total}'
                        '${_progress!.failed > 0 ? ' · lỗi ${_progress!.failed}' : ''}',
                      ),
                      TextButton(
                        onPressed: () => setState(() => _cancel = true),
                        child: const Text('Dừng'),
                      ),
                    ] else
                      FilledButton.icon(
                        onPressed: plan == null ? null : () => _download(plan!),
                        icon: const Icon(Icons.download),
                        label: const Text('Tải về máy'),
                      ),
                  ],
                ),
              ),
            ),
          const SizedBox(height: 8),
          const Text(
            'Gói đã tải',
            style: TextStyle(fontSize: 18, fontWeight: FontWeight.w700),
          ),
          packs.when(
            loading: () => const LinearProgressIndicator(),
            error: (e, _) => Text('Lỗi: $e'),
            data: (list) => Column(
              children: [
                if (list.isEmpty)
                  const Padding(
                    padding: EdgeInsets.all(12),
                    child: Text(
                      'Chưa có gói bản đồ nào. Mở màn vẽ ranh giới, di chuyển tới địa bàn rồi chạm biểu tượng tải.',
                    ),
                  ),
                for (final p in list)
                  Card(
                    child: ListTile(
                      leading: Icon(
                        p.status == 'ready' ? Icons.map : Icons.error_outline,
                        color: p.status == 'ready'
                            ? AgColors.g700
                            : AgColors.amber,
                      ),
                      title: Text(p.name),
                      subtitle: Text(
                        '${p.tileCount} ô · ${_mb(p.bytes)} · mức ${p.minZoom}–${p.maxZoom}'
                        '${p.status == 'ready' ? '' : ' · chưa đủ'}',
                      ),
                      trailing: IconButton(
                        tooltip: 'Xóa gói',
                        icon: const Icon(Icons.delete_outline),
                        onPressed: () async {
                          await svc.store.deletePack(p.id);
                          ref.invalidate(tilePacksProvider);
                        },
                      ),
                    ),
                  ),
              ],
            ),
          ),
          const SizedBox(height: 12),
          const Text(
            'Nguồn tile theo cấu hình AG_TILE_URL. Máy chủ OpenStreetMap công cộng chỉ dùng khi phát triển; bản chính thức dùng máy chủ bản đồ trong nước.',
            style: TextStyle(color: AgColors.mute, fontSize: 14),
          ),
        ],
      ),
    );
  }
}
