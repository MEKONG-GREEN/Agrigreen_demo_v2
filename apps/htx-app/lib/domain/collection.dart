/// Thu gom rơm MKG phía HTX (G3-2, RB-6 chiều mua): lịch gom và bảng kê số
/// cuộn MKG gửi qua van công bố; quản lý HTX xác nhận hoặc tranh chấp. Quy
/// tắc giống máy chủ (pf/cropcycle `domain.CheckResponse`).
library;

const scheduleStatusLabels = <String, String>{
  'planned': 'Dự kiến',
  'confirmed': 'Đã chốt',
  'in_progress': 'Đang gom',
  'completed': 'Đã gom xong',
  'cancelled': 'Đã hủy',
};

const msgDisputeRolls = 'Tranh chấp cần nhập số cuộn HTX đếm được (≥ 0).';
const msgDisputeSame = 'Số cuộn HTX bằng số MKG — hãy chọn Xác nhận.';
const msgReasonLength = 'Lý do cần từ 10 đến 500 ký tự.';
const msgSupersede =
    'Đã có phản hồi cho bảng kê này — ghi lý do thay đổi (10–500 ký tự).';
const msgNoPii = 'Lý do không được chứa số điện thoại hoặc số CCCD.';

final _piiDigits = RegExp(r'(?:\+?84|0)?\d(?:[\s.\-]?\d){8,11}');

/// null = hợp lệ. [superseding] = đã có phản hồi cho cùng revision.
String? validateCollectionResponse({
  required String outcome,
  required int mkgRolls,
  required int? htxRolls,
  required String reason,
  required bool superseding,
}) {
  final r = reason.trim();
  if (r.isNotEmpty && _piiDigits.hasMatch(r)) return msgNoPii;
  if (r.isNotEmpty && (r.length < 10 || r.length > 500)) return msgReasonLength;
  if (outcome == 'disputed') {
    if (htxRolls == null || htxRolls < 0) return msgDisputeRolls;
    if (htxRolls == mkgRolls) return msgDisputeSame;
    if (r.isEmpty) return msgReasonLength;
  }
  if (superseding && r.isEmpty) return msgSupersede;
  return null;
}
