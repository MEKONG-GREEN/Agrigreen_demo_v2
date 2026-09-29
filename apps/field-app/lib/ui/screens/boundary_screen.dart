import 'package:agrigreen_mobile/data/reference/reference_repository.dart';
import 'package:agrigreen_mobile/ui/map/boundary_editor_screen.dart';
import 'package:agrigreen_mobile/ui/map/map_packs_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers.dart';

/// Vẽ ranh giới thửa của cán bộ KN: chạm bản đồ để chấm điểm hoặc lấy điểm GPS
/// tại chỗ. Màn vẽ dùng chung ở lõi (`BoundaryEditorScreen`); lưu được khi
/// ngoại tuyến, bản đồ đọc gói tile ngoại tuyến trước (US-MAP-03).
class BoundaryScreen extends ConsumerWidget {
  const BoundaryScreen({super.key, this.plot});
  final RefRow? plot;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final s = ref.services;
    final pf = ref.read(platformProvider);
    return BoundaryEditorScreen(
      boundaries: s.boundaries,
      location: pf.location,
      tileUrlTemplate: pf.config.tileUrlTemplate,
      tileStore: s.tiles.store,
      plot: plot,
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
    );
  }
}
