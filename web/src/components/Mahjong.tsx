import { useEffect, useRef, useState } from "react";
import { api } from "../api";
import type { MahjongView, MjMeld, MjPlayer } from "../types";
import { mahjongSounds, play, setSoundOn, soundOn, unlock } from "../sound";

// Sichuan bloody-battle mahjong against three bots. Tiles are numbers:
// suit*9 + rank-1, suits 万 条 筒.

const SUIT = ["万", "条", "筒"];
const CN = ["一", "二", "三", "四", "五", "六", "七", "八", "九"];
const suit = (t: number) => Math.floor(t / 9);
const name = (t: number) => `${(t % 9) + 1}${SUIT[suit(t)]}`;

export function Tile({ t, small, sel, dim, hot, onClick }: { t: number; small?: boolean; sel?: boolean; dim?: boolean; hot?: boolean; onClick?: () => void }) {
  const s = suit(t);
  return (
    <button
      type="button"
      className={`tile s${s} ${small ? "small" : ""} ${sel ? "sel" : ""} ${dim ? "dim" : ""} ${hot ? "hot" : ""}`}
      onClick={onClick}
      disabled={!onClick}
      title={name(t)}
      data-tile={t}
    >
      <span className="tile-rank">{s === 0 ? CN[t % 9] : (t % 9) + 1}</span>
      <span className="tile-suit">{SUIT[s]}</span>
    </button>
  );
}

function Melds({ melds }: { melds: MjMeld[] }) {
  return (
    <>
      {melds.map((m, i) => (
        <span key={i} className="mj-meld" title={m.kind === "peng" ? "碰" : m.kind === "angang" ? "暗杠" : "杠"}>
          {Array.from({ length: m.kind === "peng" ? 3 : 4 }, (_, k) =>
            m.kind === "angang" && k !== 3 ? <span key={k} className="tile small back" /> : <Tile key={k} t={m.tile} small />,
          )}
        </span>
      ))}
    </>
  );
}

function Badges({ p, v }: { p: MjPlayer; v: MahjongView }) {
  return (
    <>
      {v.dealer === p.id && v.phase !== "idle" && <span className="dealer">庄</span>}
      {p.que >= 0 && <span className="mj-que">缺{SUIT[p.que]}</span>}
      {p.hu && <span className="mj-hu">胡 {p.huName}</span>}
      {(v.phase === "end" || p.net !== 0) && <span className={`mj-net ${p.net >= 0 ? "up" : "down"}`}>{p.net >= 0 ? `+${p.net}` : p.net}</span>}
    </>
  );
}

const SEAT = ["", "下家", "对家", "上家"];

