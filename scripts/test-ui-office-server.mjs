// Serve the UI and the catalog editors from one disposable development titand.
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import { once } from "node:events";
import { mkdtemp, rm } from "node:fs/promises";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { setTimeout as delay } from "node:timers/promises";

export async function startOfficeEditorServer(request, ids) {
  const binary = process.env.TITAND_BIN,
    packages = process.env.UI_OFFICE_PACKAGES;
  assert.ok(binary && packages, "Set TITAND_BIN and UI_OFFICE_PACKAGES");
  assert.notEqual(process.getuid?.(), 0, "Use an unprivileged test account");
  const socket = createServer();
  socket.listen(0, "127.0.0.1");
  await once(socket, "listening");
  const address = "127.0.0.1:" + socket.address().port;
  await new Promise((done) => socket.close(done));
  const origin = "http://" + address;
  const root = await mkdtemp(join(tmpdir(), "titanos-editor-test-"));
  const daemon = spawn(
    binary,
    ["-dev", "-dev-listen", address, "-config", root, "-webapps-dir", packages],
    { stdio: "ignore" },
  );
  const ended = once(daemon, "exit").catch(() => {});
  const api = await request.newContext({
    baseURL: origin,
    extraHTTPHeaders: { Origin: origin },
    timeout: 10000,
  });
  async function close() {
    if (daemon.exitCode === null && !daemon.killed) daemon.kill("SIGTERM");
    await ended;
    await api.dispose();
    await rm(root, { recursive: true, force: true });
  }
  try {
    for (let i = 0; i < 100; i++) {
      if (daemon.exitCode !== null) throw Error("Development daemon exited");
      try {
        if ((await api.get("/api/v1/session")).ok()) break;
      } catch {}
      await delay(150);
    }
    const user = {
      name: "officetest",
      password: randomBytes(24).toString("hex"),
    };
    for (const path of ["setup", "login"]) {
      const response = await api.post("/api/v1/" + path, { data: user });
      assert.ok(response.ok(), path + " returned " + response.status());
    }
    const install = await api.post("/api/v1/webapps/defaults/install", {
      data: { apps: ids },
    });
    assert.equal(install.status(), 202);
    for (let attempt = 0; attempt < 120; attempt++) {
      const res = await api.get("/api/v1/webapps");
      assert.ok(res.ok(), "Unable to read installed editor catalog");
      const catalog = await res.json();
      const apps = catalog.apps.filter((app) => ids.includes(app.id));
      assert.equal(apps.length, ids.length);
      for (const app of apps) assert.ok(!app.error, app.id + ": " + app.error);
      if (apps.every((app) => app.installed && app.state === "ready")) {
        return { origin, catalog, close };
      }
      await delay(500);
    }
    throw Error("Editors did not finish installing in the disposable daemon");
  } catch (err) {
    await close();
    throw err;
  }
}
