// PLAN 67-1: Docker › Images, Volumes and Aufräumen in a window of the
// desktop and on the phone, with an isolated, mocked API (nothing reaches a
// NAS or a Docker daemon). Checks: unused images are offered for deletion
// (one by one and all at once), images in use never; deleting a volume asks
// plainly, names the data loss, the dangerous button sits left of
// "Abbrechen" and is not focused; the clean-up page shows the sizes and
// calls the right endpoints; refusals (409 image_in_use) are shown.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5281 node scripts/test-ui-docker-resources.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://127.0.0.1:5240";
const out = process.env.UI_OUTPUT || "out/validation/docker-resources";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);

const day = 86400;
const now = Math.floor(Date.parse("2026-11-07T10:00:00Z") / 1000);
const GB = 1e9;
function freshImages() {
  return [
    {
      id: "sha256:a1",
      repoTags: ["ghcr.io/immich-app/immich-server:v1.132"],
      size: 1.6 * GB,
      created: now - 2 * day,
      usedBy: [{ app: "immich", container: "immich-server" }],
      dangling: false,
    },
    {
      id: "sha256:a2",
      repoTags: ["postgres:16-alpine"],
      size: 0.39 * GB,
      created: now - 2 * day,
      usedBy: [{ app: "immich", container: "immich-db" }],
      dangling: false,
    },
    {
      id: "sha256:b1",
      repoTags: ["ghcr.io/immich-app/immich-server:v1.129"],
      size: 1.6 * GB,
      created: now - 90 * day,
      usedBy: [],
      dangling: false,
    },
    {
      id: "sha256:b2",
      repoTags: ["nginx:alpine"],
      size: 0.048 * GB,
      created: now - 90 * day,
      usedBy: [],
      dangling: false,
    },
    {
      id: "sha256:3f2a9c1d00ee",
      repoTags: [],
      size: 0.76 * GB,
      created: now - 95 * day,
      usedBy: [],
      dangling: true,
    },
    {
      id: "sha256:v1",
      repoTags: ["titanos/viewer:0.64"],
      size: 0.9 * GB,
      created: now - 1 * day,
      usedBy: [],
      dangling: false,
      protected: "viewer",
    },
  ].map((i) => ({
    sharedSize: 0,
    ...i,
    created: new Date(i.created * 1000).toISOString(),
  }));
}
function freshVolumes() {
  return [
    {
      name: "immich_pgdata",
      driver: "local",
      size: 0.8 * GB,
      usedBy: [{ app: "immich", container: "immich-db" }],
      titanos: true,
    },
    {
      name: "old_redis",
      driver: "local",
      size: 0.02 * GB,
      usedBy: [],
      titanos: false,
      anonymous: false,
    },
    {
      name: "titanos-cache",
      driver: "local",
      size: 0.001 * GB,
      usedBy: [],
      titanos: true,
      anonymous: false,
    },
    {
      name: "a3f9c0e8b1d24c6f9e0a7b5c4d3e2f1a0b9c8d7e6f5a4b3c2d1e0f9a8b7c6d5e",
      driver: "local",
      size: -1,
      usedBy: [],
      titanos: false,
      anonymous: true,
    },
  ];
}
function freshNetworks() {
  const net = (name, kind, o = {}) => ({
    id: "n-" + name,
    name,
    driver: "bridge",
    scope: "local",
    kind,
    subnets: o.subnets ?? [],
    gateways: [],
    internal: false,
    usedBy: o.usedBy ?? [],
    app: o.app,
    protected: kind !== "user",
  });
  return [
    net("frontend", "user", { subnets: ["172.30.0.0/24"] }),
    net("immich_default", "app", {
      app: "immich",
      usedBy: [{ app: "immich", container: "immich-server" }],
    }),
    net("bridge", "system", { subnets: ["172.17.0.0/16"] }),
  ];
}

const df = {
  anonymousReclaimable: 0,
  measuring: false,
  images: { count: 5, size: 4.4 * GB, reclaimable: 2.4 * GB },
  containers: { count: 3, size: 0.01 * GB, reclaimable: 0 },
  volumes: { count: 3, size: 0.82 * GB, reclaimable: 0.02 * GB },
  buildCache: { count: 12, size: 0.5 * GB, reclaimable: 0.5 * GB },
};

