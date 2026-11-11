import '../domain/work_schedule.dart';

/// Kiểu thông báo lịch công tác (pf/notify, KN v7.7 P5).
const workScheduleNotificationKinds = {
  'work_schedule_reviewed',
  'work_schedule_due',
};

/// Đường dẫn lịch công tác trong app: `/extension/work-schedules`,
/// `/extension/work-schedules/mine`, `/extension/work-schedules/{id}`, tùy chọn
/// `?week_start=YYYY-MM-DD`. Trả tuần cần mở (null = tuần mặc định), hoặc
/// `false` nếu không phải đường dẫn lịch công tác.
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
