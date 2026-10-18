# Mã lỗi theo module (tệp riêng)

Phần chung (định dạng `application/problem+json`, mã dùng chung, quy tắc "mã là hợp đồng") ở [../errors.md](../errors.md). Mục của các module đã có từ trước vẫn nằm trong tệp đó.

Từ 13/10/2026 (WH-B), module **mới** hoặc **mã mới của một gói** ghi vào tệp riêng của module trong thư mục này, để các gói chạy song song không cùng sửa `docs/errors.md`:

- Tên tệp: `<cụm>-<module>.md`, vd `pf-price.md`, `mkg-orders.md`. Một gói thêm mã cho module đã có mục trong `errors.md` thì vẫn tạo tệp này (chỉ chứa mã mới), không chép mục cũ sang.
- Nội dung: một tiêu đề `### <cụm>/<module> (…)`, rồi bảng cùng cột như `errors.md`:

  ```markdown
  ### pf/price (`cap/price` — duyệt bảng giá, W11.76)

  | HTTP | code | Khi nào |
  |---|---|---|
  | 409 | `price.board_already_approved` | Bảng giá đã duyệt |
  ```

- `go run ./tools/docscheck` (CI) kiểm: mỗi tệp có tiêu đề `###`; mã ở cột `code` không trùng với mã nào trong `errors.md` hay tệp khác của thư mục này.
- Nhãn tiếng Việt cho web (`apps/*/src/api/problem.ts`) vẫn thêm như cũ. Kiểm thử web hiện đọc `docs/errors.md` cho mã G4/G5 (`g4.test.ts`, `g5.test.tsx`); mã mới ở đây không cần kiểm thử đó.

Không có danh sách tệp ở đây (danh sách là chính thư mục) nên thêm tệp không sửa README này.
