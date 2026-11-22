import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { Terminal as XTerm } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebglAddon } from "@xterm/addon-webgl";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";
import { api, upload, wsURL } from "../api";
import type { Session } from "../types";
import { getTheme, onTheme, type Theme } from "../theme";
import { pathLinks } from "../termLinks";
import Inspector, { type InspectTab } from "./Inspector";

type Props = {
  session: string;
  info?: Session;
  visible: boolean;
  docked?: boolean; // shown beside another tab: don't steal the keyboard
  onToast: (msg: string, err?: boolean) => void;
};

const themes = {
  dark: {
    background: "#161412",
    foreground: "#e8e2da",
    cursor: "#e07a4f",
    selectionBackground: "#e07a4f55",
    black: "#1d1b19",
    brightBlack: "#6b6560",
  },
  // paper: Claude Code draws some text in fixed light greys, so xterm's
  // minimum contrast ratio darkens anything unreadable on the cream ground
  light: {
    background: "#fbf8f2",
    foreground: "#2d2823",
    cursor: "#d2683c",
    cursorAccent: "#fbf8f2",
    selectionBackground: "#d2683c40",
    black: "#2d2823",
    red: "#b8402f",
    green: "#3b7d3f",
    yellow: "#8f6a12",
    blue: "#2f5f9e",
    magenta: "#8a4a8f",
    cyan: "#2c7a7d",
    white: "#6b6158",
    brightBlack: "#8b8176",
    brightRed: "#c94a4a",
    brightGreen: "#3f9a5c",
    brightYellow: "#a87c14",
    brightBlue: "#3a6fb5",
    brightMagenta: "#9c58a2",
    brightCyan: "#338c8f",
    brightWhite: "#3d3630",
  },
};
const termTheme = (t: Theme) => ({ theme: themes[t], minimumContrastRatio: t === "light" ? 4.5 : 1 });

/** Decode an OSC 52 payload ("c;<base64>") to text; null for queries/garbage. */
export function osc52Text(data: string): string | null {
  const b64 = data.slice(data.indexOf(";") + 1);
  if (!b64 || b64 === "?") return null; // clipboard read requests are refused
  try {
    const bin = atob(b64);
    return new TextDecoder().decode(Uint8Array.from(bin, (c) => c.charCodeAt(0)));
  } catch {
    return null;
  }
}

/** Copy inside a user gesture; works on http pages too (no Clipboard API). */
export function copyNow(text: string): boolean {
  const ta = document.createElement("textarea");
  ta.value = text;
  ta.style.cssText = "position:fixed;top:-1000px;opacity:0";
  document.body.appendChild(ta);
  ta.select();
  let ok = false;
  try {
    ok = document.execCommand("copy");
  } catch {
    ok = false;
  }
  ta.remove();
  if (!ok && navigator.clipboard) {
    void navigator.clipboard.writeText(text);
    ok = true;
  }
  return ok;
}

// phone key bar: what the soft keyboard can't type
const KEYS: [string, string, string][] = [
  ["Esc", "\x1b", "Esc：取消 / 打断"],
  ["Tab", "\t", "Tab"],
  ["⇧Tab", "\x1b[Z", "Shift+Tab：切换模式"],
  ["↑", "\x1b[A", "上"],
  ["↓", "\x1b[B", "下"],
  ["←", "\x1b[D", "左"],
  ["→", "\x1b[C", "右"],
  ["^C", "\x03", "Ctrl+C"],
  ["⇧⏎", "\x1b\r", "换行（不发送）"],
  ["⏎", "\r", "回车"],
];

function imageFiles(list: FileList | DataTransferItemList | null | undefined): File[] {
  const out: File[] = [];
  if (!list) return out;
  for (let i = 0; i < list.length; i++) {
    const it = list[i] as File | DataTransferItem;
    const f = it instanceof File ? it : it.kind === "file" ? it.getAsFile() : null;
    if (f) out.push(f);
  }
  return out;
}

