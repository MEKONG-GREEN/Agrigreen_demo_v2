# ADR-0016: Giấy phép phụ thuộc Python của optimizer

**Trạng thái:** Đề xuất · Bổ sung bởi [ADR-0036](0036-engine-ba-che-do-vung-noi-bo-optcore.md) (đề xuất: áp cho `lib/optcore` và engine nội bộ) · **Ngày:** 29/09/2026 · **Thành phần:** `optimizer/` (Python 3.12, pf/simulation), `deploy/policies/allowed-licenses.txt`

## Bối cảnh

RB-4 yêu cầu mọi phụ thuộc phân phối cùng sản phẩm có giấy phép thuộc `deploy/policies/allowed-licenses.txt`. Danh sách hiện có: MIT, BSD-2-Clause, BSD-3-Clause, Apache-2.0, ISC, MPL-2.0, PostgreSQL.

Tệp này **không có phần đầu hay quy trình bổ sung**. CI đọc thẳng từng dòng của nó thành cờ `--allowed_licenses` cho `go-licenses` (job `security`), nên không thể thêm dòng chú thích vào tệp. Nó cũng là chính sách chung cho cả phụ thuộc Go: thêm một giấy phép là quyết định kiến trúc, không phải việc riêng của optimizer. ADR-0014 cũng đã xử lý OFL-1.1 theo cách này (đề xuất, người tích hợp bổ sung). Vì vậy việc thêm giấy phép đi qua ADR.

Optimizer là ngoại lệ ngôn ngữ duy nhất (mục 4, `docs/dev/g2-conventions.md`). Image `agrigreen/optimizer` cài `agrigreen-optimizer[milp]`. Phụ thuộc runtime, kiểm bằng `pip-licenses` ngày 29/09/2026 trên Python 3.12:

| Gói | Phiên bản | Giấy phép (metadata) | Kéo vào bởi | Trong danh sách? |
|---|---|---|---|---|
| pydantic | 2.13 | MIT | trực tiếp | có |
| pydantic-core | 2.46 | MIT | pydantic | có |
| annotated-types | 0.8 | MIT | pydantic | có |
| typing-inspection | 0.4 | MIT | pydantic | có |
| **typing-extensions** | 4.16 | **PSF-2.0** | pydantic, pydantic-core (bắt buộc) | **không** |
| pika | 1.4 | BSD-3-Clause (classifier "BSD License") | trực tiếp: vòng nhận RabbitMQ | có |
| highspy | 1.15 | MIT | `[milp]`: chế độ THỬ NGHIỆM `select_hubs` | có |
| **numpy** | 2.5 | **BSD-3-Clause AND 0BSD AND MIT AND Zlib AND CC0-1.0** | highspy (bắt buộc) | **một phần**: thiếu 0BSD, Zlib, CC0-1.0 |

Wheel nhị phân của numpy còn **gói kèm thư viện đã biên dịch**. Các thư viện này ghi trong `numpy-*.dist-info/licenses/LICENSE.txt`, không nằm trong biểu thức giấy phép. Wheel Windows 2.5.2 đã kiểm có:

- OpenBLAS: BSD-3-Clause;
- LAPACK: BSD-3-Clause-Open-MPI;
- runtime GCC (libgfortran, liên kết tĩnh): GPL-3.0-or-later WITH GCC-exception-3.1.

Wheel manylinux, tức bản thực sự vào image, có thể kèm thêm `libquadmath` (LGPL-2.1-or-later, thư viện động trong `numpy.libs/`). CI job `optimizer` in các dòng `Name:`/`License:` của tệp đó ra log để người duyệt đối chiếu đúng bản Linux.

## Đề xuất

1. **Thêm `PSF-2.0` vào `allowed-licenses.txt`.** Đây là giấy phép dễ dãi được OSI phê duyệt: không copyleft, cho phép phân phối lại và dùng thương mại. Nó chỉ yêu cầu giữ thông báo bản quyền và ghi nhận thay đổi. typing-extensions là phụ thuộc bắt buộc của pydantic, nên không có cách tránh nếu vẫn kiểm hợp đồng ảnh chụp bằng pydantic (`extra="forbid"`, RB-7). Thêm PSF-2.0 cũng không mở rộng rủi ro cho phụ thuộc Go: Go gần như không có gói PSF-2.0.
2. **Thêm `0BSD`, `Zlib`, `CC0-1.0`** cho các đoạn mã gói kèm trong numpy. Cả ba đều dễ dãi và được OSI hoặc FSF chấp nhận, không kèm điều kiện nào ngoài giữ thông báo (0BSD và CC0-1.0 thậm chí không cần). CI web đã chấp nhận CC0-1.0 trong `--onlyAllow` của npm. Đề xuất ghi rõ phạm vi: "chỉ cho mã gói kèm trong gói đã được liệt kê", để không mặc nhiên mở cho mọi phụ thuộc.
3. **Thư viện nhị phân gói kèm trong wheel numpy.** Chấp nhận thành phần GPL-3.0-or-later WITH GCC-exception-3.1 (runtime GCC) và, nếu CI xác nhận, LGPL-2.1-or-later (`libquadmath`, liên kết động), với điều kiện:
   - Ngoại lệ GCC Runtime Library cho phép phân phối chương trình không-GPL dùng runtime này.
   - LGPL dùng qua liên kết động, không sửa thư viện.
   - Image giữ nguyên tệp `LICENSE.txt` của numpy. SBOM (job `images`, `anchore/sbom-action`) đã ghi lại các thành phần này.
   - **Không** thêm GPL hay LGPL vào `allowed-licenses.txt`. Ngoại lệ chỉ áp cho numpy, và chỉ nhận qua ADR này.
4. **Phương án thay thế cho mục 3**, nếu MKG không chấp nhận thành phần có ngoại lệ GCC: bỏ `[milp]` khỏi image production. Chế độ `select_hubs` vốn là THỬ NGHIỆM, nên bỏ nó thì highspy và numpy ra khỏi runtime. Chế độ `evaluate` không cần numpy. Khi đó chỉ cần mục 1.
5. Khi ADR được chấp nhận, bổ sung các dòng tương ứng vào `allowed-licenses.txt`. Đổi bước "Giấy phép phụ thuộc runtime" của job `optimizer` từ in thông tin sang `--allow-only` với danh sách đầy đủ, rồi bỏ dòng `pip-licenses … --format=markdown`.

## Hệ quả

- Tới khi ADR được chấp nhận, CI **in** giấy phép của typing-extensions và numpy, kèm các thư viện gói kèm của numpy, nhưng không chặn build. Các gói còn lại vẫn bị cổng `--allow-only` chặn như cũ: MIT cho pydantic và highspy, BSD-3-Clause cho pika.
- Thêm phụ thuộc Python mới vẫn phải qua `pip-licenses --allow-only` trong CI. ADR này không miễn kiểm.
- Vòng nhận RabbitMQ dùng **pika** (BSD-3-Clause, thuần Python, không có phụ thuộc), nên không phát sinh giấy phép mới.
