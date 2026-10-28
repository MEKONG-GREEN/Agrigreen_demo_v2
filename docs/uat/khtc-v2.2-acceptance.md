# Nghiệm thu KN Phòng Kế hoạch – Tài chính v2.2: yêu cầu → kiểm thử

**Khung** lập ngày 13/10/2026 ở gói KHTCQA (nhánh `khtcqa`, từ `origin/dev`), **trước khi có mã KHTC**. Mã KHTC gồm W11.61–W11.69 và W11.11; nền đã có là W11.03, W11.05, W11.06, W11.10. Định dạng giống [kn-v7.7-acceptance.md](kn-v7.7-acceptance.md):
- Mỗi yêu cầu có đúng một dòng.
- Mã mang tiền tố `KH-` (Kế hoạch – Tài chính), vì các BRD KN dùng trùng mã (Q-049).
- Tên kiểm thử mới mang tiền tố `TestKH_` và mã FN, ví dụ `TestKH_FN_TI_02_03_…`.
- Khi viết mã, agent các gói W11.6x điền cột "Kiểm thử" và cột "Trạng thái". Tên kiểm thử ghi vào bảng phải tra được trong mã. Tên ghi trong ngoặc *(dự kiến)* là tên đề nghị, chưa có trong mã.

Yêu cầu lấy từ BRD KHTC v2.2:
- 27 UR (Mục III);
- tiêu chí chấp nhận của 19 FN (Mục IV);
- quy tắc BR-DL-01…21 (Mục II.3);
- lịch kỳ (Mục II.4) và luồng trạng thái (Mục II.5).

Bảng gói lấy theo đối chiếu BRD KN 10/2026 §3.2: W11.61 Danh mục + Kế hoạch năm, W11.62 Kỳ KHTC, W11.63 Mẫu phiếu + nhập phiếu, W11.64 Bộ đọc nguồn, W11.65 Mục lục mẫu, W11.66 Màn soạn + Mẫu 1–5, W11.67 Nguồn ngoài, W11.68 Báo cáo phát sinh, W11.69 Trang chủ + TW.

## Quyết định 13/10/2026 và phạm vi G1 (15/12/2026)

Nội dung quyết định:
- KHTC phải tổng hợp số liệu của **mọi phòng** vào báo cáo của mình ngay ở **G1**. Bản đối chiếu §5 trước đây xếp KHTC vào Đợt 3.
- Trồng trọt giữ biểu `reportcycle` cũ, tuần Thứ Tư – Thứ Ba.
- CNTS dùng biểu riêng, tuần Thứ Ba – Thứ Hai.
- Báo giá dùng bảng giá ngày đã duyệt.

Quy tắc quy đổi kỳ, lũy kế, cờ "đã chốt", giá Mẫu 1 và HTX "Chưa xác nhận" được viết thành ví dụ có số để PO và KHTC ký: [khtc-week-mapping.md](khtc-week-mapping.md). Bộ dữ liệu đối chứng tương ứng: [`tests/golden/khtc/`](../../tests/golden/khtc/).

**G1 tối thiểu** (đánh dấu ★ ở cột "Phạm vi G1"):

| Gói | Phần làm ở G1 |
|---|---|
| W11.61 | Danh mục KHTC; Kế hoạch năm có phiên bản theo QĐ, thêm/ngừng chương trình |
| W11.62 | Mở kỳ (tuần, tháng, quý, năm, tiến độ CT); sinh phiếu theo phòng; gắn kỳ nguồn theo quy tắc quy đổi; tiến độ; đóng/hủy kỳ |
| W11.63 **rút gọn** | Có ở G1: mẫu phiếu nạp sẵn theo Bảng FN-PN-03.02 cho 4 phòng; 2 loại cột "Từ Kế hoạch năm" và "Lũy kế kỳ trước"; nhập, lưu nháp, gửi (tối đa 3 lần, Q-081); yêu cầu bổ sung, xác nhận; 3 quy tắc kiểm tra; "Điều chỉnh kỳ trước" (BR-DL-12). **Để sau G1:** màn "So sánh lần gửi"; Admin thiết kế mẫu phiếu mới. Cần W11.11 tối thiểu để mục "Báo cáo cho KHTC" hiện trong menu các phòng. |
| W11.64 | "Cập nhật số liệu"; mốc dữ liệu; "Nguồn đã thay đổi"; xem nguồn từng số |
| W11.66 **chỉ Excel** | Màn soạn tối thiểu (ô hành chính, khung lời văn); xuất **Excel** Mẫu 1–5 theo bố cục cố định của Bảng FN-TI-02.05…02.09 (chưa có mục lục cấu hình W11.65); Bản cuối |

**Giá trị đã chốt với PO** (15/10/2026, các dấu ⚑ trong bảng là dấu vết lịch sử):

PO đã chốt M1–M7 trong tài liệu quy đổi: tuần trống đã chốt; gốc tuần trước dùng số đóng; lũy kế nguồn và tổng phát sinh năm KHTC; hai ngày giá đã duyệt không sau ngày chốt; chỉ người phụ trách cập nhật và không phát hành lại; xác nhận HTX cuối ngày chốt; hạn 10:00 Thứ Tư kế tiếp. Mẫu phiếu nạp sẵn là phạm vi bàn giao G1; Admin thiết kế mẫu để sau G1. Bảng được cập nhật theo tên kiểm thử có thật; W11.66 đã đủ bằng chứng, chuẩn bị tích hợp dev; các hàng đăng nhập/nhắc hạn và việc tách KN vẫn chưa hoàn tất toàn G1.

| # | Mặc định |
|---|---|
| ⚑1 | G1 xuất **Excel** cho Mẫu 1–5. **Word** để G1.1 (Q-062). |
| ⚑2 | Báo cáo đã gửi trước khi xã nhận HTX, hoặc trước khi nguồn đổi, **không phát hành lại**. |
| ⚑3 | Chỉ **cán bộ KHTC phụ trách** kỳ được chạy "Cập nhật số liệu". Lịch sử thay đổi (giá trị cũ → mới, người, thời điểm) được giữ. |

Cột **Phạm vi G1**:
- **trong G1**: làm và nghiệm thu ở G1;
- **sau G1**: làm ở G1.1 hoặc Đợt 3;
- **ngoài**: KHTC không làm (thuộc hệ thống hoặc module khác).

Bốn trạng thái (như kn-v7.7):
- **đạt**;
- **một phần**: ghi rõ phần thiếu và loại (**mã**, **chờ khách**, **hạ tầng**);
- **chưa làm**: cột kiểm thử có thể ghi "nền có sẵn", tức là kiểm thử hiện có của nền tái dùng được nhưng **không** chứng minh tiêu chí KHTC;
- **ngoài module**.

## Tóm tắt

