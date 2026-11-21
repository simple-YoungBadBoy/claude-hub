import { useCallback, useEffect, useRef, useState } from "react";
import { api, connectEvents, Unauthorized } from "./api";
import type { HubState, PetState, PokerView, Session } from "./types";
import { chime, notify } from "./notify";
import Login from "./components/Login";
import Sidebar from "./components/Sidebar";
import NewSession from "./components/NewSession";
import Terminal from "./components/Terminal";
import Poker from "./components/Poker";
import Pet, { MiniPet } from "./components/Pet";

type Tab = "chat" | "poker" | "pet";
type Toast = { id: number; msg: string; err?: boolean };

const load = <T,>(k: string, d: T): T => {
  try {
    const v = localStorage.getItem(k);
    return v ? (JSON.parse(v) as T) : d;
  } catch {
    return d;
  }
};
const save = (k: string, v: unknown) => {
  try {
    localStorage.setItem(k, JSON.stringify(v));
  } catch {
    /* ignore */
  }
};

export default function App() {
  const [auth, setAuth] = useState<"checking" | "need" | "ok">("checking");
  const [st, setSt] = useState<HubState | null>(null);
  const [online, setOnline] = useState(false);
  const [tab, setTab] = useState<Tab>(() => load("tab", "chat"));
  const [active, setActive] = useState<string | null>(() => load("active", null));
  const [opened, setOpened] = useState<string[]>(() => (load<string | null>("active", null) ? [load("active", "")] : []));
  const [showAll, setShowAll] = useState(() => load("showAll", false));
  const [newOpen, setNewOpen] = useState(false);
  const [toasts, setToasts] = useState<Toast[]>([]);
  const viewRef = useRef({ tab, active });
  viewRef.current = { tab, active };

  const toast = useCallback((msg: string, err?: boolean) => {
    const id = Date.now() + Math.random();
    setToasts((t) => [...t.slice(-3), { id, msg, err }]);
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), err ? 5000 : 3000);
  }, []);

  useEffect(() => save("tab", tab), [tab]);
  useEffect(() => save("active", active), [active]);
  useEffect(() => save("showAll", showAll), [showAll]);

  // auth check
  useEffect(() => {
    if (auth !== "checking") return;
    api<HubState>("/api/state")
      .then((s) => {
        setSt(s);
        setAuth("ok");
      })
      .catch((e) => setAuth(e instanceof Unauthorized ? "need" : "ok"));
  }, [auth]);

  // live events
  useEffect(() => {
    if (auth !== "ok") return;
    return connectEvents((type, data) => {
      switch (type) {
        case "state":
          setSt(data as HubState);
          break;
        case "sessions":
          setSt((s) => (s ? { ...s, sessions: data as Session[] } : s));
          break;
        case "poker":
          setSt((s) => (s ? { ...s, poker: data as PokerView } : s));
          break;
        case "pet":
          setSt((s) => (s ? { ...s, pet: data as PetState } : s));
          break;
        case "status": {
          const { name, from, to } = data as { name: string; from: string; to: string };
          const looking = document.visibilityState === "visible" && document.hasFocus() && viewRef.current.tab === "chat" && viewRef.current.active === name;
          if (from === "working" && to === "idle") {
            chime("done");
            if (!looking) void notify("✅ Claude 完成了", name);
          } else if (to === "waiting") {
            chime("ask");
            if (!looking) void notify("❓ Claude 需要你确认", name);
          }
          break;
        }
      }
    }, setOnline);
  }, [auth]);

  // drop terminals for sessions that no longer exist
  useEffect(() => {
    if (!st) return;
    const names = new Set(st.sessions.map((s) => s.name));
    setOpened((o) => o.filter((n) => names.has(n)));
    if (active && !names.has(active)) setActive(null);
  }, [st?.sessions]);

  const open = (name: string) => {
    setOpened((o) => (o.includes(name) ? o : [...o.slice(-7), name]));
    setActive(name);
    setTab("chat");
  };

  if (auth === "checking") return <div className="splash">连接中…</div>;
  if (auth === "need") return <Login onDone={() => setAuth("checking")} />;
  if (!st) return <div className="splash">{online ? "加载中…" : "连不上服务器，重试中…"}</div>;

  const anyWorking = st.sessions.some((s) => s.status === "working");
  const counts = {
    working: st.sessions.filter((s) => s.status === "working").length,
    waiting: st.sessions.filter((s) => s.status === "waiting").length,
  };

  return (
    <div className="app">
      <aside className="sidebar">
        <div className="brand">
          <img src="/icon.svg" alt="" />
          <span>Claude Hub</span>
          <span className={`online ${online ? "on" : ""}`} title={online ? "已连接" : "断开，重连中"} />
        </div>
        <Sidebar
          sessions={st.sessions}
          active={tab === "chat" ? active : null}
          showAll={showAll}
          setShowAll={setShowAll}
          onOpen={open}
          onNew={() => setNewOpen(true)}
          onKill={async (n) => {
            try {
              await api(`/api/sessions/${encodeURIComponent(n)}`, {}, "DELETE");
            } catch (e) {
              toast((e as Error).message, true);
            }
          }}
          onRename={async (n, to) => {
            try {
              const r = await api<{ name: string }>(`/api/sessions/${encodeURIComponent(n)}/rename`, { name: to });
              setOpened((o) => o.map((x) => (x === n ? r.name : x)));
              if (active === n) setActive(r.name);
            } catch (e) {
              toast((e as Error).message, true);
            }
          }}
        />
        <MiniPet pet={st.pet} hats={st.hats} anyWorking={anyWorking} onClick={() => setTab("pet")} />
      </aside>

      <main className="main">
        <nav className="tabs">
          <button className={tab === "chat" ? "on" : ""} onClick={() => setTab("chat")}>
            💬 对话
            {counts.working > 0 && <span className="badge working">{counts.working}</span>}
            {counts.waiting > 0 && <span className="badge waiting">{counts.waiting}</span>}
          </button>
          <button className={tab === "poker" ? "on" : ""} onClick={() => setTab("poker")}>
            🃏 德州
            {st.poker.toAct === 0 && st.poker.street !== "showdown" && <span className="badge waiting">!</span>}
          </button>
          <button className={tab === "pet" ? "on" : ""} onClick={() => setTab("pet")}>
            🐾 {st.pet.name}
          </button>
          <span className="spacer" />
          <span className="chips" title="筹码：德州和宠物商店通用">
            🪙 {st.poker.stats.chips.toLocaleString()}
          </span>
        </nav>

        <div className="content">
          <div className="terms" style={{ display: tab === "chat" ? "flex" : "none" }}>
            {opened.map((n) => (
              <Terminal key={n} session={n} visible={tab === "chat" && active === n} onToast={toast} />
            ))}
            {(!active || !opened.includes(active)) && (
              <div className="placeholder">
                <p>从左边选一个会话，或者</p>
                <button className="primary" onClick={() => setNewOpen(true)}>
                  ＋ 新对话
                </button>
                <p className="muted small">图片：拖进终端、⌘V 粘贴截图，或点终端右上角 📎</p>
              </div>
            )}
          </div>
          {tab === "poker" && <Poker v={st.poker} onToast={toast} />}
          {tab === "pet" && <Pet pet={st.pet} foods={st.foods} hats={st.hats} chips={st.poker.stats.chips} anyWorking={anyWorking} onToast={toast} />}
        </div>
      </main>

      {newOpen && (
        <NewSession
          dirs={st.dirs}
          onClose={() => setNewOpen(false)}
          onCreate={async (name, dir) => {
            const r = await api<{ name: string }>("/api/sessions", { name, dir });
            setNewOpen(false);
            open(r.name);
          }}
        />
      )}

      <div className="toasts">
        {toasts.map((t) => (
          <div key={t.id} className={`toast ${t.err ? "err" : ""}`}>
            {t.msg}
          </div>
        ))}
      </div>
    </div>
  );
}
