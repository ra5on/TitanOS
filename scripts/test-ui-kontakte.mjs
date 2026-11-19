// 1.11 (step 16): Kontakte in the browser. Since 1.13 the page stands on
// the shared column frame (ColumnFrame) and the test of the page is
// test-ui-kontakte-113.mjs; 1.15 (PLAN 115-6/115-7: Apple's Kontakte, the
// window sizes, one "#", the list after coming back) is
// test-ui-kontakte-115.mjs. This name stays for the scripts and documents
// that call it and runs both, one after the other.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5213 node scripts/test-ui-kontakte.mjs
await import("./test-ui-kontakte-113.mjs");
await import("./test-ui-kontakte-115.mjs");
