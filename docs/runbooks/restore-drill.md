# Diễn tập phục hồi

Mục 22: **diễn tập phục hồi hàng quý trên môi trường sạch**; sau phục hồi kiểm số dòng, **băm bộ tham số**, **`verify_chain`**, **chạy lại một mô phỏng mẫu**, checksum kho đối tượng. Công cụ: [cmd/drill](../../cmd/drill). Sao lưu: [backup.md](backup.md).

## Tự động trong CI

Workflow [`.github/workflows/restore-drill.yml`](../../.github/workflows/restore-drill.yml): hằng tuần (02:00 thứ Hai giờ VN) và chạy tay (`workflow_dispatch`). **Không** chạy trên PR.

1. `gen-dev-secrets --force`, tạo 2 volume external sao lưu, `compose config` với mọi profile.
2. Dựng stack + profile `backup`; `pgbackrest stanza-create` + `check`.
3. Sinh dữ liệu tổng hợp `seed-synthetic -scale small`. Token lấy từ client dịch vụ tạo tạm bằng `kcadm` ([scripts/drill/seed-token.sh](../../scripts/drill/seed-token.sh); claim `amr` cứng chỉ có ở realm CI).
4. Dừng `api-platform`, `worker`, `outbox-publisher`, `identity-vault`, `web-ops` — ảnh chụp ổn định.
5. `migrate anchor` với `AG_ANCHOR_DATE` = hôm nay: neo mọi chuỗi vừa sinh và ký manifest vào kho chính.
6. `drill capture` → `db-manifest.json`.
7. `pgbackrest --type=full backup`; `objsync backup` (backup-writer) → manifest kiểm kê.
8. `down -v`: xóa pgdata, kho chính, mọi thứ trừ 2 volume sao lưu external.
9. `pgbackrest-restore` (kho gắn chỉ đọc) vào pgdata rỗng → dựng **chỉ hạ tầng** (postgres, seaweedfs, backup-s3) — không chạy ứng dụng để không có ghi sau phục hồi.
10. `objsync restore` (restore-reader) → `drill check -strict` → artifact `restore-drill-<run_id>` (`drill-report.json`, `db-manifest.json`, `objects-manifest.json`, `pgbackrest-info.txt`).

> Workflow **chưa chạy được trên máy dựng** (Windows, không Docker). Tệp đã kiểm cú pháp YAML; logic `drill`/`objsync` được kiểm bằng kiểm thử Go trên PostgreSQL thật (`TestDrillEndToEndAndDetectsCorruption`, `internal/shared/objsync`). Lần chạy đầu trên GitHub là nơi kiểm chứng phần Docker (pgBackRest, kcadm, SeaweedFS thứ hai).

## Các kiểm của `drill check`

| Kiểm | Theo cụm | Đạt khi | Phát hiện được |
|---|---|---|---|
| `row_counts` | pf, mkg, vault | số dòng mọi bảng (bảng cha phân vùng) bằng manifest | mất/thừa dòng, thiếu bảng |
| `param_set_hash` | pf | SHA-256 dạng chuẩn của `pf_masterdata.param_approved` (tenant, khóa, phiên bản, giá trị, đơn vị, hiệu lực, văn bản, bản bù, `row_hash`) bằng manifest | tham số đã duyệt bị sửa |
| `verify_chains` | mọi cụm | `shared.verify_chain` mọi chuỗi + đầu chuỗi khớp `chain_head` | sửa/xóa dòng sổ |
| `balance_recompute` | pf | tính lại (hàm thuần FN-09 của module mechanization) các dòng của ≤ 20 lần cân đối mới nhất từ đầu vào đã lưu — từng cột `required`, `required_whole`, `coverage_pct`, `gap`, `status` khớp; `result_count`, `status_counts` khớp | kết quả cân đối bị sửa; quy tắc đổi mà không nâng `rule_version` |
| `object_checksums` | — | đọc lại **nội dung** mọi đối tượng trong manifest kiểm kê, SHA-256 + kích thước khớp | đối tượng thiếu/hỏng |
| `anchor_signature` | mọi cụm | neo mới nhất trong `shared.chain_anchor` = ngày trong manifest DB; manifest `anchors/<cụm>/<ngày>.json` qua kiểm chữ ký Ed25519 bằng `AG_ANCHOR_PUBLIC_KEY`; từng đầu chuỗi đã neo khớp `chain_anchor` và `row_hash` của dòng `seq` trong bảng sổ | manifest giả/khóa khác, neo bị sửa, sổ bị viết lại cả chuỗi |

Báo cáo: JSON `ag.restore-drill-report.v1`, mỗi kiểm có `status` (`pass`/`fail`/`skip`), `detail`, `issues` (≤ 50 dòng; chỉ tên bảng, mã tham chiếu, băm — **không có dữ liệu cá nhân**). Mã thoát 0 đạt, 1 có kiểm hỏng, 2 sai cấu hình. `-strict`: kiểm bị bỏ qua (thiếu kho, thiếu khóa, chưa có lần cân đối) cũng là không đạt — dùng trong CI và diễn tập quý.

`-ignore-tables schema.bảng,…` chỉ dùng khi biết chắc có tiến trình ghi hợp lệ sau lúc chụp (ghi vào biên bản diễn tập).

## Diễn tập quý (thủ công, môi trường sạch)

1. Cấp một môi trường **mới** (cụm DB, kho đối tượng trống) cùng cấu hình production; không dùng lại môi trường đang chạy.
2. Phục hồi PostgreSQL từ kho pgBackRest bằng danh tính phục hồi (repo chỉ đọc), mục tiêu mới nhất hoặc `--type=time --target=…`.
3. `objsync restore -manifest <khóa hoặc latest>` với restore-reader.
4. `drill check -manifest <db-manifest của lần sao lưu> -strict -report báo-cáo.json` với `AG_ANCHOR_PUBLIC_KEY` lấy từ **kênh công bố riêng** (không suy từ khóa ký).
5. Ghi biên bản: thời gian phục hồi (RTO), điểm phục hồi (RPO), báo cáo JSON, ai thực hiện. Kiểm hỏng → xử lý như sự cố P1 ([verify-chains.md](verify-chains.md)).

`drill capture` phải chạy lúc sao lưu (production: ngay sau `pgbackrest backup`, trong cùng CronJob, lưu manifest vào kho sao lưu cạnh bản sao). Với sao lưu trong lúc hệ thống đang ghi, số dòng sau phục hồi đến thời điểm mục tiêu sẽ ≥ manifest; khi đó so theo bản chụp của đúng điểm phục hồi hoặc dùng `-ignore-tables` cho bảng đang ghi và ghi rõ trong biên bản.

## Biến môi trường của `drill`

| Biến | Ý nghĩa |
|---|---|
| `AG_PG_ADMIN_URL` | tài khoản đọc toàn cụm (superuser hoặc vai `BYPASSRLS` chỉ đọc); mọi truy vấn trong giao dịch READ ONLY |
| `AG_S3_*` / `AG_OBJSTORE_DIR` | kho chính đã phục hồi |
| `AG_BACKUP_S3_*` / `AG_BACKUP_OBJSTORE_DIR` | kho sao lưu, danh tính restore-reader (đọc manifest kiểm kê khi `-objects-manifest latest`) |
| `AG_ANCHOR_PUBLIC_KEY` | khóa công khai Ed25519 tin cậy, base64 32 byte |
| `AG_DB_SUFFIX` | hậu tố database (kiểm thử) |
