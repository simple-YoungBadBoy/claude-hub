import { useEffect, useRef, useState } from "react";
import { Terminal as XTerm } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebglAddon } from "@xterm/addon-webgl";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";
import { upload, wsURL } from "../api";
import type { Session } from "../types";

type Props = {
  session: string;
  info?: Session;
  visible: boolean;
  onToast: (msg: string, err?: boolean) => void;
};

const theme = {
  background: "#161412",
  foreground: "#e8e2da",
  cursor: "#e07a4f",
  selectionBackground: "#e07a4f55",
  black: "#1d1b19",
  brightBlack: "#6b6560",
};

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

export default function Terminal({ session, info, visible, onToast }: Props) {
  const box = useRef<HTMLDivElement>(null);
  const termRef = useRef<XTerm | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const [conn, setConn] = useState<"connecting" | "open" | "closed">("connecting");
  const [dragging, setDragging] = useState(false);
  const [busy, setBusy] = useState(false);
  const [copyLater, setCopyLater] = useState<string | null>(null);
  const [compose, setCompose] = useState(false);
  // Uncontrolled on purpose: the hub pushes state often, and a controlled
  // value re-applied mid-composition wipes the pinyin of third-party iOS
  // keyboards (Sogou, WeChat) before it is committed.
  const draftRef = useRef<HTMLTextAreaElement>(null);
  const [hasDraft, setHasDraft] = useState(false);
  const [hist, setHist] = useState<string[] | null>(null);
  const [answered, setAnswered] = useState("");
  const prompt = info?.status === "waiting" ? info.prompt : undefined;
  const promptKey = prompt ? JSON.stringify(prompt) : "";
  const fileInput = useRef<HTMLInputElement>(null);
  const toastRef = useRef(onToast);
  toastRef.current = onToast;

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
      theme,
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.loadAddon(new WebLinksAddon());
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
      termRef.current?.focus();
    });
    const ro = new ResizeObserver(doFit);
    if (box.current) ro.observe(box.current);
    return () => {
      cancelAnimationFrame(raf);
      ro.disconnect();
    };
  }, [visible]);

  const sendInput = (d: string) => {
    const ws = wsRef.current;
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ t: "i", d }));
  };
  // The composer suits phone keyboards / IME better than typing into xterm.
  // Text and Enter go separately: Claude Code treats fast input with a CR in
  // it as a paste (newline, not submit).
  const sendDraft = () => {
    const box = draftRef.current;
    const text = (box?.value ?? "").replace(/\s+$/, "");
    if (!box || !text) return;
    sendInput(text.includes("\n") ? `\x1b[200~${text}\x1b[201~` : text);
    window.setTimeout(() => sendInput("\r"), 120);
    saveHist(text);
    clearDraft(box);
    setHasDraft(false);
    setHist(null);
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
      termRef.current?.focus();
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
      <div className="term-box" ref={box} />
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
        <button className={compose ? "on" : ""} onPointerDown={(e) => e.preventDefault()} onClick={() => setCompose((c) => !c)} title="用输入框打字（中文输入更顺手）">
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
            autoFocus
            rows={2}
            ref={draftRef}
            placeholder="在这里输入，点发送（回车换行）· 也可用键盘上的 🎤 听写"
            onInput={(e) => setHasDraft(e.currentTarget.value.trim() !== "")}
          />
          <button
            type="button"
            className={`ghost hist-btn ${hist ? "on" : ""}`}
            title="最近发过的消息"
            onPointerDown={(e) => e.preventDefault()}
            onClick={() => setHist((h) => (h ? null : loadHist()))}
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
