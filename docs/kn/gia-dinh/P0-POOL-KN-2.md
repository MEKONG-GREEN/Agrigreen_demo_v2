## P0-POOL-KN-2 — Gỡ các lời gọi port module khác trong transaction còn lại của KN (#139)

Tiếp #125 (P0-POOL-KN, xếp chồng nhánh `p0-pool-kn`). Gốc lắp ráp mở MỘT pool cho mọi module (ADR-0028); port của module
khác — kể cả cổng nội bộ KN như KHTCPrograms, ClosedCutoff — mở giao dịch RIÊNG trên pool đó. Thao tác KN gọi port bên trong
giao dịch của mình thì giữ một kết nối trong khi chờ kết nối thứ hai: N thao tác song song với N = cỡ pool treo cả API.

Cách rà: danh sách của #125 là rà tĩnh THEO TÊN. Gói này rà THEO LUỒNG GỌI (công cụ phân tích cú pháp tạm, không đưa vào repo:
thân có tham số `pgx.Tx` → lời gọi trường kiểu interface/func của gói khác, trực tiếp hoặc qua method cùng gói), rồi đọc cài đặt
port ở `internal/platform` / module KN khác và gốc lắp ráp (`cmd/api-platform/assembly/modules.go`) để xác nhận port có mở giao
dịch trên pool chung không. Port đọc ngữ cảnh phiên trong ctx (không DB) là báo nhầm.

Cơ chế sửa: `internal/kn/extension/txdeps` — tổng quát hóa `reportcycle/app/deps.go` cho mọi gói con của pf/extension (không
vòng import): đầu thao tác `ctx = txdeps.With(ctx)`; lời gọi port đi qua `txdeps.Get(ctx, khóa, nạp)`; `s.tx` của gói chạy
`txdeps.Run`. Ngoài giao dịch: nạp ngay, nhớ (cả lỗi). Trong giao dịch: thiếu → ghi nhận, `ErrNeed`; hết thân giao dịch mà còn
khóa thiếu (dù thân trả lỗi gì hay nuốt lỗi) → hủy, nạp NGOÀI giao dịch, chạy lại thân từ đầu (đọc lại, kiểm lại mọi thứ trong
giao dịch mới — kiểm phạm vi, khóa lạc quan, idempotency giữ nguyên chỗ cũ). Quá 5 lần → 503 `extension.dependency_unavailable`.
Lỗi port chỉ trả khi thân giao dịch thực sự cần giá trị đó (như trước). Port nhận ctx đã bỏ bộ nhớ (cài đặt port có thể là use
case KN tự chạy `Run`). pf/hrm, pf/price là module riêng (không import được gói của pf/extension): nạp trước giao dịch / tra sau
giao dịch tại chỗ.

