// 1.11 (step 15): isolated data for T-Kalender, shaped like the calendar
// API (Entwuerfe/108 BAU-API "Schritt 14a"; docs/KALENDER.md). Used by
// test-ui-kalender.mjs. Install it after the shared fixtures
// (test-ui-fixtures.mjs): these routes then answer first. "Today" is
// Tuesday, 17 November 2026, 12:29 in Berlin (the test fixes the clock and
// the zone); the events are those of the drafts (daten.ts). Every request
// is kept in `state.requests` ([method, path+query, body]); nothing reaches
// a NAS. /services/state answers calendar:true (the shared fixtures have no
// such key; scripts/test-ui-pim-visibility.mjs checks titand's real answer).

const pad = (n) => String(n).padStart(2, "0");
/** Berlin's offset on a date of 2026/2027 (CEST from the last Sunday of March to the last of October). */
function offset(day) {
  const y = Number(day.slice(0, 4));
  const summer = {
    2026: ["2026-03-29", "2026-10-25"],
    2027: ["2027-03-28", "2027-10-31"],
  }[y];
  return summer && day >= summer[0] && day < summer[1] ? "+02:00" : "+01:00";
}
const at = (day, time) => `${day}T${time}:00${offset(day)}`;
function addDays(day, n) {
  const d = new Date(`${day}T12:00:00Z`);
  d.setUTCDate(d.getUTCDate() + n);
  return d.toISOString().slice(0, 10);
}
const weekday = (day) => (new Date(`${day}T12:00:00Z`).getUTCDay() + 6) % 7;
const compact = (day, time) =>
  time
    ? new Date(at(day, time)).toISOString().replace(/[-:]/g, "").slice(0, 15) +
      "Z"
    : day.replace(/-/g, "");

export const TODAY = "2026-11-17";
/** A title longer than the list keeps: the list says clipped, the single event has it all. */
export const LONG_TITLE =
  "Angebot der Hausverwaltung Sonnenhof Nord durchsprechen und Belege für die Nebenkostenabrechnung 2026 vorbereiten";
/** A file name that needs encoding in the path (devices choose any name). */
export const ODD_ID = "elternabend 3b+klasse@schule.ics";

const RULE_WEEKLY = {
  freq: "weekly",
  interval: 1,
  byDay: ["MO"],
  count: 12,
  text: {
    de: "jede Woche am Montag, seit 7. September 2026",
    en: "every week on Monday, since 7 September 2026",
  },
};
const RULE_WORKDAYS = {
  freq: "weekly",
  interval: 1,
  byDay: ["MO", "TU", "WE", "TH", "FR"],
  text: { de: "jeden Werktag", en: "every weekday" },
};
const RULE_MONTHLY = {
  freq: "monthly",
  interval: 1,
  byDay: ["3TU"],
  until: "2027-07-13",
  text: {
    de: "jeden Monat am dritten Dienstag, bis 13. Juli 2027",
    en: "every month on the third Tuesday, until 13 July 2027",
  },
};

function view(over) {
  return {
    kind: "event",
    occurrence: "",
    uid: over.id.replace(/\.ics$/, ""),
    end: "",
    allDay: false,
    timezone: "Europe/Berlin",
    location: "",
    recurring: false,
    rule: null,
    alarms: [],
    exception: false,
    etag: `"e-${over.id}"`,
    tzGuess: false,
    recurUnsupported: false,
    readOnly: false,
    ...over,
  };
}
function timed(id, cal, title, day, from, to, more = {}) {
  return view({
    id,
    calendar: cal,
    title,
    start: at(day, from),
    end: at(day, to),
    occurrence: compact(day, from),
    ...more,
  });
}
function allDay(id, cal, title, day, days = 1, more = {}) {
  return view({
    id,
    calendar: cal,
    title,
    start: day,
    end: addDays(day, days),
    allDay: true,
    timezone: "",
    occurrence: compact(day),
    ...more,
  });
}

