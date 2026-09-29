import '../../core/ids.dart';
import '../secure/secure_store.dart';
import '../sync/sync_api.dart';

/// Định danh thiết bị (UUIDv7 sinh trên máy, lưu kho bí mật) và đăng ký
/// `POST /v1/devices`.
class DeviceService {
  DeviceService({required SecureStore store, required IdGenerator ids})
    : _store = store,
      _ids = ids;
  final SecureStore _store;
  final IdGenerator _ids;

  Future<String> deviceId() async {
    final existing = await _store.read(SecureKeys.deviceId);
    if (existing != null) return existing;
    final id = _ids.newId();
    await _store.write(SecureKeys.deviceId, id);
    return id;
  }

  Future<bool> isRegistered() async =>
      (await _store.read(SecureKeys.deviceRegistered)) == '1';

  /// Đăng ký (idempotent: Idempotency-Key = `devreg:<device_id>`).
  Future<void> ensureRegistered(
    SyncApi api, {
    required String appVersion,
    String platform = 'android',
  }) async {
    if (await isRegistered()) return;
    await api.registerDevice(
      deviceId: await deviceId(),
      platform: platform,
      appVersion: appVersion,
    );
    await _store.write(SecureKeys.deviceRegistered, '1');
  }
}
