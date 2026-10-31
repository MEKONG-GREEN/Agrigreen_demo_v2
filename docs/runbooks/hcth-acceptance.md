# Nghiệm thu HCTH — W11.31

Phạm vi bộ kiểm hiện tại là [giả định W11.31](../kn/gia-dinh/W11.31.md).
Ma trận phòng duy nhất: [hcth-v3.0-acceptance.md](../uat/hcth-v3.0-acceptance.md).

## Chạy phần API ổn định

Từ gốc worktree feature đã claim W11.31, dùng Go đúng dòng `toolchain` trong `go.mod`.
Đối với Windows, dùng compiler C tương thích Go race đã được kiểm; cấu hình `CC`, `CXX`, `PATH`
chỉ trong tiến trình. Không cài vào hệ thống hoặc sửa cấu hình bảo mật để chạy ca này.

```powershell
$env:GOTOOLCHAIN = (Select-String -Path go.mod -Pattern '^toolchain\s+(\S+)').Matches[0].Groups[1].Value
$env:GOTMPDIR = 'C:\gotmp-kn'
$env:GOMAXPROCS = '2'
$env:GOFLAGS = '-p=1'
$env:CGO_ENABLED = '1'
go version
$hc30Log = Join-Path $env:TEMP 'hc30-native-race.log'
.\scripts\pg-ephemeral.ps1 run go test -v -race -count=1 -p 1 -timeout 55m ./tests/hc30 2>&1 | Tee-Object -FilePath $hc30Log
$hc30Exit = $LASTEXITCODE
if ($hc30Exit -ne 0) { throw "hc30 failed: $hc30Exit" }
if (Select-String -LiteralPath $hc30Log -Pattern '--- SKIP:|WARNING: DATA RACE|^FAIL') { throw 'Không chấp nhận SKIP/race/FAIL' }
if (-not (Select-String -LiteralPath $hc30Log -Pattern '^--- PASS: TestHCTHStaffAsOfPermissionsContact_W1131')) { throw 'Thiếu bằng chứng PASS ca native' }
```

Lưu SHA/base, phiên bản Go/compiler, lệnh, thời gian, số PASS/FAIL/SKIP, log và bằng chứng cụm tạm đã dọn.
Lỗi PostgreSQL là lỗi hạ tầng; giữ log, đợi/dùng lại đúng quy trình cụm riêng. Không lấy `ok` khi chưa đặt DB làm PASS.
Đọc giới hạn fixture: auth, danh mục đơn vị và client vault là biên giả; nghiệp vụ HRM/Contact và sổ PostgreSQL là thật.

## Kịch bản còn phải thực hiện

| Nhóm | Dữ liệu / hành động | Kết quả cần thu | Trạng thái checkpoint |
|---|---|---|---|
| H11 đánh giá | QIII/2026; 470 người MS05, 24 MS06, cán bộ xã theo HS-37 | Đếm và đối chiếu danh sách đúng snapshot, phiếu/đề xuất/tổng hợp/bản ký | Chờ #19 và fixture 494 người; chưa PASS |
| H11 NLTX | Tháng 7/2026, bốn người theo dữ liệu tổng hợp có oracle riêng | Bậc/hệ số, quyết định và sự kiện bù, tệp đúng | Chưa chạy bộ bốn người |
| Tải nộp ngày hạn | Khoảng 500 lượt nộp phiếu trong cùng ngày hạn, khóa thao tác riêng/giữ khi retry | Không mất/trùng, tổng theo bước/đơn vị đúng, ghi phân bố thời gian và lỗi | Chờ #19; chưa đo |
| NFR đồng thời | 500 người đồng thời trên môi trường/token được phép; dashboard | Dashboard <5s; ghi cấu hình, latency, throughput, tỷ lệ lỗi và tài nguyên | Chưa đo; không thay bằng 500 goroutine hoặc 500 lượt/ngày |
| NFR thao tác/tra cứu | Thao tác thường và lọc/tìm với roster 1.000 hồ sơ theo FN07 AC01 | Thao tác <3s, tìm/lọc <2s; nguồn PII/vault và số kết nối được ghi rõ | Chưa đo trên vault thật |
| NFR xuất | NS01–06/NL02–04 khoảng 500 người, gồm thời gian dựng/tải tệp | <10s, đọc lại dữ liệu/SHA, ghi lỗi và cold/warm runs | Chưa đo đầy đủ các biểu |
| UI / OTP khách | UI #179 và link/OTP #184 sau khi merge dev | Browser native đúng quyền; OTP đúng người, khóa bước hết hạn và nhật ký | Còn phụ thuộc; mock/TOTP không thay thế |

Thiết lập một phép đo cần nêu số người, dữ liệu tổng hợp, chính sách phiên, lịch phát request,
ngưỡng sai số/tỷ lệ lỗi và cách thu latency trước khi chạy. Dùng token/tài khoản test trong môi trường được phép;
không đưa token hay PII vào log/repo. Pool hai kết nối/GOMAXPROCS=2 của bộ kiểm chức năng không phải cấu hình để công bố
sức chịu tải 500 người. Chưa đủ phép đo thì giữ trạng thái chờ trong ma trận.

Trước READY toàn W11.31: ghép các phụ thuộc thực trên dev, cập nhật đúng ma trận, chạy bộ cổng AUTO-REVIEW
và kiểm native/race/migration/web liên quan trên SHA cuối; Claude review chéo. Không tự gộp mã Codex.

## Checkpoint API tiến độ — 31/10/2026

Ca `TestHCTHAppraisalCountsByTrustedCaller_W1131` dùng bốn người/hai đơn vị,
oracle phân công và trạng thái riêng. Sau khi ghép dev có W11.26, chọn ca mới bằng
`-run '^TestHCTHAppraisalCountsByTrustedCaller_W1131$'` trong lệnh native-race ở trên.
Mỗi lượt dùng một cụm tạm riêng; lưu log và băm nguồn trước/sau. Phép kiểm mutation
dùng Go overlay trong thư mục tạm, giữ nguồn production nguyên byte; phải FAIL đúng
assertion rò số đếm theo caller, rồi chạy lại với bản gốc khớp SHA-256 và thấy GREEN.
Lỗi tên fixture có chữ số ở lượt đầu được giữ log, sửa dữ liệu tổng hợp, không nới validation.

Chỉ chạy lại consumer cũ khi fixture chung/composition đã đổi; không lặp mọi ca PASS cũ
chỉ để đếm. Checkpoint này không đổi fixture chung của ca staff/Contact. Cổng đầy đủ theo
AUTO-REVIEW và phép đo tải triển khai vẫn cần ở SHA cuối khi W11.31 đủ điều kiện READY.
