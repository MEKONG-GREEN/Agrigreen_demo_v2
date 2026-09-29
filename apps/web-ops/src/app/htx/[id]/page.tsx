"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useState } from "react";
import { assignPlot, cooperativePlotsPage, coopPlace, draftPlotsInCommune, getCooperative } from "@/api/adapters/cooperatives";
import { newIdempotencyKey } from "@/api/idempotency";
import { usePagedList } from "@/api/pagination";
import { ApiProblem } from "@/api/problem";
import type { Cooperative, CoopCopyResult, PlotFeature, PlotStatus } from "@/api/types";
import { unwrap } from "@/api/client";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { Icon } from "@/components/Icon";
import { ReasonDialog } from "@/htx/common";
import { HouseholdsPanel } from "@/htx/HouseholdsPanel";
import { Dialog } from "@/components/Dialog";
import { normalizePhone } from "@/htx/rules";
import { toApiProblem } from "@/lib/useAsync";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Tabs } from "@/components/Tabs";
import { Empty, LoadMore, Loading, PageHead } from "@/components/ui";
import { formatDecimal, formatInt } from "@/lib/decimal";
import { formatDate, formatDateTime } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";

const PLOT_STATUS: Record<PlotStatus, { cls: string; text: string }> = {
  active: { cls: "c-ok", text: "Đang dùng" },
  draft: { cls: "c-warn", text: "Nháp — chưa gán HTX" },
  retired: { cls: "c-mute", text: "Ngừng dùng" },
};

