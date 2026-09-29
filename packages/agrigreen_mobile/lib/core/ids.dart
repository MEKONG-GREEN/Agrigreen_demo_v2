import 'package:uuid/uuid.dart';

/// Sinh định danh UUIDv7 trên máy (g1-conventions §3, R-41: không suy ra từ
/// thuộc tính định danh).
abstract class IdGenerator {
  String newId();
}

class UuidV7Generator implements IdGenerator {
  UuidV7Generator();
  final Uuid _uuid = const Uuid();

  @override
  String newId() => _uuid.v7();
}

/// `dev:<device_id>:<seq>` — hợp đồng đồng bộ, mục push.
String idemKeyFor(String deviceId, int seq) => 'dev:$deviceId:$seq';

final RegExp _uuidV7 = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$',
);

bool isUuidV7(String s) => _uuidV7.hasMatch(s);
