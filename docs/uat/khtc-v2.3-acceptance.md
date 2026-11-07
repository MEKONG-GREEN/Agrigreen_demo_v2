# KHTC v2.3 — nghiệm thu phần độc lập S-7

Chỉ phạm vi #267/#220, không thay kết luận các gói KHTC khác hoặc nghiệm thu toàn module. Căn cứ Function chi tiết FN-TI-02.01, FN-TI-02.05–02.09, FN-AD-04.03; UR-TI-06–10/12/13, BR-DL-04/09/19, BRD 2.0 KHTC v2.3.

| Ca | Kết quả cần đạt | Bằng chứng tự động |
|---|---|---|
| Menu Tỉnh | Không còn hai khóa nhập ngoài/mẫu báo cáo Tỉnh; template Admin giữ | `TestStandalone267RemovesOnlyRetiredKHTCLeaves`, menu parity KN77 |
| URL/API cũ | Web status404 thật; mọi12 thao tác external cho Tỉnh/Admin404 | Playwright `kn-khtc-standalone267`, `TestStandalone267RetiredExternalAPI` |
| Dữ liệu cũ | Role app INSERT42501; SELECT còn, UPDATE/DELETE/TRUNCATE bị thu; không xóa dữ liệu | `TestStandalone267OldExternalDataReadOnly` |
| Nguồn số mới | New draft/explicit Refresh không có External; KN thay đổi vẫn phát hiện stale | `TestStandalone267FreshCaptureIgnoresLegacyAndFrozenBytesSurvive` |
| Bản đã lưu | Đọc, Save lời văn, xuất và idempotent giữ capturedData/InputHash; raw payload/row_hash cũ và chain không đổi; Excel/Word tải lại đúng byte | Cùng ca DB và hồi quy archive/Word KN77 |
| Mẫu1–5 | OOXML nội dung/style/relationship/data không đổi so bản trước gỡ không có ô ngoài; payload legacy giải mã/render không đổi | `TestStandalone267AllFiveExportBaseline` (20 tài liệu) |
| HTX tắt | Reader chỉ cần published closed-family; không lookup xác nhận hay gán lại nhãn/băm | `TestStandalone267ClosedFiguresPreservePublishedRowsWithoutHTX` |
| UI | Admin mẫu/mục lục và màn soạn vẫn mở, không khối Nhập ngoài; desktop/mobile | Playwright S-7 và Vitest |

Trạng thái chỉ ghi đạt sau log thực trên SHA được tích hợp; UAT người dùng và profile chỉ-KN toàn hệ thuộc gói S-10/S-11. [Cách chạy và giới hạn](../kn/gia-dinh/KN-STANDALONE-7-KHTC.md).
