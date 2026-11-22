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
import Usage from "./components/Usage";

type Tab = "chat" | "poker" | "pet" | "usage";
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
  const [newOpen, setNewOpen] = useState(false);
  const [drawer, setDrawer] = useState(false); // phone: session list slides in
  const [toasts, setToasts] = useState<Toast[]>([]);
  const viewRef = useRef({ tab, active });
  const ctxAlerted = useRef<Record<string, boolean>>({});
  viewRef.current = { tab, active };

  const toast = useCallback((msg: string, err?: boolean) => {
    const id = Date.now() + Math.random();
    setToasts((t) => [...t.slice(-3), { id, msg, err }]);
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), err ? 5000 : 3000);
  }, []);

  useEffect(() => save("tab", tab), [tab]);
  useEffect(() => save("active", active), [active]);

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
        case "sessions": {
          const list = data as Session[];
          // warn once when a conversation's context crosses 85%
          for (const x of list) {
            const pct = x.context?.pct ?? 0;
            if (pct >= 85 && !ctxAlerted.current[x.name]) {
              ctxAlerted.current[x.name] = true;
              void notify("⚠️ 上下文快满了", `${x.name} 已用 ${Math.round(pct)}%，建议 /compact`);
            } else if (pct < 70) ctxAlerted.current[x.name] = false;
          }
          setSt((s) => (s ? { ...s, sessions: list } : s));
          break;
        }
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
          } else if (to === "error") {
            chime("ask");
            if (!looking) void notify("❌ Claude 出错了", name);
          }
          break;
        }
      }
    }, setOnline);
  }, [auth]);

  // a tapped notification opens its session (?s=… on launch, or a message
  // from the service worker when the app is already open)
  useEffect(() => {
    if (!st) return;
    const want = new URLSearchParams(location.search).get("s");
    if (want && st.sessions.some((x) => x.name === want)) {
      open(want);
      history.replaceState(null, "", "/");
    }
    const onMsg = (e: MessageEvent) => {
      const name = (e.data as { open?: string })?.open;
      if (name) open(name);
    };
    navigator.serviceWorker?.addEventListener("message", onMsg);
    return () => navigator.serviceWorker?.removeEventListener("message", onMsg);
  }, [!!st]);

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
    setDrawer(false);
  };

  if (auth === "checking") return <div className="splash">连接中…</div>;
  if (auth === "need") return <Login onDone={() => setAuth("checking")} />;
  if (!st) return <div className="splash">{online ? "加载中…" : "连不上服务器，重试中…"}</div>;

  const anyWorking = st.sessions.some((s) => s.status === "working");
  const petSignals = { working: anyWorking, waiting: st.sessions.some((s) => s.status === "waiting"), error: st.sessions.some((s) => s.status === "error") };
  const counts = {
    working: st.sessions.filter((s) => s.status === "working").length,
    waiting: st.sessions.filter((s) => s.status === "waiting").length,
  };

  return (
    <div className="app">
      {drawer && <div className="drawer-bg" onClick={() => setDrawer(false)} />}
      <aside className={`sidebar ${drawer ? "open" : ""}`}>
        <div className="brand">
          <img src="/icon.svg" alt="" />
          <span>Claude Hub</span>
          <span className={`online ${online ? "on" : ""}`} title={online ? "已连接" : "断开，重连中"} />
        </div>
        <Sidebar
          sessions={st.sessions}
          hooks={st.hooks}
          onToast={toast}
          active={tab === "chat" ? active : null}
          onOpen={open}
          onNew={() => {
            setDrawer(false);
            setNewOpen(true);
          }}
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
        <MiniPet
          pet={st.pet}
          hats={st.hats}
          signals={petSignals}
          onClick={() => {
            setTab("pet");
            setDrawer(false);
          }}
        />
      </aside>

      <main className="main">
        <nav className="tabs">
          <button className="menu-btn" onClick={() => setDrawer(true)} aria-label="会话列表">
            <i>☰</i>
            <span className="tl">会话</span>
            {counts.waiting > 0 && <span className="badge waiting">{counts.waiting}</span>}
          </button>
          <button className={tab === "chat" ? "on" : ""} onClick={() => setTab("chat")}>
            <i>💬</i>
            <span className="tl">对话</span>
            {counts.working > 0 && <span className="badge working">{counts.working}</span>}
            {counts.waiting > 0 && <span className="badge waiting">{counts.waiting}</span>}
          </button>
          <button className={tab === "poker" ? "on" : ""} onClick={() => setTab("poker")}>
            <i>🃏</i>
            <span className="tl">德州</span>
            {st.poker.toAct === 0 && st.poker.street !== "showdown" && <span className="badge waiting">!</span>}
          </button>
          <button className={tab === "pet" ? "on" : ""} onClick={() => setTab("pet")}>
            <i>🐾</i>
            <span className="tl">{st.pet.name}</span>
          </button>
          <button className={tab === "usage" ? "on" : ""} onClick={() => setTab("usage")}>
            <i>📊</i>
            <span className="tl">用量</span>
          </button>
          <span className="spacer" />
          <span className="chips" title="筹码：德州和宠物商店通用">
            🪙 {st.poker.stats.chips.toLocaleString()}
          </span>
        </nav>

        <div className="content">
          <div className="terms" style={{ display: tab === "chat" ? "flex" : "none" }}>
            {opened.map((n) => (
              <Terminal key={n} session={n} info={st.sessions.find((x) => x.name === n)} visible={tab === "chat" && active === n} onToast={toast} />
            ))}
            {(!active || !opened.includes(active)) && (
              <div className="placeholder">
                <p className="desktop-only">从左边选一个会话，或者</p>
                <p className="phone-only">点下方「☰ 会话」选一个，或者</p>
                <button className="primary" onClick={() => setNewOpen(true)}>
                  ＋ 新对话
                </button>
                <p className="muted small">图片：拖进终端、⌘V 粘贴截图，或点终端右上角 📎</p>
              </div>
            )}
          </div>
          {tab === "usage" && <Usage onToast={toast} />}
          {tab === "poker" && <Poker v={st.poker} onToast={toast} />}
          {tab === "pet" && <Pet pet={st.pet} foods={st.foods} hats={st.hats} chips={st.poker.stats.chips} signals={petSignals} onToast={toast} />}
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
