import 'dart:async';

import 'package:firebase_core/firebase_core.dart';
import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';

import '../../core/config.dart';
import 'push_messaging.dart';

/// Chỉ khởi tạo Firebase khi có cấu hình (`--dart-define=AG_FCM_*`); không
/// dùng `google-services.json` để Local/Dev chạy được mà không có Firebase.
FirebaseOptions? firebaseOptionsFrom(AppConfig c) {
  final f = c.fcm;
  if (f == null) return null;
  return FirebaseOptions(
    apiKey: f.apiKey,
    appId: f.appId,
    messagingSenderId: f.senderId,
    projectId: f.projectId,
  );
}

/// FCM: chỉ nhận thông điệp dữ liệu (ADR-0015). Nội dung lấy qua API/pull.
class FirebasePushMessaging implements PushMessaging {
  FirebasePushMessaging(this._options);
  final FirebaseOptions _options;
  FirebaseMessaging? _fm;

  @override
  Future<bool> init() async {
    try {
      if (Firebase.apps.isEmpty) {
        await Firebase.initializeApp(options: _options);
      }
      final fm = FirebaseMessaging.instance;
      await fm.requestPermission();
      _fm = fm;
      return true;
    } catch (_) {
      return false;
    }
  }

  @override
  Future<String?> token() async {
    try {
      return await _fm?.getToken();
    } catch (_) {
      return null;
    }
  }

  @override
  Stream<String> get tokenRefresh =>
      _fm?.onTokenRefresh ?? const Stream.empty();

  @override
  Stream<PushMessage> get messages => _fm == null
      ? const Stream.empty()
      : FirebaseMessaging.onMessage.map(
          (m) => PushMessage({
            for (final e in m.data.entries) e.key: '${e.value}',
          }),
        );
}

/// Thông báo cục bộ bằng flutter_local_notifications (BSD-3-Clause).
/// `visibility: private`: màn hình khóa an toàn chỉ hiện tên app.
class FlutterLocalNotifier implements LocalNotifier {
  FlutterLocalNotifier({
    FlutterLocalNotificationsPlugin? plugin,
    this.requestPermission = true,
  }) : _plugin = plugin ?? FlutterLocalNotificationsPlugin();
  final FlutterLocalNotificationsPlugin _plugin;
  final bool requestPermission;
  final _taps = StreamController<String?>.broadcast();

  static const _channel = AndroidNotificationDetails(
    'agrigreen_inbox',
    'Thông báo AgriGreen',
    channelDescription: 'Chỉ đạo, cảnh báo, tài liệu mới, nhiệm vụ',
    importance: Importance.high,
    priority: Priority.high,
    visibility: NotificationVisibility.private,
  );

  @override
  Future<void> init() async {
    await _plugin.initialize(
      settings: const InitializationSettings(
        android: AndroidInitializationSettings('@mipmap/ic_launcher'),
      ),
      onDidReceiveNotificationResponse: (r) => _taps.add(r.payload),
    );
    if (!requestPermission) return;
    await _plugin
        .resolvePlatformSpecificImplementation<
          AndroidFlutterLocalNotificationsPlugin
        >()
        ?.requestNotificationsPermission();
  }

  @override
  Future<void> show(LocalNotice n) => _plugin.show(
    id: n.id,
    title: n.title,
    body: n.body,
    notificationDetails: const NotificationDetails(android: _channel),
    payload: n.payload,
  );

  @override
  Stream<String?> get taps => _taps.stream;

  @override
  Future<String?> launchPayload() async {
    final d = await _plugin.getNotificationAppLaunchDetails();
    if (d?.didNotificationLaunchApp ?? false) {
      return d!.notificationResponse?.payload;
    }
    return null;
  }
}

/// Thông điệp tới khi app ở nền/đã tắt: CSDL đang khóa (chỉ mở sau PIN) nên
/// không đồng bộ được. Chỉ hiện thông báo **chung**, không nội dung.
/// `sync_now` bỏ qua — app đồng bộ ngay khi được mở khóa.
@pragma('vm:entry-point')
Future<void> firebaseBackgroundHandler(RemoteMessage m) async {
  final msg = PushMessage({
    for (final e in m.data.entries) e.key: '${e.value}',
  });
  if (msg.isSyncNow) return;
  final opts = firebaseOptionsFrom(AppConfig.fromEnvironment());
  if (opts != null && Firebase.apps.isEmpty) {
    await Firebase.initializeApp(options: opts);
  }
  final n = FlutterLocalNotifier(requestPermission: false);
  await n.init();
  await n.show(
    LocalNotice(
      id: noticeIdFor(msg.notificationId),
      title: genericNoticeTitle,
      body: genericNoticeBody,
      payload: '/notifications',
    ),
  );
}
