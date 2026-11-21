import { useState } from "react";
import type { Session } from "../types";

type Props = {
  sessions: Session[];
  active: string | null;
  showAll: boolean;
  setShowAll: (v: boolean) => void;
  onOpen: (name: string) => void;
  onNew: () => void;
  onKill: (name: string) => void;
  onRename: (name: string, to: string) => void;
};

const statusText: Record<string, string> = { working: "工作中", waiting: "等你确认", idle: "空闲", "": "" };

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
              <div className="session-sub">
                {s.claude ? statusText[s.status] : s.command} · {shortPath(s.path)}
              </div>
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
      {(hidden > 0 || p.showAll) && (
        <label className="show-all">
          <input type="checkbox" checked={p.showAll} onChange={(e) => p.setShowAll(e.target.checked)} />
          显示其他 tmux 会话{hidden > 0 ? `（${hidden}）` : ""}
        </label>
      )}
    </div>
  );
}
