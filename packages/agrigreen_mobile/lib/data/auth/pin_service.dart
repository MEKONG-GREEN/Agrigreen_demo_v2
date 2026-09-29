import 'dart:convert';
import 'dart:math';

import 'package:cryptography/cryptography.dart';

import '../../core/clock.dart';
import '../secure/secure_store.dart';

enum PinCheck {
  ok,
  wrong,

  /// Sai quá số lần cho phép — khóa tạm.
  lockedOut,

  /// Sai quá giới hạn cứng — PIN đã bị hủy, phải đăng nhập trực tuyến.
  mustReauthenticate,

  /// Chưa đặt PIN.
  notSet,
}

class PinResult {
  const PinResult(this.check, {this.lockedUntil, this.remainingAttempts});
  final PinCheck check;
  final DateTime? lockedUntil;
  final int? remainingAttempts;
}

class PinException implements Exception {
  PinException(this.message);
  final String message;
  @override
  String toString() => message;
}

/// PIN 6 số mở khóa phiên ngoại tuyến (US-ADM-05/06).
///
/// Lưu **băm** PBKDF2-HMAC-SHA256 + muối trong kho bí mật; bộ đếm sai và thời
/// điểm hết khóa cũng lưu bền nên tắt app không đặt lại được bộ đếm.
class PinService {
  PinService({
    required SecureStore store,
    required Clock clock,
    this.maxAttempts = 5,
    this.lockout = const Duration(minutes: 15),
    this.hardLimit = 10,
    this.iterations = 60000,
    Random? random,
  }) : _store = store,
       _clock = clock,
       _random = random ?? Random.secure();

  final SecureStore _store;
  final Clock _clock;
  final int maxAttempts;
  final Duration lockout;
  final int hardLimit;
  final int iterations;
  final Random _random;

  static final RegExp _pinFormat = RegExp(r'^\d{6}$');

  static bool isValidFormat(String pin) => _pinFormat.hasMatch(pin);

  Future<bool> isSet() async => (await _store.read(SecureKeys.pinHash)) != null;

  Future<List<int>> _derive(String pin, List<int> salt, int iter) async {
    final kdf = Pbkdf2(
      macAlgorithm: Hmac.sha256(),
      iterations: iter,
      bits: 256,
    );
    final k = await kdf.deriveKey(
      secretKey: SecretKey(utf8.encode(pin)),
      nonce: salt,
    );
    return k.extractBytes();
  }

  /// Đặt PIN — chỉ gọi sau một lần đăng nhập trực tuyến thành công.
  Future<void> setPin(String pin) async {
    if (!isValidFormat(pin)) {
      throw PinException('PIN phải gồm đúng 6 chữ số.');
    }
    final salt = List<int>.generate(16, (_) => _random.nextInt(256));
    final h = await _derive(pin, salt, iterations);
    await _store.write(SecureKeys.pinSalt, base64.encode(salt));
    await _store.write(SecureKeys.pinHash, base64.encode(h));
    await _store.write(SecureKeys.pinIterations, '$iterations');
    await _resetCounters();
  }

  Future<void> _resetCounters() async {
    await _store.delete(SecureKeys.pinFailures);
    await _store.delete(SecureKeys.pinTotalFailures);
    await _store.delete(SecureKeys.pinLockedUntil);
  }

  Future<void> clear() async {
    await _store.delete(SecureKeys.pinHash);
    await _store.delete(SecureKeys.pinSalt);
    await _store.delete(SecureKeys.pinIterations);
    await _resetCounters();
  }

  Future<DateTime?> lockedUntil() async {
    final v = int.tryParse(await _store.read(SecureKeys.pinLockedUntil) ?? '');
    return v == null ? null : DateTime.fromMillisecondsSinceEpoch(v);
  }

  Future<PinResult> verify(String pin) async {
    final hashB64 = await _store.read(SecureKeys.pinHash);
    final saltB64 = await _store.read(SecureKeys.pinSalt);
    if (hashB64 == null || saltB64 == null) {
      return const PinResult(PinCheck.notSet);
    }
    final now = _clock.now();
    final until = await lockedUntil();
    if (until != null && now.isBefore(until)) {
      return PinResult(PinCheck.lockedOut, lockedUntil: until);
    }
    final iter =
        int.tryParse(await _store.read(SecureKeys.pinIterations) ?? '') ??
        iterations;
    final ok =
        isValidFormat(pin) &&
        _constantTimeEq(
          await _derive(pin, base64.decode(saltB64), iter),
          base64.decode(hashB64),
        );
    if (ok) {
      await _resetCounters();
      return const PinResult(PinCheck.ok);
    }
    final fails =
        (int.tryParse(await _store.read(SecureKeys.pinFailures) ?? '') ?? 0) +
        1;
    final total =
        (int.tryParse(await _store.read(SecureKeys.pinTotalFailures) ?? '') ??
            0) +
        1;
    await _store.write(SecureKeys.pinTotalFailures, '$total');
    if (total >= hardLimit) {
      await clear();
      return const PinResult(PinCheck.mustReauthenticate);
    }
    if (fails >= maxAttempts) {
      final lockedTo = now.add(lockout);
      await _store.write(
        SecureKeys.pinLockedUntil,
        '${lockedTo.millisecondsSinceEpoch}',
      );
      await _store.write(SecureKeys.pinFailures, '0');
      return PinResult(PinCheck.lockedOut, lockedUntil: lockedTo);
    }
    await _store.write(SecureKeys.pinFailures, '$fails');
    return PinResult(PinCheck.wrong, remainingAttempts: maxAttempts - fails);
  }

  static bool _constantTimeEq(List<int> a, List<int> b) {
    if (a.length != b.length) return false;
    var r = 0;
    for (var i = 0; i < a.length; i++) {
      r |= a[i] ^ b[i];
    }
    return r == 0;
  }
}
