import 'dart:convert';

import 'package:agrigreen_mkg_field/domain/roles.dart';
import 'package:agrigreen_mkg_field/domain/trips.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/fake_logistics.dart';

LocalMilestone _lm(Milestone m, MilestoneState s) => LocalMilestone(
  actionId: 'a-${m.code}-${s.name}',
  tripId: tripA,
  milestone: m,
  eventTime: DateTime.utc(2026, 10, 1, 3),
  state: s,
);

/// Máy trạng thái mốc chuyến trên máy — phản chiếu `domain.CanTransition`
/// của mkg/logistics (L-02: chỉ tiến planned → departed → arrived →
/// completed, mốc tài xế từng bậc).
void main() {
  group('G4 L-02 máy trạng thái mốc (chỉ tiến)', () {
    test('mốc kế tiếp từng bậc; hết mốc tài xế sau khi đến / đóng', () {
      expect(nextMilestone(TripStatus.planned), Milestone.departed);
      expect(nextMilestone(TripStatus.departed), Milestone.arrived);
      expect(nextMilestone(TripStatus.arrived), isNull);
      expect(nextMilestone(TripStatus.completed), isNull);
      expect(nextMilestone(TripStatus.cancelled), isNull);
    });

    test('không nhảy bậc, không lùi, không ghi trên chuyến đã đóng', () {
      expect(checkMilestone(TripStatus.planned, Milestone.departed), isNull);
      expect(checkMilestone(TripStatus.departed, Milestone.arrived), isNull);
      // Nhảy bậc: planned → arrived.
      expect(
        checkMilestone(TripStatus.planned, Milestone.arrived),
        contains('Phải ghi "Xuất phát" trước'),
      );
      // Lùi / lặp.
      expect(
        checkMilestone(TripStatus.departed, Milestone.departed),
        contains('không ghi lùi'),
      );
      expect(
        checkMilestone(TripStatus.arrived, Milestone.departed),
        contains('không ghi lùi'),
      );
      expect(
        checkMilestone(TripStatus.arrived, Milestone.arrived),
        contains('không ghi lùi'),
      );
      for (final s in [TripStatus.completed, TripStatus.cancelled]) {
        for (final m in Milestone.values) {
          expect(checkMilestone(s, m), contains('hoàn tất hoặc đã bị hủy'));
        }
      }
    });

    test('trạng thái hiệu lực = máy chủ phủ mốc chờ / đã gửi; bỏ mốc bị từ chối; không bao giờ lùi', () {
      expect(effectiveStatus(TripStatus.planned, const []), TripStatus.planned);
      expect(
        effectiveStatus(TripStatus.planned, [
          _lm(Milestone.departed, MilestoneState.pending),
        ]),
        TripStatus.departed,
      );
      expect(
        effectiveStatus(TripStatus.planned, [
          _lm(Milestone.departed, MilestoneState.sent),
          _lm(Milestone.arrived, MilestoneState.pending),
        ]),
        TripStatus.arrived,
      );
      // Mốc bị từ chối không đẩy trạng thái.
      expect(
        effectiveStatus(TripStatus.planned, [
          _lm(Milestone.departed, MilestoneState.conflict),
        ]),
        TripStatus.planned,
      );
      // Máy chủ đã đi xa hơn bản trên máy → giữ máy chủ (không lùi).
      expect(
        effectiveStatus(TripStatus.arrived, [
          _lm(Milestone.departed, MilestoneState.sent),
        ]),
        TripStatus.arrived,
      );
      // Chuyến đã đóng trên máy chủ thắng mọi mốc trên máy.
      expect(
        effectiveStatus(TripStatus.cancelled, [
          _lm(Milestone.departed, MilestoneState.pending),
        ]),
        TripStatus.cancelled,
      );
    });
  });

  test(
    'bản lưu trên máy không có cước, lô, phiếu cân (tối thiểu hóa, RB-3)',
    () {
      final t = Trip.fromApi(tripData(driver: 'd1'), withLegs: true);
      final j = jsonEncode(t.toJson());
      for (final k in [
        'planned_cost',
        'actual_cost',
        'cargo',
        'rate_vnd',
        '11250000',
      ]) {
        expect(j, isNot(contains(k)), reason: k);
      }
      expect(t.legs.single.to.kind, 'facility');
      expect(t.destination?.label, 'Kho / nhà máy');
      expect(Trip.fromJson(t.toJson()).hasLegs, isTrue);
      expect(Trip.fromJson(t.toJson()).legs.single.distanceLabel, '42,3 km');
    },
  );

  test(
    'hiển thị: tấn ước, phương thức, trạng thái, mã lỗi logistics.* tiếng Việt',
    () {
      expect(tonnesLabel('12500.000'), '12,5 tấn');
      expect(tonnesLabel('90000'), '90 tấn');
      expect(tonnesLabel(null), '—');
      expect(modeLabel('waterway'), 'Đường thủy');
      expect(modeLabel('road'), 'Đường bộ');
      expect(TripStatus.label('departed'), 'Đang chạy');
      expect(
        logisticsMessage('logistics.trip_transition'),
        contains('chỉ ghi tiến từng bậc'),
      );
      expect(logisticsMessage('logistics.trip_closed'), contains('đã bị hủy'));
      expect(
        logisticsMessage('logistics.trip_not_found'),
        contains('Không còn chuyến'),
      );
      expect(logisticsMessage('xyz'), 'Bị từ chối (xyz).');
    },
  );

  test('vai tài xế độc lập với chế độ tổ gom / giám sát', () {
    expect(isDriver(const ['driver']), isTrue);
    expect(modeFor(const ['driver']), FieldMode.none);
    expect(isDriver(const ['field_collector']), isFalse);
    expect(isDriver(const ['field_collector', 'driver']), isTrue);
  });
}
