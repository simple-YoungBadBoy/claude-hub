import { useState } from "react";
import type { HubState, Session } from "../types";

type Props = {
  sessions: Session[];
  hooks: HubState["hooks"];
  active: string | null;
  showAll: boolean;
  setShowAll: (v: boolean) => void;
  onOpen: (name: string) => void;
  onNew: () => void;
  onKill: (name: string) => void;
  onRename: (name: string, to: string) => void;
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

export default function Sidebar(p: Props) {
  const [editing, setEditing] = useState<string | null>(null);
  const list = p.sessions.filter((s) => p.showAll || s.claude);
  const hidden = p.sessions.length - list.length;
  return (
    <div className="sessions">
      <button className="primary new-btn" onClick={p.onNew}>
        ＋ 新对话
      </button>
      <div className="session-list">
        {list.length === 0 && <div className="empty">还没有 Claude 会话</div>}
        {list.map((s) => (
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
              <div className="session-sub" title={s.activity || undefined}>
                {s.claude ? s.activity || statusText[s.status] : s.command} · {shortPath(s.path)}
              </div>
              {s.claude && s.context && <ContextBar c={s.context} />}
            </div>
            <button
              className="icon kill"
              title="关闭会话"
              onClick={(e) => {
                e.stopPropagation();
                if (confirm(`关闭会话「${s.name}」？里面的 Claude 会被结束。`)) p.onKill(s.name);
              }}
            >
              ×
            </button>
          </div>
        ))}
      </div>
      {p.hooks.installed < p.hooks.total && p.sessions.some((s) => s.claude) && !p.sessions.some((s) => s.hooked) && (
        <div className="hooks-hint" title="在服务器上运行：~/claude-hub/bin/claude-hub hooks install（或让 Claude 用 hub skill 安装）">
          状态靠读屏识别 · 装上 hooks 更准，还能看上下文占用
        </div>
      )}
      {(hidden > 0 || p.showAll) && (
        <label className="show-all">
          <input type="checkbox" checked={p.showAll} onChange={(e) => p.setShowAll(e.target.checked)} />
          显示其他 tmux 会话{hidden > 0 ? `（${hidden}）` : ""}
        </label>
      )}
    </div>
  );
}
