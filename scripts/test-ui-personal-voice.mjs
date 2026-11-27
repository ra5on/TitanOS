// Local, synthetic audio through the product microphone, chat and playback UI.
import assert from "node:assert/strict";
import { mkdir, readFile } from "node:fs/promises";
import { join } from "node:path";
import { fixtureFactory } from "./test-ui-fixtures.mjs";

const origin = new URL(process.env.UI_URL || "http://127.0.0.1:5393");
assert.ok(["localhost", "127.0.0.1", "[::1]"].includes(origin.hostname));
const engine = process.env.UI_ENGINE || "chromium";
const pw = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const browser = await pw[engine].launch({
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const errors = [];
const { ctx, page } = await fixtureFactory(browser, errors)(
  "owner",
  "graphit",
  390,
  "de",
  {
    viewport: { width: 390, height: 844 },
    isMobile: true,
    hasTouch: true,
    nativeShell: true,
    reducedMotion: "reduce",
    serviceWorkers: "block",
  },
);
const tools = [
  "agent.example",
  "chat.ask",
  "speech.transcribe",
  "speech.speak",
];
const jobs = [];
let enabled = false,
  turns = 0,
  plays = 0,
  grants = 0;
const caps = () =>
  tools.map((id) => ({
    id,
    title: id,
    app: "agent",
    sensitive: false,
    ceiling: "auto",
    effective:
      !enabled && id.startsWith("speech.")
        ? "never"
        : id === "chat.ask"
          ? "ask"
          : "auto",
    reason: !enabled && id.startsWith("speech.") ? "app" : "",
  }));
const sample = Buffer.alloc(44 + 32000 * 2);
sample.write("RIFF");
sample.writeUInt32LE(sample.length - 8, 4);
sample.write("WAVEfmt ", 8);
sample.writeUInt32LE(16, 16);
sample.writeUInt16LE(1, 20);
sample.writeUInt16LE(1, 22);
sample.writeUInt32LE(16000, 24);
sample.writeUInt32LE(32000, 28);
sample.writeUInt16LE(2, 32);
sample.writeUInt16LE(16, 34);
sample.write("data", 36);
sample.writeUInt32LE(sample.length - 44, 40);
for (let i = 0; i < 32000; i++)
  sample.writeInt16LE(
    Math.round(7000 * Math.sin((2 * Math.PI * 360 * i) / 16000)),
    44 + i * 2,
  );

const replyAudio = process.env.UI_VOICE_WAV
  ? await readFile(process.env.UI_VOICE_WAV)
  : sample;
assert.equal(replyAudio.toString("ascii", 0, 4), "RIFF");
await ctx.addInitScript(() => {
  window.voiceFixture = { count: 0, tracks: [], deny: false, calls: 0 };
  const realFetch = window.fetch;
  window.fetch = async (...args) => {
    const [target, init] = args;
    if (
      String(target).includes("/personal-agent/voice?") &&
      init?.body instanceof Blob
    ) {
      const bytes = await init.body.arrayBuffer();
      window.voiceFixture.wav = {
        magic: new TextDecoder().decode(bytes.slice(0, 4)),
        length: bytes.byteLength,
      };
    }
    return realFetch(...args);
  };
  const device = navigator.mediaDevices;
  Object.defineProperty(navigator, "mediaDevices", {
    configurable: true,
    value: device,
  });
  Object.defineProperty(device, "getUserMedia", {
    configurable: true,
    value: async () => {
      window.voiceFixture.calls++;
      if (window.voiceFixture.deny)
        throw new DOMException("Microphone not allowed", "NotAllowedError");
      const ctx = new AudioContext();
      await ctx.resume();
      const dest = ctx.createMediaStreamDestination(),
        source = ctx.createOscillator();
      source.connect(dest);
      source.start();
      source.stop(ctx.currentTime + 0.6);
      window.voiceFixture.count++;
      window.voiceFixture.tracks.push(...dest.stream.getAudioTracks());
      return dest.stream;
    },
  });
});
await ctx.route("**/api/v1/services/state", (r) =>
  r.fulfill({ json: { agent: true } }),
);
await ctx.route("**/api/v1/personal-agent/**", async (r) => {
  const { pathname: p } = new URL(r.request().url());
  const json = (v) => r.fulfill({ json: v });
  if (p.endsWith("/model/readiness"))
    return json({ state: "ready", model: "Testmodell" });
  if (p.endsWith("/state"))
    return json({
      chats: jobs.length
        ? [{ id: "voice-chat", title: "Sprachtest", created: jobs[0].created }]
        : [],
      jobs,
      capabilities: caps(),
      settings: {
        rights: {},
        colors: {},
        theme: "system",
        paused: false,
        dotSize: 10,
        motion: "off",
      },
    });
  if (p.endsWith("/voice")) {
    assert.equal(r.request().headers()["x-titanos-use-recording"], "yes");
    const body = r.request().postDataBuffer();
    if (body) assert.equal(body.subarray(0, 4).toString(), "RIFF");
    else {
      // WebKit's interception protocol omits binary POST bodies.
      const wav = await page.evaluate(() => window.voiceFixture.wav);
      assert.equal(wav.magic, "RIFF");
      assert.ok(wav.length > 44);
    }
    turns++;
    return json({ text: "Welche Summe ergibt der Test " + turns + "?" });
  }
  if (p.endsWith("/jobs")) {
    const request = r.request().postDataJSON();
    assert.equal(request.tool, "chat.ask");
    assert.equal(request.sources, undefined);
    if (jobs.length) assert.equal(request.chatId, "voice-chat");
    const id = String(jobs.length + 1),
      created = new Date().toISOString();
    const job = {
      id,
      phase: "permission",
      status: "waiting",
      created,
      updated: created,
      request: { ...request, chatId: "voice-chat" },
      token: "test-" + id,
    };
    jobs.push(job);
    return json(job);
  }
  const job = jobs.find((j) => p.split("/")[5] === j.id);
  if (job && p.endsWith("/approve")) {
    assert.equal(r.request().postDataJSON().token, job.token);
    job.phase = "complete";
    job.status = "done";
    job.result = { title: "Lokale Antwort", text: "Das Ergebnis ist fünf." };
    grants++;
    return json(job);
  }
  if (job && p.endsWith("/speech")) {
    assert.equal(r.request().headers()["x-titanos-play-reply"], "yes");
    plays++;
    return r.fulfill({ body: replyAudio, contentType: "audio/wav" });
  }
  if (job) return json(job);
  return json({});
});
const phase = (id) =>
  page.locator('[data-voice-phase="' + id + '"]').waitFor({ timeout: 20000 });
async function shot(name) {
  if (!process.env.UI_SHOTS) return;
  await mkdir(process.env.UI_SHOTS, { recursive: true });
  await page.screenshot({
    path: join(process.env.UI_SHOTS, engine + "-390-sprache-" + name + ".png"),
  });
}
const mic = page.getByRole("button", { name: "Spracheingabe", exact: true });
const goto = () => page.goto(new URL("/agent", origin).href);
try {
  await goto();
  await mic.click();
  await page
    .getByRole("button", { name: "Sprachdienste einrichten" })
    .waitFor();
  assert.equal(await page.evaluate(() => window.voiceFixture.count), 0);
  await shot("einrichtung");
  enabled = true;
  await goto();
  await mic.click();
  await page.evaluate(() => (window.voiceFixture.deny = true));
  await page.getByRole("button", { name: "Gespräch starten" }).click();
  await page
    .getByRole("alert")
    .filter({ hasText: "Browser erlaubt keinen Mikrofonzugriff" })
    .waitFor();
  await page.evaluate(() => (window.voiceFixture.deny = false));
  await page.getByRole("button", { name: "Gespräch starten" }).click();
  await phase("listening");
  await shot("hoert");
  await phase("permission");
  assert.equal(plays, 0);
  await page
    .getByRole("button", { name: "Lokalen Chat einmal erlauben" })
    .click();
  await phase("speaking");
  await shot("antwortet");
  await phase("listening");
  await phase("permission");
  assert.equal(jobs.length, 2);
  await page
    .getByRole("button", { name: "Lokalen Chat einmal erlauben" })
    .click();
  await phase("speaking");
  await page.getByRole("button", { name: "Beenden und zum Chat" }).click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  assert.equal(
    await page.evaluate(() =>
      window.voiceFixture.tracks.every((t) => t.readyState === "ended"),
    ),
    true,
  );
  await page
    .locator(".pa-conversation")
    .getByText("Das Ergebnis ist fünf.", { exact: true })
    .first()
    .waitFor();
  assert.deepEqual([turns, plays, grants], [2, 2, 2]);
  await mic.click();
  await page.getByRole("button", { name: "Diktat", exact: true }).click();
  await page.getByRole("button", { name: "Aufnahme starten" }).click();
  await phase("listening");
  await page.waitForTimeout(700);
  await page.getByRole("button", { name: "Aufnahme beenden" }).click();
  await phase("recorded");
  await page.getByRole("button", { name: "Aufnahme übernehmen" }).click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  assert.match(await page.locator(".pa-shell textarea").inputValue(), /Test 3/);
  assert.equal(jobs.length, 2);
  assert.equal(plays, 2);
  assert.deepEqual(errors, []);
  console.log(
    engine +
      ": Zwei Sprachbeiträge in einem Chat, konkrete Chat-Freigaben, zwei lokale Audioantworten, Stopp, Diktat und fehlender Zugriff geprüft.",
  );
} catch (error) {
  console.error(
    await page.getByRole("dialog").allInnerTexts(),
    await page.evaluate(() => ({
      secure: isSecureContext,
      media: !!navigator.mediaDevices,
      recorder: !!window.MediaRecorder,
      audio: !!window.AudioContext,
      tracks: window.voiceFixture?.count,
      calls: window.voiceFixture?.calls,
    })),
    errors,
  );
  throw error;
} finally {
  await ctx.close();
  await browser.close();
}
