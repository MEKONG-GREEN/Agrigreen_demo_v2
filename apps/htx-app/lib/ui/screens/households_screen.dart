import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/manager_service.dart';
import '../providers.dart';
import 'plots_screen.dart';

final _householdsProvider = FutureProvider.autoDispose<List<HouseholdItem>>(
  (ref) => servicesOf(ref).manager.households(),
);

/// Nông hộ của HTX (quản lý HTX). Chạm → lô của hộ → mở vụ hộ / ghi nhật ký
/// hộ (HTX US-SEASON-02, US-LOG-02).
class HouseholdsScreen extends ConsumerWidget {
  const HouseholdsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final list = ref.watch(_householdsProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Nông hộ của HTX')),
      body: list.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (items) => items.isEmpty
            ? const Padding(
                padding: EdgeInsets.all(24),
                child: Text(
                  'Chưa có nông hộ trên máy — đồng bộ khi có mạng.',
                  style: TextStyle(fontSize: 17),
                ),
              )
            : ListView(
                padding: const EdgeInsets.all(8),
                children: [
                  for (final h in items)
                    Card(
                      child: ListTile(
                        leading: const Icon(
                          Icons.home_work,
                          color: AgColors.g700,
                          size: 32,
                        ),
                        title: Text(h.label),
                        subtitle: Text(
                          [
                            if (h.code != null) h.code!,
                            '${h.plots} lô ruộng',
                          ].join(' · '),
                        ),
                        trailing: const Icon(Icons.chevron_right),
                        onTap: () => Navigator.of(context).push(
                          MaterialPageRoute<void>(
                            builder: (_) => HouseholdPlotsScreen(household: h),
                          ),
                        ),
                      ),
                    ),
                ],
              ),
      ),
    );
  }
}

final _hhPlotsProvider = FutureProvider.autoDispose
    .family<List<PlotRow>, String>((ref, hh) async {
      final s = servicesOf(ref);
      return [
        for (final p in await s.manager.plotsOf(hh))
          PlotRow(p, await s.crop.openSeasonFor(p.ref)),
      ];
    });

class HouseholdPlotsScreen extends ConsumerWidget {
  const HouseholdPlotsScreen({super.key, required this.household});
  final HouseholdItem household;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final rows = ref.watch(_hhPlotsProvider(household.ref));
    return Scaffold(
      appBar: AppBar(title: Text(household.label)),
      body: rows.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (list) => ListView(
          padding: const EdgeInsets.all(8),
          children: [
            if (list.isEmpty) const Text('Hộ chưa có lô ruộng trên máy.'),
            for (final r in list)
              Card(
                child: ListTile(
                  title: Text(r.plot.title),
                  subtitle: Text(
                    r.season == null
                        ? 'Chưa mở vụ — chạm để mở vụ hộ'
                        : 'Vụ ${r.season!.seasonCode} ${r.season!.seasonYear} — chạm để ghi nhật ký hộ',
                  ),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: () => Navigator.of(context)
                      .push(
                        MaterialPageRoute<void>(
                          builder: (_) => PlotDetailScreen(plotRef: r.plot.ref),
                        ),
                      )
                      .then(
                        (_) => ref.invalidate(_hhPlotsProvider(household.ref)),
                      ),
                ),
              ),
          ],
        ),
      ),
    );
  }
}
