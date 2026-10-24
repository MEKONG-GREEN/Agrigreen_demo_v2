"use client";

// Báo cáo dịch bệnh BM-10 (W11.49, BRD CNTS v3.7 FN-11, UR-XA-10) — một màn cho hai cấp:
// - Cấp Xã: lập báo cáo (DB-01…DB-13), cập nhật diễn biến (mỗi lần một phiên bản), ghi nhận tin báo nghi dịch rồi xác nhận
//   thành ổ dịch hoặc bác bỏ.
// - Cấp Tỉnh: theo dõi ổ dịch toàn phạm vi (lọc trạng thái/bệnh/ngày, thống kê), xem diễn biến, xác nhận kết thúc / mở lại.
// Tọa độ lưu sẵn cho lớp bản đồ (GET /v1/extension/cnts/outbreak-points) — bản đồ vùng dịch là W11.52 (GĐ2).
import { useMemo, useState } from "react";
import { unwrap } from "@/api/client";
import { ApiProblem } from "@/api/problem";
import type { CatalogItem } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, dt, useAction, useCommuneNames, useLoad } from "@/kn/cycle/common";
import {
  EVENT_LABEL,
  SOURCE_LABEL,
  STATUS_LABEL,
  STATUS_TONE,
  SUSPECT_LABEL,
  createOutbreakApi,
  emptyForm,
  formErrors,
  formOf,
  payload,
  toLocalInput,
  type Outbreak,
  type OutbreakApi,
  type OutbreakForm,
  type OutbreakStatus,
  type Suspect,
} from "./api";

type Mode = { kind: "new" } | { kind: "progress"; o: Outbreak } | { kind: "confirm"; s: Suspect };

const ymd = (iso: string) => iso.slice(0, 10).split("-").reverse().join("/");

function problemFields(e: unknown): Record<string, string> {
  const m: Record<string, string> = {};
  if (e instanceof ApiProblem) for (const x of e.problem.errors ?? []) if (x.field) m[x.field] = x.reason ?? "";
  return m;
}

