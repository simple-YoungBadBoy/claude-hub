import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../api";
import Markdown from "./Markdown";

// Read-only views beside the terminal: the conversation as chat, the git
// changes of the session's directory, and its files.

export type InspectTab = "chat" | "git" | "files";

type ChatTool = { name: string; input?: string; error?: boolean };
type ChatMsg = { role: "user" | "assistant"; text?: string; tools?: ChatTool[]; at?: string };
type Chat = { messages: ChatMsg[]; truncated?: boolean };
type Change = { path: string; from?: string; status: string };
type Git = { repo: boolean; root?: string; branch?: string; changes: Change[]; truncated?: boolean };
type Diff = { path: string; text: string; binary?: boolean; truncated?: boolean };
type Entry = { name: string; dir: boolean; size: number };
type FileView = { path: string; dir: boolean; text?: string; binary?: boolean; size: number; truncated?: boolean; entries?: Entry[] };

type Props = {
  session: string;
  status: string;
  tab: InspectTab;
  file?: { path: string; n: number }; // a path to open in the files tab
  onTab: (t: InspectTab) => void;
  onClose: () => void;
};

const enc = encodeURIComponent;

export default function Inspector({ session, status, tab, file, onTab, onClose }: Props) {
  return (
    <div className="inspector">
      <div className="insp-tabs">
        {(
          [
            ["chat", "💬 对话"],
            ["git", "± 改动"],
            ["files", "📁 文件"],
          ] as const
        ).map(([t, label]) => (
          <button key={t} className={`ghost ${tab === t ? "on" : ""}`} onClick={() => onTab(t)}>
            {label}
          </button>
        ))}
        <span className="spacer" />
        <button className="icon insp-close" title="关闭" onClick={onClose}>
          ✕
        </button>
      </div>
      <div className="insp-body">
        {tab === "chat" && <ChatView session={session} status={status} />}
        {tab === "git" && <GitView session={session} status={status} />}
        {tab === "files" && <FilesView session={session} file={file} />}
      </div>
    </div>
  );
}

function useLoad<T>(url: string | null, deps: unknown[]) {
  const [data, setData] = useState<T | null>(null);
  const [err, setErr] = useState("");
  const [loading, setLoading] = useState(false);
  const seq = useRef(0);
  const load = useCallback(() => {
    if (!url) return;
    const n = ++seq.current;
    setLoading(true);
    api<T>(url)
      .then((d) => {
        if (n !== seq.current) return;
        setData(d);
        setErr("");
      })
      .catch((e) => n === seq.current && setErr((e as Error).message))
      .finally(() => n === seq.current && setLoading(false));
  }, [url]);
  useEffect(load, [load, ...deps]); // eslint-disable-line react-hooks/exhaustive-deps
  return { data, err, loading, reload: load, setData };
}

function ChatView({ session, status }: { session: string; status: string }) {
  const { data, err, reload } = useLoad<Chat>(`/api/sessions/${enc(session)}/chat`, [status]);
  const box = useRef<HTMLDivElement>(null);
  const stick = useRef(true);
  // refresh while Claude works; follow the bottom unless the reader scrolled up
  useEffect(() => {
    if (status !== "working") return;
    const t = setInterval(reload, 4000);
    return () => clearInterval(t);
  }, [status, reload]);
  useEffect(() => {
    const el = box.current;
    if (el && stick.current) el.scrollTop = el.scrollHeight;
  }, [data]);
  if (err) return <div className="insp-empty error">{err}</div>;
  if (!data) return <div className="insp-empty">读取中…</div>;
  if (!data.messages.length) return <div className="insp-empty">还没有对话记录</div>;
  return (
    <div
      className="chat"
      ref={box}
      onScroll={(e) => {
        const el = e.currentTarget;
        stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
      }}
    >
      {data.truncated && <div className="chat-more muted">更早的消息没有显示</div>}
      {data.messages.map((m, i) => (
        <div key={i} className={`chat-msg ${m.role}`}>
          {m.text && (m.role === "user" ? <div className="chat-user">{m.text}</div> : <Markdown text={m.text} />)}
          {m.tools && m.tools.length > 0 && (
            <details className="chat-tools">
              <summary>
                🔧 {m.tools.length} 个工具调用
                {m.tools.some((t) => t.error) && <span className="chat-err"> · 有出错</span>}
                <span className="muted"> · {[...new Set(m.tools.map((t) => t.name))].slice(0, 5).join("、")}</span>
              </summary>
              {m.tools.map((t, j) => (
                <div key={j} className={`chat-tool ${t.error ? "err" : ""}`}>
                  <b>{t.name}</b> <span className="muted">{t.input}</span>
                </div>
              ))}
            </details>
          )}
        </div>
      ))}
    </div>
  );
}

const statusLabel = (s: string) => {
  if (s === "??") return ["新", "add"];
  const c = s.trim()[0];
  if (c === "A") return ["新", "add"];
  if (c === "D") return ["删", "del"];
  if (c === "R") return ["移", "mod"];
  if (c === "U") return ["冲突", "del"];
  return ["改", "mod"];
};

