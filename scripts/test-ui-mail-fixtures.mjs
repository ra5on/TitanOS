// 1.05: isolated data for T-Mail, shaped like docs/TMAIL.md (API and
// "Offen für das Backend"). Used by test-ui-mail.mjs. Install it after the
// shared fixtures (test-ui-fixtures.mjs): these routes then answer first.
// Every request is kept in `state.requests` ([method, path+query, body]);
// nothing reaches a NAS or a mail server. The HTML message carries a script
// and a tracking picture; `state.sentinel` counts what a script or a picture
// would fetch, so a test can show that nothing ran or loaded.

const now = () => new Date();
const ago = (minutes) => new Date(Date.now() - minutes * 60000).toISOString();

function account(id, name, email, over = {}) {
  const domain = email.split("@")[1];
  return {
    id,
    name,
    email,
    user: email,
    imap: { host: `imap.${domain}`, port: 993, security: "tls" },
    smtp: { host: `mail.${domain}`, port: 465, security: "tls" },
    passwordSet: true,
    notify: true,
    signature: "s1",
    status: { state: "ok", code: "", since: ago(120) },
    unread: 0,
    ...over,
  };
}

const folder = (path, name, role) => ({
  path,
  name,
  role,
  unread: 0,
  total: 0,
});

const SENDERS = [
  ["Anna Beispiel", "anna@example.org"],
  [
    "Hausverwaltung Sonnenhof Nord Verwaltungsgesellschaft mbH & Co. KG",
    "info@sonnenhof.example",
  ],
  ["", "noreply@paket.example"],
  ["Ben Muster", "ben@example.net"],
];

function row(uid, i, over = {}) {
  const [name, addr] = SENDERS[i % SENDERS.length];
  return {
    uid,
    from: { name, addr },
    to: [{ name: "Alex", addr: "alex@gmx.example" }],
    subject: `Nachricht ${uid}: ${["Termin am Freitag", "Nebenkostenabrechnung 2026 für die Wohnung im dritten Obergeschoss links mit allen Belegen", "Ihr Paket kommt heute", "Fotos vom Wochenende"][i % 4]}`,
    date: ago(i * 37 + 5),
    flags: { seen: i % 3 !== 0, flagged: i % 7 === 1, answered: i % 5 === 2 },
    size: 4000 + i * 10,
    attachments: i % 4 === 1,
    ...over,
  };
}

/** The HTML of the bank-like message: a script, a tracking picture, a link whose text lies. */
export const HTML_SCRIPT =
  "<script>fetch('/sentinel/script');parent.postMessage('script-ran','*')</script>";
function bankHtml(images) {
  return (
    "<h1>Ihr Kontoauszug</h1><p>Guten Tag, Ihr Auszug für November liegt bei.</p>" +
    HTML_SCRIPT +
    '<p><a href="https://evil.example.test/login">www.sparkasse.example</a> · <a href="https://docs.example.test/hilfe">Hilfe</a></p>' +
    '<p><img src="cid:logo@bank" alt="Logo"></p>' +
    (images
      ? '<p><img src="https://track.example.test/pixel.gif" alt="Werbung"></p>'
      : '<p><span title="Bild blockiert">[Bild blockiert]</span></p>')
  );
}