/** The data a test starts from; change it before or while the page runs. */
export function calendarState() {
  const calendars = [
    {
      id: "privat",
      name: "Privat",
      color: "#2563eb",
      order: 0,
      readOnly: false,
      kind: "own",
    },
    {
      id: "arbeit",
      name: "Arbeit",
      color: "#0f766e",
      order: 1,
      readOnly: false,
      kind: "own",
    },
    {
      id: "familie",
      name: "Familie",
      color: "#c2410c",
      order: 2,
      readOnly: false,
      kind: "own",
    },
    {
      id: "sport",
      name: "Sport",
      color: "#15803d",
      order: 3,
      readOnly: false,
      kind: "own",
    },
  ];
  const events = [];
  // Wochenbesprechung: weekly on Monday 09:00 from 7 September, 12 events.
  for (let i = 0; i < 12; i++) {
    const day = addDays("2026-09-07", 7 * i);
    events.push(
      timed(
        "wochenbesprechung.ics",
        "arbeit",
        "Wochenbesprechung",
        day,
        "09:00",
        "09:30",
        {
          recurring: true,
          rule: RULE_WEEKLY,
          occurrences: 12,
        },
      ),
    );
  }
  // Fahrgemeinschaft: every weekday 07:45, from 2 November.
  for (let day = "2026-11-02"; day <= "2027-01-31"; day = addDays(day, 1))
    if (weekday(day) < 5)
      events.push(
        timed(
          "fahrgemeinschaft.ics",
          "familie",
          "Fahrgemeinschaft Schule",
          day,
          "07:45",
          "08:15",
          {
            recurring: true,
            rule: RULE_WORKDAYS,
          },
        ),
      );
  // Elternabend: the third Tuesday of each month until July 2027 (file name needs encoding).
  for (const day of ["2026-11-17", "2026-12-15", "2027-01-19", "2027-02-16"])
    events.push(
      timed(ODD_ID, "familie", "Elternabend Klasse 3b", day, "19:00", "20:30", {
        location: "Grundschule am Park",
        recurring: true,
        rule: RULE_MONTHLY,
        alarms: [60, 1500],
        uid: "elternabend-3b",
      }),
    );
  events.push(
    allDay("quartal.ics", "arbeit", "Quartalsbericht abgeben", "2026-11-03"),
    timed(
      "reifen.ics",
      "privat",
      "Reifenwechsel",
      "2026-11-05",
      "08:00",
      "09:00",
      {
        location: "Autohaus Beispiel",
      },
    ),
    timed(
      "garten.ics",
      "familie",
      "Herbstputz Garten",
      "2026-11-07",
      "10:00",
      "13:00",
    ),
    timed(
      "laterne.ics",
      "familie",
      "Laternenumzug Kita",
      "2026-11-11",
      "17:00",
      "18:30",
    ),
    timed(
      "flohmarkt.ics",
      "privat",
      "Flohmarkt",
      "2026-11-14",
      "09:00",
      "12:00",
    ),
    timed(
      "paket.ics",
      "privat",
      "Paket bei der Post abholen",
      "2026-11-16",
      "17:30",
      "18:00",
    ),
    timed(
      "angebot.ics",
      "arbeit",
      LONG_TITLE.slice(0, 60),
      TODAY,
      "10:00",
      "11:30",
      {
        location: "Büro, Raum 2.14",
        clipped: true,
      },
    ),
    timed(
      "mittag.ics",
      "privat",
      "Mittagessen mit Anna",
      TODAY,
      "12:30",
      "13:15",
      {
        location: "Café am Markt",
        alarms: [30],
      },
    ),
    timed(
      "zahnarzt.ics",
      "privat",
      "Zahnarzt Dr. Beispiel",
      TODAY,
      "16:00",
      "16:30",
      {
        location: "Hauptstraße 12",
        alarms: [60],
      },
    ),
    allDay("oma.ics", "familie", "Oma zu Besuch", "2026-11-18", 3),
    timed(
      "sparkasse.ics",
      "arbeit",
      "Rückruf Sparkasse Beispiel",
      "2026-11-18",
      "14:00",
      "14:30",
    ),
    timed("messe.ics", "arbeit", "Messe Köln", "2026-11-19", "09:00", "17:00", {
      location: "Messe Köln, Halle 7",
    }),
    timed("kino.ics", "privat", "Kino", "2026-11-19", "20:00", "22:15"),
    timed(
      "abnahme.ics",
      "arbeit",
      "Projekt Nordlicht: Abnahme",
      "2026-11-20",
      "11:00",
      "12:00",
    ),
    timed(
      "einkauf.ics",
      "privat",
      "Wocheneinkauf",
      "2026-11-20",
      "17:00",
      "18:00",
    ),
    timed(
      "turnier.ics",
      "familie",
      "Fußballturnier Jonas",
      "2026-11-21",
      "10:00",
      "12:30",
      {
        location: "Sportplatz Süd",
      },
    ),
    timed(
      "essen.ics",
      "privat",
      "Essen bei Lena und Tom",
      "2026-11-21",
      "19:30",
      "23:00",
    ),
    timed(
      "kaffee.ics",
      "familie",
      "Kaffee bei Oma",
      "2026-11-22",
      "15:00",
      "17:00",
    ),
    timed("team.ics", "arbeit", "Teamessen", "2026-11-24", "18:00", "21:00"),
    timed(
      "steuer.ics",
      "privat",
      "Steuerberater",
      "2026-11-26",
      "15:00",
      "16:00",
    ),
    timed(
      "advent.ics",
      "familie",
      "Adventskranz binden",
      "2026-11-28",
      "14:00",
      "16:00",
    ),
    allDay("nikolaus.ics", "familie", "Nikolaus", "2026-12-06"),
    // A long word that only fits when it breaks (a file name as title).
    timed(
      "datei.ics",
      "privat",
      "Belegsammlung_Nebenkostenabrechnung_2026_endgueltig_v3.pdf",
      "2026-11-26",
      "09:00",
      "09:30",
    ),
    // Hidden calendar: never in a list without ?calendars=.
    timed("lauf.ics", "sport", "Lauftreff", TODAY, "18:00", "19:00"),
  );
  const notes = {
    [ODD_ID]:
      "Bastelsachen für den Adventsbasar mitbringen.\nPlan: https://schule.example.org/elternabend?k=3b. Fragen an mailto:krueger@schule.example oder tel:+49-221-444120.\n<img src=x onerror=alert(1)> javascript:alert(1)",
  };
  const birthdays = [
    ["anna.vcf", "Anna Beispiel", "2026-11-12", 1991],
    ["ben.vcf", "Ben Muster", "2026-11-20", 1986],
    ["lena.vcf", "Lena Beispiel", "2026-12-02", 1997],
  ].map(([id, name, day, year]) =>
    view({
      kind: "birthday",
      id,
      calendar: "geburtstage",
      title: name,
      start: day,
      end: addDays(day, 1),
      allDay: true,
      timezone: "",
      occurrence: day.replace(/-/g, ""),
      readOnly: true,
      etag: "",
      birthday: { name, year, age: Number(day.slice(0, 4)) - year },
    }),
  );
  return {
    calendars,
    events,
    notes,
    birthdays,
    hidden: ["sport"],
    /** "503": every route answers calendar_unavailable; "volume": /access says volume:false. */
    unavailable: "",
    /** The NAS zone /access names ("" leaves it out: the page takes the browser's). */
    timezone: "Europe/Berlin",
    /** The next PUT/DELETE of an event answers calendar_conflict. */
    conflictOnce: false,
    importResult: {
      added: 3,
      updated: 1,
      skipped: 2,
      failed: [
        { uid: "aufgabe-1@example.org", reason: "unsupported_component" },
      ],
    },
    created: [],
    requests: [],
  };
}

