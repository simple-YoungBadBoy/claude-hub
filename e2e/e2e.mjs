// End-to-end test: starts the real server binary against a private tmux
// server and a fake `claude`, then drives the UI in headless Chromium.
//
//   node e2e/e2e.mjs            (expects bin/claude-hub to be built)
//   E2E_HEADED=1 node e2e/e2e.mjs
//
// Screenshots land in e2e/out/ — look at them when the UI changed.
import { spawn, execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, mkdirSync, writeFileSync, existsSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const bin = join(root, "bin", "claude-hub");
const out = join(root, "e2e", "out");
mkdirSync(out, { recursive: true });
if (!existsSync(bin)) throw new Error("build first: ./build.sh");

const data = mkdtempSync(join(tmpdir(), "claude-hub-e2e-"));
const socket = `claude-hub-e2e-${process.pid}`;
const port = 20000 + (process.pid % 20000);
const base = `http://127.0.0.1:${port}`;
// named `claude` so tmux reports it as the pane command, like the real thing
const fake = join(root, "e2e", "bin", "claude");

const tmux = (...args) => execFileSync("tmux", ["-L", socket, ...args], { encoding: "utf8" });
const screen = (name) => tmux("capture-pane", "-p", "-t", `=${name}:`);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

let step = "";
const t0 = Date.now();
function log(msg) {
  step = msg;
  console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${msg}`);
}
function assert(cond, msg) {
  if (!cond) throw new Error(`assertion failed: ${msg}`);
}
async function until(fn, msg, timeout = 15000) {
  const end = Date.now() + timeout;
  let last;
  while (Date.now() < end) {
    try {
      last = await fn();
      if (last) return last;
    } catch (e) {
      last = e;
    }
    await sleep(200);
  }
  throw new Error(`timed out: ${msg} (last: ${last})`);
}

// a throwaway Claude config dir: hooks install must never touch the real one
const claudeConfig = mkdtempSync(join(tmpdir(), "claude-hub-e2e-config-"));
const env = {
  ...process.env,
  CLAUDE_CONFIG_DIR: claudeConfig,
  CLAUDE_HUB_URL: base,
  CLAUDE_HUB_DATA: data,
  CLAUDE_HUB_BIN: bin,
};
delete env.TMUX;
const server = spawn(bin, ["-addr", `127.0.0.1:${port}`, "-data", data, "-tmux-socket", socket, "-cmd", fake], {
  stdio: ["ignore", "pipe", "pipe"],
  env,
});
let serverLog = "";
server.stdout.on("data", (d) => (serverLog += d));
server.stderr.on("data", (d) => (serverLog += d));

let browser;
let page;
try {
  log("server starts");
  await until(async () => (await fetch(`${base}/api/version`)).ok, "server up");
  const token = readFileSync(join(data, "token"), "utf8").trim();
  const api = async (path, body) => {
    const r = await fetch(base + path, {
      method: body ? "POST" : "GET",
      headers: { Authorization: `Bearer ${token}`, "X-Hub": "1", "Content-Type": "application/json" },
      body: body ? JSON.stringify(body) : undefined,
    });
    return r.json();
  };

  log("auth: API rejects requests without token / without X-Hub");
  assert((await fetch(`${base}/api/state`)).status === 401, "no token → 401");
  const noHeader = await fetch(`${base}/api/poker/start`, { method: "POST", headers: { Authorization: `Bearer ${token}` } });
  assert(noHeader.status === 403, "no X-Hub → 403");

  // E2E_CHROME: use an installed Chrome/Chromium instead of Playwright's download (CI)
  browser = await chromium.launch({ headless: !process.env.E2E_HEADED, executablePath: process.env.E2E_CHROME || undefined });
  page = await browser.newPage({ viewport: { width: 1280, height: 800 }, permissions: ["clipboard-read", "clipboard-write"] });
  const pageErrors = [];
  page.on("pageerror", (e) => pageErrors.push(String(e)));

  log("login");
  await page.goto(base);
  await page.fill("input[type=password]", "wrong");
  await page.click("button.primary");
  await page.waitForSelector(".error");
  await page.fill("input[type=password]", token);
  await page.click("button.primary");
  await page.waitForSelector(".new-btn");

  log("new conversation opens a tmux session running claude");
  await page.click(".new-btn");
  await page.fill(".modal input >> nth=0", "e2e 测试");
  await page.fill(".modal input >> nth=1", data);
  await page.click(".modal button.primary");
  const name = "cc-e2e-测试";
  await until(() => tmux("ls").includes(name), "tmux session created");
  await page.waitForSelector(`.session.active >> text=${name}`);
  await until(() => screen(name).includes("fake claude ready"), "fake claude started");
  await until(async () => (await page.locator(".conn-open").count()) > 0, "terminal websocket open");

  log("typing in the terminal reaches the session");
  await page.click(".term-box");
  await page.keyboard.type("hello hub");
  await page.keyboard.press("Enter");
  await until(() => screen(name).includes("got: hello hub"), "input echoed");

  log("input bar: Enter sends, an IME's Enter doesn't, Shift+Enter is a newline, slash commands stay typed");
  const bar = page.locator(".composer textarea");
  await bar.click();
  await page.keyboard.type("bar 你好");
  await bar.evaluate((el) => el.dispatchEvent(new CompositionEvent("compositionstart", { bubbles: true }))); // choosing characters
  await page.keyboard.press("Enter");
  assert((await bar.inputValue()).startsWith("bar 你好"), "Enter during IME composition is the IME's");
  await bar.evaluate((el) => el.dispatchEvent(new CompositionEvent("compositionend", { bubbles: true })));
  await page.waitForTimeout(150);
  await bar.fill("bar 你好");
  await page.keyboard.press("Enter");
  await until(() => screen(name).includes("got: bar 你好"), "input bar sent the line");
  assert((await bar.inputValue()) === "", "input bar cleared after send");
  await page.keyboard.type("/compact now");
  await page.keyboard.press("Enter");
  await until(() => screen(name).includes("got: /compact now"), "slash command typed, not pasted");
  await page.keyboard.type("first line");
  await page.keyboard.press("Shift+Enter");
  await page.keyboard.type("second line");
  assert((await bar.inputValue()) === "first line\nsecond line", "Shift+Enter adds a newline");
  await page.keyboard.press("Enter");
  await until(() => screen(name).includes("got: second line"), "multi-line message delivered");
  await page.click(".term-box");

  log("copy in Claude (tmux load-buffer -w → OSC 52) reaches the local clipboard");
  await page.keyboard.type("copy");
  await page.keyboard.press("Enter");
  await until(() => screen(name).includes("Copied to clipboard"), "fake claude copied");
  await page.waitForSelector('.toast:has-text("已复制"), .copy-later', { timeout: 10000 });
  if (await page.locator(".copy-later").count()) await page.click(".copy-later"); // browser refused the async write
  await until(async () => (await page.evaluate(() => navigator.clipboard.readText())) === "hub copy 复制测试", "clipboard has the copied text");
  await page.click(".term-box");

  log("a tmux session started on the server itself shows up under 其他 tmux 会话");
  tmux("new-session", "-d", "-s", "server-side", "-c", data, "sleep 300");
  await page.waitForSelector('.session-group:has-text("其他 tmux 会话（1）")');
  await page.waitForSelector('.session:has-text("server-side") .dot.other');
  tmux("kill-session", "-t", "=server-side");
  await page.waitForSelector(".session-group", { state: "detached" });

  log("image upload is pasted into the session");
  const png = Buffer.from(
    "89504e470d0a1a0a0000000d4948445200000001000000010806000000" + "1f15c4890000000d49444154789c6360f8cfc0f01f0005000201" + "e5274fd50000000049454e44ae426082",
    "hex",
  );
  await page.setInputFiles(".term-bar input[type=file]", { name: "shot.png", mimeType: "image/png", buffer: png });
  await page.waitForSelector(".toast >> text=已插入");
  const uploads = join(data, "uploads", name);
  await until(() => screen(name).replace(/\s+/g, "").includes(uploads.replace(/\s+/g, "")), "upload path pasted");
  await page.keyboard.press("Enter");

  log("working → idle is detected, pet and chips react");
  const before = await api("/api/state");
  await page.keyboard.type("work");
  await page.keyboard.press("Enter");
  await page.waitForSelector(`.session:has-text("${name}") .dot.working`, { timeout: 10000 });
  await page.screenshot({ path: join(out, "1-working.png") });
  await page.waitForSelector(`.session:has-text("${name}") .dot.idle`, { timeout: 20000 });
  const after = await until(async () => {
    const s = await api("/api/state");
    return s.pet.tasksDone > before.pet.tasksDone && s;
  }, "pet counted the finished task");
  assert(after.poker.stats.chips === before.poker.stats.chips + 10, "task reward +10 chips");

  log("before hooks: the sidebar suggests installing them");
  await page.waitForSelector(".hooks-hint");

  log("hooks: activity, context bar, and hint goes away");
  await page.click(".term-box");
  await page.keyboard.type("hwork");
  await page.keyboard.press("Enter");
  await page.waitForSelector(`.session:has-text("${name}") .session-sub:has-text("运行 Bash")`, { timeout: 10000 });
  assert((await page.locator(".hooks-hint").count()) === 0, "hint hidden once hooks report");
  await page.waitForSelector(`.session:has-text("${name}") .dot.idle`, { timeout: 15000 });
  await page.waitForSelector(`.session:has-text("${name}") .ctx-high:has-text("85%")`, { timeout: 5000 });
  log("summary: AI title and Claude's last words in the sidebar");
  await page.waitForSelector(`.session:has-text("${name}") .session-title:has-text("假任务标题")`, { timeout: 5000 });
  await page.waitForSelector(`.session:has-text("${name}") .session-last:has-text("做完了：假回复")`);
  await page.screenshot({ path: join(out, "1b-hooks-context.png") });

  log("hooks: permission prompt → waiting, worried pet, then back to idle");
  await page.keyboard.type("hask");
  await page.keyboard.press("Enter");
  await page.waitForSelector(`.session:has-text("${name}") .dot.waiting`, { timeout: 10000 });
  await page.waitForSelector(`.session:has-text("${name}") .session-sub:has-text("等你确认：Bash")`);
  await page.waitForSelector(".mini-pet .mood-worried");
  assert((await api("/api/state")).pet.message.includes("在等你确认"), "pet reacts to waiting");
  log("approval card: question, command, options; a tap sends the option's number");
  await page.waitForSelector('.prompt-card .prompt-q:has-text("Do you want to proceed?")', { timeout: 5000 });
  assert((await page.locator(".prompt-detail").innerText()).includes("touch hello.txt"), "card shows the command");
  await page.waitForSelector('.prompt-opts button:has-text("allow access to /tmp/x from this project")'); // wrapped label joined
  assert((await page.locator(".prompt-opts button").count()) === 3, "three options");
  await page.screenshot({ path: join(out, "1c-waiting.png") });
  await page.click('.prompt-opts button:has-text("No")');
  await page.waitForSelector(".prompt-card", { state: "detached", timeout: 5000 });
  await page.waitForSelector(`.session:has-text("${name}") .dot.idle`, { timeout: 10000 });
  assert(screen(name).includes("answered: 3"), "option number reached the session");

  log("hooks CLI: install/uninstall into the (temporary) Claude config");
  const hooksCli = (...a) => execFileSync(bin, ["hooks", ...a], { env, encoding: "utf8" });
  writeFileSync(join(claudeConfig, "settings.json"), JSON.stringify({ model: "opus", hooks: { Stop: [{ hooks: [{ type: "command", command: "say done" }] }] } }));
  hooksCli("install");
  assert(hooksCli("status").includes("11/11"), "all hook events installed");
  const installed = readFileSync(join(claudeConfig, "settings.json"), "utf8");
  assert(installed.includes("say done") && installed.includes('"model": "opus"'), "user settings kept");
  hooksCli("uninstall");
  const removed = readFileSync(join(claudeConfig, "settings.json"), "utf8");
  assert(!removed.includes(" hook\"") && removed.includes("say done"), "uninstall removes only ours");

  log("rename keeps the session");
  await page.dblclick(`.session-name >> text=${name}`);
  await page.fill(".session-main input", "renamed");
  await page.keyboard.press("Enter");
  await until(() => tmux("ls").includes("renamed:"), "renamed in tmux");
  await page.waitForSelector(".session >> text=renamed");

  log("poker: play hands to showdown");
  await page.click('nav.tabs button:has-text("德州")');
  for (let hand = 0; hand < 3; hand++) {
    await page.click('.actions button.primary:has-text("开始"), .actions button.primary:has-text("下一手")');
    // bots "think" 0.7–1.6s per action, so a hand can take ~30s
    for (const end = Date.now() + 90000; Date.now() < end; ) {
      const v = (await api("/api/state")).poker;
      if (v.street === "showdown") break;
      if (v.toAct === 0 && v.legal) {
        // check when free, otherwise fold: never risks busting the stack
        const btn = v.legal.canCheck ? "过牌" : "弃牌";
        await page.click(`.actions button:has-text("${btn}")`);
      }
      await sleep(400);
    }
    const v = (await api("/api/state")).poker;
    assert(v.street === "showdown", `hand ${hand} finished`);
    const sum = v.players.reduce((a, p) => a + p.chips, 0);
    assert(sum > 0, "chips present");
  }
  await page.screenshot({ path: join(out, "2-poker.png") });

  log("usage: today's tokens from transcripts, duplicates counted once");
  {
    const dir = join(claudeConfig, "projects", "-tmp-e2e-proj");
    mkdirSync(dir, { recursive: true });
    const line = (id) =>
      JSON.stringify({
        type: "assistant", timestamp: new Date().toISOString(), requestId: `req_${id}`, cwd: "/tmp/e2e-proj",
        message: { id: `msg_${id}`, model: "claude-e2e", usage: { input_tokens: 1000, output_tokens: 2000, cache_read_input_tokens: 3000, cache_creation_input_tokens: 4000 } },
      }) + "\n";
    // the same response twice (one line per content block), plus another one
    writeFileSync(join(dir, "s.jsonl"), line("a") + line("a") + line("b"));
    await page.click('.tabs button:has-text("用量")');
    await page.waitForSelector('.usage-card.big b:has-text("20.0k")');
    await page.waitForSelector('.usage-table td:has-text("claude-e2e")');
    await page.waitForSelector('.usage-table td:has-text("/tmp/e2e-proj")');
    const u = await api("/api/usage");
    assert(u.today.messages === 2 && u.today.output === 4000, `usage de-duplicated: ${JSON.stringify(u.today)}`);
    assert(u.days.length === 7, "7-day series");
    await page.click('.seg button:has-text("输出")');
    await page.waitForSelector('.usage-day.today .usage-day-val:has-text("4.0k")');
    await page.screenshot({ path: join(out, "4-usage.png") });
  }

  log("pet: feed spends chips");
  await page.click('nav.tabs button:has-text("小克")');
  const chips0 = (await api("/api/state")).poker.stats.chips;
  await page.click('.shop-item:has-text("小鱼干")');
  await until(async () => (await api("/api/state")).poker.stats.chips === chips0 - 20, "fish costs 20");
  await page.screenshot({ path: join(out, "3-pet.png") });

  log("iPhone: PWA files, bottom tabs, drawer, key bar and composer");
  {
    const man = await fetch(`${base}/manifest.webmanifest`);
    assert(man.ok && man.headers.get("content-type").includes("manifest+json"), "manifest served");
    assert((await man.json()).display === "standalone", "standalone PWA");
    const icon = await fetch(`${base}/apple-touch-icon.png`);
    assert(icon.ok && icon.headers.get("content-type") === "image/png", "apple touch icon");
    assert(icon.headers.get("cache-control") === "no-cache", "icons are revalidated, not cached forever");

    const phone = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, deviceScaleFactor: 2 });
    await phone.addCookies([{ name: "claudehub", value: token, url: base }]);
    const m = await phone.newPage();
    m.on("pageerror", (e) => pageErrors.push("phone: " + String(e)));
    await m.goto(base);
    await m.waitForSelector(".menu-btn");
    const nav = await m.locator("nav.tabs").boundingBox();
    assert(nav.y > 700, `tab bar at the bottom (y=${nav.y})`);
    assert(!(await m.locator(".sidebar").evaluate((el) => el.getBoundingClientRect().right > 0)), "drawer hidden");
    await m.tap(".menu-btn");
    await m.waitForSelector(".sidebar.open");
    await m.tap('.session:has-text("renamed")');
    await m.waitForSelector(".sidebar.open", { state: "detached" });
    await m.waitForSelector(".conn-open");
    await m.waitForSelector(".keybar");
    await m.waitForSelector(".composer textarea"); // the input bar is on by default
    // IME text not yet reported to the page must survive a re-render
    // triggered by a hub broadcast (third-party iOS keyboards lost pinyin)
    await m.locator(".composer textarea").evaluate((el) => (el.value = "nihao"));
    await api("/api/pet/pat", {});
    await m.waitForTimeout(500);
    assert((await m.inputValue(".composer textarea")) === "nihao", "composer keeps uncommitted IME text across re-renders");
    await m.fill(".composer textarea", "from phone 手机");
    await m.locator(".composer textarea").evaluate((el) => el.addEventListener("input", (e) => (window.__clearedBy = e.inputType)));
    await m.tap('.composer button:has-text("发送")');
    await until(() => screen("renamed").includes("got: from phone 手机"), "composer text + Enter reached the session");
    assert((await m.inputValue(".composer textarea")) === "", "composer cleared after send");
    assert(String(await m.evaluate(() => window.__clearedBy)).startsWith("delete"), "composer cleared as an edit the keyboard sees (not a script value write)");
    log("composer history: refill a sent message");
    await m.tap(".composer .hist-btn");
    await m.tap('.hist button:has-text("from phone 手机")');
    assert((await m.inputValue(".composer textarea")) === "from phone 手机", "history refills the composer");
    assert(!(await m.locator(".composer button.primary").isDisabled()), "send enabled after refill");
    await m.locator(".composer textarea").evaluate((el) => (el.value = ""));
    await m.tap('.keybar button >> text="⏎"'); // exact: "⇧⏎" also contains ⏎
    await until(() => /^got:\s*$/m.test(screen("renamed")), "key bar Enter reached the session");
    assert(!screen("renamed").includes("^["), "⏎ sent a plain Enter (not Esc+Enter from a neighbouring key)");
    const kb = await m.locator(".keybar").evaluate((el) => [el.scrollWidth, el.clientWidth]);
    assert(kb[0] <= kb[1], `key bar fits without scrolling (${kb})`);
    await m.screenshot({ path: join(out, "5-phone.png") });
    await m.tap('nav.tabs button:has-text("用量")');
    await m.waitForSelector(".usage-card.big");
    await m.screenshot({ path: join(out, "5-phone-usage.png") });
    await phone.close();
  }

  log("closing a session kills it");
  page.once("dialog", (d) => d.accept());
  await page.click('nav.tabs button:has-text("对话")');
  await page.hover(".session >> text=renamed");
  await page.click('.session:has-text("renamed") .kill');
  await until(() => {
    try {
      return !tmux("ls").includes("renamed:");
    } catch {
      return true; // no sessions left → tmux server gone
    }
  }, "session killed");

  assert(pageErrors.length === 0, `no page errors: ${pageErrors.join("; ")}`);
  writeFileSync(join(out, "result.txt"), "PASS\n");
  console.log("E2E PASS");
} catch (e) {
  console.error(`E2E FAIL at step "${step}": ${e.message}`);
  await page?.screenshot({ path: join(out, "fail.png") }).catch(() => {});
  console.error(`screenshot: ${join(out, "fail.png")}`);
  console.error("--- server log ---\n" + serverLog.slice(-3000));
  writeFileSync(join(out, "result.txt"), `FAIL: ${step}: ${e.message}\n`);
  process.exitCode = 1;
} finally {
  await browser?.close();
  server.kill();
  try {
    tmux("kill-server");
  } catch {
    /* already gone */
  }
  // tmux leaves the socket file behind
  rmSync(join(process.env.TMUX_TMPDIR || "/tmp", `tmux-${process.getuid()}`, socket), { force: true });
}
