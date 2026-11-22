import { useState } from "react";
import type { HubState, Session } from "../types";
import PushToggle from "./PushToggle";

type Props = {
  sessions: Session[];
  hooks: HubState["hooks"];
  active: string | null;
  onOpen: (name: string) => void;
  onNew: (dir?: string) => void;
  onKill: (name: string) => void;
  onRename: (name: string, to: string) => void;
  onToast: (msg: string, err?: boolean) => void;
};

const statusText: Record<string, string> = { working: "工作中", waiting: "等你确认", idle: "空闲", error: "出错", "": "" };

function fmtK(n: number) {
  return n >= 1_000_000 ? `${(n / 1_000_000).toFixed(1)}M` : `${Math.round(n / 1000)}k`;
}

function ContextBar({ c }: { c: NonNullable<Session["context"]> }) {
  const level = c.pct >= 85 ? "high" : c.pct >= 70 ? "mid" : "low";
  return (
    <div className={`ctx ctx-${level}`} title={`上下文 ${fmtK(c.tokens)} / ${fmtK(c.limit)}${c.pct >= 85 ? "，建议 /compact" : ""}`}>
      <div className="ctx-fill" style={{ width: `${Math.max(2, c.pct)}%` }} />
      <span>{Math.round(c.pct)}%</span>
    </div>
  );
}

function shortPath(p: string) {
  const parts = p.split("/").filter(Boolean);
  return parts.length > 2 ? "…/" + parts.slice(-2).join("/") : p;
}

const rank: Record<string, number> = { waiting: 0, error: 1, working: 2, idle: 3, "": 4 };

/** Claude sessions grouped by project directory, the ones that need you first. */
export function groupSessions(sessions: Session[]) {
  const by = new Map<string, Session[]>();
  for (const s of sessions.filter((x) => x.claude)) by.set(s.path, [...(by.get(s.path) ?? []), s]);
  const order = (a: Session, b: Session) => rank[a.status] - rank[b.status] || b.created - a.created;
  const groups = [...by.entries()].map(([dir, list]) => ({ dir, list: list.sort(order) }));
  return groups.sort((a, b) => order(a.list[0], b.list[0]));
}

function age(created: number) {
  const m = Math.max(0, Math.floor((Date.now() / 1000 - created) / 60));
  return m < 60 ? `${m}分` : m < 24 * 60 ? `${Math.floor(m / 60)}时` : `${Math.floor(m / 1440)}天`;
}

function base(p: string) {
  return p.split("/").filter(Boolean).pop() || p || "/";
}

export default function Sidebar(p: Props) {
  const [editing, setEditing] = useState<string | null>(null);
  const groups = groupSessions(p.sessions);
  const others = p.sessions.filter((s) => !s.claude);
  const keyOf = new Map(groups.flatMap((g) => g.list).map((s, i) => [s.name, i + 1]));
  const row = (s: Session) => (
    <div
      key={s.name}
      className={`session ${p.active === s.name ? "active" : ""}`}
      onClick={() => p.onOpen(s.name)}
      title={`${s.path}\n${s.command}${s.attached ? `\n${s.attached} 个终端已连接` : ""}`}
    >
      <span className={`dot ${s.claude ? s.status : "other"}`} />
      <div className="session-main">
        {editing === s.name ? (
          <input
            autoFocus
            defaultValue={s.name}
            onClick={(e) => e.stopPropagation()}
            onBlur={() => setEditing(null)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                const v = (e.target as HTMLInputElement).value.trim();
                if (v && v !== s.name) p.onRename(s.name, v);
                setEditing(null);
              } else if (e.key === "Escape") setEditing(null);
            }}
          />
        ) : (
          <div className="session-name" onDoubleClick={() => setEditing(s.name)}>
            {s.name}
          </div>
        )}
        {s.claude && s.summary?.title && <div className="session-title">{s.summary.title}</div>}
        {s.claude && s.status === "idle" && s.summary?.last && (
          <div className="session-last" title={s.summary.last}>
            ↳ {s.summary.last}
          </div>
        )}
        <div className="session-sub" title={s.activity || undefined}>
          {s.claude ? (
            <>
              {s.activity || statusText[s.status]} · ⏱ {age(s.created)}
              {(keyOf.get(s.name) ?? 99) <= 9 && <kbd className="desktop-only">⌘{keyOf.get(s.name)}</kbd>}
            </>
          ) : (
            <>
              {s.command} · {shortPath(s.path)}
            </>
          )}
        </div>
        {s.claude && s.context && <ContextBar c={s.context} />}
      </div>
      <button
        className="icon kill"
        title="关闭会话"
        onClick={(e) => {
          e.stopPropagation();
          if (confirm(`关闭会话「${s.name}」？里面${s.claude ? "的 Claude" : "运行的程序"}会被结束。`)) p.onKill(s.name);
        }}
      >
        ×
      </button>
    </div>
  );
  return (
    <div className="sessions">
      <button className="primary new-btn" onClick={() => p.onNew()}>
        ＋ 新对话
      </button>
      <div className="session-list">
        {groups.length === 0 && <div className="empty">还没有 Claude 会话</div>}
        {groups.map((g) => (
          <div className="project" key={g.dir}>
            <div className="project-head" title={g.dir}>
              <span className="project-name">📁 {base(g.dir)}</span>
              <span className="project-count">{g.list.length}</span>
              <button className="icon" title={`在 ${g.dir} 里新开一个对话`} onClick={() => p.onNew(g.dir)}>
                ＋
              </button>
            </div>
            {g.list.map(row)}
          </div>
        ))}
        {others.length > 0 && (
          <div className="session-group" title="服务器上直接开的 tmux 会话（没在运行 claude）">
            其他 tmux 会话（{others.length}）
          </div>
        )}
        {others.map(row)}
      </div>
      {p.hooks.installed < p.hooks.total && p.sessions.some((s) => s.claude) && !p.sessions.some((s) => s.hooked) && (
        <div className="hooks-hint" title="在服务器上运行：~/claude-hub/bin/claude-hub hooks install（或让 Claude 用 hub skill 安装）">
          状态靠读屏识别 · 装上 hooks 更准，还能看上下文占用
        </div>
      )}
      <PushToggle onToast={p.onToast} />
    </div>
  );
}
