### pf/cooperative (neo HTX theo mã số thuế — D2, ADR-0052)

| HTTP | code | Khi nào |
|---|---|---|
| 404 | `cooperative.htx_anchor_not_found` | Không có neo HTX này (hoặc tenant khác) |
| 422 | `cooperative.htx_anchor_mst_malformed` | Mã số thuế sau chuẩn hóa (bỏ khoảng trắng, gạch) không là 10 chữ số hoặc 10 + 3 chữ số chi nhánh 001–999 |
| 409 | `cooperative.htx_anchor_mst_taken` | MST đã có neo (KN và MKG cùng lập) hoặc đã gắn hồ sơ HTX khác — `existing_anchor_id`, `existing_created_by` hoặc `existing_org_ref` chỉ bản đã có; không gộp ngầm |
| 422 | `cooperative.htx_anchor_org_not_cooperative` | Nút (org_ref / target_org_ref) không phải HTX có hồ sơ |
| 409 | `cooperative.htx_anchor_org_anchored` | Nút HTX đã thuộc một neo khác (`existing_anchor_id`) |
| 422 | `cooperative.htx_anchor_mst_profile_mismatch` | MST khác mã số thuế trong hồ sơ HTX của Nút (`profile_tax_code`) |
| 409 | `cooperative.htx_anchor_mst_present` | Gắn MST cho neo đã có MST khác |
| 422 | `cooperative.htx_anchor_mst_required` | Nhận quyền neo mã tạm — gắn MST trước |
| 422 | `cooperative.htx_anchor_mst_mismatch` | `verified_mst` khác MST của neo |
| 409 | `cooperative.htx_anchor_already_claimed` | Neo đã được nhận quyền bởi tài khoản / Nút khác (một lần nhận quyền mỗi neo) |
| 409 | `cooperative.htx_anchor_claim_conflict` | Nút HTX đích đã có dữ liệu / thuộc neo khác / MST hồ sơ khác — `conflict_report` (cùng dạng báo cáo chạy thử) |
| 422 | `cooperative.htx_anchor_created_by` | `created_by` không phải `kn` hoặc `mkg` |
