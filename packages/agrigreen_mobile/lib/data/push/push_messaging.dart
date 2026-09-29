import 'dart:async';

/// Thông điệp đẩy chỉ-dữ-liệu (ADR-0015): `{kind, notification_id}` hoặc
/// `{kind: "sync_now", entities?: "plot,task"}`. Không có tiêu đề, nội dung hay dữ liệu cá nhân.
class PushMessage {
  const PushMessage(this.data);
  final Map<String, String> data;

  String? get kind => data['kind'];
  String? get notificationId {
    final v = data['notification_id'];
    return v == null || v.isEmpty ? null : v;
  }

  bool get isSyncNow => kind == 'sync_now';

  /// Tên entity cần pull của `sync_now` (G2, KN US-SYNC-02: khóa `entities`,
  /// nối bằng dấu phẩy). null = không có / sai dạng → pull toàn bộ (tương
  /// thích ngược; tên lạ không bao giờ làm bỏ lượt đồng bộ).
  Set<String>? get entities {
    final raw = data['entities'];
    if (raw == null) return null;
    final out = <String>{};
    for (final p in raw.split(',')) {
      final e = p.trim();
      if (e.isEmpty) continue;
      if (!_entityName.hasMatch(e)) return null;
      out.add(e);
    }
    if (out.isEmpty || out.length > maxNudgeEntities) return null;
    return out;
  }

  static final _entityName = RegExp(r'^[a-z][a-z0-9_]{0,63}$');

  /// Như `notify/port.MaxNudgeEntities`.
  static const maxNudgeEntities = 30;
}

/// Kênh đẩy (FCM trên máy; bản giả trong kiểm thử — không cần Firebase).
abstract class PushMessaging {
  /// Khởi tạo, xin quyền hiển thị thông báo (Android 13+). false = không có
  /// kênh đẩy (chưa cấu hình Firebase) — app vẫn có hộp thư qua pull.
  Future<bool> init();

  Future<String?> token();
  Stream<String> get tokenRefresh;

  /// Thông điệp khi app đang chạy.
  Stream<PushMessage> get messages;
}

/// Không có kênh đẩy (dev/local không cấu hình `AG_FCM_*`).
class NoopPushMessaging implements PushMessaging {
  const NoopPushMessaging();
  @override
  Future<bool> init() async => false;
  @override
  Future<String?> token() async => null;
  @override
  Stream<String> get tokenRefresh => const Stream.empty();
  @override
  Stream<PushMessage> get messages => const Stream.empty();
}

/// Bản giả điều khiển được — kiểm thử đơn vị và tích hợp.
class FakePushMessaging implements PushMessaging {
  FakePushMessaging({this.initialToken = 'fake-fcm-token-0123456789abcdef'});
  String? initialToken;
  final _tokens = StreamController<String>.broadcast();
  final _messages = StreamController<PushMessage>.broadcast();
  bool initialized = false;

  @override
  Future<bool> init() async => initialized = true;
  @override
  Future<String?> token() async => initialToken;
  @override
  Stream<String> get tokenRefresh => _tokens.stream;
  @override
  Stream<PushMessage> get messages => _messages.stream;

  void refreshToken(String t) {
    initialToken = t;
    _tokens.add(t);
  }

  void deliver(Map<String, String> data) => _messages.add(PushMessage(data));
}

/// Thông báo cục bộ hiển thị trên máy.
class LocalNotice {
  const LocalNotice({
    required this.id,
    required this.title,
    required this.body,
    this.payload,
    this.urgent = false,
  });
  final int id;
  final String title;
  final String body;

  /// deep_link (không phải dữ liệu cá nhân) để mở đúng màn hình khi chạm.
  final String? payload;
  final bool urgent;
}

abstract class LocalNotifier {
  Future<void> init();
  Future<void> show(LocalNotice n);

  /// payload của thông báo người dùng chạm vào.
  Stream<String?> get taps;

  /// payload của thông báo đã mở app (app đang tắt), nếu có.
  Future<String?> launchPayload();
}

class NoopLocalNotifier implements LocalNotifier {
  const NoopLocalNotifier();
  @override
  Future<void> init() async {}
  @override
  Future<void> show(LocalNotice n) async {}
  @override
  Stream<String?> get taps => const Stream.empty();
  @override
  Future<String?> launchPayload() async => null;
}

class RecordingLocalNotifier implements LocalNotifier {
  final shown = <LocalNotice>[];
  final _taps = StreamController<String?>.broadcast();
  @override
  Future<void> init() async {}
  @override
  Future<void> show(LocalNotice n) async => shown.add(n);
  @override
  Stream<String?> get taps => _taps.stream;
  @override
  Future<String?> launchPayload() async => null;
  void tap(String? payload) => _taps.add(payload);
}

/// Tiêu đề chung khi app chưa mở khóa (màn hình khóa không lộ nội dung).
const genericNoticeTitle = 'Có thông báo mới';
const genericNoticeBody = 'Mở AgriGreen Hiện trường để xem.';

/// Mã số thông báo cục bộ ổn định theo id (thay thế thay vì chồng lặp).
int noticeIdFor(String? notificationId) =>
    notificationId == null ? 1 : notificationId.hashCode & 0x7fffffff;
