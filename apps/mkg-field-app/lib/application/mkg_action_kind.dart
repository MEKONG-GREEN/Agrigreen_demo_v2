import 'package:agrigreen_mobile/data/actions/action_queue.dart';

/// Lệnh xếp hàng ngoại tuyến của App hiện trường MKG — gọi api-internal
/// (vùng mkg). Khai báo ở app (KHÔNG ở lõi dùng chung) để bản build App hiện
/// trường KN (vùng nền tảng) không chứa đường dẫn `/v1/fieldops`,
/// `/v1/logistics` (phân vùng mạng §29, R-SEC-01). Đăng ký qua
/// `ActionQueue(extraKinds: MkgActionKind.values)` trong `AppServices`.
enum MkgActionKind implements ActionKind {
  /// G3: `POST /v1/fieldops/orders/{id}/crew-shifts {crew_ref, kind,
  /// headcount, event_time, device_id}` — mở / đóng ca tổ gom.
  fieldopsCrewShift('fieldops_crew_shift'),

  /// G4 (tài xế): `POST /v1/logistics/trips/{id}/milestones {kind,
  /// event_time, lat, lng}` — mốc xuất phát / đến của chuyến (chỉ tiến, L-02).
  logisticsMilestone('logistics_milestone');

  const MkgActionKind(this.code);

  @override
  final String code;

  @override
  String get method => 'POST';

  @override
  String pathFor(String? target) => switch (this) {
    MkgActionKind.fieldopsCrewShift =>
      '/v1/fieldops/orders/$target/crew-shifts',
    MkgActionKind.logisticsMilestone =>
      '/v1/logistics/trips/$target/milestones',
  };
}
