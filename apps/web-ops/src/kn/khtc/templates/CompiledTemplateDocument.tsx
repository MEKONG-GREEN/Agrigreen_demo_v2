"use client";
import type { components } from "@/api/generated/application";
import { FIELD_LABELS, SOURCE_LABELS } from "./schema";
type S = components["schemas"];
function Tokens({ tokens }: { tokens: S["KHTCTemplateToken"][] }) {
  return tokens.map((token, index) => <span key={index} title={token.source ? `${SOURCE_LABELS[token.source as keyof typeof SOURCE_LABELS] ?? "Trạng thái nguồn"}: ${FIELD_LABELS[token.field ?? ""] ?? ""}${token.locked ? " — khóa" : ""}` : undefined} style={token.source ? { background: token.locked ? "#e4f0fa" : "#fff3cc", borderRadius: 3 } : undefined}>{token.text}</span>);
}
/** Read-only compiled content shared by Admin preview and the fixed report editor. */
export function CompiledTemplateDocument({ document }: { document: S["KHTCTemplateRendered"] }) {
  return <>
    {document.provisional.length ? <div className="alert alert-warn"><b>Tạm tính</b><ul>{document.provisional.map((reason, i) => <li key={i}>{reason}</li>)}</ul></div> : null}
    {document.nodes.map(node => <section key={node.id} style={{ marginInlineStart: `${node.depth * 12}px` }}>
      <h3>{node.number}. {node.title}</h3>
      {node.rows.map((row, i) => <div key={`${row.ref}:${i}`}>
        {row.preamble ? <p><strong>{row.preamble_tokens?.length ? <Tokens tokens={row.preamble_tokens} /> : row.preamble}</strong></p> : null}
        <p style={{ whiteSpace: "pre-wrap" }}><Tokens tokens={row.tokens} /></p>
      </div>)}
    </section>)}
  </>;
}
