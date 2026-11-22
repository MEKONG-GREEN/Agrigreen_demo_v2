"use client";

import Link from "next/link";
import { useParams, usePathname, useRouter, useSearchParams } from "next/navigation";
import { useEffect, useMemo, useRef, useState } from "react";
import { fieldErrorMap } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { NumberField, SelectField, TextField } from "@/components/form";
import { BackLink, DetailPage, LabelValueBlock, LogPanel, type LogEntry } from "@/components/patterns/detail";
import { DataTable } from "@/components/patterns/list";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import { createHrmApi, dmy, RULE_LABEL, vnDecimal, type HrmAchievementConversion, type HrmApi, type HrmItem, type HrmPromotionRules, type HrmRule, type HrmRuleKey } from "./api";
import { catalogBase } from "./catalog-form";
import { effectiveRule, initialRuleValues, isRuleKey, parseRuleVersion, PROMOTION_INPUTS, RULE_RATINGS, rulePath, ruleToday, validRuleDay, validateRule, type RuleValues } from "./rule-form";

const RATING_NAMES: Record<string,string> = { HTXSNV: "Hoàn thành xuất sắc nhiệm vụ", HTTNV: "Hoàn thành tốt nhiệm vụ", HTNV: "Hoàn thành nhiệm vụ", KHTNV: "Không hoàn thành nhiệm vụ" };
function payloadValues(key: HrmRuleKey, rule: HrmRule): { label: string; value: string }[] {
 if (key === "achievement_conversion") return [{ label: "Quy đổi thành tích", value: (rule.payload as unknown as HrmAchievementConversion).rules.map((r) => `${r.award_code}: ${r.months} tháng`).join(" · ") }];
 const p = rule.payload as unknown as HrmPromotionRules;
 return [{ label: "Xếp loại tối thiểu mỗi năm (NLTX)", value: RATING_NAMES[p.min_rating] ?? p.min_rating },
  ...PROMOTION_INPUTS.map((f) => ({ label: f.label, value: f.pct ? vnDecimal(String(p[f.key])) : String(p[f.key]) })),
  { label: "Mốc nhắc (tháng tới)", value: p.remind_months.join(", ") },
  { label: "Kéo dài theo kỷ luật", value: Object.entries(p.discipline_extension_months).map(([code, months]) => `${code}: ${months} tháng`).join(" · ") || "—" }];
}
/** Recorded metadata and immutable payloads only; this API does not return an actor. */
export function ruleHistory(key: HrmRuleKey, versions: HrmRule[]): LogEntry[] {
 const sorted = [...versions].sort((a,b) => b.version-a.version);
 return sorted.map((v,i) => {
  const previous = sorted[i+1];
  const old = previous ? new Map(payloadValues(key,previous).map((r) => [r.label,r.value])) : new Map<string,string>();
  return { event: `Phiên bản ${v.version}${v.placeholder ? " · Mẫu" : ""}`, actor_display: "Chưa có thông tin người thực hiện", at: v.recorded_at, reason: v.note || null,
   changes: [{ field: "Hiệu lực từ", old: previous ? dmy(previous.valid_from) : null, new: dmy(v.valid_from) }, ...payloadValues(key,v).map((r) => ({ field: r.label, old: old.get(r.label) ?? null, new: r.value }))].filter((r) => r.old !== r.new) };
 });
}

export function HrmRuleScreen({ create = false }: { create?: boolean }) {
 const auth = useAuth();
 const { key } = useParams<{ key: string }>();
 const query = useSearchParams();
 const day = query.get("at") ?? ruleToday();
 const version = parseRuleVersion(query.get("version"));
 const base = catalogBase(usePathname());
 // Same-account selected sessions, tenant/role/MFA changes and support mode all reset the draft/permission read.
 const scope = JSON.stringify([auth.status,auth.principal?.subject,auth.principal?.tenantId,auth.principal?.orgRef,auth.principal?.roles?.slice().sort(),auth.principal?.mfa,auth.principal?.scopeAll,auth.principal?.scopeUnits?.slice().sort(),auth.kn?.status,auth.kn?.context?.context_id,auth.kn?.support?.target,auth.kn?.support?.reason]);
 if (!isRuleKey(key) || !validRuleDay(day) || version === null || query.getAll("at").length > 1 || query.getAll("version").length > 1) {
  return <><BackLink href={base}>Danh mục nhân sự</BackLink><p role="alert">Khóa tham số, ngày hiệu lực hoặc phiên bản trên URL không hợp lệ.</p></>;
 }
 return <RuleAccess key={`${scope}/${key}/${day}/${version ?? "effective"}/${create}`} api={auth.api} rkey={key} base={base} day={day} version={version} create={create} />;
}

