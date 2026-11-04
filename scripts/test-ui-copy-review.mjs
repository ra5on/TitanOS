// Isolated API data only. No copying to a real NAS or host path.
// Copy dialog and jobs panel after the copy review of 4.11.2026: the
// "missing or changed" rule chosen for identical files, the notice instead
// of a silent first click, rules for all after an inspection timeout, the
// error list of a job, "copy missing files" after a restart, and the recycle
// bin button on phones and in the volume view.
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
const base = process.env.UI_URL || "http://127.0.0.1:5323";
const out = process.env.UI_OUTPUT || "out/validation/kopieren";
await mkdir(out, { recursive: true });
const errors = [],
  create = fixtureFactory(browser, errors);

const file = {
  name: "report.txt",
  dir: false,
  size: 12,
  modified: "2026-10-25T00:00:00Z",
};
const folder = {
  name: "Fotos",
  dir: true,
  size: 0,
  modified: "2026-10-25T00:00:00Z",
};
const conflict = (over = {}) => ({
  identical: false,
  path: file.name,
  sourcePath: file.name,
  sourceVersion: "source-v1",
  targetVersion: "target-v1",
  source: file,
  target: { ...file, size: 20 },
  ...over,
});
const inspection = (over = {}) => ({
  files: 1,
  bytes: 12,
  truncated: false,
  conflicts: [],
  identical: 0,
  identicalBytes: 0,
  different: 0,
  differentBytes: 0,
  typeMismatch: 0,
  unreadable: [],
  unreadableCount: 0,
  ...over,
});
const roots = [
  {
    id: "share/Documents",
    name: "Documents",
    kind: "share",
    writable: true,
    recycle: true,
    totalBytes: 1e12,
    freeBytes: 6e11,
  },
  {
    id: "share/Projects",
    name: "Projects",
    kind: "share",
    writable: true,
    recycle: false,
    totalBytes: 1e12,
    freeBytes: 6e11,
  },
  {
    id: "volume/Data",
    name: "Data",
    kind: "volume",
    writable: true,
    recycle: false,
    bins: ["Documents", "Media"],
    totalBytes: 1e12,
    freeBytes: 6e11,
  },
  {
    id: "usb/stick",
    name: "USB-Stick",
    kind: "usb",
    writable: true,
    recycle: false,
    totalBytes: 64e9,
    freeBytes: 30e9,
  },
];

