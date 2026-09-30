# PKI nội bộ production — CA dịch vụ (mTLS) và CA thiết bị (G4)

Hiện thực G4 cho ADR-0019 ("CA nội bộ production, PKI trong lãnh thổ") và mTLS dịch vụ (mục 11, 18). Quyết định công cụ: [ADR-0021](../adr/0021-ha-tang-kubernetes-g4.md). Nghi thức CA thiết bị, cấp/thu hồi chứng thư thiết bị: [device-pki.md](device-pki.md) — tài liệu đó vẫn là nguồn cho quy trình. Tài liệu này mô tả phần chạy trên Kubernetes. Manifest: `deploy/k8s/charts/agrigreen/templates/pki.yaml`, giá trị `pki.*`.

## 1. Thiết kế

```
            CA gốc DỊCH VỤ (ngoại tuyến, HSM, két MKG)      CA gốc THIẾT BỊ (ngoại tuyến, HSM, két MKG)
                         │ ký 3–5 năm                                  │ ký 3–5 năm
            CA trung gian dịch vụ                           CA trung gian thiết bị
            (kho bí mật trong lãnh thổ →                    (khóa trong HSM, PKCS#11 —
             Secret cert-manager/agrigreen-service-ca)       step-ca ở agrigreen-pki)
                         │ cert-manager ClusterIssuer                  │ người vận hành ký CSR (VPN + MFA)
     ┌───────────────┬───┴─────────────┬──────────────┐                │
 api-platform    api-platform       optimizer     identity-vault   chứng thư client thiết bị
 Internal 8084   → vault (client)   (client)      8090 (server)    CN = SAN = device_id, EKU clientAuth
 (server)                                         ingest-ot 8443 (server, thiết bị xác thực máy chủ)
```

| | CA dịch vụ | CA thiết bị |
|---|---|---|
| Công cụ | cert-manager `ClusterIssuer agrigreen-service-ca` (kiểu CA) | step-ca `agrigreen-pki/step-ca`, khóa trong HSM (`pki.deviceCA.hsm`) |
| Ai ký | cert-manager, tự động theo `Certificate` trong namespace vùng | người vận hành CA thiết bị. **Không** có provisioner tự phục vụ cho thiết bị |
| Chứng thư lá | ECDSA P-256, 90 ngày, gia hạn trước 30 ngày, khóa mới mỗi lần (`rotationPolicy: Always`) | ≤ 825 ngày, không gia hạn cùng khóa (`disableRenewal`). Kỳ mới = khóa mới (ADR-0019 mục 5) |
| Phân phối CA công khai | trust-manager `Bundle agrigreen-service-ca` → ConfigMap `agrigreen-service-ca` ở namespace có nhãn `agrigreen.vn/trust-service-ca=true` (pf, mkg, vault) | trust-manager `Bundle agrigreen-device-ca` → ConfigMap `agrigreen-device-ca` **chỉ** ở `agrigreen-mkg` (`agrigreen.vn/trust-device-ca=true`) |
| Người tin | `api-platform` (client CA của Internal), `identity-vault` (client CA), `optimizer` và `api-platform` (xác thực máy chủ), agent biên (xác thực máy chủ ingest) | chỉ `ingest` (`AG_INGEST_CLIENT_CA`). Ingest từ chối khởi động nếu bó này ký chứng thư máy chủ của nó (`AG_INGEST_REQUIRE_DEVICE_CA=1`) |

Dịch vụ tin **bó** CA (ConfigMap của trust-manager), không tin `ca.crt` trong Secret của chính nó. Nhờ đó thay CA không gián đoạn (mục 4). Dịch vụ Go đọc chứng thư lúc khởi động. Stakater Reloader (`reloader.stakater.com/auto`) khởi động lại Pod khi Secret/ConfigMap đổi. PDB giữ số bản tối thiểu.

Mạng ([ADR-0008](../adr/0008-phan-vung-mang.md)):
- step-ca chỉ nhận kết nối từ dải VPN quản trị (F59) và chỉ đi ra tới HSM (F85);
- không Pod ứng dụng nào tới được step-ca: `check_flows.py` kiểm `api-platform` và `ingest` → step-ca bị chặn.

## 2. Dựng lần đầu (một lần mỗi môi trường, hai người)

1. **Nghi thức CA gốc** cho cả hai CA: máy không nối mạng, HSM/token FIPS 140-2 mức ≥ 2, hai người, biên bản ([device-pki.md](device-pki.md) mục 3).
2. **CA trung gian dịch vụ:**
   - CA gốc dịch vụ ký, `pathlen:0`, `nameConstraints` DNS `svc.cluster.local` + miền nội bộ;
   - đưa `tls.crt` (chuỗi trung gian + gốc) và `tls.key` vào kho bí mật trong lãnh thổ, khóa `<remotePrefix>/pki/service-ca`. External Secrets tạo `cert-manager/agrigreen-service-ca`;
   - quyền đọc khóa này ở kho: chỉ tài khoản đồng bộ của External Secrets.
3. **CA trung gian thiết bị trong HSM:**
   - sinh khóa **trong** HSM, CA gốc thiết bị ký;
   - tạo ConfigMap `agrigreen-pki/step-ca-certs` (`root_ca.crt`, `intermediate_ca.crt` — công khai);
   - đặt PIN HSM vào kho (`pki/device-ca`, thuộc tính `pin`);
   - điền `pki.deviceCA.hsm.kmsUri` / `keyUri` theo HSM đã chọn — **chờ chọn HSM**.