| Mục | Số dòng | trong G1 (★ tối thiểu) | sau G1 | ngoài | đạt | một phần | chưa làm |
|---|---|---|---|---|---|---|---|
| 3. UR | 27 | 20 (19) | 7 | 0 | 18 | 1 | 8 |
| 4. FN | 19 | 13 (12) | 6 | 0 | 12 | 1 | 6 |
| 5. Quyết định 13/10/2026 | 9 | 9 (9) | 0 | 0 | 9 | 0 | 0 |
| **Tổng** | **55** | **42 (40)** | **13** | **0** | **39** | **2** | **14** |

Dòng **đạt** chỉ xác nhận phạm vi G1 tối thiểu đã ghi cho từng hàng; Word, richtext và thiết kế mẫu vẫn ở sau G1. Hai dòng **một phần** còn lại là trang mặc định KHTC; dòng nhắc hạn KHTC chưa được khép bằng bằng chứng. Các nền được tái dùng:
- lịch kỳ W11.03;
- phiếu theo phòng ban `kn_khtc` (tối đa 3 lần gửi, hủy kỳ) của W11.05;
- menu phòng ban v3 của W11.06;
- view nguồn đã chốt W11.10;
- quy đổi tuần KHTCQA.

## 1. Cách chạy

Như [kn-v7.7-acceptance.md §1](kn-v7.7-acceptance.md#1-cách-chạy). Hai lệnh kiểm thử thuần chạy ngay được, không cần cơ sở dữ liệu:

```
go test -count=1 ./tests/golden/khtc/                                              # bộ dữ liệu đối chứng nhất quán
go test -count=1 -run "KHTCQA|PeriodRule" ./internal/kn/extension/reportcycle/domain/   # quy đổi tuần bằng lịch kỳ W11.03
```

Kiểm thử tích hợp KHTC (khi có) đặt tên `TestKH_…` và đọc bộ dữ liệu bằng `khtcgolden.Load()` (gói `tests/golden/khtc`, dữ liệu nhúng). Mỗi ca G01…G06 dựng dữ liệu nguồn, chạy "Cập nhật số liệu", rồi so số KHTC với `Expected` của từng bước.

## 2. Bộ dữ liệu đối chứng

| Ca | Nội dung | Dùng cho dòng |
|---|---|---|
| G01 | Tuần CNTS vắt qua hai tuần KHTC (cả tuần vào tuần chứa Thứ Hai) | KH-QD-01, KH-FN-TI-02.03, KH-FN-TI-02.05 |
| G02 | Tuần KHTC không có kỳ nguồn (Trồng trọt) | KH-QD-03, KH-FN-TI-02.05 |
| G02B | Giá Mẫu 1 A.III khi có ngày chưa duyệt; số phiếu giá tuần | KH-QD-05, KH-FN-TI-02.05 |
| G03 | Sửa lùi lũy kế khi kỳ KHTC còn mở; lũy kế giảm; lịch sử "Cập nhật số liệu" | KH-QD-02, KH-QD-08, KH-FN-TI-02.05 |
| G04 | Nguồn đổi sau khi kỳ KHTC đã đóng | KH-QD-04, KH-QD-07, KH-UR-TI-13 |
| G05, G05B, G05C | Cuối năm; tuần ISO 53 (Trồng trọt v1); năm 53 tuần (KHTC 2030, CNTS 2029) | KH-QD-01, KH-FN-TI-02.08 |
| G06 | HTX "Chưa xác nhận" trước và sau khi xã nhận | KH-QD-06, KH-QD-07 |

## 3. UR

| Mã | Yêu cầu | FN | Gói | Phạm vi G1 | Tiêu chí chấp nhận | Kiểm thử | Trạng thái |
|---|---|---|---|---|---|---|---|
| KH-UR-CH-01 | Đăng nhập tài khoản chung; nhiều vai trò, phòng ban thì chọn | KH-FN-CH-00.01 | P1 (có), W11.06 | trong G1 | Như KH-FN-CH-00.01 | nền có sẵn: `TestFN_CH_00_01_MultiRoleContextsAndSwitch`, `TestW1106_MenuV3DepartmentTemplates`; *(dự kiến)* `TestKH_FN_CH_00_01_DefaultRouteKHTC` | **một phần** (**mã**: trang mặc định của Cán bộ KHTC) |
| KH-UR-TW-01 | Cấp TW xem, tải báo cáo đã xuất của TTKN tỉnh | KH-FN-TW-01.01 | W11.69 | sau G1 | Như KH-FN-TW-01.01 | *(dự kiến)* `TestKH_FN_TW_01_01_ExportedReportsReadOnly` | **chưa làm** (**chờ khách** Q-055, Q-027) |
| KH-UR-TI-01 | Trang chủ cho KHTC, lãnh đạo, BGĐ: báo cáo quý/năm, tiến độ phiếu và nguồn, tiến độ CT, cảnh báo | KH-FN-TI-02.01 | W11.69 | sau G1 | Như KH-FN-TI-02.01 | *(dự kiến)* `TestKH_FN_TI_02_01_DashboardMatchesProgressReport` | **chưa làm** |
| KH-UR-TI-02 | Lập, điều chỉnh kế hoạch năm các CT và nội dung theo dõi khác; gán phòng phụ trách; lưu phiên bản | KH-FN-TI-02.02 | W11.61 | ★ trong G1 | Như KH-FN-TI-02.02 | `TestFN_TI_02_02_AnnualPlan`, `TestKH_ReportClosedSnapshotKeepsPlanVersion` | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới) |
| KH-UR-TI-03 | Mở kỳ bằng ô chọn Năm / Tuần / Tháng / Quý; hệ thống tự điền khoảng thời gian, tên kỳ; giao phiếu; gắn kỳ nguồn module | KH-FN-TI-02.03 | W11.62, W11.03, W11.05 | ★ trong G1 | Như KH-FN-TI-02.03; kỳ nguồn gắn theo quy tắc quy đổi (G01, G05) | `TestKHTCPeriod_W1162`, `TestPlanOf_WeekDeadlines`, `TestKH_FN_TI_02_03_AttachSourcePeriodsGolden` | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới) |
| KH-UR-TI-04 | Theo dõi tiến độ phiếu, nguồn module; nhắc phòng chậm | KH-FN-TI-02.03 | W11.62, W11.10 | ★ trong G1 | Trạng thái nguồn = cờ chốt của view `_pub` (Q6); nhắc trước hạn 1 ngày và khi trễ | nền có sẵn: `TestW11_10_ClosedSourcePubView`; *(dự kiến)* `TestKH_FN_TI_02_03_ProgressAndRemind` | **chưa làm** |
| KH-UR-TI-05 | Rà soát phiếu, xem kết quả kiểm tra, yêu cầu bổ sung, xác nhận | KH-FN-TI-02.04 | W11.63 rút gọn | ★ trong G1 (không có "So sánh lần gửi") | Như KH-FN-TI-02.04 | nền có sẵn: `TestFormsDepartmentLimitCancel_W1105`, `TestFormsClosedStatuses_W1105`; `TestKH_FN_TI_02_04_ReviewRulesAndRequest`, `TestKH_FN_TI_02_04_Rules` (W11.63) | **đạt** (G1 rút gọn; "So sánh lần gửi" sau G1) |
| KH-UR-TI-06 | Soạn báo cáo tuần theo mục lục Mẫu 1: ô hành chính sửa được, lời văn; tên CT, hạng mục, số, lũy kế khóa; sản xuất và giá cả lấy từ module, số CT lấy từ phiếu | KH-FN-TI-02.05 | W11.64, W11.66 | ★ trong G1 (chỉ Excel ⚑1) | Như KH-FN-TI-02.05 | `TestKH_FN_TI_02_05_AllFiveWorkbooks`, `TestKH_FN_TI_02_05_ReportExcelFrozenAndFinalCancel`, `TestKH_ReportAdministrativeOverridesAndLockedNumbers` | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới) |
| KH-UR-TI-07 | Soạn báo cáo tháng theo Mẫu 2 | KH-FN-TI-02.06 | W11.66 | ★ trong G1 (chỉ Excel ⚑1) | Như KH-FN-TI-02.06 | `TestKH_FN_TI_02_06_MonthSumsWeeks`, `TestKH_FN_TI_02_05_AllFiveWorkbooks` | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới) |
| KH-UR-TI-08 | Soạn báo cáo quý, 6 tháng, 9 tháng theo Mẫu 3, kèm phụ lục các CT | KH-FN-TI-02.07 | W11.66 | ★ trong G1 (chỉ Excel ⚑1) | Như KH-FN-TI-02.07 | `TestKH_FN_TI_02_05_AllFiveWorkbooks`, `TestPlanOf_BRDExamples` | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới) |
| KH-UR-TI-09 | Soạn báo cáo năm theo Mẫu 4, kèm phụ lục | KH-FN-TI-02.08 | W11.66 | ★ trong G1 (chỉ Excel ⚑1) | Như KH-FN-TI-02.08 | `TestKH_FN_TI_02_05_AllFiveWorkbooks`, `TestKH_ReportYearWith53WeeksAndThirteenPrograms` | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới) |
| KH-UR-TI-10 | Báo cáo tiến độ các CT theo Mẫu 5; lọc phòng, CT, kỳ, xã; xuất Word, Excel | KH-FN-TI-02.09 | W11.66 | ★ trong G1 (chỉ Excel ⚑1) | Như KH-FN-TI-02.09 | `TestKH_FN_TI_02_09_ProgressFilters`, `TestKH_ReportLevelFilterAndLatestStatus`, `TestKH_ReportProgramCopiesPreviousNarrativeOnly`, `TestKH_FN_TI_02_05_AllFiveWorkbooks`, `TestW10_17_WordAllFive` (Mẫu 5 Word, bộ lọc ghi trong văn bản) | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới). Word Mẫu 5: mã xong ở W10.17 |
| KH-UR-TI-11 | Báo cáo phát sinh từ dữ liệu sẵn có, yêu cầu số liệu bổ sung | KH-FN-TI-02.10 | W11.68 | sau G1 | Như KH-FN-TI-02.10 | *(dự kiến)* `TestKH_FN_TI_02_10_AdhocWithSupplementRequest` | **chưa làm** |
| KH-UR-TI-12 | Xem nguồn của từng số lấy từ module (module, kỳ nguồn, trạng thái chốt, cách tính) | KH-FN-TI-02.05…02.09 | W11.64 | ★ trong G1 | "Nguồn dữ liệu" và trang "Nguồn" trong tệp Excel liệt kê, cho mỗi số: module, **các kỳ nguồn quy về tuần** (khóa kỳ, từ – đến), trạng thái chốt, công thức "lũy kế − gốc" kèm gốc (đã đóng / hiện tại / chuỗi mới) như khtc-week-mapping §3 | `TestKH_UR_TI_12_SourceTraceExcel`, `TestKH_UR_TI_13_RefreshOwnerOnlyDiffHistory` | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới) |
| KH-UR-TI-13 | Mọi tệp xuất lưu phiên bản, người xuất, thời điểm, mốc dữ liệu nguồn; đánh dấu bản cuối | KH-FN-TI-02.05…02.10 | W11.64, W11.66 | ★ trong G1 | Xuất tạo vN và mốc (phiên bản từng nguồn, lần gửi từng phiếu). Nguồn đổi sau khi xuất → "Nguồn đã thay đổi"; tuần đã đóng giữ số (G04). ⚑2 không phát hành lại. ⚑3 chỉ cán bộ phụ trách "Cập nhật số liệu", có lịch sử (G03 s3). Bản cuối chỉ khi không còn mục Tạm tính (BR-DL-16). | `TestKH_UR_TI_13_RefreshOwnerOnlyDiffHistory`, `TestKH_FN_TI_02_05_ReportExcelFrozenAndFinalCancel`, `TestKH_ReportFinalAndCancelConcurrent`, `TestKH_ReportProvisionalAndAccess` | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới) |
| KH-UR-TI-14 | Soạn thảo tự do phần chữ, lưu thành mẫu cho kỳ sau; không sửa ô số | KH-FN-TI-02.05…02.10, KH-FN-AD-04.03 | W11.65, W11.66 | sau G1 (G1 chỉ có khung lời văn và ô hành chính) | BR-DL-20; như tiêu chí "sửa câu dẫn, chèn đoạn" của KH-FN-TI-02.05 | *(dự kiến)* `TestKH_UR_TI_14_FreeEditKeepsLockedCells` | **chưa làm** |
| KH-UR-TI-15 | Nhập tay hoặc nạp Excel số liệu nguồn ngoài, ghi nguồn | KH-FN-TI-02.11 | W11.67 | sau G1 (G1: ô Nhập ngoài in "–") | Như KH-FN-TI-02.11 | `TestKH_FN_TI_02_11_ImportWithSource` (PostgreSQL thật qua HTTP), `TestCheckRowsWeek39`, `TestWithExternalHash`; vitest `external.test.ts` | **đạt** (API + màn `/kn/khtc/so-lieu-ngoai`; ô Nhập ngoài trong bản soạn và Excel Mẫu 1–5; [giả định W11.67](../kn/gia-dinh/W11.67.md)) |
| KH-UR-TI-16 | Thêm, ngừng chương trình theo quyết định từng năm; phiếu, mẫu, phụ lục tự cập nhật | KH-FN-TI-02.02 | W11.61, W11.66 | ★ trong G1 | Năm 2027 có 13 CT → phiếu, Mẫu 1–5 và phụ lục Excel có 13 CT mà không sửa mẫu. Ngừng CT X từ 01/7/2027 → tuần sau ngày đó không có X, quý II vẫn có (BR-DL-21). | `TestKH_ReportYearWith53WeeksAndThirteenPrograms`, `TestKH_ReportStoppedParentAndMissingForms` | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới) |
| KH-UR-PN-01 | Phòng chuyên môn thấy phiếu được giao trong module của phòng: loại, kỳ, hạn, lần gửi, yêu cầu bổ sung | KH-FN-PN-03.01 | W11.63 rút gọn, W11.11 tối thiểu | ★ trong G1 | Như KH-FN-PN-03.01, ở cả 4 phòng TT, CNTS, TTTT, HCTH | nền có sẵn: `TestW1106_MenuV3DepartmentTemplates`; `TestKH_FN_PN_03_01_TaskVisibleInEachDepartment` (W11.63; menu các phòng W11.11) | **đạt** (API + màn `/kn/khtc/phieu`) |
| KH-UR-PN-02 | Nhập phiếu theo mẫu KHTC cho hạng mục phòng phụ trách; không nhập lại số đã có trong module | KH-FN-PN-03.02 | W11.63 rút gọn | ★ trong G1 | Như KH-FN-PN-03.02. Hạng mục có nguồn module (BR-DL-10, ví dụ số phiếu giá của Phòng TTTT = G02B "số phiếu") không có ô nhập. | `TestKH_FN_PN_03_02_ModuleSourcedCellsReadOnly`, `TestKH_FN_PN_03_02_RowsFromPlan` (W11.63) | **đạt** (số trong kỳ của hạng mục nguồn module do W11.64 điền) |
| KH-UR-PN-03 | Lưu nháp không giới hạn; gửi thì khóa; chỉ gửi lại khi KHTC yêu cầu bổ sung; kỳ đóng không sửa | KH-FN-PN-03.02 | W11.05 (có), W11.63 | ★ trong G1 | Tối đa 3 lần gửi, lần 3 tự khóa (Q-081, mặc định); kỳ đóng → Đã khóa / Không gửi | `TestFormsDepartmentLimitCancel_W1105`, `TestFormsClosedStatuses_W1105`, `TestClosedStatuses_W1105`; `TestKH_FN_PN_03_02_ResubmitOnlyOnRequest` (W11.63) | **đạt** (**chờ khách** Q-081: 3 lần, tham số) |
| KH-UR-PN-04 | Phòng xem tiến độ CT của phòng và báo cáo KHTC đã xuất | KH-FN-PN-03.03, KH-FN-TW-01.01 | W11.69, W11.11 | sau G1 | Như KH-FN-PN-03.03 | *(dự kiến)* `TestKH_FN_PN_03_03_DepartmentProgressReadOnly` | **chưa làm** |
| KH-UR-AD-01 | Admin quản lý Danh mục KHTC (người ký, nơi nhận, căn cứ mặc định từng mẫu; mặt hàng giá cả) | KH-FN-AD-04.01 | W11.61 | ★ trong G1 | Như KH-FN-AD-04.01. Mặt hàng giá đọc từ `cap/price` (`price/port.Items`), không có danh mục thứ hai (Q-082). | `TestFN_AD_04_01_Catalog`, `TestKH_FN_TI_02_05_ReportExcelFrozenAndFinalCancel` | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới) |
| KH-UR-AD-02 | Admin tạo, sửa mẫu phiếu KHTC trong Quản lý mẫu dùng chung, gán phòng nhập | KH-FN-AD-04.02 | W11.63 rút gọn | ★ trong G1: mẫu nạp sẵn; thiết kế mẫu sau G1 | Như KH-FN-AD-04.02 | `TestKH_FN_AD_04_02_SeededFormsPerDepartment` | **đạt** (G1 mẫu nạp sẵn; PO chốt Admin thiết kế mẫu sau G1) |
| KH-UR-AD-03 | Cấu hình mục lục 5 mẫu (loại xử lý, nguồn, khuôn câu), nhân bản, phát hành phiên bản | KH-FN-AD-04.03 | W11.65 | sau G1 (G1 dùng bố cục cố định) | Như KH-FN-AD-04.03 | *(dự kiến)* `TestKH_FN_AD_04_03_TemplateOutlinePublish` | **chưa làm** |
| KH-UR-KT-01 | Xuất Word đúng thể thức, mở và sửa được; xuất Excel số liệu, phụ lục | KH-FN-TI-02.05…02.10 | W11.66, W10.17 | ★ trong G1 phần **Excel**; Word ở G1.1 ⚑1 — W10.17 đã có mã Word | G1: tệp .xlsx Mẫu 1–5 mở được bằng Excel; bảng số liệu và phụ lục đúng cột của BRD; ô số là số (không phải chữ). **W10.17:** .docx Mẫu 1–5 dựng từ đúng nội dung đóng băng của một bản Excel: quốc hiệu, số `{n}/BC-TTKN` (tự cấp khi trống), "An Giang, ngày dd tháng mm năm yyyy" (tự điền khi trống), các mục, bảng số kiểu Việt Nam, nơi nhận/chữ ký, phụ lục Mẫu 3–5; cùng bản → cùng byte; lưu tệp + SHA-256. Thể thức dựng sẵn, chưa dùng mẫu .docx gốc của Sở (Q-062). | `TestKH_FN_TI_02_05_AllFiveWorkbooks`, `TestKH_FN_TI_02_05_ReportExcelFrozenAndFinalCancel`, `TestW10_17_WordAllFive`, `TestW10_17_WordGolden`, `TestKH_W10_17_WordFromFrozenExcel` | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới). Word: **mã xong, chờ Q-062** (bằng chứng W10.17 bên dưới) |
| KH-UR-KT-02 | Lũy kế, %, tổng, năng suất do hệ thống tính, ô khóa; sai thì sửa ở nguồn | KH-FN-TI-02.04…02.09 | W11.64, W11.66 | ★ trong G1 | BR-DL-05 (lũy kế từ phiếu), BR-DL-07 (năng suất bình quân gia quyền); số lấy từ module theo Q4–Q5 của khtc-week-mapping: trong kỳ = lũy kế nguồn − gốc; khớp golden G01–G05 | `TestKH_W1164_IncrementsFromCumulativesGolden`, `TestKH_FN_TI_02_06_MonthSumsWeeks`, `TestKH_UR_KT_02_YieldIsOutputOverArea`, `TestKH_ReportAdministrativeOverridesAndLockedNumbers` | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới) |

