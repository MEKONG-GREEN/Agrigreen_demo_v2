import 'package:agrigreen_mkg_field/domain/fieldops.dart';
import 'package:agrigreen_mkg_field/domain/roles.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/test_env.dart';

void main() {
  const bale20 = StageCount(rolls: 20);
  List<String> rules(List<CountRuleError> e) => [for (final x in e) x.rule];

  group('proto FM-09: ở ruộng đếm cuộn', () {
    test('FM-09: bắt buộc số cuộn (bale)', () {
      final (e, c) = checkCount(stage: Stage.bale, rollsText: '');
      expect(c, isNull);
      expect(rules(e), ['FM-09']);
      expect(e.single.field, 'rolls');
    });

    test('FM-09: kg rơm rời chỉ khi 0 cuộn', () {
      final (e, _) = checkCount(
        stage: Stage.bale,
        rollsText: '5',
        looseKgText: '12',
      );
      expect(e.single.field, 'loose_kg');
      final (ok, c) = checkCount(
        stage: Stage.bale,
        rollsText: '0',
        looseKgText: '12,50',
      );
      expect(ok, isEmpty);
      expect(c!.loose, isTrue);
      expect(c.looseKg, '12.5');
    });

    test('FM-09: bản gốc 0 cuộn không kg bị chặn; bản bù được về 0', () {
      expect(rules(checkCount(stage: Stage.bale, rollsText: '0').$1), [
        'FM-09',
      ]);
      final (e, c) = checkCount(
        stage: Stage.bale,
        rollsText: '0',
        correction: true,
        correctionReason: 'Nhập nhầm thửa bên cạnh',
      );
      expect(e, isEmpty);
      expect(c!.empty, isTrue);
    });

    test('FM-09: kg không hợp lệ / quá 3 chữ số thập phân', () {
      for (final kg in ['-1', 'abc', '1.2345', '0']) {
        final (e, _) = checkCount(
          stage: Stage.bale,
          rollsText: '0',
          looseKgText: kg,
        );
        expect(e.single.field, 'loose_kg', reason: kg);
      }
    });
  });

  group('proto FM-03: thứ tự công đoạn', () {
    test('FM-03: gather khi bale chưa có số bị chặn', () {
      final (e, _) = checkCount(stage: Stage.gather, rollsText: '20');
      expect(rules(e), ['FM-03']);
    });
    test('FM-03: bale không cần công đoạn trước', () {
      expect(checkCount(stage: Stage.bale, rollsText: '20').$1, isEmpty);
    });
  });

  group('proto FM-04: bằng công đoạn trước, hoặc ít hơn kèm lý do ≥ 10 ký tự', () {
    test('FM-04: bằng → hợp lệ', () {
      final (e, c) = checkCount(
        stage: Stage.gather,
        rollsText: '20',
        previous: bale20,
      );
      expect(e, isEmpty);
      expect(c!.rolls, 20);
    });
    test('FM-04: nhiều hơn → chặn (rơm không tự sinh ra)', () {
      final (e, _) = checkCount(
        stage: Stage.boatLoad,
        rollsText: '21',
        previous: bale20,
      );
      expect(rules(e), ['FM-04']);
      expect(e.single.message, contains('nhiều hơn'));
    });
    test('FM-04: ít hơn không lý do / lý do < 10 ký tự → chặn', () {
      expect(
        rules(
          checkCount(stage: Stage.gather, rollsText: '18', previous: bale20).$1,
        ),
        ['FM-04'],
      );
      final (e, _) = checkCount(
        stage: Stage.gather,
        rollsText: '18',
        discrepancyReason: 'ướt',
        previous: bale20,
      );
      expect(e.single.field, 'discrepancy_reason');
    });
    test('FM-04: ít hơn kèm lý do ≥ 10 ký tự → hợp lệ', () {
      final (e, c) = checkCount(
        stage: Stage.gather,
        rollsText: '18',
        discrepancyReason: '2 cuộn bị ướt, bỏ lại',
        previous: bale20,
      );
      expect(e, isEmpty);
      expect(c!.discrepancyReason, '2 cuộn bị ướt, bỏ lại');
    });
    test('FM-04: khác đơn vị (cuộn ↔ rơm rời) bị chặn', () {
      final (e, _) = checkCount(
        stage: Stage.gather,
        rollsText: '0',
        looseKgText: '50',
        previous: bale20,
      );
      expect(rules(e), ['FM-04']);
    });
    test('FM-04: rơm rời so theo kg (gam nguyên, không số thực)', () {
      const prev = StageCount(rolls: 0, looseKg: '80.5');
      expect(
        checkCount(
          stage: Stage.gather,
          rollsText: '0',
          looseKgText: '80.500',
          previous: prev,
        ).$1,
        isEmpty,
      );
      expect(
        rules(
          checkCount(
            stage: Stage.gather,
            rollsText: '0',
            looseKgText: '80.501',
            previous: prev,
          ).$1,
        ),
        ['FM-04'],
      );
    });
    test(
      'FM-04 + RB-1: bản bù cần lý do sửa ≥ 10 ký tự; công đoạn sau vẫn thỏa',
      () {
        expect(
          rules(
            checkCount(
              stage: Stage.bale,
              rollsText: '25',
              correction: true,
              correctionReason: 'sai',
            ).$1,
          ),
          ['RB-1'],
        );
        // Sửa bale 20 → 15 khi gather đang 20 → gather nhiều hơn → chặn.
        final (e, _) = checkCount(
          stage: Stage.bale,
          rollsText: '15',
          correction: true,
          correctionReason: 'Đếm lại tại thửa, thiếu 5 cuộn',
          next: const StageCount(rolls: 20),
        );
        expect(rules(e), ['FM-04']);
        // Sửa bale 20 → 25 khi gather 20 không lý do → gather sẽ ít hơn → chặn.
        final (e2, _) = checkCount(
          stage: Stage.bale,
          rollsText: '25',
          correction: true,
          correctionReason: 'Đếm lại tại thửa, dư 5 cuộn',
          next: const StageCount(rolls: 20),
        );
        expect(rules(e2), ['FM-04']);
      },
    );
  });

  test('kg: gam nguyên ↔ chuỗi API', () {
    expect(parseKgGrams('1520,500'), 1520500);
    expect(formatKg(1520500), '1520.5');
    expect(formatKg(80000), '80');
    expect(parseKgGrams('1.2345'), isNull);
  });

  test('Stage: thứ tự bale → gather → boat_load', () {
    expect(Stage.gather.previous, Stage.bale);
    expect(Stage.boatLoad.next, isNull);
    expect(Stage.tryParse('boat_load'), Stage.boatLoad);
  });

  test('vai: field_supervisor > field_collector; sub từ JWT', () {
    expect(modeFor(['field_collector']), FieldMode.collector);
    expect(
      modeFor(['field_collector', 'field_supervisor']),
      FieldMode.supervisor,
    );
    expect(modeFor(['farmer']), FieldMode.none);
    final t = fakeJwt(['field_supervisor']);
    expect(rolesFromJwt(t), ['field_supervisor']);
    expect(subjectFromJwt(t), meSub);
    expect(rolesFromJwt('hỏng'), isEmpty);
  });

  test('mã từ chối fieldops.* có lời dễ hiểu', () {
    for (final c in [
      'fieldops.previous_stage_missing',
      'fieldops.stage_exceeds_previous',
      'fieldops.discrepancy_reason_required',
      'fieldops.unit_mismatch',
      'fieldops.next_stage_conflict',
      'fieldops.stage_already_counted',
      'fieldops.order_not_open',
      'self_approval_forbidden',
    ]) {
      expect(rejectMessage(c), isNot(contains(c)), reason: c);
    }
    expect(rejectMessage('fieldops.new_code'), contains('fieldops.new_code'));
  });
}