function countOf(s, id) {
  if (id === "geburtstage") return s.birthdays.length;
  return new Set(s.events.filter((e) => e.calendar === id).map((e) => e.id))
    .size;
}
function calendarsOf(s) {
  return [
    ...s.calendars.map((c) => ({
      ...c,
      count: countOf(s, c.id),
      hidden: s.hidden.includes(c.id),
    })),
    {
      id: "geburtstage",
      name: "Geburtstage",
      color: "#be185d",
      order: 1000,
      readOnly: true,
      kind: "birthdays",
      count: s.birthdays.length,
      hidden: s.hidden.includes("geburtstage"),
    },
  ];
}
/** Date of a list bound: 'YYYY-MM-DD' or RFC 3339. */
const dayOf = (v) => (v ?? "").slice(0, 10);

/** The next dates of a simple rule, as titand's preview would say (enough for the form). */
function previewOf(body) {
  const start = dayOf(body.start);
  const r = body.repeat;
  if (!r) return { occurrences: [], text: null, total: null, more: false };
  const dates = [];
  let d = start;
  const nth = Math.ceil(Number(start.slice(8)) / 7);
  const wd = weekday(start);
  for (let i = 0; i < 400 && dates.length < 400; i++) {
    if (r.until && d > r.until) break;
    if (r.count && dates.length >= r.count) break;
    dates.push(d);
    if (r.freq === "daily") d = addDays(d, r.interval || 1);
    else if (r.freq === "weekly") {
      d = addDays(
        d,
        r.byDay && r.byDay.length > 1
          ? weekday(d) === 4
            ? 3
            : 1
          : 7 * (r.interval || 1),
      );
    } else if (r.freq === "monthly") {
      const [y, m] = d.split("-").map(Number);
      const next = new Date(Date.UTC(y, m - 1 + (r.interval || 1), 1, 12));
      const first = next.toISOString().slice(0, 10);
      if (r.byDay && r.byDay[0]) {
        const shift = (wd - weekday(first) + 7) % 7;
        d = addDays(first, shift + 7 * (nth - 1));
      } else d = first.slice(0, 8) + start.slice(8);
    } else d = String(Number(d.slice(0, 4)) + (r.interval || 1)) + d.slice(4);
  }
  const time = body.allDay ? "" : (body.start ?? "").slice(10, 16);
  return {
    occurrences: dates
      .slice(0, 5)
      .map((x) => ({ start: x + time, end: x + time })),
    text: { de: "Regel", en: "rule" },
    total: r.count || r.until ? dates.length : null,
    more: dates.length > 5,
  };
}