/** The data a test starts from; change it before or while the page runs. */
export function mailState() {
  const accounts = [
    account("acc-privat", "Privat", "alex@gmx.example"),
    account("acc-arbeit", "Arbeit", "alex@firma.example", { signature: "" }),
  ];
  const folders = {
    "acc-privat": [
      folder("INBOX", "INBOX", "inbox"),
      folder("Entw&APw-rfe", "Entwürfe", "drafts"),
      folder("Gesendet", "Gesendet", "sent"),
      folder("Papierkorb", "Papierkorb", "trash"),
      folder("Spamverdacht", "Spamverdacht", "junk"),
      folder("Rechnungen", "Rechnungen", ""),
      folder("Rechnungen/2026", "2026", ""),
    ],
    "acc-arbeit": [
      folder("INBOX", "INBOX", "inbox"),
      folder("Drafts", "Drafts", "drafts"),
      folder("Sent", "Sent", "sent"),
      folder("Trash", "Trash", "trash"),
    ],
  };
  const inbox = [];
  for (let i = 0; i < 72; i++) inbox.push(row(1000 - i, i));
  // The newest: the HTML message with an attachment.
  inbox[0] = row(1000, 0, {
    from: { name: "Sparkasse Beispiel", addr: "service@bank.example" },
    subject: "Ihr Kontoauszug November",
    flags: { seen: false, flagged: false, answered: false },
    attachments: true,
  });
  const messages = {
    "acc-privat": {
      INBOX: inbox,
      Papierkorb: [row(50, 3, { subject: "Alte Werbung" })],
      "Entw&APw-rfe": [],
      Gesendet: [],
      Spamverdacht: [],
      Rechnungen: [row(60, 1)],
      "Rechnungen/2026": [],
    },
    "acc-arbeit": {
      INBOX: [row(20, 3, { subject: "Wochenplan" }), row(19, 0)],
      Drafts: [],
      Sent: [],
      Trash: [],
    },
  };
  return {
    accounts,
    folders,
    messages,
    contacts: [
      {
        name: "Anna Beispiel",
        addr: "anna@example.org",
        count: 12,
        last: ago(600),
      },
      {
        name: "Anton Groß",
        addr: "anton@example.org",
        count: 2,
        last: ago(9000),
      },
      {
        name: "Ben Muster",
        addr: "ben@example.net",
        count: 5,
        last: ago(3000),
      },
    ],
    settings: {
      signatures: [
        { id: "s1", name: "Privat kurz", text: "Viele Grüße\nAlex" },
        {
          id: "s2",
          name: "Ausführlich",
          text: "Viele Grüße\nAlex Beispiel\nMusterstraße 1, 12345 Beispielstadt",
        },
      ],
      rules: [
        {
          id: "r1",
          account: "acc-privat",
          enabled: true,
          when: { from: "rechnung@", to: "", subject: "" },
          then: { moveTo: "Rechnungen", markRead: false, flag: true },
        },
      ],
    },
    providers: {
      "gmail.example": {
        imap: { host: "imap.gmail.example", port: 993, security: "tls" },
        smtp: { host: "smtp.gmail.example", port: 465, security: "tls" },
        appPassword: true,
      },
    },
    /** Test answers per address: "auth" or "unreachable" fail that side. */
    testFails: {},
    /** Events the next /events stream sends (then ends; the page connects again). */
    events: [],
    sent: [],
    drafts: [],
    saved: [],
    sentinel: [],
    requests: [],
    nextUid: 2000,
    failSend: false,
  };
}

const PNG =
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==";

/** The JSON `meta` of a multipart body (send, draft) and the names of its files. */
function multipart(body) {
  const text = body ?? "";
  const meta = /name="meta"\r\n\r\n([\s\S]*?)\r\n--/.exec(text);
  const files = [...text.matchAll(/name="file"; filename="([^"]*)"/g)].map(
    (m) => m[1],
  );
  const nas = [
    ...text.matchAll(/name="nasFiles"\r\n\r\n([\s\S]*?)\r\n--/g),
  ].map((m) => m[1]);
  return { meta: meta ? JSON.parse(meta[1]) : null, files, nas };
}

function counts(s) {
  for (const a of s.accounts) {
    for (const f of s.folders[a.id]) {
      const list = s.messages[a.id][f.path] ?? [];
      f.total = list.length;
      f.unread = list.filter((m) => !m.flags.seen).length;
    }
    a.unread = s.folders[a.id].find((f) => f.role === "inbox")?.unread ?? 0;
  }
}

