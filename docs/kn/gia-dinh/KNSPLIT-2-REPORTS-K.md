## KNSPLIT-2-REPORTS-K — Đăng ký nhãn và namespace gửi báo cáo

Nền5e116e26. Port.Policy.DispatchProfile nhận5scalar SubjectDepartment/Salutation/SenderName/TemplatePrefix/SheetTemplateKey, không actor/tenant/scope. Registry copyvalue, trảbyvalue, từ chối nhiều profile/thiếu nhãn/key sai; không nhận qua HTTP. KNReportPolicy sở hữu các giá trị cũ. Không dùng callback khi chỉ cần metadata.

Core lấy profile trước tác dụng phụ tạo/xếp gửi/worker/DLP ở3đường tải/xuất:4site khóa egress vàdefaultSubject/Body. Giữ kiểmtoken/tenant/recipient/scopes/PII/DLP và vị trí kiểm lỗi dữ liệu hợp lệ; email dedup/link riêng từng người nhận không đổi. Generic BodyFor dùng nhãn đăng ký. NilPolicies còn legacy profile trongK, explicit empty không suy KN. FormatterBodycũ còn để capturebyte; L mới negative/gỡ2đườnglegacy.

Golden tests/golden/kn-report-dispatch-profile.json chụp originalBody/Subject và các khóa cũ;4ca closed/provisional/missing/links/empty. Ownerprofile so5scalar với artifact, coregenericrender soSubject/Body/egresskeys byte; kiểm input-output alias/từngService/invalid/duplicate. Giữ API/SQL/UI, GoogleSheetdefaultoff và tất cả quyết định PO. Cần13PGconsumer/static/helper8migrationgate trướcdev.
