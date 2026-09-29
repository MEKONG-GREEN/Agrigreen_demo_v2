import 'dart:async';
import 'dart:math';

import 'package:flutter/material.dart';
import 'package:flutter_map/flutter_map.dart';
import 'package:latlong2/latlong.dart';

import '../../application/boundary_service.dart';
import '../../data/map/tile_store.dart';
import '../../data/photo/location_source.dart';
import '../../data/reference/reference_repository.dart';
import '../../domain/geo/boundary.dart';
import '../../domain/geo/tiles.dart';
import '../theme.dart';
import '../widgets/common.dart';
import 'offline_tile_provider.dart';

/// Nhãn của thửa trên máy.
String plotRowLabel(RefRow p) =>
    (p.data['label'] ?? p.data['name'] ?? p.data['code'] ?? p.id).toString();

/// Vẽ ranh giới thửa, dùng chung cho app hiện trường (cán bộ KN) và App HTX
/// (quản lý HTX):
/// - **đi GPS**: đứng tại từng góc ruộng, bấm "Điểm GPS" (KN US-PLOT-01, HTX
///   US-PLOT-02); GPS yếu → cảnh báo, cho đánh dấu lại hoặc chuyển sang chọn
///   trên bản đồ (AC-4);
/// - **chọn trên bản đồ**: chạm các góc trên nền bản đồ (HTX US-PLOT-03). Nền
///   là mẫu tile cấu hình (`AG_TILE_URL`); ảnh vệ tinh chờ nhà cung cấp (RB-7).
///
/// Cùng ràng buộc cho cả hai cách: ≥ 4 điểm không thẳng hàng, không tự cắt,
/// chồng lấn, lệch diện tích khai báo > `geo.area_deviation_max_pct`.
/// Lưu được khi ngoại tuyến (bản ghi vào hàng đợi, tự đồng bộ khi có mạng);
/// điểm đang vẽ dở được giữ trong CSDL mã hóa (HTX US-PLOT-05). Bản đồ đọc
/// gói tile ngoại tuyến trước ([OfflineFirstTileProvider]).
class BoundaryEditorScreen extends StatefulWidget {
  const BoundaryEditorScreen({
    super.key,
    required this.boundaries,
    required this.location,
    required this.tileUrlTemplate,
    this.tileStore,
    this.tileProvider,
    this.plot,
    this.userAgent = 'vn.agrigreen.field',
    this.onDownloadView,
    this.modeToggle = false,
    this.initialMode = BoundaryMethod.gps,
  });

  final BoundaryService boundaries;
  final LocationSource location;
  final String tileUrlTemplate;
  final TileStore? tileStore;

  /// Thay nguồn tile (kiểm thử); mặc định [OfflineFirstTileProvider].
  final TileProvider? tileProvider;

  /// Thửa đang sửa; null = thửa mới.
  final RefRow? plot;
  final String userAgent;

  /// Tải gói bản đồ ngoại tuyến cho vùng đang xem; null = ẩn nút.
  final Future<void> Function(GeoBBox bbox, int minZoom, int maxZoom)?
  onDownloadView;

  /// true = hai chế độ tách biệt (App HTX): ở chế độ đi GPS, chạm bản đồ không
  /// thêm điểm. false = chạm bản đồ và điểm GPS dùng cùng lúc (app hiện trường).
  final bool modeToggle;
  final BoundaryMethod initialMode;

  @override
  State<BoundaryEditorScreen> createState() => _BoundaryEditorScreenState();
}

class _BoundaryEditorScreenState extends State<BoundaryEditorScreen> {
  final _points = <GeoPoint>[];

  /// Sai số từng điểm (null = chọn trên bản đồ), cùng thứ tự [_points].
  final _acc = <double?>[];
  final _map = MapController();
  final _declared = TextEditingController();

  /// Lý do sửa ranh giới — sống cùng màn (hộp thoại còn dùng khi đang đóng).
  final _reason = TextEditingController();
  BoundaryCheck? _check;
  bool _busy = false;
  List<List<GeoPoint>> _others = const [];
  List<RefRow> _communes = const [];
  String? _commune;
  String? _coop;
  bool _allowed = true;
  double _limit = areaDeviationConfirmPct;
  late BoundaryMethod _mode = widget.initialMode;
  TileProvider? _tiles;

  /// Sai số (m) của điểm GPS yếu vừa đánh dấu — hiện cảnh báo (AC-4).
  double? _weakAccuracy;
  String? _restored;

  // Trung tâm mặc định: Cần Thơ.
  static const _defaultCenter = LatLng(10.0341, 105.7880);

  BoundaryService get _svc => widget.boundaries;
  String? get _plotId => widget.plot?.id;

