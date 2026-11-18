// 1.11 (step 16): Kontakte in the browser. Since 1.13 the page stands on
// the shared column frame (ColumnFrame: no card `.con`, no PageHead
// buttons; the form has its own Abbrechen/Speichern) and this test is
// test-ui-kontakte-113.mjs – kept under this name for the scripts and
// documents that call it.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5213 node scripts/test-ui-kontakte.mjs
await import("./test-ui-kontakte-113.mjs");
