"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useState } from "react";
import { assignPlot, cooperativePlotsPage, draftPlotsInCommune } from "@/api/adapters/cooperatives";
import { unitLabel } from "@/api/adapters/units";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { usePagedList } from "@/api/pagination";
import { ApiProblem } from "@/api/problem";
import type { Cooperative, PlotFeature, PlotStatus } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Tabs } from "@/components/Tabs";
import { Empty, LoadMore, Loading, PageHead } from "@/components/ui";
import { formatDecimal } from "@/lib/decimal";
import { formatDate, formatDateTime } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";
import { useUnits } from "@/lib/useUnits";

const PLOT_STATUS: Record<PlotStatus, { cls: string; text: string }> = {
  active: { cls: "c-ok", text: "Đang dùng" },
  draft: { cls: "c-warn", text: "Nháp — chưa gán HTX" },
  retired: { cls: "c-mute", text: "Ngừng dùng" },
};

function Overview({ c }: { c: Cooperative }) {
  const { api } = useAuth();
  const units = useUnits(api, [c.admin_unit_code]);
  const rows: [string, string][] = [
    ["Mã HTX", c.code],
    ["Mã số thuế", c.tax_code ?? "—"],
    ["Địa bàn", unitLabel(units, c.admin_unit_code)],
    ["Ngày thành lập", formatDate(c.established_on)],
    ["Diện tích đăng ký", c.area_ha ? `${formatDecimal(c.area_ha, 2)} ha` : "—"],
    [
      "Vị trí trụ sở",
      c.location && !c.location_provisional ? `${c.location.lat}, ${c.location.lon}` : "Vị trí tạm (tâm xã) — chưa có toạ độ",
    ],
    ["Trạng thái", c.active ? "Đang hoạt động" : `Đã vô hiệu hoá${c.status_reason ? ` — ${c.status_reason}` : ""}`],
  ];
  return (
    <dl style={{ display: "grid", gridTemplateColumns: "minmax(140px, 220px) 1fr", gap: "10px 16px" }}>
      {rows.map(([k, v]) => (
        <div key={k} style={{ display: "contents" }}>
          <dt className="mute">{k}</dt>
          <dd style={{ fontWeight: 600 }}>{v}</dd>
        </div>
      ))}
    </dl>
  );
}

