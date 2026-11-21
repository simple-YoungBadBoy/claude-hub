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
  const fileInput = useRef<HTMLInputElement>(null);

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
      {conn === "closed" && <div className="term-overlay">连接断开，正在重连…</div>}
    </div>
  );
}
