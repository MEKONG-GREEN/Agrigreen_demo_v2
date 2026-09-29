import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/crop_service.dart';
import '../../application/manager_service.dart';
import '../../domain/cropcycle.dart';
import '../providers.dart';

class _DashFilter {
  const _DashFilter(this.season, this.year);
  final String? season;
  final int? year;
  @override
  bool operator ==(Object other) =>
      other is _DashFilter && other.season == season && other.year == year;
  @override
  int get hashCode => Object.hash(season, year);
}

final _dashProvider = FutureProvider.autoDispose
    .family<DashboardData?, _DashFilter>(
      (ref, f) =>
          servicesOf(ref).manager
              .dashboard(seasonCode: f.season, seasonYear: f.year),
    );

final _seasonChoices = FutureProvider.autoDispose<List<Choice>>(
  (ref) => servicesOf(ref).crop.seasonsCatalog(),
);

/// Kg (chuỗi decimal) → "12,5 tấn".
String tonnes(Object? kg) {
  final g = parseScaled('${kg ?? '0'}', 3);
  if (g == null) return '—';
  // g = kg × 10³ ⇒ tấn × 10⁶; hiển thị 1 chữ số lẻ.
  return '${formatScaled(g, 6, digits: 1)} tấn';
}

/// Dashboard mini (HTX US-DASH-01): thẻ chỉ số + dự kiến / thực tế theo giống.
/// Đổi bộ lọc vụ → cập nhật ngay (AC-1). Cần mạng; mất mạng hiện bản gần nhất.
class DashboardScreen extends ConsumerStatefulWidget {
  const DashboardScreen({super.key});

  @override
  ConsumerState<DashboardScreen> createState() => _DashboardScreenState();
}

class _DashboardScreenState extends ConsumerState<DashboardScreen> {
  String? _season;
  late int _year = ref.read(appControllerProvider).services!.crop.today().year;

  @override
  Widget build(BuildContext context) {
    final seasons = ref.watch(_seasonChoices).value ?? const [];
    final dash = ref.watch(_dashProvider(_DashFilter(_season, _year)));
    return Scaffold(
      appBar: AppBar(title: const Text('Dashboard sản lượng')),
      body: ListView(
        padding: const EdgeInsets.all(12),
        children: [
          Wrap(
            spacing: 8,
            runSpacing: 8,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              ChoiceChip(
                label: const Text('Mọi vụ'),
                selected: _season == null,
                onSelected: (_) => setState(() => _season = null),
              ),
              for (final s in seasons)
                ChoiceChip(
                  label: Text(s.name),
                  selected: _season == s.code,
                  onSelected: (_) => setState(() => _season = s.code),
                ),
              IconButton(
                tooltip: 'Năm trước',
                icon: const Icon(Icons.chevron_left),
                onPressed: () => setState(() => _year--),
              ),
              Text('$_year', style: const TextStyle(fontSize: 18)),
              IconButton(
                tooltip: 'Năm sau',
                icon: const Icon(Icons.chevron_right),
                onPressed: () => setState(() => _year++),
              ),
            ],
          ),
          const SizedBox(height: 8),
          ...dash.when(
            loading: () => const [
              Padding(
                padding: EdgeInsets.all(24),
                child: Center(child: CircularProgressIndicator()),
              ),
            ],
            error: (e, _) => [
              Card(
                child: ListTile(
                  title: Text(e is ManagerException ? e.message : 'Lỗi: $e'),
                ),
              ),
            ],
            data: (d) => d == null
                ? const [
                    Card(
                      child: ListTile(
                        leading: Icon(Icons.wifi_off, color: AgColors.amber),
                        title: Text('Cần có mạng để tải dashboard lần đầu.'),
                      ),
                    ),
                  ]
                : _body(d),
          ),
        ],
      ),
    );
  }

  List<Widget> _body(DashboardData d) {
    final t = d.totals;
    Widget card(String label, String value) => Card(
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(label, style: const TextStyle(color: AgColors.mute)),
            const SizedBox(height: 4),
            Text(
              value,
              style: const TextStyle(fontSize: 20, fontWeight: FontWeight.w800),
            ),
          ],
        ),
      ),
    );
    String s(Object? v) => v == null ? '—' : '$v'.replaceAll('.', ',');
    return [
      if (d.stale)
        Padding(
          padding: const EdgeInsets.only(bottom: 8),
          child: Text(
            'Mất mạng — số liệu lúc ${formatLocal(d.fetchedAt)}',
            style: const TextStyle(color: AgColors.amber),
          ),
        ),
      GridView.count(
        crossAxisCount: 2,
        shrinkWrap: true,
        physics: const NeverScrollableScrollPhysics(),
        childAspectRatio: 1.8,
        children: [
          card('Tổng diện tích', '${s(t['area_ha'])} ha'),
          card('Nông hộ', s(t['households'])),
          card('Dự kiến', tonnes(t['expected_kg'])),
          card('Thực tế', tonnes(t['actual_kg'])),
          card('Năng suất thực tế', '${s(t['actual_yield_t_per_ha'])} t/ha'),
          card('Tiến độ SOP', '${s(t['sop_progress_pct'])} %'),
          card('Nhật ký đã duyệt', s(t['approved_logs'])),
          card('Nhật ký chờ duyệt', s(t['pending_logs'])),
        ],
      ),
      const SizedBox(height: 8),
      Text(
        'Dự kiến – thực tế theo giống',
        style: Theme.of(context).textTheme.titleMedium,
      ),
      for (final g in d.byVariety)
        Card(
          child: ListTile(
            title: Text('${g['key'] ?? ''}'),
            subtitle: Text(
              'Dự kiến ${tonnes(g['expected_kg'])} · Thực tế ${tonnes(g['actual_kg'])}'
              ' · ${s(g['area_ha'])} ha',
            ),
          ),
        ),
    ];
  }
}
