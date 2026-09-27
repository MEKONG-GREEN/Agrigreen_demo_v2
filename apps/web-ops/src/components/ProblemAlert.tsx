import { isConfirmable, type ApiProblem } from "@/api/problem";
import { Icon } from "./Icon";

/**
 * Hiển thị lỗi RFC 9457: thông báo tiếng Việt + mã lỗi + trace_id để tra log (R-63).
 * - mfa_required (Admin API): hướng dẫn đăng nhập lại có OTP.
 * - geo.*_needs_confirm: nút "Xác nhận vẫn lưu" khi nơi gọi truyền `onConfirm`.
 */
export function ProblemAlert({
  problem,
  onRetry,
  onConfirm,
  confirming = false,
}: {
  problem: ApiProblem;
  onRetry?: () => void;
  onConfirm?: () => void;
  confirming?: boolean;
}) {
  const confirmable = isConfirmable(problem) && !!onConfirm;
  return (
    <div className={`alert ${confirmable ? "alert-warn" : "alert-bad"}`} role="alert">
      <Icon name="alert" />
      <div style={{ flex: 1 }}>
        <b>{problem.message}</b>
        {problem.code === "mfa_required" ? (
          <p className="small" style={{ marginTop: 4 }}>
            Các màn hình quản trị gọi Admin API trên máy chủ riêng; máy chủ chỉ nhận phiên đăng nhập đã qua xác thực hai yếu tố
            (OTP). Dữ liệu chưa bị thay đổi.
          </p>
        ) : null}
        {problem.fieldErrors.length > 0 && (
          <ul style={{ margin: "6px 0 0 18px" }}>
            {problem.fieldErrors.map((e, i) => (
              <li key={`${e.field ?? ""}-${i}`}>
                {e.field ? <strong>{e.field}: </strong> : null}
                {e.reason}
              </li>
            ))}
          </ul>
        )}
        <div className="small" style={{ marginTop: 6 }}>
          Mã lỗi <code>{problem.code}</code>
          {problem.traceId ? (
            <>
              {" "}
              · Mã tra cứu <code data-testid="trace-id">{problem.traceId}</code>
            </>
          ) : null}
        </div>
      </div>
      {confirmable ? (
        <button type="button" className="btn btn-p btn-sm" onClick={onConfirm} disabled={confirming}>
          {confirming ? "Đang lưu…" : "Xác nhận vẫn lưu"}
        </button>
      ) : null}
      {onRetry ? (
        <button type="button" className="btn btn-o btn-sm" onClick={onRetry}>
          Thử lại
        </button>
      ) : null}
    </div>
  );
}
