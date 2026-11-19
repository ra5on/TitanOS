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
    /** 1.13: groups (iOS's group vCards) with their members' ids. */
    groups: [],
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
    etag: c.etag.replace(/"/g, ""),
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

/**
 * A live stream for a test (state.stream): each connection gets "ready"
 * with the book's generation, then the events the test queued in
 * state.events (each {kind, collection, generation}); the stream ends
 * there and the page connects again (lib/pim/live.ts says "resync").
 */
export function liveStream(s) {
  return (route) => {
    const blocks = [
      `event: ready\ndata: ${JSON.stringify({ account: "user", generations: { "contacts/kontakte": `e-${s.listVersion ?? 0}` } })}\n\n`,
      ...(s.events ?? []).splice(0).map((e) => `event: pim\ndata: ${JSON.stringify({ account: "user", ...e })}\n\n`),
    ];
    s.streams = (s.streams ?? 0) + 1;
    return route.fulfill({ status: 200, contentType: "text/event-stream", body: blocks.join("") });
  };
}

/** Possible duplicates like titand's cardweb.Duplicates (e-mail, number, name). */
export function duplicatesOf(contacts) {
  const buckets = new Map();
  const add = (kind, key, value, id) => {
    if (!key) return;
    const k = kind + "\u0000" + key;
    const b = buckets.get(k) ?? { kind, value, ids: [] };
    if (!b.ids.includes(id)) b.ids.push(id);
    buckets.set(k, b);
  };
  for (const c of contacts) {
    for (const e of c.emails) add("email", e.value.toLowerCase(), e.value.toLowerCase(), c.id);
    for (const p of c.phones) {
      const d = digits(p.value).replace(/^0+/, "");
      if (d.length >= 6) add("phone", d.slice(-9), p.value, c.id);
    }
    const name = fold(fullName(c)).split(/[^\p{L}\p{N}]+/u).filter(Boolean).sort().join(" ");
    if (name.replace(/ /g, "").length >= 3) add("name", name, fullName(c), c.id);
  }
  const parent = new Map(contacts.map((c) => [c.id, c.id]));
  const find = (id) => (parent.get(id) === id ? id : find(parent.get(id)));
  const joins = [...buckets.values()].filter((b) => b.ids.length >= 2 && b.ids.length <= 4);
  for (const b of joins) for (const id of b.ids.slice(1)) parent.set(find(id), find(b.ids[0]));
  const sets = new Map();
  for (const c of contacts) {
    const r = find(c.id);
    sets.set(r, [...(sets.get(r) ?? []), c.id]);
  }
  return [...sets.values()]
    .filter((ids) => ids.length >= 2)
    .map((ids) => ({
      book: "kontakte",
      ids,
      matches: joins
        .filter((b) => ids.includes(b.ids[0]))
        .map((b) => ({ kind: b.kind, value: b.value, ids: b.ids })),
    }));
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
  // 1.13: the live stream of Kalender and Kontakte (lib/pim/live.ts): none
  // here unless a test sets `state.stream` (a function getting the route).
  await target.route("**/api/v1/calendar/stream", (route) =>
    s.stream
      ? s.stream(route)
      : route.fulfill({
          status: 503,
          json: { error: { code: "calendar_unavailable", message: "test" } },
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
      // Like titand (cardweb/list.go): the folded key compared byte-wise, so
      // "1&1 Hotline" comes before A and "Øvrebø" after Z (115-7).
      [...s.contacts].sort((a, b) => {
        const ka = fold(sortKey(a));
        const kb = fold(sortKey(b));
        return ka < kb ? -1 : ka > kb ? 1 : fullName(a).localeCompare(fullName(b));
      });

    if (path === "/contacts/books")
      return json([
        { id: "kontakte", name: "Kontakte", count: s.contacts.length },
      ]);
    // 1.13: the side bar, the search in one request, groups, duplicates, merging.
    const groupView = (g, extra = {}) => ({
      id: g.id,
      book: "kontakte",
      etag: g.etag ?? `"g1-${g.id}"`,
      name: g.name,
      members: g.members.filter((id) => s.contacts.some((c) => c.id === id)),
      other: 0,
      ...extra,
    });
    const listTag = () => `"list-${s.listVersion ?? 0}"`;
    // The book's ctag: one more per write (the live stream's generation).
    const ctagNow = () => `e-${s.listVersion ?? 0}`;
    const bumpList = () => (s.listVersion = (s.listVersion ?? 0) + 1);
    if (path === "/contacts/overview")
      return json({
        books: s.books ?? [{ id: "kontakte", name: "Kontakte", count: s.contacts.length }],
        groups: s.groups.map((g) => groupView(g)),
        duplicates: duplicatesOf(s.contacts).length,
      });
    if (path === "/contacts/search")
      return json({
        contacts: sorted()
          .filter((c) => matches(c, q("q")))
          .map(itemOf),
        suggestions: !s.mail
          ? []
          : s.learned.filter((l) => {
              const f = fold(q("q"));
              const have = s.contacts.some((c) =>
                c.emails.some((e) => e.value.toLowerCase() === l.addr),
              );
              return !have && f && fold(`${l.name} ${l.addr}`).includes(f);
            }),
      });
    if (path === "/contacts/duplicates") return json(duplicatesOf(s.contacts));
    if (path === "/contacts/groups" && method === "POST") {
      s.created++;
      const g = { id: `G${s.created}.vcf`, name: body.name, members: body.members ?? [] };
      s.groups.push(g);
      bumpList();
      return json(groupView(g, { listETag: listTag(), ctag: ctagNow() }), 201);
    }
    const gm = /^\/contacts\/groups\/(.+)$/.exec(path);
    if (gm) {
      const g = s.groups.find((x) => x.id === decodeURIComponent(gm[1]));
      if (!g) return fail(404, "contact_group_not_found");
      const etag = method === "PUT" ? body?.etag : q("etag");
      if (etag !== (g.etag ?? `"g1-${g.id}"`)) return fail(409, "contacts_conflict", { current: groupView(g) });
      bumpList();
      if (method === "DELETE") {
        s.groups = s.groups.filter((x) => x !== g);
        return route.fulfill({ status: 204, headers: { "X-List-ETag": listTag(), "X-CTag": ctagNow() }, body: "" });
      }
      if (body.name) g.name = body.name;
      for (const id of body.add ?? []) if (!g.members.includes(id)) g.members.push(id);
      g.members = g.members.filter((id) => !(body.remove ?? []).includes(id));
      const n = Number((g.etag ?? `"g1-${g.id}"`).slice(2).split("-")[0]) + 1;
      g.etag = `"g${n}-${g.id}"`;
      return json(groupView(g, { listETag: listTag(), ctag: ctagNow() }));
    }
    if (path === "/contacts/merge" && method === "POST") {
      const keep = s.contacts.find((c) => c.id === body.keep.id);
      const drop = s.contacts.find((c) => c.id === body.drop.id);
      if (!keep || !drop) return fail(404, "contact_not_found");
      if (keep.etag !== body.keep.etag || drop.etag !== body.drop.etag)
        return fail(409, "contacts_conflict", { current: viewOf(keep) });
      for (const k of KNOWN) if (k in body.contact) keep[k] = body.contact[k] ?? "";
      if (body.photo === "drop") keep.hasPhoto = drop.hasPhoto;
      s.contacts = s.contacts.filter((c) => c !== drop);
      let groups = 0;
      for (const g of s.groups)
        if (g.members.includes(drop.id)) {
          g.members = [...g.members.filter((x) => x !== drop.id && x !== keep.id), keep.id];
          groups++;
        }
      keep.etag = `"9-${keep.id}"`;
      bumpList();
      return json({ id: keep.id, book: "kontakte", etag: keep.etag, item: itemOf(keep), listETag: listTag(), ctag: ctagNow(), groups });
    }
    if (path === "/contacts" && method === "GET") {
      // 1.13: the list's ETag; asked with it and unchanged, 304 without a body.
      if (!q("q") && req.headers()["if-none-match"] === listTag())
        return route.fulfill({ status: 304, headers: { ETag: listTag() }, body: "" });
      return route.fulfill({
        status: 200,
        headers: q("q") ? {} : { ETag: listTag() },
        json: sorted()
          .filter((c) => matches(c, q("q")))
          .map(itemOf),
      });
    }
    if (path === "/contacts" && method === "POST") {
      s.created++;
      const id = `neu-${s.created}.vcf`;
      const c = contact({ id, etag: `"1-${id}"`, ...body.contact });
      s.contacts.push(c);
      bumpList();
      return json({ id, book: "kontakte", etag: c.etag, item: itemOf(c), listETag: listTag(), ctag: ctagNow() }, 201);
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
      bumpList();
      return json({ id, book: "kontakte", etag: c.etag, item: itemOf(c), listETag: listTag(), ctag: ctagNow() }, 201);
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
      bumpList();
      return json({
        id: c.id,
        book: "kontakte",
        etag: c.etag,
        hasPhoto: c.hasPhoto,
        item: itemOf(c),
        listETag: listTag(), ctag: ctagNow(),
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
      bumpList();
      if (method === "DELETE") {
        s.contacts = s.contacts.filter((x) => x !== c);
        return route.fulfill({ status: 204, headers: { "X-List-ETag": listTag(), "X-CTag": ctagNow() }, body: "" });
      }
      for (const k of KNOWN)
        if (k in (body.contact ?? {})) c[k] = body.contact[k] ?? "";
      bump();
      return json({ id: c.id, book: "kontakte", etag: c.etag, item: itemOf(c), listETag: listTag(), ctag: ctagNow() });
    }
    return fail(405, "method_not_allowed");
  });
}

export { TODAY };
