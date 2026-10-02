import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/kn_menu_service.dart';
import '../../application/kn_report_service.dart';
import '../../domain/kn_report.dart';
import '../providers.dart';
import '../screens/sync_queue_screen.dart';
import '../screens/work_schedule_screen.dart';
import 'kn_context_picker_screen.dart';
import 'kn_cooperatives_screen.dart';
import 'kn_machines_screen.dart';
import 'kn_materials_screen.dart';
import 'kn_plots_screen.dart';
import 'kn_providers.dart';
import 'kn_tasks_screen.dart';

IconData _iconFor(String key) => switch (key) {
  KnMenuKey.congViec => Icons.notifications_active_outlined,
  KnMenuKey.lichCongTac => Icons.calendar_month_outlined,
  KnMenuKey.baoCaoTuan => Icons.description_outlined,
  KnMenuKey.baoCaoThang => Icons.calendar_today_outlined,
  KnMenuKey.baoCaoVu => Icons.grass_outlined,
  KnMenuKey.baoCaoCuaXa => Icons.bar_chart_outlined,
  KnMenuKey.baoCaoDotXuat => Icons.bolt_outlined,
  KnMenuKey.htx => Icons.groups_outlined,
  KnMenuKey.mayMoc => Icons.agriculture_outlined,
  KnMenuKey.thua => Icons.map_outlined,
  KnMenuKey.vatTu => Icons.inventory_2_outlined,
  _ => Icons.circle_outlined,
};

/// Màn hình cho một khóa menu Cấp Xã (null = mục mặc định, ở lại trang chủ).
Widget? knScreenFor(KnMenuEntry e) => switch (e.key) {
  KnMenuKey.congViec => null,
  KnMenuKey.lichCongTac => const WorkScheduleScreen(),
  KnMenuKey.baoCaoTuan => KnTasksScreen(
    title: e.label,
    initial: const KnTaskFilter(periodType: KnPeriodType.week, adHoc: false),
  ),
  KnMenuKey.baoCaoThang => KnTasksScreen(
    title: e.label,
    initial: const KnTaskFilter(periodType: KnPeriodType.month, adHoc: false),
  ),
  KnMenuKey.baoCaoVu => KnTasksScreen(
    title: e.label,
    initial: const KnTaskFilter(periodType: KnPeriodType.season, adHoc: false),
  ),
  KnMenuKey.baoCaoDotXuat => KnTasksScreen(
    title: e.label,
    initial: const KnTaskFilter(adHoc: true),
  ),
  KnMenuKey.baoCaoCuaXa => const KnCommuneReportsScreen(),
  KnMenuKey.htx => const KnCooperativesScreen(),
  KnMenuKey.mayMoc => const KnMachinesScreen(),
  KnMenuKey.thua => const KnPlotsScreen(),
  KnMenuKey.vatTu => const KnMaterialsScreen(),
  _ => null,
};

/// Trang chủ Cấp Xã (KN v7.7): menu từ `GET /v1/menu` (đệm ngoại tuyến, khóa
/// lạ bị ẩn), trang mặc định "Công việc cần làm", menu tài khoản "Đổi vai
/// trò/phòng ban". Màn KN cũ (chỉ đạo, thư viện, danh bạ trực, minh chứng MRV,
/// nhiệm vụ hỗ trợ, báo cáo hiện trường) không hiện ở bề mặt Trồng trọt.
class KnHomeScreen extends ConsumerWidget {
  const KnHomeScreen({super.key});

  Future<void> _switchContext(BuildContext context, WidgetRef ref) async {
    final ctl = ref.read(appControllerProvider.notifier);
    final choices = await ctl.contextChoices();
    if (!context.mounted) return;
    if (choices.isEmpty) {
      showMessage(
        context,
        'Không lấy được danh sách vai trò — cần kết nối mạng.',
        error: true,
      );
      return;
    }
    final cur = ref.read(appControllerProvider).services?.knContext;
    final ok = await Navigator.of(context).push<bool>(
      MaterialPageRoute(
        builder: (_) => KnContextPickerScreen(
          choices: choices,
          switching: true,
          currentKey: cur == null
              ? null
              : choices
                    .where(
                      (c) =>
                          c.assignmentId == cur.current?.assignmentId &&
                          c.departmentCode == cur.current?.departmentCode,
                    )
                    .firstOrNull
                    ?.key,
        ),
      ),
    );
    if (ok == true) {
      ref.invalidate(knMenuProvider);
      ref.invalidate(knBadgesProvider);
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final st = ref.watch(appControllerProvider);
    final ctx = st.kn?.context;
    final menu = ref.watch(knMenuProvider);
    final badges = ref.watch(knBadgesProvider).value ?? const <String, int>{};
    final entries = menu.value?.entries ?? const <KnMenuEntry>[];
    void open(KnMenuEntry e) {
      final w = knScreenFor(e);
      Navigator.of(context).pop(); // đóng ngăn kéo
      if (w != null) {
        Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => w));
      }
    }