## 4. FN (tiêu chí chấp nhận)

| Mã | Chức năng | Gói | Phạm vi G1 | Tiêu chí chấp nhận (BRD, chỉnh cho G1) | Kiểm thử | Trạng thái |
|---|---|---|---|---|---|---|
| KH-FN-CH-00.01 | Đăng nhập, chọn vai trò và phòng ban | P1 (có), W11.06 | trong G1 | Vào đúng trang mặc định và menu theo Mục V.3. **G1:** Cán bộ KHTC vào "Kỳ báo cáo", vì Trang chủ ở W11.69 (sau G1). Viên chức phòng chuyên môn vào "Báo cáo cho KHTC › Phiếu kỳ này". | nền có sẵn: `TestFN_CH_00_01_MultiRoleContextsAndSwitch`, `TestW1106_MenuV3DepartmentTemplates`; *(dự kiến)* `TestKH_FN_CH_00_01_DefaultRouteKHTC` | **một phần** (**mã**) |
| KH-FN-TW-01.01 | Báo cáo đã xuất (dùng chung) | W11.69 | sau G1 | Danh sách đủ các tệp đã xuất theo quyền; tệp tải về trùng tệp KHTC đã xuất | *(dự kiến)* `TestKH_FN_TW_01_01_ExportedReportsReadOnly` | **chưa làm** |
| KH-FN-TI-02.01 | Trang chủ tổng hợp | W11.69 | sau G1 | Số liệu trùng báo cáo tiến độ CT cùng thời điểm; trạng thái phiếu và nguồn cập nhật ngay khi phòng gửi hoặc chốt | *(dự kiến)* `TestKH_FN_TI_02_01_DashboardMatchesProgressReport` | **chưa làm** |
| KH-FN-TI-02.02 | Kế hoạch năm các chương trình | W11.61 | ★ trong G1 | Kế hoạch đã lưu hiện ngay trong phiếu kỳ mới và phần Tự điền của 5 mẫu. Phiên bản cũ xem được, không bị ghi đè; phiên bản dùng cho kỳ theo BR-DL-13. Năm 2027 có 13 CT → phiếu, Mẫu 1–5, phụ lục Excel có 13 CT. Ngừng CT X từ 01/7/2027 → báo cáo tuần sau ngày đó không có X, quý II vẫn có. | `TestFN_TI_02_02_AnnualPlan`, `TestKH_ReportYearWith53WeeksAndThirteenPrograms`, `TestKH_ReportStoppedParentAndMissingForms` | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới) |
| KH-FN-TI-02.03 | Kỳ báo cáo và tiến độ nộp | W11.62 | ★ trong G1 | Năm 2026, Tuần 41 → khoảng "07/10/2026 – 13/10/2026", tên "Tuần 41 năm 2026", ngày chốt 13/10/2026, hạn nộp **14/10/2026 10:00**. W11.62 phải đặt hạn này riêng, vì hạn mặc định v2 của W11.03 là 12:00 (khtc-week-mapping M7). Nhãn W11.03 là "Tuần 41/2026"; tên kỳ KHTC theo BRD. Người nhận việc thấy phiếu ngay sau khi mở kỳ. Kỳ nguồn gắn theo quy tắc Q3: CNTS 2026-W46 (10/11–16/11) vào KHTC 2026-W46 (G01). Trạng thái nguồn cập nhật ngay khi phòng chốt. Tuần 53 chỉ có ở năm 53 tuần (KHTC 2030, G05C). | `TestKHTCPeriod_W1162`, `TestPlanOf_WeekDeadlines`, `TestKH_FN_TI_02_03_AttachSourcePeriodsGolden` | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới) |
| KH-FN-TI-02.04 | Rà soát phiếu số liệu | W11.63 rút gọn | ★ trong G1 (không có "So sánh lần gửi") | Phòng nhận yêu cầu kèm nội dung trong 1 phút. Ô vi phạm được đánh dấu đúng 3 quy tắc: yêu cầu của mẫu, số âm phải có lý do, lũy kế > 100 % KH. Gửi lần 3 tự khóa. | nền có sẵn: `TestFormsDepartmentLimitCancel_W1105`; `TestKH_FN_TI_02_04_ReviewRulesAndRequest`, `TestKH_FN_TI_02_04_Rules` (W11.63) | **đạt** (G1, không có "So sánh lần gửi") |
| KH-FN-TI-02.05 | Báo cáo tuần (Mẫu 1) | W11.64, W11.66 | ★ trong G1 (chỉ Excel ⚑1) | **G1:** tệp Excel giữ đúng mục A–D của Bảng FN-TI-02.05. Số MD bằng dữ liệu đã chốt của kỳ nguồn theo quy đổi: A.I.1 Trồng trọt (G02, G03), A.I.3 Thủy sản (G01, G04), A.III giá min/max toàn tỉnh của hai ngày đã duyệt gần nhất (G02B), HTX tách đã/chưa xác nhận (G06). Số PH = lần gửi cuối của phiếu. Ô số, ô module, ô phiếu không sửa được. Mục thiếu nguồn → "Tạm tính" (BR-DL-03); câu in theo BR-DL-14. **Sau G1:** Word; sửa câu dẫn và chèn đoạn. | `TestKH_FN_TI_02_05_AllFiveWorkbooks`, `TestKH_FN_TI_02_05_PriceRangeApprovedBoards`, `TestKH_FN_TI_02_05_ReportExcelFrozenAndFinalCancel`, `TestKH_ReportLivePublishedReaderWithoutData` | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới) |
| KH-FN-TI-02.06 | Báo cáo tháng (Mẫu 2) | W11.66 | ★ trong G1 (chỉ Excel ⚑1) | Excel giữ đúng mục lục Mẫu 2. Số trong tháng = tổng các tuần báo cáo có Thứ Ba thuộc tháng (BR-DL-06, `month=week_end`), ví dụ Tháng 9/2026 có số liệu đến 29/09/2026. Ô số không sửa được. | `TestKH_FN_TI_02_06_MonthSumsWeeks`, `TestKH_FN_TI_02_05_AllFiveWorkbooks` | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới) |
| KH-FN-TI-02.07 | Báo cáo quý, 6 tháng, 9 tháng (Mẫu 3) | W11.66 | ★ trong G1 (chỉ Excel ⚑1) | Excel giữ đúng mục lục Mẫu 3 và phụ lục; phụ lục xuất riêng ra Excel; số lũy kế từ đầu năm | `TestKH_FN_TI_02_05_AllFiveWorkbooks`, `TestPlanOf_BRDExamples` | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới) |
| KH-FN-TI-02.08 | Báo cáo năm (Mẫu 4) | W11.66 | ★ trong G1 (chỉ Excel ⚑1) | Excel giữ đúng mục lục Mẫu 4; phụ lục = lũy kế đến ngày chốt. Năm 2026 = tuần 1–52, số liệu đến 29/12/2026; thủy sản năm 2026 = 6.020,0 tấn (G05). Ngày 30–31/12/2026 thuộc năm 2027. | `TestKH_FN_TI_02_05_AllFiveWorkbooks`, `TestKH_ReportYearWith53WeeksAndThirteenPrograms`, `TestKH_W1164_IncrementsFromCumulativesGolden` | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới) |
| KH-FN-TI-02.09 | Báo cáo tiến độ các CT (Mẫu 5) | W11.66 | ★ trong G1 (chỉ Excel ⚑1) | Phụ lục = số liệu phiếu đến ngày chốt. Đúng cột TT, Tên chương trình, Quy mô, Tiến độ, Ghi chú. Lọc theo phòng, CT, kỳ, xã, mức đạt. Ô số không sửa được. | `TestKH_FN_TI_02_09_ProgressFilters`, `TestKH_ReportLevelFilterAndLatestStatus`, `TestKH_ReportProgramCopiesPreviousNarrativeOnly`, `TestKH_FN_TI_02_05_AllFiveWorkbooks`, `TestW10_17_WordAllFive` (Mẫu 5 Word, bộ lọc ghi trong văn bản) | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới). Word Mẫu 5: mã xong ở W10.17 |
| KH-FN-TI-02.10 | Báo cáo phát sinh | W11.68 | sau G1 | Bảng số liệu bằng dữ liệu sẵn có; yêu cầu bổ sung hiện ở module phòng; chỉ tiêu Nhập ngoài kèm nguồn; "Lưu thành mẫu" | *(dự kiến)* `TestKH_FN_TI_02_10_AdhocWithSupplementRequest` | **chưa làm** |
| KH-FN-TI-02.11 | Số liệu nguồn ngoài | W11.67 | sau G1 | Nạp Excel tuần 39 có 4 chỉ tiêu, 1 dòng thiếu nguồn → ghi 3, báo lỗi 1. Ô Nhập ngoài hiện đúng giá trị và nguồn. Sửa có lý do → lịch sử cũ → mới. | `TestKH_FN_TI_02_11_ImportWithSource`: nạp hai bước (bước kiểm không ghi; Hoàn tất ghi 3, dòng 5 thiếu nguồn lỗi; Hoàn tất lần hai 409), trùng chỉ tiêu × kỳ hỏi ghi đè, ô Nhập ngoài + nguồn + minh chứng trong bản soạn và Excel Mẫu 1, sửa có lý do → lịch sử 26.100 → 26.400, đổi sau xuất → "Nguồn đã thay đổi", kỳ hủy → chỉ xem, quyền | **đạt** (PostgreSQL thật `--- PASS`) |
| KH-FN-PN-03.01 | Phiếu kỳ này, phiếu đã gửi | W11.63 rút gọn, W11.11 tối thiểu | ★ trong G1 | Phiếu mới hiện ngay sau khi KHTC mở kỳ; trạng thái đổi ngay sau khi gửi; có ở cả 4 phòng | nền có sẵn: `TestW1106_MenuV3DepartmentTemplates`; `TestKH_FN_PN_03_01_TaskVisibleInEachDepartment` (W11.63; menu các phòng W11.11) | **đạt** (API + màn `/kn/khtc/phieu`) |
| KH-FN-PN-03.02 | Nhập phiếu số liệu | W11.63 rút gọn | ★ trong G1 | Bố cục tab CT trùng cấu trúc mẫu. Phiếu đã gửi không sửa được nếu chưa có yêu cầu bổ sung; kỳ đóng thì không yêu cầu bổ sung được. Số liệu gửi hiện ngay ở rà soát và màn soạn. Cột "Điều chỉnh kỳ trước" bắt buộc chọn kỳ được điều chỉnh (BR-DL-12). | `TestFormsClosedStatuses_W1105`; `TestKH_FN_PN_03_02_ResubmitOnlyOnRequest`, `TestKH_FN_PN_03_02_PriorPeriodAdjustment`, `TestKH_FN_PN_03_02_PriorYearAdjustmentNotCounted`, `TestKH_FN_PN_03_02_MonthTextOnly` (W11.63) | **đạt** |
| KH-FN-PN-03.03 | Tiến độ CT của phòng, báo cáo KHTC đã xuất | W11.69, W11.11 | sau G1 | Số liệu trùng báo cáo tiến độ CT cùng kỳ | *(dự kiến)* `TestKH_FN_PN_03_03_DepartmentProgressReadOnly` | **chưa làm** |
| KH-FN-AD-04.01 | Danh mục KHTC | W11.61 | ★ trong G1 | Danh mục mới hiện ngay ở các màn sử dụng. Người ký, nơi nhận, căn cứ mặc định cho từng mẫu. Mặt hàng giá đọc từ `cap/price`. | `TestFN_AD_04_01_Catalog`, `TestKH_FN_TI_02_05_ReportExcelFrozenAndFinalCancel` | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới) |
| KH-FN-AD-04.02 | Mẫu phiếu số liệu KHTC | W11.63 rút gọn | ★ trong G1: mẫu nạp sẵn; thiết kế mẫu sau G1 | Mẫu phát hành hiện đúng ở form phiếu của phòng nhập từ kỳ áp dụng. **G1:** 4 mẫu nạp sẵn có loại cột "Từ Kế hoạch năm", "Lũy kế kỳ trước". | `TestKH_FN_AD_04_02_SeededFormsPerDepartment` | **đạt** (G1 mẫu nạp sẵn; PO chốt Admin thiết kế mẫu sau G1) |
| KH-FN-AD-04.03 | Mục lục mẫu báo cáo | W11.65 | sau G1 | Khuôn câu "+ {Tên hạng mục}: trong tuần thực hiện {Số trong kỳ} {ĐVT} … Lũy kế {Lũy kế} {ĐVT}." in một dòng mỗi hạng mục. Xem trước trùng bản soạn. "Không gửi" → câu BR-DL-14. Nhân bản Mẫu 1. | *(dự kiến)* `TestKH_FN_AD_04_03_TemplateOutlinePublish` | **chưa làm** |

