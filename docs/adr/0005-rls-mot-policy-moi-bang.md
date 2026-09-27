# ADR-0005: Row-level security, một policy mỗi bảng

**Trạng thái:** Chấp nhận · **Ngày:** 27/09/2026

## Quyết định
- Mọi bảng nghiệp vụ có `tenant_id`, ENABLE + FORCE RLS và đúng một policy `p_tenant` (R-33).
- Truy cập DB chỉ qua `tenant.WithTenantTx`, dùng `set_config('app.tenant_id', ..., true)`, tức SET LOCAL (R-30).
- Phân vùng của bảng sổ cũng có RLS riêng, vì truy cập thẳng phân vùng không đi qua policy của bảng cha.

## Lệch so với mẫu mục 9 của tài liệu kiến trúc
Trên một kết nối đã từng `SET LOCAL app.tenant_id`, sau khi giao dịch kết thúc `current_setting('app.tenant_id', true)` trả **chuỗi rỗng** chứ không phải NULL. Khi đó `''::uuid` gây **lỗi cú pháp** trên mọi truy vấn không có ngữ cảnh, thay vì trả rỗng như R-31 mô tả. Kiểm thử `TestNoTenantContextSeesNothing` đã bắt được lỗi này.

Policy vì vậy dùng `shared.current_tenant()` = `nullif(current_setting('app.tenant_id', true), '')::uuid`. **Đề nghị cập nhật mẫu SQL ở mục 9 của tài liệu kiến trúc.**

## Kiểm chứng
- `TestTenantLeakParallel` (R-32): 24 tenant × 15 vòng, song song. CI chạy qua PgBouncer transaction mode (`AG_TEST_PGBOUNCER_URL`).
- `TestNoTenantContextSeesNothing`: không có ngữ cảnh thì rỗng, kể cả với owner.
- `TestCannotWriteForeignTenant`: WITH CHECK chặn ghi hộ tenant khác.
- `TestContextDoesNotOutliveTransaction`: ngữ cảnh không còn sót trên kết nối sau giao dịch.
