## KNSPLIT-2-PLATFORM-READERS-B — Nối địa bàn và tổ chức công bố vào KN

Nền deve6a54bcc. Chỉ thay dependency tại các gốc lắp ráp production KN: extension trong API/worker jobs/report/notify; ReportBuilders/ReportSessionResolver của KN; đơn vị đã capture cho DirectiveNotificationBuilders và KNContactLabels. Dùng NewPublishedAdminUnits/NewPublishedOrganizations đã được đối chiếu624case ở A. Extension.New vẫn giữ optionalnilDeps như trước, không tự gắn adapter ở mọi constructor/fixture.

Giữ nguyên các cổng metadata native của cooperative/mechanization/geo/content, reports.Deps.Orgs/Units phục vụ native HTX/MKG, notify.Deps.Units và accounts generic để không đổi ứng dụng khác. Chưa xử lý production source binding/ctxvars/accounts/party/routes/tenant; không coi KN độc lập chỉ vì các deps trực tiếp này đã qua pub. Catalogs/Params vẫn là năng lực registry dùng chung đã tách policy ở các gói trước.

Không DDL/RLS/ACL/schema, không thay cổng/DTO/query của bộ đọc đã tích hợp.34golden nền e6a phải nguyên; quyền26flags59SELECT24membership nguyên; boundary314/directHTXimports0 không tăng. Không đổi HTTP/OpenAPI/menu/web/mobile. Giữ các quyết định PO về tenant mỗi Trung tâm tỉnh, MRV HTX/MKG, D2 G1 chỉ hồ sơ KN, mẫu KHTC nạp sẵn và M1–M7.

Nguồn kiểm tra cần chạy thật: platformdata9+new session/report metadata cases, HTX14/dashboard với pubUnits, extension handler full, tests/reports full, tests/notify full; pure metadata/golden builders, toàn bộ static; full5consumer dbadmin/metrics/KN77/API assembly/worker. Helper11migration và source suites thật trước dev, fail-on-SKIP. Chưa chạy tại mốc viết. Worker fake senders, không SMTP thực/shared5432. Giữ main và mọi worktree; G1/lịch tự tiếp tục còn.

Kiểm thêm của B: dashboard HTTP dùng cả pubUnits/pubOrganizations như production; ReportBuilders so nguyên SourceData/nhãn tỉnh/xã/HTX với nguồn native trên2tenant; ReportSessionResolver so180quyết định với principal thật trongfixture/3tenant, gồm node không có, vai không cho phép và mã tỉnh cụ thể. Không giả Principal trong job production.


Source5 thực tế24160 exit0, noFAIL/SKIP: platformdata11-161.256s, HTX14/dashboard106.502s, extension handlerfull91.123s, reportsfull79.924s, notifyfull80.548s.180session decisions và4nguyênSourceData/label comparisons mới đạt;624metadata/native111/38 và34goldens vẫn phải nguyên. Private59175/b9356f983d25 đã dọn. Pure/static90002=0/lint0/boundary314/gofmt2old. Full5wait59989 được phép bắt đầu sau source0; chưa kết luận full/helper/dev. GoALLfreeze.


Bằng chứng trước tích hợp: coreabaa48f1 giữ nguyên Go/SQL/metadata sau source5 actual0 (platform11-161.256s/HTX14-106.502s/handler91.123s/reports79.924s/notify80.548s). Pure/static90002=0/lint0/boundary314/fmt2old. Full5PG59989actual0: {"internal/shared/dbadmin": 1.188, "internal/platform/metrics/handler": 90.465, "tests/kn77": 217.393, "cmd/api-platform/assembly": 89.406, "cmd/worker": 2.01}; khôngFAIL/SKIP. Cụm59175/b9356f983d25 và51244/53c7445190f3 đã dọn/Go0.34golden nền e6a nguyên theoGitdiff, ACL26flags59SELECT24membership không đổi; khôngDDL/RLS/query/DTO/API mới. Fetchdev vẫn e6a, main27 sạch. Chờhelper11 với latestplatform/HTX/handler; reports/notify full đã actual0 trên cùng nguồn và khôngrebasecode, không cần lặp trừ source/upstream đổi.
