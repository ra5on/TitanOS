// 1.11 (step 16): isolated data for Kontakte, shaped like the contacts API
// (Entwuerfe/108 BAU-API "Schritt 14b"; docs/KALENDER.md). Used by
// test-ui-kontakte.mjs. Install it after the shared fixtures
// (test-ui-fixtures.mjs) and after installCalendar (the "Termin" button
// opens T-Kalender's event form): these routes then answer first. "Today"
// is Tuesday, 17 November 2026 in Berlin (the test fixes the clock); the
// people are those of the drafts (daten.ts) plus one with very long
// values. Every request is kept in `state.requests` ([method, path+query,
// body]); nothing reaches a NAS. /services/state answers contacts:true and
// `mail` as the state says; /mail/access (where the web UI takes T-Mail's
// release from) says the same.

const TODAY = "2026-11-17";
const WEEK = ["2026-11-16", "2026-11-22"];

export const LONG_NAME =
  "Maximiliane Konstantinopoulou-Schwarzenberger-Wittelsbach";
export const LONG_MAIL =
  "maximiliane.konstantinopoulou-schwarzenberger@verwaltungsgesellschaft-sonnenhof-nord.example";

/** Folds like titand's cardweb.Fold. */
export function fold(s) {
  return s
    .toLowerCase()
    .normalize("NFD")
    .replace(/\p{M}/gu, "")
    .replace(/ß/g, "ss");
}

const contact = (over) => ({
  book: "kontakte",
  etag: `"1-${over.id}"`,
  first: "",
  last: "",
  org: "",
  phones: [],
  emails: [],
  addresses: [],
  birthday: "",
  notes: "",
  hasPhoto: false,
  modified: "2026-11-03T18:12:00+01:00",
  ...over,
});
const tel = (type, value, label) => ({
  type,
  value,
  ...(label ? { label } : {}),
});
const mailOf = (type, value) => ({ type, value });

