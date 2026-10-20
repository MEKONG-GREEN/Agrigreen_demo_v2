## KNSPLIT-2-ACCOUNTS-A — Nhật ký hỗ trợ và đơn vị mặc định theo chủ phiên

Gói chuyển hai điểm lấy quyền từ Principal.KN trong accounts sang callback hẹp cấu hình theo instance. Chủ KN cung cấp dữ liệu nhật ký (context/session/on-behalf/audience/phòng/lý do) và đơn vị mặc định. Kernel không nhập KN hoặc giải payload; actor, tenant, MFA và realm roles lấy nguyên từ xác thực.

Nhật ký thiếu chủ/hỗ trợ vẫn403; attempted/completed, trace, lý do đã được middleware kiểm, ledger/idempotency/API không đổi. Đơn vị đọc tường minh giữ lựa chọn trong tenant; resolver lỗi là kết quả cuối kể cả có query. KN bound UnitRef rỗng không quay về token OrgRef; trả validation. Native thật không có selected/legacy giữ đường tổ tiên OrgRef. Callback chưa cấu hình mà có selected owner bị403. Legacy hiện diện ở adapter KN chỉ dùng để từ chối binding thiếu, không cấp quyền.

Không sửa issuance/context/nav/attrs/delegation, job schedule, DDL/ACL/API/35 golden. Không đổi quyết định PO tenant từng Trung tâm tỉnh, MRV HTX/MKG, D2 chỉ hồ sơ KN, mẫu KHTC nạp sẵn, M1–M7. Kiểm thử nguồn/kernel generic fixture và public-root owner/PG sẽ ghi bằng chứng thực; chưa coi compile hoặc SKIP là PASS.
