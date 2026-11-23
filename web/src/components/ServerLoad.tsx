import { useEffect, useState } from "react";
import { api } from "../api";

// How loaded the server is: claude sessions are heavy and the box may be small.

export type SysInfo = {
  available: boolean;
  cpus: number;
  load1: number;
  memTotal: number;
  memAvail: number;
  swapTotal: number;
  swapFree: number;
  hubRss: number;
  sessions: { name: string; rss: number; procs: number }[];
};

export const fmtBytes = (n: number) => (n >= 1 << 30 ? `${(n / (1 << 30)).toFixed(1)} GB` : `${Math.round(n / (1 << 20))} MB`);

export function useSysInfo(everyMs: number) {
  const [s, setS] = useState<SysInfo | null>(null);
  useEffect(() => {
    let live = true;
    const load = () => {
      if (document.hidden) return;
      api<SysInfo>("/api/system")
        .then((d) => live && setS(d))
        .catch(() => {});
    };
    load();
    const t = setInterval(load, everyMs);
    return () => {
      live = false;
      clearInterval(t);
    };
  }, [everyMs]);
  return s;
}

/** memory is tight when less than 15% (or 500 MB) is left */
export const memTight = (s: SysInfo) => s.memAvail < Math.max(s.memTotal * 0.15, 500 << 20);

export default function ServerLoad() {
  const s = useSysInfo(5000);
  if (!s?.available) return null;
  const used = s.memTotal - s.memAvail;
  const pct = Math.round((used / s.memTotal) * 100);
  const busy = s.load1 / s.cpus;
  const heavy = s.sessions.filter((x) => x.rss > 0);
  const top = Math.max(1, ...heavy.map((x) => x.rss));
  return (
    <section className="usage-block server-load">
      <h4>服务器</h4>
      <div className="sl-row">
        <span className="sl-k">内存</span>
        <div className="sl-bar">
          <div className={`sl-fill ${memTight(s) ? "bad" : pct > 70 ? "warn" : ""}`} style={{ width: `${pct}%` }} />
        </div>
        <span className="sl-v">
          还剩 <b>{fmtBytes(s.memAvail)}</b> / {fmtBytes(s.memTotal)}
        </span>
      </div>
      <div className="sl-row">
        <span className="sl-k">CPU</span>
        <div className="sl-bar">
          <div className={`sl-fill ${busy > 1.5 ? "bad" : busy > 0.8 ? "warn" : ""}`} style={{ width: `${Math.min(100, busy * 100)}%` }} />
        </div>
        <span className="sl-v">
          负载 <b>{s.load1.toFixed(2)}</b> / {s.cpus} 核
        </span>
      </div>
      {s.swapTotal > 0 && (
        <div className="sl-note muted">
          交换区已用 {fmtBytes(s.swapTotal - s.swapFree)} / {fmtBytes(s.swapTotal)}
        </div>
      )}
      {memTight(s) && <div className="sl-note error">内存紧张：先关掉不用的会话，再开新的</div>}
      {heavy.length > 0 && (
        <table className="usage-table sl-sessions">
          <tbody>
            {heavy.map((x) => (
              <tr key={x.name}>
                <td className="usage-name" title={x.name}>
                  {x.name}
                </td>
                <td className="sl-mini">
                  <div className="sl-bar">
                    <div className="sl-fill" style={{ width: `${(x.rss / top) * 100}%` }} />
                  </div>
                </td>
                <td className="num">{fmtBytes(x.rss)}</td>
                <td className="num muted">{x.procs} 个进程</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <div className="sl-note muted">Claude Hub 自己占 {fmtBytes(s.hubRss)}</div>
    </section>
  );
}
