"use client";

// FN-12 Biểu mẫu báo cáo Sở (NS01–NS06) và FN-06 Dashboard nhân sự (BRD HCTH v3.0; W11.28, dom/hrm). Danh sách 6 biểu
// (kỳ, lần xuất gần nhất), chi tiết biểu: tham số (ngày chốt, người lập, người ký), kiểm tra số liệu (R1–R3, R6, TK) kèm
// người liên quan, xem trước, nhấn ô số (NS05, NS06) xem người được đếm, Xuất Excel, Lịch sử các lần xuất (tải lại; báo
// khi nội dung khác bản gốc). Dashboard: thẻ chỉ số + phân bố, bấm để mở danh sách người. Quyền do máy chủ quyết định;
// dữ liệu cá nhân chỉ trong bộ nhớ của màn.
import Link from "next/link";
import { useParams } from "next/navigation";
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { BackLink } from "@/components/patterns/detail";
import { AlertList } from "@/components/patterns/dashboard";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import { saveBlob } from "@/lib/files";
import {
  AGE_LABEL,
  CHECK_STATUS_LABEL,
  createReportsApi,
  PERIOD_LABEL,
  REPORT_CODES,
  type HrmPersonBrief,
  type ReportCode,
  type ReportsApi,
} from "./reports-api";
import { STAFF_TYPE_LABEL, vnDate } from "./staff-api";
import { hrmDashboardAlerts } from "./dashboard-alerts";
import { createPromotionApi } from "./promotion-api";
import { KpiGrid, KpiTile } from "./ui";

const BASE = "/kn/nhan-su/bao-cao-so";

function useReportsApi(): ReportsApi {
  const { api } = useAuth();
  return useMemo(() => createReportsApi(api), [api]);
}

function vnToday(): string {
  return new Date(Date.now() + 7 * 3_600_000).toISOString().slice(0, 10);
}

function CheckPill({ s }: { s: string }) {
  return (
    <StatusPill
      tone={
        s === "ready"
          ? "ok"
          : s === "warning"
            ? "warn"
            : s === "blocked"
              ? "bad"
              : "neutral"
      }
    >
      {CHECK_STATUS_LABEL[s] ?? "Chưa kiểm tra"}
    </StatusPill>
  );
}

