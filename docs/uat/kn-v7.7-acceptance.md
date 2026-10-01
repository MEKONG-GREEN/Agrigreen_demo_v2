# Nghiệm thu KN Phòng Trồng trọt v7.7: yêu cầu → kiểm thử

**Khung** lập ngày 01/10/2026 ở gói P8 đợt 1 (nhánh `kn-p8`, từ `sub/kn-v77`), trước khi có mã của P1–P7. Agent các gói điền cột kiểm thử và trạng thái khi làm; P8 đợt 4 (E2E, hiệu năng) và người tích hợp rà lại toàn bảng trước khi nghiệm thu (như [g4-acceptance.md](g4-acceptance.md)). Mỗi yêu cầu có đúng một dòng. Tên kiểm thử ghi vào bảng phải tra được trong mã; tên kiểm thử mới mang mã FN v7.7 (vd `TestFN_TI_02_04_…`, kế hoạch §1.6).

Yêu cầu lấy từ:
- [docs/brd/kn-trong-trot-v7.7.md](../brd/kn-trong-trot-v7.7.md): 40 UR (§3), tiêu chí chấp nhận của 25 FN (§4) kèm **AC-CHUNG** (dữ liệu tạo/sửa lưu vào cơ sở dữ liệu, mọi thay đổi có truy vết), NFR (§7);
- [docs/brd/kn-v7.7-gap.md](../brd/kn-v7.7-gap.md): ma trận khoảng cách (§2), tính năng chuyển khỏi module (§3), xung đột kiến trúc và cách xử lý (§4);
- [docs/brd/kn-v7.7-plan.md](../brd/kn-v7.7-plan.md): gói P1–P8, migration pf 000060–000089, câu hỏi khách Q-01…Q-23 (§5).

Bốn trạng thái:
- **đạt**: có kiểm thử tự động chứng minh tiêu chí (kiểm thử Go `Test…`, Vitest, Playwright `*.spec.ts`, Flutter, hoặc báo cáo diễn tập có lưu). Ở khung này chỉ dòng mà ma trận khoảng cách ghi **Đáp ứng** và kiểm thử hiện có đã tra được trong mã. Tham số **tạm, chờ khách** vẫn **đạt** nếu cơ chế (phiên bản, cấu hình) đã chứng minh;
- **một phần**: ghi rõ phần còn thiếu và loại — **mã**, **chờ khách**, **hạ tầng**;
- **chưa làm**: chưa có mã / chưa diễn tập. Cột kiểm thử có thể ghi "nền có sẵn" — kiểm thử hiện có của phần sẽ được tái dùng, **không** chứng minh tiêu chí v7.7;
- **ngoài module**: tính năng v7.7 chuyển sang module/hệ thống khác; mã và kiểm thử giữ nguyên, chỉ ẩn khỏi menu Phòng Trồng trọt.

## Tóm tắt

| Mục | Số yêu cầu | đạt | một phần | chưa làm | ngoài module |
|---|---|---|---|---|---|
| 3. UR chung, Cấp Quốc gia, kỹ thuật | 5 | 0 | 0 | 5 | 0 |
| 4. UR Cấp Tỉnh | 7 | 0 | 0 | 7 | 0 |
| 5. UR Cấp Xã | 21 | 0 | 0 | 21 | 0 |
| 6. UR Admin | 7 | 1 | 0 | 6 | 0 |
| 7. Tiêu chí chấp nhận FN | 25 | 1 | 0 | 24 | 0 |
| 8. Phi chức năng | 14 | 2 | 3 | 9 | 0 |
| 9. Xung đột kiến trúc và cách xử lý | 15 | 0 | 0 | 15 | 0 |
| 10. Tính năng chuyển khỏi module | 13 | 0 | 0 | 0 | 13 |
| 11. Câu hỏi khách | 23 | 0 | 0 | 23 | 0 |
| **Tổng** | **130** | **4** | **3** | **110** | **13** |

Còn lại theo loại: **mã** — mọi dòng **chưa làm** của mục 3–9 (gói P1–P7 theo cột "Gói"); **chờ khách** — Q-01…Q-23 (mục 11), đặc biệt Q-01 (tệp Excel gốc của 14 mẫu) chặn 12/14 mẫu; **hạ tầng** — N-01 (sẵn sàng ≥ 99,5 %), N-02..N-04 (sao lưu, RPO, RTO trên cụm thật), N-05 (kiểm tải 500 người dùng trên staging).

Phát hiện khi đối chiếu dòng "Đáp ứng (xác nhận)" của ma trận khoảng cách với runbook: `repo1-retention-full=4` với bản full hằng tuần ([backup.md](../runbooks/backup.md)) chỉ giữ chắc khoảng 4 tuần (≈ 28 ngày), **chưa chắc ≥ 30 ngày** như BRD §7 → N-02 ghi **một phần**; cần tăng lên ≥ 5 bản full (hoặc giữ theo thời gian) — việc của người tích hợp hạ tầng (`deploy/**`).

## 1. Cách chạy