| Gói / đường | Lời gọi trong giao dịch | Thật hay báo nhầm | Sửa thế nào | Test (pool chung 2 kết nối, hạn 10 s/lời gọi) |
|---|---|---|---|---|
| `cntsmodel` sửa CT/DA gắn CT/DA KHTC (`UpdateProject`) | `Programs.Program` (khtcapp.Ports — giao dịch riêng của KHTC) | Thật | txdeps (`checkProject`) | kn77 `TestP0PoolKN2_CNTSModelNoNestedTx` |
| `cntsmodel` thêm/sửa điểm (`CreateSite`, `UpdateSite`) | `Units.Get` (KN platformread), `Catalogs.ItemAt` (pf/masterdata EffectiveCatalogs), `Officers.XaScopes` (pf/accounts) | Thật | txdeps (`unit`, `checkSite`, `officers`) | như trên |
| `cntsmodel` sửa hộ đổi người theo dõi (`UpdateHousehold`) | `Officers.XaScopes` | Thật | txdeps | như trên |
| `cntsmodel` cấp/sửa/thu hồi quyền | `Units.Get`, `Catalogs.FacilitiesAt`, `Officers.XaScopes` (`checkGrant`) | Thật | txdeps | như trên (CreateGrant) |
| `cntsmodel` tạo dự án/hộ, danh sách | `callerProvince`, `checkProject`, `checkHousehold`, `Vault`, tên — đã ngoài giao dịch | Báo nhầm | — (memo txdeps) | như trên (CreateProject, List*) |
| `khtc` lưu Kế hoạch năm (`SaveVersion`) | `ClosedCutoff` (Kỳ KHTC), `EntryUsage` (phiếu KHTC) — giao dịch riêng KN trên cùng pool | Thật (rà theo tên bỏ sót) | txdeps | kn77 `TestP0PoolKN2_KHTCNoNestedTx` |
| `khtcperiod` tiến độ kỳ (`Get`/`project`), danh sách | `Control.Progress`, `Crosswalk.States`, `Departments`, `Params.At`, `Reports.HasFinal` — sau giao dịch đọc sổ | Báo nhầm | — | như trên (Progress, Periods) |
| `khtcperiod` hủy kỳ (`Cancel` qua `khtcreport.GuardCancel`) | `Get`, `Reports.HasFinal`, `Control.CancelPeriod` (cap/forms) trong giao dịch khóa | Thật — CÒN LẠI | Không sửa ở gói này: khóa tư vấn của giao dịch phải bao cả việc hủy kỳ cap/forms để không lệch với "Bản cuối" (trigger `khtc_report_final_cancel`). Đường Admin hiếm. Hướng sửa: cap/forms `PeriodControl.CancelPeriod` nhận `db.JoinTx` như `Freezer` (làn shared) | — |
| `cntsalert` job nhắc nộp + trễ liên tiếp (`Remind`), quá hạn xử lý (`EscalateOverdue`) | `Units.Get` (`provinceOf`), `Dir.Officers`, `Dir.FormOwners` (pf/accounts) | Thật | txdeps; đặt lại bộ đếm ở đầu thân giao dịch | `cntsalert/handler` `TestP0PoolKN2_CNTSAlertJobNoNestedTx` |
| `cntsalert` consumer `HandleEvent`, `extension/app` consumer `HandleGpsLog` | `Feed.*`, `Params.At`, `Units.Get`, `Dir.*`; `GpsLog.RecordFix` (pf/geo) — trong giao dịch inbox | Thật — CÒN LẠI | Không sửa được trong làn kn: `inbox.Handler` luôn chạy trong giao dịch inbox (`internal/shared/inbox`), không có móc trước giao dịch. Worker dùng pool riêng; hạn mỗi thông điệp `DefaultHandlerTimeout`. Hướng sửa (làn shared): `inbox.Process` nhận bước chuẩn bị ngoài giao dịch, hoặc port nhận `db.JoinTx` | — |
| `cntsalert` cấu hình | `config.go` cấu hình/đọc — không gọi port trong giao dịch | Báo nhầm | — | — |
| `cntscal/vaccination` | `Catalogs.ListAt` ngoài giao dịch; trong giao dịch chỉ `k.Scope.Allows` (ctx) | Báo nhầm | — | — |
| `extension/app` directives, escalation, reports, roster, schedule, sync, tasks | `Scope.Scope`, `Units.Get`, `Params.At`, `Orgs.Get`, `ContactNames`, `KNContext` — đều trước/sau giao dịch (rà theo tên trùng tên hàm store) | Báo nhầm | — | `extension/handler` `TestP0PoolKN2_ExtensionPathsNoNestedTx` (chỉ đạo, xác nhận, nhiệm vụ, lịch công tác, danh bạ trực, job leo thang) — đạt cả trước sửa |
| `hrm` `Access.Session` | Đọc ngữ cảnh phiên trong ctx (`extension.HRMSession`) | Báo nhầm | — | — |
| `hrm` tạo hồ sơ, thêm/hủy quá trình, tải lên/sửa tài liệu | `Rec.Units.InternalUnits` (pf/accounts) trong `checkEvents`, `checkDocRefs` | Thật (rà theo tên bỏ sót) | `preloadUnits`: nạp trước giao dịch theo đơn vị của phiên; lỗi chỉ trả khi cần | `hrm/handler` `TestP0PoolKN2_HRMRecordsNoNestedTx` |
| `price` Biến động giá theo tuần (`Movement`) | `Weeks.WeekOf` (pf/extension ReportWeekOf → `Params.At`) | Thật (ngoài danh sách) | Kỳ xu hướng tính trước giao dịch | `price/handler` `TestP0PoolKN2_PriceMovementWeekNoNestedTx` |
| `price` duyệt bảng giá (`ApproveBoard`), khóa/mở/hủy (`busy`) | `Weeks.WeekOf`; `Officers.Officers` (pf/accounts, tên người giữ khóa) | Thật (ngoài danh sách) | Tuần tra trước giao dịch; "Đang duyệt bởi …" trả lỗi kiểu `boardBusy` trong giao dịch, tên tra SAU giao dịch | Chưa có test song song riêng (cần dựng cả luồng phiếu → khóa); test hiện có `price/handler` |

Bằng chứng (PostgreSQL thật, cụm riêng `scripts/pg-ephemeral.ps1`, pool app_platform 2 kết nối):

| Test | Trên `p0-pool-kn` (ed0f603f) | Sau sửa |
|---|---|---|
| kn77 `TestP0PoolKN2_CNTSModelNoNestedTx` (35 lời gọi) | 31/35 treo tới hết hạn 10 s | đạt |
| kn77 `TestP0PoolKN2_KHTCNoNestedTx` (18) | 18/18 treo | đạt |
| `TestP0PoolKN2_CNTSAlertJobNoNestedTx` (5 tenant) | 5/5 treo | đạt |
| `TestP0PoolKN2_HRMRecordsNoNestedTx` (6) | 6/6 treo | đạt |
| `TestP0PoolKN2_PriceMovementWeekNoNestedTx` (6) | 6/6 treo | đạt |
| `TestP0PoolKN2_ExtensionPathsNoNestedTx` (33) | đạt (báo nhầm) | đạt |

Port giả của module khác trong các test MỞ GIAO DỊCH NGẮN trên pool chung (như cài đặt thật); port nội bộ KN (KHTCPrograms,
ClosedCutoff, EntryUsage, cap/forms của KHTC) là thật.
