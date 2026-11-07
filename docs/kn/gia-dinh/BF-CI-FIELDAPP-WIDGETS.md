## BF-CI-FIELDAPP-WIDGETS — I/O và lifecycle màn nhập KN

Gói #286 là sửa kỹ thuật CI/field-app cho Trồng trọt BRD 2.0 v7.7, phần Function FN-XA-03.02 (UR-XA-02–15) và FN-XA-03.03. Giữ nguyên tự lưu khi chuyển tab, hàng đợi gửi báo cáo, nhập hồ sơ KN và các kiểm tra nội dung hiện có.

### Thứ tự và phạm vi

1. Dependency/cấu trúc: không thêm dependency. Sửa `apps/field-app/lib/ui/kn/kn_report_entry_screen.dart`, `apps/field-app/test/kn_widgets_test.dart` và tài liệu gói/nghiệm thu.
2. Data: không đổi schema, migration, dữ liệu hoặc quyền.
3. Service: giữ nguyên `KnReportService`; màn nhập giữ instance của service đã tải công việc trong phạm vi `EntryState`, không cache toàn cục hoặc dùng service của phiên khác để ghi bản đang nhập.
4. API: không đổi route hoặc payload; hồ sơ vẫn gửi `/v1/kn/producer-records`.
5. UI/test: tự lưu lúc đóng dùng service của màn; sau thao tác bất đồng bộ chỉ cập nhật widget còn mounted. Kiểm thử chờ tab mới, bản ghi đã gửi, nút hết bận hoặc form đóng; không suy đoán SQLite/HTTP hoàn tất sau một số vòng cố định.

### Kiểm chứng và giới hạn

- Ba ca dùng `Completer` giữ I/O để đóng màn khi còn bẩn, đang tự lưu và đang tải tab; kiểm cả dữ liệu được lưu/xếp hàng và việc không dùng provider/widget đã tháo.
- Bộ kiểm cũ vẫn kiểm ô bắt buộc, giá trị bản nháp/gửi và request hồ sơ; không bỏ assertion hoặc SKIP. Deadline chờ điều kiện vẫn 5 giây như helper kiểm thử hiện hữu; không tăng timeout toàn cục.
- Phép làm sai: bỏ đúng mounted guard sau `tabsFor` phải khiến ca đóng màn khi tải tab thất bại; khôi phục đúng byte/hash rồi control đạt.
- Không thêm màn, FN, ô số, tham số hay kết nối ngoài. Không thay workflow/scanner/baseline; bản sửa không chứng nhận toàn bộ KN độc lập hoặc UAT nghiệp vụ đã hoàn thành.

### Cách chạy

Từ gốc repo, chọn Go toolchain ở `go.mod` và môi trường theo [dev-setup](../../dev-setup.md). Sau đó:

```powershell
Set-Location apps/field-app
flutter pub get --enforce-lockfile
dart format --output=none --set-exit-if-changed lib test integration_test
flutter analyze --fatal-infos
flutter test
Set-Location ../../packages/agrigreen_mobile
flutter pub get --enforce-lockfile
dart format --output=none --set-exit-if-changed lib test
flutter analyze --fatal-infos
flutter test
Set-Location ../..
.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./tools/docscheck ./tests/kn77
```

Khởi động app bằng `flutter run` trong `apps/field-app` sau khi cấu hình môi trường hiện hữu theo dev-setup; gói không thêm lệnh khởi động hoặc cấu hình riêng.
