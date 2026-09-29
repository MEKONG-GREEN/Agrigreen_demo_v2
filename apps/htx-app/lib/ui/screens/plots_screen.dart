import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/crop_service.dart';
import '../../domain/cropcycle.dart';
import '../providers.dart';
import '../widgets.dart';
import 'farm_log_screen.dart';
import 'harvest_screen.dart';
import 'open_season_screen.dart';
import 'season_edit_screen.dart';
import 'supplement_screen.dart';

class PlotRow {
  const PlotRow(this.plot, this.season);
  final PlotItem plot;
  final Season? season;
}

final plotRowsProvider = FutureProvider.autoDispose<List<PlotRow>>((ref) async {
  final s = servicesOf(ref);
  return [
    for (final p in await s.crop.plots())
      PlotRow(p, await s.crop.openSeasonFor(p.ref)),
  ];
});

/// "Ruộng của tôi" (nông dân) / thửa của HTX (quản lý — ghi nhật ký hộ).
class PlotsScreen extends ConsumerWidget {
  const PlotsScreen({super.key, this.manager = false});
  final bool manager;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final rows = ref.watch(plotRowsProvider);
    return Scaffold(
      appBar: AppBar(
        title: Text(manager ? 'Lô ruộng của HTX' : 'Ruộng của tôi'),
      ),
      body: rows.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (list) => list.isEmpty
            ? const Padding(
                padding: EdgeInsets.all(24),
                child: Text(
                  'Chưa có lô ruộng trên máy. Kết nối mạng và bấm "Đồng bộ ngay" '
                  'ở trang chủ; nếu vẫn trống, liên hệ quản lý HTX để khai lô ruộng.',
                  style: TextStyle(fontSize: 17),
                ),
              )
            : ListView.builder(
                padding: const EdgeInsets.all(8),
                itemCount: list.length,
                itemBuilder: (_, i) {
                  final r = list[i];
                  final se = r.season;
                  return Card(
                    child: ListTile(
                      leading: Icon(
                        se == null ? Icons.crop_square : Icons.eco,
                        size: 36,
                        color: se == null ? AgColors.mute : AgColors.g700,
                      ),
                      title: Text(r.plot.title),
                      subtitle: Text(
                        [
                          if (r.plot.areaHa != null)
                            '${r.plot.areaHa!.replaceAll('.', ',')} ha',
                          se == null
                              ? 'Chưa mở vụ'
                              : 'Vụ ${se.seasonCode} ${se.seasonYear} · sạ ${displayDay(se.sowingDate)}'
                                    '${se.localPending ? ' · Chờ đồng bộ' : ''}',
                        ].join(' · '),
                      ),
                      trailing: const Icon(Icons.chevron_right),
                      onTap: () => Navigator.of(context)
                          .push(
                            MaterialPageRoute<void>(
                              builder: (_) =>
                                  PlotDetailScreen(plotRef: r.plot.ref),
                            ),
                          )
                          .then((_) => ref.invalidate(plotRowsProvider)),
                    ),
                  );
                },
              ),
      ),
    );
  }
}

class PlotDetail {
  const PlotDetail({
    required this.plot,
    required this.season,
    required this.sop,
    required this.logs,
    required this.harvestBlock,
    required this.harvestQueued,
  });
  final PlotItem plot;
  final Season? season;
  final SopVersion? sop;
  final List<LogEntry> logs;
  final String? harvestBlock;
  final bool harvestQueued;
}

final plotDetailProvider = FutureProvider.autoDispose
    .family<PlotDetail?, String>((ref, plotRef) async {
      final s = servicesOf(ref);
      final plot = await s.crop.plot(plotRef);
      if (plot == null) return null;
      final season = await s.crop.openSeasonFor(plotRef);
      return PlotDetail(
        plot: plot,
        season: season,
        sop: season == null ? null : await s.crop.sopFor(season),
        logs: season == null
            ? const []
            : await s.crop.logs(seasonRef: season.seasonRef),
        harvestBlock: season == null ? null : await s.crop.harvestBlock(season),
        harvestQueued:
            season != null && await s.crop.hasLocalHarvest(season.seasonRef),
      );
    });

