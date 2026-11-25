import { useEffect, useRef, useState, type CSSProperties } from "react";
import { api } from "../api";
import type { MahjongView, MjMeld, MjPlayer, MjWait } from "../types";
import { mahjongSounds, play, setSoundOn, soundOn, unlock } from "../sound";

// Sichuan bloody-battle mahjong against three bots, laid out as a square
// table: across (对家) on top, 上家 left, 下家 right, you at the bottom, each
// player's discards in rows of six in front of them and a compass in the
// middle saying whose turn it is. Tiles are numbers: suit*9 + rank-1.

const SUIT = ["万", "条", "筒"];
const CN = ["一", "二", "三", "四", "五", "六", "七", "八", "九"];
const SWAP = ["", "下家", "对家", "上家"];
const suit = (t: number) => Math.floor(t / 9);
const name = (t: number) => `${(t % 9) + 1}${SUIT[suit(t)]}`;

export function Tile({
  t,
  small,
  sel,
  dim,
  hot,
  mark,
  onClick,
}: {
  t: number;
  small?: boolean;
  sel?: boolean;
  dim?: boolean;
  hot?: boolean;
  mark?: string;
  onClick?: () => void;
}) {
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
      {mark && <span className="tile-mark">{mark}</span>}
    </button>
  );
}

const Back = ({ small }: { small?: boolean }) => <span className={`tile back ${small ? "small" : ""}`} />;

