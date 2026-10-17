## KNSPLIT-2-MDCATALOG-D — Bỏ lookup CNTS legacy

Nền78d9c470. Xóa CNTSPort và root Module.CNTSCatalogs, store.FacilityCodeByRef, domain.ToFacility đã không còn production caller từ C. NormalizeCatalogItem dùng ToPortItem generic đang có thay helper private trong phần xóa (cùng DTO, thêm clone map; 170 ca chuẩn hóa đóng băng kiểm lại). Không đổi adapter KN, generic EffectiveCatalogs, schema/migration/wire/quyền/RLS hay lắp ghép đã kiểm trên22gói ở C.

TestW1141_CNTSCatalogs giữ assertion HTTP và9callsite/17runtimecase đối chiếu trực tiếp golden legacy đã chụp ở C. Helper chỉ đọc golden, canonicalUUID và kiểm seen17; xóa generator/env ghi đè expected và mọi runtime legacy comparison. Giữ nguyên byte golden17lookup,12schema/60seed/170normalization vàparams16/13; adapter owner cũng nguyên byte. Native MRV/HTX/MKG không đổi.

Impact: chỉ lookup dead paths bị xóa, root façade không còn consumer (rg); owner implementation và wiring không đổi. Vì vậy D kiểm pure4 + PostgreSQL tests/masterdata (golden default mode, date/RLS/CRUD), API assembly (wiring), KN77 (nghiệm thu/KHTC), tổng7package cótest; không lặp22gói consumer khi algorithms/wiring không thay. Full build/vet/archlint/contracts/docs/modgen/migrate/MFA/boundary/lint và helper9migration vẫn bắt buộc. Nếu phát hiện runtime change khác, mở rộng scope trước tích hợp. Không cần web/mobile do UI/wire không đổi.

Chưa xong masterdata: interface/DTO/const CNTS, ParseDoses/Stamps, refs/stableUUID/commune/stamp write rules vẫn ở core tới E/F. Chưa đổi tên file để che coupling, baseline chỉ bỏ symbol thực sự xóa. Pure4 đạt. Cần PG7+static/helper9 thật trước dev. G1 còn các registry/data/routes/tenant/client/KHTCreminder, giữ toàn bộ quyết định PO, main và worktrees.