function GitView({ session, status }: { session: string; status: string }) {
  const { data, err, loading, reload } = useLoad<Git>(`/api/sessions/${enc(session)}/git`, [status]);
  const [open, setOpen] = useState<string | null>(null);
  if (err) return <div className="insp-empty error">{err}</div>;
  if (!data) return <div className="insp-empty">读取中…</div>;
  if (!data.repo) return <div className="insp-empty">这个会话的目录不是 git 仓库</div>;
  return (
    <div className="git">
      <div className="git-head">
        <span>
          ⎇ <b>{data.branch}</b> · {data.changes.length} 个文件有改动{data.truncated ? "（只显示前 500 个）" : ""}
        </span>
        <span className="spacer" />
        <button className="ghost" onClick={reload} disabled={loading}>
          {loading ? "…" : "刷新"}
        </button>
      </div>
      {!data.changes.length && <div className="insp-empty">工作区是干净的，没有未提交的改动</div>}
      {data.changes.map((c) => {
        const [label, cls] = statusLabel(c.status);
        return (
          <div key={c.path} className="git-file">
            <button className={`git-row ${open === c.path ? "on" : ""}`} onClick={() => setOpen(open === c.path ? null : c.path)}>
              <span className={`git-st ${cls}`}>{label}</span>
              <span className="git-path">
                {c.from && <span className="muted">{c.from} → </span>}
                {c.path}
              </span>
            </button>
            {open === c.path && <DiffView session={session} path={c.path} />}
          </div>
        );
      })}
    </div>
  );
}

function DiffView({ session, path }: { session: string; path: string }) {
  const { data, err } = useLoad<Diff>(`/api/sessions/${enc(session)}/diff?path=${enc(path)}`, []);
  if (err) return <div className="insp-empty error">{err}</div>;
  if (!data) return <div className="insp-empty">读取中…</div>;
  if (data.binary) return <div className="insp-empty">二进制文件，不显示内容</div>;
  if (!data.text.trim()) return <div className="insp-empty">没有内容上的差别（可能只是权限变了）</div>;
  return (
    <pre className="diff">
      {data.text.split("\n").map((l, i) => {
        const cls = l.startsWith("@@") ? "hunk" : l.startsWith("+++") || l.startsWith("---") || l.startsWith("diff ") || l.startsWith("index ") ? "meta" : l[0] === "+" ? "add" : l[0] === "-" ? "del" : "";
        return (
          <div key={i} className={cls}>
            {l || " "}
          </div>
        );
      })}
      {data.truncated && <div className="meta">…太长，只显示了开头</div>}
    </pre>
  );
}

const size = (n: number) => (n < 1024 ? `${n} B` : n < 1 << 20 ? `${(n / 1024).toFixed(1)} KB` : `${(n / (1 << 20)).toFixed(1)} MB`);

function FilesView({ session, file }: { session: string; file?: { path: string; n: number } }) {
  const [path, setPath] = useState(file?.path ?? "");
  useEffect(() => {
    if (file) setPath(file.path);
  }, [file]);
  const { data, err, loading } = useLoad<FileView>(`/api/sessions/${enc(session)}/file?path=${enc(path)}`, []);
  const [raw, setRaw] = useState(false);
  const shown = data?.path ?? path;
  const crumbs = shown ? shown.split("/") : [];
  const up = (n: number) => setPath(crumbs.slice(0, n).join("/"));
  const isMd = !!data && !data.dir && /\.(md|markdown|mdx)$/i.test(data.path);
  return (
    <div className="files">
      <div className="crumbs">
        <button className="ghost" onClick={() => up(0)}>
          📁 工作目录
        </button>
        {crumbs.map((c, i) => (
          <span key={i}>
            <span className="muted"> / </span>
            <button className="ghost" onClick={() => up(i + 1)}>
              {c}
            </button>
          </span>
        ))}
        <span className="spacer" />
        {loading && <span className="muted">…</span>}
        {isMd && (
          <button className="ghost" onClick={() => setRaw(!raw)}>
            {raw ? "预览" : "源码"}
          </button>
        )}
      </div>
      {err && <div className="insp-empty error">{err}</div>}
      {data?.dir && (
        <div className="dir">
          {!data.entries?.length && <div className="insp-empty">空目录</div>}
          {data.entries?.map((e) => (
            <button key={e.name} className="dir-row" onClick={() => setPath(data.path ? `${data.path}/${e.name}` : e.name)}>
              <span>{e.dir ? "📁" : "📄"}</span>
              <span className="dir-name">{e.name}</span>
              {!e.dir && <span className="muted">{size(e.size)}</span>}
            </button>
          ))}
          {data.truncated && <div className="insp-empty">只显示前 500 项</div>}
        </div>
      )}
      {data && !data.dir && (
        <div className="file">
          {data.binary ? (
            <div className="insp-empty">二进制文件（{size(data.size)}），不显示内容</div>
          ) : isMd && !raw ? (
            <div className="file-md">
              <Markdown text={data.text ?? ""} />
            </div>
          ) : (
            <pre className="file-text">
              {(data.text ?? "").replace(/\n$/, "").split("\n").map((l, i) => (
                <div key={i}>
                  <span className="ln">{i + 1}</span>
                  {l || " "}
                </div>
              ))}
            </pre>
          )}
          {data.truncated && <div className="insp-empty">文件太大，只显示了前 512 KB</div>}
        </div>
      )}
    </div>
  );
}