/// Chi tiết lô: vụ đang mở, tiến độ SOP, nhật ký; mở vụ / ghi nhật ký / khai
/// sản lượng.
class PlotDetailScreen extends ConsumerWidget {
  const PlotDetailScreen({super.key, required this.plotRef});
  final String plotRef;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final d = ref.watch(plotDetailProvider(plotRef));
    void open(Widget w) =>
        Navigator.of(context)
            .push(MaterialPageRoute<void>(builder: (_) => w))
            .then((_) => ref.invalidate(plotDetailProvider(plotRef)));
    return Scaffold(
      appBar: AppBar(title: const Text('Lô ruộng')),
      body: d.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (d) {
          if (d == null) return const Center(child: Text('Không tìm thấy lô.'));
          final se = d.season;
          final today = ref.read(appControllerProvider).services!.crop.today();
          final step = se?.sowingDate == null
              ? null
              : d.sop?.currentStep(se!.sowingDate!, today);
          return ListView(
            padding: const EdgeInsets.all(12),
            children: [
              Card(
                child: Padding(
                  padding: const EdgeInsets.all(16),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        d.plot.title,
                        style: Theme.of(context).textTheme.titleLarge,
                      ),
                      if (d.plot.areaHa != null)
                        Text(
                          'Diện tích: ${d.plot.areaHa!.replaceAll('.', ',')} ha',
                        ),
                      const SizedBox(height: 8),
                      if (se == null)
                        const Text(
                          'Chưa mở vụ.',
                          style: TextStyle(fontSize: 17),
                        )
                      else ...[
                        Text(
                          'Vụ ${se.seasonCode} ${se.seasonYear} · giống ${se.varietyCode}',
                          style: const TextStyle(
                            fontSize: 17,
                            fontWeight: FontWeight.w700,
                          ),
                        ),
                        Text('Ngày sạ: ${displayDay(se.sowingDate)}'),
                        Text(
                          'Quy trình: ${d.sop?.name ?? 'sẽ tự gán theo giống khi đồng bộ'}',
                        ),
                        if (step != null) Text('Bước hiện tại: ${step.name}'),
                        if (harvestOpenDate(se, d.sop) != null)
                          Text(
                            'Được khai sản lượng từ: ${displayDay(harvestOpenDate(se, d.sop))}',
                          ),
                        if (se.localPending)
                          const Padding(
                            padding: EdgeInsets.only(top: 6),
                            child: Tag('Chờ đồng bộ'),
                          ),
                      ],
                    ],
                  ),
                ),
              ),
              const SizedBox(height: 8),
              if (se == null)
                FilledButton.icon(
                  onPressed: () => open(OpenSeasonScreen(plotRef: d.plot.ref)),
                  icon: const Icon(Icons.add_circle_outline),
                  label: const Text('Mở vụ mới'),
                )
              else ...[
                FilledButton.icon(
                  onPressed: () => open(FarmLogScreen(seasonRef: se.seasonRef)),
                  icon: const Icon(Icons.edit_note),
                  label: const Text('Ghi nhật ký'),
                ),
                const SizedBox(height: 8),
                OutlinedButton.icon(
                  onPressed: d.harvestQueued
                      ? null
                      : () => open(HarvestScreen(seasonRef: se.seasonRef)),
                  icon: const Icon(Icons.scale),
                  label: Text(
                    d.harvestQueued
                        ? 'Đã khai sản lượng — chờ đồng bộ'
                        : 'Khai sản lượng',
                  ),
                ),
                if (d.harvestBlock != null)
                  Padding(
                    padding: const EdgeInsets.only(top: 6),
                    child: Text(
                      d.harvestBlock!,
                      style: const TextStyle(color: AgColors.mute),
                    ),
                  ),
                // Sửa / hủy vụ mở nhầm (bản ghi bù, lý do ≥ 10 ký tự) — chỉ
                // khi chưa khai sản lượng (máy chủ: cropcycle.season_state).
                if (!d.harvestQueued && se.harvestId == null) ...[
                  const SizedBox(height: 8),
                  Row(
                    children: [
                      Expanded(
                        child: OutlinedButton.icon(
                          onPressed: () =>
                              open(SeasonEditScreen(seasonRef: se.seasonRef)),
                          icon: const Icon(Icons.edit_calendar),
                          label: const Text('Sửa vụ'),
                        ),
                      ),
                      const SizedBox(width: 8),
                      Expanded(
                        child: OutlinedButton.icon(
                          style: OutlinedButton.styleFrom(
                            foregroundColor: AgColors.red,
                          ),
                          onPressed: () async {
                            if (await confirmCancelSeason(context, ref, se)) {
                              ref.invalidate(plotDetailProvider(plotRef));
                            }
                          },
                          icon: const Icon(Icons.cancel_outlined),
                          label: const Text('Hủy vụ'),
                        ),
                      ),
                    ],
                  ),
                ],
              ],
              const SizedBox(height: 16),
              if (d.logs.isNotEmpty)
                Text(
                  'Nhật ký canh tác',
                  style: Theme.of(context).textTheme.titleMedium,
                ),
              for (final l in d.logs) LogCard(entry: l, onSupplement: open),
            ],
          );
        },
      ),
    );
  }
}

class LogCard extends StatelessWidget {
  const LogCard({super.key, required this.entry, required this.onSupplement});
  final LogEntry entry;
  final void Function(Widget) onSupplement;

  @override
  Widget build(BuildContext context) {
    final v = entry.view;
    final act = FarmActivity.parse(v.activityCode);
    final loc = locationLabel(v.locationStatus, v.gpsSource);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(
                  act == null ? Icons.note : activityIcon(act),
                  color: AgColors.g700,
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    activityLabel(v.activityCode),
                    style: const TextStyle(
                      fontSize: 17,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ),
                Tag(
                  entry.statusLabel,
                  color: v.status == 'approved'
                      ? AgColors.g700
                      : v.status == 'needs_supplement'
                      ? AgColors.red
                      : AgColors.amber,
                ),
              ],
            ),
            Text(
              'Lúc ${v.eventTime}',
              style: const TextStyle(color: AgColors.mute),
            ),
            if (v.note.isNotEmpty) Text(v.note),
            if (loc != null)
              Padding(padding: const EdgeInsets.only(top: 4), child: Tag(loc)),
            if (v.gpsSource.isNotEmpty)
              Text(
                gpsSourceLabel(v.gpsSource),
                style: const TextStyle(color: AgColors.mute, fontSize: 14),
              ),
            if (v.batchRef != null)
              const Text(
                'Ghi hàng loạt',
                style: TextStyle(color: AgColors.mute, fontSize: 14),
              ),
            if (v.reviewNote != null && v.reviewNote!.isNotEmpty)
              Text(
                'Quản lý HTX: ${v.reviewNote}',
                style: const TextStyle(color: AgColors.red),
              ),
            if (v.status == 'needs_supplement' && entry.local == null)
              Align(
                alignment: Alignment.centerRight,
                child: TextButton.icon(
                  onPressed: () => onSupplement(SupplementScreen(log: v)),
                  icon: const Icon(Icons.edit),
                  label: const Text('Bổ sung'),
                ),
              ),
          ],
        ),
      ),
    );
  }
}
