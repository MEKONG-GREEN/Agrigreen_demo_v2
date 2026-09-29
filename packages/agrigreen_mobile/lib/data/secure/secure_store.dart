import 'package:flutter_secure_storage/flutter_secure_storage.dart';

/// Kho bí mật của nền tảng (Android Keystore). Khóa SQLCipher, token, băm PIN,
/// phiên ngoại tuyến đều nằm ở đây — **không bao giờ** ở SharedPreferences.
abstract class SecureStore {
  Future<String?> read(String key);
  Future<void> write(String key, String value);
  Future<void> delete(String key);
  Future<void> deleteAll();
}

/// Tên khóa dùng trong [SecureStore].
abstract final class SecureKeys {
  static const dbKey = 'ag.db.key.v1';
  static const deviceId = 'ag.device.id';
  static const deviceRegistered = 'ag.device.registered';
  static const accessToken = 'ag.oidc.access';
  static const refreshToken = 'ag.oidc.refresh';
  static const idToken = 'ag.oidc.id';
  static const accessExpiry = 'ag.oidc.access_exp';
  static const subject = 'ag.oidc.sub';
  static const lastOnlineAuthAt = 'ag.session.last_online_auth';
  static const lastSeenAt = 'ag.session.last_seen';
  static const pinHash = 'ag.pin.hash';
  static const pinSalt = 'ag.pin.salt';
  static const pinIterations = 'ag.pin.iter';
  static const pinFailures = 'ag.pin.failures';
  static const pinTotalFailures = 'ag.pin.total_failures';
  static const pinLockedUntil = 'ag.pin.locked_until';
}

class FlutterSecureStore implements SecureStore {
  FlutterSecureStore([FlutterSecureStorage? storage])
    : _s = storage ?? const FlutterSecureStorage();
  final FlutterSecureStorage _s;

  @override
  Future<String?> read(String key) => _s.read(key: key);
  @override
  Future<void> write(String key, String value) =>
      _s.write(key: key, value: value);
  @override
  Future<void> delete(String key) => _s.delete(key: key);
  @override
  Future<void> deleteAll() => _s.deleteAll();
}

/// Bản trong bộ nhớ — chỉ cho kiểm thử.
class MemorySecureStore implements SecureStore {
  final Map<String, String> values = {};
  @override
  Future<String?> read(String key) async => values[key];
  @override
  Future<void> write(String key, String value) async => values[key] = value;
  @override
  Future<void> delete(String key) async => values.remove(key);
  @override
  Future<void> deleteAll() async => values.clear();
}
