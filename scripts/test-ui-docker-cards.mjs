// Docker cards (owner's decision of 4 Nov 2026, variant B): all cards of one
// size, a Compose stack is one card (closed: running/total, CPU/RAM, services
// as chips; open: its services follow as cards), order "Custom | A–Z" in the
// editor, cards dragged at their handle or moved with the arrow keys, saved in
// the docker preference only on a change; one card size S/M/L for all cards.
// Desktop and 390 px, graphit/hell.
// Isolated API data only.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5373 node scripts/test-ui-docker-cards.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const base = process.env.UI_URL || "http://127.0.0.1:5373";
const out = process.env.UI_OUTPUT || "out/validation/docker-karten";
await mkdir(out, { recursive: true });
const errors = [],
  create = fixtureFactory(browser, errors);

const time = "2026-11-04T08:00:00Z";
const history = (cpu) => Array.from({ length: 30 }, (_, i) => Math.max(0.2, cpu + Math.sin(i / 3) * cpu * 0.4));
const port = (host, protocol = "tcp") => ({ host, container: host, protocol });
const container = (name, o = {}) => ({
  id: name,
  name,
  image: o.image ?? name + ":latest",
  state: o.state ?? "running",
  since: time,
  ports: o.ports ?? [],
  app: o.app ?? name,
  service: o.service ?? name,
  managed: true,
  cpuPercent: o.cpu ?? 1.2,
  memoryBytes: o.mem ?? 180e6,
  cpuHistory: o.state === "exited" ? [] : history(o.cpu ?? 1.2),
  webui: o.webui ?? "",
  publicUrl: "",
  icon: "",
  updateAvailable: !!o.update,
});
const containers = () => [
  container("paperless-webserver-1", { app: "paperless", service: "webserver", image: "ghcr.io/paperless-ngx/paperless-ngx:2.14", cpu: 3.4, mem: 612e6, webui: "8000", ports: [port(8000)] }),
  container("paperless-db-1", { app: "paperless", service: "db", image: "postgres:16", cpu: 0.8, mem: 96e6 }),
  container("paperless-broker-1", { app: "paperless", service: "broker", image: "redis:7", cpu: 0.3, mem: 12e6, update: true }),
  container("jellyfin", { image: "jellyfin/jellyfin:10.10", cpu: 6.1, mem: 1.4e9, webui: "8096", ports: [port(8096)] }),
  container("adguard", { image: "adguard/adguardhome:latest", cpu: 0.6, mem: 74e6, webui: "3000", ports: [port(53, "udp"), port(3000)] }),
  container("mosquitto", { image: "eclipse-mosquitto:2", cpu: 0.1, mem: 8e6, ports: [port(1883)] }),
  container("vaultwarden", { image: "vaultwarden/server:latest", state: "exited", cpu: 0, mem: 0 }),
];
const apps = ["paperless", "jellyfin", "adguard", "mosquitto", "vaultwarden"];

async function open(theme = "graphit", width = 1440, prefs = {}) {
  const made = await create("owner", theme, width);
  made.data.dockerContainers = containers();
  made.data.dockerApps = apps;
  made.data.prefs.docker = { version: 1, view: "cards", order: [], details: true, primaryServices: {}, ...prefs };
  await made.page.goto(base + "/docker");
  await made.page.locator(".cards .cell").first().waitFor();
  return made;
}
// The names of the top-level cards in reading order.
const order = (page) =>
  page.locator(".cards > .cell").evaluateAll((cells) => cells.map((cell) => cell.dataset.group));
const cardHeights = (page) =>
  page.locator(".cards .body > article").evaluateAll((cards) => cards.map((card) => Math.round(card.getBoundingClientRect().height)));
const noScroll = (page) => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1);
async function customize(page) {
  await page.getByRole("button", { name: "Docker-Einstellungen" }).click();
  await page.getByRole("menuitem", { name: /Seite anpassen/ }).click();
  await page.locator(".layout-editor").waitFor();
}

