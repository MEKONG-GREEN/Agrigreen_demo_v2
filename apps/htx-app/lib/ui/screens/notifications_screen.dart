import 'package:agrigreen_mobile/application/inbox_service.dart';
import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers.dart';

final _inboxProvider = FutureProvider.autoDispose<List<InboxItem>>(
  (ref) => servicesOf(ref).inbox.list(),
);

/// Hộp thư (entity `notification`, 30 ngày): nhật ký được duyệt / cần bổ sung,
/// cảnh báo khẩn cấp (HTX US-NEWS-03). Đánh dấu đã đọc xếp hàng khi ngoại tuyến.
class NotificationsScreen extends ConsumerWidget {
  const NotificationsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final list = ref.watch(_inboxProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Thông báo')),
      body: list.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (items) => items.isEmpty
            ? const Center(
                child: Text(
                  'Chưa có thông báo.',
                  style: TextStyle(fontSize: 17),
                ),
              )
            : ListView(
                padding: const EdgeInsets.all(8),
                children: [
                  for (final n in items)
                    Card(
                      child: ListTile(
                        leading: Icon(
                          n.severity == 'urgent'
                              ? Icons.warning_amber
                              : Icons.notifications,
                          color: n.severity == 'urgent'
                              ? AgColors.red
                              : AgColors.g700,
                        ),
                        title: Text(
                          n.title,
                          style: TextStyle(
                            fontWeight: n.read
                                ? FontWeight.w400
                                : FontWeight.w700,
                          ),
                        ),
                        subtitle: Text(
                          '${n.body}\n${formatLocal(n.createdAt)}',
                        ),
                        onTap: () async {
                          await ref.services.inbox.markRead(n.id);
                          ref.invalidate(_inboxProvider);
                        },
                      ),
                    ),
                ],
              ),
      ),
    );
  }
}
