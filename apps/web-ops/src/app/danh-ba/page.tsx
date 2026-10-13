"use client";

import Link from "next/link";
import { useState, type FormEvent } from "react";
import { listChildUnits, unitResolver } from "@/api/adapters/units";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import type { ApiProblem } from "@/api/problem";
import type { AppGet } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading, PageHead } from "@/components/ui";
import { dialUri } from "@/lib/dial";
import { formatDateTime } from "@/lib/format";
import { toApiProblem, useAsync } from "@/lib/useAsync";

type Entry = AppGet<"/v1/extension/roster">["items"][number];

const DUTY: Record<Entry["duty_status"], { label: string; cls: string }> = {
  on_duty: { label: "Đang trực", cls: "c-ok" },
  off_duty: { label: "Tạm nghỉ", cls: "c-mute" },
  unknown: { label: "Không xác định", cls: "c-warn" },
};
const SOURCE: Record<Entry["status_source"], string> = { manual: "Tự khai", shift: "Theo ca", none: "—" };

/**
 * "Gọi/nhắn 1 chạm" (US-DIR-02): số điện thoại KHÔNG nạp sẵn cùng danh bạ (PII, R-04/R-14) —
 * bấm "Liên hệ" thì đọc GET /v1/extension/roster/{id}/contact (không lưu đệm) rồi hiện liên kết tel:/sms:.
 */
function ContactCell({ actor }: { actor: string }) {
  const { api } = useAuth();
  const [phone, setPhone] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<ApiProblem | null>(null);
  const load = async () => {
    setBusy(true);
    setErr(null);
    try {
      const r = await unwrap(api.GET("/v1/extension/roster/{id}/contact", { params: { path: { id: actor } } }));
      setPhone(r.phone);
    } catch (e) {
      setErr(toApiProblem(e));
    } finally {
      setBusy(false);
    }
  };
  if (phone) {
    return (
      <div className="row" style={{ gap: 6 }} data-testid={`contact-${actor}`}>
        <a className="btn btn-p btn-sm" href={dialUri("tel", phone)}>
          Gọi {phone}
        </a>
        <a className="btn btn-o btn-sm" href={dialUri("sms", phone)}>
          Nhắn tin
        </a>
      </div>
    );
  }
  return (
    <div className="stack" style={{ gap: 6 }}>
      <button type="button" className="btn btn-o btn-sm" disabled={busy} onClick={() => void load()}>
        {busy ? "Đang tra…" : "Liên hệ"}
      </button>
      {err ? <ProblemAlert problem={err} /> : null}
    </div>
  );
}

