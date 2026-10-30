import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { compileMenus, menuSources, readyItems, repository, validatePages } from "./gen-module-menus.mjs";

const tables = () => compileMenus(menuSources(repository).map((file) => JSON.parse(readFileSync(file, "utf8"))));
const nodes = (value) => !value || typeof value !== "object" ? [] : [value, ...Object.values(value).flatMap(nodes)];
const route = "/kn/bao-gia/de-xuat-mat-hang";

test("Báo giá có đường vào đề xuất riêng cho Tỉnh/Xã và không cấp menu đó cho TW", () => {
  const t = tables();
  for (const role of ["kn_tinh", "kn_xa"]) {
    const proposals = nodes(t.deptMenus.thong_tin_truyen_thong[role]).filter((n) => n.Route === route);
    assert.equal(proposals.length, 1, role);
    assert.equal(proposals[0].ComingSoon, false);
    assert.equal(proposals[0].Label, "Đề xuất mặt hàng");
  }
  assert.equal(nodes(t.deptMenus.thong_tin_truyen_thong.kn_tw).filter((n) => n.Route === route).length, 0);
  validatePages(repository, readyItems(t));
});

test("bật danh mục hiện có cho Admin/Tỉnh mà giữ route và không thêm bản sao", () => {
  const t = tables();
  for (const key of ["admin.pb.bao_gia_mat_hang", "tinh.bao_gia.mat_hang"]) {
    const matches = nodes(t).filter((n) => n.Key === key);
    assert.equal(matches.length, 1, key);
    assert.equal(matches[0].ComingSoon, false);
    assert.equal(matches[0].Route, "/kn/bao-gia/mat-hang");
  }
});