  @override
  void initState() {
    super.initState();
    _coop =
        (widget.plot?.data['cooperative_ref'] ??
                widget.plot?.data['cooperative_id'])
            as String?;
    final ring = ringFromGeoJson(
      widget.plot?.data['boundary'] ?? widget.plot?.data['geometry'],
    );
    if (ring != null) {
      _points.addAll(ring);
      _acc.addAll(List.filled(ring.length, null));
    }
    _tiles =
        widget.tileProvider ??
        OfflineFirstTileProvider(
          store: widget.tileStore,
          urlTemplate: widget.tileUrlTemplate,
          userAgent: widget.userAgent,
        );
    unawaited(_load());
  }

  @override
  void dispose() {
    _declared.dispose();
    _reason.dispose();
    _map.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    final draft = await _svc.loadDraft(_plotId);
    final rings = await _svc.otherRings(exceptPlotId: _plotId);
    final units = await _svc.communes();
    final limit = await _svc.deviationLimit();
    final plot = widget.plot;
    final allowed = plot == null || await _svc.canEdit(plot);
    _coop ??= plot == null ? await _svc.ownCooperative() : null;
    if (!mounted) return;
    setState(() {
      _others = rings;
      _communes = units;
      _limit = limit;
      _allowed = allowed;
      if (plot == null && units.length == 1) {
        _commune = _codeOf(units[0]);
      }
      if (draft != null) {
        _points
          ..clear()
          ..addAll(draft.points);
        _acc
          ..clear()
          ..addAll([
            for (var i = 0; i < draft.points.length; i++)
              i < draft.accuracies.length ? draft.accuracies[i] : null,
          ]);
        if (draft.method != null && widget.modeToggle) _mode = draft.method!;
        _restored =
            'Đã khôi phục ${draft.points.length} điểm đang vẽ dở trên máy.';
      }
    });
    if (!allowed && mounted) showMessage(context, msgPlotNotOwned, error: true);
    await _recheck(persist: false);
  }

  static String _codeOf(RefRow u) => (u.data['code'] ?? u.id).toString();

  Future<void> _recheck({bool persist = true}) async {
    if (persist) {
      // Lưu tạm ngay mỗi điểm (không cần mạng) — HTX US-PLOT-05 AC-1.
      await _svc.saveDraft(
        _plotId,
        BoundaryDraft(
          List.of(_points),
          accuracies: List.of(_acc),
          method: _mode,
        ),
      );
    }
    final c = await _svc.check(_points, plotId: _plotId, cooperativeId: _coop);
    if (mounted) setState(() => _check = c);
  }

  double? get _worstAccuracy =>
      BoundaryDraft(_points, accuracies: _acc).worstAccuracyM;

  void _add(GeoPoint p, [double? accuracyM]) {
    setState(() {
      _points.add(p);
      _acc.add(accuracyM);
      _restored = null;
    });
    unawaited(_recheck());
  }

  void _onMapTap(LatLng ll) {
    if (!_allowed) return;
    if (widget.modeToggle && _mode != BoundaryMethod.map) return;
    _add(GeoPoint(ll.latitude, ll.longitude));
  }