/** Routes of the Docker resource API on a context; returns the calls made. */
async function mock(made) {
  const state = {
    images: freshImages(),
    volumes: freshVolumes(),
    networks: freshNetworks(),
    calls: [],
  };
  await made.ctx.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const p = url.pathname.slice(7);
    const method = route.request().method();
    const body = route.request().postData();
    if (p === "/preferences/desktop" && method === "PUT")
      return route.fulfill({ status: 204 });
    const ours =
      p.startsWith("/docker/images") ||
      p.startsWith("/docker/volumes") ||
      p.startsWith("/docker/networks/") ||
      p === "/docker/df" ||
      p === "/docker/builder/prune";
    if (!ours) return route.fallback();
    state.calls.push({ method, p, body: body ? JSON.parse(body) : null });
    if (p === "/docker/images" && method === "GET")
      return route.fulfill({
        json: {
          images: state.images,
          totals: {
            count: state.images.length,
            size: state.images.reduce((s, i) => s + i.size, 0),
            ...(() => {
              const unused = state.images.filter(
                (i) => !i.usedBy.length && !i.protected,
              );
              const dangling = unused.filter((i) => i.dangling);
              return {
                unusedCount: unused.length,
                unusedSize: unused.reduce((s, i) => s + i.size, 0),
                danglingCount: dangling.length,
                danglingSize: dangling.reduce((s, i) => s + i.size, 0),
              };
            })(),
          },
        },
      });
    if (p.startsWith("/docker/images/") && method === "DELETE") {
      const id = decodeURIComponent(p.slice("/docker/images/".length));
      const image = state.images.find((i) => i.id === id);
      if (image?.usedBy.length)
        return route.fulfill({
          status: 409,
          json: { error: { code: "image_in_use", message: "in use" } },
        });
      state.images = state.images.filter((i) => i.id !== id);
      return route.fulfill({ status: 204 });
    }
    if (p === "/docker/images/prune") {
      const unused = JSON.parse(body ?? "{}").unused;
      const gone = state.images.filter(
        (i) => !i.usedBy.length && !i.protected && (unused || i.dangling),
      );
      state.images = state.images.filter((i) => !gone.includes(i));
      return route.fulfill({
        json: {
          deleted: gone.map((i) => i.id),
          failed: [],
          reclaimed: gone.reduce((s, i) => s + i.size, 0),
        },
      });
    }
    if (p === "/docker/volumes" && method === "GET")
      return route.fulfill({ json: { volumes: state.volumes } });
    if (p.startsWith("/docker/volumes/") && method === "DELETE") {
      const name = decodeURIComponent(p.slice("/docker/volumes/".length));
      state.volumes = state.volumes.filter((v) => v.name !== name);
      return route.fulfill({ status: 204 });
    }
    if (p === "/docker/volumes/prune") {
      const gone = state.volumes.filter(
        (v) => !v.usedBy.length && !v.titanos && v.anonymous,
      );
      state.volumes = state.volumes.filter((v) => !gone.includes(v));
      return route.fulfill({
        json: { deleted: gone.map((v) => v.name), failed: [], reclaimed: 0 },
      });
    }
    if (p === "/docker/networks/all")
      return route.fulfill({ json: { networks: state.networks } });
    if (p === "/docker/networks/bridge" && method === "POST") {
      const b = JSON.parse(body);
      const entry = {
        id: "n-" + b.name,
        name: b.name,
        driver: "bridge",
        scope: "local",
        kind: "user",
        subnets: b.subnet ? [b.subnet] : ["172.31.0.0/16"],
        gateways: [],
        internal: !!b.internal,
        usedBy: [],
        protected: false,
      };
      state.networks.push(entry);
      return route.fulfill({ status: 201, json: entry });
    }
    if (p.startsWith("/docker/networks/bridge/") && method === "DELETE") {
      const name = decodeURIComponent(p.split("/").pop());
      state.networks = state.networks.filter((n) => n.name !== name);
      return route.fulfill({ status: 204 });
    }
    if (p === "/docker/networks/prune") {
      const gone = state.networks.filter(
        (n) => n.kind === "user" && !n.usedBy.length,
      );
      state.networks = state.networks.filter((n) => !gone.includes(n));
      return route.fulfill({
        json: { deleted: gone.map((n) => n.name), failed: [], reclaimed: 0 },
      });
    }
    if (p === "/docker/df") return route.fulfill({ json: df });
    if (p === "/docker/builder/prune")
      return route.fulfill({ json: { reclaimed: 0.5 * GB } });
    return route.fallback();
  });
  return state;
}

