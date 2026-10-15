// Preserve YAML and operation order while assembling independently owned fragments.
import { readFileSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { filesBelow, repository } from "./gen-module-menus.mjs";
const kinds = new Set(["schemas", "responses", "parameters", "examples", "requestBodies", "headers", "securitySchemes", "links", "callbacks", "pathItems"]);
export function parseFragment(text, source) {
  const blocks = [];
  let section, component = false, block;
  function flush() {
    if (!block) return;
    const regex = section === "paths" ? /^  (\/[^\n]+):\n/u : /^    ([A-Za-z_][A-Za-z0-9_.-]*):[^\n]*\n/u;
    const match = block.text.match(regex);
    if (!match || !section) throw Error(`invalid OpenAPI block in ${source}`);
    blocks.push({ ...block, section, key: match[1], source }); block = undefined;
  }
  for (const line of text.match(/[^\n]*\n|[^\n]+$/gu) ?? []) {
    if (line === "# @ag-end\n") { flush(); continue; }
    if (line === "paths:\n") { flush(); section = "paths"; component = false; continue; }
    if (line === "components:\n") { flush(); section = undefined; component = true; continue; }
    const header = component && line.match(/^  (\w+):\n$/u);
    if (header) {
      flush(); section = header[1];
      if (!kinds.has(section)) throw Error(`unknown OpenAPI component kind ${section}`);
      continue;
    }
    const marker = line.match(/^# @ag-order: (\d+)\n$/u);
    if (marker) { flush(); block = { order: Number(marker[1]), text: "" }; continue; }
    if (block) block.text += line;
    else if (line.trim()) throw Error(`OpenAPI content without order marker in ${source}`);
  }
  flush(); return blocks;
}
export function assembleOpenAPI(header, blocks) {
  const identities = new Set();
  for (const block of blocks) {
    const id = `${block.section}/${block.key}`;
    if (identities.has(id)) throw Error(`duplicate OpenAPI definition ${id}`);
    identities.add(id);
  }
  const ordered = [...blocks].sort((a, b) => a.order - b.order || a.source.localeCompare(b.source, "en") || a.key.localeCompare(b.key, "en"));
  const paths = ordered.filter((block) => block.section === "paths");
  const components = new Map();
  for (const block of ordered.filter((block) => block.section !== "paths")) {
    if (!components.has(block.section)) components.set(block.section, []);
    components.get(block.section).push(block.text);
  }
  return header + "paths:\n" + paths.map((block) => block.text).join("") + "components:\n" + [...components].map(([section, texts]) => `  ${section}:\n${texts.join("")}`).join("");
}
export function generateOpenAPI(root, check = false) {
  const directory = join(root, "contracts/openapi");
  for (const surface of ["application", "admin"]) {
    const blocks = filesBelow(join(directory, "modules"), (file) => file.endsWith(`/${surface}.yaml`) || file.endsWith(`\\${surface}.yaml`)).flatMap((file) => parseFragment(readFileSync(file, "utf8").replaceAll("\r\n", "\n"), file));
    const output = assembleOpenAPI(readFileSync(join(directory, `${surface}.base.yaml`), "utf8").replaceAll("\r\n", "\n"), blocks);
    const path = join(directory, `${surface}.yaml`), old = readFileSync(path, "utf8").replaceAll("\r\n", "\n");
    if (check && output !== old) throw Error(`OpenAPI output is stale: ${surface}`);
    if (!check && output !== old) writeFileSync(path, output);
  }
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) generateOpenAPI(repository, process.argv.includes("--check"));
