// MapLibre GL v6 (ESM) nạp web worker từ tệp riêng cạnh module chính. Bundler của Next
// không chép tệp này, nên ta chép worker + chunk dùng chung vào public/maplibre/ và
// gọi setWorkerUrl("/maplibre/maplibre-gl-worker.mjs") trong MapView.
import { copyFileSync, mkdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const dist = dirname(require.resolve("maplibre-gl/package.json")) + "/dist";
const out = join(root, "public", "maplibre");
mkdirSync(out, { recursive: true });
for (const f of ["maplibre-gl-worker.mjs", "maplibre-gl-shared.mjs"]) {
  copyFileSync(join(dist, f), join(out, f));
}
console.warn(`maplibre worker → ${out}`);
