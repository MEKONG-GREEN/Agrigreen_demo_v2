## KNSPLIT-2-REPORTS-F — Bỏ catalog KN mặc định trong lõi

Nền dev2a877d33 đã đăng ký catalog KN ở API/hai worker/KN77/tests-reports. F xóa legacySourceSpecs/legacySources,5kn.*builtin và provisionalNote riêng KN trong domain. Default Sources/Builtins và nil Service chỉ có4nguồn/4mẫu native CGH/HTX; nguồn/mẫu KN chỉ đến từ CatalogRegistration. Không mutate global hoặc thêm import KN trong capability.

TestReportCatalogRequiresRegistration trước sửa (red65265.exit1/app1.630s) bắt missing catalog vẫn suy5source/5builtin, empty registry đã đúng; registered case vẫn đầy đủ. Sau sửa pure-first77518.exit0 app1.903s/domain0.606s/extension1.463s,0FAIL/SKIP: nil/empty không suy KN, registered9source/9builtin, quyền10vai/mixed HTX và các guard phiên vẫn đạt. Các vòng kiểm quyền đã chuyển từ domain.Sources mặc định sang instance catalog chứa golden5source KN để không vô tình giảm phạm vi khi default còn4source.

Golden tests/golden/kn-report-catalog.json giữ nguyên byte, owner KN vẫn kiểm toàn bộ metadata. Helper capture legacy đã bị xóa cùng adapter; fixture giờ chỉ đọc golden khóa từ E, không tái tạo bằng code mới. TestRegisteredCatalogColumnLevels giữ quy tắc mục tiêu/tỷ lệ tỉnh ẩn ở xã, tên xã ẩn ở tỉnh, đồng thời kiểm cột đúng thực sự có mặt. Domain validator/visibility test dùng nativeCGH hoặc nguồn generic riêng, không dựa metadata KN ngầm.

Không thay API/SQL/UI/tenant/D2/MRV/M1–M7, không sửa seed14mẫu trong F. Bộ dựng dữ liệu KN trong app/build.go và14gridseed còn cần registry/owner trong gói sau, chưa coi toàn bộ reports hoàn tất. Cần đủ PG13package/consumer/static/helper8migrationgate trước dev.
