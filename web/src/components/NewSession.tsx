import { useEffect, useState } from "react";
import { api } from "../api";
import { fmtBytes, memTight, useSysInfo } from "./ServerLoad";

export type CreateOpts = { resume?: string; fork?: boolean };
type Past = { id: string; title: string; last?: string; at: number; open?: string };

type Props = {
  dirs: string[];
  onCreate: (name: string, dir: string, opts: CreateOpts) => Promise<void>;
  onClose: () => void;
};

export default function NewSession({ dirs, onCreate, onClose }: Props) {
  const [name, setName] = useState("");
  const [dir, setDir] = useState(dirs[0] ?? "~");
  const sys = useSysInfo(10000);
  const per = sys?.sessions.filter((x) => x.rss > 0) ?? [];
  const avg = per.length ? per.reduce((a, x) => a + x.rss, 0) / per.length : 0;
  const [past, setPast] = useState<Past[]>([]);
  const [resume, setResume] = useState("");
  const [fork, setFork] = useState(false);
  const [more, setMore] = useState(false);
  // the directory's earlier conversations, to pick one up again
  useEffect(() => {
    let live = true;
    const t = setTimeout(() => {
      api<Past[]>(`/api/history?dir=${encodeURIComponent(dir)}`)
        .then((l) => live && setPast(l))
        .catch(() => live && setPast([]));
    }, 250);
    setResume("");
    setMore(false);
    return () => {
      live = false;
      clearTimeout(t);
    };
  }, [dir]);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    setBusy(true);
    setErr("");
    try {
      await onCreate(name, dir, resume ? { resume, fork } : {});
    } catch (e) {
      setErr((e as Error).message);
      setBusy(false);
    }
  };
  return (
    <div className="modal-bg" onMouseDown={onClose}>
      <form
        className="modal"
        onMouseDown={(e) => e.stopPropagation()}
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <h3>新对话</h3>
        <p className="muted">在服务器上新开一个 tmux 会话并启动 claude。</p>
        <label>
          名字（可空）
          <input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="例如：修复登录" />
        </label>
        <label>
          工作目录
          <input list="dir-list" value={dir} onChange={(e) => setDir(e.target.value)} />
          <datalist id="dir-list">
            {dirs.map((d) => (
              <option key={d} value={d} />
            ))}
          </datalist>
        </label>
        <div className="dir-chips">
          {dirs.slice(0, 6).map((d) => (
            <button type="button" key={d} className={`chip ${d === dir ? "on" : ""}`} onClick={() => setDir(d)}>
              {d.split("/").filter(Boolean).pop() || d}
            </button>
          ))}
        </div>
        {past.length > 0 && (
          <div className="past">
            <div className="past-head muted">这个目录以前的对话（点一下接着聊）</div>
            {(more ? past : past.slice(0, 4)).map((p) => (
              <button
                type="button"
                key={p.id}
                className={`past-row ${resume === p.id ? "on" : ""}`}
                onClick={() => setResume(resume === p.id ? "" : p.id)}
              >
                <span className="past-title">{p.title}</span>
                <span className="muted past-meta">
                  {ago(p.at)}
                  {p.open && <span className="past-open"> · 正开在「{p.open}」</span>}
                </span>
                {p.last && <span className="muted past-last">{p.last}</span>}
              </button>
            ))}
            {past.length > 4 && !more && (
              <button type="button" className="ghost past-more" onClick={() => setMore(true)}>
                还有 {past.length - 4} 个…
              </button>
            )}
            {resume && (
              <label className="check">
                <input type="checkbox" checked={fork} onChange={(e) => setFork(e.target.checked)} />
                分叉成一个新对话（原来那段不受影响，适合试另一种做法）
              </label>
            )}
          </div>
        )}
        {sys?.available && (
          <div className={`new-mem ${memTight(sys) ? "error" : "muted"}`}>
            服务器还剩 {fmtBytes(sys.memAvail)} 内存{avg > 0 && `，每个会话平均占 ${fmtBytes(avg)}`}
            {memTight(sys) && "，有点紧张，建议先关掉不用的会话"}
          </div>
        )}
        {err && <div className="error">{err}</div>}
        <div className="modal-actions">
          <button type="button" className="ghost" onClick={onClose}>
            取消
          </button>
          <button className="primary" disabled={busy}>
            {busy ? "创建中…" : resume ? (fork ? "分叉" : "接着聊") : "创建"}
          </button>
        </div>
      </form>
    </div>
  );
}

function ago(at: number) {
  const s = Date.now() / 1000 - at;
  if (s < 60) return "刚刚";
  if (s < 3600) return `${Math.floor(s / 60)} 分钟前`;
  if (s < 86400) return `${Math.floor(s / 3600)} 小时前`;
  if (s < 86400 * 30) return `${Math.floor(s / 86400)} 天前`;
  return new Date(at * 1000).toLocaleDateString();
}