## 5. Quyết định 13/10/2026 → kiểm thử

| Mã | Quyết định | Gói | Phạm vi G1 | Tiêu chí chấp nhận (ca golden) | Kiểm thử | Trạng thái |
|---|---|---|---|---|---|---|
| KH-QD-01 | Kỳ nguồn tính trọn vào tuần KHTC chứa ngày cuối của nó | W11.62, W11.64 | ★ trong G1 | G01 (CNTS 10/11–16/11 → KHTC 2026-W46). G05 (CNTS 29/12/2026–04/01/2027 → 2027-W01). G05B (ISO 2026-W53 → 2027-W01). G05C (CNTS 2029-W53 → KHTC 2030-W01; CNTS 2030-W52 → KHTC 2030-W53). Mỗi tuần KHTC 2026–2031 nhận đúng một tuần của mỗi nguồn tuần. | `TestKH_FN_TI_02_03_AttachSourcePeriodsGolden`, `TestKHTCQA_WeeklySourcesMapOneToOne` | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới) |
| KH-QD-02 | Trong kỳ = hiệu lũy kế; lũy kế nguồn là chuẩn | W11.64 | ★ trong G1 | G01 (135,5 và 131,5), G03 (s3: 140/210; s4: −50), G05B (năm KHTC 2027 = 600 khi lũy kế vụ = 6.400) | `TestKH_W1164_IncrementsFromCumulativesGolden`, `TestKH_ReportSumYearToDateAndFrozenWeeks` | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới) |
| KH-QD-03 | "Đã chốt" = mọi kỳ nguồn quy về tuần đã chốt; không có kỳ nguồn → đã chốt (M1) | W11.62, W11.64 | ★ trong G1 | G02 (W48 không có kỳ nguồn → lũy kế 3.050, trong kỳ 0, đã chốt). G03 s2 (nguồn mở lại → Tạm tính, KHTC vẫn đọc 3.230). G02B (còn ngày chưa duyệt → giá Tạm tính). | `TestKH_W1164_IncrementsFromCumulativesGolden`, `TestKH_FN_TI_02_05_PriceRangeApprovedBoards` | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới) |
| KH-QD-04 | Tuần KHTC đã đóng giữ số; phần chênh vào tuần mở kế tiếp (M2) | W11.64 | ★ trong G1 | G04: W45, W46 giữ 4.947,5 / 5.079,0 và "Nguồn đã thay đổi"; W47 trong kỳ 131,0 (không phải 138,5); lũy kế = 5.210,0 | `TestKH_W1164_IncrementsFromCumulativesGolden`, `TestKH_UR_TI_13_RefreshOwnerOnlyDiffHistory` | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới) |
| KH-QD-05 | Giá Mẫu 1 = min/max toàn tỉnh của bảng giá đã duyệt; ngày chưa duyệt → ngày đã duyệt gần nhất (M4) | W11.64, W11.66, W11.76 | ★ trong G1 | G02B: lúc 01/12 16:00 hai cột 26/11 (8.000–13.500) và 27/11 (8.000–14.000), 8 phiếu, Tạm tính. Lúc 02/12 hai cột 30/11 (8.000–13.500) và 01/12 (8.500–13.000), 14 phiếu. | `TestKH_FN_TI_02_05_PriceRangeApprovedBoards`, `TestKH_FN_TI_02_05_AllFiveWorkbooks` | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới) |
| KH-QD-06 | HTX "Chưa xác nhận" tính vào tổng, tách đã/chưa xác nhận; nhận HTX chỉ đổi nhãn | W11.64, W11.66 | ★ trong G1 | G06: W49 có 6 = 4 + 2 HTX, 420 = 300 + 120 thành viên. W50 (sau khi nhận HTX05) có 6 = 5 + 1, 420 = 370 + 50. | `TestKH_W1164_UnconfirmedHTXSplitGolden`, `TestKH_M6ConfirmationAtCutoffInsteadOfCurrent`, `TestAttachMSTClaimIdempotentAudit_D2` | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới) |
| KH-QD-07 | ⚑2 Báo cáo đã gửi trước khi xã nhận HTX hoặc trước khi nguồn đổi không phát hành lại | W11.66 | ★ trong G1 | G04 (`reissued=false`), G06 (W49 giữ 4 + 2) | `TestKH_FN_TI_02_05_ReportExcelFrozenAndFinalCancel` | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới) |
| KH-QD-08 | ⚑3 Chỉ cán bộ KHTC phụ trách chạy "Cập nhật số liệu"; giữ lịch sử thay đổi | W11.64 | ★ trong G1 | G03 s3: 3 dòng lịch sử (W49 lũy kế 3.230 → 3.190, W49 trong kỳ 180 → 140, W50 trong kỳ 170 → 210), người = cán bộ phụ trách; cán bộ KHTC khác bấm → 403 | `TestKH_UR_TI_13_RefreshOwnerOnlyDiffHistory` | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới) |
| KH-QD-09 | ⚑1 G1 xuất Excel Mẫu 1–5; Word ở G1.1 | W11.66, W10.17 | ★ trong G1 | Như KH-UR-KT-01 | `TestKH_FN_TI_02_05_AllFiveWorkbooks`, `TestKH_W10_17_WordFromFrozenExcel` | **đạt** (G1 tối thiểu, Excel; bằng chứng W11.66 bên dưới). Word: mã xong ở W10.17, bật khi PO chốt Q-062 |

