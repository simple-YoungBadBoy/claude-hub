import { useState } from "react";
import { api } from "../api";

export default function Login({ onDone }: { onDone: () => void }) {
  const [tok, setTok] = useState("");
  const [err, setErr] = useState("");
  return (
    <div className="login">
      <form
        className="modal"
        onSubmit={async (e) => {
          e.preventDefault();
          setErr("");
          try {
            await api("/api/login", { token: tok });
            onDone();
          } catch (e) {
            setErr((e as Error).message);
          }
        }}
      >
        <h3>Claude Hub</h3>
        <p className="muted">
          输入服务器上 <code>~/claude-hub/data/token</code> 里的口令（只需一次）。
        </p>
        <input autoFocus type="password" value={tok} onChange={(e) => setTok(e.target.value)} placeholder="token" />
        {err && <div className="error">{err}</div>}
        <div className="modal-actions">
          <button className="primary">连接</button>
        </div>
      </form>
    </div>
  );
}