/** The data a test starts from; change it before or while the page runs. */
export function contactsState() {
  return {
    contacts: [
      contact({
        id: "anna.vcf",
        first: "Anna",
        last: "Beispiel",
        org: "Praxis am Markt",
        hasPhoto: true,
        phones: [tel("mobile", "+49 170 1234567")],
        emails: [mailOf("home", "anna@beispiel.example")],
        birthday: "1991-11-12",
      }),
      contact({
        id: "ben.vcf",
        first: "Ben",
        last: "Muster",
        hasPhoto: true,
        phones: [
          tel("mobile", "+49 151 7654321"),
          tel("work", "+49 221 555 0142"),
        ],
        emails: [
          mailOf("home", "ben.muster@mail.example"),
          mailOf("work", "b.muster@firma.example"),
        ],
        addresses: [
          {
            type: "home",
            street: "Lindenallee 7",
            postcode: "50667",
            city: "Köln",
            region: "",
            country: "Deutschland",
          },
        ],
        birthday: "1986-11-20",
        notes:
          "Spielt mittwochs Fußball. Allergisch gegen Nüsse – beim Grillen dran denken.",
      }),
      contact({
        id: "carla.vcf",
        first: "Carla",
        last: "Beispiel-Hoffmann",
        org: "Hausverwaltung Sonnenhof Nord Verwaltungsgesellschaft mbH & Co. KG",
        phones: [tel("work", "+49 221 987 650")],
        emails: [mailOf("work", "c.hoffmann@sonnenhof-nord.example")],
      }),
      contact({
        id: "dennis.vcf",
        first: "Dennis",
        last: "Brandt",
        phones: [tel("mobile", "+49 160 2223344")],
        birthday: "--11-16",
      }),
      contact({
        id: "autohaus.vcf",
        org: "Autohaus Beispiel",
        phones: [tel("work", "+49 221 300 400")],
        emails: [mailOf("work", "service@autohaus.example")],
      }),
      contact({
        id: "eva.vcf",
        first: "Eva",
        last: "Krüger",
        emails: [mailOf("home", "eva.krueger@mail.example")],
        birthday: "1979-02-03",
      }),
      contact({
        id: "schule.vcf",
        org: "Grundschule am Park",
        phones: [tel("work", "+49 221 444 120")],
      }),
      contact({
        id: "lena.vcf",
        first: "Lena",
        last: "Beispiel",
        hasPhoto: true,
        phones: [tel("mobile", "+49 172 9988776")],
        birthday: "1997-12-02",
      }),
      contact({
        // A device's file name that needs encoding; a label of its own and a region.
        id: "Oma Hilde+1@icloud.vcf",
        first: "Oma",
        last: "Hilde",
        phones: [tel("other", "+49 2203 12345", "Festnetz")],
        addresses: [
          {
            type: "home",
            street: "Am Weiher 3\nHinterhaus",
            postcode: "51147",
            city: "Köln",
            region: "NRW",
            country: "",
          },
        ],
      }),
      contact({
        id: "tom.vcf",
        first: "Tom",
        last: "Richter",
        phones: [tel("mobile", "+49 176 1112223")],
        emails: [mailOf("home", "tom@richter.example")],
        birthday: "1990-11-17",
      }),
      contact({
        id: "lang.vcf",
        first: "Maximiliane",
        last: "Konstantinopoulou-Schwarzenberger-Wittelsbach",
        org: "Hausverwaltung Sonnenhof Nord Verwaltungsgesellschaft mbH & Co. KG, Abteilung Mietnebenkostenabrechnung",
        phones: [tel("work", "+49 221 987 650 12345 67890")],
        emails: [mailOf("work", LONG_MAIL)],
        addresses: [
          {
            type: "work",
            street:
              "Bürgermeister-Johann-Christian-Friedrich-Hölderlin-Straße 1234a, Seitenflügel, Treppenhaus C, 5. Obergeschoss",
            postcode: "50667",
            city: "Köln-Neustadt-Nord",
            region: "",
            country: "Deutschland",
          },
        ],
        notes:
          "Sehr lange Notiz ohne Leerzeichen: Mietnebenkostenabrechnungsunterlagenzusammenstellungsfrist https://example.com/eine/sehr/lange/adresse/die/umbrechen/muss/damit/nichts/abgeschnitten/wird",
      }),
    ],
    learned: [
      { name: "Sparkasse Beispiel", addr: "service@bank.example", count: 14 },
      { name: "Paketdienst", addr: "noreply@paket.example", count: 9 },
      { name: "Jonas Trainer", addr: "trainer@fc-sued.example", count: 6 },
    ],
    /** T-Mail's release of the account. */
    mail: true,
    /** "503": every route answers contacts_unavailable. */
    unavailable: "",
    /** The next PUT/DELETE of a contact answers contacts_conflict. */
    conflictOnce: false,
    importResult: {
      added: 4,
      updated: 1,
      skipped: 2,
      failed: [{ name: "Kaputte Karte", reason: "invalid" }],
    },
    files: [
      { name: "ben.jpg", dir: false, size: 48211 },
      { name: "notizen.txt", dir: false, size: 120 },
      { name: "Urlaub", dir: true, size: 0 },
    ],
    created: 0,
    requests: [],
  };
}

const fullName = (c) =>
  [c.first, c.last].filter(Boolean).join(" ") ||
  c.org ||
  c.emails[0]?.value ||
  c.phones[0]?.value ||
  "";
const sortKey = (c) =>
  fold(
    [c.last, c.first].filter(Boolean).join(" ") ||
      c.org ||
      c.emails[0]?.value ||
      "",
  );
