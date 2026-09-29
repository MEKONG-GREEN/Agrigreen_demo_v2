/// Yêu cầu hỗ trợ của nông dân (HTX US-SUPPORT-01) — payload `SupportPayload`
/// của pf/extension (docs/dev/sync-contract.md, entity `support_request`).
library;

/// Loại sự cố (`category`).
const supportCategories = <String, String>{
  'dich_hai': 'Dịch hại',
  'benh_lua': 'Bệnh lúa',
  'thoi_tiet': 'Thời tiết bất thường',
  'ky_thuat': 'Vướng mắc kỹ thuật',
  'may_moc': 'Máy móc',
  'khac': 'Khác',
};

const supportMaxPhotos = 5;

const msgSupportRequired =
    'Vui lòng nhập mô tả và chọn loại sự cố trước khi gửi.';
const msgSupportTooManyPhotos =
    'Vượt quá số lượng ảnh cho phép (tối đa 5) hoặc dung lượng ảnh quá lớn.';
const msgSupportNoPii =
    'Mô tả không được chứa số điện thoại hoặc số CCCD (dữ liệu cá nhân).';

/// Chuỗi 9–12 chữ số liền (bỏ khoảng trắng, dấu chấm, gạch) — SĐT/CCCD.
final _piiDigits = RegExp(r'(?:\+?84|0)?\d(?:[\s.\-]?\d){8,11}');

/// null = hợp lệ; ngược lại là thông báo (AC-3 và quy tắc máy chủ).
String? validateSupport({
  required String? category,
  required String description,
  required int photos,
}) {
  final d = description.trim();
  if (category == null ||
      !supportCategories.containsKey(category) ||
      d.isEmpty) {
    return msgSupportRequired;
  }
  if (d.length < 10) return 'Mô tả cần ít nhất 10 ký tự.';
  if (d.length > 2000) return 'Mô tả tối đa 2000 ký tự.';
  if (_piiDigits.hasMatch(d)) return msgSupportNoPii;
  if (photos > supportMaxPhotos) return msgSupportTooManyPhotos;
  return null;
}

String supportStatusLabel(String? taskStatus) => switch (taskStatus) {
  'new' => 'Đã gửi — chờ tiếp nhận',
  'assigned' => 'Đã giao cán bộ',
  'accepted' => 'Cán bộ đã nhận',
  'in_progress' => 'Đang xử lý',
  'resolved' => 'Đã xử lý xong',
  _ => 'Đã gửi',
};
