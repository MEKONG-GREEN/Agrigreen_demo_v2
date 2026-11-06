## TEST-CNTS-KNQG-ALLOC — byte guard cho đầu vào số

### Phạm vi và hợp đồng

Đây là sửa kỹ thuật đo allocation, không đổi FN/UR/BR nghiệp vụ, API, DB hay UI của CNTS. Giữ parser đồng bộ hiện hành và giới hạn 22 ký tự / 18 chữ số phần nguyên / 4 số lẻ. Giữ đầy đủ syntax/fuzz cases; không dựng decimal cho đầu vào bị từ chối.

Byte gate giữ nguyên: cell 4 KiB, Normalize 64 KiB, Warnings 16 KiB. Guard Normalize đo đầu vào số mũ và chuỗi 23 chữ số; cell/Warn có thêm chuỗi 100.000 chữ số. Không áp budget 64 KiB cho toàn bộ Normalize của nội dung 200 KB: thao tác này còn json.Marshal toàn nội dung để kiểm MaxContent, nên chi phí tuyến tính theo kích thước nội dung là thật.

### Nguyên nhân đã chứng minh và điều chưa kết luận

Helper cũ lấy chênh lệch TotalAlloc của cả tiến trình nhưng chú thích là allocation của callback. Đối chứng cố định cho một goroutine bên cạnh cấp phát 1 MiB ngay trong cửa sổ đã chứng minh counter cũ tính cả allocation ngoài callback. Tiến trình go test của package khác không dùng chung heap này.

Probe trước sửa tại a19df98d545b49226abfc3be289546531aeb0531 chạy Linux Go1.26.10, plain/race × GOMAXPROCS default4/2: mỗi job 288 mẫu raw + 64 mẫu profile, mọi mẫu được lưu rồi mới báo cáo. Tất cả job và domain guard cũ PASS. Không tái hiện 5576 byte đã gặp ở CI cũ; chưa xác định nguyên nhân chính xác của lần đó. Profile GC worker có cả hoạt động setup/flush ngoài cửa sổ, không được dùng để quy kết hay trừ allocation lịch sử.

### Phép đo thay thế

TestMain đặt MemProfileRate=1 một lần trước callback để mọi block được ghi. Helper đo chênh lệch cumulative allocated bytes từ public runtime/pprof allocs profile, gồm cả block đã giải phóng. Hai GC trước mỗi snapshot đồng bộ profile ngoài callback. Full allocation stack được gán cho một sentinel noinline bao quanh callback và mọi lời gọi đồng bộ bên trong; không chọn riêng tên hàm parser, không lấy min, không trừ baseline/noise, không đổi sang số allocation.

Dùng full stack của pprof thay MemProfileRecord vì record công khai giới hạn 32 frame, không đủ cho đường cold reflection của Normalize. Counter lùi, record hỏng, rate khác1 hoặc stack mới không đủ để phân loại đều FAIL; không bỏ qua trường hợp không rõ. Nhận diện các root goroutine/g0 theo đúng Go1.26.10 runtime/stubs.go và asm_amd64.s (goexit/main/systemstack/mcall/morestack/mstart/rt0_go); mcall và morestack là ranh giới stack g0, không phải stack ứng dụng bị cắt. Linux cold đã bắt lỗi phân loại morestack với1152byte và13PC; sửa theo source runtime, không thêm ngoại lệ byte. Hợp đồng này áp cho các entry đồng bộ hiện hành; nếu đổi parser sang worker bất đồng bộ phải thiết kế lại attribution, không coi worker đó là callback đồng bộ.

Đối chứng phải đếm đúng 1 MiB cấp phát rồi giải phóng ở lần đầu/lặp lại, callback rỗng=0; goroutine bên cạnh được counter process-wide nhìn thấy >=1 MiB nhưng callback=0. Không warm-up parser, không ép GOMAXPROCS1. Input được tạo ngoài callback vì payload đã tồn tại khi vào parser.

### Kiểm chứng và cách chạy

Workflow riêng .github/workflows/cnts-allocation207.yml chạy Go1.26.10 Linux/compiler, plain/race × default/2. Mỗi entry có process riêng để lần đầu gọi entry thực sự cold và gọi lặp lại trong cùng process; full domain chạy thêm. Canary chèn cấp phát 1 MiB trước scan cú pháp phải làm FAIL cả number/int/Normalize/Warnings do byte budget; workflow xác nhận đúng nguyên nhân, khôi phục source chính xác rồi yêu cầu full domain PASS và worktree sạch. Không thay workflow CI chính.

`go test -v -count=1 -timeout 5m ./internal/kn/extension/cntsknqg/domain`

`go test -race -v -count=1 -timeout 5m ./internal/kn/extension/cntsknqg/domain`

`go test -v -count=1 -tags allocationdiagnostics207 ./internal/kn/extension/cntsknqg/domain -run '^TestAllocationDiagnostics207$'`

Để kiểm cold, chạy mỗi subtest /number, /int, /Normalize, /Warnings bằng một lệnh go test -run riêng như workflow. Không cần dependency, migration hoặc lệnh start API/web mới. Bản sửa tại cc7204d08d67c669312ecb4bd8e7360df7a30a59 đã đạt đủ4job Linux plain/race × default4/2, cold/warm, canary1MiB và exact restore (run42249211057). Cổng tĩnh đủ/lint0; module chạy trên pg-ephemeral23PASS và migration gates8PASS, đều0SKIP. CI đầy đủ đúng head cuối vẫn là cổng bắt buộc trước tự review/gộp.

Nguồn kỹ thuật: [Go1.26.10 MemProfileRate/GC lag](https://github.com/golang/go/blob/go1.26.10/src/runtime/mprof.go), [public allocs profile và full stack output](https://github.com/golang/go/blob/go1.26.10/src/runtime/pprof/pprof.go). Không dùng tài liệu nghiệp vụ cũ làm authority.
