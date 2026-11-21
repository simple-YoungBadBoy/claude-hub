import { useState } from "react";

type Props = {
  dirs: string[];
  onCreate: (name: string, dir: string) => Promise<void>;
  onClose: () => void;
};

export default function NewSession({ dirs, onCreate, onClose }: Props) {
  const [name, setName] = useState("");
  const [dir, setDir] = useState(dirs[0] ?? "~");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    setBusy(true);
    setErr("");
    try {
      await onCreate(name, dir);
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
        {err && <div className="error">{err}</div>}
        <div className="modal-actions">
          <button type="button" className="ghost" onClick={onClose}>
            取消
          </button>
          <button className="primary" disabled={busy}>
            {busy ? "创建中…" : "创建"}
          </button>
        </div>
      </form>
    </div>
  );
}