  Future<void> _addGps() async {
    setState(() => _busy = true);
    try {
      final loc = await widget.location.current();
      if (!mounted) return;
      if (loc.fix == null) {
        showMessage(
          context,
          '${loc.status.label}. Hãy chọn điểm trên bản đồ.',
          error: true,
        );
        if (widget.modeToggle) setState(() => _mode = BoundaryMethod.map);
        return;
      }
      final acc = loc.fix!.accuracyM;
      setState(
        () => _weakAccuracy = loc.status == GpsStatus.weak ? (acc ?? 0) : null,
      );
      _add(GeoPoint(loc.fix!.lat, loc.fix!.lon), acc ?? 0);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  /// GPS yếu: bỏ điểm vừa lấy và lấy lại tại chỗ.
  Future<void> _retakeGps() async {
    if (_points.isNotEmpty) {
      setState(() {
        _points.removeLast();
        _acc.removeLast();
      });
    }
    setState(() => _weakAccuracy = null);
    await _addGps();
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
    final ctl = _reason;
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
    return ok == true ? ctl.text : null;
  }

  Future<void> _save() async {
    final c = _check;
    if (c == null) return;
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
      if (dev > _limit) {
        final ok = await _confirm(
          'Lệch diện tích khai báo',
          'Diện tích vẽ ${formatHa(c.validation.areaHa)} ha lệch '
              '${dev.toStringAsFixed(1)} % so với khai báo '
              '${declared.toStringAsFixed(2)} ha (ngưỡng '
              '${_limit.toStringAsFixed(0)} %). Vẫn lưu?',
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
      await _svc.save(
        points: _points,
        method: _worstAccuracy == null
            ? BoundaryMethod.map
            : BoundaryMethod.gps,
        plotId: _plotId,
        cooperativeId: _coop,
        communeCode: _commune,
        declaredAreaHa: declared,
        correctionReason: reason,
        acknowledgeOverlap: ack,
        worstAccuracyM: _worstAccuracy,
      );
      if (!mounted) return;
      showMessage(context, 'Đã lưu ranh giới. Sẽ tự đồng bộ khi có mạng.');
      Navigator.of(context).pop(true);
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
    await widget.onDownloadView!(bbox, max(z - 2, 10), min(z + 2, 18));
  }

  void _clear() {
    setState(() {
      _points.clear();
      _acc.clear();
      _weakAccuracy = null;
    });
    unawaited(_recheck());
  }

  void _undo() {
    setState(() {
      _points.removeLast();
      _acc.removeLast();
    });
    unawaited(_recheck());
  }

  @override
  Widget build(BuildContext context) {
    final c = _check;
    final pts = [for (final p in _points) LatLng(p.lat, p.lon)];
    final center = pts.isNotEmpty ? pts.first : _defaultCenter;
    final valid = c?.validation.ok ?? false;
    final gpsActive = !widget.modeToggle || _mode == BoundaryMethod.gps;
    return Scaffold(
      appBar: AppBar(
        title: Text(
          widget.plot == null
              ? 'Vẽ thửa mới'
              : 'Ranh giới: ${plotRowLabel(widget.plot!)}',
        ),
        actions: [
          if (widget.onDownloadView != null)
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
          if (widget.modeToggle)
            Padding(
              padding: const EdgeInsets.fromLTRB(8, 8, 8, 0),
              child: SegmentedButton<BoundaryMethod>(
                segments: const [
                  ButtonSegment(
                    value: BoundaryMethod.gps,
                    icon: Icon(Icons.directions_walk),
                    label: Text('Đi GPS'),
                  ),
                  ButtonSegment(
                    value: BoundaryMethod.map,
                    icon: Icon(Icons.touch_app),
                    label: Text('Chọn trên bản đồ'),
                  ),
                ],
                selected: {_mode},
                onSelectionChanged: (s) => setState(() => _mode = s.first),
              ),
            ),
          Expanded(
            child: FlutterMap(
              mapController: _map,
              options: MapOptions(
                initialCenter: center,
                initialZoom: pts.isNotEmpty ? 17 : 13,
                onTap: (_, ll) => _onMapTap(ll),
              ),
              children: [
                TileLayer(
                  urlTemplate: widget.tileUrlTemplate,
                  userAgentPackageName: widget.userAgent,
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
                if (!_allowed)
                  const Text(
                    msgPlotNotOwned,
                    style: TextStyle(color: AgColors.red, fontSize: 15),
                  ),
                if (_restored != null)
                  Text(
                    _restored!,
                    style: const TextStyle(color: AgColors.g700, fontSize: 15),
                  ),
                if (_weakAccuracy != null && gpsActive)
                  Card(
                    color: AgColors.amberBg,
                    margin: const EdgeInsets.only(bottom: 8),
                    child: Padding(
                      padding: const EdgeInsets.all(8),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [
                          Text(
                            'Độ chính xác GPS thấp (±${_weakAccuracy!.round()} m). '
                            'Có thể đánh dấu lại hoặc chọn trên bản đồ.',
                            style: const TextStyle(fontSize: 15),
                          ),
                          Wrap(
                            alignment: WrapAlignment.end,
                            spacing: 8,
                            children: [
                              TextButton(
                                onPressed: _busy ? null : _retakeGps,
                                child: const Text('Đánh dấu lại'),
                              ),
                              TextButton(
                                onPressed: () => setState(() {
                                  _weakAccuracy = null;
                                  _mode = BoundaryMethod.map;
                                }),
                                child: const Text('Chọn trên bản đồ'),
                              ),
                            ],
                          ),
                        ],
                      ),
                    ),
                  ),
                Text(
                  '${_points.length} điểm'
                  '${valid ? ' · Diện tích ≈ ${formatHa(c!.validation.areaHa)} ha' : ''}',
                  style: const TextStyle(
                    fontSize: 18,
                    fontWeight: FontWeight.w700,
                  ),
                ),
                if (widget.modeToggle && _mode == BoundaryMethod.map)
                  const Text(
                    'Chạm lần lượt các góc thửa trên bản đồ. Nền bản đồ theo '
                    'cấu hình; ảnh vệ tinh sẽ có khi chọn được nhà cung cấp.',
                    style: TextStyle(color: AgColors.mute, fontSize: 14),
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
                    if (gpsActive)
                      Expanded(
                        child: OutlinedButton.icon(
                          onPressed: _busy || !_allowed ? null : _addGps,
                          icon: const Icon(Icons.my_location),
                          label: const Text('Điểm GPS'),
                        ),
                      )
                    else
                      const Spacer(),
                    const SizedBox(width: 8),
                    IconButton.outlined(
                      iconSize: 28,
                      tooltip: 'Hoàn tác',
                      onPressed: _points.isEmpty ? null : _undo,
                      icon: const Icon(Icons.undo),
                    ),
                    IconButton.outlined(
                      iconSize: 28,
                      tooltip: 'Xóa hết',
                      onPressed: _points.isEmpty ? null : _clear,
                      icon: const Icon(Icons.delete_outline),
                    ),
                  ],
                ),
                const SizedBox(height: 8),
                FilledButton.icon(
                  onPressed: valid && _allowed && !(c?.blocked ?? false)
                      ? _save
                      : null,
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
