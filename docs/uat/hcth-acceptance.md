# HCTH v4.0 — nghiệm thu theo từng gói

Nguồn nghiệp vụ duy nhất: BRD HCTH v4.0 trong BRD 2.0 đã chốt,
[tài liệu Drive](https://docs.google.com/document/d/1r0TqvDU8yRZSdm5BwabvWkgElm8ynC7Q/edit).
Các phần dưới ghi phạm vi HC-0 (#256) và HC-2 (#281); các FN khác được nghiệm thu tại gói tương ứng.

## HC-0 — Gỡ FN-21 cũ và tự đánh giá Cấp Xã

| Function / BR / trường | Tiêu chí cần đạt | Bằng chứng chạy lại |
|---|---|---|
| FN-16 BR-04; FN-17 BR-01, DG-04/DG-05 | Nhân viên/Cấp Xã không có đường tự nhập; lãnh đạo được phân công và HCTH vẫn đọc/nhập đề xuất của họ | `TestAppraisalRemovedSelfCapabilityHC0`, `TestHRMRemovedSelfHTTPHC0`; hồi quy native workflow/export/proof |
| FN-20 BR-01; quyền hệ thống | Xã/TW/cán bộ Tỉnh thông thường không được đọc/ghi đánh giá kể cả có AccountRef, cờ Manager hoặc override assessment all cũ; các quyền Contact/hồ sơ/tài liệu không bị mở rộng | Test domain/HTTP HC-0, hồi quy đầy đủ `./internal/kn/hrm/...` |
| FN-21 cũ bị bỏ | Không còn route/handler/service/contract guest-link HCTH; POST đường cũ trả404 | `TestHRMRemovedSelfHTTPHC0`; mock regression |
| FN-21 cũ; tự đánh giá bị bỏ | Không còn trang `/kn/nhan-su/danh-gia-cua-toi` hoặc hai trang con, menu và alias; cả ba URL trả404 và giữ nguyên URL | E2E HC-0; shell test từ chối cả menu cũ đã cache |
| Bảo toàn dữ liệu | Migration không thay event/payload/hash cũ; đọc được self_rating/điểm cũ; INSERT self_draft/self_submit bị DB từ chối SQLSTATE42501 | `TestHRMRemovedSelfMigrationPreservesHistoryHC0`, `TestHRMRemovedSelfHTTPHC0`: đếm + chuỗi băm trước/sau |
| Hồi quy cohort/idempotency HC-0 | 494 hồ sơ (470 MS05/24 MS06), hoặc 500 hồ sơ, vẫn đóng băng; 500 lần nhập thay tuần tự do lãnh đạo/HCTH từ scan đúng quyền; cá nhân/Xã không được tự đọc/nhập | `tests/hc30`: `TestHCTHQuarterCohort_HC0`, `TestHCTH500ManagementSubmissions_HC0`; không phải chứng nhận tải đồng thời/toàn FN |
| Giao diện còn dùng | HCTH giữ mẫu đóng băng/lịch sử và gửi manager_submit; không có nút/action tự nộp; mock không giả chứng từ | `appraisal.test.tsx`; E2E luồng HCTH hiện có |

### Phần chưa thuộc nghiệm thu HC-0

HC-2 thực hiện FN-16/17/18 đầy đủ: bỏ tiêu chí/điểm/phiếu chấm, chuyển trạng thái và dữ liệu cũ sang chỉ đọc.
Tại thời điểm HC-0, nhập thay từ bản giấy có scan do lãnh đạo/HCTH và các trình bày lịch sử vẫn được giữ; HC-2 bên dưới đã gỡ writer đó.
đây không phải kết luận đạt FN-17 BR-02 hoặc vòng đời v4.0 đầy đủ.
Các FN khác (nâng lương, biểu NB, quyết định, hồ sơ) có tiêu chí ở gói tương ứng, chưa được chấm tại đây.

### Chạy lại

```powershell
.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./tests/hc30 ./internal/kn/hrm/...
cd apps/web-ops
npm ci
npm run check:api
npm run typecheck
npm test
npm run build:mock
npm run e2e -- tests/e2e/hrm-appraisal.spec.ts
```

Chi tiết migration, giới hạn phạm vi và chạy API/web: [HCTH-V4-REMOVE-FN21-SELF](../kn/gia-dinh/HCTH-V4-REMOVE-FN21-SELF.md).

## HC-2 — Đề xuất xếp loại v4.0 (#281)

Bảng này thay các assertions chấm điểm, proxy/scan và annual-rating trong kiểm thử trước HC-2. Bộ bảo vệ reader lịch sử, HS37, tenant/nút, vault và hash tiếp tục giữ.

| Function / UR / BR / trường | Hành vi nghiệm thu | Bằng chứng |
|---|---|---|
| FN-16, UR-HC-20, BR-01–06, AC-01 | Chụp ngày chốt và chức vụ; chia Mẫu 05/06, giao theo HS37; 494 người = 470/24; thay nhân sự không đổi cohort | `tests/hc30` hai ca cohort 494/500; `TestHRMAppraisalPositionSnapshotHTTP_W1126` |
| FN-16, AC-02/03 | Nhập/gửi cập nhật năm trạng thái và số đơn vị đã gửi; không còn bước tự chấm/khóa bước cũ; không yêu cầu tài khoản cá nhân | `TestHRMAppraisalWorkflowHTTP_W1126`, `TestHRMAppraisalPeriodV4GroupsWarningsAndActions281`; UI/mock/menu parity |
| FN-17, UR-LDP-01, DG-04–08, BR-01/02, AC-01/02/04 | Lãnh đạo nhập cả hai mức của người trong đơn vị quản lý; HCTH nhập MS06; HTXSNV/KHTNV ở bất kỳ mức nào thiếu căn cứ đều không lưu được | `TestAppraisalV4ManagerEntersProposalDirectly281`, `TestAppraisalV4RejectsIncompleteAndScoring281`, native Workflow, web `appraisal.test.tsx` |
| FN-17, luồng Excel, BR-03 | Tải mẫu tám cột, nhập trong phân công; chặn trùng/không khớp tên, thiếu mức/căn cứ/công thức; toàn bộ tệp nguyên tử và retry không thêm phiên bản | `TestHRMAppraisalImportAtomicScopeAndRetry281`, `TestHRMAppraisalImportRejectsFormulaAndWrongFormat281`, multipart retry web |
| FN-17 AC-03; FN-18 BR-04 | Sau gửi HCTH thấy đề xuất; đơn vị chỉ sửa khi được yêu cầu; sau chốt chỉ mở lại riêng một người có lý do | native Workflow/AnnualAndRaces; domain state/correction tests; UI/mock |
| FN-18, UR-HC-21, BR-01/02, AC-01 | Preview/Word/Excel theo snapshot mẫu, đúng tám cột và phần ký, sắp đơn vị/chức vụ; chặn xuất nếu bất kỳ người nào trong đợt chưa đủ | `TestHRMAppraisalExportAndAuditHTTP_W1126`, import atomic test; production Playwright kiểm preview |
| FN-18, DG-09, BR-03, AC-02 | Chốt ưu tiên kết luận chính thức rồi tập thể; kết quả quý/năm trong hồ sơ; không ghi hoặc phục hồi POST annual-rating | native Workflow/AnnualAndRaces; `AppraisalProfileResults`; HTTP âm POST ratings |
| FN-18 AC-03 | Bản ký lưu qua Hồ sơ đánh giá; thiếu bản ký cảnh báo có xác nhận; kiểm lại quyền/chủ thể/năm tài liệu trước gắn/chốt | `TestHRMAppraisalProofAndSignedHTTP_W1126`, hồi quy kho tài liệu native |
| HC-CR07/08/09; dữ liệu cũ | Không có writer tiêu chí/điểm/scan, không còn route phiếu; form, answers và lần xuất điểm lịch sử vẫn đọc; migration và rollback/reapply không đổi count/payload/hash, gồm annual-rating cũ; đổi Nút rồi replay không nhận metadata | `TestHRMAppraisalLegacyReadAndMigration281`, các ca HC-0 giữ nguyên, UI legacy reader và E2E 404 |

Chi tiết trường, quyết định khi bảng Function khác phần tổng hợp, kịch bản count/checksum và lệnh chạy: [HC-2](../kn/gia-dinh/HCTH-V4-APPRAISAL-LITE.md). Native sử dụng PostgreSQL thật và adapter kho định danh/tệp kiểm quyền; chưa phải xác nhận môi trường triển khai hoặc dữ liệu người dùng thật.
