"use client";

// "Báo cáo cho KHTC" — màn phía phòng (W11.11, BRD KHTC v2.2 FN-PN-03.01, Hình 21): một màn dùng chung cho mọi phòng chuyên
// môn (Trồng trọt, CNTS, Thông tin tuyên truyền, HCTH), phòng lấy từ ngữ cảnh phiên; Admin mở qua Phòng ban › <phòng> ›
// Báo cáo cho KHTC (mã phòng trong URL). Hai góc nhìn: "Phiếu kỳ này" (mặc định lọc Cần xử lý, hạn gần nhất trước) và
// "Phiếu đã gửi". Bộ lọc Loại báo cáo, Trạng thái; nhãn đếm; dải yêu cầu bổ sung; nút Nhập / Tiếp tục / Sửa, gửi lại / Xem
// mở màn phiếu của W11.63 (entry_route).
import Link from "next/link";
import { useParams } from "next/navigation";
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, dt, useLoad } from "@/kn/cycle/common";
import { REPORT_TYPES } from "@/kn/khtc/periods/api";
import {
  STATE_FILTERS,
  STATE_TONE,
  attemptText,
  createDeptFormsApi,
  filterRows,
  latestSupplement,
  type DeptForm,
  type DeptFormsView,
} from "./api";

const TABS: { view: DeptFormsView; label: string; href: string }[] = [
  { view: "current", label: "Phiếu kỳ này", href: "/kn/bao-cao-khtc" },
  { view: "sent", label: "Phiếu đã gửi", href: "/kn/bao-cao-khtc/da-gui" },
];

export function DepartmentFormsScreen({
  view: initial,
}: {
  view: DeptFormsView;
}) {
  const { api, kn } = useAuth();
  const params = useParams<{ dept?: string }>();
  const adminDept = params?.dept;
  // Phòng: hai mục menu (hai đường dẫn); Admin: một mục theo phòng ban, đổi góc nhìn tại chỗ.
  const [view, setView] = useState<DeptFormsView>(initial);
  const dapi = useMemo(() => (api ? createDeptFormsApi(api) : null), [api]);
  const list = useLoad(dapi ? () => dapi.list(view, adminDept) : null, [
    dapi,
    view,
    adminDept,
  ]);
  const [type, setType] = useState("");
  const [state, setState] = useState(
    initial === "current" ? "needs_action" : "",
  );
  if (!dapi) return null;

  return (
    <div className="page">
      <PageHeader
        title={`Báo cáo cho KHTC${list.data?.department_name ? ` · ${list.data.department_name}` : ""}`}
      >
        <p className="mute">
          Phiếu Phòng Kế hoạch – Tài chính giao cho phòng theo kỳ báo cáo. Chỉ
          nhập số liệu chưa có trong module của phòng; số liệu đã chốt ở module
          được KHTC lấy trực tiếp.
          {kn.context?.department_code === "chan_nuoi_thuy_san"
            ? " Báo cáo BM-11 vẫn gửi để lưu, số liệu cho KHTC chỉ đi qua phiếu và nguồn đã chốt."
            : ""}
        </p>
      </PageHeader>
      <nav className="row" aria-label="Báo cáo cho KHTC">
        {TABS.map((t) =>
          adminDept ? (
            <button
              key={t.view}
              className={t.view === view ? "btn btn-primary" : "btn"}
              aria-pressed={t.view === view}
              onClick={() => (
                setView(t.view),
                setState(t.view === "current" ? "needs_action" : "")
              )}
            >
              {t.label}
            </button>
          ) : (
            <Link
              key={t.view}
              href={t.href}
              className={t.view === view ? "btn btn-primary" : "btn"}
              aria-current={t.view === view ? "page" : undefined}
            >
              {t.label}
            </Link>
          ),
        )}
      </nav>
      <LoadState s={list}>
        {(d) => {
          const rows = filterRows(d.items, type, state);
          const sup = latestSupplement(d.items);
          return (
            <section className="card sec">
              <div className="row">
                <StatusPill tone="info">{`Tổng ${d.counts.total}`}</StatusPill>
                {view === "current" ? (
                  <StatusPill
                    tone={d.counts.needs_action ? "warn" : "ok"}
                  >{`Cần xử lý ${d.counts.needs_action}`}</StatusPill>
                ) : null}
                {d.counts.supplement ? (
                  <StatusPill tone="warn">{`Yêu cầu bổ sung ${d.counts.supplement}`}</StatusPill>
                ) : null}
                {d.counts.late ? (
                  <StatusPill tone="bad">{`Trễ hạn ${d.counts.late}`}</StatusPill>
                ) : null}
                {d.read_only ? (
                  <StatusPill tone="neutral">Chỉ xem</StatusPill>
                ) : null}
              </div>
              {sup ? (
                <div
                  className="alert alert-warn"
                  role="status"
                  aria-label="Yêu cầu bổ sung"
                >
                  <strong>Yêu cầu bổ sung — {sup.name}:</strong>{" "}
                  {sup.supplement_note}
                </div>
              ) : null}
              <div className="row">
                <label>
                  Loại báo cáo{" "}
                  <select
                    aria-label="Loại báo cáo"
                    value={type}
                    onChange={(e) => setType(e.target.value)}
                  >
                    <option value="">Tất cả</option>
                    {REPORT_TYPES.map((t) => (
                      <option key={t.value} value={t.value}>
                        {t.label}
                      </option>
                    ))}
                  </select>
                </label>
                <label>
                  Trạng thái{" "}
                  <select
                    aria-label="Trạng thái"
                    value={state}
                    onChange={(e) => setState(e.target.value)}
                  >
                    {STATE_FILTERS.map((s) => (
                      <option key={s.value} value={s.value}>
                        {s.label}
                      </option>
                    ))}
                  </select>
                </label>
              </div>
              {rows.length === 0 ? (
                <p className="mute">
                  {d.items.length === 0
                    ? view === "current"
                      ? "Chưa có phiếu nào — phiếu hiện khi Phòng KHTC mở kỳ."
                      : "Phòng chưa gửi phiếu nào."
                    : "Không có phiếu khớp bộ lọc."}
                </p>
              ) : (
                <table className="dt">
                  <thead>
                    <tr>
                      <th>STT</th>
                      <th>Loại báo cáo</th>
                      <th>Kỳ</th>
                      <th>Hạn nộp</th>
                      <th>Lần gửi</th>
                      <th>Trạng thái</th>
                      <th>Thao tác</th>
                    </tr>
                  </thead>
                  <tbody>
                    {rows.map((r: DeptForm, i) => (
                      <tr key={r.period_id}>
                        <td>{i + 1}</td>
                        <td>{r.report_type_label}</td>
                        <td>{r.name}</td>
                        <td>
                          {dt(r.due_at)}
                          {r.period_state === "closed" ? (
                            <span className="mute"> · kỳ đã đóng</span>
                          ) : null}
                        </td>
                        <td>
                          {attemptText(r)}
                          {r.late ? (
                            <span className="mute"> · gửi trễ</span>
                          ) : null}
                        </td>
                        <td>
                          <StatusPill tone={STATE_TONE[r.state]}>
                            {r.state_label}
                          </StatusPill>
                        </td>
                        <td>
                          <Link
                            className={
                              r.action === "view" ? "btn" : "btn btn-primary"
                            }
                            href={r.entry_route}
                          >
                            {r.action_label}
                          </Link>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </section>
          );
        }}
      </LoadState>
    </div>
  );
}
