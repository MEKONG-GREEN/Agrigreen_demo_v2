import 'package:agrigreen_mobile/domain/deep_link.dart';
import 'package:flutter/material.dart';

import '../domain/work_schedule.dart';
import 'screens/directives_screen.dart';
import 'screens/library_screen.dart';
import 'screens/notifications_screen.dart';
import 'screens/sync_queue_screen.dart';
import 'screens/tasks_screen.dart';
import 'screens/work_schedule_screen.dart';

/// Kiểu thông báo lịch công tác (pf/notify, KN v7.7 P5).
const workScheduleNotificationKinds = {
  'work_schedule_reviewed',
  'work_schedule_due',
};

/// Đường dẫn lịch công tác trong app: `/extension/work-schedules`,
/// `/extension/work-schedules/mine`, `/extension/work-schedules/{id}`, tùy chọn
/// `?week_start=YYYY-MM-DD`. Trả tuần cần mở (null = tuần mặc định), hoặc
/// `false` nếu không phải đường dẫn lịch công tác.
///
/// Lõi `DeepLink` (sealed, dùng chung với App HTX) chưa có loại này nên app
/// nhận diện trước khi gọi `DeepLink.parse`.
(bool, DateTime?) workScheduleLinkOf(String? raw, {String? kind}) {
  final uri = raw == null ? null : Uri.tryParse(raw.trim());
  final seg = uri?.pathSegments.where((s) => s.isNotEmpty).toList() ?? [];
  final isLink =
      seg.length >= 2 &&
      seg.length <= 3 &&
      seg[0] == 'extension' &&
      seg[1] == 'work-schedules';
  if (!isLink && !workScheduleNotificationKinds.contains(kind)) {
    return (false, null);
  }
  final w = parseYmd(uri?.queryParameters['week_start']);
  return (true, w == null ? null : mondayOf(w));
}

/// Màn hình cho một đường dẫn trong app (từ thông báo).
Widget screenForDeepLink(DeepLink link) => switch (link) {
  DirectiveLink(:final id) => DirectiveDetailScreen(directiveId: id),
  TaskLink(:final id) => TasksScreen(highlightTaskId: id),
  DocumentLink(:final ref) => DocumentScreen(documentRef: ref),
  ConflictLink() => const SyncQueueScreen(),
  // Đường dẫn App HTX (nhật ký/vụ) không có màn hình ở app hiện trường.
  InboxLink() || FarmLogLink() || SeasonLink() => const NotificationsScreen(),
};

/// Màn hình cho đường dẫn thô [raw] (và kiểu thông báo [kind] nếu biết).
Widget screenForRawLink(String? raw, {String? kind}) {
  final (ws, week) = workScheduleLinkOf(raw, kind: kind);
  if (ws) return WorkScheduleScreen(initialWeek: week);
  return screenForDeepLink(DeepLink.parse(raw));
}

Future<void> openDeepLink(BuildContext context, String? raw, {String? kind}) =>
    Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => screenForRawLink(raw, kind: kind),
      ),
    );
