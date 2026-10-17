## KNSPLIT-2-MDCATALOG-B — Bỏ danh mục CNTS ngầm

Nền 7c399eca (A đã tích hợp). Xóa 12 định nghĩa/nhãn/validator CNTS và 60 dòng seed khỏi đường mặc định của masterdata; module KN đăng ký bằng MasterdataCatalogs như A. Nil/empty registry không tự suy danh mục, hiệu lực hay seed của KN. Giữ 55 dòng seed gốc cùng thứ tự/giá trị, không đổi schema/wire/quyền/RLS/giao dịch/sổ. Dữ liệu và tham số MRV vẫn thuộc HTX/MKG.

Đường RED TestCatalogsRequireRegistration (nil/empty) thất bại trên A trước sửa, rồi đạt trên B. Giữ nguyên byte golden 12 schema/60 seed/170 ca và dữ liệu owner. Chuyển toàn bộ 25 hàm Test (gồm TestMain) HTTP sang tests/masterdata; từng hàm kiểm thử giữ nguyên byte, fixture mặc định lắp KN thật. Fixture generic WithDeps/WithParams vẫn chỉ dùng đăng ký được truyền, chứng minh danh mục hiệu lực và tham số hoạt động không cần KN.

Bốn kiểm thử schema cũ chuyển sang tests/masterdatacatalogs qua facade root + provider KN thật; test ParseDoses giữ ở core trong lúc typed bridge còn tồn tại. Ca thiếu xã bỏ lỗi trùng mã GS/GC trước để kiểm đúng thiếu xã. Không sao chép validator vào fixture, không giảm assertions. Capture generator cũ xóa; golden đóng băng giữ.

Phạm vi hồi quy: masterdata và tests/masterdata, extension root/app/store/handler, reportcycle, cntsalert, cntscal, outbreak, API assembly, worker, KN77, frozen composition. Bốn gói thuần đạt; lượt thử đầu khai nhầm tests/masterdataparams không tồn tại trả exit1, không dùng làm bằng chứng. Chạy PG riêng thật, toàn bộ cổng tĩnh và helper 9 migration (8 bắt buộc + CatalogRegistryConstraintUpgrade) trước dev. Web/mobile không đổi hợp đồng/UI nên không cần chạy lại.

Chưa xong toàn masterdata: typed CNTSCatalogs/ref/commune/stamp/FacilityCodeByRef/DTO còn ở core và sẽ tách ở gói kế tiếp. Các registry authz/accounts/party/notify, sản phẩm dữ liệu, route KN, tenant theo Trung tâm tỉnh, client kn-web và nhắc phiếu KHTC còn trong G1. Không thay quyết định PO, không sửa main, giữ worktree.
