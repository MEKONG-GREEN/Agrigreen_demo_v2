---
name: Gói việc
about: Một gói việc (work package) cho người hoặc AI agent — xem AGENTS.md §3
title: "[PKG] "
labels: ["st:ready"]
assignees: []
---

<!-- Tiêu đề: `[<MÃ-GÓI>] <mô tả ngắn>`, mã gói viết hoa, vd `[P0-POOL-KN] …`. Nhãn cần thêm sau khi tạo:
     lane:kn|shared · phong:tt|cnts|khtc|hcth|tttt|shared · size:S|M|L|XL · kn, g1 (nếu thuộc phạm vi).
     agent:claude|codex do `ticket.ps1 claim` gắn. -->

## Mục tiêu

<!-- Vấn đề / yêu cầu, nguồn (BRD, báo cáo, ticket cha). -->

## Làn

- Làn: `kn` / `shared`
- Ngoại lệ làn (tệp ngoài làn sẽ sửa, ai review):

## Phạm vi

<!-- Tệp/module dự kiến đụng. Ngoài phạm vi. Phụ thuộc (#issue). -->

## Tiêu chí nghiệm thu

- [ ] …
- [ ] Test tái hiện đỏ trên dev trước khi sửa (gói sửa lỗi)
- [ ] Test DB `--- PASS` (không SKIP); cổng ở AGENTS.md §8 xanh
- [ ] Tài liệu gói + dòng roadmap

## Tham chiếu

<!-- ADR, đặc tả, ma trận UAT. Không dán bí mật hay đường dẫn riêng của máy. -->

Review chéo bắt buộc ("Review OK" của bên còn lại theo SHA đầu PR) trước khi gộp. Chỉ gộp vào `dev`.