export default function Mahjong({ v, onToast }: { v: MahjongView; onToast: (m: string, err?: boolean) => void }) {
  const [sel, setSel] = useState<number | null>(null);
  const [sound, setSound] = useState(soundOn);
  const prev = useRef<MahjongView>();
  useEffect(() => {
    play(mahjongSounds(prev.current, v));
    prev.current = v;
    setSel(null);
  }, [v]);

  const send = async (path: string, body?: object) => {
    unlock();
    try {
      await api(`/api/mahjong/${path}`, body ?? {});
    } catch (e) {
      onToast((e as Error).message, true);
    }
  };
  const act = (action: string, tile = -1) => send("act", { action, tile });

  const me = v.players[0];
  const o = v.options;
  const canDiscard = !!o?.discard;
  // my hand: void suit last, so the tiles to throw first sit together
  const order = (t: number) => (suit(t) === me.que ? 100 : 0) + t;
  const hand = [...(me.hand ?? [])].sort((a, b) => order(a) - order(b));
  const tap = (t: number, key: number) => {
    if (!canDiscard) return;
    if (o?.mustQue && suit(t) !== me.que) return onToast(`先把缺的${SUIT[me.que]}打完`, true);
    if (sel === key) void act("discard", t);
    else setSel(key);
  };

  return (
    <div className="poker mj">
      <div className="mj-main">
        <div className="mj-opps">
          {[3, 2, 1].map((id) => {
            const p = v.players[id];
            return (
              <div key={id} className={`mj-opp ${v.turn === id && v.phase === "play" ? "acting" : ""} ${p.hu ? "won" : ""}`}>
                <div className="mj-who">
                  <b>{p.name}</b>
                  <span className="muted small">{SEAT[id]}</span>
                  <Badges p={p} v={v} />
                  <span className="mj-count muted small">{v.phase === "dingque" || v.phase === "play" ? `🀫×${p.count}` : ""}</span>
                  {p.lastAction && <span className="seat-action">{p.lastAction}</span>}
                </div>
                <div className="mj-row">
                  <Melds melds={p.melds} />
                  {p.hand?.map((t, i) => <Tile key={i} t={t} small />)}
                  {p.hu && <Tile t={p.huTile} small hot />}
                </div>
                <div className="mj-discards">
                  {p.discards.map((t, i) => (
                    <Tile key={i} t={t} small hot={v.last?.from === id && i === p.discards.length - 1} />
                  ))}
                </div>
              </div>
            );
          })}
        </div>

        <div className="mj-center">
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
          {v.phase === "idle" ? (
            <span className="muted">四川血战到底 · 108 张 · 定缺 · 可碰杠不可吃</span>
          ) : (
            <>
              <span>
                第 {v.round} 局 · 剩 <b>{v.wall}</b> 张
              </span>
              {v.last && (
                <span className="mj-last">
                  {v.players[v.last.from].name} 打出 <Tile t={v.last.tile} hot />
                </span>
              )}
            </>
          )}
        </div>

        <div className={`mj-me ${v.turn === 0 && v.phase === "play" ? "acting" : ""} ${me.hu ? "won" : ""}`}>
          <div className="mj-who">
            <b>你</b>
            <Badges p={me} v={v} />
            {me.lastAction && <span className="seat-action">{me.lastAction}</span>}
          </div>
          {me.discards.length > 0 && (
            <div className="mj-discards">
              {me.discards.map((t, i) => (
                <Tile key={i} t={t} small hot={v.last?.from === 0 && i === me.discards.length - 1} />
              ))}
            </div>
          )}
          <div className="mj-hand">
            <Melds melds={me.melds} />
            {hand.map((t, i) => (
              <Tile key={i} t={t} sel={sel === i} dim={canDiscard && o?.mustQue && suit(t) !== me.que} onClick={canDiscard ? () => tap(t, i) : undefined} />
            ))}
            {v.drawn >= 0 && (
              <span className="mj-drawn">
                <Tile t={v.drawn} sel={sel === -1} dim={canDiscard && o?.mustQue && suit(v.drawn) !== me.que} onClick={canDiscard ? () => tap(v.drawn, -1) : undefined} />
              </span>
            )}
            {me.hu && <Tile t={me.huTile} hot />}
          </div>
        </div>

        <div className="actions">
          {v.phase === "idle" || v.phase === "end" ? (
            <>
              {v.phase === "end" && (
                <span className={`mj-result ${me.net >= 0 ? "up" : "down"}`}>{me.net >= 0 ? `本局赢 ${me.net}` : `本局输 ${-me.net}`}</span>
              )}
              <button className="primary" onClick={() => send("start")}>
                {v.phase === "end" ? "再来一局" : "开始"}
              </button>
              <span className="muted small">每局押 500 筹码，最多输 500</span>
            </>
          ) : v.phase === "dingque" ? (
            <>
              <span>定缺：选一门不要的</span>
              {SUIT.map((s, i) => (
                <button key={s} className={i === v.queHint ? "primary" : "ghost"} onClick={() => send("act", { action: "que", suit: i })}>
                  缺{s}
                </button>
              ))}
            </>
          ) : o?.pass ? (
            <>
              <span>
                {v.players[v.last?.from ?? 0].name} 打出 {name(o.tile)}
              </span>
              {o.hu && (
                <button className="primary" onClick={() => act("hu")}>
                  胡
                </button>
              )}
              {o.gang?.length ? (
                <button className="primary" onClick={() => act("gang", o.tile)}>
                  杠
                </button>
              ) : null}
              {o.peng && (
                <button className="primary" onClick={() => act("peng")}>
                  碰
                </button>
              )}
              <button className="ghost" onClick={() => act("pass")}>
                过
              </button>
            </>
          ) : canDiscard ? (
            <>
              {o!.hu && (
                <button className="primary" onClick={() => act("hu")}>
                  自摸
                </button>
              )}
              {o!.gang?.map((t) => (
                <button key={t} className="primary" onClick={() => act("gang", t)}>
                  杠 {name(t)}
                </button>
              ))}
              <span className="muted">{o!.mustQue ? `先打${SUIT[me.que]}，` : ""}点两下出牌</span>
            </>
          ) : me.hu ? (
            <span className="muted">你已经胡了，看他们打完</span>
          ) : (
            <span className="muted">等 {v.players[v.turn]?.name} 出牌…</span>
          )}
        </div>
      </div>
      <aside className="poker-side">
        <div className="stats">
          <div>
            <b>{v.stats.rounds}</b>
            <span className="muted small">局数</span>
          </div>
          <div>
            <b>{v.stats.wins}</b>
            <span className="muted small">胡牌</span>
          </div>
          <div>
            <b>{v.stats.net >= 0 ? `+${v.stats.net}` : v.stats.net}</b>
            <span className="muted small">累计输赢</span>
          </div>
          <div>
            <b>{v.stats.bestName ?? "—"}</b>
            <span className="muted small">最大番</span>
          </div>
        </div>
        <div className="log">
          {[...v.log].reverse().map((l, i) => (
            <div key={i} className={l.startsWith("——") ? "log-hand" : ""}>
              {l}
            </div>
          ))}
        </div>
        <div className="muted small">底分 10，封顶 4 番 · 筹码和德州、宠物商店通用</div>
      </aside>
    </div>
  );
}
