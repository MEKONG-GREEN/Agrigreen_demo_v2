import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/kn_collection_service.dart';
import '../../application/kn_menu_service.dart';
import '../../application/kn_report_service.dart';
import '../../domain/kn_report.dart';
import '../providers.dart';

/// Dịch vụ KN (ghi đè được trong kiểm thử widget).
final knReportServiceProvider = Provider<KnReportService>(
  (ref) => servicesOf(ref).knReports,
);
final knCollectionServiceProvider = Provider<KnCollectionService>(
  (ref) => servicesOf(ref).knData,
);
final knMenuServiceProvider = Provider<KnMenuService>(
  (ref) => servicesOf(ref).knMenu,
);

/// Menu Cấp Xã (máy chủ hoặc bản đệm ngoại tuyến).
final knMenuProvider = FutureProvider<KnMenu>(
  (ref) => ref.watch(knMenuServiceProvider).load(),
);

/// Số đếm badge, làm mới theo trạng thái đồng bộ.
final knBadgesProvider = FutureProvider<Map<String, int>>((ref) {
  ref.watch(syncStatusStreamProvider);
  return ref.watch(knMenuServiceProvider).badges();
});

Color knStatusColor(String s) => switch (s) {
  KnTaskStatus.overdue => AgColors.red,
  KnTaskStatus.adjustmentRequested => AgColors.amber,
  KnTaskStatus.submitted || KnTaskStatus.confirmed => AgColors.g700,
  KnTaskStatus.draft => AgColors.blue,
  KnTaskStatus.closed => AgColors.mute,
  _ => AgColors.ink,
};

Color knSyncColor(KnSyncState s) => switch (s) {
  KnSyncState.pending => AgColors.amber,
  KnSyncState.accepted => AgColors.g700,
  KnSyncState.conflict => AgColors.amber,
  KnSyncState.rejected => AgColors.red,
  KnSyncState.none => AgColors.mute,
};

String knDate(DateTime? t, {bool time = true}) {
  if (t == null) return '—';
  // Giờ Việt Nam (UTC+7) — không phụ thuộc múi giờ máy.
  final v = t.toUtc().add(const Duration(hours: 7));
  String two(int x) => x.toString().padLeft(2, '0');
  final d = '${two(v.day)}/${two(v.month)}/${v.year}';
  return time ? '$d ${two(v.hour)}:${two(v.minute)}' : d;
}

/// Chip trạng thái nhỏ.
class KnChip extends StatelessWidget {
  const KnChip(this.text, this.color, {super.key});
  final String text;
  final Color color;
  @override
  Widget build(BuildContext context) => Container(
    padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
    decoration: BoxDecoration(
      color: color.withValues(alpha: 0.12),
      borderRadius: BorderRadius.circular(10),
      border: Border.all(color: color.withValues(alpha: 0.5)),
    ),
    child: Text(
      text,
      style: TextStyle(color: color, fontSize: 13, fontWeight: FontWeight.w600),
    ),
  );
}

/// Dải "đang xem bản đệm" khi ngoại tuyến.
class KnOfflineBanner extends StatelessWidget {
  const KnOfflineBanner({super.key, this.text});
  final String? text;
  @override
  Widget build(BuildContext context) => Container(
    width: double.infinity,
    color: AgColors.amberBg,
    padding: const EdgeInsets.all(8),
    child: Text(
      text ?? 'Ngoại tuyến — đang xem dữ liệu đã lưu trên máy.',
      style: const TextStyle(fontSize: 14),
    ),
  );
}
