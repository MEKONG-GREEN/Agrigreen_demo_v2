import 'dart:math';

import 'package:flutter/material.dart';
import 'package:flutter_map/flutter_map.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:latlong2/latlong.dart';

import '../../application/boundary_service.dart';
import '../../data/photo/location_source.dart';
import '../../data/reference/reference_repository.dart';
import '../../domain/geo/boundary.dart';
import '../../domain/geo/tiles.dart';
import '../map/offline_tile_provider.dart';
import '../providers.dart';
import '../theme.dart';
import '../widgets/common.dart';
import 'map_packs_screen.dart';
import 'plots_screen.dart';

/// Vẽ ranh giới thửa: chạm bản đồ để chấm điểm hoặc lấy điểm GPS tại chỗ.
/// Lưu được khi ngoại tuyến — bản ghi vào hàng đợi, tự đồng bộ khi có mạng.
/// Bản đồ đọc gói tile ngoại tuyến trước (US-MAP-03).
class BoundaryScreen extends ConsumerStatefulWidget {
  const BoundaryScreen({super.key, this.plot});
  final RefRow? plot;

  @override
  ConsumerState<BoundaryScreen> createState() => _BoundaryScreenState();
}

class _BoundaryScreenState extends ConsumerState<BoundaryScreen> {
  final _points = <GeoPoint>[];
  final _map = MapController();
  final _declared = TextEditingController();
  double? _worstAccuracy;
  BoundaryCheck? _check;
  bool _busy = false;
  List<List<GeoPoint>> _others = const [];
  List<RefRow> _communes = const [];
  String? _commune;
  OfflineFirstTileProvider? _tiles;

  // Trung tâm mặc định: Cần Thơ.
  static const _defaultCenter = LatLng(10.0341, 105.7880);

  String? get _coop =>
      (widget.plot?.data['cooperative_ref'] ??
              widget.plot?.data['cooperative_id'])
          as String?;

  @override
  void initState() {
    super.initState();
    final ring = ringFromGeoJson(
      widget.plot?.data['boundary'] ?? widget.plot?.data['geometry'],
    );
    if (ring != null) _points.addAll(ring);
    final s = ref.read(appControllerProvider).services;
    _tiles = OfflineFirstTileProvider(
      store: s?.tiles.store,
      urlTemplate: ref.read(platformProvider).config.tileUrlTemplate,
    );
    _load();
    _recheck();
  }

  @override
  void dispose() {
    _declared.dispose();
    _map.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    final s = ref.read(appControllerProvider).services;
    if (s == null) return;
    final all = await s.refs.list(RefEntity.plot);
    final rings = <List<GeoPoint>>[];
    for (final p in all) {
      if (p.id == widget.plot?.id) continue;
      final r = ringFromGeoJson(p.data['boundary'] ?? p.data['geometry']);
      if (r != null && r.length >= 3) rings.add(r);
    }
    final units = (await s.refs.list(RefEntity.adminUnit))
        .where(
          (u) =>
              u.data['level'] == null ||
              u.data['level'] == 'commune' ||
              u.data['level'] == 'xa',
        )
        .toList();
    if (!mounted) return;
    setState(() {
      _others = rings;
      _communes = units;
      if (widget.plot == null && units.length == 1) {
        _commune = _codeOf(units[0]);
      }
    });
  }

  static String _codeOf(RefRow u) => (u.data['code'] ?? u.id).toString();

