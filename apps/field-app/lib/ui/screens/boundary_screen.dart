import 'dart:math';

import 'package:flutter/material.dart';
import 'package:flutter_map/flutter_map.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:latlong2/latlong.dart';

import '../../application/boundary_service.dart';
import '../../data/photo/location_source.dart';
import '../../data/reference/reference_repository.dart';
import '../../domain/geo/boundary.dart';
import '../providers.dart';
import '../theme.dart';
import '../widgets/common.dart';
import 'plots_screen.dart';

/// Vẽ ranh giới thửa: chạm bản đồ để chấm điểm hoặc lấy điểm GPS tại chỗ.
/// Lưu được khi ngoại tuyến — bản ghi vào hàng đợi, tự đồng bộ khi có mạng.
class BoundaryScreen extends ConsumerStatefulWidget {
  const BoundaryScreen({super.key, this.plot});
  final RefRow? plot;

  @override
  ConsumerState<BoundaryScreen> createState() => _BoundaryScreenState();
}

class _BoundaryScreenState extends ConsumerState<BoundaryScreen> {
  final _points = <GeoPoint>[];
  double? _worstAccuracy;
  BoundaryCheck? _check;
  bool _busy = false;
  List<List<GeoPoint>> _others = const [];

  // Trung tâm mặc định: Cần Thơ.
  static const _defaultCenter = LatLng(10.0341, 105.7880);

  @override
  void initState() {
    super.initState();
    final ring = ringFromGeoJson(
      widget.plot?.data['boundary'] ?? widget.plot?.data['geometry'],
    );
    if (ring != null) _points.addAll(ring);
    _loadOthers();
    _recheck();
  }

  Future<void> _loadOthers() async {
    final s = ref.read(appControllerProvider).services;
    if (s == null) return;
    final all = await s.refs.list(RefEntity.plot);
    final rings = <List<GeoPoint>>[];
    for (final p in all) {
      if (p.id == widget.plot?.id) continue;
      final r = ringFromGeoJson(p.data['boundary'] ?? p.data['geometry']);
      if (r != null && r.length >= 3) rings.add(r);
    }
    if (mounted) setState(() => _others = rings);
  }

  Future<void> _recheck() async {
    final s = ref.read(appControllerProvider).services;
    if (s == null) return;
    final c = await s.boundaries.check(
      _points,
      plotId: widget.plot?.id,
      cooperativeId: widget.plot?.data['cooperative_id'] as String?,
    );
    if (mounted) setState(() => _check = c);
  }

  void _add(GeoPoint p) {
    setState(() => _points.add(p));
    _recheck();
  }

