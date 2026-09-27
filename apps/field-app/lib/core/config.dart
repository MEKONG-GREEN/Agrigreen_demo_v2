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
  });

  factory AppConfig.fromEnvironment() => const AppConfig(
    apiBaseUrl: String.fromEnvironment(
      'AG_API_BASE',
      defaultValue: 'https://api.dev.agrigreen.vn',
    ),
    oidcIssuer: String.fromEnvironment(
      'AG_OIDC_ISSUER',
      defaultValue: 'https://id.dev.agrigreen.vn/realms/agrigreen',
    ),
    tileUrlTemplate: String.fromEnvironment(
      'AG_TILE_URL',
      defaultValue: 'https://tile.openstreetmap.org/{z}/{x}/{y}.png',
    ),
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

  /// TODO(offline-map): thay bằng gói tile ngoại tuyến theo địa bàn (US-MAP-03).
  /// OSM tile.openstreetmap.org chỉ dùng cho dev — xem chính sách sử dụng tile OSM.
  final String tileUrlTemplate;
}
