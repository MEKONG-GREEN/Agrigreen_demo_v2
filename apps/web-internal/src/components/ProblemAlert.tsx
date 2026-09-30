import type { ApiProblem } from "@/api/problem";
import { Icon } from "./Icon";

/**
 * Hiển thị lỗi RFC 9457: thông báo tiếng Việt + mã lỗi + trace_id để tra log (R-63).
 * mfa_required (bề mặt quản trị): hướng dẫn đăng nhập lại có OTP.
 */
export function ProblemAlert({ problem, onRetry }: { problem: ApiProblem; onRetry?: () => void }) {
  return (
    <div className="alert alert-bad" role="alert">
      <Icon name="alert" />
      <div style={{ flex: 1 }}>
        <b>{problem.message}</b>
        {problem.code === "mfa_required" ? (
          <p className="small" style={{ marginTop: 4 }}>
            Màn hình quản trị gọi bề mặt quản trị của api-internal (host riêng); máy chủ chỉ nhận phiên đã qua xác thực hai yếu tố
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
      {onRetry ? (
        <button type="button" className="btn btn-o btn-sm" onClick={onRetry}>
          Thử lại
        </button>
      ) : null}
    </div>
  );
}