export async function installMail(target, state = mailState()) {
  const s = state;
  // What a script or an external picture inside the message would load.
  await target.route(/\/sentinel\//, (route) => {
    s.sentinel.push(route.request().url());
    return route.fulfill({ status: 204 });
  });
  await target.route(/^https:\/\/track\.example\.test\//, (route) => {
    s.sentinel.push(route.request().url());
    return route.fulfill({
      status: 200,
      contentType: "image/png",
      body: Buffer.from(PNG, "base64"),
    });
  });
  // The app release of the account: T-Mail shown.
  await target.route(/\/api\/v1\/services\/state$/, (route) =>
    route.fulfill({
      json: {
        docker: true,
        vms: true,
        lxc: true,
        agent: false,
        mail: s.disabled ? false : true,
      },
    }),
  );
  await target.route(/\/api\/v1\/files\/list/, (route) => {
    const url = new URL(route.request().url());
    const dir = url.searchParams.get("path") ?? "";
    const t = "2026-11-10T10:00:00Z";
    if (dir === "")
      return route.fulfill({
        json: [
          { name: "Rechnungen", dir: true, size: 0, modified: t },
          { name: "Angebot Küche.pdf", dir: false, size: 182000, modified: t },
          { name: "Lageplan.png", dir: false, size: 640000, modified: t },
        ],
      });
    return route.fulfill({ json: [] });
  });
  await target.route(/\/api\/v1\/mail(\/|\?|$)/, async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname.slice("/api/v1".length);
    const method = request.method();
    let body = null;
    try {
      body = request.postDataJSON();
    } catch {
      body = request.postData();
    }
    s.requests.push([method, path + url.search, body]);
    const json = (value, status = 200) =>
      route.fulfill({ status, json: value });
    const fail = (status, code, message = code) =>
      route.fulfill({ status, json: { error: { code, message } } });
    const q = (k) => url.searchParams.get(k);
    let m;
    counts(s);

    if (path === "/mail/events") {
      const events = s.events.splice(0);
      const text =
        [
          ": hallo",
          ...events.map(
            (e) => `event: ${e.type}\ndata: ${JSON.stringify(e.data)}`,
          ),
        ].join("\n\n") + "\n\n";
      return route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        body: text,
      });
    }
    if (path === "/mail/accounts" && method === "GET") return json(s.accounts);
    if (path === "/mail/accounts" && method === "POST") {
      const a = account("acc-" + s.accounts.length, body.name, body.email, {
        ...body,
        password: undefined,
        passwordSet: !!body.password,
      });
      delete a.password;
      s.accounts.push(a);
      s.folders[a.id] = [
        folder("INBOX", "INBOX", "inbox"),
        folder("Sent", "Sent", "sent"),
        folder("Trash", "Trash", "trash"),
      ];
      s.messages[a.id] = {
        INBOX: [row(5, 0, { subject: "Willkommen bei Ihrem neuen Postfach" })],
        Sent: [],
        Trash: [],
      };
      return json(a, 201);
    }
    if (path === "/mail/accounts/test") {
      const how = s.testFails[body?.email] ?? "";
      return json({
        imap:
          how === "auth"
            ? { ok: false, code: "mail_auth_failed" }
            : how === "unreachable"
              ? { ok: false, code: "mail_unreachable" }
              : { ok: true },
        smtp:
          how === "unreachable"
            ? { ok: false, code: "mail_unreachable" }
            : { ok: true },
      });
    }
    if ((m = /^\/mail\/accounts\/([^/]+)$/.exec(path))) {
      const id = decodeURIComponent(m[1]);
      const a = s.accounts.find((x) => x.id === id);
      if (!a) return fail(404, "mail_not_found");
      if (method === "DELETE") {
        s.accounts = s.accounts.filter((x) => x.id !== id);
        return route.fulfill({ status: 204 });
      }
      if (method === "PUT") {
        const { password, ...rest } = body;
        Object.assign(a, rest);
        // A new password: the login is tried again.
        if (password)
          a.status = { state: "ok", code: "", since: now().toISOString() };
        return json(a);
      }
    }
    if (path === "/mail/providers") {
      const domain = (q("email") ?? "").split("@")[1] ?? "";
      const p = s.providers[domain];
      return p ? json(p) : fail(404, "mail_not_found");
    }
    if (path === "/mail/contacts") {
      const needle = (q("q") ?? "").toLowerCase();
      return json(
        s.contacts
          .filter((c) => (c.name + " " + c.addr).toLowerCase().includes(needle))
          .slice(0, 20),
      );
    }
    if (path === "/mail/settings") {
      if (method === "PUT") s.settings = body;
      return json(s.settings);
    }
    if (!(m = /^\/mail\/accounts\/([^/]+)\/(.+)$/.exec(path)))
      return fail(404, "not_found");
    const id = decodeURIComponent(m[1]);
    const what = m[2];
    const a = s.accounts.find((x) => x.id === id);
    if (!a) return fail(404, "mail_not_found");
    if (a.status.state === "auth_failed" && what !== "folders")
      return fail(502, "mail_auth_failed");
    if (a.status.state === "unreachable" && what !== "folders")
      return fail(504, "mail_unreachable");
    const box = s.messages[id];

    if (what === "folders") {
      if (a.status.state === "auth_failed")
        return fail(502, "mail_auth_failed");
      if (a.status.state === "unreachable")
        return fail(504, "mail_unreachable");
      return json(s.folders[id]);
    }
    if (what === "messages") {
      let list = [...(box[q("folder")] ?? [])].sort((x, y) => y.uid - x.uid);
      const needle = (q("q") ?? "").toLowerCase();
      if (needle)
        list = list.filter((r) =>
          (r.subject + " " + r.from.name + " " + r.from.addr)
            .toLowerCase()
            .includes(needle),
        );
      if (q("unread") === "1") list = list.filter((r) => !r.flags.seen);
      if (q("flagged") === "1") list = list.filter((r) => r.flags.flagged);
      const total = list.length;
      const before = Number(q("before") || 0);
      if (before) list = list.filter((r) => r.uid < before);
      const limit = Math.min(100, Number(q("limit") || 50));
      const page = list.slice(0, limit);
      return json({
        messages: page,
        next: list.length > limit ? page.at(-1).uid : null,
        total,
      });
    }
    if (what === "message") {
      const r = (box[q("folder")] ?? []).find(
        (x) => x.uid === Number(q("uid")),
      );
      if (!r) return fail(404, "mail_not_found");
      r.flags.seen = true;
      const bank = r.uid === 1000 && q("folder") === "INBOX";
      return json({
        uid: r.uid,
        folder: q("folder"),
        from: r.from,
        to: [
          { name: "Alex", addr: "alex@gmx.example" },
          { name: "Kim Lang", addr: "kim@example.org" },
          { name: "", addr: "familie@example.org" },
        ],
        cc: [{ name: "Ben Muster", addr: "ben@example.net" }],
        replyTo: [],
        subject: r.subject,
        date: r.date,
        messageId: `<${r.uid}@fixture.example>`,
        inReplyTo: "",
        references: [],
        flags: r.flags,
        text: bank
          ? "Guten Tag, Ihr Auszug für November liegt bei."
          : `Hallo Alex,\n\nhier der Text zu „${r.subject}“. Mehr unter https://example.org/info?id=${r.uid}\n\nViele Grüße`,
        html: bank ? bankHtml(q("images") === "1") : "",
        externalImages: bank && q("images") !== "1",
        attachments: r.attachments
          ? [
              {
                part: "2",
                name: "Kontoauszug_November_2026_Girokonto_DE12345678901234567890.pdf",
                type: "application/pdf",
                size: 182344,
                inline: false,
                cid: "",
              },
              ...(bank
                ? [
                    {
                      part: "3",
                      name: "logo.png",
                      type: "image/png",
                      size: 70,
                      inline: true,
                      cid: "logo@bank",
                    },
                  ]
                : []),
            ]
          : [],
        truncated: false,
      });
    }
    if (what === "part")
      return route.fulfill({
        status: 200,
        headers: {
          "Content-Type": "application/pdf",
          "Content-Disposition": 'attachment; filename="Kontoauszug.pdf"',
          "X-Content-Type-Options": "nosniff",
          "Content-Security-Policy": "sandbox",
        },
        body: "%PDF-1.4 fixture",
      });
    if (what === "cid")
      return route.fulfill({
        status: 200,
        contentType: "image/png",
        body: Buffer.from(PNG, "base64"),
      });
    if (what === "part/save") {
      s.saved.push(body);
      return json({ path: `${body.path}/Kontoauszug.pdf` });
    }
    if (what === "flags") {
      for (const r of box[body.folder] ?? [])
        if (body.uids.includes(r.uid)) {
          for (const f of body.add) r.flags[f] = true;
          for (const f of body.remove) r.flags[f] = false;
        }
      return route.fulfill({ status: 204 });
    }
    if (what === "move" || what === "delete") {
      const from = box[body.folder] ?? [];
      const moving = from.filter((r) => body.uids.includes(r.uid));
      box[body.folder] = from.filter((r) => !body.uids.includes(r.uid));
      const trash = s.folders[id].find((f) => f.role === "trash")?.path;
      const to =
        what === "move" ? body.to : body.folder === trash ? null : trash;
      if (to) box[to] = [...moving, ...(box[to] ?? [])];
      return route.fulfill({ status: 204 });
    }
    if (what === "send" || what === "draft") {
      const sent = multipart(request.postDataBuffer()?.toString("utf8"));
      if (what === "draft") {
        s.drafts.push(sent);
        return json({ uid: s.nextUid++ });
      }
      if (s.failSend || /FEHLER/.test(sent.meta?.subject ?? ""))
        return fail(502, "mail_send_failed", "550 5.1.1 Mailbox unavailable");
      s.sent.push(sent);
      return route.fulfill({ status: 204 });
    }
    return fail(404, "not_found");
  });
}
