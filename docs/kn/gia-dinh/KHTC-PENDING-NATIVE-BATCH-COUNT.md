## KHTC-PENDING-NATIVE-BATCH-COUNT — native phiếu chờ rà soát (#293)

Supplier hạ tầng độc lập cho KHTC v2.3 FN-TI-02.01 P760/P768 và FN-TI-02.04 P894–904, BR-DL-02. Function chi tiết được ưu tiên: phiếu đã gửi, chưa rà soát, kỳ còn nhận đơn vị; lần gửi đạt giới hạn tự khóa nên không còn chờ rà soát. Không dùng nháp, bảng đã niêm phong, intent, bài giữ hoặc phiên bản superseded làm bằng chứng.

### Hợp đồng và phạm vi

`forms.Module.PendingSubmissions()` cung cấp port `PendingReader` mới; không đổi `PeriodControl`, `Entry`, `SubmissionFeed`, REST, menu hay UI hiện hữu. `UsePendingPolicy(profile, policy)` đăng ký owner lúc khởi tạo; nil, thiếu pool, joined transaction hoặc owner không chứng minh `Complete` đều unavailable. Owner của #294 phải nối reader quyền hiện hành thật sau khi supplier và #245 đã vào dev; #293 chưa bật consumer hay tự tạo quyền KN.

Mỗi request, kể cả continuation/retry, kiểm principal/MFA, resolve native actor và gọi owner policy **trước** đọc cursor hoặc mở native TX. Owner phải kiểm tài khoản, tenant, role, node, context đang chọn, read-only và thu hồi hiện hành; binding phải phiên bản hóa các sự kiện quyền liên quan. Owner phải đọc và kiểm toán đúng yêu cầu thật; không được bỏ audit hay kiểm quyền để giữ cursor sống. Selected-session authorization stamp chỉ băm projection bất biến, không phải OIDC sid/jti và không tự chứng minh chưa bị thu hồi.

Query và grant dùng tập selector canonical có giới hạn 200 phần tử; page từ 1 đến 200, cursor tối đa 2048 byte được chặn trước parse/owner/DB. Mỗi chiều grant dùng cờ All hoặc tập cụ thể; All kèm tập là không hợp lệ. Tập rỗng với Complete=true là quyền rỗng đã chứng minh, trả count=0; thiếu bằng chứng coverage không được biến thành 0. Scope owner chỉ thu hẹp native unit/parent/department/template policy. Với native filter opaque, owner phải đưa tập cụ thể đánh giá ngoài TX; không thể chiếu thành selector đầy đủ thì unavailable.

Một SQL statement dưới `db.ForModule`/module tenant TX tính projection mới nhất, kiểm toàn bộ candidate phù hợp trước page, đếm chính xác và trả tối đa limit+1. Count áp dụng đúng giao của tenant/profile, grant owner, quyền native và query; không phải số toàn KN. `CoverageComplete` nói count phủ đủ tập đó, không nói `Items` là toàn bộ. Duplicate unit/identity, thiếu state, status không biết hoặc evidence không hợp lệ làm unavailable. Không gọi Progress cho từng kỳ, không I/O owner trong native TX, không mutation nghiệp vụ, không migration/dependency mới.

### Nhất quán và giới hạn sẵn sàng

Count/page/watermark đọc cùng một MVCC statement. Snapshot chỉ trả SHA-256 của database, thời điểm server khởi động và `pg_current_snapshot()` của statement; raw visibility/metadata không đi qua port. Cursor gắn canonical query, principal/resolved scope, current owner binding, selected-session stamp, visibility epoch và khóa tiếp tục. Stable epoch cho phép phân trang ổn định. Mọi visibility đổi do writer ở database/cluster, kể cả transaction đã active trước trang đầu rồi commit mà không cấp XID mới, có thể làm continuation trả `forms.pending_changed`; phải tải lại từ đầu. Server restart cũng đổi token. Đây là restart bảo thủ, không giữ snapshot lịch sử. SHA-256 có xác suất va chạm lý thuyết; token là phát hiện thay đổi, không phải token cấp quyền.