  Future<void> _addGps() async {
    setState(() => _busy = true);
    try {
      final loc = await const GeolocatorLocationSource().current();
      if (!mounted) return;
      if (loc.fix == null) {
        showMessage(
          context,
          '${loc.status.label}. Hãy chọn điểm trên bản đồ vệ tinh.',
          error: true,
        );
        return;
      }
      if (loc.status == GpsStatus.weak) {
        showMessage(
          context,
          'Độ chính xác GPS thấp (±${loc.fix!.accuracyM?.round()} m). Có thể đánh dấu lại hoặc chọn trên bản đồ.',
        );
      }
      _worstAccuracy = max(_worstAccuracy ?? 0, loc.fix!.accuracyM ?? 0);
      _add(GeoPoint(loc.fix!.lat, loc.fix!.lon));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _save() async {
    final s = ref.read(appControllerProvider).services;
    final c = _check;
    if (s == null || c == null) return;
    var ack = false;
    if (c.needsAck) {
      ack =
          await showDialog<bool>(
            context: context,
            builder: (ctx) => AlertDialog(
              title: const Text('Cảnh báo chồng lấn'),
              content: Text(
                'Ranh giới bị chồng lấn với lô đã đăng ký: ${c.overlaps.map((o) => o.label).join(', ')}. '
                'Bạn đã kiểm tra thực địa và vẫn muốn lưu?',
              ),
              actions: [
                TextButton(
                  onPressed: () => Navigator.pop(ctx, false),
                  child: const Text('Chỉnh lại'),
                ),
                FilledButton(
                  onPressed: () => Navigator.pop(ctx, true),
                  child: const Text('Vẫn lưu'),
                ),
              ],
            ),
          ) ??
          false;
      if (!ack) return;
    }
    try {
      await s.boundaries.save(
        points: _points,
        method: _worstAccuracy == null
            ? BoundaryMethod.map
            : BoundaryMethod.gps,
        plotId: widget.plot?.id,
        cooperativeId: widget.plot?.data['cooperative_id'] as String?,
        acknowledgeOverlap: ack,
        worstAccuracyM: _worstAccuracy,
      );
      if (!mounted) return;
      showMessage(context, 'Đã lưu ranh giới. Sẽ tự đồng bộ khi có mạng.');
      Navigator.of(context).pop();
    } on BoundaryException catch (e) {
      if (mounted) showMessage(context, e.message, error: true);
    }
  }

  @override
  Widget build(BuildContext context) {
    final c = _check;
    final pts = [for (final p in _points) LatLng(p.lat, p.lon)];
    final center = pts.isNotEmpty ? pts.first : _defaultCenter;
    final cfg = ref.read(platformProvider).config;
    final valid = c?.validation.ok ?? false;
    return Scaffold(
      appBar: AppBar(
        title: Text(
          widget.plot == null
              ? 'Vẽ thửa mới'
              : 'Ranh giới: ${plotLabel(widget.plot!)}',
        ),
      ),
      body: Column(
        children: [
          Expanded(
            child: FlutterMap(
              options: MapOptions(
                initialCenter: center,
                initialZoom: pts.isNotEmpty ? 17 : 13,
                onTap: (_, ll) => _add(GeoPoint(ll.latitude, ll.longitude)),
              ),
              children: [
                // TODO(offline-map): gói tile ngoại tuyến theo địa bàn (US-MAP-03);
                // tile OSM trực tuyến chỉ là placeholder cho dev.
                TileLayer(
                  urlTemplate: cfg.tileUrlTemplate,
                  userAgentPackageName: 'vn.agrigreen.field',
                ),
                PolygonLayer(
                  polygons: [
                    for (final r in _others)
                      Polygon(
                        points: [for (final p in r) LatLng(p.lat, p.lon)],
                        color: AgColors.blue.withValues(alpha: 0.15),
                        borderColor: AgColors.blue,
                        borderStrokeWidth: 1.5,
                      ),
                    if (pts.length >= 3)
                      Polygon(
                        points: pts,
                        color:
                            (valid && (c?.overlaps.isEmpty ?? true)
                                    ? AgColors.g600
                                    : AgColors.red)
                                .withValues(alpha: 0.25),
                        borderColor: valid ? AgColors.g700 : AgColors.red,
                        borderStrokeWidth: 3,
                      ),
                  ],
                ),
                MarkerLayer(
                  markers: [
                    for (var i = 0; i < pts.length; i++)
                      Marker(
                        point: pts[i],
                        width: 30,
                        height: 30,
                        child: CircleAvatar(
                          backgroundColor: AgColors.g700,
                          child: Text(
                            '${i + 1}',
                            style: const TextStyle(
                              color: Colors.white,
                              fontSize: 13,
                            ),
                          ),
                        ),
                      ),
                  ],
                ),
                const SimpleAttributionWidget(
                  source: Text('© OpenStreetMap contributors'),
                ),
              ],
            ),
          ),
          Container(
            color: Colors.white,
            padding: const EdgeInsets.all(12),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text(
                  '${_points.length} điểm'
                  '${valid ? ' · Diện tích ≈ ${formatHa(c!.validation.areaHa)} ha' : ''}',
                  style: const TextStyle(
                    fontSize: 18,
                    fontWeight: FontWeight.w700,
                  ),
                ),
                if (c != null && !c.validation.ok && _points.isNotEmpty)
                  Text(
                    c.validation.errors.first.message,
                    style: const TextStyle(color: AgColors.red, fontSize: 15),
                  ),
                if (c != null && c.overlaps.isNotEmpty)
                  Text(
                    c.blocked
                        ? 'Chồng lấn với thửa của HTX/nông hộ khác — không được lưu.'
                        : 'Chồng lấn với: ${c.overlaps.map((o) => o.label).join(', ')}',
                    style: const TextStyle(color: AgColors.amber, fontSize: 15),
                  ),
                const SizedBox(height: 8),
                Row(
                  children: [
                    Expanded(
                      child: OutlinedButton.icon(
                        onPressed: _busy ? null : _addGps,
                        icon: const Icon(Icons.my_location),
                        label: const Text('Điểm GPS'),
                      ),
                    ),
                    const SizedBox(width: 8),
                    IconButton.outlined(
                      iconSize: 28,
                      tooltip: 'Hoàn tác',
                      onPressed: _points.isEmpty
                          ? null
                          : () {
                              setState(() => _points.removeLast());
                              _recheck();
                            },
                      icon: const Icon(Icons.undo),
                    ),
                    IconButton.outlined(
                      iconSize: 28,
                      tooltip: 'Xóa hết',
                      onPressed: _points.isEmpty
                          ? null
                          : () {
                              setState(() {
                                _points.clear();
                                _worstAccuracy = null;
                              });
                              _recheck();
                            },
                      icon: const Icon(Icons.delete_outline),
                    ),
                  ],
                ),
                const SizedBox(height: 8),
                FilledButton.icon(
                  onPressed: valid && !(c?.blocked ?? false) ? _save : null,
                  icon: const Icon(Icons.save),
                  label: const Text('Lưu ranh giới'),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}
