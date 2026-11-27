"""Native PIM acceptance hooks for the two-account, offline upgrade probe."""
import hashlib
import json
import secrets
import subprocess
from pathlib import Path


API = "/api/v1"
MAIL = API + "/mail"
EVENTS = API + "/calendar/events"
PANEL = API + "/agent/pim-chat"
CHATS = API + "/agent/chats"
AGENT = API + "/personal-agent"
ARCHIVE = Path("/mnt/testdaten/.titanos/agent")


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def before(clients):
    """Create different native records and an actual old panel pointer per account."""
    evidence = {}
    for name, client in clients.items():
        owner = clients["systemtester"]
        owner.request(API + "/users/" + name + "/mail", {"enabled": True}, "PUT")
        assert client.request(MAIL + "/access")["enabled"]
        address = name + "@example.invalid"
        mailbox = client.request(MAIL + "/accounts", {
            "name": "Privates Testpostfach " + name,
            "email": address, "user": address, "password": secrets.token_urlsafe(20),
            "imap": {"host": "imap.example.invalid", "port": 993, "security": "tls"},
            "smtp": {"host": "smtp.example.invalid", "port": 465, "security": "tls"},
            "notify": False,
        })
        assert mailbox["email"] == address and mailbox["passwordSet"]
        signature = "Testsignatur " + name
        settings = client.request(MAIL + "/settings")
        settings["signatures"].append({"name": name, "text": signature, "default": [mailbox["id"]]})
        client.request(MAIL + "/settings", settings, "PUT")
        event = client.request(EVENTS, {"calendar": "privat", "event": {
            "title": "Persönlicher Testtermin " + name,
            "start": "2026-11-20T10:00", "end": "2026-11-20T10:45", "timezone": "UTC",
        }})
        native = client.request(EVENTS + "/privat/" + event["id"])
        assert native["title"].endswith(name) and native["etag"] == event["etag"]
        marker = "Alter gemeinsamer PIM-Chat " + name
        chat = client.request(CHATS, {"draft": True})["id"]
        client.request(CHATS + "/" + chat, {"title": marker}, "PATCH")
        client.request(PANEL, {"chat": chat, "cloudNoted": True}, "PUT")
        assert client.request(PANEL)["chat"] == chat
        pointer = ARCHIVE / name / "pimchat.json"
        assert json.loads(pointer.read_bytes())["chat"] == chat
        archived = [pointer, ARCHIVE / name / "chats" / (chat + ".json")]
        evidence[name] = {
            "mailbox": mailbox["id"], "email": address, "signature": signature,
            "event": native, "chat": chat, "marker": marker,
            "archive": {str(path): sha(path) for path in archived},
        }
    assert len(evidence) == 2 and len({v["chat"] for v in evidence.values()}) == 2
    print("PASS old binary: two native mail identities, two calendar events and two distinct persistent PIM panel pointers", flush=True)
    return evidence


def after(clients, evidence):
    """Use the native resources again and round-trip a fresh selection for each owner."""
    for name, client in clients.items():
        data = evidence[name]
        other = next(row for owner, row in evidence.items() if owner != name)
        assert client.request(MAIL + "/access")["enabled"]
        mailboxes = client.request(MAIL + "/accounts")
        assert {box["id"] for box in mailboxes} == {data["mailbox"]}
        assert mailboxes[0]["email"] == data["email"] and mailboxes[0]["passwordSet"]
        settings = client.request(MAIL + "/settings")
        assert [s["text"] for s in settings["signatures"]] == [data["signature"]]
        assert settings["signatures"][0]["default"] == [data["mailbox"]]
        settings["signatures"][0]["text"] += " (aktualisiert)"
        client.request(MAIL + "/settings", settings, "PUT")
        assert client.request(MAIL + "/settings")["signatures"] == settings["signatures"]
        event = data["event"]
        url = EVENTS + "/privat/" + event["id"]
        assert client.request(url) == event
        listing = client.request(EVENTS + "?from=2026-11-19&to=2026-11-22")
        assert {e["id"] for e in listing["events"]} == {event["id"]}
        client.expect(404, EVENTS + "/privat/" + other["event"]["id"])
        updated_title = event["title"] + " (aktualisiert)"
        updated = client.request(url, {"scope": "all", "etag": event["etag"], "event": {"title": updated_title}}, "PUT")
        saved = client.request(url)
        assert saved["title"] == updated_title and saved["etag"] == updated["etag"]
        client.expect(404, PANEL)
        client.expect(404, PANEL, {"chat": data["chat"]}, "PUT")
        client.expect(404, CHATS + "/" + data["chat"])
        state = json.dumps(client.request(AGENT + "/state"), ensure_ascii=False)
        for old in evidence.values():
            assert old["chat"] not in state and old["marker"] not in state
        source = {"kind": "calendar", "label": updated_title, "id": "privat/" + event["id"],
                  "ref": {"collection": "privat", "object": event["id"]}}
        selection = {"sources": [source]}
        context = client.request(AGENT + "/contexts", selection)["id"]
        consume = AGENT + "/contexts/" + context + "/consume"
        next(c for owner, c in clients.items() if owner != name).expect(404, consume, {})
        assert client.request(consume, {}) == selection
        client.expect(404, consume, {})
    for data in evidence.values():
        assert all(sha(Path(path)) == digest for path, digest in data["archive"].items())
    print("PASS candidate: native mail identities and signatures survived; both calendar events read and edited by their owner; old panel routes retired and archives unchanged; fresh context consumed once by its own account", flush=True)
    if Path("/tools/test-ui-upgrade-pim.mjs").is_file():
        sessions = {}
        for name, client in clients.items():
            jar = next(h.cookiejar for h in client.opener.handlers if hasattr(h, "cookiejar"))
            sessions[name] = {"cookies": [{"name": c.name, "value": c.value} for c in jar],
                              "marker": evidence[name]["marker"]}
        subprocess.run(["/tools/node", "/tools/test-ui-upgrade-pim.mjs"],
                       input=json.dumps(sessions), text=True, check=True)