## 6. Rủi ro cho G1 (15/12/2026)

- **Lịch.** Bản đối chiếu §5 xếp W11.61–W11.69 vào Đợt 3 (03–05/2027). Quyết định 13/10/2026 kéo phần ★ về G1. Phần ★ gồm W11.61 (L), W11.62 (L), W11.63 rút gọn (≈ M), W11.64 (M), W11.66 chỉ Excel (≈ L), cộng W11.11 tối thiểu. Tổng cỡ 5 gói L/M trong khoảng 9 tuần, song song với Trồng trọt và Báo giá.
- **Phụ thuộc chưa có.**
  - Biểu CNTS native (W11.42) chưa có nhánh `UNION ALL` vào view nguồn đã chốt. Ghi chú W11.10 nói rõ: "nếu dùng kỳ native `cap/forms` thì thêm nhánh". Nếu chưa làm, KHTC không đọc được CNTS.
  - Cờ HTX "Chưa xác nhận" chưa có trong `_pub`.
- **Hạn nộp tuần.** BRD Mục II.4 đặt 10:00 Thứ Tư; W11.03 mặc định 12:00 (M7).
- **Kỳ trước 04/11/2026.** Tuần KHTC dùng quy tắc v2 từ 04/11/2026. Đợt Trồng trọt cũ (v1) có tuần ISO; quy đổi vẫn đúng theo ngày cuối (G05B). Phải có khóa kỳ đã lưu (W11.05b/W11.10: kỳ phát hành trước pf 000143 không có `period_key`).

