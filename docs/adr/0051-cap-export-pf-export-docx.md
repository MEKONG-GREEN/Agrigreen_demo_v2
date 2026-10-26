# ADR-0051: Tách `cap/export` thành module năng lực `pf/export` — mẫu văn bản có phiên bản, bộ ghi .docx tất định, sổ lần xuất có băm, số văn bản tự cấp

**Trạng thái:** Đề xuất — hiện thực ở W10.17 (20/10/2026, pf 20261024090957); dùng thật chờ Q-062 · **Ngày:** 20/10/2026 (gói W10.17) · **Yêu cầu:** kiến trúc v4.1 §5, §7.1 (R-MOD-05, R-MOD-09), §26, RB-1, RB-4, R-12, R-20 · **Nguồn:** đối chiếu BRD KN 10/2026 §3.2 hàng W10.17, Phụ lục D X7, D3 H8; BRD KHTC v2.2 KH-UR-KT-01, KH-QD-09; prototype App A `routes_more.py` (số `{n}/BC-TTKN`, ngày ký tự điền) · **Thành phần:** `internal/platform/export`, `internal/shared/docx`, `internal/shared/doctpl`, `internal/kn/extension/khtcreport` (bên dùng đầu), `cmd/api-platform/assembly/export.go` · **Liên quan:** [ADR-0014](0014-thu-vien-xuat-bao-cao-va-phong-chu.md), [ADR-0024](0024-diem-kiem-soat-xuat-du-lieu-dlp.md), [ADR-0027](0027-bon-tang-module-tep-khai-bao.md), [ADR-0028](0028-vai-dang-nhap-theo-don-vi-trien-khai.md), [ADR-0055](0055-tach-he-thong-khuyen-nong.md)

## Bối cảnh

- W6.6 (`docs/dev/modules.md`) quyết chưa tách `cap/export`, khai `hosts` ở 8 module, và đặt điều kiện tách (i): "mẫu văn bản có phiên bản/PDF cần cho ứng dụng thứ hai".
- Đợt KN 10/2026 có bốn bên cần xuất Word có thể thức: KHTC Mẫu 1–5 (W11.66), CNTS BM-11 (W11.45), HCTH MS05/06 (W11.26), Mẫu KNQG 01–07 (W11.53). Repo chưa có bộ ghi .docx (Q-062). Điều kiện (i) đạt.
- Văn bản hành chính cần: phiên bản mẫu bất biến (đối chiếu sau này), tệp lưu nguyên byte + băm (như Excel W11.66), số văn bản tuần tự theo năm không trùng/không hở, ngày tháng giờ Việt Nam dd/mm/yyyy.
- RB-4: phụ thuộc mới phải có giấy phép trong `deploy/policies/allowed-licenses.txt`. Thư viện .docx Go phổ biến có giấy phép thương mại/AGPL hoặc ít bảo trì.

## Quyết định