4. **Cài release `pki`** (k8s-deploy.md mục 3). Rồi:
   ```powershell
   kubectl -n agrigreen-pki logs deploy/step-ca          # "Serving HTTPS on :9000"
   # Quản trị viên step-ca (lưu trong DB, authority.enableAdmin): tạo provisioner JWK cho người vận hành,
   # gắn mẫu chứng thư lá thiết bị (/home/step/config/device-leaf.tpl: EKU clientAuth, CN = SAN).
   step ca provisioner add device-operators --type JWK --create --x509-template /home/step/config/device-leaf.tpl `
     --x509-max-dur 19800h --x509-default-dur 8760h --admin-subject <người vận hành> --ca-url https://step-ca.agrigreen-pki.svc:9000
   ```
5. **Bó CA thiết bị cho ingest.** Đặt PEM công khai (trung gian + gốc thiết bị) vào ConfigMap `agrigreen-device-ca-source` (khóa `ca.crt`) ở namespace tin cậy của trust-manager (mặc định `cert-manager`). trust-manager phát nó tới `agrigreen-mkg/agrigreen-device-ca`.
6. **Kiểm:**
   ```powershell
   kubectl get certificate -A                              # READY=True: api-platform-internal-tls, api-platform-vault-client-tls, optimizer-client-tls, identity-vault-tls, ingest-server-tls
   kubectl -n agrigreen-mkg get configmap agrigreen-device-ca agrigreen-service-ca
   kubectl -n agrigreen-mkg logs deploy/ingest | Select-String "CA"   # không có lỗi tách CA
   ```

## 3. Cấp chứng thư cho thiết bị cân (mỗi kỳ)

Theo [device-pki.md](device-pki.md) mục 4. Trên Kubernetes:
1. Thiết bị: `edge-agent enroll -epoch <N>`. Khóa và CSR sinh trên máy; lấy `csr_sha256`.
2. Người vận hành (qua VPN quản trị + MFA) đối chiếu `csr_sha256` ngoài băng, rồi ký:
   ```powershell
   step ca sign device.csr device.crt --provisioner device-operators --not-after 8760h --ca-url https://<step-ca qua VPN>:9000 --root root_ca.crt
   ```
   Ghi sổ cấp phát: serial, `device_id`, kỳ, người ký, người đối chiếu.
3. Đề xuất + duyệt kỳ hai người trong sổ thiết bị ([ingest.md](ingest.md) mục 3). Chỉ sau bước này ingest mới nhận lô.

Không có đường cấp tự động: đúng thiết kế, vì lộ CA thiết bị vẫn chưa đủ để ghi số đo (cần kỳ đã duyệt khớp khóa ký).

## 4. Xoay vòng

**Chứng thư lá dịch vụ.** cert-manager tự gia hạn trước 30 ngày, Reloader khởi động lại Pod. Theo dõi:
- `certmanager_certificate_expiration_timestamp_seconds` (ServiceMonitor của cert-manager);
- cảnh báo khi < 14 ngày.

**CA trung gian dịch vụ** (trước hạn 6 tháng, hoặc khi nghi lộ):
1. CA gốc ký CA trung gian mới (nghi thức hai người).
2. Đặt PEM CA **cũ** vào ConfigMap `agrigreen-service-ca-extra` (khóa `ca.crt`, namespace tin cậy). Bật `pki.serviceCAExtraSource: true`, `helm upgrade` release `pki`. Bó tin cậy giờ gồm cả hai CA.
3. Thay `pki/service-ca` ở kho bằng CA mới. External Secrets cập nhật Secret, cert-manager cấp lại mọi `Certificate` (`cmctl renew --all -A`), Reloader khởi động lại Pod.
4. Khi mọi chứng thư lá đã do CA mới ký (`kubectl get certificate -A`, kiểm `issuer` bằng `openssl x509 -noout -issuer`): tắt `serviceCAExtraSource`, xóa ConfigMap phụ, thu hồi CA cũ ở CA gốc.
5. Agent biên xác thực máy chủ ingest bằng CA dịch vụ. Phát CA mới tới hub **trước** bước 3 (cập nhật agent có chữ ký, [edge-agent.md](edge-agent.md)).

**CA trung gian thiết bị:** theo [device-pki.md](device-pki.md) mục 5. Trên Kubernetes, "gộp hai CA vào `AG_INGEST_CLIENT_CA`" nghĩa là đặt cả hai PEM vào `agrigreen-device-ca-source`. trust-manager phát tới ingest, Reloader khởi động lại ingest.

**Nghi lộ CA** (P1, mục 23):
- dừng cấp mới: `kubectl -n agrigreen-pki scale deploy/step-ca --replicas=0` cho CA thiết bị; xóa Secret `agrigreen-service-ca` và dựng lại cho CA dịch vụ;
- rồi làm theo mục trên với CA gốc mới.

Số đo đã nhận giữ nguyên (RB-1). Chữ ký nguồn Ed25519 vẫn chứng minh nguồn gốc lô.

## 5. Kiểm định kỳ (hằng quý, cùng [device-pki.md](device-pki.md) mục 6)

- [ ] `kubectl -n agrigreen-mkg get cm agrigreen-device-ca -o jsonpath='{.data.ca\.crt}'` chỉ chứa CA thiết bị; `openssl verify -CAfile <nó> ingest-server.crt` **thất bại**.
- [ ] Không `Certificate` nào dùng issuer khác `agrigreen-service-ca`: `kubectl get certificate -A -o jsonpath='{..issuerRef.name}'`.
- [ ] `step-ca` chỉ 1 bản, PVC `step-ca-db` có sao lưu; `kubectl -n agrigreen-pki get netpol` gồm `default-deny-all`, `flow-f59-in`, `flow-f85-out`.
- [ ] Khóa CA gốc vẫn ngoại tuyến (biên bản kiểm két). PIN HSM chỉ ở kho bí mật trong lãnh thổ.