function Melds({ melds, small }: { melds: MjMeld[]; small?: boolean }) {
  return (
    <>
      {melds.map((m, i) => (
        <span key={i} className="mj-meld" title={m.kind === "peng" ? "碰" : m.kind === "angang" ? "暗杠" : "杠"}>
          {Array.from({ length: m.kind === "peng" ? 3 : 4 }, (_, k) =>
            m.tile < 0 || (m.kind === "angang" && k !== 3) ? <Back key={k} small={small} /> : <Tile key={k} t={m.tile} small={small} />,
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
      {p.que >= 0 && <span className={`mj-que q${p.que}`}>缺{SUIT[p.que]}</span>}
      {p.hu && (
        <span className="mj-hu" title={p.huName}>
          {p.huOrder ? `${"一二三"[p.huOrder - 1]}胡` : "胡"}
        </span>
      )}
      {(v.phase === "end" || p.net !== 0) && <span className={`mj-net ${p.net >= 0 ? "up" : "down"}`}>{p.net >= 0 ? `+${p.net}` : p.net}</span>}
    </>
  );
}

// a player's discards, six to a row; the latest one marked while it's live
function Pool({ p, v }: { p: MjPlayer; v: MahjongView }) {
  return (
    <div className={`mj-pool seat${p.id}`}>
      {p.discards.map((t, i) => (
        <Tile key={i} t={t} small hot={v.last?.from === p.id && i === p.discards.length - 1} />
      ))}
    </div>
  );
}

function Opponent({ p, v, pos }: { p: MjPlayer; v: MahjongView; pos: string }) {
  const acting = v.phase === "play" && v.turn === p.id && !p.hu;
  return (
    <div className={`mj-seat ${pos} ${acting ? "acting" : ""} ${p.hu ? "won" : ""}`} data-seat={p.id}>
      <div className="mj-who">
        <b>{p.name}</b>
        <Badges p={p} v={v} />
        {v.phase !== "idle" && !p.hand && <span className="muted small">{p.count}张</span>}
        {p.lastAction && <span className="seat-action">{p.lastAction}</span>}
      </div>
      <div className="mj-row">
        <Melds melds={p.melds} small />
        {p.hand ? p.hand.map((t, i) => <Tile key={i} t={t} small />) : Array.from({ length: p.count }, (_, i) => <Back key={i} small />)}
        {p.hu && <Tile t={p.huTile} small hot />}
      </div>
    </div>
  );
}

function Compass({ v }: { v: MahjongView }) {
  const seats = ["me", "right", "top", "left"];
  return (
    <div className="mj-compass">
      {v.players.map((p) => (
        <span key={p.id} className={`mj-dir ${seats[p.id]} ${v.phase === "play" && v.turn === p.id ? "on" : ""} ${p.hu ? "won" : ""}`}>
          {p.hu ? "胡" : p.id === 0 ? "你" : p.name.slice(0, 1)}
        </span>
      ))}
      <div className="mj-compass-mid">
        {v.phase === "idle" ? (
          <span className="muted small">血战到底</span>
        ) : (
          <>
            <b>{v.wall}</b>
            <span className="muted small">剩余</span>
          </>
        )}
      </div>
    </div>
  );
}

const waitText = (ws: MjWait[]) => ws.map((w) => `${name(w.tile)}·剩${w.left}${w.fan > 0 ? `·${w.fan}番` : ""}`).join("  ");

export default function Mahjong({ v, onToast }: { v: MahjongView; onToast: (m: string, err?: boolean) => void }) {
  const [sel, setSel] = useState<number | null>(null);
  const [pick, setPick] = useState<number[]>([]); // 换三张: indexes in the sorted hand
  const [sound, setSound] = useState(soundOn);
  const prev = useRef<MahjongView>();
  useEffect(() => {
    play(mahjongSounds(prev.current, v));
    prev.current = v;
  }, [v]);
  // a lifted tile drops back only when the hand changes, not on every
  // broadcast (a full state refresh between the two taps must not undo it)
  const handKey = `${v.round}/${v.phase}/${v.turn}/${v.drawn}/${v.players[0].count}/${!!v.options?.discard}`;
  useEffect(() => setSel(null), [handKey]);

  const me = v.players[0];
  const o = v.options;
  const canDiscard = !!o?.discard;
  // my hand: void suit last, so the tiles to throw first sit together
  const order = (t: number) => (suit(t) === me.que ? 100 : 0) + t;
  const hand = [...(me.hand ?? [])].sort((a, b) => order(a) - order(b));

  // the suggested three are picked when the swap starts
  const swapKey = `${v.round}/${v.phase}`;
  useEffect(() => {
    if (v.phase !== "swap" || !v.swapHint) return setPick([]);
    const left = [...v.swapHint];
    const idx: number[] = [];
    hand.forEach((t, i) => {
      const k = left.indexOf(t);
      if (k >= 0) {
        left.splice(k, 1);
        idx.push(i);
      }
    });
    setPick(idx);
  }, [swapKey]); // eslint-disable-line react-hooks/exhaustive-deps

  const send = async (path: string, body?: object) => {
    unlock();
    try {
      await api(`/api/mahjong/${path}`, body ?? {});
    } catch (e) {
      onToast((e as Error).message, true);
    }
  };
  const act = (action: string, tile = -1) => send("act", { action, tile });

  const tap = (t: number, key: number) => {
    if (v.phase === "swap" && !me.swapped) {
      if (pick.includes(key)) return setPick(pick.filter((k) => k !== key));
      const same = pick.filter((k) => suit(hand[k]) === suit(t));
      return setPick([...same, key].slice(-3)); // switching suit starts over
    }
    if (!canDiscard) return;
    if (o?.mustQue && suit(t) !== me.que) return onToast(`先把缺的${SUIT[me.que]}打完`, true);
    if (sel === key) void act("discard", t);
    else setSel(key);
  };

  const hintFor = (t: number) => v.hints?.find((h) => h.tile === t);
  const selTile = sel === null ? null : sel === -1 ? v.drawn : hand[sel];
  const got = [...(v.got ?? [])];
  const isGot = (t: number) => {
    const k = got.indexOf(t);
    if (k < 0) return false;
    got.splice(k, 1);
    return true;
  };
  const tappable = (v.phase === "swap" && !me.swapped) || canDiscard;
  const tileFor = (t: number, key: number) => (
    <Tile
      key={key}
      t={t}
      sel={v.phase === "swap" ? pick.includes(key) : sel === key}
      dim={canDiscard && o?.mustQue && suit(t) !== me.que}
      hot={v.phase !== "swap" && isGot(t)}
      mark={canDiscard && !o?.mustQue && hintFor(t) ? "听" : undefined}
      onClick={tappable ? () => tap(t, key) : undefined}
    />
  );

  const pool = (id: number) => <Pool p={v.players[id]} v={v} />;
  const status = () => {
    switch (v.phase) {
      case "idle":
        return <span className="muted">四川血战到底 · 换三张 · 定缺 · 可碰杠不可吃 · 封顶 8 番</span>;
      case "swap":
        return <span>{me.swapped ? "等其他人换牌…" : "换三张：选三张同花色的牌给别人（方向换完揭晓）"}</span>;
      case "dingque":
        return (
          <span>
            换给了{SWAP[v.swapDir ?? 0]}
            {v.got?.length ? <>，收到 {v.got.map(name).join(" ")}（金框）</> : null}
          </span>
        );
      case "end":
        return <span>本局结束</span>;
    }
    if (o?.pass) return <span>{o.rob ? `${v.players[v.last?.from ?? 0].name} 补杠 ${name(o.tile)}，可以抢杠胡` : `${v.players[v.last?.from ?? 0].name} 打出 ${name(o.tile)}`}</span>;
    if (canDiscard) {
      if (selTile !== null) {
        const h = hintFor(selTile);
        return <span>{h ? `打 ${name(selTile)} 听：${waitText(h.waits)}` : `打 ${name(selTile)}？再点一下出牌`}</span>;
      }
      return <span className="muted">{o!.mustQue ? `先打${SUIT[me.que]}，` : ""}点一下抬起，再点一下出牌</span>;
    }
    if (me.hu) return <span className="muted">你已经胡了，看他们打完</span>;
    if (v.waits?.length) return <span>听：{waitText(v.waits)}</span>;
    return <span className="muted">等 {v.players[v.turn]?.name} 出牌…</span>;
  };

  return (
    <div className="poker mj">
      <div className="mj-main">
        <div className="mj-table">
          <Opponent p={v.players[2]} v={v} pos="top" />
          <Opponent p={v.players[3]} v={v} pos="left" />
          <Opponent p={v.players[1]} v={v} pos="right" />
          <div className="mj-felt">
            <div className="pool-top">{pool(2)}</div>
            <div className="pool-left">{pool(3)}</div>
            <Compass v={v} />
            <div className="pool-right">{pool(1)}</div>
            <div className="pool-me">{pool(0)}</div>
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
          </div>
        </div>

        <div className={`mj-me ${v.phase === "play" && v.turn === 0 && !me.hu ? "acting" : ""} ${me.hu ? "won" : ""}`}>
          <div className="mj-who">
            <b>你</b>
            <Badges p={me} v={v} />
            {me.passHu && <span className="mj-que" title="放过了一次胡，下次摸牌前不能点炮胡">过手胡</span>}
            {me.lastAction && <span className="seat-action">{me.lastAction}</span>}
            <span className="mj-status">{status()}</span>
          </div>
          <div
            className="mj-hand"
            // tiles size to fit the row: melds + hand + the drawn tile's gap
            style={{ "--n": me.melds.reduce((a, m) => a + (m.kind === "peng" ? 3 : 4), 0) + hand.length + (v.drawn >= 0 ? 1.4 : 0) + (me.hu ? 1 : 0) + me.melds.length * 0.3 } as CSSProperties}
          >
            <Melds melds={me.melds} />
            {hand.map((t, i) => tileFor(t, i))}
            {v.drawn >= 0 && <span className="mj-drawn">{tileFor(v.drawn, -1)}</span>}
            {me.hu && <Tile t={me.huTile} hot />}
          </div>
        </div>

        <div className="actions">
          {v.phase === "idle" || v.phase === "end" ? (
            <>
              {v.phase === "end" && <span className={`mj-result ${me.net >= 0 ? "up" : "down"}`}>{me.net >= 0 ? `本局赢 ${me.net}` : `本局输 ${-me.net}`}</span>}
              <button className="primary" onClick={() => send("start")}>
                {v.phase === "end" ? "再来一局" : "开始"}
              </button>
              <span className="muted small">每局押 1000 筹码，最多输 1000</span>
            </>
          ) : v.phase === "swap" ? (
            !me.swapped && (
              <button className="primary" disabled={pick.length !== 3} onClick={() => send("act", { action: "swap", tiles: pick.map((k) => hand[k]) })}>
                换出这三张
              </button>
            )
          ) : v.phase === "dingque" ? (
            <>
              <span>定缺：</span>
              {SUIT.map((s, i) => (
                <button key={s} className={i === v.queHint ? "primary" : "ghost"} onClick={() => send("act", { action: "que", suit: i })}>
                  缺{s}
                </button>
              ))}
            </>
          ) : o?.pass ? (
            <>
              {o.hu && (
                <button className="primary mj-big" onClick={() => act("hu")}>
                  {o.rob ? "抢杠胡" : "胡"}
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
          ) : canDiscard && (o!.hu || o!.gang?.length) ? (
            <>
              {o!.hu && (
                <button className="primary mj-big" onClick={() => act("hu")}>
                  自摸
                </button>
              )}
              {o!.gang?.map((t) => (
                <button key={t} className="primary" onClick={() => act("gang", t)}>
                  杠 {name(t)}
                </button>
              ))}
            </>
          ) : null}
        </div>
      </div>

      {v.phase === "end" && v.sheet && <Sheet v={v} onAgain={() => send("start")} />}

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
        <div className="muted small">底分 10，封顶 8 番 · 筹码和德州、宠物商店通用</div>
      </aside>
    </div>
  );
}

// the settlement sheet: each player's win, where every chip came from and went
function Sheet({ v, onAgain }: { v: MahjongView; onAgain: () => void }) {
  const [open, setOpen] = useState(true);
  useEffect(() => setOpen(true), [v.round]);
  if (!open) return null;
  return (
    <div className="mj-sheet-wrap" onClick={() => setOpen(false)}>
      <div className="mj-sheet" onClick={(e) => e.stopPropagation()}>
        <h3>第 {v.round} 局结算</h3>
        {v.players.map((p) => (
          <div key={p.id} className={`mj-sheet-row ${p.id === 0 ? "me" : ""}`}>
            <div className="mj-sheet-head">
              <b>{p.name}</b>
              {p.hu ? (
                <span className="mj-hu">
                  {"一二三"[(p.huOrder ?? 1) - 1]}胡 · {p.huZimo ? "自摸" : "点炮"} {name(p.huTile)}
                </span>
              ) : p.pig ? (
                <span className="mj-que">花猪</span>
              ) : p.ready ? (
                <span className="mj-que">听牌</span>
              ) : (
                <span className="mj-que">没叫</span>
              )}
              <span className={`mj-net ${p.net >= 0 ? "up" : "down"}`}>{p.net >= 0 ? `+${p.net}` : p.net}</span>
            </div>
            {p.huItems && (
              <div className="mj-fans">
                {p.huItems.map((f, i) => (
                  <span key={i} className="mj-fan">
                    {f.name}
                    {f.fan > 0 && <small> {f.fan}番</small>}
                  </span>
                ))}
                <span className="muted small">共 {p.huFan} 番</span>
              </div>
            )}
            <div className="mj-row">
              <Melds melds={p.melds} small />
              {p.hand?.map((t, i) => <Tile key={i} t={t} small />)}
            </div>
            {(v.sheet?.[p.id] ?? []).map((l, i) => (
              <div key={i} className="mj-line">
                <span>{l.text}</span>
                <span className={l.amount >= 0 ? "up" : "down"}>{l.amount >= 0 ? `+${l.amount}` : l.amount}</span>
              </div>
            ))}
          </div>
        ))}
        <div className="actions">
          <button className="ghost" onClick={() => setOpen(false)}>
            看牌桌
          </button>
          <button className="primary" onClick={onAgain}>
            再来一局
          </button>
        </div>
      </div>
    </div>
  );
}
