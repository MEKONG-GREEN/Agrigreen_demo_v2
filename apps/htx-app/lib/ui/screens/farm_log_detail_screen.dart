import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/crop_service.dart';
import '../providers.dart';
import 'plots_screen.dart' show LogCard;

/// Nhật ký theo id phiên bản; phiên bản đã bị thay → phiên bản mới nhất cùng
/// `log_ref` (máy giữ phiên bản mới — sync-contract `farm_log`).
final _logProvider = FutureProvider.autoDispose.family<LogEntry?, String>((
  ref,
  id,
) async {
  final logs = await servicesOf(ref).crop.logs();
  final exact = logs.where((l) => l.view.id == id).firstOrNull;
  if (exact != null) return exact;
  return logs.where((l) => l.view.logRef == id).firstOrNull;
});

/// Mở từ thông báo `farm_log_reviewed` (HTX US-LOG-05 AC-3): trạng thái duyệt,
/// ghi chú của quản lý HTX và nút "Bổ sung" khi bị yêu cầu bổ sung.
class FarmLogDetailScreen extends ConsumerWidget {
  const FarmLogDetailScreen({super.key, required this.logId});
  final String logId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final l = ref.watch(_logProvider(logId));
    return Scaffold(
      appBar: AppBar(title: const Text('Nhật ký canh tác')),
      body: l.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (entry) => entry == null
            ? const Padding(
                padding: EdgeInsets.all(24),
                child: Text(
                  'Nhật ký chưa đồng bộ về máy. Kết nối mạng rồi bấm "Đồng bộ ngay".',
                  style: TextStyle(fontSize: 17),
                ),
              )
            : ListView(
                padding: const EdgeInsets.all(12),
                children: [
                  LogCard(
                    entry: entry,
                    onSupplement: (w) =>
                        Navigator.of(context)
                            .push(MaterialPageRoute<void>(builder: (_) => w))
                            .then((_) => ref.invalidate(_logProvider(logId))),
                  ),
                ],
              ),
      ),
    );
  }
}
