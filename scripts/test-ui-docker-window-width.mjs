// Measures the actual container-list viewport, including the area beside its detail pane.
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const url = new URL(process.env.UI_URL);
assert.ok(["127.0.0.1", "localhost", "[::1]"].includes(url.hostname));
const engine = process.env.UI_ENGINE || "chromium";
const pw = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const browser = await pw[engine].launch();
const errors = [];
const baseline = process.env.UI_BASELINE === "1";
const out = process.env.UI_OUTPUT;
if (out) await mkdir(out, { recursive: true });
try {
  const width = 1440;
  const { ctx, page, data } = await fixtureFactory(browser, errors)(
    "owner",
    "graphit",
    width,
    "de",
    {
      nativeShell: true,
      viewport: { width, height: 950 },
      serviceWorkers: "block",
      reducedMotion: "reduce",
    },
  );
  const stack = "medienarchiv-filme-und-familienaufnahmen";
  const services = ["vorschau-worker", "datenbank", "medienimport"];
  data.dockerApps = [stack];
  data.dockerContainers = services.map((svc, i) => ({
    id: svc,
    name: stack + "-" + svc + "-1",
    image:
      "registry.example.test/projekte/medienverwaltung-und-archiv/" +
      svc +
      ":2026.11.18-stabil",
    state: i ? "running" : "exited",
    since: new Date().toISOString(),
    ports: [8081, 8092, 9201, 9202].map((host) => ({
      host,
      container: 80,
      protocol: "tcp",
    })),
    app: stack,
    service: svc,
    managed: true,
    cpuPercent: 2.6,
    memoryBytes: 128e6,
    cpuHistory: [],
    webui: "8081",
    publicUrl: "",
    icon: "",
    updateAvailable: true,
  }));
  await page.goto(url.origin + "/docker");
  const iframe = page.locator('iframe[data-app="app:docker"]').last();
  const frame = iframe.contentFrame();
  const table = frame.locator(".app-table");
  await table.waitFor({ timeout: 30000 });
  const win = iframe.locator(
    'xpath=ancestor::*[contains(concat(" ",normalize-space(@class)," ")," win ")][1]',
  );
  const current = await iframe.evaluate((el) => ({
    ancestors: [
      el.parentElement?.className,
      el.parentElement?.parentElement?.className,
      el.parentElement?.parentElement?.parentElement?.className,
    ],
    width: el.clientWidth,
  }));
  console.log(engine, "initial frame", current);
  async function size(w) {
    // Resize the actual managed window (or its host if running under another desktop layout).
    if (await win.count())
      await win.evaluate((n, width) => {
        n.style.width = width + "px";
        n.style.maxWidth = width + "px";
      }, w);
    else
      await iframe.evaluate((n, width) => {
        n.style.width = width + "px";
        n.style.maxWidth = width + "px";
      }, w);
    await page.waitForTimeout(180);
  }
  async function measure(scene) {
    const values = await frame
      .locator(".table-wrap")
      .first()
      .evaluate((el) => ({
        list: el.clientWidth,
        scroll: el.scrollWidth,
        document: document.documentElement.clientWidth,
        docScroll: document.documentElement.scrollWidth,
        pane: !!document.querySelector(".explorer.with-pane"),
        filters: [...document.querySelectorAll(".segmented,.filters")].map(
          (f) => [f.clientWidth, f.scrollWidth],
        ),
        buttons: [
          ...el.querySelectorAll("tbody tr:first-child .actions button"),
        ].filter((b) => b.getBoundingClientRect().width > 0).length,
      }));
    console.log(engine, scene, JSON.stringify(values));
    if (out)
      await page.screenshot({
        path: out + "/" + engine + "-" + scene + ".png",
        animations: "disabled",
      });
    if (!baseline) {
      assert.ok(
        values.scroll <= values.list + 1,
        scene + " list:" + JSON.stringify(values),
      );
      assert.ok(values.docScroll <= values.document + 1, scene + " document");
      assert.ok(values.buttons > 0, scene + " actions");
      for (const [c, s] of values.filters)
        assert.ok(s <= c + 1, scene + " filters");
    }
    return values;
  }
  await measure("normal");
  await size(1360);
  await measure("wide");
  await table
    .locator("tbody tr")
    .first()
    .locator("td.state")
    .click({ position: { x: 2, y: 2 } });
  await page.waitForTimeout(170);
  await measure("detail");
  const twist = table.locator(".twisty").first();
  if (await twist.count()) await twist.click();
  await measure("stack");
  await size(900);
  await measure("900");
  await size(750);
  await measure("750");
  await size(550);
  await measure("550");
  if (!baseline) {
    await win.locator("button.fold").click();
    await measure("sidebar-folded");
    await win.locator("button.fold").click();
    await win.evaluate((el) => el.style.removeProperty("max-width"));
    await win.getByRole("button", { name: "Maximieren", exact: true }).click();
    await win
      .getByRole("button", { name: "Verkleinern", exact: true })
      .waitFor();
    const full = await measure("maximized");
    assert.ok(
      full.document > 1000,
      "the managed window must really be maximized",
    );
    await frame.locator("html").evaluate((el) => {
      el.style.fontSize = "125%";
      el.style.zoom = "1.25";
    });
    await measure("zoom-125");
    const repo = "registry.example.test/archiv/ablage/" + stack;
    const resourceData = {
      images: [
        {
          id: "sha256:abc",
          repoTags: [repo + ":release"],
          size: 500000,
          created: 1710000000,
          usedBy: [{ app: stack }],
          protected: "",
        },
      ],
      volumes: [
        {
          name: stack + "-laufwerk-mit-fortlaufenden-aufnahmen",
          driver: "local",
          size: 4006000,
          usedBy: [{ app: stack }],
        },
      ],
      networks: [
        {
          name: stack + "-internes-netz",
          driver: "bridge",
          subnets: ["172.19.0.0/24", "2001:db8:ffff::/64"],
          usedBy: [{ app: stack }],
        },
      ],
    };
    for (const [area, values] of Object.entries(resourceData)) {
      await ctx.route(
        "**/api/v1/docker/" + (area === "networks" ? "networks/all" : area),
        (r) => r.fulfill({ json: { [area]: values } }),
      );
      await page.goto(new URL("/docker/" + area, url).href, {
        waitUntil: "networkidle",
      });
      const resource = frame.locator(".table-wrap").first();
      await resource.locator("tbody tr").first().waitFor();
      await size(590);
      const box = await resource.evaluate((el) => [
        el.clientWidth,
        el.scrollWidth,
        document.documentElement.clientWidth,
        document.documentElement.scrollWidth,
      ]);
      console.log(engine, area, box);
      assert.ok(
        box[1] <= box[0] + 1 && box[3] <= box[2] + 1,
        area + " overflows",
      );
    }
    const phone = await fixtureFactory(browser, errors)(
      "owner",
      "graphit",
      390,
      "de",
      {
        nativeShell: true,
        isMobile: true,
        hasTouch: true,
        viewport: { width: 390, height: 844 },
        serviceWorkers: "block",
      },
    );
    phone.data.dockerContainers = data.dockerContainers;
    phone.data.dockerApps = data.dockerApps;
    await phone.page.goto(new URL("/docker", url).href, {
      waitUntil: "networkidle",
    });
    const row = phone.page.getByText(stack, { exact: true }).first();
    await row.waitFor();
    await row.click();
    const service = phone.page.locator("details.service").first();
    await service.locator("summary").click();
    const phoneWidth = await service.evaluate((el) => [
      document.documentElement.clientWidth,
      document.documentElement.scrollWidth,
      el.clientWidth,
      el.scrollWidth,
    ]);
    assert.ok(
      phoneWidth[1] <= phoneWidth[0] + 1 && phoneWidth[3] <= phoneWidth[2] + 1,
      "phone details " + phoneWidth,
    );
    if (out)
      await phone.page.screenshot({
        path: out + "/" + engine + "-phone-stack.png",
      });
    await phone.ctx.close();
  }
  await ctx.close();
  assert.deepEqual(errors, []);
} finally {
  await browser.close();
}
