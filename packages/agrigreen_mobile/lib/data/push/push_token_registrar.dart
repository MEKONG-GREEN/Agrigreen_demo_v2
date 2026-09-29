import 'dart:convert';

import 'package:crypto/crypto.dart';

import '../db/local_db.dart';
import '../sync/sync_api.dart';

/// Gắn token đẩy FCM với thiết bị đã đăng ký (`POST /v1/devices/{id}/push-token`).
///
/// Theo hợp đồng: gắn sau khi đăng ký thiết bị, **mỗi lần khởi động** và khi
/// FCM đổi token. Token là định danh thiết bị: chỉ lưu trong CSDL SQLCipher,
/// không ghi log, không hiển thị.
class PushTokenRegistrar {
  PushTokenRegistrar(this._db, {required DateTime startedAt})
    : _startTag = startedAt.millisecondsSinceEpoch.toRadixString(36);
  final LocalDb _db;

  /// Khóa idempotency đổi theo lần khởi động (máy chủ giữ khóa 48 giờ; cùng
  /// khóa sẽ chỉ phát lại phản hồi cũ mà không làm mới phạm vi).
  final String _startTag;

  static const _tokenKey = 'push.token';
  static const _registeredKey = 'push.token.registered';

  /// Mỗi lần mở app phải gắn lại (máy chủ làm mới ảnh chụp phạm vi/vai).
  bool _registeredThisStart = false;

  Future<void> setToken(String token) async {
    final t = token.trim();
    if (t.isEmpty) return;
    final prev = await _db.getMeta(_tokenKey);
    if (prev != t) {
      await _db.setMeta(_tokenKey, t);
      _registeredThisStart = false;
    }
  }

  Future<bool> hasToken() async => (await _db.getMeta(_tokenKey)) != null;

  static String _hash(String t) =>
      sha256.convert(utf8.encode(t)).toString().substring(0, 24);

  /// Gọi trong `beforeSync`, sau khi thiết bị đã đăng ký. Lỗi dữ liệu/phụ
  /// thuộc (403 notify.device_unregistered, 503) không chặn đồng bộ: lần sau
  /// thử lại. Lỗi mạng, thu hồi, 401 được ném tiếp cho SyncService xử lý.
  Future<bool> flush(SyncApi api, String deviceId) async {
    final token = await _db.getMeta(_tokenKey);
    if (token == null) return false;
    final h = _hash(token);
    if (_registeredThisStart && await _db.getMeta(_registeredKey) == h) {
      return false;
    }
    try {
      await api.registerPushToken(
        deviceId: deviceId,
        token: token,
        idempotencyKey: 'ptok:$h:$_startTag',
      );
    } on DeviceRevokedException {
      rethrow;
    } on UnauthenticatedException {
      rethrow;
    } on ApiException {
      return false;
    }
    await _db.setMeta(_registeredKey, h);
    _registeredThisStart = true;
    return true;
  }
}
