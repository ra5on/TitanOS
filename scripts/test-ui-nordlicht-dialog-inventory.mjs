// Emit modal declarations independently of the data and permissions of the demo
// account. Browser captures are indexed separately by test-ui-crawl.mjs.
import { createRequire } from "node:module";
import { readdir, readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { join } from "node:path";
const require = createRequire(new URL("../web/package.json", import.meta.url));
const { parse } = require("svelte/compiler");
const root = fileURLToPath(new URL("../web/src/", import.meta.url));
const files = await readdir(root, { recursive: true });
const dictionary = {};
for (const file of files.filter(
  (f) => f === "lib/i18n/de.json" || f.endsWith(".de.json"),
)) {
  Object.assign(
    dictionary,
    JSON.parse(await readFile(join(root, file), "utf8")),
  );
}
const entries = [];
function visit(node, accept) {
  if (!node || typeof node !== "object") return;
  if (Array.isArray(node)) {
    for (const child of node) visit(child, accept);
    return;
  }
  accept(node);
  for (const [key, child] of Object.entries(node)) {
    if (!["loc", "metadata", "name_loc"].includes(key)) visit(child, accept);
  }
}
for (const file of files.filter((f) => f.endsWith(".svelte")).sort()) {
  const content = await readFile(join(root, file), "utf8");
  const { fragment } = parse(content, { modern: true });
  visit(fragment, (node) => {
    const attribute = (name) =>
      node.attributes?.find((a) => a.name === name)?.value;
    const text = (value) =>
      Array.isArray(value)
        ? value
            .map((n) => (n.type === "Text" ? n.data : ""))
            .join("")
            .trim()
        : "";
    const role = text(attribute("role"));
    if (
      !(node.type === "Component" && node.name === "Dialog") &&
      !(
        node.type === "RegularElement" &&
        (node.name === "dialog" || ["dialog", "alertdialog"].includes(role))
      )
    )
      return;
    const value = attribute("title") || attribute("aria-label");
    const titles = new Set();
    visit(value, (part) => {
      if (part.type === "CallExpression" && part.callee?.name === "t") {
        visit(part.arguments[0], (key) => {
          if (key.type === "Literal" && dictionary[key.value])
            titles.add(dictionary[key.value]);
        });
      }
    });
    const labels = text(value) || [...titles].join(" / ");
    entries.push({
      file: "web/src/" + file,
      line: content.slice(0, node.start).split("\n").length,
      element: node.name,
      title: labels || null,
      binding:
        labels || !value ? null : content.slice(value.start, value.end).trim(),
    });
  });
}
process.stdout.write(JSON.stringify(entries, null, 2) + "\n");
