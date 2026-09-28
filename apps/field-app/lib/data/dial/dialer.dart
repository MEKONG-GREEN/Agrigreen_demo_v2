import 'package:url_launcher/url_launcher.dart';

/// Mở ứng dụng gọi điện / nhắn tin của máy bằng URI `tel:` / `sms:`
/// ("gọi/nhắn 1 chạm", KN US-DIR-02). Thay được trong kiểm thử.
abstract class Dialer {
  /// `false` khi máy không có ứng dụng xử lý URI.
  Future<bool> open(Uri uri);
}

/// Bản thật: url_launcher (BSD-3-Clause), mở ứng dụng ngoài.
class UrlLauncherDialer implements Dialer {
  const UrlLauncherDialer();

  @override
  Future<bool> open(Uri uri) =>
      launchUrl(uri, mode: LaunchMode.externalApplication);
}