Như [g4-acceptance.md §1](g4-acceptance.md#1-cách-chạy). Kiểm thử Go (PowerShell, PostgreSQL 17 + PostGIS + pgRouting cục bộ):

```
. .\scripts\dev-env.ps1
go test -count=1 -p 3 -timeout 40m ./...
go test -count=1 -run "TestFN_" -v ./internal/...        # kiểm thử mang mã FN v7.7 (khi các gói đã có)
```

Web `apps/web-ops`: `npm ci`, `npm run check:api`, typecheck, lint, Vitest, Playwright chế độ mock (`timeZone: Asia/Ho_Chi_Minh`; spec KN mới đặt tên `tests/e2e/kn-*.spec.ts`). Flutter: `flutter test` + `flutter analyze --fatal-infos` ở `packages/agrigreen_mobile`, `apps/field-app`.

Kịch bản đầu–cuối **dự kiến** (P8 đợt 4, chưa có mã): `go test -count=1 -timeout 40m -run TestKN77AnGiangTuan36 -v ./tests/e2e/` — Admin khởi tạo → Tỉnh phát hành T01/T03/T04/T05/T06 → hai Cán bộ xã nộp (một ngoại tuyến) → nhắc → yêu cầu điều chỉnh → gửi lại → "Cập nhật" tổng hợp (ô thay đổi, nhật ký) → chốt tự động → mở lại → gửi Chi cục (SMTP giả, Google tắt) → Cấp Quốc gia xem. Kiểm tải dashboard 500 người dùng: `tools/loadgen` ([load-drill.md](../runbooks/load-drill.md)) mở rộng kịch bản KN. Diễn tập sao lưu, phục hồi: [restore-drill.md](../runbooks/restore-drill.md).

## 2. Kịch bản đầu–cuối

Chưa có (P8 đợt 4). Khi có, bảng bước `TestKN77AnGiangTuan36` đặt ở đây theo mẫu [g4-acceptance.md §2](g4-acceptance.md#2-kịch-bản-đầucuối) và cột "Dòng" trỏ về mã UR/FN/N ở các mục dưới.

## 3. UR chung, Cấp Quốc gia, kỹ thuật

| Mã | Yêu cầu | FN | Gói | Kiểm thử | Trạng thái |
|---|---|---|---|---|---|
| UR-CH-01 | Đăng nhập bằng email hoặc SĐT; tài khoản nhiều vai trò/phòng ban thì chọn vai trò và phòng ban (Admin không chọn phòng ban) | FN-CH-00.01 | P1, P6, P7 | — (nền có sẵn: `TestVerifyBuildsPrincipal`, `TestVerifyRejects`, Flutter `auth_test.dart`) | **chưa làm** |
| UR-TW-01 | Cấp Quốc gia xem dashboard trồng trọt của Tỉnh và Xã | FN-TW-01.01 | P4, P6 | — (nền có sẵn: `TestUnifiedDashboardG02AndTaskNamesG09`, `TestNationalKPIEqualsSumOfProvinces` — nguồn `field_report`, sẽ thay bằng snapshot tổng hợp) | **chưa làm** |
| UR-TW-02 | Cấp Quốc gia xem báo cáo tuần/tháng/vụ/năm của Tỉnh và Xã, chỉ xem | FN-TW-01.02 | P3, P4, P6 | — | **chưa làm** |
| UR-KT-01 | Mobile chạy trên Android giá rẻ, 3G; nhập liệu và vẽ thửa được khi offline | FN-XA-03.02, 03.05 | P7 | — (phần vẽ thửa ngoại tuyến đã đạt ở N-08; nhập biểu ngoại tuyến N-07 chưa có) | **chưa làm** |
| UR-KT-02 | Mở thêm Phòng ban (module) bằng cấu hình danh mục và mẫu báo cáo, không phát triển lại đăng nhập và phân quyền | FN-CH-00.01, FN-AD-04.03, 04.06 | P2, P6 | — | **chưa làm** |

## 4. UR Cấp Tỉnh

| Mã | Yêu cầu | FN | Gói | Kiểm thử | Trạng thái |
|---|---|---|---|---|---|
| UR-TI-01 | Cấp Tỉnh xem dashboard toàn tỉnh và tình hình nộp | FN-TI-02.01 | P4, P6 | — | **chưa làm** |
| UR-TI-02 | Cấp Tỉnh phát hành biểu cho các xã, khai báo kỳ, hạn nộp, giá trị cột Tỉnh giao | FN-TI-02.02 | P4 | — | **chưa làm** |
| UR-TI-03 | Cấp Tỉnh biết xã chưa nộp, trễ hạn và nhắc trên hệ thống | FN-TI-02.03 | P4 | — (nền có sẵn: hộp thư + đẩy FCM chỉ dữ liệu của `notify`) | **chưa làm** |
| UR-TI-04 | Cấp Tỉnh rà soát, yêu cầu điều chỉnh kèm lý do, chốt kỳ và mở lại kỳ | FN-TI-02.04 | P4 | — (nền có sẵn: `TestClosedPeriodNeedsCorrectionReason` — kỳ `YYYY-MM` cũ, không mở lại) | **chưa làm** |
| UR-TI-05 | Cấp Tỉnh xem tổng hợp các xã đúng mẫu, bấm "Cập nhật", xem nhật ký thay đổi | FN-TI-02.05 | P4 | — | **chưa làm** |
| UR-TI-06 | Cấp Tỉnh xuất file và gửi email cho Chi cục từ hệ thống (file hoặc link Google Sheet) | FN-TI-02.06 | P4 | — (nền có sẵn: `TestXLSXContent`, `TestPDFVietnamese`, `TestSMTPDeliversOverSTARTTLS`, `TestComposeUTF8NoAttachment` — hiện **không** đính kèm) | **chưa làm** |
| UR-TI-07 | Cấp Tỉnh xem, duyệt lịch làm việc hằng tuần Cán bộ xã và đăng ký lịch của mình | FN-TI-02.07 | P5 | — | **chưa làm** |

## 5. UR Cấp Xã

| Mã | Yêu cầu | FN | Gói | Kiểm thử | Trạng thái |
|---|---|---|---|---|---|
| UR-XA-01 | Cán bộ xã biết phải nộp biểu nào, cho xã nào, kỳ nào, hạn khi nào | FN-XA-03.01 | P4, P6, P7 | — | **chưa làm** |
| UR-XA-02 | Nhập T01 – Tiến độ xuống giống lúa (PL 01) hằng tuần | FN-XA-03.02 | P3, P4, P6, P7 | — | **chưa làm** |
| UR-XA-03 | Nhập T02 – Xuống giống và phân trà lúa mùa (PL 01B) hằng tuần (xã có lúa mùa) | FN-XA-03.02 | P3, P4, P6, P7 | — | **chưa làm** (cấu trúc mẫu chờ khách Q-01) |
| UR-XA-04 | Nhập T03 – Phân trà lúa (PL 02) hằng tuần | FN-XA-03.02 | P3, P4, P6, P7 | — | **chưa làm** (chờ khách Q-01) |
| UR-XA-05 | Nhập T04 – Xuống giống và thu hoạch rau màu (PL 03) hằng tuần | FN-XA-03.02 | P3, P4, P6, P7 | — | **chưa làm** (danh sách 54 cây chờ khách Q-01) |
| UR-XA-06 | Nhập T05 – Sâu bệnh hại lúa (PL 04) hằng tuần | FN-XA-03.02 | P3, P4, P6, P7 | — | **chưa làm** (chờ khách Q-01) |
| UR-XA-07 | Nhập T06 – Sâu bệnh hại cây trồng khác (PL 05) hằng tuần | FN-XA-03.02 | P3, P4, P6, P7 | — | **chưa làm** (chờ khách Q-01) |
| UR-XA-08 | Nhập T07 – Tiến độ thu hoạch lúa (PL 07) hằng tuần trong giai đoạn thu hoạch | FN-XA-03.02 | P3, P4, P6, P7 | — | **chưa làm** (chờ khách Q-01) |
| UR-XA-09 | Nhập M01 – Cây ăn trái và cây công nghiệp hằng tháng, trước ngày 10 | FN-XA-03.02 | P3, P4, P6, P7 | — | **chưa làm** (chờ khách Q-01, Q-17) |
| UR-XA-10 | Nhập V01 – Áp dụng 3 giảm 3 tăng (PL1 3G3T) hằng vụ | FN-XA-03.02 | P3, P4, P6, P7 | — | **chưa làm** (chờ khách Q-01) |
| UR-XA-11 | Nhập V02 – Áp dụng 1 phải 5 giảm (PL2 1P5G) hằng vụ | FN-XA-03.02 | P3, P4, P6, P7 | — | **chưa làm** (chờ khách Q-01) |
| UR-XA-12 | Nhập V03 – Trang thiết bị cơ giới hóa (PL3 CGH) hằng vụ | FN-XA-03.02 | P3, P4, P6, P7 | — | **chưa làm** (14 loại máy chờ khách Q-01) |
| UR-XA-13 | Nhập V04 – Cơ cấu giống lúa (PL4 CCG) hằng vụ | FN-XA-03.02 | P3, P4, P6, P7 | — | **chưa làm** (40 giống chờ khách Q-01) |
| UR-XA-14 | Nhập V05 – Lượng giống gieo sạ (PL5) hằng vụ | FN-XA-03.02 | P3, P4, P6, P7 | — | **chưa làm** (chờ khách Q-01) |
| UR-XA-15 | Nhập V06 – Diện tích tham gia mô hình (PL6) hằng vụ | FN-XA-03.02 | P3, P4, P6, P7 | — | **chưa làm** (chờ khách Q-01) |
| UR-XA-16 | Cập nhật danh sách HTX và nông hộ | FN-XA-03.03 | P7, P6 | — (nền có sẵn: HTX/nông hộ `party` + vault, UAT G1 US-HTX-01…05) | **chưa làm** |
| UR-XA-17 | Khai báo máy móc của HTX | FN-XA-03.04 | P7 | — (nền có sẵn: `TestImportMachinesXLSXReplace`, `TestMachineCountAtT`) | **chưa làm** |
| UR-XA-18 | Vẽ ranh giới thửa ruộng trên bản đồ, kể cả offline | FN-XA-03.05 | P7 | — (nền có sẵn: `TestBoundaryValidationRules`, `TestOverlapRules`, `TestGeodesicAreaNearCanTho`, Flutter `boundary_test.dart`; thiếu hình tròn/vuông, tên nông dân, HTX, web vẽ thửa) | **chưa làm** |
| UR-XA-19 | Ghi nhận thông tin vật tư trên địa bàn | FN-XA-03.06 | P2, P7 | — | **chưa làm** |
| UR-XA-20 | Xem lại báo cáo đã gửi và thống kê của xã | FN-XA-03.07 | P4, P6 | — | **chưa làm** |
| UR-XA-21 | Đăng ký lịch làm việc hằng tuần, gửi Cấp Tỉnh duyệt | FN-XA-03.08 | P5, P7 | — | **chưa làm** |

## 6. UR Admin

| Mã | Yêu cầu | FN | Gói | Kiểm thử | Trạng thái |
|---|---|---|---|---|---|
| UR-AD-01 | Admin quản lý danh mục ĐVHC | FN-AD-04.01 | P6 (giao diện) | `TestSuccessorResolutionAfterMerger`, `TestDescendantsAtTime`, `TestDescendantsCoverWholeProvince`, `TestResolveCurrentMergerSplitAndReuse`, `TestUnitValidate`, `TestSeedCountsMatchDoc`, `TestSeedCodesAndValidity`, `TestSeedSuccessorsExistAndAreCurrent`, `TestSyncAdminUnitScopeAndTombstone` (`internal/platform/masterdata`; seed chính thức sau sắp xếp 2025 có An Giang, nạp có hiệu lực theo thời gian, kế thừa mã, không xóa — chỉ hết hiệu lực) | **đạt** (dữ liệu, API. Màn List theo Design Sample, khung Sắp xếp ĐVHC, tọa độ trung tâm — P6) |
| UR-AD-02 | Admin quản lý cơ cấu tổ chức 3 cấp, gán Khu vực cho xã | FN-AD-04.02 | P2 | — (nền có sẵn: `TestOrgMoveHistoryAndChain`, `TestOrgTreeExportImportRoundTrip`, `TestScopeProvinceSeesOnlyItsCommunes`) | **chưa làm** |
| UR-AD-03 | Admin khai báo danh mục phòng ban và trạng thái áp dụng | FN-AD-04.03 | P2 | — | **chưa làm** |
| UR-AD-04 | Admin tạo tài khoản, phân quyền; một tài khoản nhiều vai trò và phòng ban, mỗi vai trò có Nút, phòng ban, xã phụ trách riêng; chỉ thực hiện sau UR-AD-02, 03 | FN-AD-04.04 | P1 | — (nền có sẵn: `TestCreateUserUSADM01`, `TestLockUnlockNeedsReason`, `TestImportXLSXMixedRows`, `TestBulkActions`) | **chưa làm** |
| UR-AD-05 | Admin quản lý danh mục nông nghiệp (giống lúa, vật tư, cây trồng, sâu bệnh, loại máy, mô hình) | FN-AD-04.05 | P2, P3 | — (nền có sẵn: `TestCatalogDeactivateKeepsHistory`, `TestCatalogImportPerRowErrors`) | **chưa làm** |
| UR-AD-06 | Admin chỉnh sửa mẫu báo cáo của các phòng ban cấp tỉnh, cấp xã, tạo mẫu mới, gán vào loại báo cáo của phòng ban | FN-AD-04.06 | P3 | — (nền có sẵn: `TestTemplateVersionsAdmin` — chỉ bố cục xuất) | **chưa làm** |
| UR-AD-07 | Admin vào menu mọi cấp, nhập liệu thay khi hỗ trợ; mọi thao tác ghi nhật ký | FN-AD-04.07 | P1, P6 | — | **chưa làm** |

## 7. Tiêu chí chấp nhận FN

Mỗi dòng gồm tiêu chí của FN **và** AC-CHUNG (lưu cơ sở dữ liệu, truy vết người/thời điểm, cũ → mới).

| Mã | Tiêu chí chấp nhận | Gói | Kiểm thử | Trạng thái |
|---|---|---|---|---|
| FN-CH-00.01 | Đăng nhập vào đúng menu và phạm vi dữ liệu của vai trò + phòng ban đã chọn; đổi vai trò trong phiên hiện đúng menu mới (không đăng nhập lại); Admin vào thẳng Tổng quan hệ thống; phòng ban chuyển "Đang áp dụng" hiện ngay mà không cần cập nhật ứng dụng; AC-CHUNG | P1, P2, P6, P7 | — | **chưa làm** |
| FN-TW-01.01 | Dashboard tải < 5 giây với 500 người dùng đồng thời; chỉ đọc, mỗi thẻ ghi mốc cập nhật, cấp trên = tổng cấp dưới, chỉ tính báo cáo đã gửi; AC-CHUNG | P4, P6, P8 | — | **chưa làm** |
| FN-TW-01.02 | Toàn tỉnh = tổng các xã đã gửi trong kỳ; năng suất dòng tổng = sản lượng / DT thu hoạch; không có nút sửa; AC-CHUNG | P3, P4, P6 | — | **chưa làm** |
| FN-TI-02.01 | Số liệu dashboard tỉnh trùng báo cáo tổng hợp FN-TI-02.05 cùng kỳ; chỉ dữ liệu Phòng Trồng trọt của tỉnh mình; AC-CHUNG | P4, P6 | — | **chưa làm** |
| FN-TI-02.02 | Cán bộ xã nhận công việc và thông báo ngay sau phát hành; Cán bộ xã khác Phòng ban không nhận; AC-CHUNG | P4 | — | **chưa làm** |
| FN-TI-02.03 | Số xã theo trạng thái (Đã nộp / Lưu nháp / Chưa nhập / Trễ hạn) đúng thời gian thực; Cán bộ xã nhận nhắc trong vòng 1 phút; AC-CHUNG | P4 | — | **chưa làm** |
| FN-TI-02.04 | Cán bộ xã nhận yêu cầu điều chỉnh kèm lý do (bắt buộc); số liệu sau chốt không sửa được nếu chưa mở lại kỳ; Cấp Tỉnh không sửa số gốc; AC-CHUNG | P4 | — | **chưa làm** |
| FN-TI-02.05 | Sau "Cập nhật" bảng đúng số liệu mới nhất; mọi ô thay đổi được đánh dấu và có trong nhật ký (không xóa); toàn tỉnh = tổng xã; Excel xuất đúng bố cục mẫu; AC-CHUNG | P3, P4 | — | **chưa làm** |
| FN-TI-02.06 | Số trong file = số tổng hợp của kỳ; Chi cục nhận email kèm file hoặc link mở được; mọi lần gửi có trong log; AC-CHUNG | P4 | — | **chưa làm** (Google Sheet phụ thuộc ADR-0026, Q-14) |
| FN-TI-02.07 | Lịch Cán bộ xã gửi hiện ngay ở "Chờ duyệt"; không xem được lịch Phòng ban/tỉnh khác; AC-CHUNG | P5 | — | **chưa làm** |
| FN-XA-03.01 | Công việc mới hiện ngay sau phát hành; trạng thái chuyển "Đã nộp" ngay sau khi gửi; AC-CHUNG | P4, P6, P7 | — | **chưa làm** |
| FN-XA-03.02 | Bố cục bảng trùng sheet mẫu (đối chiếu theo chữ cái cột); Cán bộ xã không thấy dòng xã khác; số liệu gửi hiện ngay ở tổng hợp Cấp Tỉnh ("Thay đổi chờ cập nhật"); vi phạm yêu cầu mẫu → không cho gửi; gửi sau chốt → từ chối; AC-CHUNG | P3, P4, P6, P7 | — | **chưa làm** |
| FN-XA-03.03 | Không thao tác ngoài phạm vi xã phụ trách; không xóa được HTX đã có dữ liệu liên kết (vô hiệu hóa thay xóa); SĐT che một phần; AC-CHUNG | P7, P6 | — (nền có sẵn: vô hiệu hóa HTX `deactivation-preview`, mặt nạ SĐT ở UAT G1) | **chưa làm** |
| FN-XA-03.04 | Tổng theo loại máy của xã = tổng các HTX; số lượng âm/không nguyên bị chặn; AC-CHUNG | P7 | — | **chưa làm** |
| FN-XA-03.05 | Không lưu được ranh giới không khép kín hoặc chồng lấn; vẽ offline đồng bộ đủ 100 %; AC-CHUNG | P7 | — (nền có sẵn: `TestOverlapRules`, `TestClassifyOverlaps`, Flutter `boundary_test.dart` "chồng lấn thửa của HTX khác → chặn lưu kể cả khi xác nhận"; v7.7 chặn mọi chồng lấn — hiện cùng HTX chỉ cần xác nhận) | **chưa làm** |
| FN-XA-03.06 | Cấp Tỉnh xem được tổng hợp vật tư theo xã; AC-CHUNG | P2, P7 | — | **chưa làm** |
| FN-XA-03.07 | Thống kê của xã khớp báo cáo đã gửi; AC-CHUNG | P4, P6 | — | **chưa làm** |
| FN-XA-03.08 | Cán bộ xã nhận thông báo kết quả duyệt kèm lý do; lịch tuần đã kết thúc bị khóa; AC-CHUNG | P5, P7 | — | **chưa làm** |
| FN-AD-04.01 | Đúng tỉnh An Giang và các xã/phường (sắp xếp 2025); không chọn được ĐVHC ngoài danh mục; mã duy nhất, không xóa, chỉ "Hết hiệu lực"; AC-CHUNG | P6 (giao diện) | `TestSeedCountsMatchDoc`, `TestSeedCodesAndValidity`, `TestDescendantsCoverWholeProvince`, `TestUnitValidate`, `TestResolveCurrentMergerSplitAndReuse`, `TestSuccessorResolutionAfterMerger` (mã lạ → 404 `masterdata.admin_unit_not_found`, tham chiếu sai → 422 `masterdata.admin_unit_invalid_reference`), `TestCatalogDeactivateKeepsHistory` | **đạt** (dữ liệu, API; màn Design Sample ở P6) |
| FN-AD-04.02 | Cây trực quan; di chuyển Nút cập nhật ngay phạm vi tài khoản; báo cáo nhóm đúng theo Khu vực; không di chuyển Nút cấp 1, 2; AC-CHUNG | P2 | — | **chưa làm** |
| FN-AD-04.03 | Chuyển "Đang áp dụng" hiện ngay ở bước 2 đăng nhập; không xóa phòng ban đã có tài khoản/dữ liệu; AC-CHUNG | P2, P1 | — | **chưa làm** |
| FN-AD-04.04 | Tài khoản mới đúng phạm vi; vai trò khác Admin không thấy menu "Người dùng"; hai Cán bộ xã khác Phòng ban trên cùng xã chỉ thấy dữ liệu Phòng ban mình; mọi lần lưu, khóa, gửi lại mật khẩu, thêm/xóa vai trò hiện trong Log (người, thời điểm, cũ → mới); khóa thu hồi ngay mọi phiên; AC-CHUNG | P1 | — | **chưa làm** |
| FN-AD-04.05 | Mục danh mục mới hiện ngay trong biểu và màn nhập; không xóa cứng mục đã dùng; AC-CHUNG | P2, P3 | — | **chưa làm** |
| FN-AD-04.06 | Thêm/bớt cột, dòng hiện đúng trên màn nhập và bảng tổng hợp từ kỳ áp dụng; mẫu mới gán cho phòng ban hiện trong danh sách phát hành của Cấp Tỉnh phòng ban đó; kỳ đã có số giữ phiên bản cũ; AC-CHUNG | P3 | — | **chưa làm** |
| FN-AD-04.07 | Mọi thao tác hỗ trợ có trong nhật ký (thời điểm, Admin, thay cho ai, chức năng, nội dung, lý do; không xóa); yêu cầu MFA; AC-CHUNG | P1, P6 | — | **chưa làm** |

## 8. Phi chức năng

| Mã | Yêu cầu | Gói | Kiểm thử | Trạng thái |
|---|---|---|---|---|
| N-01 | Sẵn sàng ≥ 99,5 %/tháng (trừ bảo trì báo trước) | hạ tầng | — | **chưa làm** (**hạ tầng**: chưa có cụm production, chưa có SLO/giám sát sẵn sàng) |
| N-02 | Sao lưu DB hằng ngày, giữ ≥ 30 ngày | hạ tầng | `TestDrillEndToEndAndDetectsCorruption` (`cmd/drill`), `TestBackupRestoreVerify` (`internal/shared/objsync`); lịch [backup.md](../runbooks/backup.md): full Chủ nhật + diff thứ Hai–thứ Bảy 01:00, `objsync` hằng ngày | **một phần** — **hạ tầng**: `repo1-retention-full=4` (≈ 4 tuần) chưa bảo đảm ≥ 30 ngày, cần ≥ 5 bản full hoặc giữ theo thời gian; chưa chạy trên cụm thật |
| N-03 | RPO ≤ 24 giờ | hạ tầng | `TestDrillEndToEndAndDetectsCorruption`; WAL liên tục (`archive_timeout=60`) + sao lưu hằng ngày ([backup.md](../runbooks/backup.md)); workflow `restore-drill` đạt 29/09/2026 (G1, [g1-acceptance.md](g1-acceptance.md)) | **một phần** — **hạ tầng**: RPO chưa đo trên cụm thật (diễn tập mới trên Compose) |
| N-04 | RTO ≤ 4 giờ | hạ tầng | quy trình [restore-drill.md](../runbooks/restore-drill.md) (bước 5 ghi RTO vào biên bản); `TestDrillEndToEndAndDetectsCorruption` | **một phần** — **hạ tầng**: chưa có biên bản RTO trên môi trường phục hồi sạch có khối lượng thật |
| N-05 | Dashboard (và bản đồ) < 5 giây với 500 người dùng đồng thời | P4, P8 | — (nền có sẵn: `tools/loadgen` `TestAPIScenariosAgainstFakeServer`) | **chưa làm** (**mã**: kịch bản tải dashboard KN đọc snapshot; **hạ tầng**: staging) |
| N-06 | Mobile chạy trên Android giá rẻ, mạng 3G; thao tác thường < 3 giây trên 3G | P7, P8 | — | **chưa làm** |
| N-07 | Nhập báo cáo (FN-XA-03.02) trên Mobile khi mất mạng: lưu tạm, tự đồng bộ, không mất, không trùng | P4, P7 | — (nền có sẵn: outbox UUIDv7 + `idem_key`, `TestPushMixedBatchAndReplay`) | **chưa làm** |
| N-08 | Vẽ thửa (FN-XA-03.05) trên Mobile khi mất mạng: lưu tạm, tự đồng bộ, không mất, không trùng | — | Flutter `boundary_test.dart` ("vẽ lại ranh giới chính thửa đó không tự báo chồng lấn; lưu được khi ngoại tuyến", "ranh giới không hợp lệ không vào hàng đợi"), `TestSyncSinkIdempotentAndConflict` (`pf/geo`), `TestPushMixedBatchAndReplay` (`pf/sync`) | **đạt** (cơ chế ngoại tuyến; thuộc tính thửa v7.7 tính ở UR-XA-18 / FN-XA-03.05) |
| N-09 | Phiên hết hạn sau 4 giờ không thao tác trên Web, 24 giờ trên Mobile; đổi vai trò/phòng ban không đăng nhập lại | P1 | — (realm hiện `ssoSessionIdleTimeout` 3600 s) | **chưa làm** (bản vá realm giao người tích hợp) |
| N-10 | Sai 5 lần → khóa tạm 15 phút, banner ghi số lần thử còn lại; lần đầu bắt buộc đổi mật khẩu | P1 | — (realm hiện `permanentLockout: true`) | **chưa làm** (số lần khóa tạm trước khi khóa hẳn chờ khách Q-21) |
| N-11 | Mobile: PIN 6 số, đăng nhập ngoại tuyến với phiên đã có tối đa 72 giờ | P7 (giữ) | Flutter `packages/agrigreen_mobile/test/auth_test.dart` ("chỉ nhận đúng 6 chữ số; lưu băm, không lưu PIN rõ", "sai 5 lần liên tiếp → khóa 15 phút…", "hợp lệ đến đúng 72 giờ, hết hạn sau đó", "lùi đồng hồ để kéo dài phiên bị phát hiện") | **đạt** (P7 thêm danh sách ngữ cảnh vai trò vào phiên ngoại tuyến — tính ở FN-CH-00.01) |
| N-12 | Truy vết (AC-CHUNG): mọi bản ghi có người tạo, người sửa, thời điểm tạo, sửa, phiên bản; thay đổi quan trọng có log cũ → mới; số đã chốt không xóa, chỉ điều chỉnh có lý do; thao tác Admin ghi riêng; API Log chuẩn `GET …/{id}/log` cho khung Log mọi màn Detail | P1–P7 | — (nền có sẵn: sổ chỉ ghi thêm RB-1, `TestCatalogGates`) | **chưa làm** (**mã**: API Log chuẩn, sổ của dữ liệu KN mới) |
| N-13 | ATTT cấp độ 3: MFA cho Admin; nhật ký truy cập ≥ 1 năm | P1, hạ tầng | — (nền có sẵn: `TestAdminRequiresMFA`) | **chưa làm** (chính sách lưu nhật ký truy cập ≥ 1 năm, kho sự kiện Keycloak; MFA cho "Menu các cấp" — Q-22) |
| N-14 | Tương thích Excel: nhập/xuất đúng file mẫu hiện hành; công thức tính trên hệ thống, kết quả trùng Excel; xuất Word | P3, P4 | — | **chưa làm** (ca thử vàng chờ tệp gốc Q-01; Word chờ Q-10) |

## 9. Xung đột kiến trúc và cách xử lý

Theo [kn-v7.7-gap.md §4](../brd/kn-v7.7-gap.md#4-xung-đột-với-ràng-buộc-kiến-trúc-và-đề-xuất-xử-lý). Dòng **đạt** khi cách xử lý đã có mã và kiểm thử chứng minh.

| Mã | Xung đột | Cách xử lý | Gói | Kiểm thử | Trạng thái |
|---|---|---|---|---|---|
| X-01 | RB-1 (chỉ ghi thêm) ↔ "sửa trước chốt", "mở lại kỳ", "gửi lại", "khôi phục phiên bản", "thu hồi đợt", "sửa hạn nộp" | Phiên bản bài nộp bất biến (`submission_version`, `supersedes_id`); `period_event` closed/reopened/auto_closed/reclosed; tổng hợp là sổ (`consolidation_run` + `consolidation_cell_change`); phiên bản mẫu đã phát hành bất biến, "khôi phục" = nháp mới; `batch_withdrawn`, `deadline_changed` là sự kiện; "người sửa/thời điểm sửa" lấy từ sự kiện cuối | P3, P4 | — | **chưa làm** |
| X-02 | RB-3 (PII ở vault): người đại diện HTX, chủ hộ, tên nông dân, email Chi cục, thành phần phối hợp của lịch | `*_ref` vault, SĐT che ở API; danh bạ Chi cục là chủ thể vault hoặc hộp thư đơn vị (Q-09); Log tra `actor_ref` lúc đọc; tệp/Google Sheet gửi Chi cục không có PII; cảnh báo nhập PII ở ô tự do | P4, P5, P7 | — | **chưa làm** |
| X-03 | RB-7 (lưu trong lãnh thổ) ↔ Google Sheet + tài khoản Google + email gửi đi | ADR-0026 "Xuất bản báo cáo tổng hợp sang Google Sheets" (chỉ số tổng hợp không định danh, bộ chuyển `reports/gsheet` tắt mặc định, allowlist ADR-0007, van chặn trường lạ); thông tin xác thực Google trong KMS trong lãnh thổ; dự phòng đính kèm + liên kết tải có hạn từ kho trong lãnh thổ; SMTP relay công vụ (Q-14); chia sẻ theo email người nhận cần khách chấp thuận | P4 | — | **chưa làm** |
| X-04 | Mật khẩu tạm gửi đi ↔ `accounts` chỉ gửi liên kết kích hoạt Keycloak | Giữ liên kết kích hoạt, đổi nhãn "Gửi lại kích hoạt / đặt lại mật khẩu"; cán bộ không có email → SMS trong nước hoặc Admin đặt mật khẩu tạm hiển thị một lần (Q-03) | P1 | — | **chưa làm** |
| X-05 | Nhiều vai trò × Nút × phòng ban trong token Keycloak (một `org_ref`) | Phương án A: Keycloak chỉ xác thực; `role_assignment` ở `pf_accounts`; `GET /v1/me/contexts`, `POST /v1/me/context`, header `X-AG-Context`, `Principal`/`Scope` mới; đổi ngữ cảnh không cần token mới; giữ đường `org_ref` cho module khác | P1 | — | **chưa làm** |
| X-06 | Khóa tạm 15 phút ↔ `permanentLockout: true` | `permanentLockout:false`, chờ 900 s, `maxTemporaryLockouts` > 0 rồi khóa hẳn (Q-21); chủ sản phẩm xác nhận | P1 (bản vá realm) | — | **chưa làm** |
| X-07 | Phiên Web 4 giờ không thao tác ↔ hiện 1 giờ | Client `agrigreen-web-ops` idle 14400 s; client mobile tối đa 86400 s; Admin (MFA) có thể ngắn hơn | P1 (bản vá realm) | — | **chưa làm** |
| X-08 | Trang đăng nhập theo Design Sample ↔ trang Keycloak mặc định | Theme Keycloak `agrigreen` (giữ PKCE, không làm form mật khẩu trong web-ops); "còn n lần thử" nếu đọc được số lần sai, không thì ghi "sau 5 lần sai sẽ bị khóa 15 phút" | P1, P6 | — | **chưa làm** |
| X-09 | Đăng nhập bằng SĐT | `username` = SĐT chuẩn hóa `0xxxxxxxxx`, email qua `loginWithEmailAllowed`; tài khoản không SĐT dùng username sinh | P1 | — | **chưa làm** |
| X-10 | Admin "nhập liệu thay" ↔ maker-checker, "Tỉnh không nhập đè" | Bản nộp mang `on_behalf_of` + lý do; nhật ký hỗ trợ là sổ riêng; chặn Admin tự xác nhận/chốt bản do mình nhập thay | P1, P4 | — | **chưa làm** |
| X-11 | Scope suy từ loại tổ chức của `org_ref` ↔ Cấp Xã gắn Nút tỉnh + danh sách xã phụ trách | `Scope` mới = Nút của context (xuống dưới) ∩ phòng ban; Cấp Xã = danh sách xã; giữ `ScopeResolver` cũ cho module khác | P1 | — | **chưa làm** |
| X-12 | Dữ liệu cũ không có phòng ban | Đọc qua module KN mới mặc định "Phòng Trồng trọt"; không di trú bằng UPDATE | P2, P4 | — | **chưa làm** |
| X-13 | Sửa ĐVHC/di chuyển Nút sau chốt ("số đã chốt giữ cấu trúc lúc chốt") | Lần cập nhật tổng hợp lưu khu vực, mã ĐVHC, tên xã tại thời điểm chạy | P2, P4 | — | **chưa làm** |
| X-14 | Engine công thức Excel trong Go | Tập hàm con cho 14 mẫu (SUM, + − × ÷, IF, ROUND, MIN/MAX, tham chiếu cùng dòng, tổng nhóm); thư viện trong `allowed-licenses.txt`; ca thử vàng so `excelize.CalcCellValue` | P3 | — | **chưa làm** |
| X-15 | Thông báo đẩy qua FCM ngoài lãnh thổ (ADR-0015) | Giữ đẩy chỉ dữ liệu ("có công việc mới"); app kéo nội dung qua API; kiểu thông báo mới ở `pf_notify` | P4, P7 | — | **chưa làm** |

## 10. Tính năng chuyển khỏi module

Theo [kn-v7.7-gap.md §3](../brd/kn-v7.7-gap.md#3-tính-năng-kn-hiện-có-mà-v77-bỏ-khỏi-module-phòng-trồng-trọt). **Không xóa mã**: giữ module, API, kiểm thử (phải xanh); chỉ ẩn khỏi menu Phòng Trồng trọt (P6 menu theo cấu hình, P7 bề mặt app). Cột kiểm thử là kiểm thử hiện có phải giữ xanh.

| Mã | Tính năng | Chuyển sang | Kiểm thử giữ xanh | Trạng thái |
|---|---|---|---|---|
| M-01 | Thư viện số, quy trình kỹ thuật, soạn bài, theo dõi phổ biến (`content`) | Module dùng chung / Phòng Thông tin - Truyền thông | `TestPublishNewVersionKeepsOld`, `TestSearchAccentInsensitive`, `TestAuthorCannotSelfApprove`, `TestPersonalLibraryUSLIB05` | **ngoài module** |
| M-02 | Chỉ đạo khẩn + xác nhận đã đọc | Module điều hành chung (Q-11) | `TestDirectiveAckRate`, `TestAckRate` | **ngoài module** |
| M-03 | Nhiệm vụ hỗ trợ SLA 24 giờ, yêu cầu hỗ trợ từ App HTX | Tích hợp ngoài (liên thông App HTX) | `TestTaskSLAFromParamAtCreation`, `TestSupportRequestCreatesTaskWithDutyFallback`, `TestEscalationJobOnlyOverdueAndIdempotent` | **ngoài module** |
| M-04 | Danh bạ trực, trạng thái trực, ca trực | Module dùng chung (hồ sơ cán bộ) | `TestRosterContactOneTap`, `TestRosterDutyStatus`, `TestStaffDirectoryPort` | **ngoài module** |
| M-05 | Bản đồ khuyến nông, GIS nhiều lớp, phát lại (bản đồ vẽ thửa vẫn ở Trồng trọt) | Module dùng chung (Bản đồ GIS vùng trồng & dịch bệnh) | `TestLayersZoomGate_GIS_BR24`, `TestGISLayerLogSnapshotReplay`, `TestAdminBoundariesByParentG05` | **ngoài module** |
| M-06 | MRV (hồ sơ, đối chiếu, phương pháp) | Hệ thống ngoài | `TestG3OnlyApprovedEvidenceCountsKNUSMRV05`, `TestG3ComparisonRecordKNUSMRV02`, `TestReviewerIsNotCapturer` | **ngoài module** |
| M-07 | Báo cáo Đề án 1 triệu ha (đăng ký/triển khai/hoàn thành, duyệt) | Không có trong v7.7; có thể thành "Báo cáo đột xuất" (Q-12) | `TestProgramProgressFR03` | **ngoài module** |
| M-08 | Báo cáo hiện trường tự do (gieo sạ, sinh trưởng – sâu bệnh) — thay bằng T01/T05… theo mẫu | Giữ bảng, API (dữ liệu G1); không di trú | `TestReportDataSowingAndSummary`, `TestSyncSinkDuplicatesAndClosedPeriod`, `TestClosedPeriodNeedsCorrectionReason`, E2E `TestG1DongXuanCanTho` | **ngoài module** |
| M-09 | Dashboard 4 KPI hợp nhất (`GET /v1/dashboard`) — thay bằng Dashboard v7.7 | Bề mặt khác | `TestUnifiedDashboardG02AndTaskNamesG09`, `TestKNDash01MetricsAsOfExcludesLateRecords`, `TestNationalKPIEqualsSumOfProvinces` | **ngoài module** |
| M-10 | Xuất báo cáo "theo mẫu Bộ", lịch gửi báo cáo email (bộ render xlsx/pdf tái dùng cho P3/P4) | Màn cũ ẩn; lịch gửi HTX giữ cho App HTX | `TestExportXLSXAsyncViaInboxOnce`, `TestScheduleFiresOncePerPeriodAndDelivers`, `TestHTXProductionSchedule_HTX_US_DASH_03` | **ngoài module** |
| M-11 | HTX: nhật ký canh tác, phê duyệt nhật ký, mùa vụ, sản lượng, tài khoản nông dân, thu gom rơm MKG (danh sách HTX/nông hộ v7.7 vẫn giữ — FN-XA-03.03) | Tích hợp ngoài (App HTX) | `TestFarmLogReview_HTX_US_LOG_05`, `TestOpenSeasonFarmer_HTX_US_SEASON_01`, `TestHarvestDeclare_HTX_US_YIELD_01_03` | **ngoài module** |
| M-12 | Cơ giới hóa: bản đồ CGH, cân đối, dự báo; mô phỏng Hub; đường thủy; kho/hub (bảng `machine` dùng lại cho FN-XA-03.04) | Không thuộc module | `TestComputeBalanceQT01`, `TestForecastFN09AndCoopCodeG10`, `TestProjectAreaFN09` | **ngoài module** |
| M-13 | Tham số & phê duyệt, quy chuẩn hiển thị bản đồ, xung đột đồng bộ, giám sát đồng bộ | Công cụ vận hành nền tảng; nhóm Quản trị hệ thống mở rộng hoặc bề mặt riêng (Q-13) | `TestParamProposalAndTransitions`, `TestSelfApprovalBlocked`, `TestConflictSLA`, `TestIntegrationMonitor_GIS_US_SYNC_01_02` | **ngoài module** |

Cảnh báo sớm thiên tai & dịch hại, đào tạo, tin tức, sự kiện, hỏi đáp (một phần trong `notify`/`content`) cũng thuộc module khác; không tách dòng riêng vì không có màn KN đang hiển thị.

## 11. Câu hỏi khách

Theo [kn-v7.7-plan.md §5](../brd/kn-v7.7-plan.md#5-câu-hỏi-cho-khách-hàng). Khi chưa có trả lời, các gói làm với giá trị mặc định có phiên bản, cấu hình được, ghi "tạm, chờ khách".

| Mã | Câu hỏi | Chặn | Trạng thái |
|---|---|---|---|
| Q-01 | Tệp Excel gốc của 14 mẫu (và CC01 nếu có) cùng danh mục dùng trong mẫu | UR-XA-02…15, FN-XA-03.02, FN-AD-04.05, 04.06, N-14 | **chưa làm** (chờ khách) |
| Q-02 | Tên, mã, thứ tự 6 phòng ban của TTKN An Giang | UR-AD-03, FN-AD-04.03 | **chưa làm** (chờ khách) |
| Q-03 | Cán bộ không có email: kích hoạt tài khoản thế nào; liên kết đặt mật khẩu thay "mật khẩu tạm"; có cần SMS | UR-AD-04, X-04 | **chưa làm** (chờ khách) |
| Q-04 | Danh sách Khu vực (huyện cũ) và xã thuộc từng Khu vực của An Giang sau sắp xếp 2025 | UR-AD-02, FN-AD-04.02, FN-TI-02.01 | **chưa làm** (chờ khách) |
| Q-05 | Giá trị "Cấp quản lý"; người duyệt lịch = Trưởng phòng — có ủy quyền Phó phòng | UR-TI-07, FN-TI-02.07, FN-AD-04.04 | **chưa làm** (chờ khách) |
| Q-06 | Cấp Quốc gia xem HTX, máy, thửa ở đâu khi menu TW không có nhóm Dữ liệu | FN-XA-03.03…03.05 | **chưa làm** (chờ khách) |
| Q-07 | "Báo cáo năm" của Cấp Quốc gia: cộng dồn kỳ vụ/tháng hay mẫu năm riêng | UR-TW-02, FN-TW-01.02 | **chưa làm** (chờ khách) |
| Q-08 | Báo cáo Chi cục dùng mẫu riêng (CC01) hay gửi đúng T01–T07/M01/V01–V06 | UR-TI-06, FN-TI-02.06, FN-AD-04.06 | **chưa làm** (chờ khách) |
| Q-09 | Email Chi cục và CC là hộp thư đơn vị hay cá nhân (lưu ở vault) | FN-TI-02.06, X-02 | **chưa làm** (chờ khách) |
| Q-10 | Xuất Word áp dụng cho báo cáo nào | N-14 | **chưa làm** (chờ khách) |
| Q-11 | Chỉ đạo khẩn, nhiệm vụ hỗ trợ, danh bạ trực đã triển khai G1: tắt hẳn ở An Giang hay giữ ở module dùng chung | M-02, M-03, M-04 | **chưa làm** (chờ khách) |
| Q-12 | Báo cáo Đề án 1 triệu ha còn cần không, có chuyển thành "Báo cáo đột xuất" | M-07 | **chưa làm** (chờ khách) |
| Q-13 | Công cụ quản trị nền tảng đặt trong nhóm Quản trị hệ thống của Admin | M-13 | **chưa làm** (chờ khách) |
| Q-14 | "Email công vụ TTKN An Giang" là SMTP công vụ trong nước hay Google Workspace; văn bản chấp thuận dùng Google Drive/Sheets; chia sẻ link theo email người nhận hay "ai có liên kết" | FN-TI-02.06, X-03 | **chưa làm** (chờ khách) |
| Q-15 | Cấu hình tự nhắc: trước hạn bao lâu, nhắc lại khi trễ bao lâu một lần, ai nhận bản sao | FN-TI-02.03 | **chưa làm** (chờ khách) |
| Q-16 | Lịch của cán bộ Cấp Tỉnh do ai duyệt; hạn đăng ký và hạn duyệt mặc định | UR-TI-07, FN-TI-02.07 | **chưa làm** (chờ khách) |
| Q-17 | Quy tắc chia kỳ: tuần ISO hay tuần thứ n của vụ; tuần bắt đầu thứ Hai; tháng nộp trước ngày 10 tháng sau | FN-TI-02.02, UR-XA-09, FN-AD-04.06 | **chưa làm** (chờ khách) |
| Q-18 | Danh mục vụ (Đông Xuân, Hè Thu, Thu Đông, lúa mùa) và khoảng ngày mặc định ở An Giang | FN-TI-02.02, FN-AD-04.05 | **chưa làm** (chờ khách) |
| Q-19 | Bộ nhận diện TTKNQG: tên sản phẩm, logo, ảnh nền đăng nhập, dòng footer | FN-CH-00.01, X-08 | **chưa làm** (chờ khách) |
| Q-20 | Có cần nạp số liệu lịch sử từ Google Sheet để so sánh cùng kỳ năm trước | FN-TW-01.02 | **chưa làm** (chờ khách) |
| Q-21 | Khóa tạm 15 phút: sau bao nhiêu lần khóa tạm thì khóa hẳn (ATTT cấp 3) | N-10, X-06 | **chưa làm** (chờ khách) |
| Q-22 | MFA cho Admin: TOTP có đủ; Admin Mekong Green có cần tài khoản riêng cho từng người | N-13, FN-AD-04.07 | **chưa làm** (chờ khách) |
| Q-23 | Bản đồ nền vệ tinh cho vẽ thửa offline: nguồn tile trong lãnh thổ hay qua proxy ADR-0007 | UR-XA-18, FN-XA-03.05 | **chưa làm** (chờ khách) |

## 12. Giới hạn đã biết

- Khung lập trước khi có mã P1–P7: mọi dòng **chưa làm** ở mục 3–9 sẽ được các gói điền kiểm thử; cột "nền có sẵn" chỉ để định hướng tái dùng, không tính là đạt.
- Chưa có tệp Excel gốc của 14 mẫu (Q-01): tiêu chí "bố cục trùng sheet mẫu theo chữ cái cột" và "kết quả công thức trùng Excel" chỉ kiểm được với T01, T04 (cấu trúc đã biết) cho tới khi có tệp.
- Máy dev không có Docker, Kubernetes, Keycloak thật: các dòng realm (N-09, N-10, X-06..X-09) kiểm bằng `realm_test.go` + emulator; sao lưu, sẵn sàng, tải (N-01..N-05) chờ hạ tầng.