function Overview({ c }: { c: Cooperative }) {
  const rows: [string, string][] = [
    ["Mã HTX", c.code],
    ["Mã số thuế", c.tax_code ?? "—"],
    ["Địa bàn", coopPlace(c)],
    ["Địa chỉ trụ sở", c.address ?? "—"],
    ["Hộ thành viên", c.member_households === undefined ? "—" : formatInt(c.member_households)],
    ["Thửa đã số hoá", c.digitized_plots === null || c.digitized_plots === undefined ? "—" : formatInt(c.digitized_plots)],
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

/**
 * Hộp thoại vô hiệu hoá HTX (Admin API, registry_admin / platform_admin): hiện số vụ đang mở và nhật ký
 * chờ duyệt (GET /v1/cropcycle/stats/cooperatives/{ref}) để cảnh báo phạm vi ảnh hưởng; lý do ≥ 20 ký tự.
 */
function DeactivateDialog({ coop, onClose, onDone }: { coop: Cooperative; onClose: () => void; onDone: () => void }) {
  const { adminApi } = useAuth();
  const stats = useAsync(() => unwrap(adminApi.GET("/v1/cropcycle/stats/cooperatives/{ref}", { params: { path: { ref: coop.ref } } })), [adminApi, coop.ref]);
  return (
    <ReasonDialog
      title={`Vô hiệu hoá ${coop.name}`}
      label="Lý do vô hiệu hoá (ít nhất 20 ký tự, ghi vào sổ biến động)"
      min={20}
      danger
      confirmText="Vô hiệu hoá HTX"
      onClose={onClose}
      run={async (reason, key) => {
        await unwrap(adminApi.POST("/v1/cooperatives/{ref}/deactivate", { params: { path: { ref: coop.ref }, header: { "Idempotency-Key": key } }, body: { reason } }));
        onDone();
      }}
    >
      <div className="alert alert-warn small" role="note" aria-label="Hoạt động đang dở">
        <Icon name="alert" />
        <span>
          {stats.error ? (
            <>Không đọc được số liệu mùa vụ ({stats.error.code}).</>
          ) : stats.data ? (
            <>
              HTX đang có <b style={{ display: "inline" }}>{formatInt(stats.data.open_seasons)} vụ đang mở</b> và{" "}
              <b style={{ display: "inline" }}>{formatInt(stats.data.pending_logs)} nhật ký chờ duyệt</b>. Sau khi vô hiệu hoá, tài khoản của HTX bị khóa; vụ và nhật ký được giữ nguyên
              (không xoá).
            </>
          ) : (
            "Đang đọc số vụ đang mở và nhật ký chờ duyệt…"
          )}
        </span>
      </div>
    </ReasonDialog>
  );
}

/**
 * Sao chép hồ sơ HTX (HTX US-HTX-03, Admin API): bản sao giữ địa bàn và cấu trúc của hồ sơ nguồn; chỉ Tên và
 * Người đại diện được sửa riêng — người đại diện của hồ sơ nguồn KHÔNG được sao chép. Tên trùng → thông báo AC-3.
 */
function CopyCoopDialog({ coop, onClose }: { coop: Cooperative; onClose: () => void }) {
  const { adminApi } = useAuth();
  const [name, setName] = useState(`${coop.name} (bản sao)`);
  const [repName, setRepName] = useState("");
  const [repPhone, setRepPhone] = useState("");
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [done, setDone] = useState<CoopCopyResult | null>(null);
  const [key, setKey] = useState(newIdempotencyKey);
  const nameErr = !name.trim() ? "Nhập tên HTX." : name.trim().toLowerCase() === coop.name.trim().toLowerCase() ? "Tên HTX này đã tồn tại, vui lòng chọn tên khác." : null;
  const repErr = repName.trim() || repPhone.trim() ? (!repName.trim() ? "Nhập họ tên người đại diện." : !normalizePhone(repPhone) ? "SĐT phải có dạng 0xxxxxxxxx (10 chữ số) hoặc +84…" : null) : null;
  const touch = () => setKey(newIdempotencyKey());
  const submit = async () => {
    setTouched(true);
    if (nameErr || repErr) return;
    setBusy(true);
    setProblem(null);
    try {
      const representative = repName.trim() ? { full_name: repName.trim(), phone: normalizePhone(repPhone)! } : undefined;
      const r = await unwrap(
        adminApi.POST("/v1/cooperatives/{ref}/copy", { params: { path: { ref: coop.ref }, header: { "Idempotency-Key": key } }, body: { name: name.trim(), representative } }),
      );
      setDone(r);
    } catch (e) {
      setProblem(toApiProblem(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={`Sao chép hồ sơ ${coop.name}`}
      onClose={onClose}
      actions={
        done ? null : (
          <button type="button" className="btn btn-p btn-sm" disabled={busy} onClick={() => void submit()}>
            {busy ? "Đang lưu…" : "Lưu bản sao"}
          </button>
        )
      }
    >
      {done ? (
        <p role="status">
          Đã tạo <Link href={`/htx/${done.org.ref}`}>{done.org.name}</Link> (Mã {done.org.code}).
        </p>
      ) : (
        <div className="stack" style={{ gap: 10 }}>
          <p className="small mute">
            Giữ nguyên địa bàn ({coopPlace(coop)}), địa chỉ trụ sở, tổ chức cấp trên, diện tích đăng ký, ngày thành lập. Không sao chép Mã HTX (hệ thống sinh
            mới), mã số thuế, toạ độ trụ sở và người đại diện.
          </p>
          <div className="field">
            <label htmlFor="cp-coop-name">Tên HTX của bản sao</label>
            <input id="cp-coop-name" className="input" value={name} aria-invalid={touched && !!nameErr} onChange={(e) => { setName(e.target.value); touch(); }} />
            {touched && nameErr ? <span className="err small">{nameErr}</span> : null}
          </div>
          <div className="row" style={{ alignItems: "flex-start" }}>
            <div className="field" style={{ flex: "1 1 200px" }}>
              <label htmlFor="cp-rep-name">Người đại diện</label>
              <input id="cp-rep-name" className="input" value={repName} autoComplete="off" onChange={(e) => { setRepName(e.target.value); touch(); }} />
            </div>
            <div className="field" style={{ flex: "1 1 160px" }}>
              <label htmlFor="cp-rep-phone">SĐT người đại diện</label>
              <input id="cp-rep-phone" className="input" inputMode="tel" value={repPhone} autoComplete="off" onChange={(e) => { setRepPhone(e.target.value); touch(); }} />
            </div>
          </div>
          {touched && repErr ? <span className="err small">{repErr}</span> : null}
          <span className="small mute">Người đại diện và SĐT được lưu mã hoá trong kho định danh, không nằm trong hồ sơ HTX.</span>
          {problem ? <ProblemAlert problem={problem} /> : null}
        </div>
      )}
    </Dialog>
  );
}

export default function CooperativeDetailPage() {
  const params = useParams<{ id: string }>();
  const id = params?.id ?? "";
  const { api, principal } = useAuth();
  const [tab, setTab] = useState("overview");
  const [deactivating, setDeactivating] = useState(false);
  const [copying, setCopying] = useState(false);
  const coop = useAsync(() => getCooperative(api, id), [api, id]);
  const canDeactivate = hasRole(principal, ROLES.registryAdmin, ROLES.platformAdmin);
  const canCopy = hasRole(principal, ROLES.registryAdmin, ROLES.platformAdmin, ROLES.knProvinceAdmin);

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
            subtitle={`${coop.data.code} · ${coopPlace(coop.data)}`}
            actions={
              <>
                {coop.data.active ? <span className="chip c-ok">Đang hoạt động</span> : <span className="chip c-mute">Đã vô hiệu hoá</span>}
                {canCopy ? (
                  <button type="button" className="btn btn-o btn-sm" onClick={() => setCopying(true)}>
                    Sao chép hồ sơ
                  </button>
                ) : null}
                {coop.data.active && canDeactivate ? (
                  <button type="button" className="btn btn-o btn-sm" onClick={() => setDeactivating(true)}>
                    Vô hiệu hoá HTX
                  </button>
                ) : null}
              </>
            }
          />
          {copying ? <CopyCoopDialog coop={coop.data} onClose={() => setCopying(false)} /> : null}
          {deactivating ? (
            <DeactivateDialog
              coop={coop.data}
              onClose={() => setDeactivating(false)}
              onDone={() => {
                setDeactivating(false);
                coop.reload();
              }}
            />
          ) : null}
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
              {tab === "households" ? <HouseholdsPanel coop={coop.data} /> : null}
              {tab === "plots" ? <PlotsTab coop={coop.data} /> : null}
            </Tabs>
          </div>
        </>
      ) : null}
    </>
  );
}
