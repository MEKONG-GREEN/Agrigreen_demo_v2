"use client";

import { useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import { collectPages } from "@/api/pagination";
import { newIdempotencyKey } from "@/api/idempotency";
import { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { useAsync } from "@/lib/useAsync";
import { canCreateOrg, unitValidOn, vnToday } from "./orgCreate";
import { createProvisional, normalizeMST } from "./htxAnchors";

export function NewAnchorForm({ onCreated }: { onCreated: (id: string) => void }) {
  const { principal } = useAuth();
  return canCreateOrg(principal?.roles) ? <Form onCreated={onCreated} /> : null;
}

function Form({ onCreated }: { onCreated: (id: string) => void }) {
  const { api, adminApi, principal } = useAuth();
  const [name, setName] = useState("");
  const [province, setProvince] = useState("");
  const [commune, setCommune] = useState("");
  const [mst, setMst] = useState("");
  const [agreement, setAgreement] = useState("");
  const [busy, setBusy] = useState(false);
  const [retryOnly, setRetryOnly] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [failure, setFailure] = useState(false);
  const [idem, setIdem] = useState(newIdempotencyKey);
  const day = vnToday();
  const provinces = useAsync(() => collectPages(cursor => unwrap(api.GET("/v1/masterdata/admin-units", { params: { query: { at: day, cursor, limit: 200 } } }))), [api, day]);
  const communes = useAsync(() => province ? collectPages(cursor => unwrap(api.GET("/v1/masterdata/admin-units", { params: { query: { parent: province, at: day, cursor, limit: 200 } } }))) : Promise.resolve([]), [api, province, day]);
  const validCommunes = (communes.data ?? []).filter(u => u.level === "commune" && u.parent_code === province && unitValidOn(u, day));
  const validMST = !mst.trim() || normalizeMST(mst) !== null;
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!name.trim() || !validMST || !validCommunes.some(u => u.code === commune)) return;
    setBusy(true); setProblem(null); setFailure(false);
    try {
      const a = await createProvisional(adminApi, { created_by: principal?.roles.includes("platform_admin") ? "mkg" : "kn", mst: mst.trim() || undefined, agreement_ref: agreement.trim() || undefined, new_cooperative: { name: name.trim(), admin_unit_code: commune } }, idem);
      setIdem(newIdempotencyKey()); setRetryOnly(false); setName(""); setMst(""); setAgreement(""); onCreated(a.anchor_id);
    } catch (err) {
      const p = err instanceof ApiProblem ? err : null;
      setProblem(p); setFailure(!p);
      if (!p || p.code === "network_error" || p.status >= 500) setRetryOnly(true);
    } finally { setBusy(false); }
  };
  return <form className="card sec stack" aria-label="Tạo HTX và lập neo" onSubmit={e => void submit(e)}>
    <h2 className="h3">Tạo HTX và lập neo</h2>
    <p className="muted small">Tạo hồ sơ HTX tạm và neo theo mã số thuế trong cùng luồng. Dữ liệu mang nhãn “Chưa xác nhận” tới khi HTX nhận quyền.</p>
    {problem && <ProblemAlert problem={problem} />}
    {failure && <p role="alert">Không nhận được kết quả. Hãy thử lại để hoàn tất yêu cầu này.</p>}
    {retryOnly && <p role="status">Giữ nguyên thông tin và bấm thử lại để hoàn tất, tránh tạo HTX trùng.</p>}
    {provinces.error && <ProblemAlert problem={provinces.error} />}
    {communes.error && <ProblemAlert problem={communes.error} />}
    <fieldset disabled={busy || retryOnly} className="stack" style={{ border: 0, padding: 0 }}>
      <label className="field"><span>Tên HTX</span><input aria-label="Tên HTX mới" value={name} onChange={e => setName(e.target.value)} required maxLength={200} /></label>
      <div className="row" style={{ gap: 8, flexWrap: "wrap" }}>
        <label className="field"><span>Tỉnh</span><select aria-label="Tỉnh của HTX mới" value={province} onChange={e => { setProvince(e.target.value); setCommune(""); }} required><option value="">Chọn tỉnh</option>{(provinces.data ?? []).filter(u => u.level === "province" && unitValidOn(u, day)).map(u => <option key={u.code} value={u.code}>{u.name}</option>)}</select></label>
        <label className="field"><span>Xã</span><select aria-label="Xã của HTX mới" value={commune} onChange={e => setCommune(e.target.value)} required disabled={!province || !communes.data}><option value="">Chọn xã</option>{validCommunes.map(u => <option key={u.code} value={u.code}>{u.name}</option>)}</select></label>
        <label className="field"><span>Mã số thuế (tùy chọn)</span><input aria-label="Mã số thuế HTX mới" value={mst} onChange={e => setMst(e.target.value)} aria-invalid={!validMST} />{!validMST && <small role="alert">Mã số thuế gồm 10 số hoặc 10 số và mã chi nhánh 3 số.</small>}</label>
        <label className="field"><span>Thỏa thuận HTX (căn cứ chia sẻ)</span><input aria-label="Thỏa thuận HTX mới" value={agreement} onChange={e => setAgreement(e.target.value)} maxLength={200} /></label>
      </div>
    </fieldset>
    <div><button type="submit" className="btn btn-primary" disabled={busy || !name.trim() || !validMST || !validCommunes.some(u => u.code === commune)}>{busy ? "Đang hoàn tất…" : retryOnly ? "Thử lại để hoàn tất" : "Tạo HTX và lập neo"}</button></div>
  </form>;
}