### Kiểm tra tiếp nhận W11.63 ngày 15/10/2026

Các ca `TestKH_FN_PN_03_01_TaskVisibleInEachDepartment`,
`TestKH_FN_PN_03_02_ModuleSourcedCellsReadOnly`,
`TestKH_FN_PN_03_02_ResubmitOnlyOnRequest`,
`TestKH_FN_TI_02_04_ReviewRulesAndRequest`,
`TestKH_FN_PN_03_02_PriorPeriodAdjustment` và
`TestKH_FN_PN_03_02_OrphanSealedSheetIgnored` đã PASS trên PostgreSQL thật.
Toàn bộ suite KN, cap/forms, kỳ/kế hoạch KHTC, assembly và cổng migration PASS,
không có SKIP. Frontend đạt typecheck, 509 test (57 tệp, chạy 1 worker,
giữ timeout mặc định) và build mock. Lượt chạy nhiều worker có một ca CNTS
cũ timeout; ca đó PASS khi chạy riêng và trong toàn suite tuần tự.

Phạm vi nghiệm thu vẫn là G1 rút gọn đã ghi trong bảng; không nâng các mục
Admin sửa mẫu, nhập Excel hay so sánh lần gửi thành đã hoàn thành.

### Kiểm chứng W11.66 đang hoàn thiện — 16/10/2026

