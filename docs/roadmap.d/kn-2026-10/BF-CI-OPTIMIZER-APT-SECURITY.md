# BF-CI-OPTIMIZER-APT-SECURITY

[#235](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/235): optimizer dùng chung base Python3.12 slim-trixie đã nâng apt/libapt-pkg7.0 và kiểm phiên bản tối thiểu3.0.4 cho cả build/runtime, xử lý CVE-2026-109518. Giữ scanner/workflow/baseline và hợp đồng optimizer; kết quả CI đúng SHA, review chéo và gộp được ghi tại issue. Đây là phụ thuộc mở chặn CI #224, không phải rollout Forms hoặc thay đổi nghiệp vụ KN.
