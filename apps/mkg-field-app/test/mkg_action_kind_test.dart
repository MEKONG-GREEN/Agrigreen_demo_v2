import 'package:agrigreen_mkg_field/application/mkg_action_kind.dart';
import 'package:agrigreen_mobile/data/actions/action_queue.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('G3/G4 mkg: lệnh ca tổ gom và mốc chuyến tới đúng đường dẫn', () {
    expect(
      MkgActionKind.fieldopsCrewShift.pathFor('o1'),
      '/v1/fieldops/orders/o1/crew-shifts',
    );
    expect(
      MkgActionKind.logisticsMilestone.pathFor('t1'),
      '/v1/logistics/trips/t1/milestones',
    );
    // Mã lưu trong api_action giữ nguyên như trước khi tách khỏi lõi.
    expect(MkgActionKind.fieldopsCrewShift.code, 'fieldops_crew_shift');
    expect(MkgActionKind.logisticsMilestone.code, 'logistics_milestone');
  });

  test('lệnh mkg do app khai báo, không trùng mã với lõi (§29, R-SEC-01)', () {
    final core = {for (final k in ApiActionKind.values) k.code};
    for (final k in MkgActionKind.values) {
      expect(core, isNot(contains(k.code)), reason: k.code);
    }
  });
}