  Future<void> _recheck() async {
    final s = ref.read(appControllerProvider).services;
    if (s == null) return;
    final c = await s.boundaries.check(
      _points,
      plotId: widget.plot?.id,
      cooperativeId: _coop,
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
      final loc = await ref.read(platformProvider).location.current();
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

  Future<bool> _confirm(String title, String body) async =>
      await showDialog<bool>(
        context: context,
        builder: (ctx) => AlertDialog(
          title: Text(title),
          content: Text(body),
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

  Future<String?> _askReason() async {
    final ctl = TextEditingController();
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Lý do sửa ranh giới'),
        content: TextField(
          controller: ctl,
          maxLines: 2,
          decoration: const InputDecoration(
            labelText: 'Bắt buộc, ít nhất 10 ký tự',
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Hủy'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Lưu'),
          ),
        ],
      ),
    );
    final r = ctl.text;
    ctl.dispose();
    return ok == true ? r : null;
  }

  Future<void> _save() async {
    final s = ref.read(appControllerProvider).services;
    final c = _check;
    if (s == null || c == null) return;
    double? declared;
    try {
      declared = parseDeclaredHa(_declared.text);
    } on BoundaryException catch (e) {
      showMessage(context, e.message, error: true);
      return;
    }
    var ack = false;
    if (c.needsAck) {
      ack = await _confirm(
        'Cảnh báo chồng lấn',
        'Ranh giới bị chồng lấn với lô đã đăng ký: ${c.overlaps.map((o) => o.label).join(', ')}. '
            'Bạn đã kiểm tra thực địa và vẫn muốn lưu?',
      );
      if (!ack) return;
    }
    if (declared != null && c.validation.ok) {
      final dev = areaDeviationPct(c.validation.areaHa, declared);
      if (dev > areaDeviationConfirmPct) {
        final ok = await _confirm(
          'Lệch diện tích khai báo',
          'Diện tích vẽ ${formatHa(c.validation.areaHa)} ha lệch '
              '${dev.toStringAsFixed(1)} % so với khai báo '
              '${declared.toStringAsFixed(2)} ha. Vẫn lưu?',
        );
        if (!ok) return;
        ack = true;
      }
    }
    String? reason;
    if (widget.plot?.data['boundary_id'] != null) {
      reason = await _askReason();
      if (reason == null) return;
    }
    try {
      await s.boundaries.save(
        points: _points,
        method: _worstAccuracy == null
            ? BoundaryMethod.map
            : BoundaryMethod.gps,
        plotId: widget.plot?.id,
        cooperativeId: _coop,
        communeCode: _commune,
        declaredAreaHa: declared,
        correctionReason: reason,
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

  Future<void> _downloadView() async {
    final cam = _map.camera;
    final b = cam.visibleBounds;
    final z = cam.zoom.round();
    final bbox = GeoBBox(
      minLat: b.south,
      minLon: b.west,
      maxLat: b.north,
      maxLon: b.east,
    );
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => MapPacksScreen(
          proposal: bbox,
          minZoom: max(z - 2, 10),
          maxZoom: min(z + 2, 18),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final c = _check;
    final pts = [for (final p in _points) LatLng(p.lat, p.lon)];
    final center = pts.isNotEmpty ? pts.first : _defaultCenter;
    final valid = c?.validation.ok ?? false;
    return Scaffold(
      appBar: AppBar(
        title: Text(
          widget.plot == null
              ? 'Vẽ thửa mới'
              : 'Ranh giới: ${plotLabel(widget.plot!)}',
        ),
        actions: [
          IconButton(
            tooltip: 'Tải bản đồ ngoại tuyến vùng này',
            iconSize: 28,
            icon: const Icon(Icons.download_for_offline),
            onPressed: _downloadView,
          ),
        ],
      ),
      body: Column(
        children: [
          Expanded(
            child: FlutterMap(
              mapController: _map,
              options: MapOptions(
                initialCenter: center,
                initialZoom: pts.isNotEmpty ? 17 : 13,
                onTap: (_, ll) => _add(GeoPoint(ll.latitude, ll.longitude)),
              ),
              children: [
                TileLayer(
                  urlTemplate: ref
                      .read(platformProvider)
                      .config
                      .tileUrlTemplate,
                  userAgentPackageName: 'vn.agrigreen.field',
                  tileProvider: _tiles,
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
                    if (widget.plot == null)
                      Expanded(
                        child: DropdownButtonFormField<String>(
                          initialValue: _commune,
                          isExpanded: true,
                          decoration: const InputDecoration(labelText: 'Xã'),
                          items: [
                            for (final u in _communes)
                              DropdownMenuItem(
                                value: _codeOf(u),
                                child: Text(
                                  (u.data['name'] ??
                                          u.data['label'] ??
                                          _codeOf(u))
                                      .toString(),
                                ),
                              ),
                          ],
                          onChanged: (v) => setState(() => _commune = v),
                        ),
                      ),
                    if (widget.plot == null) const SizedBox(width: 8),
                    Expanded(
                      child: TextField(
                        controller: _declared,
                        keyboardType: const TextInputType.numberWithOptions(
                          decimal: true,
                        ),
                        decoration: const InputDecoration(
                          labelText: 'Diện tích khai báo (ha)',
                        ),
                      ),
                    ),
                  ],
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
