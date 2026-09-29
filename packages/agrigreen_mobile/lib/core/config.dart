/// Cấu hình Firebase Cloud Messaging (chỉ thông điệp dữ liệu — ADR-0015).
/// Không có → không đăng ký kênh đẩy; hộp thư vẫn đồng bộ qua pull.
class FcmConfig {
  const FcmConfig({
    required this.projectId,
    required this.appId,
    required this.apiKey,
    required this.senderId,
  });
  final String projectId;
  final String appId;
  final String apiKey;
  final String senderId;

  static FcmConfig? fromEnvironment() {
    const projectId = String.fromEnvironment('AG_FCM_PROJECT_ID');
    const appId = String.fromEnvironment('AG_FCM_APP_ID');
    const apiKey = String.fromEnvironment('AG_FCM_API_KEY');
    const senderId = String.fromEnvironment('AG_FCM_SENDER_ID');
    if (projectId.isEmpty || appId.isEmpty || apiKey.isEmpty) return null;
    return const FcmConfig(
      projectId: projectId,
      appId: appId,
      apiKey: apiKey,
      senderId: senderId,
    );
  }
}

/// Cấu hình môi trường. Giá trị truyền qua `--dart-define` khi build (CI),
/// mặc định trỏ môi trường dev.
class AppConfig {
  const AppConfig({
    required this.apiBaseUrl,
    required this.oidcIssuer,
    this.oidcClientId = 'agrigreen-field-app',
    this.oidcRedirectUrl = 'vn.agrigreen.field:/oauth2redirect',
    this.oidcScopes = const ['openid', 'profile', 'offline_access'],
    this.offlineSessionMax = const Duration(hours: 72),
    this.pinMaxAttempts = 5,
    this.pinLockout = const Duration(minutes: 15),
    this.pinHardLimit = 10,
    this.ackRetention = const Duration(hours: 48),
    this.pushBatchSize = 100,
    this.pullLimit = 500,
    this.maxFileBytes = 15 * 1024 * 1024,
    this.tileUrlTemplate = 'https://tile.openstreetmap.org/{z}/{x}/{y}.png',
    this.fcm,
  });

  /// [oidcClientId]/[oidcRedirectUrl] khác nhau theo app: mặc định là app hiện
  /// trường (`agrigreen-field-app`); App HTX truyền `agrigreen-htx-app` và
  /// `vn.agrigreen.htx:/oauth2redirect`.
  factory AppConfig.fromEnvironment({
    String oidcClientId = 'agrigreen-field-app',
    String oidcRedirectUrl = 'vn.agrigreen.field:/oauth2redirect',
  }) => AppConfig(
    oidcClientId: oidcClientId,
    oidcRedirectUrl: oidcRedirectUrl,
    apiBaseUrl: const String.fromEnvironment(
      'AG_API_BASE',
      defaultValue: 'https://api.dev.agrigreen.vn',
    ),
    oidcIssuer: const String.fromEnvironment(
      'AG_OIDC_ISSUER',
      defaultValue: 'https://id.dev.agrigreen.vn/realms/agrigreen',
    ),
    tileUrlTemplate: const String.fromEnvironment(
      'AG_TILE_URL',
      defaultValue: 'https://tile.openstreetmap.org/{z}/{x}/{y}.png',
    ),
    fcm: FcmConfig.fromEnvironment(),
  );

  final String apiBaseUrl;
  final String oidcIssuer;
  final String oidcClientId;
  final String oidcRedirectUrl;
  final List<String> oidcScopes;

  /// Phiên ngoại tuyến tối đa kể từ lần xác thực trực tuyến cuối (US-ADM-06).
  final Duration offlineSessionMax;

  /// Số lần sai PIN liên tiếp trước khi khóa tạm (theo US-ADM: 5 lần → 15 phút).
  final int pinMaxAttempts;
  final Duration pinLockout;

  /// Sai tổng cộng chừng này lần → hủy PIN, bắt buộc đăng nhập trực tuyến.
  final int pinHardLimit;

  /// Giữ bản ghi đã được máy chủ xác nhận thêm 48 giờ rồi mới xóa (mục 15).
  final Duration ackRetention;
  final int pushBatchSize;
  final int pullLimit;
  final int maxFileBytes;

  /// Máy chủ tile (`AG_TILE_URL`), dùng cả khi tải gói ngoại tuyến (US-MAP-03).
  /// OSM tile.openstreetmap.org **chỉ dùng cho dev** (chính sách OSM cấm tải
  /// hàng loạt); production: máy chủ tile trong lãnh thổ (ADR-0007).
  final String tileUrlTemplate;

  final FcmConfig? fcm;
}
