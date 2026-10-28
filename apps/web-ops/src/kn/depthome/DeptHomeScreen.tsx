"use client";

// Trang chủ phòng (W11.54 — BRD CNTS v3.7 FN-17 UR-TINH-06; trang chủ tương ứng của Trồng trọt, KHTC, HCTH, Thông tin –
// Truyền thông). Bố cục theo prototype B (dải thẻ chỉ số → nội dung hai cột): thẻ chỉ số bấm mở màn chi tiết, tiến độ số liệu
// đã chốt (W11.10), bảng giá đã duyệt (Thông tin – Truyền thông), giới thiệu + chức năng nhiệm vụ (Lãnh đạo phòng sửa),
// sơ đồ tổ chức (từ tài khoản), văn bản mới của kho phòng. Ban Giám đốc xem như Lãnh đạo phòng nhưng không sửa (BR-02).
import Link from "next/link";
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, dt, useAction, useLoad } from "@/kn/cycle/common";
import { DEPT_NAME, KIND_LABEL, MGMT_LABEL, VALIDITY_LABEL, VALIDITY_TONE, createDeptHomeApi, kpiColor, kpiLink, type DeptHome, type DeptHomeApi } from "./api";

const ymd = (s: string | null | undefined) => (s ? s.slice(0, 10).split("-").reverse().join("/") : "—");

export function DeptHomeScreen({ department }: { department?: string }) {
  const { api, kn } = useAuth();
  const hapi = useMemo(() => (api ? createDeptHomeApi(api, department) : null), [api, department]);
  const home = useLoad(hapi ? () => hapi.home() : null, [hapi]);
  if (!hapi) return null;
  const dept = department ?? kn.context?.department_code ?? "";
  return (
    <div className="page">
      <PageHeader title={`Trang chủ Phòng ${DEPT_NAME[dept] ?? ""}`}>
        <p className="mute">Tổ chức, nhiệm vụ, chỉ số nóng và văn bản của phòng trên một trang (FN-17). Số liệu chỉ lấy từ biểu đã nộp, đã chốt.</p>
      </PageHeader>
      <LoadState s={home}>{(h) => <HomeBody h={h} hapi={hapi} department={department} reload={home.reload} />}</LoadState>
    </div>
  );
}

