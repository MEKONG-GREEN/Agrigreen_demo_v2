import '../db/local_db.dart';
import '../files/blob_store.dart';
import '../secure/secure_store.dart';

/// Xóa dữ liệu cục bộ khi thiết bị bị thu hồi (`403 sync.device_revoked`,
/// mục 15; KN US-ADM-06): CSDL SQLCipher, ciphertext ảnh, token, khóa CSDL,
/// PIN, định danh thiết bị. Không thể hoàn tác.
class WipeService {
  WipeService({
    required LocalDb db,
    required BlobStore blobs,
    required SecureStore store,
    this.onWiped,
  }) : _db = db,
       _blobs = blobs,
       _store = store;

  final LocalDb _db;
  final BlobStore _blobs;
  final SecureStore _store;

  /// UI đăng ký để quay về màn đăng nhập.
  final void Function()? onWiped;

  bool _wiped = false;
  bool get wiped => _wiped;

  Future<void> wipe() async {
    if (_wiped) return;
    _wiped = true;
    // Khóa token trước tiên: dù bước sau lỗi, không còn gọi API được.
    await _store.deleteAll();
    try {
      await _db.destroy();
    } finally {
      await _blobs.deleteAll();
    }
    onWiped?.call();
  }
}