function RosterTable({ items, caption }: { items: Entry[]; caption: string }) {
  return (
    <div className="tbl-wrap">
      <table className="tbl">
        <caption className="sr-only">{caption}</caption>
        <thead>
          <tr>
            <th scope="col">Cán bộ</th>
            <th scope="col">Chức danh</th>
            <th scope="col">Đơn vị</th>
            <th scope="col">Trạng thái</th>
            <th scope="col">Nguồn</th>
            <th scope="col">Đến</th>
            <th scope="col">Liên hệ</th>
          </tr>
        </thead>
        <tbody>
          {items.map((e) => (
            <tr key={e.actor_ref} data-actor={e.actor_ref}>
              <td>
                <b>{e.display_name}</b>
              </td>
              <td>{e.role_title}</td>
              <td>
                <code className="small">{e.unit_code}</code>
              </td>
              <td>
                <span className={`chip ${DUTY[e.duty_status].cls}`}>{DUTY[e.duty_status].label}</span>
              </td>
              <td className="small">{SOURCE[e.status_source]}</td>
              <td className="small">{e.until ? formatDateTime(e.until) : "—"}</td>
              <td style={{ minWidth: 200 }}>
                <ContactCell actor={e.actor_ref} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** Cán bộ tự khai "Đang trực"/"Tạm nghỉ" (US-DIR-01); hết hạn theo extension.duty_status_ttl_hours. */
function MyStatus({ onChanged }: { onChanged: () => void }) {
  const { api } = useAuth();
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState<{ status: string; valid_until: string } | null>(null);
  const [err, setErr] = useState<ApiProblem | null>(null);
  const set = async (status: "on_duty" | "off_duty") => {
    setBusy(true);
    setErr(null);
    try {
      const r = await unwrap(
        api.POST("/v1/extension/roster/status", { params: { header: { "Idempotency-Key": newIdempotencyKey() } }, body: { status } }),
      );
      setDone({ status: r.status, valid_until: r.valid_until });
      onChanged();
    } catch (e) {
      setErr(toApiProblem(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <section className="card sec" style={{ marginBottom: 16 }} aria-labelledby="db-me">
      <div className="sec-h">
        <h2 id="db-me">Trạng thái trực của tôi</h2>
      </div>
      <div className="row">
        <button type="button" className="btn btn-p btn-sm" disabled={busy} onClick={() => void set("on_duty")}>
          Đang trực
        </button>
        <button type="button" className="btn btn-o btn-sm" disabled={busy} onClick={() => void set("off_duty")}>
          Tạm nghỉ
        </button>
        {done ? (
          <span className="small" role="status" style={{ color: "var(--g800)" }}>
            Đã cập nhật: {done.status === "on_duty" ? "Đang trực" : "Tạm nghỉ"} đến {formatDateTime(done.valid_until)}
          </span>
        ) : null}
      </div>
      {err ? <ProblemAlert problem={err} /> : null}
    </section>
  );
}

/** TTKN tỉnh/QG khai báo ca trực cho cán bộ trong phạm vi (US-DIR-03). */
function ShiftForm({ staff, onAdded }: { staff: Entry[]; onAdded: () => void }) {
  const { api } = useAuth();
  const [actor, setActor] = useState("");
  const [starts, setStarts] = useState("");
  const [ends, setEnds] = useState("");
  const [busy, setBusy] = useState(false);
  const [ok, setOk] = useState(false);
  const [err, setErr] = useState<ApiProblem | null>(null);
  const chosen = actor || staff[0]?.actor_ref || "";
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    setOk(false);
    try {
      await unwrap(
        api.POST("/v1/extension/roster/shifts", {
          params: { header: { "Idempotency-Key": newIdempotencyKey() } },
          body: { actor_ref: chosen, starts_at: new Date(starts).toISOString(), ends_at: new Date(ends).toISOString() },
        }),
      );
      setOk(true);
      onAdded();
    } catch (x) {
      setErr(toApiProblem(x));
    } finally {
      setBusy(false);
    }
  };
  return (
    <section className="card sec" style={{ marginTop: 16 }} aria-labelledby="db-shift">
      <div className="sec-h">
        <h2 id="db-shift">Khai báo ca trực</h2>
        <span className="mute small">Trong ca, trạng thái tự chuyển &quot;Đang trực&quot; (trừ khi cán bộ tự khai khác)</span>
      </div>
      <form className="row" style={{ alignItems: "flex-end" }} onSubmit={(e) => void submit(e)}>
        <div className="field">
          <label htmlFor="db-shift-who">Cán bộ</label>
          <select id="db-shift-who" className="select" value={chosen} onChange={(e) => setActor(e.target.value)}>
            {staff.map((s) => (
              <option key={s.actor_ref} value={s.actor_ref}>
                {s.display_name}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="db-shift-from">Bắt đầu</label>
          <input id="db-shift-from" className="input" type="datetime-local" required value={starts} onChange={(e) => setStarts(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="db-shift-to">Kết thúc</label>
          <input id="db-shift-to" className="input" type="datetime-local" required value={ends} onChange={(e) => setEnds(e.target.value)} />
        </div>
        <button type="submit" className="btn btn-p btn-sm" disabled={busy || !chosen}>
          Lưu ca trực
        </button>
      </form>
      {ok ? (
        <p className="small" role="status" style={{ color: "var(--g800)", marginTop: 8 }}>
          Đã lưu ca trực.
        </p>
      ) : null}
      {err ? <ProblemAlert problem={err} /> : null}
    </section>
  );
}

export default function RosterPage() {
  const { api, principal } = useAuth();
  const level = principal?.level ?? null;
  const commune = level === "xa";
  const scopeProvince = level === "tinh" ? (principal?.scopeUnits.find((u) => u.length === 2) ?? null) : null;
  const [province, setProvince] = useState("");
  const [unit, setUnit] = useState("");

  const provinces = useAsync(async () => {
    if (commune) return [];
    const all = await listChildUnits(api, null);
    return scopeProvince ? all.filter((p) => p.code === scopeProvince) : all;
  }, [api, commune, scopeProvince]);
  const chosenProvince = province || provinces.data?.[0]?.code || "";
  const communes = useAsync(async () => {
    if (commune) {
      const m = await unitResolver(api).many(principal?.scopeUnits ?? [], false);
      return (principal?.scopeUnits ?? []).map((c) => ({ code: c, name: m.get(c)?.name ?? c }));
    }
    return chosenProvince ? (await listChildUnits(api, chosenProvince)).map((u) => ({ code: u.code, name: u.name })) : [];
  }, [api, commune, chosenProvince, principal?.scopeUnits]);
  const chosen = commune ? unit || communes.data?.[0]?.code || "" : unit || chosenProvince;

  const roster = useAsync(
    () => (chosen ? unwrap(api.GET("/v1/extension/roster", { params: { query: { unit: chosen } } })) : Promise.resolve(null)),
    [api, chosen],
  );
  const selfService = hasRole(principal, ROLES.knOfficer, ROLES.knProvinceAdmin, ROLES.knNationalAdmin);
  const supervisor = hasRole(principal, ROLES.knProvinceAdmin, ROLES.knNationalAdmin, ROLES.platformAdmin);
  const r = roster.data;

  return (
    <>
      <PageHead title="Danh bạ trực" subtitle="Tra cứu cán bộ đang trực theo địa bàn, gọi hoặc nhắn tin một chạm" />
      {/* W11.23 (Q-072): danh bạ liên hệ dùng chung là Contact (pf/contact); màn này chỉ còn trạng thái trực. */}
      <p className="mute small" data-testid="contact-hint">
        Danh bạ liên hệ cán bộ, đơn vị các cấp: <Link href="/kn/dung-chung/contact">Dùng chung › Contact</Link>.
      </p>
      {selfService ? <MyStatus onChanged={roster.reload} /> : null}
      <section className="card sec" aria-labelledby="db-list">
        <div className="sec-h">
          <h2 id="db-list">Theo địa bàn</h2>
          {r ? (
            <span className="chip c-info" data-testid="on-duty-count">
              {r.on_duty} đang trực
            </span>
          ) : null}
        </div>
        <div className="row" style={{ alignItems: "flex-end", marginBottom: 12 }}>
          {!commune ? (
            <div className="field">
              <label htmlFor="db-province">Tỉnh</label>
              <select
                id="db-province"
                className="select"
                value={chosenProvince}
                onChange={(e) => {
                  setProvince(e.target.value);
                  setUnit("");
                }}
              >
                {(provinces.data ?? []).map((p) => (
                  <option key={p.code} value={p.code}>
                    {p.name}
                  </option>
                ))}
              </select>
            </div>
          ) : null}
          <div className="field">
            <label htmlFor="db-unit">Xã</label>
            <select id="db-unit" className="select" value={commune ? chosen : unit} onChange={(e) => setUnit(e.target.value)}>
              {!commune ? <option value="">Toàn tỉnh</option> : null}
              {(communes.data ?? []).map((c) => (
                <option key={c.code} value={c.code}>
                  {c.name}
                </option>
              ))}
            </select>
          </div>
        </div>
        {roster.error ? <ProblemAlert problem={roster.error} onRetry={roster.reload} /> : null}
        {roster.loading && !r ? <Loading /> : null}
        {r ? <RosterTable items={r.items} caption="Cán bộ trong địa bàn" /> : null}
        {r && r.items.length === 0 ? <p className="mute small">Địa bàn chưa có cán bộ trong danh bạ.</p> : null}
        {r && r.fallback.length > 0 ? (
          <div style={{ marginTop: 16 }} data-testid="roster-fallback">
            <div className="alert alert-info" role="status" style={{ marginBottom: 10 }}>
              <Icon name="info" />
              <span>Không ai đang trực ở xã này — liên hệ cán bộ TTKN tỉnh phụ trách.</span>
            </div>
            <RosterTable items={r.fallback} caption="Cán bộ TTKN tỉnh dự phòng" />
          </div>
        ) : null}
      </section>
      {supervisor && r && r.items.length > 0 ? <ShiftForm staff={r.items} onAdded={roster.reload} /> : null}
    </>
  );
}