// Recently sent composer texts: a per-device convenience, so browser storage.
const HIST_KEY = "hub.sent";
function loadHist(): string[] {
  try {
    const h = JSON.parse(localStorage.getItem(HIST_KEY) || "[]");
    return Array.isArray(h) ? h.filter((x) => typeof x === "string") : [];
  } catch {
    return [];
  }
}
function saveHist(text: string) {
  try {
    localStorage.setItem(HIST_KEY, JSON.stringify([text, ...loadHist().filter((x) => x !== text)].slice(0, 30)));
  } catch {
    /* storage unavailable */
  }
}

// The input bar is the default way to type (local echo, native IME); a
// viewer can turn it off. Per-device preference, so browser storage.
const BAR_KEY = "hub.composer";
function barPref(): boolean {
  try {
    return localStorage.getItem(BAR_KEY) !== "0";
  } catch {
    return true;
  }
}
function saveBarPref(on: boolean) {
  try {
    localStorage.setItem(BAR_KEY, on ? "1" : "0");
  } catch {
    /* storage unavailable */
  }
}
const coarse = typeof matchMedia === "function" && matchMedia("(pointer: coarse)").matches;
// one-tap Claude Code commands from the input bar (⚡)
const QUICK: [string, string][] = [
  ["/compact", "压缩上下文"],
  ["/clear", "清空对话（会丢掉当前上下文）"],
  ["/model", "换模型"],
  ["/effort", "思考强度"],
  ["/context", "看上下文占用"],
  ["/usage", "看额度"],
  ["/resume", "恢复以前的对话"],
  ["/init", "生成 CLAUDE.md"],
];
// built-in commands offered when the input starts with "/" (plus the
// project's and user's own commands and skills from the server)
const BUILTIN: [string, string][] = [
  ...QUICK,
  ["/review", "审查改动"],
  ["/memory", "编辑记忆（CLAUDE.md）"],
  ["/rewind", "回退到之前的某一步"],
  ["/export", "导出对话"],
  ["/permissions", "工具权限"],
  ["/agents", "子代理"],
  ["/mcp", "MCP 服务器"],
  ["/hooks", "hooks 设置"],
  ["/plugin", "插件"],
  ["/add-dir", "添加工作目录"],
  ["/config", "设置"],
  ["/status", "版本、账号、模型"],
  ["/doctor", "诊断安装问题"],
  ["/help", "帮助"],
];
type SuggItem = { text: string; label: string; desc?: string; dir?: boolean };
type Sugg = { kind: "file" | "cmd"; start: number; end: number; items: SuggItem[]; sel: number };
// keys that go straight to Claude while the input bar is empty
const PASS_EMPTY: Record<string, string> = { ArrowUp: "\x1b[A", ArrowDown: "\x1b[B", Tab: "\t" };

