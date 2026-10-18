## KNSPLIT-2-PLATFORM-READERS-B — Nối địa bàn và tổ chức công bố vào KN

Nền deve6a54bcc. Chỉ thay dependency tại các gốc lắp ráp production KN: extension trong API/worker jobs/report/notify; ReportBuilders/ReportSessionResolver của KN; đơn vị đã capture cho DirectiveNotificationBuilders và KNContactLabels. Dùng NewPublishedAdminUnits/NewPublishedOrganizations đã được đối chiếu624case ở A. Extension.New vẫn giữ optionalnilDeps như trước, không tự gắn adapter ở mọi constructor/fixture.

Giữ nguyên các cổng metadata native của cooperative/mechanization/geo/content, reports.Deps.Orgs/Units phục vụ native HTX/MKG, notify.Deps.Units và accounts generic để không đổi ứng dụng khác. Chưa xử lý production source binding/ctxvars/accounts/party/routes/tenant; không coi KN độc lập chỉ vì các deps trực tiếp này đã qua pub. Catalogs/Params vẫn là năng lực registry dùng chung đã tách policy ở các gói trước.

Không DDL/RLS/ACL/schema, không thay cổng/DTO/query của bộ đọc đã tích hợp.34golden nền e6a phải nguyên; quyền26flags59SELECT24membership nguyên; boundary314/directHTXimports0 không tăng. Không đổi HTTP/OpenAPI/menu/web/mobile. Giữ các quyết định PO về tenant mỗi Trung tâm tỉnh, MRV HTX/MKG, D2 G1 chỉ hồ sơ KN, mẫu KHTC nạp sẵn và M1–M7.

Nguồn kiểm tra cần chạy thật: platformdata9+new session/report metadata cases, HTX14/dashboard với pubUnits, extension handler full, tests/reports full, tests/notify full; pure metadata/golden builders, toàn bộ static; full5consumer dbadmin/metrics/KN77/API assembly/worker. Helper11migration và source suites thật trước dev, fail-on-SKIP. Chưa chạy tại mốc viết. Worker fake senders, không SMTP thực/shared5432. Giữ main và mọi worktree; G1/lịch tự tiếp tục còn.

Kiểm thêm của B: dashboard HTTP dùng cả pubUnits/pubOrganizations như production; ReportBuilders so nguyên SourceData/nhãn tỉnh/xã/HTX với nguồn native trên2tenant; ReportSessionResolver so180quyết định với principal thật trongfixture/3tenant, gồm node không có, vai không cho phép và mã tỉnh cụ thể. Không giả Principal trong job production.