function HomeBody({ h, hapi, department, reload }: { h: DeptHome; hapi: DeptHomeApi; department?: string; reload: () => void }) {
  const libHref = department ? `/kn/phong-ban/${department}/van-ban` : "/kn/van-ban";
  return (
    <>
      {h.read_only ? <div className="alert alert-info" role="status">Ngữ cảnh chỉ xem — không sửa được giới thiệu, văn bản.</div> : null}
      <section className="grid4" aria-label="Chỉ số của phòng">
        {h.kpis.map((k) => {
          const href = kpiLink(k.key, department);
          const body = (
            <div className={`kpi ${kpiColor(k.tone)}`} data-testid={`dept-kpi-${k.key}`}>
              <div className="lb">{k.label}</div>
              <div className="vl">{k.value.toLocaleString("vi-VN")}</div>
              {href ? <div className="ft">Xem chi tiết →</div> : null}
            </div>
          );
          return href ? <Link key={k.key} href={href} style={{ textDecoration: "none" }}>{body}</Link> : <div key={k.key}>{body}</div>;
        })}
      </section>

      <div className="grid2" style={{ display: "grid", gridTemplateColumns: "minmax(0,1.5fr) minmax(0,1fr)", gap: 16, marginTop: 16 }}>
        <div>
          {h.closed_periods.length ? (
            <section className="card sec">
              <h2>Tiến độ nộp và chốt số liệu</h2>
              <table className="dt" aria-label="Tiến độ kỳ nguồn">
                <thead><tr><th>Biểu</th><th>Kỳ</th><th>Đơn vị có số liệu</th><th>Trạng thái</th></tr></thead>
                <tbody>
                  {h.closed_periods.map((p) => (
                    <tr key={`${p.form_code}:${p.period_key}`}>
                      <td>{p.form_code}</td>
                      <td>{p.period_label || p.period_key}</td>
                      <td>{p.units_with_data ?? "—"}/{p.units_total ?? "—"}</td>
                      <td><StatusPill tone={p.closed ? "ok" : "warn"}>{p.closed ? "Đã chốt" : "Chưa chốt"}</StatusPill></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </section>
          ) : null}
          {h.price ? (
            <section className="card sec">
              <h2>Bảng giá đã duyệt</h2>
              <p>
                Ngày {ymd(h.price.board_date)}: {h.price.items} mặt hàng · Tuần {h.price.week_key ?? "—"}: {h.price.tickets} phiếu, {h.price.week_days_approved} ngày đã duyệt.{" "}
                <Link href="/kn/bao-gia/bang-gia">Mở bảng giá</Link>
              </p>
            </section>
          ) : null}
          <section className="card sec">
            <h2>Văn bản mới</h2>
            <p className="mute">
              {h.library.total} văn bản · {h.library.new_30d} mới trong 30 ngày · {h.library.expired} hết hiệu lực. <Link href={libHref}>Mở kho văn bản</Link>
            </p>
            {h.latest_docs.length ? (
              <table className="dt" aria-label="Văn bản mới">
                <thead><tr><th>Tên văn bản</th><th>Loại</th><th>Hiệu lực</th><th>Cập nhật</th></tr></thead>
                <tbody>
                  {h.latest_docs.map((d) => (
                    <tr key={d.ref}>
                      <td>{d.title}{d.number ? <span className="mute"> · {d.number}</span> : null}</td>
                      <td>{KIND_LABEL[d.kind]}</td>
                      <td><StatusPill tone={VALIDITY_TONE[d.validity]}>{VALIDITY_LABEL[d.validity]}</StatusPill></td>
                      <td>{dt(d.recorded_at)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : <p className="mute">Chưa có văn bản.</p>}
          </section>
        </div>
        <div>
          <ProfileCard h={h} hapi={hapi} reload={reload} />
          <section className="card sec">
            <h2>Sơ đồ tổ chức</h2>
            {h.officers.length ? (
              <ul aria-label="Cán bộ của phòng" style={{ listStyle: "none", padding: 0, margin: 0 }}>
                {h.officers.map((o) => (
                  <li key={o.actor.ref} style={{ padding: "6px 0", borderBottom: "1px solid var(--slate-200, #e2e8f0)" }}>
                    <strong>{o.actor.display ?? "Cán bộ"}</strong>{" "}
                    <span className="mute">{o.mgmt_level ? MGMT_LABEL[o.mgmt_level] ?? o.mgmt_level : "Viên chức"}</span>
                    {o.leader ? <StatusPill tone="info">Lãnh đạo</StatusPill> : null}
                  </li>
                ))}
              </ul>
            ) : <p className="mute">Danh sách lấy từ tài khoản của phòng (chưa có dữ liệu).</p>}
          </section>
        </div>
      </div>
    </>
  );
}

function ProfileCard({ h, hapi, reload }: { h: DeptHome; hapi: DeptHomeApi; reload: () => void }) {
  const [edit, setEdit] = useState(false);
  const [intro, setIntro] = useState(h.profile?.intro ?? "");
  const [duties, setDuties] = useState(h.profile?.duties ?? "");
  const act = useAction();
  const save = async () => {
    if (await act.run(() => hapi.saveProfile(intro, duties, h.profile?.version ?? 0), "Đã lưu giới thiệu phòng.")) {
      setEdit(false);
      reload();
    }
  };
  return (
    <section className="card sec">
      <h2>Giới thiệu và chức năng nhiệm vụ</h2>
      {act.view}
      {edit ? (
        <div className="form">
          <label className="field">Giới thiệu<textarea rows={3} value={intro} onChange={(e) => setIntro(e.target.value)} maxLength={4000} /></label>
          <label className="field">Chức năng, nhiệm vụ<textarea rows={6} value={duties} onChange={(e) => setDuties(e.target.value)} maxLength={8000} /></label>
          <div className="row">
            <button className="btn btn-primary" disabled={act.busy} onClick={() => void save()}>Lưu</button>
            <button className="btn" onClick={() => setEdit(false)}>Hủy</button>
          </div>
        </div>
      ) : (
        <>
          {h.profile ? (
            <>
              <p style={{ whiteSpace: "pre-line" }}>{h.profile.intro}</p>
              <p style={{ whiteSpace: "pre-line" }}>{h.profile.duties}</p>
              <p className="mute">Bản {h.profile.version} · {dt(h.profile.recorded_at)}{h.profile.actor.display ? ` · ${h.profile.actor.display}` : ""}</p>
            </>
          ) : <p className="mute">Chưa có giới thiệu.</p>}
          {h.can_edit_profile ? <button className="btn btn-sm" onClick={() => setEdit(true)}>Sửa giới thiệu</button> : null}
        </>
      )}
    </section>
  );
}
