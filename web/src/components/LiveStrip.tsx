import type { Session } from "../types";
import { groupSessions } from "./Sidebar";

// While playing poker or looking at the pet / usage on a narrow window or the
// phone (no room for the docked terminal), show what each Claude session is
// doing in one slim strip; a tap jumps to its terminal.

const label: Record<string, string> = { working: "工作中", waiting: "等你确认", idle: "空闲", error: "出错" };

export default function LiveStrip({ sessions, onOpen }: { sessions: Session[]; onOpen: (name: string) => void }) {
  const list = groupSessions(sessions)
    .flatMap((g) => g.list)
    .sort((a, b) => Number(b.status === "waiting") - Number(a.status === "waiting"));
  if (!list.length) return null;
  const shown = list.slice(0, 3);
  return (
    <div className="live-strip">
      {shown.map((s) => (
        <button key={s.name} className={`live-item ${s.status}`} onClick={() => onOpen(s.name)} title="打开这个会话的终端">
          <span className={`dot ${s.status}`} />
          <span className="live-name">{s.name}</span>
          <span className="live-what">
            {s.status === "waiting" && s.prompt ? `等你确认：${s.prompt.question}` : s.activity || (s.status === "idle" && s.summary?.last) || label[s.status] || ""}
          </span>
        </button>
      ))}
      {list.length > shown.length && <span className="live-more muted">+{list.length - shown.length}</span>}
    </div>
  );
}
