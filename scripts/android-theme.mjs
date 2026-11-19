#!/usr/bin/env node
// PLAN 116: TitanOS-Stil für die native Android-App ohne Doppelpflege.
//
// Liest die Theme-Variablen aus web/src/app.css (Graphit als Vorgabe, die
// Hinweisfarben für hell und dunkel, --icon-square-*, Radien, Abstände,
// Schriftgrößen), die Paletten Graphit/Hell und die Akzentfarbe aus
// web/src/lib/theme/themes.ts sowie GLYPHS/GRADIENTS aus
// web/src/lib/desktop/DesktopIcon.svelte und erzeugt daraus Kotlin-Dateien
// (Farben, Maße, Programmsymbole als ImageVector) für das Repo
// TitanOS-Android. Dazu kommt die Schrift des Webs (--font-sans/--font-mono,
// IBM Plex, OFL-1.1) aus @fontsource als TTF-Ressource.
//
//   node scripts/android-theme.mjs ../TitanOS-Android
//
// Node 24, keine Abhängigkeiten. Schreibt nur unterhalb des Zielordners.
import { readFileSync, writeFileSync, mkdirSync, existsSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { inflateSync } from "node:zlib";

const repo = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const target = process.argv[2] ? resolve(process.argv[2]) : "";
if (!target || !existsSync(join(target, "settings.gradle.kts"))) {
  console.error(
    "Aufruf: node scripts/android-theme.mjs <Pfad zu TitanOS-Android>",
  );
  process.exit(2);
}
const read = (p) => readFileSync(join(repo, p), "utf8");
const KT_PKG = "io.github.ra5on.titanos.ui.generated";
const KT_DIR = join(target, "core-ui/src/main/kotlin", ...KT_PKG.split("."));
const FONT_DIR = join(target, "core-ui/src/main/res/font");
const HEADER = `// Erzeugt von TitanOS/scripts/android-theme.mjs – nicht von Hand ändern.
// Quellen: web/src/app.css, web/src/lib/theme/themes.ts, web/src/lib/desktop/DesktopIcon.svelte.
`;

// ---------- app.css ----------
const css = read("web/src/app.css");
/** Variablen des ersten Blocks mit diesem Selektor (Kommentare entfernt). */
function block(selector) {
  const start = css.indexOf(selector + " {");
  if (start < 0) throw new Error(`app.css: ${selector} fehlt`);
  const end = css.indexOf("\n}", start);
  const body = css.slice(start, end).replace(/\/\*[\s\S]*?\*\//g, "");
  const vars = {};
  for (const m of body.matchAll(/--([a-z0-9-]+):\s*([^;]+);/g))
    vars[m[1]] = m[2].trim();
  return vars;
}
const root = block(":root");
const darkMode = block("[data-mode='dark']");
const lightMode = block("[data-mode='light']");

// ---------- themes.ts ----------
const themes = read("web/src/lib/theme/themes.ts");
function palette(id) {
  const m = themes.match(new RegExp(`\\n\\t${id}: \\{([\\s\\S]*?)\\n\\t\\}`));
  if (!m) throw new Error(`themes.ts: Palette ${id} fehlt`);
  const p = {};
  for (const e of m[1].matchAll(/(\w+): '(#[0-9A-Fa-f]{6})'/g))
    p[e[1]] = e[2].toLowerCase();
  return p;
}
const graphit = palette("graphit");
const hell = palette("hell");
const accentMatch = themes.match(
  /blau: \{ hex: '(#[0-9A-Fa-f]{6})', dark: '(#[0-9A-Fa-f]{6})' \}/,
);
const onDark = themes
  .match(/ON_ACCENT_DARK: Hex = '(#[0-9A-Fa-f]{6})'/)[1]
  .toLowerCase();
const onLight = themes
  .match(/ON_ACCENT_LIGHT: Hex = '(#[0-9A-Fa-f]{6})'/)[1]
  .toLowerCase();

// Dieselbe Farbmathematik wie web/src/lib/theme/color.ts.
const toRgb = (h) => {
  const n = parseInt(h.slice(1), 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
};
const toHex = (rgb) =>
  "#" + rgb.map((v) => Math.round(v).toString(16).padStart(2, "0")).join("");
const luminance = (h) => {
  const [r, g, b] = toRgb(h).map((v) => {
    const c = v / 255;
    return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
};
const contrast = (a, b) => {
  const x = luminance(a),
    y = luminance(b);
  return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
};
const shade = (h, amt) =>
  toHex(toRgb(h).map((v) => v + (amt > 0 ? (255 - v) * amt : v * amt)));
const darkenFor = (h, bg, min) => {
  let c = h;
  for (let i = 0; i < 40 && contrast(c, bg) < min; i++) c = shade(c, -0.05);
  return c;
};
// resolveAccent: dunkle Themes nehmen den Akzent, helle seine dunkle Fassung (mind. 3:1 zum Hintergrund).
const accentDark = accentMatch[1].toLowerCase();
const accentLight = darkenFor(accentMatch[2].toLowerCase(), hell.bg, 3);
const onAccent = (a) => (contrast(a, onDark) >= 4.5 ? onDark : onLight);

// ---------- Kotlin: Farben ----------
const hex = (v) => {
  let s = String(v).trim();
  if (/^#[0-9a-f]{3}$/i.test(s))
    s = "#" + [...s.slice(1)].map((c) => c + c).join("");
  const m = s.match(/^#([0-9a-f]{6})$/i);
  if (m) return `Color(0xFF${m[1].toUpperCase()})`;
  const r = s.match(/^rgba?\((\d+),\s*(\d+),\s*(\d+)(?:,\s*([\d.]+))?\)$/);
  if (r) {
    const a = Math.round((r[4] === undefined ? 1 : Number(r[4])) * 255);
    const h = (n) => Number(n).toString(16).padStart(2, "0").toUpperCase();
    return `Color(0x${h(a)}${h(r[1])}${h(r[2])}${h(r[3])})`;
  }
  throw new Error(`keine Farbe: ${s}`);
};
const scheme = (name, p, accent, mode, dark) => `    val ${name} = TitanColors(
        dark = ${dark},
        bg = ${hex(p.bg)},
        surface = ${hex(p.surface)},
        header = ${hex(p.header)},
        border = ${hex(p.border)},
        text = ${hex(p.text)},
        muted = ${hex(p.muted)},
        track = ${hex(p.track)},
        chip = ${hex(p.chip)},
        accent = ${hex(accent)},
        onAccent = ${hex(onAccent(accent))},
        warnBg = ${hex(mode["warn-bg"])},
        warnBorder = ${hex(mode["warn-border"])},
        warnText = ${hex(mode["warn-text"])},
        errorBg = ${hex(mode["error-bg"])},
        errorBorder = ${hex(mode["error-border"])},
        errorText = ${hex(mode["error-text"])},
        okBg = ${hex(mode["ok-bg"])},
        okBorder = ${hex(mode["ok-border"])},
        okText = ${hex(mode["ok-text"])},
    )`;
const squares = Object.keys(root).filter((k) => k.startsWith("icon-square-"));
const camel = (s) => s.replace(/-([a-z0-9])/g, (_, c) => c.toUpperCase());

const colorsKt = `${HEADER}package ${KT_PKG}

import androidx.compose.runtime.Immutable
import androidx.compose.ui.graphics.Color

/** Theme-Farben wie die CSS-Variablen der Weboberfläche (--bg, --surface, …). */
@Immutable
class TitanColors(
    val dark: Boolean,
    val bg: Color,
    val surface: Color,
    val header: Color,
    val border: Color,
    val text: Color,
    val muted: Color,
    val track: Color,
    val chip: Color,
    val accent: Color,
    val onAccent: Color,
    val warnBg: Color,
    val warnBorder: Color,
    val warnText: Color,
    val errorBg: Color,
    val errorBorder: Color,
    val errorText: Color,
    val okBg: Color,
    val okBorder: Color,
    val okText: Color,
) {
    /** Statusfarben (in jedem Theme gleich). Nie ohne Text verwenden. */
    val statusOk: Color get() = StatusColors.ok
    val statusWarn: Color get() = StatusColors.warn
    val statusError: Color get() = StatusColors.error
    val statusInactive: Color get() = StatusColors.inactive

    companion object {
${scheme("Graphit", graphit, accentDark, darkMode, true)}

${scheme("Hell", hell, accentLight, lightMode, false)}
    }
}

object StatusColors {
    val ok = ${hex(root["status-ok"])}
    val warn = ${hex(root["status-warn"])}
    val error = ${hex(root["status-error"])}
    val inactive = ${hex(root["status-inactive"])}
    val onWarn = ${hex(root["on-warn"])}
}

/** Farbige Symbol-Quadrate der Systemsteuerung (--icon-square-*), in jedem Theme gleich. */
object IconSquareColors {
${squares.map((k) => `    val ${camel(k.replace("icon-square-", ""))} = ${hex(root[k])}`).join("\n")}
    val on = ${hex(root["on-icon-square"])}
}

/** QR-Code immer dunkel auf hell. */
object QrColors {
    val light = ${hex(root["qr-light"])}
    val dark = ${hex(root["qr-dark"])}
}
`;

// ---------- Kotlin: Maße ----------
const px = (v) => {
  const m = String(v).match(/^([\d.]+)px$/);
  if (!m) throw new Error(`keine px-Angabe: ${v}`);
  return Number(m[1]);
};
const rem = (v) => {
  const m = String(v).match(/^([\d.]+)rem$/);
  if (!m) throw new Error(`keine rem-Angabe: ${v}`);
  return Number(m[1]) * 16;
};
const num = (n) => (Number.isInteger(n) ? `${n}` : `${+n.toFixed(3)}`);
const ctl = px(root["radius-ctl"]);
const calcOf = (v, base) => {
  const m = String(v).match(/^calc\(var\(--radius-ctl\) \* ([\d.]+)\)$/);
  if (!m) throw new Error(`unerwartet: ${v}`);
  return base * Number(m[1]);
};
const spaces = Object.keys(root).filter((k) => /^space-[\d-]+$/.test(k));
const texts = [
  "text-xs",
  "text-sm",
  "text-base",
  "text-md",
  "text-lg",
  "text-xl",
];
const dimensKt = `${HEADER}package ${KT_PKG}

import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/** Radien, Abstände und Schriftstufen wie in app.css (1rem = 16sp). */
object TitanDimens {
    val radiusTile = ${num(px(root["radius-tile"]))}.dp
    val radiusCtl = ${num(ctl)}.dp
    val radiusSmall = ${num(calcOf(root["radius-small"], ctl))}.dp
    val radiusBar = ${num(calcOf(root["radius-bar"], ctl))}.dp
${spaces.map((k) => `    val ${camel(k.replace(/-(\d)-(\d)/, "-$1_$2"))} = ${num(px(root[k]))}.dp`).join("\n")}
    val pagePad = ${num(px(root["page-pad"]))}.dp
    val gap = ${num(px(root["gap"]))}.dp
    val tilePad = ${num(px(root["tile-pad"]))}.dp
${texts.map((k) => `    val ${camel(k)} = ${num(rem(root[k]))}.sp`).join("\n")}
}
`;

// ---------- Programmsymbole ----------
const icon = read("web/src/lib/desktop/DesktopIcon.svelte");
const grab = (name) => {
  const m = icon.match(
    new RegExp(`const ${name}[^=]*= \\{([\\s\\S]*?)\\n\\t\\};`),
  );
  if (!m) throw new Error(`DesktopIcon.svelte: ${name} fehlt`);
  return Function(`return {${m[1].replace(/^\s*\/\/.*$/gm, "")}}`)();
};
const GLYPHS = grab("GLYPHS");
const GRADIENTS = grab("GRADIENTS");
// Neu für die App (PLAN 116, Entwurf 19.11.2026): Fotos und Mitteilungen. Sie gelten dann auch im Web.
GLYPHS.photos =
  '<rect x="2.5" y="4" width="19" height="16" rx="3" fill="#fff"/><path d="M2.5 16.5 8 11l4.5 4.5 3-3 6 6V17a3 3 0 0 1-3 3H5.5a3 3 0 0 1-3-3Z" fill="var(--c)" fill-opacity=".55"/><circle cx="16" cy="8.8" r="2" fill="var(--c)" fill-opacity=".7"/>';
GLYPHS.bell =
  '<path d="M12 2.8a6.2 6.2 0 0 0-6.2 6.2v4.6L3.8 17h16.4l-2-3.4V9A6.2 6.2 0 0 0 12 2.8Z" fill="#fff"/><path d="M9.2 18.6a2.8 2.8 0 0 0 5.6 0Z" fill="#fff" fill-opacity=".75"/>';

const attrs = (s) =>
  Object.fromEntries(
    [...s.matchAll(/([a-z-]+)="([^"]*)"/g)].map((m) => [m[1], m[2]]),
  );
const n = (v, d = 0) => (v === undefined ? d : Number(v));
function rectPath(a) {
  const x = n(a.x),
    y = n(a.y),
    w = n(a.width),
    h = n(a.height);
  let rx = a.rx !== undefined ? n(a.rx) : n(a.ry);
  let ry = a.ry !== undefined ? n(a.ry) : rx;
  rx = Math.min(rx, w / 2);
  ry = Math.min(ry, h / 2);
  if (!rx) return `M${x} ${y}h${w}v${h}h${-w}Z`;
  return `M${x + rx} ${y}H${x + w - rx}A${rx} ${ry} 0 0 1 ${x + w} ${y + ry}V${y + h - ry}A${rx} ${ry} 0 0 1 ${x + w - rx} ${y + h}H${x + rx}A${rx} ${ry} 0 0 1 ${x} ${y + h - ry}V${y + ry}A${rx} ${ry} 0 0 1 ${x + rx} ${y}Z`;
}
function circlePath(a) {
  const cx = n(a.cx),
    cy = n(a.cy),
    r = n(a.r);
  return `M${cx - r} ${cy}A${r} ${r} 0 1 0 ${cx + r} ${cy}A${r} ${r} 0 1 0 ${cx - r} ${cy}Z`;
}
const fmt = (d) =>
  d.replace(/-?\d*\.\d+|-?\d+/g, (x) => `${+Number(x).toFixed(4)}`);
/** Farbe eines Attributs: var(--c) ist die dunklere Verlaufsfarbe, #fff weiß, sonst die feste Farbe. */
function ink(v) {
  if (v === undefined || v === "none") return null;
  if (v === "var(--c)") return "c";
  if (v === "#fff" || v === "#ffffff") return "Color.White";
  if (/^#[0-9a-f]{6}$/i.test(v)) return hex(v);
  throw new Error(`unbekannte Farbe im Symbol: ${v}`);
}
function layers(svg) {
  const out = [];
  for (const m of svg.matchAll(/<(path|rect|circle)\s([^>]*?)\/?>/g)) {
    const a = attrs(m[2]);
    const d =
      m[1] === "path" ? a.d : m[1] === "rect" ? rectPath(a) : circlePath(a);
    // SVG füllt ohne fill-Angabe schwarz; die offenen Linien mit Strich füllen nichts.
    const fill =
      a.fill === undefined ? (a.stroke ? null : "Color.Black") : ink(a.fill);
    out.push({
      d: fmt(d),
      fill,
      fillAlpha: n(a["fill-opacity"], 1),
      stroke: ink(a.stroke),
      strokeAlpha: n(a["stroke-opacity"], 1),
      strokeWidth: n(a["stroke-width"], 1),
      cap:
        { round: "StrokeCap.Round", square: "StrokeCap.Square" }[
          a["stroke-linecap"]
        ] ?? "StrokeCap.Butt",
      join:
        { round: "StrokeJoin.Round", bevel: "StrokeJoin.Bevel" }[
          a["stroke-linejoin"]
        ] ?? "StrokeJoin.Miter",
    });
  }
  return out;
}
const f = (x) => `${+x.toFixed(4)}f`;
const glyphFns = Object.entries(GLYPHS).map(([name, svg]) => {
  const body = layers(svg)
    .map(
      (l) => `        addPath(
            pathData = addPathNodes("${l.d}"),
            fill = ${l.fill ? `SolidColor(${l.fill})` : "null"},
            fillAlpha = ${f(l.fillAlpha)},
            stroke = ${l.stroke ? `SolidColor(${l.stroke})` : "null"},
            strokeAlpha = ${f(l.strokeAlpha)},
            strokeLineWidth = ${f(l.strokeWidth)},
            strokeLineCap = ${l.cap},
            strokeLineJoin = ${l.join},
        )`,
    )
    .join("\n");
  return `    private fun ${camel(name)}(c: Color): ImageVector = ImageVector.Builder("${name}", 24.dp, 24.dp, 24f, 24f).apply {
${body}
    }.build()`;
});
const glyphsKt = `${HEADER}package ${KT_PKG}

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.addPathNodes
import androidx.compose.ui.unit.dp

/**
 * Programmsymbole wie am Arbeitsplatz (GLYPHS in DesktopIcon.svelte): weiß auf
 * einem Verlauf, Einzelheiten in der dunkleren Verlaufsfarbe [c].
 */
object ProgramGlyphs {
    val names: List<String> = listOf(${Object.keys(GLYPHS)
      .map((k) => `"${k}"`)
      .join(", ")})

    fun vector(name: String, c: Color): ImageVector? = when (name) {
${Object.keys(GLYPHS)
  .map((k) => `        "${k}" -> ${camel(k)}(c)`)
  .join("\n")}
        else -> null
    }

${glyphFns.join("\n\n")}
}

/** Verläufe der Programmsymbole (GRADIENTS): hell oben links, dunkel unten rechts. */
object ProgramGradients {
    val all: Map<String, Pair<Color, Color>> = mapOf(
${Object.entries(GRADIENTS)
  .map(([k, [a, c]]) => `        "${k}" to (${hex(a)} to ${hex(c)}),`)
  .join("\n")}
    )

    fun of(name: String): Pair<Color, Color> = all[name] ?: all.getValue("blue")
}
`;

// ---------- Schrift ----------
// --font-sans/--font-mono nennen IBM Plex; das Web lädt die Schnitte aus
// @fontsource (web/src/routes/+layout.svelte). Android liest kein WOFF; WOFF 1
// enthält die Tabellen der Schrift unverändert (zlib), daraus wird wieder TTF.
const sans = root["font-sans"].match(/'([^']+)'/)[1];
const mono = root["font-mono"].match(/'([^']+)'/)[1];
const layout = read("web/src/routes/+layout.svelte");
const used = (family) =>
  [
    ...layout.matchAll(
      new RegExp(`@fontsource/${family}/latin-(\\d+)\\.css`, "g"),
    ),
  ]
    .map((m) => Number(m[1]))
    .sort();
function woffToTtf(buf) {
  if (buf.toString("latin1", 0, 4) !== "wOFF") throw new Error("kein WOFF 1");
  const flavor = buf.readUInt32BE(4);
  const count = buf.readUInt16BE(12);
  const tables = [];
  for (let i = 0; i < count; i++) {
    const o = 44 + i * 20;
    const tag = buf.toString("latin1", o, o + 4);
    const off = buf.readUInt32BE(o + 4),
      comp = buf.readUInt32BE(o + 8),
      orig = buf.readUInt32BE(o + 12);
    const raw = buf.subarray(off, off + comp);
    const data = comp < orig ? inflateSync(raw) : Buffer.from(raw);
    if (data.length !== orig)
      throw new Error(`Tabelle ${tag}: Länge passt nicht`);
    tables.push({ tag, data, sum: buf.readUInt32BE(o + 16) });
  }
  tables.sort((a, b) => (a.tag < b.tag ? -1 : 1));
  let pow = 1;
  while (pow * 2 <= count) pow *= 2;
  const head = Buffer.alloc(12 + 16 * count);
  head.writeUInt32BE(flavor, 0);
  head.writeUInt16BE(count, 4);
  head.writeUInt16BE(pow * 16, 6);
  head.writeUInt16BE(Math.log2(pow), 8);
  head.writeUInt16BE(count * 16 - pow * 16, 10);
  const parts = [head];
  let offset = head.length;
  tables.forEach((t, i) => {
    const r = 12 + i * 16;
    head.write(t.tag, r, "latin1");
    head.writeUInt32BE(t.sum, r + 4);
    head.writeUInt32BE(offset, r + 8);
    head.writeUInt32BE(t.data.length, r + 12);
    const pad = (4 - (t.data.length % 4)) % 4;
    parts.push(t.data, Buffer.alloc(pad));
    t.offset = offset;
    offset += t.data.length + pad;
  });
  const out = Buffer.concat(parts);
  // checkSumAdjustment im head neu, da Reihenfolge und Abstände neu sind.
  const h = tables.find((t) => t.tag === "head");
  if (h) {
    out.writeUInt32BE(0, h.offset + 8);
    let sum = 0;
    for (let i = 0; i < out.length; i += 4)
      sum = (sum + out.readUInt32BE(i)) >>> 0;
    out.writeUInt32BE((0xb1b0afba - sum) >>> 0, h.offset + 8);
  }
  return out;
}
mkdirSync(FONT_DIR, { recursive: true });
const fonts = [];
for (const [family, kind] of [
  [sans, "sans"],
  [mono, "mono"],
]) {
  const pkg = family.toLowerCase().replace(/ /g, "-");
  const dir = join(repo, "web/node_modules/@fontsource", pkg);
  if (!existsSync(dir))
    throw new Error(`${dir} fehlt – zuerst im Ordner web „npm ci“`);
  const version = JSON.parse(
    readFileSync(join(dir, "package.json"), "utf8"),
  ).version;
  for (const w of used(pkg)) {
    const ttf = woffToTtf(
      readFileSync(join(dir, `files/${pkg}-latin-${w}-normal.woff`)),
    );
    const res = `${pkg.replace(/-/g, "_")}_${w}`;
    writeFileSync(join(FONT_DIR, `${res}.ttf`), ttf);
    fonts.push({ kind, res, weight: w, family, version });
  }
  mkdirSync(join(target, "licenses"), { recursive: true });
  writeFileSync(
    join(target, `licenses/${pkg}-OFL.txt`),
    readFileSync(join(dir, "LICENSE")),
  );
}
const fontsKt = `${HEADER}package ${KT_PKG}

import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import io.github.ra5on.titanos.ui.R

/**
 * Schrift wie im Web: --font-sans „${sans}“, --font-mono „${mono}“, dieselben
 * Schnitte wie web/src/routes/+layout.svelte (@fontsource ${fonts[0].version}, OFL-1.1).
 */
object TitanFonts {
    val sans = FontFamily(
${fonts
  .filter((x) => x.kind === "sans")
  .map((x) => `        Font(R.font.${x.res}, FontWeight(${x.weight})),`)
  .join("\n")}
    )
    val mono = FontFamily(
${fonts
  .filter((x) => x.kind === "mono")
  .map((x) => `        Font(R.font.${x.res}, FontWeight(${x.weight})),`)
  .join("\n")}
    )
}
`;

mkdirSync(KT_DIR, { recursive: true });
writeFileSync(join(KT_DIR, "TitanColors.kt"), colorsKt);
writeFileSync(join(KT_DIR, "TitanDimens.kt"), dimensKt);
writeFileSync(join(KT_DIR, "ProgramGlyphs.kt"), glyphsKt);
writeFileSync(join(KT_DIR, "TitanFonts.kt"), fontsKt);
console.log(
  `geschrieben: ${KT_DIR} (Farben, Maße, ${Object.keys(GLYPHS).length} Symbole, ${Object.keys(GRADIENTS).length} Verläufe), ` +
    `${fonts.length} Schriftschnitte in ${FONT_DIR}`,
);
