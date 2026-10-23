// All NAS endpoints are isolated fixtures. No production connection is used.
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
const base = process.env.UI_URL || "http://127.0.0.1:5186";
const out = process.env.UI_OUTPUT || "out/validation/ui-completion";
await mkdir(out, { recursive: true });
const errors = [];
const context = fixtureFactory(browser, errors);
const at = "2026-10-24T00:00:00Z";
try {
  const { ctx, page, data } = await context();
  await page.goto(base + "/tools/maintenance");
  await page
    .getByRole("button", { name: "Zeitplan hinzufügen", exact: true })
    .click();
  let dialog = page.getByRole("dialog");
  await dialog
    .getByRole("combobox", { name: "Daten-Volume", exact: true })
    .selectOption("Safe");
  await dialog.getByRole("button", { name: "Abbrechen", exact: true }).click();
  await page.getByRole("button", { name: "Änderungen verwerfen", exact: true }).click();
  assert.equal(
    await page
      .getByRole("combobox", { name: "Daten-Volume", exact: true })
      .inputValue(),
    "Data",
  );
  data.operations = {
    transfers: [
      {
        id: "paused",
        state: "running",
        from: "share/Documents",
        to: "volume/Safe",
        paused: true,
        bytes: 100,
        copied: 25,
        priority: 1,
        rate: 1024,
      },
    ],
    maintenance: {
      ...data.maintenance,
      error: "fixture history unavailable",
      history: [
        {
          id: "done",
          name: "Completed check",
          started: at,
          finished: at,
          error: "",
        },
      ],
    },
    docker: { failed: { state: "failed", output: "fixture image failure" } },
    vms: [
      {
        name: "failed-vm",
        job: { state: "failed", text: "fixture VM failure" },
      },
    ],
    assistant: [
      {
        id: "mine",
        title: "Own AI task",
        updated: at,
        run: { state: "confirmation", detail: "fixture approval" },
      },
    ],
    automations: [
      { id: "auto", name: "Own automation", enabled: true, next: at },
    ],
    uploads: [
      { id: "upload", name: "browser-file", size: 100, offset: 30, busy: true },
    ],
  };
  await page.goto(base + "/tools/tasks");
  await page
    .getByText("fixture image failure", { exact: true }).first()
    .waitFor({ state: "attached" });
  assert.equal(
    await page
      .getByText("Keine offenen Fehler in den geladenen Auftragszuständen.", {
        exact: true,
      })
      .count(),
    0,
  );
  await page
    .getByRole("button", { name: "Abgeschlossen", exact: true })
    .click();
  await page
    .getByRole("link", { name: "Completed check", exact: true })
    .waitFor();
  assert.equal(
    await page.getByRole("link", { name: "failed-vm", exact: true }).count(),
    1,
  ); // attention remains visible
  await page.getByRole("button", { name: "Aktiv", exact: true }).click();
  await page.getByRole("button", { name: "Fortsetzen", exact: true }).click();
  assert.ok(
    data.requests.some(
      ([method, path]) =>
        method === "POST" && path === "/files/jobs/paused/control",
    ),
  );
  data.recovery = {
    id: "0123456789abcdef01234567",
    volume: "Data",
    job: "late-recovery",
    members: [
      {
        kind: "docker",
        name: "original-app",
        running: true,
        phase: "stopped",
        error: "",
      },
    ],
  };
  await page.getByText(/late-recovery/).waitFor({ timeout: 22000 });
  await page
    .locator(".recovery")
    .getByRole("button", { name: "Ergebnis prüfen", exact: true })
    .click();
  dialog = page.getByRole("dialog");
  await dialog.getByText("original-app", { exact: true }).waitFor();
  await dialog.getByText(/Vorher laufend/).waitFor();
  await dialog.getByRole("button", { name: "Abbrechen", exact: true }).click();
  await page.goto(base + "/tools/monitoring");
  const download = page.waitForEvent("download");
  await page
    .getByRole("button", { name: "CSV exportieren", exact: true })
    .click();
  assert.equal(
    (await download).suggestedFilename(),
    "titanos-monitoring-24h.csv",
  );
  assert.equal(data.exports, 1);
  data.coverage = {
    items: [
      {
        kind: "share",
        name: "Photos",
        available: true,
        status: "stale",
        lastOK: at,
        jobs: [
          {
            id: "app-backup",
            automatic: false,
            partial: true,
            method: "files",
            destination: "local",
            destinationAvailable: false,
            state: { lastOK: at },
          },
        ],
      },
    ],
    unavailable: [],
  };
  await page.goto(base + "/settings/backups/coverage");
  await page.getByText("Nur manuell", { exact: false }).waitFor();
  await page.getByText(/Teilauswahl oder Ausschlüsse/).waitFor();
  await page
    .getByText("Ziel-Volume nicht eingebunden.", { exact: true })
    .waitFor();
  data.backups = [
    {
      job: {
        id: "app-backup",
        applications: [{ kind: "docker", name: "app", method: "stop" }],
        destination: { type: "local" },
      },
      state: { running: false, phase: "" },
    },
  ];
  await page.goto(base + "/settings/backups/applications");
  await page
    .getByRole("combobox", { name: "Anwendungssicherung", exact: true })
    .selectOption("app-backup");
  await page
    .getByRole("combobox", { name: "Sicherungsstand", exact: true })
    .selectOption("abcdef01");
  await page
    .getByRole("combobox", {
      name: "Ziel-Volume für die neue Kopie",
      exact: true,
    })
    .selectOption("Safe");
  await page
    .getByRole("button", { name: "Lesend prüfen", exact: true })
    .click();
  await page.getByText("/mnt/Data/appdata/app", { exact: true }).waitFor();
  const before = data.requests.length;
  await page
    .getByRole("button", {
      name: "Geprüfte Kopie wiederherstellen",
      exact: true,
    })
    .click();
  await page
    .getByRole("heading", {
      name: "Kopie wiederhergestellt und geprüft",
      exact: true,
    })
    .waitFor();
  assert.ok(
    !data.requests
      .slice(before)
      .some(
        ([method, path]) =>
          method !== "GET" && !path.endsWith("/applications/restore"),
      ),
  );
  await page.screenshot({
    path: out + "/restore-verified.png",
    fullPage: true,
  });
  await ctx.close();
  for (const theme of ["graphit", "hell"])
    for (const width of [1440, 390]) {
      const test = await context("owner", theme, width);
      for (const route of [
        "/tools/tasks",
        "/settings/backups/coverage",
        "/settings/backups/applications",
      ]) {
        await test.page.goto(base + route);
        await test.page.getByRole("heading", { level: 1 }).waitFor();
        await test.page.waitForTimeout(150);
        assert.ok(
          await test.page.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth + 1,
          ),
          route + " overflow",
        );
        await test.page.screenshot({
          path:
            out +
            "/" +
            route.replaceAll("/", "-") +
            "-" +
            theme +
            "-" +
            width +
            ".png",
          fullPage: true,
        });
      }
      await test.ctx.close();
    }
  assert.deepEqual(errors, []);
  console.log(
    "Completion UI passed: cancelled draft, task errors/filters/controls, delayed recovery, export, coverage, independent restore, both themes and widths.",
  );
} catch (error) {
  for (const ctx of browser.contexts())
    for (const page of ctx.pages())
      await page
        .screenshot({ path: out + "/failure.png", fullPage: true })
        .catch(() => {});
  throw error;
} finally {
  await browser.close();
}