// Routes for the file API; inspections of a target wait for the test.
function fileRoutes(ctx, state) {
  return ctx.route("**/api/v1/files/**", async (route) => {
    const url = new URL(route.request().url()),
      path = url.pathname;
    if (path.endsWith("/roots")) return route.fulfill({ json: roots });
    if (path.endsWith("/list"))
      return route.fulfill({
        json: url.searchParams.get("path") ? [] : [folder, file],
      });
    if (path.endsWith("/jobs")) return route.fulfill({ json: state.jobs });
    if (path.endsWith("/trash"))
      return route.fulfill({
        json: [{ ...file, path: "alex/report.txt", original: "report.txt" }],
      });
    if (path.endsWith("/inspect")) {
      const request = route.request().postDataJSON();
      if (!request.toRoot)
        return route.fulfill({ json: inspection() });
      await new Promise((resolve) =>
        state.pending.push({
          request,
          respond: async (json, status = 200) => {
            try {
              await route.fulfill({ status, json });
            } catch {
            } finally {
              resolve();
            }
          },
        }),
      );
      return;
    }
    if (path.endsWith("/copy")) {
      state.copies.push(route.request().postDataJSON());
      return route.fulfill({ status: 202, json: { id: "fixture-copy" } });
    }
    return route.fallback();
  });
}
// The next inspection of the folder the tests copy into ("Fotos"); checks
// of the folder the dialog opened on are answered and dropped.
async function next(state) {
  for (let i = 0; i < 400; i++) {
    const item = state.pending.shift();
    if (item?.request.toDir === "Fotos") return item;
    if (item) await item.respond(inspection());
    else await new Promise((r) => setTimeout(r, 25));
  }
  throw Error("no automatic target inspection");
}
async function openCopy(page) {
  await page.goto(base + "/files?root=share%2FDocuments");
  await page
    .getByRole("checkbox", { name: "report.txt auswählen", exact: true })
    .check();
  await page
    .getByRole("button", { name: "Kopieren nach …", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await dialog
    .getByLabel("Gibt es den Namen schon", { exact: true })
    .waitFor();
  // Into a subfolder: copying into the folder the file is in only works
  // with "keep both".
  await dialog.getByRole("button", { name: "Fotos", exact: true }).click();
  return dialog;
}

try {
  // 1. Identical files: "only missing or changed" is chosen and explained.
  {
    const { ctx, page } = await create();
    const state = { pending: [], copies: [], jobs: [] };
    await fileRoutes(ctx, state);
    const dialog = await openCopy(page);
    const rule = dialog.getByLabel("Gibt es den Namen schon", { exact: true });
    const copy = dialog.getByRole("button", {
      name: "Hierher kopieren",
      exact: true,
    });
    const first = await next(state);
    assert.equal(first.request.toRoot, "share/Documents");
    assert.equal(first.request.toDir, "Fotos");
    await first.respond(
      inspection({
        conflicts: [conflict({ identical: true, target: file })],
        identical: 1,
        identicalBytes: 12,
      }),
    );
    await dialog
      .getByText("1 Dateien (12 B) sind am Ziel schon gleich vorhanden", {
        exact: false,
      })
      .waitFor();
    assert.equal(await rule.inputValue(), "missing");
    // No changed files: nothing is replaced, so no confirmation is needed.
    assert.equal(await copy.isEnabled(), true);
    await page.screenshot({ path: out + "/dialog-fehlende-dateien.png" });
    await copy.click();
    await dialog.waitFor({ state: "hidden" });
    assert.equal(state.copies.length, 1);
    assert.equal(state.copies[0].conflict, "missing");
    await ctx.close();
  }

  // 2. The first click no longer returns silently: when the check finds
  // names that exist, the dialog says so and shows them.
  {
    const { ctx, page } = await create();
    const state = { pending: [], copies: [], jobs: [] };
    await fileRoutes(ctx, state);
    const dialog = await openCopy(page);
    const copy = dialog.getByRole("button", {
      name: "Hierher kopieren",
      exact: true,
    });
    await (
      await next(state)
    ).respond({ error: { code: "internal", message: "fixture" } }, 500);
    await dialog.getByRole("alert").first().waitFor();
    assert.equal(await copy.isEnabled(), true, "keep both blocked by a failed check");
    await copy.click();
    await (
      await next(state)
    ).respond(inspection({ conflicts: [conflict()], different: 1, differentBytes: 12 }));
    await dialog
      .getByText("Am Ziel gibt es 1 gleiche Namen", { exact: false })
      .waitFor();
    assert.equal(state.copies.length, 0, "started without showing the conflicts");
    await dialog.locator("td", { hasText: "report.txt" }).waitFor();
    await page.screenshot({ path: out + "/dialog-konflikte-hinweis.png" });
    await copy.click();
    await dialog.waitFor({ state: "hidden" });
    assert.equal(state.copies.length, 1);
    assert.equal(state.copies[0].conflict, "rename");
    await ctx.close();
  }

  // 3. A check that takes too long: a rule for all can start; deciding one
  // by one says why it is not available.
  {
    const { ctx, page } = await create();
    const state = { pending: [], copies: [], jobs: [] };
    await fileRoutes(ctx, state);
    const dialog = await openCopy(page);
    await (
      await next(state)
    ).respond({ error: { code: "file_scan_timeout", message: "timeout" } }, 408);
    await dialog
      .getByText("Die Prüfung des Ziels hat zu lange gedauert. Eine Regel", {
        exact: false,
      })
      .waitFor();
    const rule = dialog.getByLabel("Gibt es den Namen schon", { exact: true });
    assert.equal(
      await rule.locator('option[value="individual"]').evaluate((o) => o.disabled),
      true,
    );
    await dialog
      .getByText("Einzeln entscheiden ist nicht möglich", { exact: false })
      .waitFor();
    await rule.selectOption("replace");
    const confirm = dialog.getByRole("checkbox", {
      name: "Ich bestätige das Ersetzen vorhandener Dateien.",
      exact: true,
    });
    // Copying into Documents: replaced files go to its recycle bin.
    await dialog
      .getByText("Ersetzte Dateien kommen in den Papierkorb", { exact: false })
      .waitFor();
    assert.equal(await confirm.isEnabled(), true, "timeout blocked replacing");
    const copy = dialog.getByRole("button", {
      name: "Hierher kopieren",
      exact: true,
    });
    assert.equal(await copy.isDisabled(), true);
    await confirm.check();
    await page.screenshot({ path: out + "/dialog-zeitlimit-ersetzen.png" });
    await copy.click();
    await dialog.waitFor({ state: "hidden" });
    assert.equal(state.copies[0].conflict, "replace");
    await ctx.close();
  }

  // 4. Jobs panel: the error list of a job finished with errors, and
  // "copy missing files" after a restart of the NAS.
  {
    const { ctx, page } = await create();
    const job = {
      id: "j1",
      move: true,
      state: "done",
      from: "share/Documents",
      to: "usb/stick",
      dir: "Sicherung",
      names: ["Fotos"],
      files: 5,
      done: 3,
      bytes: 5e9,
      copied: 3e9,
      current: "",
      error: "",
      failed: 2,
      kept: 1,
      identical: 0,
      retryable: true,
      problems: [
        { path: "Fotos/film.mkv", code: "too_large" },
        { path: "Fotos/a:b.jpg", code: "name_not_allowed" },
        { path: "Fotos/link", code: "kept_link" },
      ],
    };
    const rebooted = {
      ...job,
      id: "j2",
      move: false,
      state: "failed",
      to: "share/Projects",
      dir: "",
      error: "Transfer interrupted; recheck current access and resume incomplete files.",
      errorCode: "interrupted",
      failed: 0,
      kept: 0,
      problems: [],
      retryable: false,
      rebooted: true,
      durable: true,
    };
    const state = { pending: [], copies: [], jobs: [job, rebooted] };
    await fileRoutes(ctx, state);
    await page.goto(base + "/files?root=share%2FDocuments");
    const panel = page.getByRole("region", { name: "Dateiaufträge", exact: true });
    await panel.getByText("Mit Fehlern abgeschlossen", { exact: true }).waitFor();
    await panel
      .getByText("Zu groß für das Ziel (FAT32", { exact: false })
      .waitFor();
    await panel
      .getByText("Name auf diesem Datenträger nicht erlaubt", { exact: false })
      .waitFor();
    await panel
      .getByText("Verknüpfung (Symlink) – wird nicht übertragen", {
        exact: false,
      })
      .waitFor();
    await panel
      .getByRole("button", { name: "Fehlgeschlagene erneut versuchen", exact: true })
      .waitFor();
    await panel
      .getByText("Der Auftrag wurde unterbrochen (Neustart des Dienstes).", {
        exact: true,
      })
      .waitFor();
    assert.equal(
      await panel.getByRole("button", { name: "Offene Dateien erneut versuchen" }).count(),
      0,
      "retry offered after a restart",
    );
    await panel.screenshot({ path: out + "/auftraege-fehlerliste.png" });
    await panel
      .getByRole("button", { name: "Fehlende Dateien kopieren", exact: true })
      .click();
    for (let i = 0; i < 100 && !state.copies.length; i++)
      await new Promise((r) => setTimeout(r, 25));
    assert.deepEqual(state.copies[0], {
      root: "share/Documents",
      paths: ["Fotos"],
      toRoot: "share/Projects",
      toDir: "",
      move: false,
      conflict: "missing",
    });
    await ctx.close();
  }

  // 5. Recycle bin: a button of its own on phones; in the volume view the
  // bins of the shares on it.
  {
    const { ctx, page } = await create("owner", "graphit", 390);
    const state = { pending: [], copies: [], jobs: [] };
    await fileRoutes(ctx, state);
    await page.goto(base + "/files?root=share%2FDocuments");
    const bar = page.getByRole("toolbar", { name: "Weitere Aktionen" });
    const trash = bar.getByRole("button", { name: "Papierkorb", exact: true });
    await trash.waitFor();
    assert.equal(await trash.isVisible(), true);
    await page.screenshot({ path: out + "/handy-papierkorb-knopf.png" });
    await page.goto(base + "/files?root=usb%2Fstick");
    await bar.waitFor();
    assert.equal(
      await bar.getByRole("button", { name: "Papierkorb", exact: true }).count(),
      0,
      "USB drive shows a recycle bin",
    );
    await ctx.close();
  }
  {
    const { ctx, page } = await create();
    const state = { pending: [], copies: [], jobs: [] };
    await fileRoutes(ctx, state);
    await page.goto(base + "/files?root=volume%2FData");
    await page.getByRole("button", { name: "Papierkorb", exact: true }).first().click();
    await page
      .getByRole("heading", { name: "Papierkörbe der Freigaben auf diesem Volume" })
      .waitFor();
    await page.screenshot({ path: out + "/volume-papierkoerbe.png" });
    await page.getByRole("button", { name: "Data/Media" }).click();
    await page.getByText("report.txt", { exact: false }).first().waitFor();
    await ctx.close();
  }

  assert.deepEqual(errors, []);
  console.log(
    "Missing-or-changed preselection, conflict notice instead of a silent click, rules after an inspection timeout, job error list, copy missing after a restart and recycle bin buttons passed",
  );
} finally {
  await browser.close();
}