1. **Module `pf/export`** (`aka: [cap/export]`, `kind: capability`, `zones: [pf]`, `enable: always`, đặt ở `internal/platform/export` vì là năng lực dùng chung, không riêng KN). Chủ schema `pf_export`, vai `r_export`. Gỡ `hosts: [cap/export]` ở 8 module (`modreg` bắt buộc khi năng lực đã có module).
2. **Ngôn ngữ mẫu là thư viện** `internal/shared/doctpl`: mẫu = khóa + phiên bản + biến khai báo + danh sách (bản ghi trường chuỗi) + khối (`paragraph`, `heading`, `section`, `loop`, `table`, `columns`, `page_break`). Khuôn câu `{bien}`, trong lặp/bảng `{ds.truong}`, `{{`/`}}` là ngoặc. `Validate` kiểm mọi tham chiếu trong phạm vi lúc đăng ký (mẫu sai → lỗi khởi động). Dữ liệu chỉ là chuỗi: bên dùng định dạng số thập phân chính xác (không qua số thực) và ngày.
3. **Bộ ghi .docx tự viết** `internal/shared/docx` bằng `archive/zip` + WordprocessingML, không thêm phụ thuộc (RB-4 không đổi). **Tất định**: thứ tự phần cố định, mốc thời gian zip cố định (01/01/1980), phương thức Store (byte không phụ thuộc phiên bản `compress/flate`), không docProps ngày tạo, không rsid. Times New Roman 13, A4, lề NĐ 30/2020. `Extract` đọc lại chữ/bảng cho kiểm thử khứ hồi.
4. **Ba sổ chỉ ghi thêm** (`shared.apply_ledger`):
   - `template_version`: phiên bản mẫu ghi sổ lần đầu dùng trong tenant (định nghĩa JSON + SHA-256). Cùng khóa + phiên bản mà khác băm → `export.template_drift` (đổi mẫu phải tăng phiên bản).
   - `export_run`: byte tệp + `body_hash` (CHECK `= encode(sha256(body),'hex')` ở DB), băm mẫu, băm dữ liệu vào, số văn bản, `subject` (`<bên dùng>:<mã đối tượng>`). Trigger buộc trỏ tới phiên bản mẫu có thật cùng băm. Tệp lưu trong DB như Excel W11.66 (≤ 32 MB), không qua kho đối tượng.
   - `doc_number`: số `{n}/<ký hiệu>` theo năm (giờ Việt Nam lúc xuất). `n` = `seq` của luồng `docno:<ký hiệu>:<năm>` do `shared.fn_chain` gán dưới khóa đầu luồng → tuần tự, không trùng; cấp trong cùng giao dịch với lần xuất nên giao dịch hủy thì số không bị tiêu (không hở). Mỗi subject một số cho mỗi ký hiệu (`idem_key`), các lần xuất sau dùng lại.
5. **Port** `Exporter` (`Export`, `Run`, `Runs`) và `Access`; bên dùng tự kiểm quyền nghiệp vụ rồi gọi `Export`. Yêu cầu có thể xin `Numbering` (cấp số khi biến trống) và `DateLine` ("<địa danh>, ngày dd tháng mm năm yyyy" khi biến trống). API `/v1/exports/*` chỉ đọc cho màn quản trị (quyền do gốc lắp ráp cấu hình — KN: Admin; không ngữ cảnh KN: `platform_admin`).
6. **Bên dùng đầu: KHTC Mẫu 1–5.** Word dựng từ nội dung đóng băng của **một bản Excel đã xuất** (cùng lời văn, số, bộ lọc) — không dựng lại từ dữ liệu sống. Subject `khtc_report:<mã kỳ>` (một số mỗi báo cáo); bản Tạm tính không xin số. Tải lại đi qua điểm kiểm soát xuất (ADR-0024) như Excel.

## Hệ quả

- Các đường xuất bất đồng bộ cũ (`pf/reports`, `pf/mrv`, `pf/apiclient`) và tải đồng bộ (b) của W6.6 **chưa chuyển** sang `pf/export`; điều kiện (ii)/(iii) của W6.6 vẫn để gói sau. Bảng W6.6 trong `modules.md` ghi lại.
- Đổi bộ ghi hoặc mẫu làm đổi byte: kiểm thử golden khóa băm; tệp đã lưu không bị dựng lại. Phiên bản mẫu cũ phải giữ trong mã nếu cần dựng lại bản cũ (hiện chỉ tải byte đã lưu).
- Tệp lớn nằm trong bảng phân vùng theo `recorded_at`; đủ cho văn bản (KB–vài trăm KB). Nếu sau này cần PDF/tệp lớn: chuyển `body` sang kho đối tượng (giữ `body_hash`) — migration mới.
- Q-062 còn mở: dùng mẫu .docx gốc của Sở làm khung thì cần thêm khối "khung tệp gốc" (đọc tệp mẫu, thay biến) — mở rộng `doctpl` + phiên bản mẫu mới, không đổi sổ.
- Bên dùng sau (CNTS BM-11, HCTH MS05/06, KNQG): đăng ký `TemplateRegistration` ở `cmd/api-platform/assembly/export.go`, gọi `Exporter.Export` với subject riêng và ký hiệu số riêng.
