## KNSPLIT-2-MDPARAMS-A — KN đăng ký tham số vào masterdata

Nền 05b77fab. Thêm port.ParamRegistration (mô tả khóa + seed scalar), registry immutable theo từng instance, sao chép cả con trỏ value/text, kiểm trùng/chủ sở hữu/định dạng trước khi khởi động. Metadata giới hạn số giữ vai trò gợi ý như trước; không tự đổi quy tắc nghiệp vụ.

KN sở hữu 16 mô tả extension.* / cnts.* và đúng 13 seed có sẵn (không phải 15). Golden tests/golden/kn-masterdata-params.json chụp từ đường cũ độc lập trước khi xóa, giữ nguyên nhãn, nguồn, ngày hiệu lực và giá trị thập phân. API và worker đăng ký tại 6 file composition root, tổng 10 constructor calls; command không lắp ráp KN giữ capability gốc. Không đẩy MRV sang KN.

Seed/khôi phục mặc định và danh sách metadata dùng registry; vẫn áp tenant từ principal, giao dịch/RLS, idempotency và hai tác nhân hệ thống khác nhau. Khôi phục tạo nháp và duyệt bốn mắt như cũ. Lỗi JSON/khóa trùng/xung đột metadata hoặc seed dừng khởi động. A vẫn giữ dữ liệu KN cũ, chỉ chấp nhận bản đăng ký trùng giá trị để tương thích; B xóa nguồn cũ sau khi A đạt và tích hợp dev. Chưa tách toàn bộ catalog CNTS.

Không đổi OpenAPI, JSON API, UI, schema, migration hay PO D2/tenant/MRV/M1–M7. Không tạo luồng đề nghị HTX trong G1. Cần hồi quy PostgreSQL thật, đầy đủ cổng tĩnh và 8 cổng migration trước dev.


Kết quả thực trên production27248e07: PG90646.exit0 đủ16/16 package có test, 0FAIL/SKIP (handler:282.126s, handler:114.069s, handler:106.552s, handler:117.097s, assembly:111.522s, worker:2.764s, kn77:301.189s). TestRegisteredParamsSeedRestoreAndTenantIsolation PASS6.75s; frozen16spec/13seed, registry invalid/crossowner/deepcopy/isolation/color đạt. Static81712.exit0 đủ build/vet/modgen/migrate-base/archlint/contract/docs/MFA/boundary382/lint0issues; gofmt chỉ2fixturecũ. ProofTEMP agrigreen-codex-knsplit2mdparamsa-proof.json. Helper còn8migrationgate trước dev. B mới xóa legacy và chuyển11keys/helper về KN.
