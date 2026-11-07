"use client";
import { useState } from "react";
import { useRouter } from "next/navigation";
import { BackLink, DetailLayout, LogPanel } from "@/components/patterns/detail";
import { PageHeader } from "@/components/patterns/list";
import { TextField } from "@/components/form";
import { LoadState, useLoad } from "@/kn/cycle/common";
import { APPRAISAL_ACTION, APPRAISAL_BASE, APPRAISAL_STATUS, appraisalEntryPath, type AppraisalDetail, type AppraisalEntryChange, type AppraisalItem } from "./appraisal-api";
import { APPRAISAL_RATING_LABEL, validateAppraisalProposal } from "./appraisal-form";
import { RatingSelect, useAppraisalAction, useAppraisalApi } from "./appraisal-ui";

export function AppraisalEntryScreen({ period, staff }: { period: string; staff: string }) {
  const api = useAppraisalApi(), detail = useLoad(() => api.detail(period, staff), [period, staff]);
  return <><BackLink href={`${APPRAISAL_BASE}/dot/${period}`}>Danh sách đề xuất</BackLink><LoadState s={detail}>{v => <EntryForm key={v.item.id} detail={v} reload={detail.reload} />}</LoadState></>;
}
function EntryForm({ detail: v, reload }: { detail: AppraisalDetail; reload: () => void }) {
  const api = useAppraisalApi(), router = useRouter(), act = useAppraisalAction(), item = v.item;
  const [selfRating, setSelfRating] = useState(item.self_rating), [collective, setCollective] = useState(item.collective_rating), [official, setOfficial] = useState(item.official_rating);
  const [reason, setReason] = useState(item.reason), [note, setNote] = useState(item.note), [personnel, setPersonnel] = useState(item.personnel_proposal), [changeReason, setChangeReason] = useState("");
  const [errors, setErrors] = useState<Record<string,string>>({}), [history, setHistory] = useState(v.history), [more, setMore] = useState(v.history_more), [old, setOld] = useState<AppraisalItem | null>(null);
  const legacy = item.legacy_read_only || v.period.workflow_version !== "hrm.appraisal-proposal/v4";
  const canEdit = !legacy && item.can_manage && (!v.period.closed || item.reopened) && ["not_started", "entered", "change_requested"].includes(item.status) && !item.member.excluded_reason;
  async function change(action: AppraisalEntryChange["action"], next = false) {
    const problems = action === "manager_submit" ? validateAppraisalProposal(selfRating, collective, reason) : {} as Record<string,string>;
    if (["return","reopen"].includes(action) && [...changeReason.trim()].length < 10) problems.change_reason = "Nhập lý do ít nhất 10 ký tự.";
    if (Object.keys(problems).length) { setErrors(problems); return; }
    const body: AppraisalEntryChange = { action, version:item.version };
    if (action === "manager_submit") Object.assign(body, { self_rating:selfRating, collective_rating:collective, reason, note, personnel_proposal:personnel });
    if (action === "official") body.official_rating = official;
    if (action === "return" || action === "reopen") body.reason = changeReason;
    const result = await act.execute(`${item.period_id}:${item.member.staff_id}:${action}`, body, k => api.change(item.period_id, item.member.staff_id, body, k));
    if (result) {
      if (next) {
        const list = await act.run(() => api.entries(item.period_id));
        const index = list?.items.findIndex(x => x.member.staff_id === item.member.staff_id) ?? -1;
        const following = index >= 0 ? list?.items.slice(index + 1).find(x => x.can_manage && ["not_started","entered","change_requested"].includes(x.status)) : undefined;
        if (following) { router.push(appraisalEntryPath(item.period_id, following.member.staff_id)); return; }
        act.setNote("Đã lưu; không còn người tiếp theo cần nhập trong phạm vi.");
      }
      reload();
    }
  }
  return <><PageHeader title={`${item.name || item.member.code} · ${APPRAISAL_STATUS[item.status]}`}>{item.member.position_name} · {item.member.unit_name} · {item.member.summary_kind}. {item.manager_late ? "Trễ hạn gửi đề xuất." : ""}</PageHeader>
    {legacy ? <p role="note">Dữ liệu đánh giá cũ chỉ đọc. Phiếu, điểm và lịch sử được giữ nguyên.</p> : null}
    {item.member.excluded_reason ? <p role="note">Loại trừ: {item.member.excluded_reason}</p> : null}
    {item.correction_reason ? <p role="note">Yêu cầu điều chỉnh: {item.correction_reason}</p> : null}{act.view}
    {v.period.quarter === 0 ? <section className="card"><h2>Tham khảo bốn quý</h2><p>Kết quả chụp khi mở đợt năm; kết luận năm được nhập độc lập.</p><table className="tbl"><thead><tr><th>Quý</th><th>Trạng thái</th><th>Kết quả đã chốt</th></tr></thead><tbody>{[1,2,3,4].map(q => { const ref = item.quarter_results?.find(x => x.quarter === q); return <tr key={q}><td>Quý {q}</td><td>{ref ? APPRAISAL_STATUS[ref.status as AppraisalItem["status"]] ?? ref.status : "Chưa có kết quả trong phạm vi"}</td><td>{ref?.rating ? APPRAISAL_RATING_LABEL[ref.rating] : "—"}</td></tr>; })}</tbody></table></section> : null}
    <DetailLayout main={<><section className="card"><h2>Đề xuất xếp loại</h2><p>Lãnh đạo nhập theo bản tự đề xuất của người được đánh giá và kết quả họp tập thể; Mẫu số 06 do Phòng HCTH nhập.</p>
      <div className="grid2"><div><RatingSelect label="Cá nhân tự đề xuất" value={selfRating} onChange={setSelfRating} disabled={!canEdit || act.busy} />{errors.self_rating ? <p role="alert">{errors.self_rating}</p> : null}</div><div><RatingSelect label="Tập thể đề xuất" value={collective} onChange={setCollective} disabled={!canEdit || act.busy} />{errors.collective_rating ? <p role="alert">{errors.collective_rating}</p> : null}</div></div>
      <TextField label="Tóm tắt căn cứ, cơ sở lý do" value={reason} onChange={setReason} multiline maxLength={2000} error={errors.reason} disabled={!canEdit || act.busy} />
      <TextField label="Đề xuất công tác cán bộ" value={personnel} onChange={setPersonnel} multiline maxLength={2000} disabled={!canEdit || act.busy} />
      <TextField label="Ghi chú" value={note} onChange={setNote} multiline maxLength={2000} disabled={!canEdit || act.busy} />
      <div className="row">{canEdit ? <><button disabled={act.busy} className="btn btn-p" onClick={() => void change("manager_submit")}>Lưu đề xuất</button><button disabled={act.busy} className="btn btn-o" onClick={() => void change("manager_submit",true)}>Lưu và người tiếp theo</button></> : null}
      {!legacy && item.can_manage && item.status === "entered" ? <button disabled={act.busy} className="btn btn-p" onClick={() => void change("send")}>Gửi Phòng HCTH</button> : null}</div>
      {item.closed_rating ? <p>Kết quả đã chốt: <strong>{APPRAISAL_RATING_LABEL[item.closed_rating]}</strong></p> : null}
    </section>
    {!legacy && item.can_center ? <section className="card"><h2>Kết luận của HCTH</h2><RatingSelect label="Kết quả chính thức" value={official} onChange={setOfficial} disabled={item.status !== "sent" || act.busy} /><p>Để trống thì dùng mức tập thể đề xuất khi chốt.</p>
      {item.status === "sent" ? <button disabled={act.busy} className="btn btn-p" onClick={() => void change("official")}>Ghi kết luận</button> : null}
      {["sent","closed"].includes(item.status) ? <><TextField label="Lý do điều chỉnh hoặc mở lại" value={changeReason} onChange={setChangeReason} multiline maxLength={2000} error={errors.change_reason} />{item.status === "sent" ? <button disabled={act.busy} className="btn btn-o" onClick={() => void change("return")}>Yêu cầu đơn vị điều chỉnh</button> : <button disabled={act.busy} className="btn btn-o" onClick={() => void change("reopen")}>Mở lại riêng người có lý do</button>}</> : null}
    </section> : null}
    {legacy ? <LegacyPoints item={item} /> : null}
    {old ? <section className="card"><h2>Phiên bản {old.version} · {APPRAISAL_STATUS[old.status]}</h2><p>Cá nhân: {APPRAISAL_RATING_LABEL[old.self_rating]} · Tập thể: {APPRAISAL_RATING_LABEL[old.collective_rating]}</p><p>{old.reason}</p><p>{old.personnel_proposal}</p><p>{old.note}</p><LegacyPoints item={old} /></section> : null}</>}
    log={<><LogPanel title="Lịch sử đề xuất" entries={history.map(x => ({ event:`${APPRAISAL_ACTION[x.action] ?? x.action} · v${x.version}`, actor_display:x.actor_ref, at:x.recorded_at, reason:x.change_reason, changes:[{field:"Trạng thái",old:null,new:APPRAISAL_STATUS[x.status]}] }))} /><div className="card">{history.map(x => <button className="btn btn-o btn-sm" key={x.id} onClick={() => void act.run(() => api.version(item.period_id,item.member.staff_id,x.id)).then(value => {if(value)setOld(value);})}>Xem bản {x.version}</button>)}{more ? <button className="btn btn-o" onClick={() => void act.run(() => api.history(item.period_id,item.member.staff_id,String(history.at(-1)?.version ?? ""))).then(next => {if(next){setHistory([...history,...next.items]);setMore(next.more);}})}>Tải lịch sử cũ hơn</button> : null}</div></>} />
  </>;
}
function LegacyPoints({item}:{item:AppraisalItem}) {
  if (!item.self.total && !item.manager.total && !item.self.answers?.length && !item.manager.answers?.length) return null;
  const codes = [...new Set([...(item.self.answers ?? []).map(x=>x.code),...(item.manager.answers ?? []).map(x=>x.code)])];
  return <section className="card" aria-label="Phiếu điểm lịch sử chỉ đọc"><h3>Phiếu điểm lịch sử chỉ đọc</h3><p>Tự đánh giá: {item.self.total || "—"} · Cấp quản lý: {item.manager.total || "—"}</p><table className="tbl"><thead><tr><th>Mã tiêu chí</th><th>Điểm tự đánh giá</th><th>Điểm cấp quản lý</th></tr></thead><tbody>{codes.map(code=><tr key={code}><td>{code}</td><td>{item.self.answers?.find(x=>x.code===code)?.points ?? "—"}</td><td>{item.manager.answers?.find(x=>x.code===code)?.points ?? "—"}</td></tr>)}</tbody></table></section>;
}