async function inWindow(path, w = 1100) {
  const made = await create("owner", "graphit", 1600);
  await made.page.setViewportSize({ width: 1600, height: 1000 });
  await made.ctx.addInitScript(
    ({ path, w }) => {
      localStorage.removeItem("titanos.layout");
      localStorage.setItem(
        "titanos.desktop.windows.alex",
        JSON.stringify([
          {
            id: "w1",
            app: "app:docker",
            href: path,
            title: "Docker",
            x: 120,
            y: 60,
            w,
            h: 820,
            z: 10,
            minimized: false,
            snap: null,
          },
        ]),
      );
    },
    { path, w },
  );
  const state = await mock(made);
  await made.page.goto(base + "/");
  const handle = await made.page.waitForSelector("section.win iframe", {
    timeout: 15000,
  });
  const frame = await handle.contentFrame();
  await frame.waitForSelector("main", { timeout: 15000 });
  await made.page.waitForTimeout(700);
  return { ...made, frame, state };
}

/** Positions of the dialog's buttons and which one has the focus. */
async function dialogButtons(frame) {
  return frame.evaluate(() => {
    const d = document.querySelector("dialog[open]");
    const buttons = [...d.querySelectorAll(".dialog-actions button")];
    return {
      buttons: buttons.map((b) => ({
        text: b.textContent.trim(),
        x: b.getBoundingClientRect().left,
        danger: b.classList.contains("btn-danger"),
      })),
      focused: document.activeElement?.textContent?.trim() ?? "",
      text: d.innerText,
    };
  });
}

function dangerLeft(res, where) {
  const danger = res.buttons.find((b) => b.danger);
  const cancel = res.buttons.find((b) => b.text === "Abbrechen");
  assert.ok(danger && cancel, `${where}: danger and cancel buttons`);
  assert.ok(danger.x < cancel.x, `${where}: danger button left of Abbrechen`);
  assert.notEqual(
    res.focused,
    danger.text,
    `${where}: the danger button is not focused`,
  );
}

const noOverflow = async (frame, where) => {
  const over = await frame.evaluate(
    () =>
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  );
  assert.ok(over <= 1, `${where}: no sideways scrolling (${over})`);
};

// 1. Images: banner, table, in-use rows without delete, delete one, refuse, prune.
{
  const { ctx, page, frame, state } = await inWindow("/docker/images");
  await frame.getByText("3 Images werden nicht benutzt").waitFor();
  const banner = frame.locator("#images-unused");
  assert.match(await banner.innerText(), /2,41 GB/, "unused size in banner");
  const title = await page.locator("section.win header.titlebar").innerText();
  assert.match(title, /Images/, "title bar names the page");
  const side = page.locator("section.win nav [aria-current='page']");
  assert.match(await side.innerText(), /Images/, "side bar marks Images");
  const rows = frame.locator("table.resources tbody tr");
  assert.equal(await rows.count(), 6);
  assert.equal(
    await rows
      .filter({ hasText: "titanos/viewer" })
      .getByRole("button")
      .count(),
    0,
    "a protected image has no delete button",
  );
  assert.match(
    await rows.filter({ hasText: "titanos/viewer" }).innerText(),
    /geschützt/,
  );
  const inUse = rows.filter({ hasText: "postgres" });
  assert.equal(
    await inUse.getByRole("button").count(),
    0,
    "an image in use has no delete button",
  );
  await page
    .locator("section.win")
    .screenshot({ path: `${out}/images-1100.png` });
  await noOverflow(frame, "images @1100");

  // Delete one unused image.
  await rows
    .filter({ hasText: "nginx" })
    .getByRole("button", { name: /nginx.*löschen/ })
    .click();
  const res = await dialogButtons(frame);
  dangerLeft(res, "delete image");
  assert.match(res.text, /nginx:alpine/);
  await frame.locator("dialog[open] .dialog-actions .btn-danger").click();
  await frame.getByText("2 Images werden nicht benutzt").waitFor();
  assert.ok(
    state.calls.some(
      (c) =>
        (c.method === "DELETE" && c.p === "/docker/images/sha256%3Ab2") ||
        (c.method === "DELETE" && c.p.endsWith("sha256:b2")),
    ),
    "DELETE of the image sent",
  );

  // The NAS refuses an image in use (another program started it meanwhile).
  state.images.find((i) => i.id === "sha256:b1").usedBy = [
    { app: "immich", container: "immich-server" },
  ];
  await rows.filter({ hasText: "v1.129" }).getByRole("button").click();
  await frame.locator("dialog[open] .dialog-actions .btn-danger").click();
  await frame.getByText("noch von einem Container benutzt").waitFor();

  // Delete all unused at once.
  await frame.locator("#images-unused .btn-primary").click();
  const prune = await dialogButtons(frame);
  dangerLeft(prune, "prune images");
  await frame.locator("dialog[open] .dialog-actions .btn-danger").click();
  await frame.locator("#images-unused").waitFor({ state: "detached" });
  assert.deepEqual(
    state.calls.find((c) => c.p === "/docker/images/prune")?.body,
    { unused: true },
  );
  // Search narrows the list.
  await frame.getByPlaceholder("Images suchen").fill("postgres");
  assert.equal(await rows.count(), 1);
  await ctx.close();
}

