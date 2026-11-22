import { useEffect, useRef, useState } from "react";
import { Terminal as XTerm } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebglAddon } from "@xterm/addon-webgl";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";
import { upload, wsURL } from "../api";

type Props = {
  session: string;
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

export default function Terminal({ session, visible, onToast }: Props) {
  const box = useRef<HTMLDivElement>(null);
  const termRef = useRef<XTerm | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const [conn, setConn] = useState<"connecting" | "open" | "closed">("connecting");
  const [dragging, setDragging] = useState(false);
  const [busy, setBusy] = useState(false);
  const [copyLater, setCopyLater] = useState<string | null>(null);
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
    try {
      const gl = new WebglAddon();
      gl.onContextLoss(() => gl.dispose());
      term.loadAddon(gl);
    } catch {
      /* canvas fallback */
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
