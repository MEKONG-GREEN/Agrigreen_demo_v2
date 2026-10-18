/// X-02 (RB-3, W11.02): nhận ra dữ liệu cá nhân gõ vào ô tự do (ghi chú, lý do) để cảnh báo — không chặn lưu.
/// SĐT, email, số CCCD/CMND phải nằm ở kho định danh; ô tự do được lưu nguyên văn trong sổ và nhật ký.
/// Cùng mẫu nhận dạng với web-ops (`src/kn/pii.tsx`).
library;

enum PiiKind { phone, email, nationalId }

final _email = RegExp(r'[^\s@,;]+@[^\s@,;]+\.[a-zA-Z]{2,}');
final _phone = RegExp(r'(?:\+?84|0)(?:[\s.-]?\d){9}(?!\d)');
final _id12 = RegExp(r'(?<!\d)\d{12}(?!\d)');
final _id9or12 = RegExp(r'(?<!\d)(?:\d{12}|\d{9})(?!\d)');

/// Các loại dữ liệu cá nhân nhận ra (không trả chính giá trị).
List<PiiKind> detectPii(String? text) {
  if (text == null || text.isEmpty) return const [];
  final out = <PiiKind>[];
  if (_email.hasMatch(text)) out.add(PiiKind.email);
  if (_phone.hasMatch(text)) out.add(PiiKind.phone);
  if (_id9or12.hasMatch(text) &&
      (!out.contains(PiiKind.phone) || _id12.hasMatch(text))) {
    out.add(PiiKind.nationalId);
  }
  return out;
}

const _label = {
  PiiKind.phone: 'số điện thoại',
  PiiKind.email: 'email',
  PiiKind.nationalId: 'số CCCD/CMND',
};

/// Câu cảnh báo dưới ô tự do (helperText), null khi không nhận ra gì.
String? piiWarning(String? text) {
  final kinds = detectPii(text);
  if (kinds.isEmpty) return null;
  return 'Ô này có vẻ chứa ${kinds.map((k) => _label[k]).join(', ')} — '
      'không ghi dữ liệu cá nhân vào ô tự do.';
}