/** Like titand's cardweb.Initials: one letter for a company. */
function initials(name, org) {
  const words = name.split(/\s+/).filter(Boolean);
  if (!words.length) return "?";
  if (name === org || words.length === 1) return words[0][0].toUpperCase();
  return (words[0][0] + words[words.length - 1][0]).toUpperCase();
}
function soon(c) {
  const m = /^(\d{4}|-)-?(\d{2})-(\d{2})$/.exec(c.birthday);
  if (!m) return undefined;
  const date = `2026-${m[2]}-${m[3]}`;
  if (date < WEEK[0] || date > WEEK[1]) return undefined;
  return m[1] === "-" ? { date } : { date, age: 2026 - Number(m[1]) };
}
function itemOf(c) {
  const name = fullName(c);
  const out = {
    id: c.id,
    book: "kontakte",
    name,
    sortKey: sortKey(c),
    org: c.org,
    initials: initials(name, c.org),
    hasPhoto: c.hasPhoto,
  };
  if (c.emails[0]) out.email = c.emails[0].value;
  const s = soon(c);
  if (s) out.birthdaySoon = s;
  return out;
}
const digits = (s) => s.replace(/\D/g, "");
function matches(c, q) {
  const f = fold(q.trim());
  if (!f) return true;
  const text = [
    fullName(c),
    c.org,
    ...c.emails.map((e) => e.value),
    ...c.addresses.map((a) =>
      [a.street, a.postcode, a.city, a.region, a.country].join(" "),
    ),
  ];
  if (text.some((x) => fold(x).includes(f))) return true;
  if (/^[\d+\-/(). ]+$/.test(q) && digits(q).length >= 3) {
    const d = digits(q).replace(/^0+/, "");
    return c.phones.some((p) => digits(p.value).includes(d));
  }
  return false;
}
function viewOf(c) {
  return { ...c, name: fullName(c) };
}
const KNOWN = [
  "first",
  "last",
  "org",
  "phones",
  "emails",
  "addresses",
  "birthday",
  "notes",
];

/** A drawn picture (no real photo) in the color of a contact. */
function picture(id) {
  const color =
    { "anna.vcf": "#0f766e", "ben.vcf": "#7c3aed", "lena.vcf": "#ea580c" }[
      id
    ] ?? "#2563eb";
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 40"><rect width="40" height="40" fill="${color}"/><circle cx="20" cy="16" r="7" fill="#fff" fill-opacity=".9"/><path d="M6 40a14 14 0 0 1 28 0Z" fill="#fff" fill-opacity=".9"/></svg>`;
}

