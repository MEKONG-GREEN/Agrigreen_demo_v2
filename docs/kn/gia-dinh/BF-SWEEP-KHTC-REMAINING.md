## BF-SWEEP-KHTC-REMAINING — Cây kế hoạch qua route (#206)

Nguồn: BRD KHTC v2.3 Product Drive, DOCX SHA256 `15859351f90574bf667190b1a12160465ba03f8ca9e408b94c90d285a3502e25`, P779–P825; UI-KIT R-A1/A2/A4/A5; issue206 và reservation Plan-only. BRD v2.3 ưu tiên ma trận v2.2 lịch sử.

### Lát hiện thực

- CT/DA chủ mới và chọn CT/DA; nhóm/hạng mục thêm, sửa, xem trên route riêng trong layout workspace kế hoạch. Tạo CT/DA chủ ghi qua API hiện hữu; đưa CT/DA vào cây chỉ thay nháp cục bộ tới khi xác nhận Lưu phiên bản. Không thay hợp đồng hay ngữ nghĩa field_code/inheritance. Các giá trị lĩnh vực hiện hữu được giữ nguyên.
- NumberField và validateForm/lỗi từng trường cho thứ tự/số điểm0–100000, khối lượng nhỏ hơn10^16/scale4, kinh phí nhỏ hơn10^18/scale2. Giới hạn chuỗi thô trước chuẩn hóa, kiểm số mũ, độ chính xác/biên rồi mới đổi hai số nguyên hữu hạn bằng Number; tiền/khối lượng luôn là chuỗi chính xác. Server vẫn giữ guard JSON/Excel của194 và domain hiện hành.
- DataTable có STT và thứ tự cây cha trước con; sáu tab giữ nội dung. Xem phiên bản đã lưu dùng đúng snapshot và cây/tab, luôn chỉ đọc. Log lấy actor/thời gian/quyết định/ghi chú từ KhtcPlanVersion thật, không dựng Log cho chỉnh sửa cục bộ chưa lưu.
- Nháp cây, form dòng và đầu quyết định chỉ ở bộ nhớ workspace/năm. Điều hướng cùng workspace giữ dữ liệu; Hủy form bỏ riêng form đó, không ghi. Thay tenant/actor/context/vai/MFA/API hoặc rời workspace làm mất nháp theo identity194; không có nháp server giả.
- Một thao tác ghi dùng useAction/idempotency hiện hữu, thử lại sau mất ACK giữ key/body, chống bấm lặp. Chỉ ACK thành công của chính request trong workspace còn hợp lệ được tăng CAS base. ACK đến sau đổi route được ghi nhận vào base nhưng không điều hướng; giữ chỉnh sửa cục bộ phát sinh sau request. Không suy thành công từ head hiện hành và không bỏ CAS.

### Phạm vi còn lại

Chưa hoàn tất FN-TI-02.02, KHTC, BRD v2.3, UAT hoặc G1. Còn vòng đời năm Đang lập/Đang áp dụng/Đã kết thúc, nháp server/tự mở điều chỉnh, đối chiếu/Áp dụng/Hủy, chọn CT khi copy, tự sinh mẫu phiếu/tab/dòng, xóa theo số liệu và hướng dẫn Excel raw exponent. Copy/import legacy hiện hữu không trở thành bằng chứng cho vòng đời mới.

Phần năm/serverdraft giữ [#225 BF-SWEEP-KHTC-YEAR-LIFECYCLE](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/225). Phần non-Plan giữ [#226 BF-SWEEP-KHTC-NONPLAN-UI](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/226), gồm inbox/forms (giữ FormGrid), report editor, Adhoc/nguồn nhúng và luồng còn lại đã có child chính thức; không thuộc lát này. Không trùng #202 dashboard/live/nhắc phòng, #205 archive policy, #215 tiến độ/kho cho phòng, #216 Excel phiếu, #217 copy kỳ/3lần gửi, #220 menu. CAS/receipt kỳ native theo #209/#211/#212 khi đủ contract thật; gói này không đổi các đường đó.

### Kiểm chứng

Test RED trước sửa chứng minh ACK muộn làm lần sau gửi base1 thaybase2. Test route/native API, ô số mũ1e±100000/100000chữ số/giá trị lớn/scale, dirty/Cancel/context/readonly/lateACK, masterCTDA lostACK, immutable snapshot và mutation FAIL→restore/control PASS. Bằng chứng fullweb, Go gates/lint, PG thật0SKIP/ALLmigrations, E2E/screenshot và CI đúng SHA ghi trong bàn giao/READY sau khi chạy; mô tả này không tự chứng nhận cổng chưa chạy.
