import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../data/reference/reference_repository.dart';
import '../../domain/geo/boundary.dart';
import '../providers.dart';
import '../theme.dart';
import 'boundary_screen.dart';

final plotsProvider = FutureProvider.autoDispose<List<RefRow>>(
  (ref) => servicesOf(ref).refs.list(RefEntity.plot),
);

String plotLabel(RefRow p) =>
    (p.data['label'] ?? p.data['code'] ?? p.id).toString();

class PlotsScreen extends ConsumerWidget {
  const PlotsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final plots = ref.watch(plotsProvider);
    Future<void> draw(RefRow? plot) async {
      await Navigator.of(context).push(
        MaterialPageRoute<void>(builder: (_) => BoundaryScreen(plot: plot)),
      );
      ref.invalidate(plotsProvider);
    }

    return Scaffold(
      appBar: AppBar(title: const Text('Thửa ruộng')),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: () => draw(null),
        icon: const Icon(Icons.add_location_alt),
        label: const Text('Vẽ thửa mới', style: TextStyle(fontSize: 17)),
      ),
      body: plots.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (list) => list.isEmpty
            ? const Center(
                child: Padding(
                  padding: EdgeInsets.all(24),
                  child: Text(
                    'Chưa có thửa nào trong địa bàn. Hãy đồng bộ khi có mạng.',
                    textAlign: TextAlign.center,
                    style: TextStyle(fontSize: 17),
                  ),
                ),
              )
            : ListView.separated(
                padding: const EdgeInsets.only(bottom: 96),
                itemCount: list.length,
                separatorBuilder: (_, _) => const Divider(height: 1),
                itemBuilder: (_, i) {
                  final p = list[i];
                  final ring = ringFromGeoJson(
                    p.data['boundary'] ?? p.data['geometry'],
                  );
                  final hasBoundary = ring != null && ring.length >= 3;
                  final area = p.data['area_ha'];
                  return ListTile(
                    tileColor: Colors.white,
                    leading: Icon(
                      hasBoundary ? Icons.check_circle : Icons.pending,
                      color: hasBoundary ? AgColors.g700 : AgColors.amber,
                      size: 32,
                    ),
                    title: Text(plotLabel(p)),
                    subtitle: Text(
                      hasBoundary
                          ? 'Đã có ranh giới${area != null ? ' · $area ha' : ''}'
                          : 'Chưa vẽ ranh giới',
                    ),
                    trailing: const Icon(Icons.chevron_right, size: 28),
                    onTap: () => draw(p),
                  );
                },
              ),
      ),
    );
  }
}