// 2. Volumes: data-loss confirmation, one and all.
{
  const { ctx, page, frame, state } = await inWindow("/docker/volumes", 800);
  await frame.getByText("2 Volumes werden nicht benutzt").waitFor();
  assert.equal(
    await frame
      .locator("table.resources tbody tr", { hasText: "titanos-cache" })
      .getByRole("button")
      .count(),
    0,
    "a volume of TitanOS has no delete button",
  );
  await page
    .locator("section.win")
    .screenshot({ path: `${out}/volumes-800.png` });
  await noOverflow(frame, "volumes @800");
  const rows = frame.locator("table.resources tbody tr");
  assert.equal(
    await rows.filter({ hasText: "immich_pgdata" }).getByRole("button").count(),
    0,
    "a volume in use has no delete button",
  );
  assert.match(
    await rows.filter({ hasText: "a3f9c0e8" }).innerText(),
    /unbekannt/,
    "unknown size is said, not guessed",
  );
  await rows.filter({ hasText: "old_redis" }).getByRole("button").click();
  const one = await dialogButtons(frame);
  dangerLeft(one, "delete volume");
  assert.match(one.text, /old_redis/);
  assert.match(one.text, /endgültig/, "says the data is lost for good");
  assert.equal(
    await frame.locator("dialog[open] input").count(),
    0,
    "nothing to type",
  );
  // Enter on the focused "Abbrechen" does not delete.
  await page.keyboard.press("Enter");
  await page.waitForTimeout(300);
  assert.ok(
    !state.calls.some((c) => c.method === "DELETE"),
    "Enter does not delete",
  );
  if (await frame.locator("dialog[open]").count())
    await frame.getByRole("button", { name: "Abbrechen" }).click();

  await frame.locator("#volumes-unused .btn").click();
  const all = await dialogButtons(frame);
  dangerLeft(all, "prune volumes");
  assert.match(all.text, /a3f9c0e8/, "the anonymous volume is listed");
  assert.doesNotMatch(all.text, /old_redis/, "named volumes are not taken");
  assert.match(all.text, /nicht rückgängig/);
  await frame.locator("dialog[open] .dialog-actions .btn-danger").click();
  await frame.getByText("1 Volume wird nicht benutzt").waitFor();
  assert.deepEqual(
    state.calls.find((c) => c.p === "/docker/volumes/prune")?.body,
    { anonymousOnly: true },
  );
  await ctx.close();
}

