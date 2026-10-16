## KNSPLIT-2-REPORTS-I — Registry bộ mẫu khởi tạo

Nền59621835. Thêm FormSeedRegistration/FormSeedTemplate DTO và registry bất biến theo Service; một bộ mặc định rõ ràng, từ chối trùng key/mã, nhiều default, thiếu cấu hình hoặc định nghĩa không hợp lệ. Clone sâu định nghĩa khi nhận/trả; không nhận registry qua HTTP. SeedForms giữ quyền admin, tenant/actor từ Principal, cùng giao dịch và quy tắc giữ mã đã có/nháp v1/gán loại/bốn mắt. Endpoint và payload HTTP không đổi.

KN sở hữu ReportFormSeeds với đủ14định nghĩa, phòng ban mặc định và nhãn phiên bản; API/hai worker/KN77/tests-reports đăng ký provider thật, cmd/formimport lấy owner thật. Golden tests/golden/kn-report-form-seeds.json chụp từ legacy adapter trước xóa và đối chiếu byte owner. Lần compile đầu bắt thiếu ColumnIndex helper; payload được tạo lại bằng token-aware mapping giữ nguyên chuỗi, không nới assertion. I giữ legacy khi registry nil; explicit empty failclosed. J mới gỡ seed.go/wrapper/default KN trong capability và chuyển các ca dùng mẫu về nơi lắp ráp.

Pure-first đã đạt cả app/owner, đủ14mẫu byte/cách ly input-output/nested aliases/duplicate/invalid metadata và formula. Cần14PGpackage (thêm CLI có kiểm export T04 thật), static vàhelper8migrationgate trướcdev. Chưa đổi API/SQL/UI/tenant/D2/MRV/M1–M7; không nạp14tệp khách Q-01 chưa có.
