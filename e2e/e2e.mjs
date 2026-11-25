// End-to-end test: starts the real server binary against a private tmux
// server and a fake `claude`, then drives the UI in headless Chromium.
//
//   node e2e/e2e.mjs            (expects bin/claude-hub to be built)
//   E2E_HEADED=1 node e2e/e2e.mjs
//
// Screenshots land in e2e/out/ — look at them when the UI changed.
import { spawn, execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, mkdirSync, writeFileSync, appendFileSync, existsSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, join, dirname } from "node:path";
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
const server = spawn(bin, ["-addr", `127.0.0.1:${port}`, "-data", data, "-tmux-socket", socket, "-cmd", fake, "-mahjong-bot-ms", "40"], {
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

  log("📖 查看 → 对话: the transcript as chat, Markdown rendered, tools collapsed");
  const rec = (o) => JSON.stringify(o) + "\n";
  appendFileSync(
    join(data, "fake-transcript.jsonl"),
    rec({ type: "user", message: { role: "user", content: "把 README 改一下" } }) +
      rec({ type: "assistant", message: { content: [{ type: "tool_use", id: "e1", name: "Edit", input: { file_path: join(data, "README.md") } }] } }) +
      rec({ type: "user", message: { content: [{ type: "tool_result", tool_use_id: "e1", content: "README.md 已更新：第 3 行" }] } }) +
      rec({ type: "assistant", message: { content: [{ type: "text", text: "<div align=\"center\">\n## 改好了\n- 加了 **一行**\n\n| 文件 | 行 |\n|---|---|\n| README.md | 2 |\n\n[x](javascript:alert(1))" }] } }),
  );
  await page.click(".insp-toggle");
  await page.waitForSelector('.chat-msg.user:has-text("把 README 改一下")', { timeout: 5000 });
  await page.waitForSelector('.chat-msg.assistant .md-h:has-text("改好了")');
  await page.waitForSelector('.chat-msg.assistant td:has-text("README.md")');
  await page.waitForSelector('.chat-msg.assistant strong:has-text("一行")');
  assert((await page.locator(".chat a").count()) === 0, "javascript: links are not clickable");
  assert(!(await page.locator(".chat").innerText()).includes("<div"), "HTML-only lines are hidden, never rendered");
  await page.waitForSelector('.chat-tools summary:has-text("1 个工具调用")');
  await page.click('.chat-tools summary:has-text("1 个工具调用")');
  await page.click('.chat-tool-head:has-text("Edit")');
  await page.waitForSelector('.chat-out:has-text("README.md 已更新：第 3 行")');
  await page.screenshot({ path: join(out, "1d-inspect-chat.png") });
  // polling an unchanged transcript costs no re-read or download
  const chatURL = `/api/sessions/${encodeURIComponent(name)}/chat`;
  // a new conversation has no transcript file until its first prompt: empty, not an error
  const pane = (await api("/api/state")).sessions.find((x) => x.name === name).paneId;
  const hook = (tr) => api("/api/hook", { pane, event: "SessionStart", session: "fresh-" + tr.length, transcript: tr });
  await hook(join(data, "not-written-yet.jsonl"));
  const fresh = await api(chatURL);
  assert(!fresh.error && fresh.messages.length === 0, "missing transcript → empty chat: " + JSON.stringify(fresh));
  await hook(join(data, "fake-transcript.jsonl"));
  const c1 = await api(chatURL);
  assert(c1.key && c1.messages.length > 0, "chat carries a version key");
  const c2 = await api(`${chatURL}?since=${encodeURIComponent(c1.key)}`);
  assert(c2.unchanged === true && !c2.messages, "unchanged transcript → no messages sent");
  appendFileSync(join(data, "fake-transcript.jsonl"), rec({ type: "user", message: { role: "user", content: "再看一眼" } }));
  const c3 = await api(`${chatURL}?since=${encodeURIComponent(c1.key)}`);
  assert(!c3.unchanged && c3.messages.some((m) => m.text === "再看一眼"), "changed transcript → full chat again");

  log("± 改动: git changes of the session's directory, diff per file");
  const git = (...a) => execFileSync("git", ["-C", data, "-c", "user.name=e2e", "-c", "user.email=e2e@x", ...a], { encoding: "utf8" });
  writeFileSync(join(data, ".gitignore"), "*\n!.gitignore\n!README.md\n!notes/\n!notes/*\n");
  writeFileSync(join(data, "README.md"), "# 说明\n\n第一行\n");
  git("init", "-q", "-b", "main");
  git("add", ".");
  git("commit", "-q", "-m", "init");
  writeFileSync(join(data, "README.md"), "# 说明\n\n第一行\n新的一行\n");
  mkdirSync(join(data, "notes"));
  writeFileSync(join(data, "notes", "todo.md"), "- [ ] 待办\n");
  await page.click('.insp-tabs button:has-text("改动")');
  await page.waitForSelector('.git-head:has-text("main")', { timeout: 5000 });
  await page.waitForSelector('.git-row:has-text("notes/todo.md") .git-st.add');
  await page.click('.git-row:has-text("README.md")');
  await page.waitForSelector('.diff .add:has-text("+新的一行")');
  await page.click('.commit-row:has-text("init")');
  await page.waitForSelector('.commits .diff .add:has-text("+第一行")');
  assert(await page.locator('.commits-head:has-text("没有上游分支")').count(), "no upstream: nothing marked unpushed");
  await page.screenshot({ path: join(out, "1e-inspect-git.png") });
  const badSha = await api(`/api/sessions/${encodeURIComponent(name)}/commit?sha=${encodeURIComponent("HEAD~1")}`);
  assert(badSha.error, "commit diff only for a hex commit id");

  log("📁 文件: browse the directory, Markdown rendered, never outside it");
  await page.click('.insp-tabs button:has-text("文件")');
  await page.click('.dir-row:has-text("README.md")');
  await page.waitForSelector('.file-md .md-h:has-text("说明")');
  await page.click('.crumbs button:has-text("源码")');
  await page.waitForSelector('.file-text:has-text("新的一行")');
  await page.screenshot({ path: join(out, "1f-inspect-file.png") });
  const esc = await api(`/api/sessions/${encodeURIComponent(name)}/file?path=${encodeURIComponent("../../etc/passwd")}`);
  assert(esc.error && !esc.text, "file outside the session directory refused");
  const tok = await api(`/api/sessions/${encodeURIComponent(name)}/diff?path=${encodeURIComponent("--output=/tmp/x")}`);
  assert(tok.error, "diff only for files git reports");
  await page.click(".insp-close");
  await page.waitForSelector(".inspector", { state: "detached" });

  log("input bar: @ completes files of the session's folder, / completes commands");
  mkdirSync(join(data, ".claude", "commands"), { recursive: true });
  writeFileSync(join(data, ".claude", "commands", "hello.md"), "---\ndescription: 打个招呼\n---\nsay hi\n");
  await page.click(".composer textarea");
  await page.keyboard.type("看看 @no");
  await page.waitForSelector('.sugg button.on:has-text("notes/")', { timeout: 5000 });
  await page.keyboard.press("Tab"); // a directory keeps completing inside it
  await page.waitForSelector('.sugg button.on:has-text("notes/todo.md")', { timeout: 5000 });
  await page.keyboard.press("Enter"); // on a file: inserts, doesn't send
  await page.waitForSelector(".sugg", { state: "detached" });
  assert((await page.inputValue(".composer textarea")) === "看看 @notes/todo.md ", "file path inserted");
  await page.fill(".composer textarea", "");
  await page.keyboard.type("/hel");
  await page.waitForSelector('.sugg button.on:has-text("/hello"):has-text("项目 · 打个招呼")', { timeout: 5000 });
  assert(await page.locator('.sugg button:has-text("/help")').count(), "built-in commands listed too");
  await page.keyboard.press("Enter"); // on a command: runs it
  await until(() => screen(name).includes("got: /hello"), "command sent");
  assert((await page.inputValue(".composer textarea")) === "", "composer cleared");

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

  log("sidebar: sessions grouped by project, ＋ opens a new session in that project");
  await page.waitForSelector(`.project-head:has-text("${basename(data)}")`);
  await page.click(`.project-head:has-text("${basename(data)}") button`);
  assert((await page.inputValue(".modal input >> nth=1")) === data, "project ＋ prefills its directory");
  await page.click('.modal button:has-text("取消")');

  log("input bar ⚡ quick commands are typed into Claude");
  await page.click('.composer button:has-text("⚡")');
  await page.click('.hist.quick button:has-text("/compact")');
  await until(() => screen("renamed").includes("got: /compact"), "quick command delivered");

  log("new session can continue the directory's last conversation");
  const cont = await api("/api/sessions", { name: "cont", dir: data, continue: true });
  await until(() => tmux("list-panes", "-t", `=${cont.name}:`, "-F", "#{pane_start_command}").includes("--continue"), "started with --continue");
  tmux("kill-session", "-t", `=${cont.name}`);

  log("new session can pick up (or fork) one of the directory's earlier conversations");
  const pastId = "0e2e0e2e-1111-4222-8333-444455556666";
  const pastDir = join(claudeConfig, "projects", data.replace(/[^A-Za-z0-9-]/g, "-"));
  mkdirSync(pastDir, { recursive: true });
  writeFileSync(join(pastDir, `${pastId}.jsonl`), '{"type":"ai-title","aiTitle":"上次修的登录页"}\n{"type":"assistant","message":{"content":[{"type":"text","text":"登录页修好了"}]}}\n');
  await page.click(`.project-head:has-text("${basename(data)}") button`);
  await page.click('.past-row:has-text("上次修的登录页")');
  assert((await page.textContent(".past-row.on")).includes("登录页修好了"), "shows Claude's last words");
  await page.click('.past label.check input');
  await page.click('.modal button.primary:has-text("分叉")');
  await page.waitForSelector(".modal", { state: "detached" });
  const forked = (await api("/api/state")).sessions.find((s) => tmux("list-panes", "-t", `=${s.name}:`, "-F", "#{pane_start_command}").includes(`--resume ${pastId} --fork-session`));
  assert(forked, "started with --resume <id> --fork-session");
  tmux("kill-session", "-t", `=${forked.name}`);
  await page.waitForSelector(`.session-name >> text=${forked.name}`, { state: "detached", timeout: 8000 });
  await page.click(".session-name >> text=renamed");
  const bad = await api("/api/sessions", { dir: data, resume: "x; touch /tmp/pwned" });
  assert(!bad.name && bad.error, "only ids of the directory's transcripts are accepted");

  log("poker: play hands to showdown (terminal docked on the right)");
  await page.click('nav.tabs button:has-text("德州")');
  await page.waitForSelector(".terms.docked", { timeout: 5000 });
  assert(await page.locator(".terms.docked .term-box:visible").first().isVisible(), "active terminal stays visible beside the game");
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
  const heard = await page.evaluate(() => window.__pokerSounds.join(","));
  for (const s of ["deal", "turn"]) assert(heard.includes(s), `poker plays ${s}: ${heard}`);
  assert(/win|lose/.test(heard), `a finished hand sounds: ${heard}`);
  await page.click(".sound-toggle");
  assert((await page.textContent(".sound-toggle")).includes("🔇"), "sound can be muted");
  assert((await page.evaluate(() => localStorage.getItem("pokerSound"))) === "off", "mute is remembered");
  await page.click(".sound-toggle");

  log("mahjong: void suit, tap a tile twice to discard, play a round to the end");
  const mjChips = (await api("/api/state")).poker.stats.chips;
  await page.click('nav.tabs button:has-text("麻将")');
  await page.click('.actions button.primary:has-text("开始")');
  await page.waitForSelector('.actions button:has-text("缺万")');
  assert((await api("/api/state")).poker.stats.chips === mjChips - 500, "the stake is held during the round");
  assert(!(await page.locator(".mj-opp .mj-que").count()), "bots' void suits stay hidden until you choose");
  await page.click('.actions button.primary:has-text("缺")'); // the suggested one
  await page.waitForSelector(".mj-opp .mj-que");
  let tapped = false;
  for (const end = Date.now() + 120000; Date.now() < end; ) {
    const v = (await api("/api/state")).mahjong;
    if (v.phase === "end") break;
    const o = v.options;
    if (o?.pass) await api("/api/mahjong/act", { action: o.hu ? "hu" : "pass" });
    else if (o?.discard && o.hu) await api("/api/mahjong/act", { action: "hu" });
    else if (o?.discard) {
      const me = v.players[0];
      const all = [...me.hand, ...(v.drawn >= 0 ? [v.drawn] : [])];
      const q = o.mustQue ? all.find((t) => Math.floor(t / 9) === me.que) : undefined;
      const pick = q ?? (v.drawn >= 0 ? v.drawn : all[all.length - 1]);
      if (!tapped) {
        const tile = page.locator(`.mj-hand .tile[data-tile="${pick}"]`).first();
        await tile.click();
        await page.waitForSelector(".mj-hand .tile.sel");
        assert(me.discards.length === (await api("/api/state")).mahjong.players[0].discards.length, "one tap only lifts the tile");
        // tap the lifted tile; if a state update dropped it back meanwhile (the
        // page can lag the API by one step), lift it again first
        await until(async () => {
          const m = (await api("/api/state")).mahjong;
          if (m.players[0].discards.length > me.discards.length || m.phase === "end") return true;
          const lifted = page.locator(".mj-hand .tile.sel");
          await ((await lifted.count()) ? lifted.first() : tile).click({ timeout: 2000 }).catch(() => {});
          await sleep(200);
          return false;
        }, "the second tap discards");
        tapped = true;
      } else await api("/api/mahjong/act", { action: "discard", tile: pick });
    }
    await sleep(30);
  }
  const mv = (await api("/api/state")).mahjong;
  assert(mv.phase === "end" && tapped, "a round of mahjong finished");
  assert(mv.players.reduce((a, p) => a + p.net, 0) === 0, "what is won is what is lost");
  assert((await api("/api/state")).poker.stats.chips === mjChips + mv.players[0].net, "the result is settled into the shared chips");
  await page.waitForSelector(".mj-result");
  assert((await page.evaluate(() => window.__pokerSounds.join(","))).includes("clack"), "discards clack");
  await page.screenshot({ path: join(out, "2c-mahjong.png") });
  await page.click('nav.tabs button:has-text("德州")');

  log("dock without opening the session first (fresh page straight on 德州)");
  await page.evaluate(() => {
    localStorage.setItem("active", "null");
    localStorage.setItem("tab", JSON.stringify("poker"));
  });
  await page.reload();
  await page.waitForSelector(".terms.docked .term-box", { timeout: 10000 });
  assert(await page.locator(".terms.docked .term-box:visible").first().isVisible(), "the session that needs you most is docked");
  log("narrow window: a live strip instead of the dock, a tap opens the terminal");
  await page.setViewportSize({ width: 1000, height: 800 });
  await page.waitForSelector(".live-strip .live-item .live-name", { timeout: 5000 });
  assert(!(await page.locator(".terms.docked").count()), "no dock when narrow");
  await page.screenshot({ path: join(out, "2b-poker-strip.png") });
  await page.click(".live-strip .live-item");
  await page.waitForSelector('nav.tabs button.on:has-text("对话")');
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.click('nav.tabs button:has-text("德州")');

  log("⌘1 jumps to the first session; light theme");
  await page.keyboard.press("Meta+1");
  await page.waitForSelector('nav.tabs button.on:has-text("对话")');
  await page.click(".theme-btn");
  assert((await page.evaluate(() => document.documentElement.dataset.theme)) === "light", "light theme on");
  await page.waitForTimeout(300);
  await page.screenshot({ path: join(out, "6-light.png") });
  await page.click('nav.tabs button:has-text("德州")');
  await page.screenshot({ path: join(out, "6-light-poker.png") });
  await page.click(".theme-btn");
  assert((await page.evaluate(() => document.documentElement.dataset.theme)) === "dark", "back to dark");

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
    await page.waitForSelector('.server-load .sl-v:has-text("还剩")');
    const sys = await api("/api/system");
    assert(sys.available && sys.memTotal > 0 && sys.hubRss > 0, `server load read: ${JSON.stringify(sys).slice(0, 200)}`);
    assert(sys.sessions.some((x) => x.name === "renamed" && x.rss > 0 && x.procs > 0), "memory of each tmux session's processes");
    await page.waitForSelector('.sl-sessions td:has-text("renamed")');
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
    const sw = await fetch(`${base}/sw.js`);
    assert(sw.ok && sw.headers.get("content-type").includes("javascript") && sw.headers.get("cache-control") === "no-cache", "service worker served fresh");
    const push = await api("/api/push");
    assert(push.available && push.key.length > 80, "web push key available");
    const evil = await api("/api/push/subscribe", { endpoint: "https://evil.example/x", keys: { p256dh: "x", auth: "y" } });
    assert(String(evil.error).includes("不支持的推送服务"), "push endpoints limited to browser push services");
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
    await m.tap('.composer .hist-btn:has-text("🕘")');
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
    await m.tap(".insp-toggle");
    await m.waitForSelector('.chat-msg.user:has-text("把 README 改一下")');
    const ib = await m.locator(".inspector").boundingBox();
    assert(ib.width >= 380, `inspector full width on the phone (${ib.width})`);
    await m.screenshot({ path: join(out, "5-phone-inspect.png") });
    await m.tap(".insp-close");
    await m.tap('nav.tabs button:has-text("麻将")');
    await m.waitForSelector(".mj-hand .tile");
    const mw = await m.evaluate(() => [document.documentElement.scrollWidth, innerWidth]);
    assert(mw[0] <= mw[1], `mahjong fits the phone width (${mw})`);
    await m.screenshot({ path: join(out, "5-phone-mahjong.png") });
    await m.tap('nav.tabs button:has-text("用量")');
    await m.waitForSelector(".usage-card.big");
    await m.waitForSelector(".live-strip .live-item");
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