// 3. Clean-up: sizes per kind and the right endpoints.
{
  const { ctx, page, frame, state } = await inWindow("/docker/cleanup", 800);
  await frame.locator("#cleanup-cache").waitFor();
  await page
    .locator("section.win")
    .screenshot({ path: `${out}/cleanup-800.png` });
  await noOverflow(frame, "cleanup @800");
  const text = await frame.locator("main").innerText();
  assert.match(text, /Build-Cache/);
  assert.match(
    await frame.locator("#cleanup-images button").innerText(),
    /2,41 GB/,
  );
  await frame.locator("#cleanup-dangling button").click();
  dangerLeft(await dialogButtons(frame), "dangling");
  await frame.locator("dialog[open] .dialog-actions .btn-danger").click();
  await frame.getByText("frei geworden").waitFor();
  assert.deepEqual(
    state.calls.find((c) => c.p === "/docker/images/prune")?.body,
    { unused: false },
    "dangling images only",
  );
  await frame.locator("#cleanup-cache button").click();
  await frame.locator("dialog[open] .dialog-actions .btn-danger").click();
  await page.waitForTimeout(400);
  assert.ok(state.calls.some((c) => c.p === "/docker/builder/prune"));
  await frame.locator("#cleanup-volumes button").click();
  const vol = await dialogButtons(frame);
  dangerLeft(vol, "cleanup volumes");
  assert.match(vol.text, /a3f9c0e8/);
  await frame.getByRole("button", { name: "Abbrechen" }).click();
  await ctx.close();
}

// 4. Networks: all networks, kept ones with a reason; own bridge networks.
{
  const { ctx, page, frame, state } = await inWindow("/docker/networks", 800);
  const rows = frame.locator("table.nets tbody tr");
  await rows.first().waitFor();
  assert.equal(await rows.count(), 3);
  assert.match(
    await rows.filter({ hasText: "immich_default" }).innerText(),
    /App immich/,
  );
  assert.equal(
    await rows
      .filter({ has: frame.locator(".nname", { hasText: /^bridge$/ }) })
      .getByRole("button")
      .count(),
    0,
    "a system network cannot be removed",
  );
  await page
    .locator("section.win")
    .screenshot({ path: `${out}/networks-800.png` });
  await noOverflow(frame, "networks @800");
  // New bridge network: a public subnet is refused before sending.
  await frame.getByRole("button", { name: "Neues Netzwerk" }).click();
  const dialog = frame.locator("dialog[open]");
  await dialog.getByLabel("Name").fill("backend");
  await dialog.getByLabel("Subnetz (optional)").fill("8.8.8.0/24");
  assert.ok(await dialog.getByRole("button", { name: "Anlegen" }).isDisabled());
  await dialog.getByLabel("Subnetz (optional)").fill("172.29.0.0/24");
  await dialog.getByLabel("Intern (ohne Internet)").check();
  await dialog.getByRole("button", { name: "Anlegen" }).click();
  await rows.filter({ hasText: "backend" }).waitFor();
  assert.deepEqual(
    state.calls.find((c) => c.p === "/docker/networks/bridge")?.body,
    { name: "backend", subnet: "172.29.0.0/24", internal: true },
  );
  // Remove one with a plain confirmation.
  await rows
    .filter({ hasText: "frontend" })
    .getByRole("button", { name: /frontend/ })
    .click();
  dangerLeft(await dialogButtons(frame), "remove network");
  await frame.locator("dialog[open] .dialog-actions .btn-danger").click();
  await rows.filter({ hasText: "frontend" }).waitFor({ state: "detached" });
  // The rest at once.
  await frame.getByRole("button", { name: /Ungenutzte entfernen/ }).click();
  await frame.locator("dialog[open] .dialog-actions .btn-danger").click();
  await rows.filter({ hasText: "backend" }).waitFor({ state: "detached" });
  await ctx.close();
}

// 5. The phone: the three pages full screen without sideways scrolling.
for (const path of [
  "/docker/images",
  "/docker/volumes",
  "/docker/networks",
  "/docker/cleanup",
]) {
  const made = await create("owner", "graphit", 390);
  await made.page.setViewportSize({ width: 390, height: 844 });
  await made.ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
  await mock(made);
  await made.page.goto(base + path);
  await made.page.waitForSelector(".phone-bar", { timeout: 15000 });
  await made.page.waitForSelector("main :is(.tile, .srow)", { timeout: 15000 });
  await made.page.waitForTimeout(500);
  await noOverflow(made.page.mainFrame(), `${path} @phone`);
  await made.page.screenshot({
    path: `${out}/phone-${path.split("/").pop()}.png`,
    fullPage: true,
  });
  await made.ctx.close();
}

await browser.close();
assert.deepEqual(errors, [], "no script errors");
console.log("docker resources ok");