Owner revalidation phải read-only. Nếu đường kiểm quyền/audit thật có ghi hoặc database có writer thường xuyên, continuation có thể liên tục yêu cầu restart; count/trang đầu vẫn đúng ở statement của nó. Không bỏ required audit/revalidation để né giới hạn này. Consumer phải hiển thị lỗi/restart rõ, không cộng các trang khác snapshot hoặc suy count từ page.

### Tệp → lớp → Function → kiểm chứng

| Tệp | Bước | FN/BR | Kiểm thử |
|---|---|---|---|
| `internal/platform/forms/port/pending.go`, `pending_test.go` | 1,4: port, bounds, scope/coverage/cursor contract | FN-TI-02.01 P768; FN-TI-02.04 | `TestPending293CanonicalOwnedSelectors`, `TestPending293IncompleteAndAmbiguousGrants` |
| `internal/platform/forms/store/pending.go` | 2: latest native projection, count/page/visibility cùng statement | FN-TI-02.01 P760/P768; BR-DL-02 | `TestPending293NativeCountBeyondPage`, `TestPending293NativeVisibilityCommitWithoutNewXID`, `TestPending293NativeInvalidCoverageAcrossCandidates` |
| `internal/platform/forms/app/service.go`, `pending.go` | 3: current owner, native intersection, fail closed, continuation | FN-TI-02.04 P894–904 | `TestPending293CurrentAuthorityBeforeCursorAndData`, `TestPending293ScopeIntersectionAndConnectionReuse`, `TestPending293PoolOneOwnerIOAndRLSCleanup`, `TestPending293QueryAndCursorTampering` |
| `internal/platform/forms/module_pending.go`, `pending_test.go` | 4: additive capability, dormant registration | HẠ_TẦNG cho hai FN trên | `TestPending293PublicRegistrationFailsClosed` |
| `internal/platform/forms/app/pending*_test.go` | 3,4: native regressions | FN-TI-02.04 P900/P901/P904 | `TestPending293NativeLifecycleAndLockedLimit`, `TestPending293NativeMutationInvalidatesContinuation`, `TestPending293BoundsBeforeOwnerOrDB`, `TestPending293NativeBoundaryRejectsInvalidEvidence` |
| `internal/platform/forms/module.yaml` | 1,4: metadata sinh cho public port | HẠ_TẦNG cho FN-TI-02.01/02.04 | `modgen -check`, `TestPending293PublicRegistrationFailsClosed` |
| `docs/errors/pf-forms.md` | 4: mã unavailable/changed cho consumer | FN-TI-02.01/02.04 | `TestPending293CurrentAuthorityBeforeCursorAndData`, `TestPending293QueryAndCursorTampering` |
| `docs/kn/gia-dinh/KHTC-PENDING-NATIVE-BATCH-COUNT.md`, `docs/roadmap.d/kn-2026-10/KHTC-PENDING-NATIVE-BATCH-COUNT.md`, `docs/uat/khtc-v23-pending-native293-acceptance.md` | 1–5: phạm vi, mapping và lệnh kiểm; không có UI rollout | FN-TI-02.01/02.04, BR-DL-02 | `docscheck`; native membership trong packet |

Không có bước 5 UI trong supplier này. Baseline chưa có `docs/kn/brd-surface.yaml` (#262 chưa vào dev); port nội bộ không thêm bề mặt route/menu. Đối chiếu lại inventory khi tích hợp foundation thật, không chép source unmerged.

### Chạy và kiểm

Đặt `GOTOOLCHAIN=go1.26.10`, `GOTMPDIR=<gotmp>`, `TEMP=TMP=<private-temp>`, `GOMAXPROCS=2`; xác nhận `go version`. Chạy `scripts/pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/platform/forms/... -run Pending293`; yêu cầu native PASS và 0 SKIP. Chạy thêm mọi direct consumers khai báo trong packet, migrations và cổng CONTEXT §3.3. Không có migrate mới. Windows thiếu CGO nên `-race` toàn Linux CI trên SHA tích hợp vẫn bắt buộc; local packet không thay cho whole CI/UAT/toàn FN.