export function OutbreakScreen() {
  const { api, kn } = useAuth();
  const oapi = useMemo(() => (api ? createOutbreakApi(api) : null), [api]);
  const role = kn.context?.role;
  const xa = role === "kn_xa" || role === "admin";
  const tinh = role === "kn_tinh" || role === "admin";
  const readOnly = !!kn.context?.read_only;
  const myCommunes = (kn.context?.communes ?? []).map((c) => c.code);

  const [status, setStatus] = useState<OutbreakStatus | "">("");
  const [disease, setDisease] = useState("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const list = useLoad(
    oapi ? () => oapi.list({ ...(status ? { status } : {}), ...(disease ? { disease } : {}), ...(from ? { from } : {}), ...(to ? { to } : {}) }) : null,
    [oapi, status, disease, from, to],
  );
  const suspects = useLoad(oapi ? () => oapi.suspects() : null, [oapi]);
  const catalog = (name: "cnts_disease" | "cnts_species") => async () =>
    (await unwrap(api!.GET("/v1/masterdata/catalogs/{catalog}", { params: { path: { catalog: name }, query: {} } }))).items as CatalogItem[];
  const diseases = useLoad(api ? catalog("cnts_disease") : null, [api]);
  const species = useLoad(api ? catalog("cnts_species") : null, [api]);
  const communeName = useCommuneNames([...(list.data?.items ?? []).map((o) => o.commune_code), ...(suspects.data ?? []).map((s) => s.commune_code)]);

  const [open, setOpen] = useState<string | null>(null);
  const [mode, setMode] = useState<Mode | null>(null);
  const [newSuspect, setNewSuspect] = useState(false);

  if (!oapi) return null;
  const reload = () => {
    list.reload();
    suspects.reload();
  };
  return (
    <div className="page">
      <PageHeader
        title="Báo cáo dịch bệnh"
        actions={
          xa && !readOnly && role === "kn_xa" ? (
            <div className="row">
              <button className="btn btn-primary" onClick={() => setMode({ kind: "new" })}>
                Lập báo cáo dịch bệnh
              </button>
              <button className="btn" onClick={() => setNewSuspect(true)}>
                Ghi nhận tin báo nghi dịch
              </button>
            </div>
          ) : null
        }
      >
        <p className="mute">
          {role === "kn_xa"
            ? "Báo cáo ngay khi phát hiện (BM-10); cập nhật diễn biến theo ngày đến khi Cấp Tỉnh xác nhận kết thúc."
            : "Ổ dịch các xã gửi (FN-11): theo dõi diễn biến, xác nhận kết thúc. Tọa độ sẵn sàng cho bản đồ vùng dịch (giai đoạn 2)."}
        </p>
      </PageHeader>

      {mode ? (
        <OutbreakEditor
          key={mode.kind === "progress" ? mode.o.version_id : mode.kind === "confirm" ? mode.s.version_id : "new"}
          mode={mode}
          oapi={oapi}
          communes={myCommunes}
          communeName={communeName}
          diseases={diseases.data ?? []}
          species={species.data ?? []}
          onDone={(ref) => {
            setMode(null);
            reload();
            if (ref) setOpen(ref);
          }}
        />
      ) : null}
      {newSuspect ? (
        <SuspectEditor
          oapi={oapi}
          communes={myCommunes}
          communeName={communeName}
          species={species.data ?? []}
          onDone={() => {
            setNewSuspect(false);
            suspects.reload();
          }}
        />
      ) : null}

      <section className="card sec" aria-label="Ổ dịch">
        <h2>Ổ dịch</h2>
        <div className="row">
          <div className="row" role="tablist" aria-label="Trạng thái">
            <button role="tab" aria-selected={status === ""} className={`btn btn-sm${status === "" ? " btn-primary" : ""}`} onClick={() => setStatus("")}>
              Tất cả
            </button>
            {(list.data?.by_status ?? []).map((c) => (
              <button
                key={c.key}
                role="tab"
                aria-selected={status === c.key}
                className={`btn btn-sm${status === c.key ? " btn-primary" : ""}`}
                onClick={() => setStatus(c.key as OutbreakStatus)}
              >
                {c.label} ({c.count})
              </button>
            ))}
          </div>
          <select className="select" aria-label="Lọc bệnh" value={disease} onChange={(e) => setDisease(e.target.value)}>
            <option value="">Mọi bệnh</option>
            {(diseases.data ?? []).map((d) => (
              <option key={d.code} value={d.code}>
                {d.name}
              </option>
            ))}
          </select>
          <label className="row small">
            Tiếp nhận từ <input className="input" type="date" aria-label="Từ ngày" value={from} onChange={(e) => setFrom(e.target.value)} />
          </label>
          <label className="row small">
            đến <input className="input" type="date" aria-label="Đến ngày" value={to} onChange={(e) => setTo(e.target.value)} />
          </label>
        </div>
        <LoadState s={list}>
          {(d) => (
            <>
              {d.by_disease.length ? (
                <p className="mute small">Theo bệnh: {d.by_disease.map((c) => `${c.label} ${c.count}`).join(" · ")}</p>
              ) : null}
              <table className="dt" aria-label="Danh sách ổ dịch">
                <thead>
                  <tr>
                    <th>Bệnh</th>
                    <th>Vật nuôi</th>
                    <th>Xã</th>
                    <th>Tiếp nhận</th>
                    <th>Diễn biến đến</th>
                    <th>Chết / quy mô</th>
                    <th>Trạng thái</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {d.items.map((o) => (
                    <tr key={o.ref}>
                      <td>{o.disease_name}</td>
                      <td>{o.species_name}</td>
                      <td>{communeName(o.commune_code)}</td>
                      <td>{dt(o.received_at)}</td>
                      <td>{ymd(o.as_of)}</td>
                      <td>
                        {o.dead} / {o.herd_size}
                      </td>
                      <td>
                        <StatusPill tone={STATUS_TONE[o.status]}>{STATUS_LABEL[o.status]}</StatusPill>
                        <span className="mute"> · v{o.version}</span>
                      </td>
                      <td>
                        <button className="btn btn-sm" onClick={() => setOpen(o.ref)}>
                          Xem
                        </button>
                      </td>
                    </tr>
                  ))}
                  {d.items.length === 0 ? (
                    <tr>
                      <td colSpan={8} className="mute">
                        Không có ổ dịch.
                      </td>
                    </tr>
                  ) : null}
                </tbody>
              </table>
            </>
          )}
        </LoadState>
      </section>

      {open ? (
        <OutbreakDetailView
          key={open}
          oref={open}
          oapi={oapi}
          canUpdate={xa && role === "kn_xa" && !readOnly}
          canClose={tinh && role === "kn_tinh" && !readOnly}
          communeName={communeName}
          onUpdate={(o) => setMode({ kind: "progress", o })}
          onChanged={list.reload}
          onClose={() => setOpen(null)}
        />
      ) : null}

      <section className="card sec" aria-label="Tin báo nghi dịch">
        <h2>Tin báo nghi dịch</h2>
        <LoadState s={suspects}>
          {(ss) => (
            <SuspectTable
              items={ss}
              oapi={oapi}
              canAct={role === "kn_xa" && !readOnly}
              communeName={communeName}
              speciesName={(c) => species.data?.find((x) => x.code === c)?.name ?? c}
              onConfirm={(s) => setMode({ kind: "confirm", s })}
              onOpenOutbreak={setOpen}
              onChanged={suspects.reload}
            />
          )}
        </LoadState>
      </section>
    </div>
  );
}