function Households({ id }: { id: string }) {
  const { api } = useAuth();
  const list = usePagedList(
    (cursor) => unwrap(api.GET("/v1/cooperatives/{ref}/households", { params: { path: { ref: id }, query: { cursor, limit: 20 } } })),
    [api, id],
  );
  const units = useUnits(api, list.items.map((h) => h.commune_code));
  return (
    <>
      <div className="alert alert-info" style={{ marginBottom: 12 }}>
        <Icon name="info" />
        <span>
          Thông tin định danh nông hộ (họ tên, SĐT, CCCD, địa chỉ) được lưu tách trong kho định danh mã hoá. Web vận hành chỉ
          hiển thị <b style={{ display: "inline" }}>nhãn đã che</b> do máy chủ trả về.
        </span>
      </div>
      {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
      <div className="tbl-wrap">
        <table className="tbl">
          <caption className="sr-only">Nông hộ thành viên (nhãn đã che)</caption>
          <thead>
            <tr>
              <th scope="col">Nông hộ (đã che)</th>
              <th scope="col">SĐT (đã che)</th>
              <th scope="col">Xã</th>
              <th scope="col" className="num">Diện tích canh tác (ha)</th>
              <th scope="col">Vào HTX</th>
              <th scope="col">Trạng thái</th>
            </tr>
          </thead>
          <tbody>
            {list.items.map((h) => (
              <tr key={h.household_ref}>
                <td>{h.label_unavailable ? <span className="mute">Không hiển thị được nhãn</span> : h.label}</td>
                <td style={{ fontFamily: "ui-monospace, Consolas, monospace" }}>{h.masked_phone || "—"}</td>
                <td>{unitLabel(units, h.commune_code, false)}</td>
                <td className="num">{formatDecimal(h.cultivated_area_ha, 2)}</td>
                <td>{formatDate(h.member_since)}</td>
                <td>{h.active ? <span className="chip c-ok">Thành viên</span> : <span className="chip c-mute">Đã rời</span>}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {list.loading ? <Loading /> : null}
      {!list.loading && list.items.length === 0 && !list.error ? <Empty>Chưa có nông hộ.</Empty> : null}
      <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
    </>
  );
}

/** Gán thửa nháp về HTX này; 409 needs_confirm → "Xác nhận vẫn lưu" (gửi lại confirm_overlap: true). */
function AssignButton({ plot, coopRef, onDone }: { plot: PlotFeature; coopRef: string; onDone: () => void }) {
  const { api } = useAuth();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  // Một khóa cho lần gửi đầu (thử lại sau lỗi mạng dùng lại), khóa mới cho lần xác nhận (thân khác).
  const [idemKey] = useState(newIdempotencyKey);

  const run = async (confirm: boolean) => {
    setBusy(true);
    try {
      await assignPlot(
        api,
        plot.properties.ref,
        { cooperative_ref: coopRef, reason: "Gán thửa nháp về HTX từ web vận hành", confirm },
        confirm ? newIdempotencyKey() : idemKey,
      );
      setProblem(null);
      onDone();
    } catch (e) {
      setProblem(e instanceof ApiProblem ? e : null);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="stack" style={{ gap: 6 }}>
      <button type="button" className="btn btn-o btn-sm" disabled={busy} onClick={() => void run(false)}>
        {busy && !problem ? "Đang gán…" : "Gán về HTX này"}
      </button>
      {problem ? <ProblemAlert problem={problem} onConfirm={() => void run(true)} confirming={busy} /> : null}
    </div>
  );
}

/** Thửa KN vẽ trước trong xã, chưa gán HTX ("Nháp - KN vẽ trước") — gán về HTX đang xem. */
function DraftPlots({ coop, onAssigned }: { coop: Cooperative; onAssigned: () => void }) {
  const { api } = useAuth();
  const commune = coop.admin_unit_code;
  const drafts = useAsync(
    () => (commune ? draftPlotsInCommune(api, commune) : Promise.resolve({ items: [] as PlotFeature[], truncated: false })),
    [api, commune],
  );
  const done = () => {
    drafts.reload();
    onAssigned();
  };
  return (
    <section style={{ marginTop: 20 }} aria-labelledby="draft-title">
      <div className="sec-h">
        <h3 id="draft-title">Thửa nháp trong xã (chưa gán HTX)</h3>
        <span className="mute small">KN vẽ trước — gán về HTX này sau khi xác minh</span>
      </div>
      {drafts.error ? <ProblemAlert problem={drafts.error} onRetry={drafts.reload} /> : null}
      {drafts.loading && !drafts.data ? <Loading /> : null}
      {drafts.data?.truncated ? <p className="mute small">Có hơn 1.000 thửa trong khung xã — chỉ hiện một phần.</p> : null}
      {drafts.data && drafts.data.items.length === 0 ? <Empty>Không có thửa nháp chờ gán trong xã.</Empty> : null}
      {drafts.data && drafts.data.items.length > 0 ? (
        <div className="tbl-wrap">
          <table className="tbl">
            <caption className="sr-only">Thửa nháp chưa gán HTX</caption>
            <thead>
              <tr>
                <th scope="col">Mã thửa</th>
                <th scope="col" className="num">Diện tích (ha)</th>
                <th scope="col">Vẽ lúc</th>
                <th scope="col">Thao tác</th>
              </tr>
            </thead>
            <tbody>
              {drafts.data.items.map((p) => (
                <tr key={p.properties.ref}>
                  <td>
                    <b>{p.properties.code}</b>
                    {p.properties.name ? <div className="mute small">{p.properties.name}</div> : null}
                  </td>
                  <td className="num">{formatDecimal(p.properties.area_ha, 2)}</td>
                  <td>{formatDateTime(p.properties.created_at)}</td>
                  <td style={{ minWidth: 260 }}>
                    <AssignButton plot={p} coopRef={coop.ref} onDone={done} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </section>
  );
}

function PlotsTab({ coop }: { coop: Cooperative }) {
  const [version, setVersion] = useState(0);
  return (
    <>
      <Plots key={version} id={coop.ref} />
      {coop.active ? <DraftPlots coop={coop} onAssigned={() => setVersion((v) => v + 1)} /> : null}
    </>
  );
}

/** Thửa của HTX — GET /v1/cooperatives/{ref}/plots (pf/geo). */
function Plots({ id }: { id: string }) {
  const { api } = useAuth();
  const list = usePagedList((cursor) => cooperativePlotsPage(api, id, cursor, 20), [api, id]);
  return (
    <>
      {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
      <div className="tbl-wrap">
        <table className="tbl">
          <caption className="sr-only">Thửa ruộng của HTX</caption>
          <thead>
            <tr>
              <th scope="col">Mã thửa</th>
              <th scope="col" className="num">Diện tích (ha)</th>
              <th scope="col">Trạng thái</th>
              <th scope="col">Ranh giới</th>
              <th scope="col">Cập nhật</th>
            </tr>
          </thead>
          <tbody>
            {list.items.map((p) => (
              <tr key={p.properties.ref}>
                <td>
                  <b>{p.properties.code}</b>
                  {p.properties.name ? <div className="mute small">{p.properties.name}</div> : null}
                </td>
                <td className="num">{formatDecimal(p.properties.area_ha, 2)}</td>
                <td>
                  <span className={`chip ${PLOT_STATUS[p.properties.status].cls}`} title={p.properties.status_reason ?? undefined}>
                    {PLOT_STATUS[p.properties.status].text}
                  </span>
                </td>
                <td>
                  Phiên bản {p.properties.version}
                  {p.properties.needs_review ? <div><span className="chip c-bad">Có chồng lấn chờ xem xét</span></div> : null}
                </td>
                <td>{formatDateTime(p.properties.updated_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {list.loading ? <Loading /> : null}
      {!list.loading && list.items.length === 0 && !list.error ? <Empty>HTX chưa có thửa.</Empty> : null}
      <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
    </>
  );
}

export default function CooperativeDetailPage() {
  const params = useParams<{ id: string }>();
  const id = params?.id ?? "";
  const { api } = useAuth();
  const [tab, setTab] = useState("overview");
  const coop = useAsync(() => unwrap(api.GET("/v1/cooperatives/{ref}", { params: { path: { ref: id } } })), [api, id]);
  const units = useUnits(api, [coop.data?.admin_unit_code]);

  return (
    <>
      <p style={{ marginBottom: 8 }}>
        <Link href="/htx">← Danh sách HTX</Link>
      </p>
      {coop.error ? <ProblemAlert problem={coop.error} onRetry={coop.reload} /> : null}
      {coop.loading && !coop.data ? <Loading /> : null}
      {coop.data ? (
        <>
          <PageHead
            title={coop.data.name}
            subtitle={`${coop.data.code} · ${unitLabel(units, coop.data.admin_unit_code)}`}
            actions={coop.data.active ? <span className="chip c-ok">Đang hoạt động</span> : <span className="chip c-mute">Đã vô hiệu hoá</span>}
          />
          <div className="card sec">
            <Tabs
              label="Thông tin HTX"
              value={tab}
              onChange={setTab}
              tabs={[
                { id: "overview", label: "Tổng quan" },
                { id: "households", label: "Nông hộ" },
                { id: "plots", label: "Thửa ruộng" },
              ]}
            >
              {tab === "overview" ? <Overview c={coop.data} /> : null}
              {tab === "households" ? <Households id={id} /> : null}
              {tab === "plots" ? <PlotsTab coop={coop.data} /> : null}
            </Tabs>
          </div>
        </>
      ) : null}
    </>
  );
}