Bộ thành phần Excel mở lại đủ Mẫu 1–5; SUM trên ô số cho kết quả đúng; phần trăm là số có định dạng; phụ lục năm cột có sheet chi tiết số riêng. Trang nguồn ghi kỳ gốc, ngày, module, cờ chốt, gốc lũy kế và cách tính. Năng suất từ tổng sản lượng chia tổng diện tích, không trung bình các tỷ lệ.

Tám ca HTTP report trên PostgreSQL riêng đã PASS, không SKIP (360,519 giây); có final/cancel đồng thời, giữ bytes lịch sử, nguồn công bố thực và Mẫu5 mụcII chép kỳ trước. Những bổ sung trace/yield được kiểm lại trong bộ consumers cuối. Bằng chứng chi tiết và mã thoát ở [REGRESSION](../REGRESSION.md). Chưa đánh dấu W11.66 tích hợp khi các cổng cuối chưa kết thúc.

### Kết quả cuối W11.66 — 16/10/2026

Các package party/extension/forms/cooperative-anchor/assembly đều hoàn tất PASS trong full-third. Lượt này bị gián đoạn ở KN77; giữ nguyên mã và chạy lại riêng KN77: **exit0,268,904 giây, không FAIL/SKIP**, gồm toàn bộ ca phiếu/kỳ/nguồn và8ca báo cáo mới. Không ghi lượt bị gián đoạn thành lệnh đạt.