export async function installCalendar(target, state = calendarState()) {
  const s = state;
  await target.route("**/api/v1/services/state", (route) =>
    route.fulfill({
      json: {
        docker: true,
        vms: true,
        lxc: true,
        calendar: true,
        contacts: true,
      },
    }),
  );
  await target.route(/\/api\/v1\/preferences\/calendar$/, (route) => {
    const req = route.request();
    const body = req.method() === "PUT" ? req.postDataJSON() : null;
    s.requests.push([req.method(), "/preferences/calendar", body]);
    if (body) s.hidden = body.hidden ?? [];
    return route.fulfill({ status: 204, body: "" });
  });
  await target.route(/\/api\/v1\/calendar(\/|\?|$)/, async (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const path = url.pathname.slice("/api/v1".length);
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
    s.requests.push([method, path + url.search, body]);
    const json = (value, status = 200) =>
      route.fulfill({ status, json: value });
    const fail = (status, code) =>
      route.fulfill({ status, json: { error: { code, message: code } } });
    const q = (k) => url.searchParams.get(k);
    if (s.unavailable === "503") {
      return route.fulfill({
        status: 503,
        headers: { "Retry-After": "300" },
        json: { error: { code: "calendar_unavailable", message: "no volume" } },
      });
    }
    if (path === "/calendar/access")
      return json({
        enabled: true,
        volume: s.unavailable !== "volume",
        ...(s.timezone ? { timezone: s.timezone } : {}),
      });
    if (path === "/calendar/calendars" && method === "GET")
      return json(calendarsOf(s));
    if (path === "/calendar/calendars" && method === "POST") {
      const id =
        body.name.toLowerCase().replace(/[^a-z0-9]+/g, "-") || "kalender";
      const c = {
        id,
        name: body.name,
        color: body.color ?? "#7c3aed",
        order: s.calendars.length,
        readOnly: false,
        kind: "own",
      };
      s.calendars.push(c);
      return json({ ...c, count: 0, hidden: false }, 201);
    }
    let m = path.match(/^\/calendar\/calendars\/([^/]+)$/);
    if (m) {
      const id = decodeURIComponent(m[1]);
      if (id === "geburtstage") return fail(403, "calendar_read_only");
      const c = s.calendars.find((x) => x.id === id);
      if (!c) return fail(404, "calendar_not_found");
      if (method === "PUT") {
        Object.assign(c, body);
        return json({
          ...c,
          count: countOf(s, id),
          hidden: s.hidden.includes(id),
        });
      }
      if (method === "DELETE") {
        if (s.calendars.length === 1) return fail(409, "calendar_last");
        s.calendars = s.calendars.filter((x) => x !== c);
        s.events = s.events.filter((e) => e.calendar !== id);
        s.hidden = s.hidden.filter((h) => h !== id);
        return route.fulfill({ status: 204, body: "" });
      }
    }
    m = path.match(/^\/calendar\/calendars\/([^/]+)\/import$/);
    if (m && method === "POST") return json(s.importResult);
    m = path.match(/^\/calendar\/calendars\/([^/]+)\/export$/);
    if (m) {
      const id = decodeURIComponent(m[1]);
      return route.fulfill({
        status: 200,
        headers: {
          "Content-Type": "text/calendar; charset=utf-8",
          "Content-Disposition": `attachment; filename="${id}.ics"`,
        },
        body: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//TitanOS//T-Kalender 1.11//DE\r\nEND:VCALENDAR\r\n",
      });
    }
    if (path === "/calendar/events" && method === "GET") {
      const from = dayOf(q("from"));
      const to = dayOf(q("to"));
      const inWindow = (e) => {
        const start = dayOf(e.start);
        const end = e.allDay
          ? dayOf(e.end)
          : addDays(dayOf(e.end || e.start), 1);
        return start < to && end > from;
      };
      const list = s.events
        .filter((e) => !s.hidden.includes(e.calendar) && inWindow(e))
        .concat(
          s.hidden.includes("geburtstage") ? [] : s.birthdays.filter(inWindow),
        )
        .sort((a, b) => new Date(a.start) - new Date(b.start));
      return json({ events: list, truncated: false });
    }
    if (path === "/calendar/events" && method === "POST") {
      const id = `neu-${s.created.length + 1}.ics`;
      s.created.push(body);
      return json(
        {
          id,
          etag: `"e-${id}"`,
          calendar: body.calendar,
          uid: id,
          scope: "all",
          droppedExceptions: 0,
        },
        201,
      );
    }
    if (path === "/calendar/preview" && method === "POST")
      return json(previewOf(body));
    m = path.match(/^\/calendar\/events\/([^/]+)\/([^/]+)$/);
    if (m) {
      const cal = decodeURIComponent(m[1]);
      const id = decodeURIComponent(m[2]);
      const occ = q("occurrence") ?? "";
      const pool = cal === "geburtstage" ? s.birthdays : s.events;
      const all = pool.filter((e) => e.calendar === cal && e.id === id);
      if (!all.length) return fail(404, "event_not_found");
      const one = (occ && all.find((e) => e.occurrence === occ)) || all[0];
      if (method === "GET") {
        const full = { ...one, notes: s.notes[id] ?? "" };
        if (id === "angebot.ics") {
          full.title = LONG_TITLE;
          delete full.clipped;
        }
        return json(full);
      }
      if (method === "PUT" || method === "DELETE") {
        const etag = method === "PUT" ? body.etag : q("etag");
        if (!etag) return fail(400, "calendar_invalid");
        if (s.conflictOnce || etag !== one.etag) {
          s.conflictOnce = false;
          return route.fulfill({
            status: 409,
            json: {
              error: { code: "calendar_conflict", message: "changed" },
              current: one,
            },
          });
        }
        if (method === "DELETE") {
          const scope = q("scope");
          const keep = (e) => {
            if (e.calendar !== cal || e.id !== id) return true;
            if (scope === "this") return e.occurrence !== occ;
            if (scope === "following")
              return new Date(e.start) < new Date(one.start);
            return false;
          };
          s.events = s.events.filter(keep);
          const removed = !s.events.some((e) => e.id === id);
          return json({
            removed,
            scope,
            ...(removed ? {} : { etag: `"e-${id}-2"` }),
          });
        }
        const ev = body.event ?? {};
        for (const e of all) {
          if (body.scope === "this" && e !== one) continue;
          if (typeof ev.title === "string") e.title = ev.title;
          if (typeof ev.location === "string") e.location = ev.location;
          if (body.calendar) e.calendar = body.calendar;
        }
        return json({
          id,
          etag: one.etag,
          calendar: body.calendar ?? cal,
          scope: body.scope,
          droppedExceptions: s.dropOnSave ?? 0,
        });
      }
    }
    return fail(404, "not_found");
  });
}
