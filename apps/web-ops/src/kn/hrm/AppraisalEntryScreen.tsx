"use client";
import { useMemo, useState } from "react";
import Link from "next/link";
import { useAuth } from "@/auth/AuthProvider";
import { createDocApi } from "./doc-api";
import { BackLink, DetailLayout, LogPanel } from "@/components/patterns/detail";
import { PageHeader } from "@/components/patterns/list";
import { NumberField, TextField } from "@/components/form";
import { LoadState, useLoad } from "@/kn/cycle/common";
import { APPRAISAL_ACTION, APPRAISAL_BASE, APPRAISAL_STATUS, type AppraisalDetail, type AppraisalEntryChange, type AppraisalItem } from "./appraisal-api";
import { APPRAISAL_RATING_LABEL, appraisalSuggestion, validateAppraisalAnswers, type AppraisalAnswer } from "./appraisal-form";
import { MINE_BASE, RatingSelect, useAppraisalAction, useAppraisalApi, uuidPattern } from "./appraisal-ui";

export function AppraisalEntryScreen({ period, staff, mine = false }: { period: string; staff: string; mine?: boolean }) {
  const api = useAppraisalApi(), detail = useLoad(() => api.detail(period, staff), [period, staff]);
  return <><BackLink href={`${mine ? MINE_BASE : APPRAISAL_BASE}/dot/${period}`}>Danh sách phiếu</BackLink><LoadState s={detail}>{v => <EntryForm key={v.item.id} detail={v} reload={detail.reload} />}</LoadState></>;
}
function EntryForm({ detail: v, reload }: { detail: AppraisalDetail; reload: () => void }) {
  const api = useAppraisalApi(), act = useAppraisalAction(), item = v.item, form = v.form.form!;
  const [selfAnswers, setSelfAnswers] = useState<AppraisalAnswer[]>(item.self.answers ?? []), [managerAnswers, setManagerAnswers] = useState<AppraisalAnswer[]>(item.manager.answers ?? item.self.answers ?? []);
  const [selfRating, setSelfRating] = useState(item.self_rating), [collective, setCollective] = useState(item.collective_rating), [official, setOfficial] = useState(item.official_rating);
  const [reason, setReason] = useState(item.reason), [note, setNote] = useState(item.note), [personnel, setPersonnel] = useState(item.personnel_proposal), [scan, setScan] = useState(item.proxy_document ?? "");
  const [errors, setErrors] = useState<Record<string,string>>({}), [history, setHistory] = useState(v.history), [more, setMore] = useState(v.history_more), [old, setOld] = useState<AppraisalItem | null>(null);
  const selfOpen = (!v.period.closed || item.reopened) && (!v.period.self_locked || item.reopened) && (item.status === "not_started" || item.status === "change_requested" && item.returned_to === "self");
  const managerOpen = (!v.period.closed || item.reopened) && (!v.period.manager_locked || item.reopened) && (item.status === "self_submitted" || item.status === "manager_submitted" || item.status === "change_requested" && item.returned_to === "manager");
  const [side, setSide] = useState<"self"|"manager">(item.can_self && selfOpen ? "self" : "manager");
  const answers = side === "self" ? selfAnswers : managerAnswers, setAnswers = side === "self" ? setSelfAnswers : setManagerAnswers;
  const canEdit = side === "self" ? item.can_self && selfOpen || item.can_manage && selfOpen && !item.can_self : item.can_manage && managerOpen;
  const suggestion = appraisalSuggestion(form, answers);
  const {api:raw}=useAuth(),docsApi=useMemo(()=>createDocApi(raw),[raw]);
  const documents=useLoad(item.can_manage&&!item.can_self&&selfOpen?()=>docsApi.list({doc_type:"danh_gia",staff:item.member.staff_id,limit:100}):null,[item.member.staff_id,item.can_manage,selfOpen]);

  const update = (code: string, key: "points"|"note", value: string) => setAnswers(form.criteria.map(c => ({code:c.code, points:answers.find(a => a.code === c.code)?.points ?? "", note:answers.find(a => a.code === c.code)?.note ?? "", ...(c.code === code ? {[key]:value}: {})})));
  async function change(action: AppraisalEntryChange["action"], returnTo?: "self"|"manager") {
    let checked = { valid:true, errors:{} as Record<string,string>, answers };
    if (["self_draft","self_submit","proxy_self","manager_draft","manager_submit"].includes(action)) checked = validateAppraisalAnswers(form, answers, !action.endsWith("draft"));
    if(action === "proxy_self" && !uuidPattern.test(scan)) checked.errors.proxy_document = "Chọn mã tài liệu bản scan đã lưu ở kho tài liệu.";
    if (["return","reopen"].includes(action) && [...reason.trim()].length < 10) checked.errors.reason = "Nhập lý do ít nhất 10 ký tự.";
    if(Object.keys(checked.errors).length) { setErrors(checked.errors); return; }
    const body: AppraisalEntryChange = { action, version:item.version, reason, note, personnel_proposal:personnel };
    if (["self_draft","self_submit","proxy_self","manager_draft","manager_submit"].includes(action)) body.answers = checked.answers;
    if(action.startsWith("self_") || action === "proxy_self") body.self_rating = selfRating;
    if(action.startsWith("manager_")) body.collective_rating = collective;
    if(action === "official") body.official_rating = official;
    if(action === "proxy_self") body.proxy_document = scan;
    if(returnTo) body.return_to = returnTo;
    const result = await act.execute(`${item.period_id}:${staffID(item)}:${action}`, body, k => api.change(item.period_id, staffID(item), body, k));
    if(result) reload();
  }
  return <><PageHeader title={`${item.name || item.member.code} · ${APPRAISAL_STATUS[item.status]}`}>{item.member.position_name} · {item.member.unit_name} · {item.member.summary_kind} · mẫu v{v.form.version} (đóng băng). {item.self_late || item.manager_late ? "Trễ hạn; vẫn nộp được đến khi khóa bước." : ""}</PageHeader>
    {item.correction_reason ? <p role="note">Yêu cầu điều chỉnh: {item.correction_reason}</p> : null}{act.view}
    {v.period.quarter === 0 ? <section className="card"><h2>Tham khảo bốn quý</h2><p>Kết quả tại lúc mở đợt năm; tập thể kết luận năm độc lập, không tự quy đổi từ bốn quý.</p><table className="tbl"><thead><tr><th>Quý</th><th>Trạng thái lúc chụp</th><th>Kết quả đã chốt</th><th>Phiên bản</th></tr></thead><tbody>{[1,2,3,4].map(q => { const ref = item.quarter_results?.find(x => x.quarter === q); return <tr key={q}><td>Quý {q}</td><td>{ref ? APPRAISAL_STATUS[ref.status as AppraisalItem["status"]] ?? ref.status : "Chưa có kết quả trong phạm vi"}</td><td>{ref?.rating ? APPRAISAL_RATING_LABEL[ref.rating] : "—"}</td><td>{ref ? `v${ref.version} · ${ref.entry_version}` : "—"}</td></tr>; })}</tbody></table></section> : null}
    <DetailLayout main={<><section className="card"><h2>Phiếu đánh giá</h2><div className="row"><button className="btn btn-o" aria-pressed={side === "self"} onClick={() => setSide("self")}>Tự đánh giá</button><button className="btn btn-o" aria-pressed={side === "manager"} onClick={() => setSide("manager")}>Cấp quản lý</button></div>
      <table className="tbl"><thead><tr><th>Tiêu chí</th><th>Tối đa</th><th>Tự đánh giá</th><th>Cấp quản lý</th></tr></thead><tbody>{form.criteria.map((c,i) => <tr key={c.code}><td>{c.group ? <strong>{c.group} · </strong> : null}{c.title}{c.guide ? <p className="small mute">{c.guide}</p> : null}</td><td>{c.maximum}</td>{(["self","manager"] as const).map(column => <td key={column}>{side === column ? <><NumberField label={`${column === "self" ? "Tự chấm" : "Quản lý chấm"} ${c.title}`} value={answers.find(a => a.code === c.code)?.points ?? ""} onChange={s => update(c.code,"points",s)} disabled={!canEdit || act.busy} rule={{max:c.maximum,maxDecimals:2,maxInputLength:32}} error={errors[`answers.${i}.points`]} /><TextField label={`Nhận xét ${c.title}`} value={answers.find(a => a.code === c.code)?.note ?? ""} onChange={s => update(c.code,"note",s)} maxLength={2000} disabled={!canEdit || act.busy} /></> : <>{(column === "self" ? item.self : item.manager).answers?.find(a => a.code === c.code)?.points || "—"}</>}</td>)}</tr>)}</tbody></table>
      <p>Tổng điểm {side === "self" ? "tự đánh giá" : "cấp quản lý"}: {suggestion?.total ?? "Chưa đủ điểm"}; gợi ý: {suggestion ? APPRAISAL_RATING_LABEL[suggestion.rating] : "Chưa có"}. Gợi ý không thay kết luận tập thể.</p>
      <div className="grid2"><RatingSelect label="Cá nhân tự đề xuất" value={selfRating} onChange={setSelfRating} disabled={!(item.can_self && selfOpen || item.can_manage && selfOpen) || act.busy} /><RatingSelect label="Tập thể đề xuất" value={collective} onChange={setCollective} disabled={!item.can_manage || !managerOpen || act.busy} /></div>
      <TextField label="Tóm tắt căn cứ, cơ sở lý do" value={reason} onChange={setReason} multiline maxLength={2000} error={errors.reason} disabled={act.busy || !(canEdit || item.can_center || item.can_manage)} />
      <TextField label="Đề xuất công tác cán bộ" value={personnel} onChange={setPersonnel} multiline maxLength={2000} disabled={!item.can_manage || !managerOpen || act.busy} />
      {form.questions?.length ? <section><h3>Câu hỏi nhận xét</h3><ol>{form.questions.map((q,i)=><li key={i}>{q}</li>)}</ol><p>Trả lời các câu hỏi ở phần Ghi chú dưới đây.</p></section> : null}
      <TextField label="Ghi chú" value={note} onChange={setNote} multiline maxLength={2000} disabled={!canEdit || act.busy} />
      <div className="row">{item.can_self && selfOpen && side === "self" ? <><button disabled={act.busy} className="btn btn-o" onClick={() => void change("self_draft")}>Lưu nháp tự chấm</button><button disabled={act.busy} className="btn btn-p" onClick={() => void change("self_submit")}>Nộp phiếu tự đánh giá</button></> : null}
      {item.can_manage && managerOpen && side === "manager" ? <><button disabled={act.busy} className="btn btn-o" onClick={() => void change("manager_draft")}>Lưu nháp cấp quản lý</button><button disabled={act.busy} className="btn btn-p" onClick={() => void change("manager_submit")}>Hoàn tất đề xuất</button></> : null}
      {item.can_manage && item.status === "manager_submitted" && (!v.period.manager_locked || item.reopened) ? <button disabled={act.busy} className="btn btn-p" onClick={() => void change("send")}>Gửi Phòng HCTH</button> : null}
      {item.can_manage && ["self_submitted","manager_submitted"].includes(item.status) ? <button disabled={act.busy} className="btn btn-o" onClick={() => void change("return","self")}>Trả bước tự đánh giá</button> : null}</div>
      {item.can_manage && !item.can_self && selfOpen && side === "self" ? <section><h3>Nhập thay từ phiếu giấy</h3><p>Chọn bản PDF/ảnh đã lưu cho đúng người trong <Link href={item.can_center?"/kn/nhan-su/kho-tai-lieu":"/kn/nhan-su/tai-lieu-phong"}>Kho tài liệu nhân sự</Link>.</p><button className="btn btn-o btn-sm" onClick={documents.reload}>Tải lại bản scan</button><LoadState s={documents}>{d=><label className="field">Bản scan phiếu giấy<select aria-label="Bản scan phiếu giấy" value={scan} onChange={e=>setScan(e.target.value)}><option value="">Chọn bản scan đã lưu</option>{d.items.filter(x=>x.status==="active"&&(x.content_type==="application/pdf"||x.content_type.startsWith("image/"))).map(x=><option key={x.doc_id} value={x.doc_id}>{x.title}</option>)}</select></label>}</LoadState>{errors.proxy_document?<p role="alert">{errors.proxy_document}</p>:null}<button className="btn btn-p" disabled={act.busy||!scan} onClick={() => void change("proxy_self")}>Nộp bản nhập thay có scan</button></section> : null}
    </section>
    {item.can_center ? <section className="card"><h2>Kết luận của HCTH</h2><RatingSelect label="Kết quả chính thức" value={official} onChange={setOfficial} disabled={item.status !== "sent" || act.busy} />{item.status === "sent" ? <div className="row"><button disabled={act.busy} className="btn btn-p" onClick={() => void change("official")}>Ghi kết luận</button><button disabled={act.busy} className="btn btn-o" onClick={() => void change("return","manager")}>Yêu cầu cấp quản lý điều chỉnh</button></div> : null}{item.status === "closed" ? <button disabled={act.busy} className="btn btn-o" onClick={() => void change("reopen")}>Mở lại riêng người có lý do</button> : null}</section> : null}
    {old ? <section className="card"><h2>Phiên bản {old.version} · {APPRAISAL_STATUS[old.status]}</h2><p>{old.reason}</p><table className="tbl"><thead><tr><th>Tiêu chí</th><th>Tự đánh giá</th><th>Cấp quản lý</th></tr></thead><tbody>{form.criteria.map(c => <tr key={c.code}><td>{c.title}</td><td>{old.self.answers?.find(a => a.code === c.code)?.points ?? "—"}</td><td>{old.manager.answers?.find(a => a.code === c.code)?.points ?? "—"}</td></tr>)}</tbody></table></section> : null}</>}
    log={<><LogPanel title="Lịch sử đánh giá" entries={history.map(x => ({ event:`${APPRAISAL_ACTION[x.action] ?? x.action} · v${x.version}`, actor_display:x.actor_ref, at:x.recorded_at, reason:x.change_reason, changes:[{field:"Trạng thái",old:null,new:APPRAISAL_STATUS[x.status]}] }))} /><div className="card">{history.map(x => <button className="btn btn-o btn-sm" key={x.id} onClick={() => void act.run(() => api.version(item.period_id,staffID(item),x.id)).then(value => {if(value)setOld(value);})}>Xem bản {x.version}</button>)}{more ? <button className="btn btn-o" onClick={() => void act.run(() => api.history(item.period_id,staffID(item),String(history.at(-1)?.version ?? ""))).then(next => {if(next){setHistory([...history,...next.items]);setMore(next.more);}})}>Tải lịch sử cũ hơn</button> : null}</div></>} />
  </>;
}
function staffID(item: AppraisalItem) { return item.member.staff_id; }