try {
  // 1. Closed stack: one card with x/y, CPU/RAM, chips; no service cards.
  {
    const { ctx, page, data } = await open();
    const stack =page.getByRole("article", { name: "Compose-Stack paperless" });
    await stack.waitFor();
    assert.match(await stack.innerText(), /3\/3 laufen/);
    assert.match(await stack.innerText(), /Stack · 3 Dienste/);
    assert.deepEqual(
      await stack.locator(".service").evaluateAll((items) => items.map((item) => item.firstChild.nextSibling.textContent.trim())),
      ["webserver", "db", "broker"],
    );
    assert.equal(await stack.locator(".service .arrow").count(), 1, "update arrow on broker");
    assert.equal(await page.locator(".drawer").count(), 0);
    assert.equal(await stack.getByRole("button", { name: "Dienste" }).getAttribute("aria-expanded"), "false");
    // Default own order without a saved order: Docker's order.
    assert.deepEqual(await order(page), ["app:paperless", "app:jellyfin", "app:adguard", "app:mosquitto", "app:vaultwarden"]);
    // All cards the same height.
    const heights = await cardHeights(page);
    assert.equal(new Set(heights).size, 1, `equal heights ${heights}`);
    await page.screenshot({ path: `${out}/b1-stack-zu-graphit-1440.png`, fullPage: true });

    // 2. Open stack: the services as cards directly after it, named by service.
    await stack.getByRole("button", { name: "Dienste" }).click();
    const drawer = page.locator(".drawer");
    await drawer.waitFor();
    assert.equal(await stack.getByRole("button", { name: "Dienste" }).getAttribute("aria-expanded"), "true");
    assert.equal(
      await page.evaluate(() => document.querySelector('[data-group="app:paperless"]').nextElementSibling.classList.contains("drawer")),
      true,
    );
    assert.deepEqual(
      await drawer.locator("article .name").allInnerTexts(),
      ["webserver", "db", "broker"],
    );
    const all = await cardHeights(page);
    assert.equal(new Set(all).size, 1, `service cards same height ${all}`);
    await page.screenshot({ path: `${out}/b2-stack-offen-graphit-1440.png`, fullPage: true });
    // Stop all of the stack: one request per running service.
    await stack.getByRole("button", { name: "Alle stoppen" }).click();
    await page.waitForTimeout(500);
    assert.deepEqual(
      data.requests.filter(([method, path]) => method === "POST" && path.endsWith("/stop")).map(([, path]) => path),
      ["paperless-webserver-1", "paperless-db-1", "paperless-broker-1"].map((id) => `/docker/containers/${id}/stop`),
    );
    await ctx.close();
  }

  // 3. A–Z vs own order, drag reorder saved, keyboard move, no write without change.
  {
    const { ctx, page, data } = await open("graphit", 1440, { order: ["app:vaultwarden", "container:gone"] });
    // The saved order puts vaultwarden first; the absent "gone" stays saved.
    assert.equal((await order(page))[0], "app:vaultwarden");
    await customize(page);
    // No change: saving writes nothing.
    await page.getByRole("button", { name: "Layout speichern" }).click();
    await page.locator(".layout-editor").waitFor({ state: "detached" });
    assert.equal(data.putDocker.length, 0, "unchanged save must not write");

    await customize(page);
    assert.equal(await page.locator(".cards .handle").count(), 5);
    await page.screenshot({ path: `${out}/b3-bearbeiten-graphit-1440.png`, fullPage: true });
    // Drag the third card onto the first.
    const before = await order(page);
    const from = page.locator(`.cards [data-group="${before[2]}"] .handle`);
    const to = page.locator(`.cards [data-group="${before[0]}"] .body`);
    const a = await from.boundingBox(), b = await to.boundingBox();
    await page.mouse.move(a.x + 20, a.y + a.height / 2);
    await page.mouse.down();
    await page.mouse.move(a.x + 40, a.y + 40, { steps: 4 });
    await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2, { steps: 12 });
    await page.mouse.up();
    const dragged = await order(page);
    assert.equal(dragged[0], before[2], `drag moved ${dragged}`);

    // Keyboard: the handle of the first card, ArrowRight moves it one later and keeps focus.
    const first = dragged[0];
    await page.locator(`.cards [data-group="${first}"] .handle`).focus();
    await page.keyboard.press("ArrowRight");
    const keyed = await order(page);
    assert.equal(keyed[1], first, `keyboard moved ${keyed}`);
    assert.equal(await page.evaluate(() => document.activeElement?.closest("[data-group]")?.dataset.group), first);
    assert.match(await page.locator(".layout-editor [role=status]").innerText(), /Platz 2 von 5/);
    // Escape during a drag puts the card back.
    const h = await page.locator(`.cards [data-group="${keyed[3]}"] .handle`).boundingBox();
    const t0 = await page.locator(`.cards [data-group="${keyed[0]}"] .body`).boundingBox();
    await page.mouse.move(h.x + 20, h.y + h.height / 2);
    await page.mouse.down();
    await page.mouse.move(t0.x + t0.width / 2, t0.y + t0.height / 2, { steps: 12 });
    assert.notDeepEqual(await order(page), keyed);
    await page.keyboard.press("Escape");
    await page.mouse.up();
    assert.deepEqual(await order(page), keyed, "escape restores");

    await page.getByRole("button", { name: "Layout speichern" }).click();
    await page.locator(".layout-editor").waitFor({ state: "detached" });
    assert.equal(data.putDocker.length, 1);
    assert.equal(data.putDocker[0].sort, "own");
    assert.deepEqual(data.putDocker[0].order.filter((id) => id !== "container:gone"), keyed);
    assert.ok(data.putDocker[0].order.includes("container:gone"), "absent entries stay saved");
    await page.reload();
    await page.locator(".cards .cell").first().waitFor();
    assert.deepEqual(await order(page), keyed, "order kept after reload");

    // A–Z: stacks among containers by name, no handles, own order kept.
    await customize(page);
    await page.getByRole("radio", { name: "A–Z" }).click();
    assert.equal(await page.locator(".cards .handle").count(), 0);
    const az = ["app:adguard", "app:jellyfin", "app:mosquitto", "app:paperless", "app:vaultwarden"];
    assert.deepEqual(await order(page), az);
    await page.screenshot({ path: `${out}/b4-a-z-bearbeiten-graphit-1440.png`, fullPage: true });
    await page.getByRole("button", { name: "Layout speichern" }).click();
    await page.locator(".layout-editor").waitFor({ state: "detached" });
    assert.equal(data.putDocker.length, 2);
    assert.equal(data.putDocker[1].sort, "az");
    assert.deepEqual(data.putDocker[1].order, data.putDocker[0].order, "A–Z keeps the own order");
    await page.reload();
    await page.locator(".cards .cell").first().waitFor();
    assert.deepEqual(await order(page), az);
    // The list view follows the same sort.
    await page.getByRole("button", { name: "Liste" }).click();
    await page.locator(".list").first().waitFor();
    const listNames = await page.locator(".list > [role=listitem]").evaluateAll((items) => items.map((item) => item.getAttribute("aria-label")));
    assert.deepEqual(listNames, ["adguard", "jellyfin", "mosquitto", "Compose-Stack paperless", "vaultwarden"]);
    // Back to own order: the dragged order returns.
    await page.getByRole("button", { name: "Karten" }).click();
    await page.locator(".cards .cell").first().waitFor();
    await customize(page);
    await page.getByRole("radio", { name: "Eigene Anordnung" }).click();
    assert.deepEqual(await order(page), keyed);
    await page.getByRole("button", { name: "Abbrechen" }).click();
    assert.deepEqual(await order(page), az);
    await ctx.close();
  }

  // 4. One card size for all (S/M/L): more or fewer cards per row, still equal;
  //    saved as `size` only on a change; phones keep one column.
  {
    const columns = (page) =>
      page.locator(".cards > .cell").evaluateAll((cells) => new Set(cells.map((cell) => Math.round(cell.getBoundingClientRect().x))).size);
    const { ctx, page, data } = await open("graphit", 1440);
    const counts = {};
    await customize(page);
    for (const size of ["S", "M", "L"]) {
      await page.getByRole("radio", { name: new RegExp(`^${{ S: "Klein", M: "Mittel", L: "Groß" }[size]}`) }).click();
      await page.waitForTimeout(200);
      counts[size] = await columns(page);
      const heights = await cardHeights(page);
      assert.equal(new Set(heights).size, 1, `size ${size}: equal heights ${heights}`);
      assert.ok(await noScroll(page));
    }
    assert.ok(counts.S > counts.M && counts.M > counts.L, `columns ${JSON.stringify(counts)}`);
    // Back on M: nothing changed, nothing written.
    await page.getByRole("radio", { name: /^Mittel/ }).click();
    await page.getByRole("button", { name: "Layout speichern" }).click();
    await page.locator(".layout-editor").waitFor({ state: "detached" });
    assert.equal(data.putDocker.length, 0, "unchanged size must not write");
    for (const [size, label] of [["s", "Klein"], ["l", "Groß"]]) {
      await customize(page);
      await page.getByRole("radio", { name: new RegExp(`^${label}`) }).click();
      await page.getByRole("button", { name: "Layout speichern" }).click();
      await page.locator(".layout-editor").waitFor({ state: "detached" });
      assert.equal(data.putDocker.at(-1).size, size);
      await page.reload();
      await page.locator(".cards .cell").first().waitFor();
      assert.ok(await page.locator(`.cards.size-${size}`).count(), `size ${size} kept after reload`);
    }
    await ctx.close();
    for (const width of [1440, 390])
      for (const size of ["s", "m", "l"]) {
        const made = await open("graphit", width, { size });
        await made.page.getByRole("article", { name: "Compose-Stack paperless" }).getByRole("button", { name: "Dienste" }).click();
        await made.page.locator(".drawer").waitFor();
        await made.page.waitForTimeout(200);
        const heights = await cardHeights(made.page);
        assert.equal(new Set(heights).size, 1, `size ${size} ${width}: equal heights with open stack ${heights}`);
        if (width === 390) assert.equal(await columns(made.page), 1, `size ${size}: one column on the phone`);
        assert.ok(await noScroll(made.page));
        await made.page.screenshot({ path: `${out}/groesse-${size}-graphit-${width}.png`, fullPage: true });
        await made.ctx.close();
      }
  }

  // 5. Screenshots and the phone: one column, same behaviour.
  for (const theme of ["graphit", "hell"])
    for (const width of [1440, 390]) {
      const { ctx, page } = await open(theme, width);
      if (width === 390) {
        const boxes = await page.locator(".cards > .cell").evaluateAll((cells) => cells.map((cell) => {
          const r = cell.getBoundingClientRect();
          return [Math.round(r.x), Math.round(r.width)];
        }));
        assert.equal(new Set(boxes.map((b) => b.join())).size, 1, `one column ${JSON.stringify(boxes)}`);
      }
      assert.ok(await noScroll(page), "no horizontal scroll");
      await page.screenshot({ path: `${out}/karten-zu-${theme}-${width}.png`, fullPage: true });
      await page.getByRole("article", { name: "Compose-Stack paperless" }).getByRole("button", { name: "Dienste" }).click();
      await page.locator(".drawer").waitFor();
      assert.ok(await noScroll(page), "no horizontal scroll with open stack");
      await page.screenshot({ path: `${out}/karten-offen-${theme}-${width}.png`, fullPage: true });
      await customize(page);
      if (width === 390) {
        // Touch-sized handle, keyboard move works on the phone too.
        const first = (await order(page))[0];
        await page.locator(`.cards [data-group="${first}"] .handle`).focus();
        await page.keyboard.press("ArrowDown");
        assert.equal((await order(page))[1], first);
      }
      assert.ok(await noScroll(page), "no horizontal scroll while editing");
      await page.screenshot({ path: `${out}/karten-bearbeiten-${theme}-${width}.png`, fullPage: true });
      await ctx.close();
    }
  assert.deepEqual(errors, []);
  console.log("Docker cards: stack closed/open, equal heights, A–Z vs own order, drag and keyboard move saved, phone passed.");
} catch (error) {
  console.error("Browser errors:", errors);
  for (const context of browser.contexts())
    for (const page of context.pages())
      await page.screenshot({ path: out + "/failure.png", fullPage: true }).catch(() => {});
  throw error;
} finally {
  await browser.close();
}
