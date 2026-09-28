import 'dart:async';

import '../data/push/push_messaging.dart';
import 'inbox_service.dart';

/// Phần của app đã mở khóa mà kênh đẩy cần dùng.
abstract class PushHost {
  /// Đồng bộ ngay (push → phát lại lệnh → pull), bỏ qua thời điểm lùi.
  Future<void> syncNow();

  /// Thông báo đã có trên máy sau khi đồng bộ (null nếu chưa về).
  Future<InboxItem?> notification(String id);

  /// Lưu token và gắn lên máy chủ ở lượt đồng bộ kế tiếp.
  Future<void> setPushToken(String token);
}

/// Xử lý thông điệp đẩy chỉ-dữ-liệu (ADR-0015):
/// - `{kind: "sync_now"}` → đồng bộ ngay (US-SYNC-02);
/// - `{kind, notification_id}` → pull để lấy nội dung, rồi hiện thông báo cục
///   bộ. **Tiêu đề chung "Có thông báo mới"** khi app chưa mở khóa, không ở
///   tiền cảnh, hoặc nội dung chưa đồng bộ về — màn hình khóa không lộ nội dung.
///
/// Token: gắn sau khi đăng ký thiết bị (SyncService.beforeSync), mỗi lần mở
/// khóa và mỗi khi FCM đổi token.
class PushCoordinator {
  PushCoordinator({
    required PushMessaging messaging,
    required LocalNotifier notifier,
    bool Function()? isForeground,
  }) : _messaging = messaging,
       _notifier = notifier,
       _isForeground = isForeground ?? (() => true);

  final PushMessaging _messaging;
  final LocalNotifier _notifier;
  final bool Function() _isForeground;

  PushHost? _host;
  String? _token;
  bool _started = false;
  bool available = false;
  final _subs = <StreamSubscription<Object?>>[];

  /// Đếm để kiểm thử/giám sát (không chứa dữ liệu).
  int syncNowHandled = 0;

  Future<void> start() async {
    if (_started) return;
    _started = true;
    try {
      await _notifier.init();
    } catch (_) {
      // Không có quyền hiển thị thông báo — hộp thư trong app vẫn dùng được.
    }
    available = await _messaging.init();
    if (!available) return;
    _subs
      ..add(_messaging.tokenRefresh.listen((t) => unawaited(_onToken(t))))
      ..add(_messaging.messages.listen((m) => unawaited(handle(m))));
    final t = await _messaging.token();
    if (t != null) await _onToken(t);
  }

  Future<void> _onToken(String t) async {
    _token = t;
    final h = _host;
    if (h == null) return;
    await h.setPushToken(t);
    await h.syncNow();
  }

  /// Gọi khi mở khóa (CSDL đã mở). Gắn lại token mỗi lần (hợp đồng).
  Future<void> attach(PushHost host) async {
    _host = host;
    final t = _token;
    if (t != null) await host.setPushToken(t);
  }

  void detach() => _host = null;

  Future<void> handle(PushMessage m) async {
    final h = _host;
    if (m.isSyncNow) {
      if (h == null) return; // chưa mở khóa: đồng bộ ngay khi mở khóa
      syncNowHandled++;
      await h.syncNow();
      return;
    }
    final id = m.notificationId;
    InboxItem? item;
    if (h != null) {
      try {
        await h.syncNow();
        if (id != null) item = await h.notification(id);
      } catch (_) {
        item = null;
      }
    }
    final reveal = h != null && item != null && _isForeground();
    await _notifier.show(
      LocalNotice(
        id: noticeIdFor(id),
        title: reveal ? item.title : genericNoticeTitle,
        body: reveal ? _clip(item.body) : genericNoticeBody,
        payload: item?.deepLink ?? '/notifications',
        urgent: item?.urgent ?? false,
      ),
    );
  }

  static String _clip(String s) =>
      s.length <= 160 ? s : '${s.substring(0, 157)}…';

  Stream<String?> get taps => _notifier.taps;
  Future<String?> launchPayload() => _notifier.launchPayload();

  Future<void> dispose() async {
    for (final s in _subs) {
      await s.cancel();
    }
    _subs.clear();
  }
}
