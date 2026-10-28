"use client";

// Chi tiết Báo cáo phòng BM-11 (W11.45): bố cục theo báo cáo giấy của phòng — thủy sản (khai thác, nuôi trồng, lũy kế), bảng
// chăn nuôi (tổng đàn, tiêm phòng, hóa chất, KSGM), kết quả chương trình/dự án, phần viết theo mục; "Tổng hợp từ n/N xã" + xã
// chưa nộp; số liệu kỳ trước nộp bổ sung. Phần tự động không sửa tay (BR-01). Thao tác theo quyền máy chủ trả về (`can`).
import { useState } from "react";
import { StatusPill } from "@/components/patterns/list";
import { TextField } from "@/components/form";
import { dt, num, type useAction } from "@/kn/cycle/common";
import { saveBlob } from "@/lib/files";
import { ReasonDialog } from "../ReasonDialog";
import { changedTexts, EVENT_LABEL, MAX_TEXT, RECIPIENT_LABEL, STATUS_LABEL, TYPE_LABEL, type Bm11, type Bm11Api } from "./api";

export function Bm11Detail({ v, bapi, act, onChange }: {
  v: Bm11;
  bapi: Bm11Api;
  act: ReturnType<typeof useAction>;
  onChange: (next: Bm11) => void;
}) {
  const sec0 = Object.fromEntries(v.sections.map((s) => [s.key, s.text]));
  const prog0 = Object.fromEntries(v.programs.map((p) => [p.ref, p.text]));
  const [sec, setSec] = useState<Record<string, string>>(sec0);
  const [prog, setProg] = useState<Record<string, string>>(prog0);
  const [returning, setReturning] = useState(false);
  const st = STATUS_LABEL[v.status];
  const dirty = Object.keys(changedTexts(sec0, sec)).length + Object.keys(changedTexts(prog0, prog)).length > 0;
  const step = async (f: () => Promise<Bm11>, ok: string) => {
    const r = await act.run(f, ok);
    if (r) onChange(r);
  };
  const exportWord = async () => {
    const run = await act.run(() => bapi.word(v.id), (r) => (r.doc_number ? `Đã xuất Word số ${r.doc_number}.` : "Đã xuất Word (bản nháp, chưa cấp số)."));
    if (run) saveBlob(await bapi.download(v.id, run.id), run.file_name);
  };
  const received = v.recipient_list.filter((r) => r.received).length;
  return (
    <div className="stack">
      <section className="card sec">
        <h2>
          {TYPE_LABEL[v.type]} — {v.label} <StatusPill tone={st[1]}>{st[0]}</StatusPill>
          {v.status === "sent" && received === v.recipient_list.length ? <StatusPill tone="ok">Đã nhận</StatusPill> : null}
        </h2>
        <p>
          <strong>{v.communes}</strong> · Số liệu: {v.basis === "closed" ? "đã chốt" : "đã nộp, đã xác thực"} · Phiên bản {v.version} · Cập nhật{" "}
          {dt(v.last_at)}
        </p>
        {v.coverage.filter((c) => c.missing_names.length > 0).map((c) => (
          <p key={c.form} className="warn">Xã chưa nộp {c.form}: {c.missing_names.join(", ")} (không ước tính phần thiếu)</p>
        ))}
        {v.status === "returned" && v.return_comment ? <p className="warn">Ý kiến trả lại: {v.return_comment}</p> : null}
        <div className="toolbar">
          {v.can.edit ? (
            <button className="btn btn-primary" disabled={act.busy || !dirty}
              onClick={() => step(() => bapi.edit(v.id, v.seq, changedTexts(sec0, sec), changedTexts(prog0, prog)), "Đã lưu phần viết.")}>
              Lưu phần viết
            </button>
          ) : null}
          {v.can.refresh ? (
            <button className="btn btn-o" disabled={act.busy || dirty} title={dirty ? "Lưu phần viết trước" : undefined}
              onClick={() => step(() => bapi.refresh(v.id, v.seq), "Đã làm mới số liệu tự động.")}>
              Làm mới số liệu
            </button>
          ) : null}
          {v.can.submit ? (
            <button className="btn btn-o" disabled={act.busy || dirty} title={dirty ? "Lưu phần viết trước" : undefined}
              onClick={() => step(() => bapi.submit(v.id, v.seq), "Đã trình Lãnh đạo phòng.")}>
              Trình duyệt
            </button>
          ) : null}
          {v.can.approve ? (
            <button className="btn btn-primary" disabled={act.busy} onClick={() => step(() => bapi.approve(v.id, v.seq), "Đã duyệt và gửi theo nơi nhận.")}>
              Duyệt và gửi
            </button>
          ) : null}
          {v.can.return ? (
            <button className="btn btn-o" disabled={act.busy} onClick={() => setReturning(true)}>Trả lại</button>
          ) : null}
          {v.can.receive ? (
            <button className="btn btn-primary" disabled={act.busy} onClick={() => step(() => bapi.receive(v.id), "Đã xác nhận Đã nhận.")}>
              Đã nhận
            </button>
          ) : null}
          {v.can.word ? <button className="btn btn-o" disabled={act.busy} onClick={exportWord}>Xuất Word</button> : null}
        </div>
        {returning ? (
          <ReasonDialog title={`Trả lại báo cáo ${v.label}`} label="Ý kiến của Lãnh đạo phòng" confirmLabel="Trả lại" min={1} max={2000}
            busy={act.busy} onClose={() => setReturning(false)}
            onConfirm={async (c) => {
              const r = await act.run(() => bapi.ret(v.id, v.seq, c), "Đã trả lại báo cáo.");
              if (r) {
                setReturning(false);
                onChange(r);
              }
            }} />
        ) : null}
      </section>

      <section className="card sec">
        <h2>Thủy sản</h2>
        <p>
          Tổng sản lượng khai thác tự nhiên <strong>{num(v.fishery.catch_cur, 3)}</strong> tấn (cá {num(v.fishery.groups.ca, 3)}, tôm{" "}
          {num(v.fishery.groups.tom, 3)}, mực {num(v.fishery.groups.muc, 3)}, khác {num(v.fishery.groups.khac, 3)}), lũy kế{" "}
          {num(v.fishery.catch_ytd, 3)} tấn.
        </p>
        <p>
          Nuôi trồng thủy sản: <strong>{num(v.fishery.aqua_cur, 3)}</strong> tấn, lũy kế {num(v.fishery.aqua_ytd, 3)} tấn. Tổng sản lượng trong kỳ{" "}
          {num(v.fishery.total_cur, 3)} tấn.
        </p>
      </section>

      <section className="card sec">
        <h2>Chăn nuôi</h2>
        <table className="dt" aria-label="Chăn nuôi">
          <thead><tr><th>Nội dung</th><th className="num">Số lượng</th><th>ĐVT</th></tr></thead>
          <tbody>
            {v.groups.map((g) => [
              <tr key={g.key}><td colSpan={3}><strong>* {g.label}</strong></td></tr>,
              ...v.lines.filter((l) => l.group === g.key).map((l) => (
                <tr key={l.key}><td>{l.sub ? <>&nbsp;&nbsp;- {l.label}</> : l.label}</td><td className="num">{num(l.value, 3)}</td><td>{l.unit}</td></tr>
              )),
            ])}
          </tbody>
        </table>
      </section>

      {v.supplement.length ? (
        <section className="card sec">
          <h2>Số liệu kỳ trước nộp bổ sung</h2>
          <table className="dt" aria-label="Số liệu kỳ trước nộp bổ sung">
            <thead><tr><th>Kỳ</th><th>Chỉ tiêu</th><th className="num">Đã gửi</th><th className="num">Hiện tại</th><th>ĐVT</th></tr></thead>
            <tbody>
              {v.supplement.map((x) => (
                <tr key={x.period + x.key}><td>{x.period}</td><td>{x.label}</td><td className="num">{num(x.sent, 3)}</td><td className="num">{num(x.now, 3)}</td><td>{x.unit}</td></tr>
              ))}
            </tbody>
          </table>
        </section>
      ) : null}

      <section className="card sec">
        <h2>Kết quả thực hiện các chương trình, dự án</h2>
        {v.programs.length === 0 ? <p className="mute">Không có chương trình, dự án còn hiệu lực trong kỳ.</p> : null}
        {v.programs.map((p) => (
          <div key={p.ref} className="stack">
            <p>
              <strong>• {p.name}</strong> — {p.sites} điểm, {p.households} hộ{p.manager_name ? ` · Chủ nhiệm: ${p.manager_name}` : ""}
            </p>
            {p.can_edit ? (
              <TextField multiline label={`Kết quả — ${p.code}`} maxLength={MAX_TEXT} value={prog[p.ref] ?? ""}
                onChange={(x) => setProg({ ...prog, [p.ref]: x })} />
            ) : p.text ? <p className="pre">{p.text}</p> : null}
          </div>
        ))}
      </section>

      <section className="card sec">
        <h2>Phần viết</h2>
        {v.sections.map((s) =>
          v.can.edit ? (
            <TextField key={s.key} multiline label={s.label} maxLength={MAX_TEXT} value={sec[s.key] ?? ""} onChange={(x) => setSec({ ...sec, [s.key]: x })} />
          ) : (
            <div key={s.key}>
              <h3>{s.label}</h3>
              <p className="pre">{s.text || "—"}</p>
            </div>
          ),
        )}
      </section>

      <section className="card sec">
        <h2>Nơi nhận và lịch sử</h2>
        <ul>
          {v.recipient_list.map((r) => (
            <li key={r.code}>
              {r.label}: {v.status !== "sent" ? "chưa gửi" : r.received ? `Đã nhận ${dt(r.received_at)}${r.actor_name ? ` (${r.actor_name})` : ""}` : "Đã gửi"}
            </li>
          ))}
        </ul>
        <table className="dt" aria-label="Lịch sử">
          <thead><tr><th>#</th><th>Bước</th><th>Người</th><th>Lúc</th><th>Ghi chú</th></tr></thead>
          <tbody>
            {[...v.history].reverse().map((h) => (
              <tr key={h.seq}>
                <td>{h.seq}</td>
                <td>{EVENT_LABEL[h.event] ?? h.event}</td>
                <td>{h.actor_name || "—"}</td>
                <td>{dt(h.at)}</td>
                <td>{h.comment || (h.recipient ? RECIPIENT_LABEL[h.recipient] : "")}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>
    </div>
  );
}
