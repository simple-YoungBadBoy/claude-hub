import { useCallback, useEffect, useState } from "react";
import ServerLoad from "./ServerLoad";
import { api } from "../api";
import type { UsageCounts, UsageReport } from "../types";

export function fmtTokens(n: number) {
  if (n >= 1e9) return `${(n / 1e9).toFixed(2)}B`;
  if (n >= 1e6) return `${(n / 1e6).toFixed(n >= 1e8 ? 0 : 1)}M`;
  if (n >= 1e3) return `${(n / 1e3).toFixed(n >= 1e5 ? 0 : 1)}k`;
  return String(n);
}

const total = (c: UsageCounts) => c.input + c.output + c.cacheRead + c.cacheWrite;

function shortPath(p: string) {
  const parts = p.split("/").filter(Boolean);
  return parts.length > 2 ? "…/" + parts.slice(-2).join("/") : p;
}

function Breakdown({ title, rows, path }: { title: string; rows: UsageReport["models"]; path?: boolean }) {
  return (
    <section className="usage-block">
      <h4>{title}</h4>
      {rows.length === 0 ? (
        <div className="muted small">今天还没有记录</div>
      ) : (
        <table className="usage-table">
          <thead>
            <tr>
              <th></th>
              <th>请求</th>
              <th>输入</th>
              <th>输出</th>
              <th>缓存写入</th>
              <th>缓存读取</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.name}>
                <td className="usage-name" title={r.name}>
                  {path ? shortPath(r.name) : r.name}
                </td>
                <td>{r.messages.toLocaleString()}</td>
                <td>{fmtTokens(r.input)}</td>
                <td>{fmtTokens(r.output)}</td>
                <td>{fmtTokens(r.cacheWrite)}</td>
                <td>{fmtTokens(r.cacheRead)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

export default function Usage({ onToast }: { onToast: (m: string, err?: boolean) => void }) {
  const [r, setR] = useState<UsageReport | null>(null);
  const [busy, setBusy] = useState(false);
  const [metric, setMetric] = useState<"total" | "output">("total");

  const load = useCallback(async () => {
    setBusy(true);
    try {
      setR(await api<UsageReport>("/api/usage"));
    } catch (e) {
      onToast(`读取用量失败：${(e as Error).message}`, true);
    } finally {
      setBusy(false);
    }
  }, [onToast]);

  useEffect(() => {
    void load();
    const t = setInterval(() => void load(), 30_000);
    return () => clearInterval(t);
  }, [load]);

  if (!r) return <div className="usage-page placeholder">{busy ? "正在统计（第一次要读一会儿）…" : "没有数据"}</div>;

  const t = r.today;
  const value = (c: UsageCounts) => (metric === "total" ? total(c) : c.output);
  const max = Math.max(1, ...r.days.map(value));
  const updated = new Date(r.scannedAt * 1000).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" });
  const cards: [string, number, string][] = [
    ["输出", t.output, "Claude 写出的内容"],
    ["输入", t.input, "未命中缓存的输入"],
    ["缓存写入", t.cacheWrite, "新写入提示缓存"],
    ["缓存读取", t.cacheRead, "命中缓存的上下文，最便宜"],
  ];

  return (
    <div className="usage-page">
      <div className="usage-head">
        <h3>今日用量</h3>
        <span className="muted small">
          {r.days[r.days.length - 1]?.day} · 更新于 {updated} · 来自服务器上所有 Claude Code 对话
        </span>
        <span className="spacer" />
        <button onClick={() => void load()} disabled={busy}>
          {busy ? "统计中…" : "刷新"}
        </button>
      </div>

      <ServerLoad />

      <div className="usage-cards">
        <div className="usage-card big">
          <span className="muted">总 token</span>
          <b>{fmtTokens(total(t))}</b>
          <span className="muted small">
            {t.messages.toLocaleString()} 次请求
            {r.todaySub.messages > 0 && ` · 子代理 ${fmtTokens(total(r.todaySub))}`}
          </span>
        </div>
        {cards.map(([k, v, hint]) => (
          <div key={k} className="usage-card" title={hint}>
            <span className="muted">{k}</span>
            <b>{fmtTokens(v)}</b>
          </div>
        ))}
      </div>

      <section className="usage-block">
        <h4 className="usage-days-head">
          最近 {r.days.length} 天
          <span className="seg">
            <button className={metric === "total" ? "on" : ""} onClick={() => setMetric("total")}>
              总 token
            </button>
            <button className={metric === "output" ? "on" : ""} onClick={() => setMetric("output")}>
              输出
            </button>
          </span>
        </h4>
        <div className="usage-days">
          {r.days.map((d, i) => {
            const v = value(d);
            const today = i === r.days.length - 1;
            return (
              <div
                key={d.day}
                className={`usage-day ${today ? "today" : ""}`}
                title={`${d.day}\n总 ${total(d).toLocaleString()}\n输出 ${d.output.toLocaleString()} · 输入 ${d.input.toLocaleString()}\n缓存写入 ${d.cacheWrite.toLocaleString()} · 缓存读取 ${d.cacheRead.toLocaleString()}\n${d.messages} 次请求`}
              >
                <span className="usage-day-val">{v ? fmtTokens(v) : ""}</span>
                <div className="usage-bar" style={{ height: `${v ? Math.max(3, (v / max) * 100) : 0}%` }} />
                <span className="usage-day-label">{today ? "今天" : d.day.slice(5)}</span>
              </div>
            );
          })}
        </div>
      </section>

      <div className="usage-grid">
        <Breakdown title="按模型" rows={r.models} />
        <Breakdown title="按项目" rows={r.projects} path />
      </div>
      <p className="muted small">只统计 token，不折算费用。数据来自服务器上的 ~/.claude/projects。</p>
    </div>
  );
}
