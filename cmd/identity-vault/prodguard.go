package main

import (
	"crypto/tls"
	"errors"
	"log/slog"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/residency"
)

// Ngoại lệ chấp nhận rủi ro TƯỜNG MINH ở production (giá trị "1"); mỗi ngoại lệ được ghi cảnh báo lúc khởi động.
const (
	// AllowPlaintextEnv cho phép chạy không mTLS (vd VPS một máy: kho chỉ trong mạng compose, không publish).
	AllowPlaintextEnv = "AG_VAULT_ALLOW_PLAINTEXT"
	// AllowLocalKMSEnv cho phép KEK phần mềm từ biến môi trường khi chưa có KMS/HSM trong lãnh thổ.
	AllowLocalKMSEnv = "AG_VAULT_ALLOW_LOCAL_KMS"
)

// productionGuard: ở production (residency.Production) kho định danh — nơi giữ dữ liệu cá nhân — bắt buộc mTLS
// (TLS + chứng thư client) và KMS không phải phần mềm, trừ khi người vận hành đặt ngoại lệ tường minh. Ngoài
// production không kiểm. AG_ENV thiếu/sai → lỗi (residency.ValidateEnv): guard không "mở cửa" vì quên đặt biến.
// localKMS lấy từ hiện thực KMS thật sự được cấu hình (identity.KMSIsSoftware), không phải hằng.
func productionGuard(getenv func(string) string, tlsCfg *tls.Config, localKMS bool, log *slog.Logger) error {
	if err := residency.ValidateEnv(getenv); err != nil {
		return err
	}
	if !residency.Production(getenv) {
		return nil
	}
	var errs []error
	if tlsCfg == nil || tlsCfg.ClientCAs == nil || tlsCfg.ClientAuth != tls.RequireAndVerifyClientCert {
		if getenv(AllowPlaintextEnv) == "1" {
			log.Warn("production chạy kho định danh KHÔNG mTLS theo ngoại lệ "+AllowPlaintextEnv, "event", "startup", "outcome", "degraded")
		} else {
			errs = append(errs, errors.New("production: kho định danh bắt buộc mTLS (AG_TLS_CERT, AG_TLS_KEY, AG_TLS_CLIENT_CA) — hoặc đặt "+
				AllowPlaintextEnv+"=1 để chấp nhận rủi ro"))
		}
	}
	if localKMS {
		if getenv(AllowLocalKMSEnv) == "1" {
			// Cảnh báo LỚN, có event riêng để luật log/alert bắt được; readyz và metric ag.vault.kms_software
			// cũng phơi trạng thái này (main.go, router.go). Chờ quyết định nhà cung cấp KMS/HSM trong lãnh thổ.
			log.Warn("!!! RỦI RO RB-3/RB-7: production dùng KEK PHẦN MỀM (AG_VAULT_KEK) theo ngoại lệ "+AllowLocalKMSEnv+
				" — KEK nằm trong biến môi trường/bộ nhớ tiến trình, không có HSM bảo vệ. Cần quyết định nhà cung cấp KMS/HSM trong lãnh thổ"+
				" rồi đặt AG_VAULT_KMS và bỏ ngoại lệ !!!",
				"event", "security.kms_software", "kms_local", 1, "outcome", "degraded")
		} else {
			errs = append(errs, errors.New("production: KMS phần mềm (AG_VAULT_KEK) chỉ cho dev/test — cấu hình KMS/HSM hoặc đặt "+
				AllowLocalKMSEnv+"=1 để chấp nhận rủi ro"))
		}
	}
	return errors.Join(errs...)
}
