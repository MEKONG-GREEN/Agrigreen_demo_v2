## KN-STANDALONE-9-FIELD-APP — Bản build App KN chỉ có màn KN theo BRD 2.0 (#270)

- **Căn cứ:** kế hoạch `kn-handoff/PLAN-KN-STANDALONE-20261107.md` nguyên tắc 2 (mục không gắn được FN thì gỡ cả màn
  mobile) và 3 (KN chạy độc lập); audit `reports/kn-standalone-audit-20261107.md` §2.4 (13 màn KN, 14 màn cũ) và §3 điểm 3, 11.
  FN BRD 2.0 còn trên app: Trồng trọt v7.7 FN-CH-00.01, FN-XA-03.01..03.08; CNTS v4.0 FN-06, FN-08, FN-09, FN-12.
- **Gỡ khỏi `apps/field-app`** (App KN; không đụng `apps/htx-app`, `apps/mkg-field-app`):
  - Trang chủ cũ `HomeScreen` và 10 màn ngoài BRD: `PlotsScreen`, `BoundaryScreen` (thửa/ranh giới HTX), `EvidenceFormScreen`
    (minh chứng MRV), `DirectivesScreen`, `LibraryScreen`, `RosterScreen`, `FieldReportScreen`, `TasksScreen`, `ConsentScreen`,
    `NotificationsScreen` (kèm màn chi tiết `DirectiveDetailScreen`, `DocumentScreen`).
  - Dịch vụ chỉ các màn đó dùng: chỉ đạo, thư viện, danh bạ trực, nhiệm vụ hỗ trợ, minh chứng MRV, đồng ý, báo cáo hiện
    trường; `domain/field_report.dart` (hai hàm `containsPii`, `vietnamTime` mà Lịch công tác dùng chuyển vào
    `domain/work_schedule.dart`). App không còn gọi `/v1/mrv/*`, `/v1/extension/{directives,tasks,roster,field-reports}`,
    `/v1/documents/*`, `/v1/notifications/*/read`.
  - Giữ: 13 màn KN, `LoginScreen`, `WorkScheduleScreen`, `SyncQueueScreen` (hạ tầng đồng bộ; nút "Xác nhận giữ nguyên" ranh
    giới thửa KN chuyển vào đây, bỏ nhánh bản ghi bù của báo cáo hiện trường cũ).
- **Tài khoản không có ngữ cảnh KN** (không có phân công KN, hoặc ngoại tuyến chưa chọn được vai trò): màn
  `NoKnAccessScreen` "Không có quyền", chỉ có nút Đăng xuất — thay cho `HomeScreen` cũ.
- **Đồng bộ chỉ kéo nguồn KN:** gói dùng chung `packages/agrigreen_mobile` thêm tham số tùy chọn
  `SyncService(pullEntities: …)` (theo cách #255 làm với `AuthFlow.pins`): app có khai thì MỌI lượt pull gửi `?entities=`
  đúng danh sách đó — `sync_now` có `entities` thì giao với danh sách, trang `reset` nạp lại theo danh sách chứ không rơi về
  pull tất cả. App KN khai `knPullEntities` = `admin_unit`, `catalog_item`, `notification`, `kn_plot`, `report_task`,
  `form_layout`, `report_submission`, `forms_task`. App HTX và App MKG không khai → hành vi giữ nguyên (pull tất cả).
  App KN cũng thôi tải danh mục báo cáo hiện trường (`rice_variety`, `season`, `pest`) và danh mục trường MRV
  (`GET /v1/mrv/catalog`) sau mỗi lượt pull.
- **Máy chủ không đổi:** bộ lọc `?entities=` đã có ở `pf/sync` (KN US-SYNC-02); máy KN không còn đòi nguồn
  `coop.SyncSources()`, MRV, nông dân, nên không cần đăng ký nguồn theo module bật ở gốc lắp ráp trong gói này. Nguồn và
  đích cũ (`task`, `directive`, `field_report`, `task_update`…) vẫn đăng ký ở máy chủ cho tới gói KN-STANDALONE-3 (#269).
- **Giả định / điều để lại:**
  - Máy đã cài bản cũ còn dòng `ref_entity` của entity không kéo nữa (`plot`, `task`, `directive`, `document`…) và khóa công
    khai hộ: không màn nào đọc; chúng bị xóa ở lượt `reset` kế tiếp (đổi vai trò/phòng ban/xã phụ trách).
  - Chạm thông báo đẩy: trang Cấp Xã chưa từng mở màn theo đường dẫn (chỉ `HomeScreen` cũ làm); gói này không thêm tính năng
    đó. `workScheduleLinkOf` (nhận diện đường dẫn lịch công tác) giữ lại cùng kiểm thử.
  - Lõi dùng chung còn mã chỉ App KN cũ dùng (cổng đồng ý `ConsentGate` của `PhotoPipeline`, `CatalogCache.refresh`,
    `domain/mrv.dart`, loại lệnh `directiveAck`/`librarySave`…): giữ nguyên theo nguyên tắc không xóa mã dùng chung với App HTX.
  - `/v1/kn/producer-htx-candidates` (ứng viên liên kết HTX đã công bố) vẫn được màn HTX của app gọi — thuộc issue con phần
    máy của #271, không thuộc gói này.
- **BRD tự mâu thuẫn:** không có.