function OutbreakDetailView({
  oref,
  oapi,
  canUpdate,
  canClose,
  communeName,
  onUpdate,
  onChanged,
  onClose,
}: {
  oref: string;
  oapi: OutbreakApi;
  canUpdate: boolean;
  canClose: boolean;
  communeName: (c: string) => string;
  onUpdate: (o: Outbreak) => void;
  onChanged: () => void;
  onClose: () => void;
}) {
  const detail = useLoad(() => oapi.get(oref), [oapi, oref]);
  const act = useAction();
  const [reason, setReason] = useState("");
  return (
    <section className="card sec" aria-label="Chi tiết ổ dịch">
      <LoadState s={detail}>
        {(d) => {
          const o = d.current;
          const setStatus = async (kind: "close" | "reopen") => {
            if (!reason.trim()) {
              act.setNote(null);
              return;
            }
            const r = await act.run(() => (kind === "close" ? oapi.close(o.ref, o.version, reason) : oapi.reopen(o.ref, o.version, reason)),
              kind === "close" ? "Đã xác nhận kết thúc ổ dịch." : "Đã mở lại ổ dịch.");
            if (r) {
              setReason("");
              detail.reload();
              onChanged();
            }
          };
          return (
            <>
              <h2>
                {o.disease_name} — {o.species_name} · {communeName(o.commune_code)}{" "}
                <StatusPill tone={STATUS_TONE[o.status]}>{STATUS_LABEL[o.status]}</StatusPill>
              </h2>
              {act.view}
              <dl className="kv">
                <dt>Tiếp nhận (DB-02)</dt>
                <dd>
                  {dt(o.received_at)} · {SOURCE_LABEL[o.source]}
                  {o.suspect_ref ? " · từ tin báo nghi dịch" : ""}
                </dd>
                <dt>Địa bàn (DB-04)</dt>
                <dd>{o.locality ?? "—"}</dd>
                <dt>Vị trí (DB-05)</dt>
                <dd>
                  {o.location ? `${o.location.lat}, ${o.location.lon}` : "—"}
                  {o.location?.accuracy_m ? ` (±${o.location.accuracy_m} m)` : ""}
                </dd>
                <dt>Quy mô đàn (DB-06)</dt>
                <dd>
                  {o.herd_size} con{o.herd_age_days !== null ? `, ${o.herd_age_days} ngày tuổi` : ""}
                </dd>
                <dt>Số con chết (DB-07)</dt>
                <dd>{o.dead}</dd>
                <dt>Triệu chứng (DB-08)</dt>
                <dd>{o.symptoms}</dd>
                <dt>Xét nghiệm (DB-09)</dt>
                <dd>{[o.sample_date ? ymd(o.sample_date) : null, o.lab_name, o.lab_result].filter(Boolean).join(" · ") || "—"}</dd>
                <dt>Xử lý (DB-10)</dt>
                <dd>
                  Tiêu hủy {o.culled_head ?? 0} con{o.culled_kg ? `, ${o.culled_kg} kg` : ""}; hóa chất {o.chemical_liters ?? "0"} lít
                </dd>
                <dt>Vùng đệm, vùng bị uy hiếp (DB-11)</dt>
                <dd>{o.buffer_note ?? "—"}</dd>
                <dt>Phòng chống, kiến nghị (DB-12)</dt>
                <dd>{o.prevention_note ?? "—"}</dd>
              </dl>
              <h3>Diễn biến ({d.versions.length} phiên bản)</h3>
              <table className="dt" aria-label="Diễn biến">
                <thead>
                  <tr>
                    <th>Phiên bản</th>
                    <th>Sự việc</th>
                    <th>Ngày</th>
                    <th>Chết</th>
                    <th>Tiêu hủy</th>
                    <th>Ghi chú</th>
                    <th>Người, lúc</th>
                  </tr>
                </thead>
                <tbody>
                  {d.versions.map((v) => (
                    <tr key={v.version_id}>
                      <td>v{v.version}</td>
                      <td>{EVENT_LABEL[v.event]}</td>
                      <td>{ymd(v.as_of)}</td>
                      <td>{v.dead}</td>
                      <td>{v.culled_head ?? "—"}</td>
                      <td>{v.status_reason ?? v.progress_note ?? "—"}</td>
                      <td>
                        {v.actor.display ?? v.actor.ref.slice(0, 8)} · {dt(v.recorded_at)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              <div className="row">
                {canUpdate && o.status !== "closed" ? (
                  <button className="btn btn-primary" onClick={() => onUpdate(o)}>
                    Cập nhật diễn biến
                  </button>
                ) : null}
                {canClose ? (
                  <>
                    <input
                      className="input"
                      aria-label="Lý do"
                      placeholder={o.status === "closed" ? "Lý do mở lại" : "Lý do kết thúc (vd hết 21 ngày không phát sinh)"}
                      maxLength={1000}
                      value={reason}
                      onChange={(e) => setReason(e.target.value)}
                    />
                    {o.status === "closed" ? (
                      <button className="btn" disabled={act.busy || !reason.trim()} onClick={() => setStatus("reopen")}>
                        Mở lại
                      </button>
                    ) : (
                      <button className="btn btn-primary" disabled={act.busy || !reason.trim()} onClick={() => setStatus("close")}>
                        Xác nhận kết thúc
                      </button>
                    )}
                  </>
                ) : null}
                <button className="btn" onClick={onClose}>
                  Đóng
                </button>
              </div>
            </>
          );
        }}
      </LoadState>
    </section>
  );
}

function SuspectTable({
  items,
  oapi,
  canAct,
  communeName,
  speciesName,
  onConfirm,
  onOpenOutbreak,
  onChanged,
}: {
  items: Suspect[];
  oapi: OutbreakApi;
  canAct: boolean;
  communeName: (c: string) => string;
  speciesName: (c: string) => string;
  onConfirm: (s: Suspect) => void;
  onOpenOutbreak: (ref: string) => void;
  onChanged: () => void;
}) {
  const act = useAction();
  const [dismissing, setDismissing] = useState<string | null>(null);
  const [reason, setReason] = useState("");
  return (
    <>
      {act.view}
      <table className="dt" aria-label="Danh sách tin báo nghi dịch">
        <thead>
          <tr>
            <th>Phát hiện</th>
            <th>Xã, địa bàn</th>
            <th>Vật nuôi</th>
            <th>Mô tả</th>
            <th>Vị trí</th>
            <th>Trạng thái</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {items.map((s) => (
            <tr key={s.ref}>
              <td>
                {dt(s.observed_at)} · {SOURCE_LABEL[s.reporter_kind]}
              </td>
              <td>
                {communeName(s.commune_code)}
                {s.locality ? ` · ${s.locality}` : ""}
              </td>
              <td>{s.species_code ? speciesName(s.species_code) : "—"}</td>
              <td>{s.description}</td>
              <td>{s.location ? "Có" : "Chưa có"}</td>
              <td>
                {SUSPECT_LABEL[s.status]}
                {s.status_reason ? <span className="mute"> · {s.status_reason}</span> : null}
              </td>
              <td>
                {s.status === "confirmed" && s.outbreak_ref ? (
                  <button className="btn btn-sm" onClick={() => onOpenOutbreak(s.outbreak_ref!)}>
                    Xem ổ dịch
                  </button>
                ) : null}
                {canAct && s.status === "reported" ? (
                  dismissing === s.ref ? (
                    <span className="row">
                      <input className="input" aria-label="Lý do bác bỏ" maxLength={1000} value={reason} onChange={(e) => setReason(e.target.value)} />
                      <button
                        className="btn btn-sm"
                        disabled={act.busy || !reason.trim()}
                        onClick={async () => {
                          if (await act.run(() => oapi.dismiss(s.ref, s.version, reason), "Đã bác bỏ tin báo.")) {
                            setDismissing(null);
                            setReason("");
                            onChanged();
                          }
                        }}
                      >
                        Bác bỏ
                      </button>
                    </span>
                  ) : (
                    <span className="row">
                      <button className="btn btn-sm btn-primary" onClick={() => onConfirm(s)}>
                        Xác nhận → lập BM-10
                      </button>
                      <button className="btn btn-sm" onClick={() => setDismissing(s.ref)}>
                        Bác bỏ…
                      </button>
                    </span>
                  )
                ) : null}
              </td>
            </tr>
          ))}
          {items.length === 0 ? (
            <tr>
              <td colSpan={7} className="mute">
                Không có tin báo.
              </td>
            </tr>
          ) : null}
        </tbody>
      </table>
    </>
  );
}

function useGps(set: (lat: string, lon: string, acc: string) => void) {
  const [msg, setMsg] = useState<string | null>(null);
  const locate = () => {
    if (typeof navigator === "undefined" || !navigator.geolocation) {
      setMsg("Trình duyệt không hỗ trợ định vị — nhập tọa độ.");
      return;
    }
    setMsg("Đang lấy vị trí…");
    navigator.geolocation.getCurrentPosition(
      (p) => {
        set(p.coords.latitude.toFixed(7), p.coords.longitude.toFixed(7), p.coords.accuracy.toFixed(1));
        setMsg(null);
      },
      () => setMsg("Không lấy được vị trí — cấp quyền định vị hoặc nhập tọa độ."),
      { enableHighAccuracy: true, timeout: 15000 },
    );
  };
  return { locate, msg };
}

function OutbreakEditor({
  mode,
  oapi,
  communes,
  communeName,
  diseases,
  species,
  onDone,
}: {
  mode: Mode;
  oapi: OutbreakApi;
  communes: string[];
  communeName: (c: string) => string;
  diseases: CatalogItem[];
  species: CatalogItem[];
  onDone: (ref: string | null) => void;
}) {
  const init = (): OutbreakForm => {
    if (mode.kind === "progress") return formOf(mode.o);
    if (mode.kind === "confirm") {
      const s = mode.s;
      return {
        ...emptyForm(s.commune_code), species_code: s.species_code ?? "", disease_code: s.disease_code ?? "", received_at: toLocalInput(s.observed_at),
        source: s.reporter_kind, locality: s.locality ?? "", lat: s.location?.lat ?? "", lon: s.location?.lon ?? "", accuracy_m: s.location?.accuracy_m ?? "",
        dead: String(s.dead ?? 0), symptoms: s.description,
      };
    }
    return emptyForm(communes.length === 1 ? communes[0] : "");
  };
  const [f, setF] = useState<OutbreakForm>(init);
  const [errs, setErrs] = useState<Record<string, string>>({});
  const [conflict, setConflict] = useState<string | null>(null);
  const act = useAction();
  const up = (patch: Partial<OutbreakForm>) => setF((x) => ({ ...x, ...patch }));
  const gps = useGps((lat, lon, acc) => up({ lat, lon, accuracy_m: acc, location_source: "gps" }));
  const dz = diseases.find((d) => d.code === f.disease_code);
  const allowed = (dz?.attrs.species ?? "").split(",").map((x) => x.trim()).filter(Boolean);
  const speciesOpts = allowed.length ? species.filter((s) => allowed.includes(s.code)) : species;
  const title = mode.kind === "new" ? "Lập báo cáo dịch bệnh (BM-10)" : mode.kind === "progress" ? `Cập nhật diễn biến — phiên bản ${mode.o.version + 1}` : "Xác nhận tin báo → báo cáo dịch bệnh";

  const submit = async () => {
    const fe = formErrors(f);
    setErrs(fe);
    if (Object.keys(fe).length) return;
    try {
      // P0-WEB-IDEMKEY: trong phiên thao tác của act — gửi lại sau lỗi mạng dùng lại khóa cũ.
      const o = await act.idem(() =>
        mode.kind === "new"
          ? oapi.report(payload(f))
          : mode.kind === "progress"
            ? oapi.progress(mode.o.ref, payload(f, mode.o.version))
            : oapi.confirm(mode.s.ref, payload(f, mode.s.version)),
      );
      onDone(o.ref);
    } catch (e) {
      if (e instanceof ApiProblem && (e.problem.code === "extension.cnts_outbreak_version_conflict" || e.problem.code === "extension.cnts_suspect_version_conflict")) {
        setConflict(e.problem.detail ?? e.problem.title);
        return;
      }
      setErrs(problemFields(e));
      await act.run(() => Promise.reject(e));
    }
  };

  const field = (key: keyof OutbreakForm, label: string, opts: { req?: boolean; type?: string; area?: boolean; ph?: string; err?: string } = {}) => (
    <label className="field">
      <span>
        {label}
        {opts.req ? <span className="req"> *</span> : null}
      </span>
      {opts.area ? (
        <textarea className="input" aria-label={label} maxLength={2000} value={f[key]} onChange={(e) => up({ [key]: e.target.value } as Partial<OutbreakForm>)} />
      ) : (
        <input
          className="input"
          aria-label={label}
          type={opts.type ?? "text"}
          placeholder={opts.ph}
          value={f[key]}
          onChange={(e) => up({ [key]: e.target.value } as Partial<OutbreakForm>)}
        />
      )}
      {errs[opts.err ?? key] ? <span className="err">{errs[opts.err ?? key]}</span> : null}
    </label>
  );

  return (
    <section className="card sec" aria-label="Nhập báo cáo dịch bệnh">
      <h2>{title}</h2>
      {act.view}
      {conflict ? (
        <div className="alert alert-warn" role="alert">
          <span>{conflict}</span>
          <button className="btn btn-sm" onClick={() => onDone(mode.kind === "progress" ? mode.o.ref : null)}>
            Tải bản mới
          </button>
        </div>
      ) : null}
      <div className="ws-two">
        <label className="field">
          <span>
            Xã<span className="req"> *</span>
          </span>
          <select className="select" aria-label="Xã" disabled={mode.kind !== "new"} value={f.commune_code} onChange={(e) => up({ commune_code: e.target.value })}>
            <option value="">—</option>
            {(mode.kind === "new" ? communes : [f.commune_code]).map((c) => (
              <option key={c} value={c}>
                {communeName(c)}
              </option>
            ))}
          </select>
          {errs.commune_code ? <span className="err">{errs.commune_code}</span> : null}
        </label>
        <label className="field">
          <span>
            DB-01 Bệnh<span className="req"> *</span>
          </span>
          <select className="select" aria-label="Bệnh" value={f.disease_code} onChange={(e) => up({ disease_code: e.target.value })}>
            <option value="">—</option>
            {diseases.map((d) => (
              <option key={d.code} value={d.code}>
                {d.name}
              </option>
            ))}
          </select>
          {errs.disease_code ? <span className="err">{errs.disease_code}</span> : null}
        </label>
        <label className="field">
          <span>
            Vật nuôi<span className="req"> *</span>
          </span>
          <select className="select" aria-label="Vật nuôi" value={f.species_code} onChange={(e) => up({ species_code: e.target.value })}>
            <option value="">—</option>
            {speciesOpts.map((s) => (
              <option key={s.code} value={s.code}>
                {s.name}
              </option>
            ))}
          </select>
          {errs.species_code ? <span className="err">{errs.species_code}</span> : null}
        </label>
        {field("received_at", "DB-02 Thời điểm tiếp nhận thông tin", { req: true, type: "datetime-local" })}
        <label className="field">
          <span>
            DB-03 Nguồn tin<span className="req"> *</span>
          </span>
          <select className="select" aria-label="Nguồn tin" value={f.source} onChange={(e) => up({ source: e.target.value as OutbreakForm["source"] })}>
            <option value="">—</option>
            {Object.entries(SOURCE_LABEL).map(([k, v]) => (
              <option key={k} value={k}>
                {v}
              </option>
            ))}
          </select>
          {errs.source ? <span className="err">{errs.source}</span> : null}
        </label>
        {field("locality", "DB-04 Hộ chăn nuôi — địa bàn (ấp, khu vực)", { req: true, ph: "Không ghi họ tên, số điện thoại" })}
      </div>
      <fieldset className="field">
        <legend>
          DB-05 Vị trí ổ dịch<span className="req"> *</span>
        </legend>
        <div className="row">
          {field("lat", "Vĩ độ", { ph: "10.25341", err: "location" })}
          {field("lon", "Kinh độ", { ph: "105.9721", err: "location" })}
          {field("accuracy_m", "Sai số (m)")}
          <button className="btn btn-sm" type="button" onClick={gps.locate}>
            Lấy GPS
          </button>
        </div>
        {gps.msg ? <p className="mute small">{gps.msg}</p> : null}
        <p className="mute small">Chọn trên bản đồ: giai đoạn 2 (bản đồ vùng dịch). Có thể nhập tọa độ đọc từ bản đồ khác (nguồn = bản đồ).</p>
        <label className="row small">
          <input type="checkbox" checked={f.location_source === "map"} onChange={(e) => up({ location_source: e.target.checked ? "map" : "gps" })} /> Tọa độ chọn trên bản đồ
        </label>
      </fieldset>
      <div className="ws-two">
        {field("herd_size", "DB-06 Quy mô đàn (con)", { req: true })}
        {field("herd_age_days", "Ngày tuổi")}
        {field("dead", "DB-07 Số con chết (lũy kế)", { req: true })}
        {field("symptoms", "DB-08 Triệu chứng", { req: true, area: true })}
        {field("sample_date", "DB-09 Ngày lấy mẫu", { type: "date" })}
        {field("lab_name", "Nơi xét nghiệm")}
        {field("lab_result", "Kết quả xét nghiệm", { area: true })}
        {field("culled_head", "DB-10 Tiêu hủy (con, lũy kế)")}
        {field("culled_kg", "Tiêu hủy (kg)")}
        {field("chemical_liters", "Hóa chất (lít)")}
        {field("buffer_note", "DB-11 Vùng đệm, vùng bị uy hiếp", { area: true })}
        {field("prevention_note", "DB-12 Công tác phòng chống; kiến nghị", { area: true })}
        {mode.kind === "progress" ? field("as_of", "Ngày diễn biến (trống = hôm nay)", { type: "date" }) : null}
        {mode.kind === "progress" ? field("progress_note", "Diễn biến lần này", { area: true }) : null}
      </div>
      <p className="mute small">DB-13 Đính kèm (ảnh, PDF): tải tệp từ ứng dụng di động; web hiển thị mã tệp đã gắn.</p>
      <div className="row">
        <button className="btn btn-primary" disabled={act.busy} onClick={submit}>
          {mode.kind === "progress" ? "Lưu diễn biến" : "Gửi Cấp Tỉnh"}
        </button>
        <button className="btn" onClick={() => onDone(null)}>
          Hủy
        </button>
      </div>
    </section>
  );
}

function SuspectEditor({
  oapi,
  communes,
  communeName,
  species,
  onDone,
}: {
  oapi: OutbreakApi;
  communes: string[];
  communeName: (c: string) => string;
  species: CatalogItem[];
  onDone: () => void;
}) {
  const [s, setS] = useState({
    commune_code: communes.length === 1 ? communes[0]! : "",
    reporter_kind: "nong_ho" as Suspect["reporter_kind"],
    observed_at: toLocalInput(new Date().toISOString()),
    species_code: "",
    locality: "",
    lat: "",
    lon: "",
    accuracy_m: "",
    affected: "",
    dead: "",
    description: "",
  });
  const [errs, setErrs] = useState<Record<string, string>>({});
  const act = useAction();
  const up = (p: Partial<typeof s>) => setS((x) => ({ ...x, ...p }));
  const gps = useGps((lat, lon, accuracy_m) => up({ lat, lon, accuracy_m }));
  const submit = async () => {
    const e: Record<string, string> = {};
    if (!s.commune_code) e.commune_code = "Chọn xã";
    if (!s.locality.trim()) e.locality = "Ghi địa bàn";
    if (!s.description.trim()) e.description = "Bắt buộc";
    setErrs(e);
    if (Object.keys(e).length) return;
    const n = (v: string) => (v.trim() === "" ? null : Number(v));
    const r = await act.run(() =>
      oapi.reportSuspect({
        commune_code: s.commune_code, reporter_kind: s.reporter_kind, observed_at: new Date(s.observed_at).toISOString(), species_code: s.species_code || undefined,
        locality: s.locality.trim(), location: s.lat && s.lon ? { lat: s.lat, lon: s.lon, accuracy_m: s.accuracy_m || null } : null,
        affected: n(s.affected), dead: n(s.dead), description: s.description.trim(),
      }),
    );
    if (r) onDone();
    else if (act.problem) setErrs(problemFields(act.problem));
  };
  return (
    <section className="card sec" aria-label="Ghi nhận tin báo nghi dịch">
      <h2>Ghi nhận tin báo nghi dịch</h2>
      {act.view}
      <div className="ws-two">
        <label className="field">
          <span>Xã *</span>
          <select className="select" aria-label="Xã tin báo" value={s.commune_code} onChange={(e) => up({ commune_code: e.target.value })}>
            <option value="">—</option>
            {communes.map((c) => (
              <option key={c} value={c}>
                {communeName(c)}
              </option>
            ))}
          </select>
          {errs.commune_code ? <span className="err">{errs.commune_code}</span> : null}
        </label>
        <label className="field">
          <span>Người báo</span>
          <select className="select" aria-label="Người báo" value={s.reporter_kind} onChange={(e) => up({ reporter_kind: e.target.value as Suspect["reporter_kind"] })}>
            {Object.entries(SOURCE_LABEL).map(([k, v]) => (
              <option key={k} value={k}>
                {v}
              </option>
            ))}
          </select>
        </label>
        <label className="field">
          <span>Thời điểm phát hiện</span>
          <input className="input" type="datetime-local" aria-label="Thời điểm phát hiện" value={s.observed_at} onChange={(e) => up({ observed_at: e.target.value })} />
        </label>
        <label className="field">
          <span>Vật nuôi</span>
          <select className="select" aria-label="Vật nuôi tin báo" value={s.species_code} onChange={(e) => up({ species_code: e.target.value })}>
            <option value="">—</option>
            {species.map((x) => (
              <option key={x.code} value={x.code}>
                {x.name}
              </option>
            ))}
          </select>
        </label>
        <label className="field">
          <span>Địa bàn (ấp, khu vực) *</span>
          <input className="input" aria-label="Địa bàn tin báo" placeholder="Không ghi họ tên, số điện thoại" value={s.locality} onChange={(e) => up({ locality: e.target.value })} />
          {errs.locality ? <span className="err">{errs.locality}</span> : null}
        </label>
        <label className="field">
          <span>Số con có biểu hiện / chết</span>
          <span className="row">
            <input className="input" aria-label="Số con có biểu hiện" value={s.affected} onChange={(e) => up({ affected: e.target.value })} />
            <input className="input" aria-label="Số con chết tin báo" value={s.dead} onChange={(e) => up({ dead: e.target.value })} />
          </span>
        </label>
      </div>
      <label className="field">
        <span>Mô tả *</span>
        <textarea className="input" aria-label="Mô tả tin báo" maxLength={2000} value={s.description} onChange={(e) => up({ description: e.target.value })} />
        {errs.description ? <span className="err">{errs.description}</span> : null}
      </label>
      <div className="row">
        <input className="input" aria-label="Vĩ độ tin báo" placeholder="Vĩ độ" value={s.lat} onChange={(e) => up({ lat: e.target.value })} />
        <input className="input" aria-label="Kinh độ tin báo" placeholder="Kinh độ" value={s.lon} onChange={(e) => up({ lon: e.target.value })} />
        <button className="btn btn-sm" type="button" onClick={gps.locate}>
          Lấy GPS
        </button>
        {gps.msg ? <span className="mute small">{gps.msg}</span> : null}
      </div>
      <div className="row">
        <button className="btn btn-primary" disabled={act.busy} onClick={submit}>
          Ghi nhận
        </button>
        <button className="btn" onClick={onDone}>
          Hủy
        </button>
      </div>
    </section>
  );
}
