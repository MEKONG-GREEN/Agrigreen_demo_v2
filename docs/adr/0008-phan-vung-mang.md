# ADR-0008: Phân vùng mạng và bảng luồng

**Trạng thái:** Chấp nhận — hiện thực từ G4 · **Ngày:** 27/09/2026

## Quyết định
Theo bảng ở mục 17: Biên → DMZ → Ứng dụng → Dữ liệu / Định danh; Quản trị qua VPN + MFA; Biên OT chỉ tới `ingest`.
- Kubernetes: mỗi vùng một namespace, NetworkPolicy mặc định từ chối (R-81).
- Compose ở ĐTK 1 **chỉ cho Local/Dev**. Mọi cổng chỉ bind `127.0.0.1`.
- Mức DB đã cưỡng chế từ G0: mỗi database chỉ cấp CONNECT cho đúng các vai của nó. `party_app` không vào được `ag_vault` (R-04).
