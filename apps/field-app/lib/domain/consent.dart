/// Mục đích thu minh chứng MRV (pf/party, ConsentInput.purpose).
const mrvEvidencePurpose = 'mrv.evidence';

class ConsentRequiredException implements Exception {
  ConsentRequiredException(this.subjectRef);
  final String subjectRef;
  String get message =>
      'Nông hộ chưa đồng ý cho mục đích thu minh chứng (mrv.evidence). '
      'Hãy đọc văn bản đồng ý và ghi nhận trước khi chụp ảnh.';
  @override
  String toString() => message;
}

/// Kiểm tra đồng ý trước khi thu minh chứng.
abstract class ConsentGate {
  Future<bool> isGranted(String subjectRef, String purpose);
}
