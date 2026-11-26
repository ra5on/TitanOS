// The mobile file manager and its per-folder settings, with isolated API fixtures.
import assert from "node:assert/strict";
import { mkdir, readFile } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";

const playwright = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const base = process.env.UI_URL || "http://127.0.0.1:5276";
assert.ok(["localhost", "127.0.0.1", "[::1]"].includes(new URL(base).hostname));
const browser = await playwright[process.env.BROWSER_ENGINE || "chromium"].launch({
  headless: true, executablePath: process.env.BROWSER_EXECUTABLE,
});
const errors = [];
const sample = await readFile(new URL("../web/static/backgrounds/nordlicht.webp",import.meta.url));
let testPage;
try {
  const { ctx, page, data } = await fixtureFactory(browser, errors)("owner", "graphit", 390, "de", {
    serviceWorkers: "block", isMobile: true, hasTouch: true, viewport: { width: 390, height: 844 },
  });
  testPage = page;
  page.setDefaultTimeout(12000);
  await ctx.addInitScript(() => {
    const original = Storage.prototype.setItem;
    Storage.prototype.setItem = function(key,value) { if(key !== "titanos.layout") original.call(this,key,value); };
    localStorage.removeItem("titanos.layout");
  });
  const root = "share/Daten";
  const filesURL = (path) => base + "/files?" + new URLSearchParams({ root, path });
  const dir = (name, folder = true) => ({ name, dir: folder, size: folder ? 0 : 1234, modified: "2026-11-25T12:00:00Z" });
  data.prefs.desktop = {version:1,wallpaper:{source:"builtin",id:"nordlicht",live:false}};
  data.prefs.fileviews = { default: "list", folders: [] };
  let edits = 0;
  await ctx.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    switch (url.pathname.slice(7)) {
      case "/files/roots": return route.fulfill({json:[{id:root,name:"Daten",kind:"share",writable:true,recycle:true,totalBytes:10000,freeBytes:5000}]});
      case "/files/list": if(url.searchParams.get("path")==="Sammlung") return route.fulfill({json:Array.from({length:501},(_,i)=>dir("Album "+i))});
        return route.fulfill({json:url.searchParams.get("path") ? [dir("Archiv"),dir("Nordlicht.jpg",false),dir("Reiseplan.pdf",false)] : [dir("Dokumente"),dir("Medien")]});
      case "/files/thumb": if(url.searchParams.get("path")?.endsWith(".pdf")) return route.fulfill({status:404});
        return route.fulfill({contentType:"image/webp",body:sample});
      case "/preferences/fileviews": {
        const {folder,view}=route.request().postDataJSON();
        const prefs = data.prefs.fileviews;
        if(!folder) prefs.default=view;
        else {
          prefs.folders=prefs.folders.filter((f)=> f.root!==folder.root || f.path!==folder.path || f.node!==folder.node);
          if(view!=="inherit") prefs.folders.push({...folder,view});
        }
        edits++;
        return route.fulfill({json:prefs});
      }
      default: return route.fallback();
    }
  });
  const expectLayout = (id) => page.locator(`.mobile-entries[data-layout="${id}"]`).waitFor();
  const choose = async (label) => {
    await page.getByRole("button",{name:"Ordneransicht wählen"}).click();
    await page.getByRole("menuitemradio",{name:label,exact:true}).click();
  };
  await page.goto(filesURL("Dokumente"));
  await expectLayout("list");
  await page.getByRole("button",{name:"Reiseplan.pdf",exact:true}).waitFor();
  assert.equal(edits,0);
  await page.goto(filesURL("Medien"));
  for (const [label,id] of [["Details","details"],["Ordnerleiste","rail"],["Vorschaukarten","cards"],["Symbolraster","grid"]]) {
    await choose(label); await expectLayout(id);
    assert.equal(await page.getByRole("button",{name:"Reiseplan.pdf",exact:true}).count(),1);
  }
  await page.locator('.mobile-entries img[src*="thumb"]').first().waitFor();
  await page.waitForFunction(() => document.querySelector('.mobile-entries img')?.naturalWidth > 0);
  await page.reload(); await expectLayout("grid");
  await page.getByRole("button",{name:"Ordneransicht wählen"}).click();
  await page.getByRole("menuitem",{name:"Dateien-Einstellungen",exact:true}).click();
  const settings = page.locator("#folder-views");
  await settings.getByText("Medien",{exact:true}).waitFor();
  await settings.getByRole("combobox").selectOption("cards");
  await page.waitForFunction(()=>document.querySelector("#folder-views select")?.disabled === false);
  if(process.env.UI_OUTPUT) {
    await mkdir(process.env.UI_OUTPUT,{recursive:true});
    await page.screenshot({path:process.env.UI_OUTPUT+"/dateien-ordneransichten.png"});
  }
  await settings.getByRole("button",{name:"Ansicht von Freigaben / Daten / Medien zurücksetzen"}).click();
  await settings.getByRole("status").getByText(/Medien.*Vorschaukarten/).waitFor();
  await settings.getByText("Alle Ordner verwenden die Standardansicht.").waitFor();
  await page.goto(filesURL("Medien")); await expectLayout("cards");
  await choose("Liste"); await expectLayout("list");
  await choose("Standardansicht verwenden"); await expectLayout("cards");
  await page.goto(filesURL("Dokumente")); await expectLayout("cards");
  if(process.env.UI_OUTPUT) await page.screenshot({path:process.env.UI_OUTPUT+"/dateien-vorschaukarten.png"});
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth <= innerWidth),true);
  await page.goto(filesURL("Sammlung"));
  await choose("Ordnerleiste");
  const strip = page.locator(".mobile-entries .horizontal");
  await strip.waitFor();
  assert.ok(await strip.getByRole("button").count()<60);
  await strip.evaluate(element=>element.scrollLeft=element.scrollWidth);
  await strip.getByRole("button",{name:"Album 500",exact:true}).waitFor();
  assert.ok(await strip.getByRole("button").count()<60);
  assert.equal(edits,9);
  assert.deepEqual(errors,[]);
  console.log("Phone Files: five layouts, actual thumbnail requests, reload, default, both folder resets and settings feedback passed.");
} catch (error) {
  console.error(await testPage?.locator("body").innerText(), errors);
  throw error;
} finally {
  await browser.close();
}
