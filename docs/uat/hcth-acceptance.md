# HCTH v4.0 — nghiệm thu theo từng gói

Nguồn nghiệp vụ duy nhất: BRD HCTH v4.0 trong BRD 2.0 đã chốt,
[tài liệu Drive](https://docs.google.com/document/d/1r0TqvDU8yRZSdm5BwabvWkgElm8ynC7Q/edit).
Phần dưới chỉ ghi các tiêu chí HC-0 (#256) chạm tới; không chứng nhận toàn bộ module hoặc toàn bộ FN.

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
HC-0 vẫn giữ nhập thay từ bản giấy có scan do lãnh đạo/HCTH thực hiện và các trình bày lịch sử cần cho luồng cũ;
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