function RuleAccess({ api, rkey, base, day, version, create }: { api: Parameters<typeof createHrmApi>[0]; rkey: HrmRuleKey; base: string; day: string; version?: number; create: boolean }) {
 const ha = useMemo(() => createHrmApi(api),[api]);
 const status = useLoad(() => ha.status(),[ha]);
 const allowed = status.data?.can_manage === true;
 const loaded = useLoad(status.data && (!create || allowed) ? async () => {
  const [data, refs] = await Promise.all([ha.rule(rkey,day),ha.list(rkey === "promotion" ? "discipline_form" : "award_form",true)]);
  return { versions: data.versions, references: refs.items };
 } : null,[ha,rkey,day,create,allowed,status.data]);
 return <LoadState s={status}>{() => create && !allowed ? <><BackLink href={base}>Danh mục nhân sự</BackLink><p role="alert">Bạn không có quyền thêm phiên bản tham số.</p></> : <LoadState s={loaded}>{({versions,references}) => {
  const selected = version === undefined ? effectiveRule(versions,day) : versions.find((v) => v.version === version) ?? null;
  if (version !== undefined && !selected) return <><BackLink href={base}>Danh mục nhân sự</BackLink><p role="alert">Không tìm thấy phiên bản tham số.</p></>;
  return create ? <RuleEditor ha={ha} rkey={rkey} base={base} day={day} selected={selected} versions={versions} references={references} />
   : <RuleDetail rkey={rkey} base={base} day={day} selected={selected} versions={versions} references={references} canManage={allowed} />;
 }}</LoadState>}</LoadState>;
}

function RuleDetail({rkey,base,day,selected,versions,references,canManage}: {rkey:HrmRuleKey;base:string;day:string;selected:HrmRule|null;versions:HrmRule[];references:HrmItem[];canManage:boolean}) {
 const router=useRouter();
 const [date,setDate]=useState(day);
 const refName=(code:string)=>references.find((r)=>r.code===code)?.name ?? code;
 return <DetailPage back={{href:base,label:"Danh mục nhân sự"}} title={RULE_LABEL[rkey]}
  actions={canManage ? <Link className="btn btn-p btn-sm" href={rulePath(base,rkey,day,true,selected?.version)}>Phiên bản mới</Link> : undefined}
  log={<LogPanel entries={ruleHistory(rkey,versions)} empty="Chưa có phiên bản tham số." />}>
  <p className="mute">Quy tắc theo cấu hình triển khai. Dữ liệu mẫu cần Phòng HCTH xác nhận (Q-070).</p>
  <div className="field"><label htmlFor="rule-day">Hiệu lực tại ngày</label><input id="rule-day" type="date" value={date} aria-invalid={!validRuleDay(date) || undefined} onChange={(e)=>{const next=e.target.value;setDate(next);if(validRuleDay(next))router.replace(rulePath(base,rkey,next));}} />{!validRuleDay(date)?<span className="err" role="alert">Ngày hiệu lực không hợp lệ.</span>:null}</div>
  {selected ? <>
   <LabelValueBlock title="Phiên bản đang xem" items={[{label:"Phiên bản",value:selected.version},{label:"Hiệu lực từ",value:dmy(selected.valid_from)},{label:"Ghi chú (căn cứ)",value:selected.note},{label:"Dữ liệu mẫu",value:selected.placeholder?"Mẫu — chờ HCTH xác nhận (Q-070)":"Không"}]} />
   {rkey === "promotion" ? <LabelValueBlock title="Nội dung quy tắc" items={payloadValues(rkey,selected)} /> : <DataTable caption="Bảng quy đổi thành tích" rows={(selected.payload as unknown as HrmAchievementConversion).rules} rowKey={(r)=>r.award_code} columns={[{key:"code",header:"Hình thức khen thưởng",cell:(r)=>`${refName(r.award_code)} (${r.award_code})`},{key:"months",header:"Số tháng nâng trước hạn",cell:(r)=>r.months}]} />}
  </> : <p>Chưa có phiên bản hiệu lực tại ngày này.</p>}
  <DataTable caption="Lịch sử tham số" rows={[...versions].sort((a,b)=>b.version-a.version)} rowKey={(v)=>String(v.version)} columns={[
   {key:"version",header:"Phiên bản",cell:(v)=><Link href={rulePath(base,rkey,day,false,v.version)} aria-label={`Xem phiên bản ${v.version}`}>v{v.version}</Link>},
   {key:"from",header:"Hiệu lực từ",cell:(v)=>dmy(v.valid_from)},{key:"note",header:"Ghi chú (căn cứ)",cell:(v)=>v.note||"—"},{key:"source",header:"Dữ liệu",cell:(v)=>v.placeholder?"Mẫu (Q-070)":"Phiên bản đã ghi"},
  ]} />
 </DetailPage>;
}

