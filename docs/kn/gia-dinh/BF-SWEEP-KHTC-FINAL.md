# BF-SWEEP-KHTC-FINAL (#194) — phiên bản kế hoạch năm

Nguồn: BRD KHTC v2.3, 08/10/2026, Product Drive, DOCX SHA256 `15859351f90574bf667190b1a12160465ba03f8ca9e408b94c90d285a3502e25`; FN-TI-02.02 P782–P825; PRD R-E1/R-A1–A5 và UI-KIT. BRD v2.3 ưu tiên hơn hợp đồng W11.61/v2.2 và prototype. Prototype chỉ là đặc tả, không chép mã.

## Lát trọn vẹn trong gói

- Lưu/Điều chỉnh theo quyết định, lịch sử và xem một phiên bản có route riêng dưới `ke-hoach-nam`; alias phòng ban dùng cùng workspace. Form quyết định dùng DetailPage/TextField/validateForm, dấu bắt buộc và lỗi theo trường; ngày thật, đúng năm hiệu lực, độ dài số QĐ100/ghi chú1000 giữ hợp đồng server. Lịch sử và nội dung phiên bản dùng DataTable/STT; Log lấy đúng actor/recorded_at/quyết định/ghi chú từ KhtcPlanVersion, không tạo người hay thời điểm giả.
- URL năm2000–2100, phiên bản số nguyên dương int32 phải có cú pháp/độ dài hợp lệ trước Number. URL sai hoặc phiên bản không có không rơi về mới nhất. Xem phiên bản đã lưu luôn chỉ đọc, kể cả đó là phiên bản mới nhất.
- Layout giữ cây nháp và đầu quyết định khi chuyển các trang trong cùng năm/phiên workspace; Hủy không ghi, bỏ đầu quyết định nhưng giữ cây chưa lưu. Đổi actor/tenant/context/vai/read-only/MFA hoặc API client tạo lại workspace; không lưu nháp hoặc authority trong browser storage. Rời hẳn workspace, đổi năm hoặc reload không phải lưu nháp bền vững.
- Chỉ gửi khi có thay đổi và còn quyền lập kế hoạch; nút Lưu chống lặp khi đang gửi. useAction/idempotency middleware giữ khóa theo thao tác và thân khi mất ACK; base_version vẫn kiểm lạc quan. Phản hồi cũ sau rời trang/đổi ngữ cảnh không điều hướng phiên mới. Phiên bản cũ không bị ghi đè.
- Biên JSON dùng custom decimal decoder: chặn raw quá dài, số mũ, dạng không phải thập phân thường trước decimal parser; mọi occurrence kể cả khóa trùng/case-fold đều được kiểm. JSON null vẫn là nil; số thường có giới hạn, số âm có giới hạn vẫn đến validation domain như trước. Không thay biên nghiệp vụ lượng <10^16/scale4, kinh phí <10^18/scale2.
- Import Excel/CSV dùng cùng parser thuần có giới hạn trước decimal arithmetic; sort/points kiểm chuỗi nguyên có giới hạn trước Atoi. Dòng sai không vào nháp và preview không lưu phiên bản; dữ liệu hợp lệ tiếp tục đi qua validation hiện có. Không migration mới, không đổi API shape/Principal/RLS/menu/shared UI, không I/O mới trong TX và không ghi HTX.

## Phần còn lại chính thức, chưa đạt v2.3/UAT

[#206 BF-SWEEP-KHTC-REMAINING](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/206) giữ CT/DA/nhóm/hạng mục còn Dialog, chuẩn hóa NumberField/STT/Log còn lại và inbox/forms/reports/Adhoc/nguồn nhúng. Vòng đời năm Đang lập/Đang áp dụng/Đã kết thúc, nháp điều chỉnh trên server, đối chiếu/Hủy/Áp dụng, chọn CT khi sao chép, tự sinh biểu/tab/dòng theo P795–P825/BR-DL-21 chưa được hiện thực từ lát này. Không dựng trạng thái giả ở client, không coi nháp trong bộ nhớ là nháp server; không tuyên bố hoàn tất FN-TI-02.02, KHTC hay UAT/G1. Dashboard/live/nhắc phòng giữ #202; kho Word giữ #199/#205. PO vẫn cần chốt và nghiệm thu phần vòng đời/chuyển tiếp server và quy tắc còn thiếu.

## Kiểm chứng

Unit JSON RED trên nguồn cũ chấp nhận số mũ và100000chữ số, sau guard GREEN; native HTTP/CSV phải chứng minh trả lỗi/loại dòng và sổ phiên bản không tăng, control điều chỉnh/replay/read-only và AnnualPlan hiện hành. Test web dùng đồng hồ giả lập, URL độc hại, ngày không tồn tại, giữ nháp, Hủy, mất ACK cùng key, phiên bản cũ/không có và phản hồi muộn/đổi ngữ cảnh. Controlled mutation phải FAIL rồi khôi phục/control PASS. Bằng chứng đủ gates/lint/fullweb/E2E/PG thật0SKIP/migration/CI đúng SHA nằm ở bình luận ready, chưa có nghĩa khách đã nghiệm thu. Reviewer Claude; chỉ merge qua ticket sau review chéo.
