import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/cnts_period_service.dart';
import '../../domain/cnts_period.dart';
import '../providers.dart';
import 'cnts_catalog_screen.dart';
import 'cnts_entry_screen.dart';
import 'kn_providers.dart';

/// Dịch vụ biểu kỳ CNTS (ghi đè được trong kiểm thử widget).
final cntsPeriodServiceProvider = Provider<CntsPeriodService>(
  (ref) => servicesOf(ref).cntsPeriods,
);

Color cntsStatusColor(String s) => switch (s) {
  FormsStatus.overdue || FormsStatus.missed => AgColors.red,
  FormsStatus.returned ||
  FormsStatus.awaitingReopen ||
  FormsStatus.closedUnadjusted => AgColors.amber,
  FormsStatus.submitted || FormsStatus.accepted => AgColors.g700,
  _ => AgColors.ink,
};

/// Màn 1 — "Báo cáo kỳ CNTS" (W11.42, BRD CNTS UR-XA-06…09, 13): công việc
/// biểu kỳ của các xã / cơ sở giết mổ phụ trách từ entity `forms_task` đã pull
/// (xem, nhập được ngoại tuyến), lọc theo biểu, trạng thái tính lại "Trễ hạn"
/// theo giờ máy, trạng thái đồng bộ của lần gửi cuối.
class CntsTasksScreen extends ConsumerStatefulWidget {
  const CntsTasksScreen({super.key, this.title});
  final String? title;

  @override
  ConsumerState<CntsTasksScreen> createState() => _CntsTasksScreenState();
}

class _CntsTasksScreenState extends ConsumerState<CntsTasksScreen> {
  String? _code;
  List<FormsTaskRow>? _rows;
  Object? _error;

  CntsPeriodService get _svc => ref.read(cntsPeriodServiceProvider);

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final r = await _svc.list(code: _code);
      if (mounted) setState(() => (_rows = r, _error = null));
    } on Object catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  Future<void> _refresh() async {
    await ref
        .read(appControllerProvider)
        .services
        ?.sync
        .syncOnce(force: true, entities: CntsPeriodService.focusedEntities);
    await _load();
  }

  Future<void> _open(FormsTaskRow r) async {
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => CntsEntryScreen(taskId: r.task.id),
      ),
    );
    await _load();
  }

  @override
  Widget build(BuildContext context) {
    ref.listen(syncStatusStreamProvider, (_, _) => _load());
    final rows = _rows;
    return Scaffold(
      appBar: AppBar(
        title: Text(widget.title ?? 'Báo cáo kỳ CNTS'),
        actions: [
          IconButton(
            tooltip: 'Danh mục CNTS',
            icon: const Icon(Icons.menu_book_outlined),
            onPressed: () => Navigator.of(context).push(
              MaterialPageRoute<void>(
                builder: (_) => const CntsCatalogScreen(),
              ),
            ),
          ),
        ],
      ),
      body: _error != null && rows == null
          ? Center(child: Text('Lỗi: $_error'))
          : rows == null
          ? const Center(child: CircularProgressIndicator())
          : RefreshIndicator(
              onRefresh: _refresh,
              child: ListView(
                children: [
                  SingleChildScrollView(
                    scrollDirection: Axis.horizontal,
                    padding: const EdgeInsets.all(8),
                    child: Row(
                      children: [
                        for (final c in <String?>[null, ...CntsForms.order])
                          Padding(
                            padding: const EdgeInsets.only(right: 6),
                            child: ChoiceChip(
                              key: Key('cnts-filter-${c ?? 'all'}'),
                              label: Text(
                                c == null ? 'Tất cả' : CntsForms.short(c),
                              ),
                              selected: _code == c,
                              onSelected: (_) {
                                setState(() => _code = c);
                                _load();
                              },
                            ),
                          ),
                      ],
                    ),
                  ),
                  if (rows.isEmpty)
                    const Padding(
                      padding: EdgeInsets.all(24),
                      child: Text(
                        'Chưa có công việc — kéo xuống để đồng bộ.',
                        textAlign: TextAlign.center,
                      ),
                    ),
                  for (final r in rows)
                    ListTile(
                      key: Key('cnts-task-${r.task.id}'),
                      title: Text(
                        '${CntsForms.short(r.task.templateCode)} · ${r.task.label}',
                      ),
                      subtitle: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            '${r.task.unitKind == 'facility' ? 'Cơ sở GM' : 'Xã'} ${r.task.unit == r.task.parent ? r.task.unit : '${r.task.unit.substring(0, 8)}… · xã ${r.task.parent}'}'
                            ' · hạn ${knDate(r.task.dueAt)}',
                          ),
                          if (r.sync.state != FormsSyncState.none)
                            Text(
                              r.sync.label,
                              style: TextStyle(
                                color: r.sync.state == FormsSyncState.accepted
                                    ? AgColors.g700
                                    : AgColors.amber,
                              ),
                            ),
                        ],
                      ),
                      trailing: KnChip(
                        FormsStatus.label(r.status) +
                            (r.task.late ? ' · trễ' : ''),
                        cntsStatusColor(r.status),
                      ),
                      onTap: () => _open(r),
                    ),
                ],
              ),
            ),
    );
  }
}