    final groups = <String, List<KnMenuEntry>>{};
    for (final e in entries) {
      groups.putIfAbsent(e.group, () => []).add(e);
    }
    return Scaffold(
      appBar: AppBar(
        title: const Text('Công việc cần làm'),
        actions: [
          PopupMenuButton<String>(
            key: const ValueKey('kn-account'),
            icon: const Icon(Icons.account_circle, size: 28),
            onSelected: (v) async {
              switch (v) {
                case 'switch':
                  await _switchContext(context, ref);
                case 'queue':
                  await Navigator.of(context).push(
                    MaterialPageRoute<void>(
                      builder: (_) => const SyncQueueScreen(),
                    ),
                  );
                case 'sync':
                  await st.services?.sync.syncOnce(force: true);
                  ref.invalidate(knBadgesProvider);
                case 'lock':
                  await ref.read(appControllerProvider.notifier).lock();
              }
            },
            itemBuilder: (_) => const [
              PopupMenuItem(
                value: 'switch',
                child: Text('Đổi vai trò/phòng ban'),
              ),
              PopupMenuItem(value: 'sync', child: Text('Đồng bộ ngay')),
              PopupMenuItem(value: 'queue', child: Text('Hàng đợi đồng bộ')),
              PopupMenuItem(value: 'lock', child: Text('Khóa')),
            ],
          ),
        ],
      ),
      drawer: Drawer(
        child: ListView(
          children: [
            DrawerHeader(
              decoration: const BoxDecoration(color: AgColors.g700),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisAlignment: MainAxisAlignment.end,
                children: [
                  Text(
                    ctx?.title ?? 'Khuyến nông',
                    style: const TextStyle(
                      color: Colors.white,
                      fontSize: 18,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                  if (ctx != null)
                    Text(
                      ctx.orgName,
                      style: const TextStyle(color: Colors.white70),
                    ),
                  if (ctx != null && ctx.communes.isNotEmpty)
                    Text(
                      'Xã: ${ctx.communes.map((c) => c.name).join(', ')}',
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(color: Colors.white70),
                    ),
                  if (st.kn?.offline == true ||
                      (menu.value?.fromCache ?? false))
                    const Text(
                      'Ngoại tuyến — menu đã lưu',
                      style: TextStyle(color: Colors.amberAccent),
                    ),
                ],
              ),
            ),
            for (final g in groups.entries) ...[
              Padding(
                padding: const EdgeInsets.fromLTRB(16, 12, 16, 4),
                child: Text(
                  g.key,
                  style: const TextStyle(
                    color: AgColors.mute,
                    fontWeight: FontWeight.w700,
                  ),
                ),
              ),
              for (final e in g.value)
                ListTile(
                  key: ValueKey('kn-menu-${e.key}'),
                  enabled: e.enabled,
                  leading: Icon(_iconFor(e.key)),
                  title: Text(e.label),
                  trailing: (badges[e.badgeSource] ?? 0) > 0
                      ? KnChip('${badges[e.badgeSource]}', AgColors.red)
                      : null,
                  onTap: () => open(e),
                ),
            ],
          ],
        ),
      ),
      body: Column(
        children: [
          if (ctx?.supportSessionId != null)
            const KnOfflineBanner(
              text: 'Đang ở chế độ hỗ trợ (Admin nhập thay).',
            ),
          if (st.kn?.offline == true)
            const KnOfflineBanner(
              text: 'Ngoại tuyến — đang dùng vai trò đã lưu trên máy.',
            ),
          const Expanded(child: KnTasksView()),
        ],
      ),
    );
  }
}

/// "Báo cáo của xã" (FN-XA-03.07): danh sách báo cáo của các xã phụ trách
/// (trực tuyến; bản đệm khi ngoại tuyến), mở để xem / sửa nếu chưa chốt.
class KnCommuneReportsScreen extends ConsumerStatefulWidget {
  const KnCommuneReportsScreen({super.key});
  @override
  ConsumerState<KnCommuneReportsScreen> createState() => _CommuneReportsState();
}

class _CommuneReportsState extends ConsumerState<KnCommuneReportsScreen> {
  List<Map<String, Object?>>? _items;
  bool _cached = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final (items, cached) = await ref
          .read(knReportServiceProvider)
          .communeReports();
      if (mounted) {
        setState(() => (_items = items, _cached = cached, _error = null));
      }
    } on Object {
      if (mounted) {
        setState(() => _error = 'Không tải được — cần kết nối mạng.');
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final items = _items;
    return Scaffold(
      appBar: AppBar(title: const Text('Báo cáo của xã')),
      body: RefreshIndicator(
        onRefresh: _load,
        child: ListView.builder(
          itemCount: (items?.length ?? 0) + 1,
          itemBuilder: (ctx, i) {
            if (i == 0) {
              if (_cached) return const KnOfflineBanner();
              if (_error != null) {
                return Padding(
                  padding: const EdgeInsets.all(12),
                  child: Text(_error!),
                );
              }
              if (items == null) {
                return const Padding(
                  padding: EdgeInsets.all(24),
                  child: Center(child: CircularProgressIndicator()),
                );
              }
              if (items.isEmpty) {
                return const Padding(
                  padding: EdgeInsets.all(24),
                  child: Text(
                    'Chưa có báo cáo đã gửi.',
                    textAlign: TextAlign.center,
                  ),
                );
              }
              return const SizedBox.shrink();
            }
            final r = items![i - 1];
            final task = r['task'] is Map
                ? (r['task'] as Map).cast<String, Object?>()
                : r;
            final status = '${task['display_status'] ?? task['status'] ?? ''}';
            return ListTile(
              title: Text(
                '${task['form_code'] ?? ''} · ${task['form_name'] ?? r['form_name'] ?? ''}',
              ),
              subtitle: Text(
                '${task['commune_name'] ?? task['commune_code'] ?? ''} · '
                '${task['period_label'] ?? ''} · gửi ${knDate(DateTime.tryParse('${task['submitted_at']}'))}',
              ),
              trailing: KnChip(
                KnTaskStatus.label(status),
                knStatusColor(status),
              ),
            );
          },
        ),
      ),
    );
  }
}