export default function Terminal({ session, info, visible, docked, onToast }: Props) {
  const box = useRef<HTMLDivElement>(null);
  const termRef = useRef<XTerm | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const [conn, setConn] = useState<"connecting" | "open" | "closed">("connecting");
  const [dragging, setDragging] = useState(false);
  const [busy, setBusy] = useState(false);
  const [copyLater, setCopyLater] = useState<string | null>(null);
  const [compose, setComposeState] = useState(barPref);
  // Uncontrolled on purpose: the hub pushes state often, and a controlled
  // value re-applied mid-composition wipes the pinyin of third-party iOS
  // keyboards (Sogou, WeChat) before it is committed.
  const draftRef = useRef<HTMLTextAreaElement>(null);
  const [hasDraft, setHasDraft] = useState(false);
  const [hist, setHist] = useState<string[] | null>(null);
  const [quick, setQuick] = useState(false);
  const composing = useRef(false);
  const [sugg, setSugg] = useState<Sugg | null>(null);
  const suggRef = useRef<Sugg | null>(null);
  suggRef.current = sugg;
  const suggSeq = useRef(0);
  const suggTimer = useRef<number>();
  const cmds = useRef<{ at: number; list: { name: string; desc?: string }[] } | null>(null);
  const compEnd = useRef(0);
  const sendQ = useRef<Promise<unknown>>(Promise.resolve());
  const [answered, setAnswered] = useState("");
  const prompt = info?.status === "waiting" ? info.prompt : undefined;
  const promptKey = prompt ? JSON.stringify(prompt) : "";
  const fileInput = useRef<HTMLInputElement>(null);
  const toastRef = useRef(onToast);
  toastRef.current = onToast;
  const [insp, setInsp] = useState<InspectTab | null>(null);
  const [inspFile, setInspFile] = useState<{ path: string; n: number }>();
  const openFile = useRef((path: string) => {
    setInspFile((f) => ({ path, n: (f?.n ?? 0) + 1 }));
    setInsp("files");
  });

  // create terminal + websocket once per session
  useEffect(() => {
    const term = new XTerm({
      fontFamily: '"SF Mono", Menlo, "JetBrains Mono", Monaco, "PingFang SC", monospace',
      fontSize: 13,
      lineHeight: 1.15,
      cursorBlink: true,
      macOptionIsMeta: true,
      macOptionClickForcesSelection: true,
      allowProposedApi: true,
      scrollback: 5000,
      ...termTheme(getTheme()),
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.loadAddon(new WebLinksAddon());
    term.registerLinkProvider(pathLinks(term, (p) => openFile.current(p)));
    term.open(box.current!);
    // WebGL is fast on desktops; on phones (iOS Safari especially) it can
    // mis-scale or lose its context, and the DOM renderer is plenty there
    if (!matchMedia("(pointer: coarse)").matches) {
      try {
        const gl = new WebglAddon();
        gl.onContextLoss(() => gl.dispose());
        term.loadAddon(gl);
      } catch {
        /* DOM renderer fallback */
      }
    }
    termRef.current = term;
    fitRef.current = fit;
    const offTheme = onTheme((t) => (term.options = termTheme(t)));

    const send = (o: object) => {
      const ws = wsRef.current;
      if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(o));
    };
    // Shift+Enter → newline in Claude Code (sent as ESC CR, like iTerm's setup)
    term.attachCustomKeyEventHandler((e) => {
      if (e.type === "keydown" && e.key === "Enter" && e.shiftKey) {
        send({ t: "i", d: "\x1b\r" });
        return false;
      }
      return true;
    });
    term.onData((d) => send({ t: "i", d }));

    // Claude Code (and tmux) copy with OSC 52, which xterm.js ignores. Writing
    // the clipboard needs a user gesture in WebKit, and the sequence arrives
    // later over the network, so on mouseup we start a clipboard write whose
    // content is a promise that the next OSC 52 fulfils.
    let pending: { resolve: (t: string) => void; timer: number } | null = null;
    let hideLater: number | undefined;
    const delivered = (text: string) => {
      toastRef.current(`已复制 ${text.length} 个字符`);
      setCopyLater(null);
    };
    const offerClick = (text: string) => {
      setCopyLater(text);
      clearTimeout(hideLater);
      hideLater = window.setTimeout(() => setCopyLater(null), 20000);
    };
    term.parser.registerOscHandler(52, (data) => {
      const text = osc52Text(data);
      if (text === null) return true;
      if (pending) {
        clearTimeout(pending.timer);
        pending.resolve(text);
        pending = null;
        delivered(text);
        return true;
      }
      if (navigator.clipboard?.writeText) {
        // some engines neither resolve nor reject without a gesture/focus
        let settled = false;
        const giveUp = window.setTimeout(() => !settled && offerClick(text), 1500);
        navigator.clipboard.writeText(text).then(
          () => {
            settled = true;
            clearTimeout(giveUp);
            delivered(text);
          },
          () => {
            settled = true;
            clearTimeout(giveUp);
            offerClick(text);
          },
        );
      } else offerClick(text);
      return true;
    });
    const el = box.current!;
    let down: { x: number; y: number } | null = null;
    const onDown = (e: MouseEvent) => (down = { x: e.clientX, y: e.clientY });
    const onUp = (e: MouseEvent) => {
      const dragged = down && Math.abs(e.clientX - down.x) + Math.abs(e.clientY - down.y) > 4;
      down = null;
      if (!(dragged || e.detail >= 2) || pending || !navigator.clipboard?.write || typeof ClipboardItem === "undefined") return;
      let resolve!: (t: string) => void;
      let reject!: (e: Error) => void;
      const text = new Promise<string>((res, rej) => ((resolve = res), (reject = rej)));
      const timer = window.setTimeout(() => {
        pending = null;
        reject(new Error("no copy"));
      }, 2000);
      pending = { resolve, timer };
      const blob = text.then((t) => new Blob([t], { type: "text/plain" }));
      navigator.clipboard.write([new ClipboardItem({ "text/plain": blob })]).catch(() => {
        /* nothing was copied (plain click/selection) or the write was refused */
      });
    };
    el.addEventListener("mousedown", onDown, true);
    el.addEventListener("mouseup", onUp, true);
    term.onResize(({ cols, rows }) => send({ t: "r", c: cols, r: rows }));

    let stopped = false;
    let retry: number | undefined;
    const connect = () => {
      try {
        fit.fit();
      } catch {
        /* hidden */
      }
      setConn("connecting");
      const ws = new WebSocket(wsURL(`/ws/term/${encodeURIComponent(session)}?c=${term.cols}&r=${term.rows}`));
      ws.binaryType = "arraybuffer";
      wsRef.current = ws;
      ws.onopen = () => {
        setConn("open");
        term.reset();
        send({ t: "r", c: term.cols, r: term.rows });
      };
      ws.onmessage = (e) => {
        if (typeof e.data === "string") return; // control message
        term.write(new Uint8Array(e.data));
      };
      ws.onclose = () => {
        setConn("closed");
        if (!stopped) retry = window.setTimeout(connect, 2000);
      };
    };
    connect();
    return () => {
      stopped = true;
      clearTimeout(retry);
      clearTimeout(hideLater);
      el.removeEventListener("mousedown", onDown, true);
      el.removeEventListener("mouseup", onUp, true);
      wsRef.current?.close();
      offTheme();
      term.dispose();
    };
  }, [session]);

  // fit on show / window resize
  useEffect(() => {
    if (!visible) return;
    const doFit = () => {
      try {
        fitRef.current?.fit();
      } catch {
        /* ignore */
      }
    };
    const raf = requestAnimationFrame(() => {
      doFit();
      if (!docked) focusInput();
    });
    const ro = new ResizeObserver(doFit);
    if (box.current) ro.observe(box.current);
    return () => {
      cancelAnimationFrame(raf);
      ro.disconnect();
    };
  }, [visible, docked]);

  const sendInput = (d: string) => {
    const ws = wsRef.current;
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ t: "i", d }));
  };
  // Where typing goes: the input bar when it's open (on phones only after a
  // tap, so the soft keyboard doesn't cover the screen), else the terminal.
  const focusInput = () => {
    if (draftRef.current) {
      if (!coarse) draftRef.current.focus();
    } else termRef.current?.focus();
  };
  const setCompose = (on: boolean) => {
    saveBarPref(on);
    setComposeState(on);
    window.setTimeout(() => (on && !coarse ? draftRef.current?.focus() : termRef.current?.focus()), 0);
  };
  const fitDraft = (box: HTMLTextAreaElement) => {
    box.style.height = "auto";
    box.style.height = `${Math.min(box.scrollHeight + 2, coarse ? 160 : 220)}px`;
  };
  // The server types the text, waits until it shows in Claude's input, then
  // presses Enter (an Enter that overtakes the text submits nothing).
  // Messages are queued so two quick sends never interleave.
  const sendDraft = () => {
    const box = draftRef.current;
    const text = (box?.value ?? "").replace(/\s+$/, "");
    if (!box || !text) return;
    saveHist(text);
    clearDraft(box);
    fitDraft(box);
    setHasDraft(false);
    setHist(null);
    sendQ.current = sendQ.current.then(() =>
      api(`/api/sessions/${encodeURIComponent(session)}/send`, { text }).catch((e) => {
        onToast(`发送失败：${(e as Error).message}`, true);
        if (draftRef.current && !draftRef.current.value) fillDraft(text);
      }),
    );
  };
  // "@path" and "/command" completion, like Claude Code's own input
  const updateSugg = (box: HTMLTextAreaElement, refresh = true) => {
    clearTimeout(suggTimer.current);
    const pos = box.selectionStart;
    const before = box.value.slice(0, pos);
    const at = pos === box.selectionEnd ? before.match(/(?:^|\s)@([^\s@]*)$/) : null;
    const slash = !at && pos === box.selectionEnd ? before.match(/^\/([^\s]*)$/) : null;
    const n = ++suggSeq.current;
    if (!at && !slash) {
      if (suggRef.current) setSugg(null);
      return;
    }
    const enc = encodeURIComponent;
    if (at) {
      const q = at[1];
      suggTimer.current = window.setTimeout(() => {
        api<{ files: string[] }>(`/api/sessions/${enc(session)}/complete?q=${enc(q)}`)
          .then((r) => {
            if (n !== suggSeq.current) return;
            const items = r.files.map((f) => ({ text: "@" + f + (f.endsWith("/") ? "" : " "), label: f, dir: f.endsWith("/") }));
            setSugg(items.length ? { kind: "file", start: pos - q.length - 1, end: pos, items, sel: 0 } : null);
          })
          .catch(() => {});
      }, 100);
      return;
    }
    const q = slash![1].toLowerCase();
    const show = (list: { name: string; desc?: string }[]) => {
      if (n !== suggSeq.current) return;
      const hits = list
        .filter((c) => c.name.toLowerCase().includes(q))
        .sort((a, b) => Number(!a.name.toLowerCase().startsWith(q)) - Number(!b.name.toLowerCase().startsWith(q)))
        .slice(0, 40);
      setSugg(hits.length ? { kind: "cmd", start: 0, end: pos, items: hits.map((c) => ({ text: "/" + c.name + " ", label: "/" + c.name, desc: c.desc })), sel: 0 } : null);
    };
    const c = cmds.current;
    if (c) show(c.list);
    if (refresh && (!c || !suggRef.current || Date.now() - c.at > 30000)) { // refresh whenever the list opens
      const builtin = BUILTIN.map(([name, desc]) => ({ name: name.slice(1), desc }));
      api<{ commands: { name: string; desc?: string; source: string }[] }>(`/api/sessions/${enc(session)}/commands`)
        .then((r) => {
          const own = r.commands.map((x) => ({ name: x.name, desc: (x.source === "project" ? "项目 · " : "我的 · ") + (x.desc || "") }));
          cmds.current = { at: Date.now(), list: [...own, ...builtin.filter((b) => !own.some((o) => o.name === b.name))] };
          // keys typed meanwhile made this request stale: match what's typed now
          if (draftRef.current && n !== suggSeq.current) updateSugg(draftRef.current, false);
          else show(cmds.current.list);
        })
        .catch(() => {
          cmds.current = { at: Date.now(), list: builtin };
          show(builtin);
        });
    }
  };
  const acceptSugg = (i: number, send = false) => {
    const sg = suggRef.current;
    const box = draftRef.current;
    if (!sg || !box || !sg.items[i]) return;
    const it = sg.items[i];
    box.focus();
    box.setSelectionRange(sg.start, sg.end);
    let ok = false;
    try {
      ok = document.execCommand("insertText", false, it.text);
    } catch {
      /* unsupported */
    }
    if (!ok) box.setRangeText(it.text, sg.start, sg.end, "end");
    fitDraft(box);
    setHasDraft(box.value.trim() !== "");
    if (send) {
      setSugg(null);
      sendDraft();
    } else if (!it.dir) setSugg(null); // a directory keeps completing inside it
    if (!ok || it.dir) updateSugg(box);
  };
  useEffect(() => {
    if (sugg) document.querySelector(".sugg button.on")?.scrollIntoView({ block: "nearest" });
  }, [sugg]);

  const onDraftKey = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    // an IME is choosing characters: Enter/arrows belong to it (Safari sends
    // the confirming Enter right after compositionend, hence the time check)
    if (e.nativeEvent.isComposing || e.keyCode === 229 || composing.current || performance.now() - compEnd.current < 80) return;
    const box = e.currentTarget;
    const sg = suggRef.current;
    if (sg) {
      const len = sg.items.length;
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        setSugg({ ...sg, sel: (sg.sel + (e.key === "ArrowDown" ? 1 : -1) + len) % len });
        return;
      }
      const plain = !e.shiftKey && !e.altKey && !e.metaKey && !e.ctrlKey;
      if (plain && (e.key === "Tab" || e.key === "Enter")) {
        e.preventDefault();
        // Enter on a command runs it (like Claude Code); Tab only fills it in to add arguments
        acceptSugg(sg.sel, e.key === "Enter" && sg.kind === "cmd" && !coarse);
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        setSugg(null);
        return;
      }
    }
    const empty = box.value === "";
    if (e.key === "Escape") {
      e.preventDefault();
      if (hist || quick) {
        setHist(null);
        setQuick(false);
      } else sendInput("\x1b"); // interrupt Claude / close its menu, even mid-draft
      return;
    }
    if (e.key === "c" && e.ctrlKey && !e.metaKey && box.selectionStart === box.selectionEnd) {
      e.preventDefault();
      sendInput("\x03");
      return;
    }
    if (e.key === "Enter" && !e.shiftKey && !e.altKey && !e.metaKey && !coarse) {
      e.preventDefault();
      if (empty) sendInput("\r");
      else sendDraft();
      return;
    }
    if (empty && !e.altKey && !e.metaKey && !e.ctrlKey) {
      const seq = e.key === "Tab" && e.shiftKey ? "\x1b[Z" : PASS_EMPTY[e.key];
      if (seq) {
        e.preventDefault();
        sendInput(seq);
      }
    }
  };
  // fill from history as an edit too, so the keyboard sees it
  const fillDraft = (text: string) => {
    const box = draftRef.current;
    setHist(null);
    if (!box) return;
    box.focus();
    box.select();
    let ok = false;
    try {
      ok = document.execCommand("insertText", false, text);
    } catch {
      /* unsupported */
    }
    if (!ok || box.value !== text) box.value = text;
    fitDraft(box);
    setHasDraft(text.trim() !== "");
  };
  // Clear through the editing pipeline, not `value = ""`: a script write is
  // invisible to iOS third-party keyboards (Sogou), which then keep the sent
  // text as context and delete the wrong characters on the next edit.
  const clearDraft = (box: HTMLTextAreaElement) => {
    box.focus();
    box.select();
    try {
      document.execCommand("delete");
    } catch {
      /* unsupported */
    }
    if (box.value) box.value = "";
  };

  const sendFiles = async (files: File[]) => {
    if (!files.length) return;
    setBusy(true);
    try {
      const paths = await upload(session, files);
      onToast(`已插入 ${paths.length} 个文件，补充说明后回车发送`);
    } catch (e) {
      onToast(`上传失败：${(e as Error).message}`, true);
    } finally {
      setBusy(false);
      focusInput();
    }
  };

  // ⌘V with an image on the clipboard → upload instead of pasting text
  useEffect(() => {
    const el = box.current!;
    const onPaste = (e: ClipboardEvent) => {
      const files = imageFiles(e.clipboardData?.items).filter((f) => f.type.startsWith("image/") || f.size > 0);
      if (files.length) {
        e.preventDefault();
        e.stopPropagation();
        void sendFiles(files);
      }
    };
    el.addEventListener("paste", onPaste, true);
    return () => el.removeEventListener("paste", onPaste, true);
  });

  return (
    <div
      className="term-wrap"
      style={{ display: visible ? "flex" : "none" }}
      onDragOver={(e) => {
        if (e.dataTransfer.types.includes("Files")) {
          e.preventDefault();
          setDragging(true);
        }
      }}
      onDragLeave={() => setDragging(false)}
      onDrop={(e) => {
        e.preventDefault();
        setDragging(false);
        void sendFiles(imageFiles(e.dataTransfer.files));
      }}
    >
      <div className="term-bar">
        <span className={`conn conn-${conn}`} />
        <span className="term-title">{session}</span>
        <span className="spacer" />
        <button className={`ghost insp-toggle ${insp ? "on" : ""}`} title="对话记录、git 改动、文件（只读）" onClick={() => setInsp(insp ? null : "chat")}>
          📖 查看
        </button>
        <button className={`ghost bar-toggle ${compose ? "on" : ""}`} title={compose ? "关掉输入栏，直接在终端里打字" : "打开输入栏（本地输入，不等回显，中文更顺）"} onClick={() => setCompose(!compose)}>
          ⌨ 输入栏
        </button>
        <button className="ghost" title="发送图片/文件（也可直接拖进来或 ⌘V 粘贴截图）" onClick={() => fileInput.current?.click()} disabled={busy}>
          {busy ? "上传中…" : "📎 图片"}
        </button>
        <input
          ref={fileInput}
          type="file"
          multiple
          hidden
          onChange={(e) => {
            void sendFiles(imageFiles(e.target.files));
            e.target.value = "";
          }}
        />
      </div>
      <div className="term-main">
        <div className="term-box" ref={box} />
        {insp && <Inspector session={session} status={info?.status ?? ""} tab={insp} file={inspFile} onTab={setInsp} onClose={() => setInsp(null)} />}
      </div>
      {prompt && answered !== promptKey && (
        <div className="prompt-card">
          <div className="prompt-q">{prompt.question}</div>
          {prompt.detail && prompt.detail.length > 0 && <pre className="prompt-detail">{prompt.detail.join("\n")}</pre>}
          <div className="prompt-opts">
            {prompt.options.map((o) => (
              <button
                key={o.key}
                className={o.key === "1" ? "primary" : ""}
                onPointerDown={(e) => e.preventDefault()}
                onClick={() => {
                  sendInput(o.key);
                  setAnswered(promptKey);
                }}
              >
                <b>{o.key}</b> {o.label}
              </button>
            ))}
          </div>
        </div>
      )}
      <div className="keybar">
        {KEYS.map(([label, seq, title]) => (
          <button
            key={label}
            title={title}
            // keep focus (and the soft keyboard) where it is
            onPointerDown={(e) => e.preventDefault()}
            onClick={() => sendInput(seq)}
          >
            {label}
          </button>
        ))}
        <button className={compose ? "on" : ""} onPointerDown={(e) => e.preventDefault()} onClick={() => setCompose(!compose)} title="输入栏（中文输入更顺手）">
          ✎
        </button>
      </div>
      {compose && (
        <form
          className="composer"
          onSubmit={(e) => {
            e.preventDefault();
            sendDraft();
          }}
        >
          {quick && (
            <div className="hist quick">
              {QUICK.map(([cmd, what]) => (
                <button
                  key={cmd}
                  type="button"
                  onPointerDown={(e) => e.preventDefault()}
                  onClick={() => {
                    setQuick(false);
                    if (cmd === "/clear" && !confirm("清空这个会话的对话？当前上下文会丢掉。")) return;
                    sendQ.current = sendQ.current.then(() =>
                      api(`/api/sessions/${encodeURIComponent(session)}/send`, { text: cmd }).catch((e) => onToast(`发送失败：${(e as Error).message}`, true)),
                    );
                  }}
                >
                  <b>{cmd}</b> <span className="muted">{what}</span>
                </button>
              ))}
            </div>
          )}
          {sugg && !hist && !quick && (
            <div className="hist sugg">
              {sugg.items.map((it, i) => (
                <button key={it.label} type="button" className={i === sugg.sel ? "on" : ""} onPointerDown={(e) => e.preventDefault()} onClick={() => acceptSugg(i)}>
                  {sugg.kind === "file" && <span className="sugg-icon">{it.dir ? "📁" : "📄"}</span>}
                  <b>{it.label}</b> {it.desc && <span className="muted">{it.desc}</span>}
                </button>
              ))}
            </div>
          )}
          {hist && (
            <div className="hist">
              {hist.length === 0 && <div className="muted small">还没有发过消息</div>}
              {hist.map((h, i) => (
                <button key={i} type="button" onPointerDown={(e) => e.preventDefault()} onClick={() => fillDraft(h)}>
                  {h}
                </button>
              ))}
            </div>
          )}
          <textarea
            rows={1}
            ref={draftRef}
            enterKeyHint={coarse ? "enter" : "send"}
            placeholder={coarse ? "给 Claude 发消息" : "给 Claude 发消息 · 回车发送，Shift+回车换行"}
            title={coarse ? undefined : "回车发送，Shift+回车换行 · 输入框空着时 ↑↓ Tab 回车直接给终端 · Esc 打断 Claude · ⌘1–9 切换会话"}
            onInput={(e) => {
              setHasDraft(e.currentTarget.value.trim() !== "");
              fitDraft(e.currentTarget);
              if (!composing.current) updateSugg(e.currentTarget);
            }}
            onBlur={() => setSugg(null)}
            onKeyDown={onDraftKey}
            onCompositionStart={() => (composing.current = true)}
            onCompositionEnd={(e) => {
              composing.current = false;
              compEnd.current = performance.now();
              updateSugg(e.currentTarget);
            }}
            onPaste={(e) => {
              const files = imageFiles(e.clipboardData?.items);
              if (files.length) {
                e.preventDefault();
                void sendFiles(files);
              }
            }}
          />
          <button
            type="button"
            className={`ghost hist-btn ${quick ? "on" : ""}`}
            title="常用命令"
            onPointerDown={(e) => e.preventDefault()}
            onClick={() => {
              setHist(null);
              setQuick((q) => !q);
            }}
          >
            ⚡
          </button>
          <button
            type="button"
            className={`ghost hist-btn ${hist ? "on" : ""}`}
            title="最近发过的消息"
            onPointerDown={(e) => e.preventDefault()}
            onClick={() => {
              setQuick(false);
              setHist((h) => (h ? null : loadHist()));
            }}
          >
            🕘
          </button>
          <button className="primary" disabled={!hasDraft}>
            发送
          </button>
        </form>
      )}
      {dragging && <div className="drop-hint">松手发送给 Claude</div>}
      {copyLater !== null && (
        <button
          className="copy-later"
          onClick={() => {
            const ok = copyNow(copyLater);
            setCopyLater(null);
            onToast(ok ? `已复制 ${copyLater.length} 个字符` : "复制失败，请用 Option+拖动 选中后 ⌘C", !ok);
          }}
        >
          📋 Claude 复制了 {copyLater.length} 个字符 · 点这里复制到本机
        </button>
      )}
      {conn === "closed" && <div className="term-overlay">连接断开，正在重连…</div>}
    </div>
  );
}