function RuleEditor({ha,rkey,base,day,selected,versions,references}: {ha:HrmApi;rkey:HrmRuleKey;base:string;day:string;selected:HrmRule|null;versions:HrmRule[];references:HrmItem[]}) {
 const [values,setValues]=useState<RuleValues>(()=>initialRuleValues(rkey,selected,day));
 const [errors,setErrors]=useState<Record<string,string>>({});
 const router=useRouter(); const act=useAction(); const live=useRef(true); const saving=useRef(false);
 useEffect(()=>{live.current=true;return()=>{live.current=false;};},[]);
 const server=act.problem?fieldErrorMap(act.problem):{};
 const error=(field:string)=>errors[field]||server[field === "from"?"valid_from":field]||server[`payload.${field}`];
 const patch=(part:Partial<RuleValues>)=>setValues((v)=>({...v,...part}));
 const blur=(field:string,message:string|undefined)=>setErrors((old)=>({...old,[field]:message??validateRule(rkey,values,references).errors[field]??""}));
 const back=rulePath(base,rkey,day,false,selected?.version);
 const save=async()=>{
  if(saving.current)return;
  const checked=validateRule(rkey,values,references);setErrors(checked.errors);if(!checked.valid)return;
  saving.current=true;
  try { const saved=await act.run(()=>ha.addRule(rkey,checked.body));if(saved&&live.current)router.push(rulePath(base,rkey,checked.body.valid_from,false,saved.version)); }
  finally {saving.current=false;}
 };
 return <DetailPage back={{href:back,label:RULE_LABEL[rkey]}} title={`${RULE_LABEL[rkey]} — phiên bản mới`}
  actions={<><Link className="btn btn-o btn-sm" href={back}>Hủy</Link><button className="btn btn-p btn-sm" type="submit" form="hrm-rule" disabled={act.busy}>Lưu</button></>}
  log={<LogPanel entries={ruleHistory(rkey,versions)} empty="Lịch sử xuất hiện sau khi lưu phiên bản đầu tiên." />}>
  {act.view}<p>Phiên bản mới có ngày hiệu lực riêng; các phiên bản đã ghi được giữ nguyên. Cấu hình mẫu chờ HCTH xác nhận (Q-070).</p>
  {selected?<p className="mute">Nội dung được sao từ phiên bản {selected.version}, hiệu lực từ {dmy(selected.valid_from)}.</p>:null}
  <form id="hrm-rule" noValidate onSubmit={(e)=>{e.preventDefault();void save();}}>
   <div className="grid2">
    <div className="field"><label htmlFor="rule-from">Ngày hiệu lực <span className="req" aria-hidden="true">*</span></label><input id="rule-from" type="date" value={values.from} disabled={act.busy} aria-required="true" aria-invalid={!!error("from")||undefined} aria-describedby={error("from")?"rule-from-error":undefined} onChange={(e)=>patch({from:e.target.value})}/>{error("from")?<span className="err" id="rule-from-error" role="alert">{error("from")}</span>:null}</div>
    <TextField label="Ghi chú (căn cứ)" value={values.note} maxLength={500} disabled={act.busy} onChange={(note)=>patch({note})} error={error("note")} />
    {rkey === "promotion" ? <>
     <SelectField label="Xếp loại tối thiểu mỗi năm (NLTX)" required value={values.rating} onChange={(rating)=>patch({rating})} disabled={act.busy} options={RULE_RATINGS.map((value)=>({value,label:RATING_NAMES[value]!}))} error={error("rating")||server.min_rating} />
     {PROMOTION_INPUTS.map((f)=><NumberField key={f.key} label={f.label} required value={values.fields[f.key]} disabled={act.busy}
      onChange={(value)=>patch({fields:{...values.fields,[f.key]:value}})} rule={{integer:!f.pct,positive:f.min>0,max:f.max,maxDecimals:f.pct?2:0,maxInputLength:32}}
      error={error(f.key)} onBlurError={(message)=>blur(f.key,message)} hint={`${f.min}–${f.max}${f.pct?"%; tối đa 2 số lẻ":" (số nguyên)"}.`} />)}
    </>:null}
   </div>
   {rkey === "promotion" ? <fieldset><legend>Mốc nhắc đến kỳ — tăng dần, không trùng</legend><div className="grid2">{values.reminders.map((value,i)=><div key={i}>
    <NumberField label={`Mốc nhắc ${i+1} (tháng tới)`} required value={value} disabled={act.busy} onChange={(value)=>patch({reminders:values.reminders.map((v,j)=>j===i?value:v)})}
     rule={{integer:true,positive:true,max:24,maxInputLength:32}} error={error(`reminder_${i}`)} onBlurError={(message)=>blur(`reminder_${i}`,message)} hint="Số nguyên từ 1 đến 24." />
    <button type="button" className="btn btn-o btn-sm" disabled={act.busy||values.reminders.length<=1} onClick={()=>{patch({reminders:values.reminders.filter((_,j)=>j!==i)});setErrors({});}} aria-label={`Bỏ mốc nhắc ${i+1}`}>Bỏ mốc</button>
   </div>)}</div>{error("reminders")?<p role="alert" className="err">{error("reminders")}</p>:null}<button type="button" className="btn btn-o btn-sm" disabled={act.busy||values.reminders.length>=6} onClick={()=>patch({reminders:[...values.reminders,""]})}>Thêm mốc nhắc</button></fieldset>:null}
   <fieldset><legend>{rkey==="promotion"?"Kéo dài thời gian giữ bậc theo kỷ luật":"Bảng quy đổi thành tích"}</legend>
    <DataTable caption={rkey==="promotion"?"Thời gian theo kỷ luật":"Các dòng quy đổi thành tích"} rows={values.rows.map((row,index)=>({...row,index}))} rowKey={(row)=>String(row.index)} columns={[
     {key:"code",header:rkey==="promotion"?"Hình thức kỷ luật":"Hình thức khen thưởng",cell:(row)=><SelectField label={`Mục ${row.index+1}`} required value={row.code} disabled={act.busy}
      options={references.filter((r)=>r.active||r.code===row.code).map((r)=>({value:r.code,label:`${r.name} (${r.code})${r.active?"":" — đã ngừng sử dụng"}`}))}
      onChange={(code)=>patch({rows:values.rows.map((r,i)=>i===row.index?{...r,code}:r)})} error={error(`row_${row.index}_code`)} />},
     {key:"months",header:"Số tháng",cell:(row)=><NumberField label={`Số tháng ${row.index+1}`} required value={row.months} disabled={act.busy}
      onChange={(months)=>patch({rows:values.rows.map((r,i)=>i===row.index?{...r,months}:r)})} rule={{integer:true,positive:rkey==="achievement_conversion",max:rkey==="promotion"?60:36,maxInputLength:32}}
      error={error(`row_${row.index}_months`)} onBlurError={(message)=>blur(`row_${row.index}_months`,message)} hint={rkey==="promotion"?"0–60 tháng.":"1–36 tháng."} />},
     {key:"remove",header:"Thao tác",cell:(row)=><button type="button" className="btn btn-o btn-sm" disabled={act.busy} aria-label={`Bỏ dòng ${row.index+1}`} onClick={()=>{patch({rows:values.rows.filter((_,i)=>i!==row.index)});setErrors({});}}>Bỏ dòng</button>},
    ]} empty={rkey==="promotion"?"Không có hình thức kéo dài giữ bậc.":"Chưa có dòng quy đổi."} />
    {error("rows")?<p className="err" role="alert">{error("rows")}</p>:null}
    <button type="button" className="btn btn-o btn-sm" disabled={act.busy||values.rows.length>=references.filter((r)=>r.active).length||(rkey==="achievement_conversion"&&values.rows.length>=50)} onClick={()=>patch({rows:[...values.rows,{code:"",months:""}]})}>Thêm dòng</button>
   </fieldset>
  </form>
 </DetailPage>;
}
