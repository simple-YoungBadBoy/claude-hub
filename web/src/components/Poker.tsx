import { useEffect, useRef, useState } from "react";
import { api } from "../api";
import type { PokerView } from "../types";
import { play, pokerSounds, setSoundOn, soundOn, unlock } from "../sound";

const SUIT: Record<string, string> = { s: "♠", h: "♥", d: "♦", c: "♣" };
const STREET: Record<string, string> = { idle: "等待开局", preflop: "翻牌前", flop: "翻牌", turn: "转牌", river: "河牌", showdown: "本手结束" };
const STYLE: Record<string, string> = { loose: "松凶", rock: "超紧", tight: "紧凶", maniac: "疯狂" };

export function CardView({ c, small }: { c?: string; small?: boolean }) {
  if (!c) return <div className={`card empty ${small ? "small" : ""}`} />;
  if (c === "??") return <div className={`card back ${small ? "small" : ""}`} />;
  const r = c[0] === "T" ? "10" : c[0];
  const s = c[1];
  return (
    <div className={`card ${s === "h" || s === "d" ? "red" : ""} ${small ? "small" : ""}`}>
      <span className="card-rank">{r}</span>
      <span className="card-suit">{SUIT[s]}</span>
    </div>
  );
}

// seat positions around the oval (human at bottom)
const SEATS = [
  { left: "50%", top: "88%" },
  { left: "8%", top: "62%" },
  { left: "18%", top: "10%" },
  { left: "82%", top: "10%" },
  { left: "92%", top: "62%" },
];