export async function installContacts(target, state = contactsState()) {
  const s = state;
  await target.route("**/api/v1/services/state", (route) =>
    route.fulfill({
      json: {
        docker: true,
        vms: true,
        lxc: true,
        calendar: true,
        contacts: true,
        mail: s.mail,
      },
    }),
  );
  await target.route("**/api/v1/mail/access", (route) =>
    route.fulfill({ json: { enabled: s.mail } }),
  );
  // T-Mail's compose ("E-Mail schreiben") opens without mailboxes.
  await target.route("**/api/v1/mail/accounts", (route) =>
    route.fulfill({ json: [] }),
  );
  // NAS files for a contact's picture (the shared fixtures list nothing).
  await target.route(/\/api\/v1\/files\/(list|thumb)\?/, (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.endsWith("/thumb"))
      return route.fulfill({
        contentType: "image/svg+xml",
        body: picture("ben.vcf"),
      });
    if (url.searchParams.get("path")) return route.fulfill({ json: [] });
    return route.fulfill({
      json: s.files.map((f) => ({
        ...f,
        modified: "2026-11-01T10:00:00Z",
      })),
    });
  });
  await target.route(/\/api\/v1\/contacts(\/|\?|$)/, async (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const path = decodeURI(url.pathname.slice("/api/v1".length));
    const method = req.method();
    let body = null;
    const type = req.headers()["content-type"] ?? "";
    if (type.includes("application/json")) {
      try {
        body = req.postDataJSON();
      } catch {
        body = null;
      }
    } else if (type.includes("multipart")) body = "(multipart)";
    // Kept as sent (an id stays encoded).
    s.requests.push([
      method,
      url.pathname.slice("/api/v1".length) + url.search,
      body,
    ]);
    const json = (value, status = 200) =>
      route.fulfill({ status, json: value });
    const fail = (status, code, extra = {}) =>
      route.fulfill({
        status,
        json: { error: { code, message: code }, ...extra },
      });
    const q = (k) => url.searchParams.get(k) ?? "";
    if (s.unavailable === "503")
      return route.fulfill({
        status: 503,
        headers: { "Retry-After": "300" },
        json: { error: { code: "contacts_unavailable", message: "no volume" } },
      });
    const sorted = () =>
      [...s.contacts].sort(
        (a, b) =>
          sortKey(a).localeCompare(sortKey(b)) ||
          fullName(a).localeCompare(fullName(b)),
      );

    if (path === "/contacts/books")
      return json([
        { id: "kontakte", name: "Kontakte", count: s.contacts.length },
      ]);
    if (path === "/contacts" && method === "GET")
      return json(
        sorted()
          .filter((c) => matches(c, q("q")))
          .map(itemOf),
      );
    if (path === "/contacts" && method === "POST") {
      s.created++;
      const id = `neu-${s.created}.vcf`;
      const c = contact({ id, etag: `"1-${id}"`, ...body.contact });
      s.contacts.push(c);
      return json({ id, book: "kontakte", etag: c.etag }, 201);
    }
    if (path === "/contacts/suggestions") {
      if (!s.mail) return json([]);
      const have = new Set(
        s.contacts.flatMap((c) => c.emails.map((e) => e.value.toLowerCase())),
      );
      const f = fold(q("q"));
      return json(
        s.learned.filter(
          (l) =>
            !have.has(l.addr) &&
            (!f || fold(`${l.name} ${l.addr}`).includes(f)),
        ),
      );
    }
    if (path === "/contacts/from-suggestion") {
      const l = s.mail && s.learned.find((x) => x.addr === body.addr);
      if (!l) return fail(404, "contact_suggestion_not_found");
      if (s.contacts.some((c) => c.emails.some((e) => e.value === l.addr)))
        return fail(409, "contact_exists");
      const words = l.name.split(" ");
      s.created++;
      const id = `neu-${s.created}.vcf`;
      const c = contact({
        id,
        first: words.length > 1 ? words.slice(0, -1).join(" ") : words[0],
        last: words.length > 1 ? words.at(-1) : "",
        emails: [mailOf("other", l.addr)],
      });
      s.contacts.push(c);
      return json({ id, book: "kontakte", etag: c.etag }, 201);
    }
    if (path === "/contacts/books/kontakte/import") return json(s.importResult);
    if (path === "/contacts/books/kontakte/export")
      return route.fulfill({
        status: 200,
        headers: {
          "Content-Type": "text/vcard; charset=utf-8",
          "Content-Disposition": 'attachment; filename="kontakte.vcf"',
        },
        body: "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Ben Muster\r\nEND:VCARD\r\n",
      });

    const m = /^\/contacts\/kontakte\/(.+?)(\/photo)?$/.exec(path);
    if (!m) return fail(404, "not_found");
    const id = decodeURIComponent(url.pathname.split("/")[5]);
    const c = s.contacts.find((x) => x.id === id);
    if (!c) return fail(404, "contact_not_found");
    const bump = () => {
      const n = Number(c.etag.slice(1).split("-")[0]) + 1;
      c.etag = `"${n}-${c.id}"`;
    };
    if (m[2]) {
      if (method === "GET") {
        if (!c.hasPhoto) return fail(404, "contact_photo_not_found");
        return route.fulfill({
          contentType: "image/svg+xml",
          body: picture(c.id),
        });
      }
      if (q("etag") && q("etag") !== c.etag)
        return fail(409, "contacts_conflict", { current: viewOf(c) });
      bump();
      c.hasPhoto = method === "PUT";
      return json({
        id: c.id,
        book: "kontakte",
        etag: c.etag,
        hasPhoto: c.hasPhoto,
      });
    }
    if (method === "GET") return json(viewOf(c));
    if (method === "PUT" || method === "DELETE") {
      const etag = method === "PUT" ? body?.etag : q("etag");
      if (!etag) return fail(400, "contacts_invalid");
      if (s.conflictOnce || etag !== c.etag) {
        s.conflictOnce = false;
        bump();
        return fail(409, "contacts_conflict", { current: viewOf(c) });
      }
      if (method === "DELETE") {
        s.contacts = s.contacts.filter((x) => x !== c);
        return route.fulfill({ status: 204, body: "" });
      }
      for (const k of KNOWN)
        if (k in (body.contact ?? {})) c[k] = body.contact[k] ?? "";
      bump();
      return json({ id: c.id, book: "kontakte", etag: c.etag });
    }
    return fail(405, "method_not_allowed");
  });
}

export { TODAY };