Web cuối566ca/68tệp PASS137,54s, registry5PASS, checkAPI/lint/typecheck/buildmock0; ca trình duyệt báo cáo1PASS14,7s. Cổng tĩnh cuối đều0, boundary390. Excel mở lại kiểm ô số cộng được, %numeric, phụ lục5cột, gốc/trace, hai ngày giá; năm53tuần/13chương trình và năng suất gia quyền đã kiểm. Quy trình helper còn kiểm migration trước khi đẩy dev.

**Toàn G1 vẫn chưa xong:** KNSPLIT1/2/4b/3 và3hàng KHTC về trang mặc định (UR/FN đăng nhập), nhắc hạn KHTC. Không mở lại các quyết định PO; không tự nhận đã bàn giao Word/richtext/mẫu khách chưa cung cấp.


Bổ sung M4 (16/10/2026, W11.66): `TestKH_M4_PriceCutoffPublishedReader` đối chứng hai ngày giá không sau cutoff14/10 dù kỳ hết13/10, và nguồn giá cũ hơn62ngày; `TestKH_M4_SparsePriceItems` giữ hai ngày riêng cho mặt hàng thưa báo giá. `TestKH_M4_ExportKeepsPriceSnapshot` kiểm cách ly tenant, đọc hai cột ngày/min từ Excel thật, cảnh báo khi board ngoài kỳ đổi, bản xuất cũ giữ nguyên byte sau duyệt và refresh. `TestKH_M4_PriceDateDoesNotExtendQuantityPeriod` giữ kỳ số lượng/D8; `TestKH_M4_LegacyPriceProjectionDoesNotInventSourceChange` bảo vệ snapshot cũ khỏi cảnh báo giả. Toàn bộ11package liên quan PASS không SKIP, xem REGRESSION. Không thay M1–M7; phần số lượng/phiếu Mẫu5 khi cutoff sớm tiếp tục được kiểm chứng riêng.


Bổ sung KH-FN-TI-02.09 (Mẫu5 cutoff sớm,16/10): `TestKH_M5_ProgramUsesCutoffForWeeklyFigures` giữ tuần nguồn trọn vẹn tới cutoff và phiếu hiện tại cho lời văn; `TestKH_M5_ProgramCutoffHTTP` đối chứng10 thay vì100 từ phiếu,100 thay vì120 từ module; đọc Excel phụ lục10/100 và giữ hash/cảnh báo/byte cũ sau thay dữ liệu tuần sau. `TestKH_M5_LiveProgramSourceCrosswalk` kiểm đường published thật với custom FormsKey và cutoff giữa tuần. Đủ11package liên quan PASS không SKIP; không đổi identity kỳ/To hoặc M4/M6. Xem REGRESSION.

### Bằng chứng W10.17 — Word Mẫu 1–5 qua `cap/export` (20/10/2026)

- Bộ ghi .docx thuần Go, tất định (`internal/shared/docx`): `TestDeterministic` khóa băm byte, `TestExtractRoundTrip` đọc lại chữ/bảng. Ngôn ngữ mẫu (`internal/shared/doctpl`): `TestValidate`, `TestRenderGolden`, `TestDates` (dd/mm/yyyy, giờ Việt Nam).
- Mẫu Word KHTC: `TestW10_17_WordAllFive` (đủ 5 mẫu, đọc lại số 100/20/85/85%, 12.500,75, giá 13.500, phụ lục Mẫu 3–5, Kính gửi Mẫu 5), `TestW10_17_WordGolden` (băm byte Mẫu 3 khóa cứng, số/ngày để pf/export cấp).
- PostgreSQL thật: `TestExportRun_W10_17`, `TestDocNumberGapSafe_W10_17` (12 lần xuất đồng thời → số 1…13 không trùng, không hở; giao dịch hỏng sau khi cấp số không để lỗ; năm mới theo giờ Việt Nam đánh lại từ 1), `TestTemplateVersionsImmutable_W10_17`, `TestAdminAPI_W10_17`, `TestKH_W10_17_WordFromFrozenExcel` (HTTP: chỉ cán bộ phụ trách, bản Tạm tính không cấp số, số dùng lại cho cùng báo cáo, xuất lại cùng byte, tải qua điểm kiểm soát xuất, Word sau Bản cuối, số tay "77" → "77/BC-TTKN").
- Web: nút "Xuất Word vN" cạnh từng bản Excel, "Lịch sử Word"; màn Admin "Mẫu văn bản & lịch sử xuất" (`/kn/quan-tri/mau-van-ban`); `word.test.ts` trên giả lập. Giả lập trả tệp văn bản có nhãn GIẢ LẬP — không dùng làm bằng chứng .docx.
- Còn mở: Q-062 (mẫu .docx gốc của Sở làm khung hay bộ ghi tự dựng; PDF cho biểu NS). Thể thức hiện là bộ ghi tự dựng theo NĐ 30/2020; chưa đối chiếu với mẫu gốc của khách.