export default function Poker({ v, onToast }: { v: PokerView; onToast: (m: string, err?: boolean) => void }) {
  const legal = v.legal;
  const [raise, setRaise] = useState(0);
  useEffect(() => {
    if (legal) setRaise(legal.minRaiseTo);
  }, [legal?.minRaiseTo, v.hand, v.street]);

  // sound effects: compare with the view before this one
  const [sound, setSound] = useState(soundOn);
  const prev = useRef<PokerView>();
  useEffect(() => {
    play(pokerSounds(prev.current, v));
    prev.current = v;
  }, [v]);

  const send = async (path: string, body?: object) => {
    unlock(); // iOS: audio only starts from a tap
    try {
      await api(`/api/poker/${path}`, body ?? {});
    } catch (e) {
      onToast((e as Error).message, true);
    }
  };
  const act = (action: string, amount = 0) => send("act", { action, amount });

  const between = v.street === "idle" || v.street === "showdown";
  const me = v.players[0];
  const winnerIds = new Set((v.winners ?? []).map((w) => w.id));

  // keyboard: F fold, C check/call, R raise, Space/N next hand
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement;
      if (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.isContentEditable || e.metaKey || e.ctrlKey) return; // typing elsewhere (input bar, terminal)
      if (between && (e.key === "n" || e.key === " ")) {
        e.preventDefault();
        void send("start");
      }
      if (!legal) return;
      if (e.key === "f") void act("fold");
      if (e.key === "c") void act(legal.canCheck ? "check" : "call");
      if (e.key === "r") void act("raise", raise);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  const presets = legal
    ? [
        { label: "½池", to: v.curBet + Math.floor((v.pot + (v.curBet - me.bet)) / 2) },
        { label: "¾池", to: v.curBet + Math.floor(((v.pot + (v.curBet - me.bet)) * 3) / 4) },
        { label: "1池", to: v.curBet + v.pot + (v.curBet - me.bet) },
      ].map((p) => ({ ...p, to: Math.max(legal.minRaiseTo, Math.min(legal.maxRaiseTo, p.to)) }))
    : [];

  return (
    <div className="poker">
      <div className="poker-main">
        <div className="table">
          <button
            className="ghost sound-toggle"
            title={sound ? "关掉音效" : "打开音效"}
            onClick={() => {
              setSoundOn(!sound);
              setSound(!sound);
            }}
          >
            {sound ? "🔊" : "🔇"}
          </button>
          <div className="felt">
            <div className="board">
              {[0, 1, 2, 3, 4].map((i) => (
                <CardView key={i} c={v.board[i]} />
              ))}
            </div>
            <div className="pot">底池 {v.pot.toLocaleString()}</div>
            <div className="street">
              {STREET[v.street]}
              {v.hand > 0 ? ` · 第 ${v.hand} 手` : ""}
            </div>
          </div>
          {v.players.map((p, i) => (
            <div
              key={p.id}
              className={`seat ${v.toAct === p.id ? "acting" : ""} ${p.folded ? "folded" : ""} ${winnerIds.has(p.id) ? "winner" : ""} ${p.id === 0 ? "me" : ""}`}
              style={SEATS[i]}
            >
              <div className="seat-cards">
                {p.hole.length ? p.hole.map((c, j) => <CardView key={j} c={c} small={p.id !== 0} />) : <div className="no-cards" />}
              </div>
              <div className="seat-info">
                <div className="seat-name">
                  {p.name}
                  {v.dealer === p.id && <span className="dealer">D</span>}
                  {p.bot && p.style && <span className="muted small"> {STYLE[p.style]}</span>}
                </div>
                <div className="seat-chips">{p.chips.toLocaleString()}</div>
                {p.lastAction && <div className="seat-action">{p.lastAction}</div>}
                {winnerIds.has(p.id) && (
                  <div className="seat-win">
                    +{v.winners!.find((w) => w.id === p.id)!.amount}
                    {v.winners!.find((w) => w.id === p.id)!.hand && ` ${v.winners!.find((w) => w.id === p.id)!.hand}`}
                  </div>
                )}
              </div>
              {p.bet > 0 && <div className="seat-bet">{p.bet}</div>}
            </div>
          ))}
        </div>

        <div className="actions">
          {v.myHand && <span className="my-hand">{v.myHand}</span>}
          {between ? (
            <>
              {v.canRelief ? (
                <button className="primary" onClick={() => send("relief")}>
                  领救济 1000
                </button>
              ) : (
                <button className="primary" onClick={() => send("start")} disabled={me.chips < 20}>
                  {v.hand === 0 ? "开始" : "下一手"} <kbd>N</kbd>
                </button>
              )}
              {me.chips < 20 && !v.canRelief && <span className="muted">筹码不足，救济每小时可领一次</span>}
            </>
          ) : legal ? (
            <>
              <button className="danger" onClick={() => act("fold")}>
                弃牌 <kbd>F</kbd>
              </button>
              <button className="ghost" onClick={() => act(legal.canCheck ? "check" : "call")}>
                {legal.canCheck ? "过牌" : `跟注 ${legal.callAmount}`} <kbd>C</kbd>
              </button>
              {legal.maxRaiseTo > v.curBet && (
                <div className="raise">
                  {presets.map((p) => (
                    <button key={p.label} className="chip" onClick={() => setRaise(p.to)}>
                      {p.label}
                    </button>
                  ))}
                  <input
                    type="range"
                    min={legal.minRaiseTo}
                    max={legal.maxRaiseTo}
                    step={10}
                    value={raise}
                    onChange={(e) => setRaise(+e.target.value)}
                  />
                  <input
                    type="number"
                    className="raise-num"
                    value={raise}
                    min={legal.minRaiseTo}
                    max={legal.maxRaiseTo}
                    onChange={(e) => setRaise(+e.target.value)}
                  />
                  <button className="primary" onClick={() => act("raise", raise)}>
                    {raise >= legal.maxRaiseTo ? "全下" : v.curBet === 0 ? "下注" : "加注到"} {raise >= legal.maxRaiseTo ? "" : raise} <kbd>R</kbd>
                  </button>
                </div>
              )}
            </>
          ) : (
            <span className="muted">等待 {v.players[v.toAct]?.name ?? ""}…</span>
          )}
        </div>
      </div>
      <aside className="poker-side">
        <div className="stats">
          <div>
            <b>{v.stats.chips.toLocaleString()}</b>
            <span className="muted small">筹码</span>
          </div>
          <div>
            <b>{v.stats.hands}</b>
            <span className="muted small">手数</span>
          </div>
          <div>
            <b>{v.stats.won}</b>
            <span className="muted small">赢</span>
          </div>
          <div>
            <b>{v.stats.biggestPot.toLocaleString()}</b>
            <span className="muted small">最大底池</span>
          </div>
        </div>
        <div className="log">
          {[...v.log].reverse().map((l, i) => (
            <div key={i} className={l.startsWith("——") ? "log-hand" : ""}>
              {l}
            </div>
          ))}
        </div>
        <div className="muted small">盲注 10/20 · 筹码和宠物商店通用</div>
      </aside>
    </div>
  );
}
