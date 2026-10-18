import 'package:agrigreen_field/domain/pii_hint.dart';
import 'package:flutter_test/flutter_test.dart';

// X-02 (W11.02): cảnh báo dữ liệu cá nhân ở ô tự do — cùng kết quả với web-ops src/kn/pii.tsx.
void main() {
  test('X-02 nhận ra SĐT, email, CCCD; không báo số liệu thường', () {
    expect(detectPii('Liên hệ anh Ba 0912 345 678'), [PiiKind.phone]);
    expect(detectPii('+84912345678'), [PiiKind.phone]);
    expect(detectPii('gửi ba@vi-du.vn'), [PiiKind.email]);
    expect(detectPii('CCCD 087123456789'), [PiiKind.nationalId]);
    expect(detectPii('Diện tích 126,5 ha, 3 HTX, năm 2026'), isEmpty);
    expect(piiWarning(''), isNull);
    expect(piiWarning('0912345678'), contains('số điện thoại'));
  });
}