function People({
  items,
  hidden,
}: {
  items: HrmPersonBrief[];
  hidden?: number;
}) {
  if (!items.length && !hidden)
    return <p className="mute small">Không có người.</p>;
  return (
    <div className="tbl-wrap">
      <table className="dt">
        <caption className="sr-only">Danh sách người</caption>
        <thead>
          <tr>
            <th>Mã hồ sơ</th>
            <th>Họ tên</th>
            <th>Loại</th>
            <th>Đơn vị</th>
          </tr>
        </thead>
        <tbody>
          {items.map((p) => (
            <tr key={p.id}>
              <td>
                <Link href={`/kn/nhan-su/ho-so/${p.id}`}>{p.code}</Link>
              </td>
              <td>{p.name || <span className="mute">(ẩn)</span>}</td>
              <td>{STAFF_TYPE_LABEL[p.type]}</td>
              <td>{p.unit_name}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {hidden ? (
        <p className="mute small">{hidden} người ngoài phạm vi xem của bạn.</p>
      ) : null}
    </div>
  );
}

// --------------------------------------------------------------------------------------------------------------------
// FN-12 B1 — danh sách 6 biểu.
// --------------------------------------------------------------------------------------------------------------------

export function HrmReportListScreen() {
  const ra = useReportsApi();
  const list = useLoad(() => ra.list(), []);
  return (
    <>
      <PageHeader title="Biểu mẫu báo cáo Sở">
        Sáu biểu NS01–NS06 lấy dữ liệu hồ sơ tại ngày chốt số liệu, kiểm tra số
        liệu trước khi xuất Excel theo mẫu Sở. Mọi lần xem, xuất được ghi nhật
        ký.
      </PageHeader>
      <LoadState s={list}>
        {(l) => (
          <div className="card dt-card">
            <div className="tbl-wrap">
              <table className="dt">
                <caption className="sr-only">Biểu báo cáo Sở</caption>
                <thead>
                  <tr>
                    <th>Mã</th>
                    <th>Tên biểu</th>
                    <th>Kỳ</th>
                    <th>Lần xuất gần nhất</th>
                    <th>Kiểm tra số liệu</th>
                  </tr>
                </thead>
                <tbody>
                  {l.items.map((r) => (
                    <tr key={r.code}>
                      <td>
                        <Link href={`${BASE}/${r.code}`}>{r.code}</Link>
                      </td>
                      <td>
                        {r.name}
                        {r.sensitive_fields.length ? (
                          <span className="mute small">
                            {" "}
                            · có trường nhạy cảm
                          </span>
                        ) : null}
                      </td>
                      <td>{PERIOD_LABEL[r.period]}</td>
                      <td>
                        {r.last_export
                          ? `${r.last_export.period} · ngày chốt ${vnDate(r.last_export.as_of)} (${r.exports} lần)`
                          : "Chưa xuất"}
                      </td>
                      <td>
                        <CheckPill s={r.last_export?.check_status ?? ""} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}
      </LoadState>
    </>
  );
}

// --------------------------------------------------------------------------------------------------------------------
// FN-12 B2–B4 — chi tiết biểu.
// --------------------------------------------------------------------------------------------------------------------

export function HrmReportDetailScreen() {
  const params = useParams<{ code: string }>();
  const code = (REPORT_CODES.find((c) => c === params.code) ??
    "NS01") as ReportCode;
  const ra = useReportsApi();
  const [at, setAt] = useState(vnToday());
  const [preparer, setPreparer] = useState("");
  const [signer, setSigner] = useState("");
  const [nonce, setNonce] = useState(0);
  const [cell, setCell] = useState<{
    row: string;
    col: string;
    label: string;
  } | null>(null);
  const act = useAction();
  const view = useLoad(() => ra.preview(code, at), [code, at]);
  const hist = useLoad(() => ra.history(code), [code, nonce]);
  const v = view.data;
  const persons = (v?.rows ?? []).filter((r) => r.kind === "person");
  return (
    <>
      <BackLink href={BASE}>Biểu mẫu báo cáo Sở</BackLink>
      <PageHeader
        title={`${code} — ${v?.report.name ?? ""}`}
        badge={v ? <CheckPill s={v.status} /> : null}
        actions={
          <button
            type="button"
            className="btn btn-p btn-sm"
            disabled={act.busy || !v || v.status === "blocked"}
            onClick={() =>
              void act.run(
                async () => {
                  const r = await ra.exportReport(code, {
                    as_of: at,
                    preparer_id: preparer || null,
                    signer_id: signer || null,
                  });
                  const f = await ra.file(r.id);
                  saveBlob(f.blob, f.name);
                  setNonce((n) => n + 1);
                  return r;
                },
                (r) => `Đã xuất ${r.file_name} (thao tác được ghi nhật ký).`,
              )
            }
          >
            Xuất Excel
          </button>
        }
      >
        {v
          ? `${v.title} — ${v.period}. Số liệu tính đến ngày ${vnDate(v.as_of)}; ${v.people} người.`
          : null}
      </PageHeader>
      {act.view}
      <div className="card">
        <h2 className="h3">Tham số xuất</h2>
        <div className="row">
          <label className="small">
            Ngày chốt số liệu{" "}
            <input
              type="date"
              aria-label="Ngày chốt số liệu"
              value={at}
              max={vnToday()}
              onChange={(e) => setAt(e.target.value || vnToday())}
            />
          </label>
          <label className="small">
            Người lập biểu{" "}
            <select
              aria-label="Người lập biểu"
              value={preparer}
              onChange={(e) => setPreparer(e.target.value)}
            >
              <option value="">— Không ghi —</option>
              {persons.map((r) => (
                <option key={r.staff_id} value={r.staff_id}>
                  {r.cells[1]}
                </option>
              ))}
            </select>
          </label>
          <label className="small">
            Người ký{" "}
            <select
              aria-label="Người ký"
              value={signer}
              onChange={(e) => setSigner(e.target.value)}
            >
              <option value="">— Không ghi —</option>
              {persons.map((r) => (
                <option key={r.staff_id} value={r.staff_id}>
                  {r.cells[1]}
                </option>
              ))}
            </select>
          </label>
        </div>
      </div>
      <LoadState s={view}>
        {(rv) => (
          <>
            <div className="card">
              <h2 className="h3">Kiểm tra số liệu</h2>
              <ul className="plain">
                {rv.checks.map((c, i) => (
                  <li key={i}>
                    <StatusPill
                      tone={c.ok ? "ok" : c.level === "block" ? "bad" : "warn"}
                    >
                      {c.ok ? "Đạt" : c.level === "block" ? "Chặn" : "Cảnh báo"}
                    </StatusPill>{" "}
                    <strong>{c.rule}</strong> {c.message}
                    {!c.ok && c.people.length ? (
                      <details>
                        <summary>{c.people.length} người liên quan</summary>
                        <People items={c.people} />
                      </details>
                    ) : null}
                  </li>
                ))}
              </ul>
            </div>
            <div className="card dt-card">
              <div className="tbl-wrap">
                <table className="dt">
                  <caption>
                    {rv.title} — {rv.period}
                    {rv.report.aggregate
                      ? " · bấm vào ô số để xem người được đếm"
                      : ""}
                  </caption>
                  <thead>
                    <tr>
                      {rv.columns.map((c) => (
                        <th
                          key={c.key}
                          title={
                            c.sensitive
                              ? "Trường thẻ Thống kê BC Sở (nhạy cảm)"
                              : undefined
                          }
                        >
                          {c.group ? `${c.group} — ` : ""}
                          {c.label}
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {rv.rows.map((r, i) => (
                      <tr
                        key={i}
                        className={
                          r.kind === "person" ? undefined : "row-strong"
                        }
                      >
                        {r.kind !== "person" && !rv.report.aggregate ? (
                          <td colSpan={rv.columns.length}>
                            <strong>{r.cells[1]}</strong>
                          </td>
                        ) : (
                          r.cells.map((v0, j) => {
                            const col = rv.columns[j]!;
                            return (
                              <td
                                key={j}
                                className={col.numeric ? "num" : undefined}
                              >
                                {col.numeric && v0 !== "0" ? (
                                  <button
                                    type="button"
                                    className="btn-link"
                                    aria-label={`Xem ${v0} người: ${r.cells[1]} — ${col.label}`}
                                    onClick={() =>
                                      setCell({
                                        row: r.key,
                                        col: col.key,
                                        label: `${r.cells[1]} — ${col.group ? `${col.group} ` : ""}${col.label}`,
                                      })
                                    }
                                  >
                                    {v0}
                                  </button>
                                ) : (
                                  v0
                                )}
                              </td>
                            );
                          })
                        )}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          </>
        )}
      </LoadState>
      <div className="card">
        <h2 className="h3">Lịch sử các lần xuất</h2>
        <LoadState s={hist}>
          {(h) =>
            h.items.length === 0 ? (
              <p className="mute small">Chưa có lần xuất.</p>
            ) : (
              <ul className="plain">
                {h.items.map((x) => (
                  <li key={x.id}>
                    {new Date(x.recorded_at).toLocaleString("vi-VN")} ·{" "}
                    {x.action === "export" ? "Xuất" : "Tải lại"} · ngày chốt{" "}
                    {vnDate(x.as_of)} · {x.item_count} người · {x.file_name}
                    {x.drift ? (
                      <StatusPill tone="warn">Khác bản gốc</StatusPill>
                    ) : null}
                    {x.action === "export" ? (
                      <>
                        {" "}
                        <button
                          type="button"
                          className="btn btn-o btn-sm"
                          disabled={act.busy}
                          onClick={() =>
                            void act.run(
                              async () => {
                                const f = await ra.file(x.id);
                                saveBlob(f.blob, f.name);
                                setNonce((n) => n + 1);
                                return f;
                              },
                              (f) =>
                                f.drift
                                  ? "Đã tải lại — nội dung khác bản gốc (định danh đã sửa sau lần xuất)."
                                  : "Đã tải lại tệp đúng bản gốc.",
                            )
                          }
                        >
                          Tải lại
                        </button>
                      </>
                    ) : null}
                  </li>
                ))}
              </ul>
            )
          }
        </LoadState>
      </div>
      {cell ? (
        <CellDialog
          ra={ra}
          code={code}
          at={at}
          cell={cell}
          onClose={() => setCell(null)}
        />
      ) : null}
    </>
  );
}

function CellDialog({
  ra,
  code,
  at,
  cell,
  onClose,
}: {
  ra: ReportsApi;
  code: ReportCode;
  at: string;
  cell: { row: string; col: string; label: string };
  onClose: () => void;
}) {
  const r = useLoad(
    () => ra.cell(code, at, cell.row, cell.col),
    [code, at, cell.row, cell.col],
  );
  return (
    <Dialog title={`Người được đếm: ${cell.label}`} onClose={onClose}>
      <LoadState s={r}>
        {(x) => <People items={x.items} hidden={x.hidden} />}
      </LoadState>
    </Dialog>
  );
}

// --------------------------------------------------------------------------------------------------------------------
// FN-06 — Dashboard nhân sự.
// --------------------------------------------------------------------------------------------------------------------

export function HrmDashboardScreen() {
  const ra = useReportsApi();
  const [at, setAt] = useState(vnToday());
  const [metric, setMetric] = useState<{ key: string; label: string } | null>(
    null,
  );
  const d = useLoad(() => ra.dashboard(at), [at]);
  // W11.29: khối "Cảnh báo" như prototype — nâng bậc khung 3 tháng (chỉ phạm vi toàn Trung tâm: HCTH/Admin).
  const { api } = useAuth();
  const pa = useMemo(() => createPromotionApi(api), [api]);
  const promo = useLoad(
    d.data?.scope === "all" ? () => pa.list(3, at) : null,
    [d.data?.scope, at],
  );
  const Tile = ({ k, label, n }: { k: string; label: string; n: number }) => (
    <KpiTile
      label={label}
      value={n}
      onClick={() => setMetric({ key: k, label })}
      ariaLabel={`${label}: ${n} — xem danh sách`}
    />
  );
  const Bars = ({
    title,
    items,
    prefix,
  }: {
    title: string;
    items: { key: string; name: string; count: number }[];
    prefix: string;
  }) => {
    const max = Math.max(1, ...items.map((x) => x.count));
    return (
      <div className="card">
        <h2 className="h3">{title}</h2>
        {items.length === 0 ? (
          <p className="mute small">Chưa có dữ liệu.</p>
        ) : (
          <ul className="plain">
            {items.map((x) => (
              <li key={x.key}>
                <button
                  type="button"
                  className="btn-link"
                  onClick={() =>
                    setMetric({
                      key: `${prefix}${x.key}`,
                      label: `${title}: ${x.name}`,
                    })
                  }
                >
                  {x.name}
                </button>{" "}
                <span
                  className="bar"
                  style={{
                    display: "inline-block",
                    width: `${(x.count / max) * 60}%`,
                    minWidth: 4,
                    height: 8,
                    background: "var(--accent, #2f7d4f)",
                  }}
                />{" "}
                {x.count}
              </li>
            ))}
          </ul>
        )}
      </div>
    );
  };
  return (
    <>
      <PageHeader
        title="Dashboard nhân sự"
        actions={
          <label className="small">
            Ngày chốt{" "}
            <input
              type="date"
              aria-label="Ngày chốt"
              value={at}
              max={vnToday()}
              onChange={(e) => setAt(e.target.value || vnToday())}
            />
          </label>
        }
      >
        Tổng quan nhân sự tại ngày chốt; tổng số có mặt khớp số dòng NS01 cùng
        ngày. Bấm vào thẻ hoặc dòng để mở danh sách.
      </PageHeader>
      <LoadState s={d}>
        {(x) => (
          <>
            {!x.pii ? (
              <div className="alert alert-info" role="note">
                <span>
                  Bạn chưa được cấp quyền đọc kho định danh: giới tính, độ tuổi
                  hiển thị “Chưa rõ”.
                </span>
              </div>
            ) : null}
            <p className="mute small">
              Ngày chốt {vnDate(x.as_of)} · phạm vi:{" "}
              {x.scope === "all" ? "toàn Trung tâm" : "đơn vị của bạn"}
            </p>
            {x.staff === 0 && x.total === 0 ? (
              <p className="mute">Chưa có dữ liệu tại ngày chốt.</p>
            ) : null}
            <KpiGrid label="Chỉ số nhân sự">
              <Tile k="total" label="Viên chức có mặt (NS01)" n={x.total} />
              <Tile k="staff" label="Viên chức + HĐLĐ" n={x.staff} />
              <Tile
                k="type:can_bo_xa"
                label="Cán bộ khuyến nông xã"
                n={x.by_type.can_bo_xa ?? 0}
              />
              <Tile k="joined" label="Vào làm trong tháng" n={x.joined} />
              <Tile k="left" label="Nghỉ việc trong tháng" n={x.left} />
              <Tile
                k="retire_soon"
                label="Sắp đến tuổi nghỉ hưu"
                n={x.retire_soon}
              />
              <Tile
                k="over_retire"
                label="Trên tuổi nghỉ hưu"
                n={x.over_retire}
              />
            </KpiGrid>
            <div className="grid-2">
              <Bars title="Theo đơn vị" items={x.by_unit} prefix="unit:" />
              <Bars
                title="Trình độ chuyên môn"
                items={x.education}
                prefix="edu:"
              />
              <Bars
                title="Giới tính"
                items={Object.entries(x.gender).map(([k, n]) => ({
                  key: k,
                  name: k === "nam" ? "Nam" : k === "nu" ? "Nữ" : "Chưa rõ",
                  count: n,
                }))}
                prefix="gender:"
              />
              <Bars
                title="Độ tuổi"
                items={Object.entries(x.age).map(([k, n]) => ({
                  key: k,
                  name: AGE_LABEL[k] ?? k,
                  count: n,
                }))}
                prefix="age:"
              />
              <Bars
                title="Ngạch/hạng"
                items={Object.entries(x.grade_group).map(([k, n]) => ({
                  key: k,
                  name: k === "CON_LAI" ? "Còn lại" : `${k} và tương đương`,
                  count: n,
                }))}
                prefix="grade:"
              />
            </div>
            <div style={{ marginTop: 24 }}>
              <AlertList
                alerts={hrmDashboardAlerts(
                  promo.data ?? null,
                  { joined: x.joined, left: x.left },
                  `${x.as_of}T00:00:00+07:00`,
                )}
              />
            </div>
          </>
        )}
      </LoadState>
      {metric ? (
        <MembersDialog
          ra={ra}
          at={at}
          metric={metric}
          onClose={() => setMetric(null)}
        />
      ) : null}
    </>
  );
}

function MembersDialog({
  ra,
  at,
  metric,
  onClose,
}: {
  ra: ReportsApi;
  at: string;
  metric: { key: string; label: string };
  onClose: () => void;
}) {
  const r = useLoad(() => ra.members(metric.key, at), [metric.key, at]);
  return (
    <Dialog title={metric.label} onClose={onClose}>
      <LoadState s={r}>
        {(x) => (
          <>
            <p className="mute small">
              {x.total} người
              {x.total > x.items.length
                ? ` — hiển thị ${x.items.length} người đầu`
                : ""}
              .
            </p>
            <People items={x.items} />
          </>
        )}
      </LoadState>
    </Dialog>
  );
}
