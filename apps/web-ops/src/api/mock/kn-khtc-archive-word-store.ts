// GIẢ LẬP: liên kết tường minh tới byte văn bản giả lập, không dựng DOCX thật.
import type { components } from "../generated/application";
type Binding = {tenant: string; period: string; exportID: string; exportName: string; exportVersion: number; meta: components["schemas"]["KHTCArchiveWord"]; body: string};
const bindings: Binding[] = [];
export function bindArchiveMockWord(binding: Binding) { bindings.push(structuredClone(binding)); }
export function archiveMockWords(tenant: string,period: string,exportID: string) {return bindings.filter(b=>b.tenant===tenant && b.period===period && b.exportID===exportID).slice().reverse().map(b=>structuredClone(b));}
export function archiveMockWord(tenant: string,period: string,exportID: string,run: string) {return archiveMockWords(tenant,period,exportID).find(b=>b.meta.run_id===run);}
