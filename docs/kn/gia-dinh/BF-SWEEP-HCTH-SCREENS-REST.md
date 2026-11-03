# BF-SWEEP-HCTH-SCREENS-REST (#203) — điều chỉnh kế hoạch nhân lực đã chốt

Nguồn: BRD HCTH v3.0 FN-11, BR-01/BR-02 và ngoại lệ kế hoạch đã chốt; UI-KIT R-A1/A2/A4/A5. Lát này tách độc lập từ phần còn lại sau #197; reservation đã ghi tại issue203. Các symbol và hợp đồng Plan đã có trên dev086708 trước #197, không stack/cherry-pick/import rule-form của197.

## Phạm vi

Thay đúng popup Điều chỉnh kế hoạch đã chốt bằng canonical route `/kn/nhan-su/ke-hoach/<year>/<unit>/dieu-chinh`; lịch sử `.../lich-su` và bản đã ghi `.../<seq>` dùng actual seq/actor_ref/recorded_at/reason/values của HrmPlanUnit.adjustments. Không giả người/thời gian từ người đang xem. URL số không hợp lệ hoặc seq không có không fallback latest. Chi tiết cũ chỉ đọc, bảng có STT. Chỉ sửa metadata của adjustment trong Log legacy liên quan; các Log/Dialogs khác chưa được sweep.

Form chỉ đổi leaving/additional (số nguyên0..9999) và reason10..500 codepoints theo Go TrimSpace/RuneCount. Chuỗi số giữ dạng raw trong NumberField, trần32 trước chuẩn hóa/so sánh, không nhận exponent hoặc chuỗi rất dài. Lý do đếm Unicode codepoints, NEL/NBSP được trim theo White_Space, FEFF không bị trim; không dùng HTML maxLength500 UTF16 làm mất500runes hợp lệ. Lỗi trường đã sửa được kiểm lại riêng, không giữ đỏ lỗi cũ hoặc xóa lỗi trường khác. Giữ positions/note từ bản server, số hiện có/nghỉ hưu theo ngày chốt. Không thêm CAS/base_version/serverdraft giả vào API adjustment hiện hữu.

Quyền do server can_adjust và kỳclosed, thêm fail-closed ở UI theo phiên/read-only/MFAAdmin. Identity gồm principal/tenant/context/support/API và route; đổi identity bỏ nháp và vô hiệu phản hồi muộn. Hủy/Back không ghi. useAction giữ retry key khi mất ACK, chống double-submit; thành công điều hướng đúng seq thực. Không browser-storage authority, không sửa cap/forms closed snapshot hoặc lịch sử cũ.

API/schema/productionGo/migration/menu không đổi. Mock adjustmentreason sửa duy nhất trim/codepointcount cho khớp backend hiện hữu, không thay luật các handler khác. Mẫu giấy/vị trí cũ giữ nguyên; Plan hiện dùng DetailLayout/lv-grid, không gọi là FormGrid mới. Không sửa sharedform/context/common/shell/menu/Catalog/Rule và ba docs chung đang review trong197.

## Còn lại

Formal child #208 giữ Staff/Promotion/Report/Decision/Doc và các PlanDialogs/workflow khác (mở kỳ, nhập vị trí, yêu cầu điều chỉnh trước chốt, bảng tổng hợp...) cùng sweep/Log/NumberField còn thiếu. #197 là tham số FN19 riêng, #179 bảng hệ số, #183 multipart; các hợp đồng này không bị viết lại. Không tuyên bố toàn FN11/HCTH, G1 hoặc UAT/500VU/Q070 đã đạt từ lát này. Sau khi203 được review/gộp mới tiếp phần dùng chung theochild; PO/mẫu Sở giữ trạng thái chưa chốt hiện hữu.

## Kiểm chứng cần đạt trước READY

Legacy numeric RED8 cho hai ô với exponent/100kdigits từng phát POST; giữ log, không đổi baseline. Replacement route phải có hostile0POST, normal controls, Unicode9/500runes, immutable snapshot/realmetadata/exactseq, lostACK/retry, readonly/context/MFA/year/lateACK và cancel. Controlled mutation bỏ lateACK guard phảiFAIL, restore byte/controlPASS. Native HTTP qua HRM/forms, owner-session codec và middleware idempotency thật; pool vận hành dùng db.ForModule(pool,db.Ops) đúng assembly, không AdminPool. Hạ tầng identity/unit trong fixture không được ghi là OIDC/staging/UAT thật. Đủ nativePG0SKIP và8migrationgates/fullweb/E2E/screenshot/gates/lint/CIexactSHA trước ticketreadyReviewerClaude. Kết quả/lệnh/SHA cuối lưu tại bằng chứng ready; tài liệu này không tự chứng nhận các lượt đang chờ.
