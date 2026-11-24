import type { MahjongView, PokerView } from "./types";

// Poker sound effects, synthesized with Web Audio: no audio files, no
// dependencies. Which sounds to play is worked out by comparing two table
// views, so the server needs to know nothing about it.

export type Sound = "deal" | "flip" | "check" | "call" | "bet" | "allin" | "fold" | "turn" | "win" | "lose" | "clack" | "peng" | "gang" | "hu" | "zimo";

const ACTION: Record<string, Sound> = { 弃牌: "fold", 过牌: "check", 跟注: "call", 下注: "bet", 加注: "bet", 全下: "allin" };

// pokerSounds lists what changed between two views, in playing order.
export function pokerSounds(prev: PokerView | undefined, next: PokerView): Sound[] {
  if (!prev) return [];
  const out: Sound[] = [];
  const newHand = next.hand !== prev.hand;
  if (newHand && next.street !== "idle") out.push("deal");
  if (!newHand && next.board.length > prev.board.length) out.push("flip");
  if (!newHand) {
    // one action sound per update: a bot acting is one update
    for (const p of next.players) {
      const was = prev.players.find((q) => q.id === p.id);
      const s = ACTION[p.lastAction];
      if (s && was && (was.lastAction !== p.lastAction || was.bet !== p.bet || was.chips !== p.chips)) {
        out.push(s);
        break;
      }
    }
  }
  if (next.legal && next.toAct === 0 && !(prev.legal && prev.toAct === 0 && !newHand)) out.push("turn");
  if (next.winners?.length && !prev.winners?.length) out.push(next.winners.some((w) => w.id === 0) ? "win" : "lose");
  return out;
}

const MJ: Record<string, Sound> = { 碰: "peng", 杠: "gang", 胡: "hu", 自摸: "zimo" };

// mahjongSounds is pokerSounds for the mahjong table.
export function mahjongSounds(prev: MahjongView | undefined, next: MahjongView): Sound[] {
  if (!prev) return [];
  const out: Sound[] = [];
  if (next.round !== prev.round) return next.phase === "dingque" ? ["deal"] : [];
  for (const p of next.players) {
    const was = prev.players.find((q) => q.id === p.id);
    if (!was) continue;
    const s = MJ[p.lastAction ?? ""];
    if (s && was.lastAction !== p.lastAction) out.push(s);
    else if (p.discards.length > was.discards.length) out.push("clack");
  }
  const mine = (v: MahjongView) => !!v.options && (v.options.discard || v.options.pass);
  if (mine(next) && !mine(prev)) out.push(next.options?.pass ? "check" : "turn");
  if (next.phase === "end" && prev.phase !== "end") out.push(next.players[0].net >= 0 ? "win" : "lose");
  return out;
}

// ---- playback ----

const KEY = "pokerSound";

export function soundOn(): boolean {
  try {
    return localStorage.getItem(KEY) !== "off";
  } catch {
    return true;
  }
}

export function setSoundOn(on: boolean) {
  try {
    localStorage.setItem(KEY, on ? "on" : "off");
  } catch {
    /* private mode: on for this page only */
  }
  if (on) unlock();
}

let ctx: AudioContext | null = null;

function audio(): AudioContext | null {
  if (!ctx) {
    const AC = window.AudioContext ?? (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
    if (!AC) return null;
    ctx = new AC();
  }
  return ctx;
}

// iOS only lets a page make sound after a tap: call this from one.
export function unlock() {
  const c = audio();
  if (c && c.state !== "running") void c.resume();
}

// played keeps the last sounds for tests to read
const played: Sound[] = [];
(window as unknown as { __pokerSounds: Sound[] }).__pokerSounds = played;

export function play(list: Sound[]) {
  if (!list.length || !soundOn()) return;
  played.push(...list);
  played.splice(0, Math.max(0, played.length - 20));
  const c = audio();
  if (!c || c.state !== "running") return;
  for (const s of list) if (SAY[s]) say(SAY[s]);
  let t = c.currentTime + 0.01;
  for (const s of list) t = SYNTH[s](c, t) + 0.06;
}

const SAY: Partial<Record<Sound, string>> = { peng: "碰", gang: "杠", hu: "胡了", zimo: "自摸" };

// the calls at a mahjong table are spoken, when the device has a Chinese voice
function say(text: string) {
  try {
    const u = new SpeechSynthesisUtterance(text);
    u.lang = "zh-CN";
    u.rate = 1.1;
    speechSynthesis.speak(u);
  } catch {
    /* no speech: the synthesized sound still plays */
  }
}

function out(c: AudioContext, at: number, peak: number, len: number): GainNode {
  const g = c.createGain();
  g.gain.setValueAtTime(0.0001, at);
  g.gain.exponentialRampToValueAtTime(peak, at + 0.005);
  g.gain.exponentialRampToValueAtTime(0.0001, at + len);
  g.connect(c.destination);
  return g;
}

function tone(c: AudioContext, at: number, freq: number, len: number, peak = 0.2, type: OscillatorType = "sine", to?: number) {
  const o = c.createOscillator();
  o.type = type;
  o.frequency.setValueAtTime(freq, at);
  if (to) o.frequency.exponentialRampToValueAtTime(to, at + len);
  o.connect(out(c, at, peak, len));
  o.start(at);
  o.stop(at + len + 0.02);
  return at + len;
}

let noiseBuf: AudioBuffer | null = null;

function noise(c: AudioContext, at: number, len: number, freq: number, peak = 0.25, q = 1, to?: number) {
  if (!noiseBuf || noiseBuf.sampleRate !== c.sampleRate) {
    noiseBuf = c.createBuffer(1, c.sampleRate / 2, c.sampleRate);
    const d = noiseBuf.getChannelData(0);
    for (let i = 0; i < d.length; i++) d[i] = Math.random() * 2 - 1;
  }
  const src = c.createBufferSource();
  src.buffer = noiseBuf;
  const f = c.createBiquadFilter();
  f.type = "bandpass";
  f.frequency.setValueAtTime(freq, at);
  if (to) f.frequency.exponentialRampToValueAtTime(to, at + len);
  f.Q.value = q;
  src.connect(f).connect(out(c, at, peak, len));
  src.start(at);
  src.stop(at + len + 0.02);
  return at + len;
}

// chip clicks: short bright pings, slightly random so stacks sound alive
function chips(c: AudioContext, at: number, n: number) {
  let t = at;
  for (let i = 0; i < n; i++) {
    tone(c, t, 2600 + Math.random() * 1400, 0.035, 0.12, "triangle");
    noise(c, t, 0.03, 5000, 0.08, 2);
    t += 0.045 + Math.random() * 0.03;
  }
  return t;
}

const SYNTH: Record<Sound, (c: AudioContext, at: number) => number> = {
  deal: (c, at) => {
    noise(c, at, 0.07, 3000, 0.3, 0.8, 1200);
    return noise(c, at + 0.12, 0.07, 3000, 0.3, 0.8, 1200);
  },
  flip: (c, at) => noise(c, at, 0.09, 2500, 0.3, 0.7, 900),
  check: (c, at) => {
    tone(c, at, 170, 0.06, 0.4, "sine", 90);
    return tone(c, at + 0.11, 170, 0.06, 0.4, "sine", 90);
  },
  call: (c, at) => chips(c, at, 2),
  bet: (c, at) => chips(c, at, 4),
  allin: (c, at) => {
    tone(c, at, 110, 0.35, 0.25, "sawtooth", 70);
    return chips(c, at + 0.05, 9);
  },
  fold: (c, at) => noise(c, at, 0.18, 1500, 0.2, 0.6, 400),
  turn: (c, at) => {
    tone(c, at, 660, 0.12, 0.15);
    return tone(c, at + 0.1, 990, 0.22, 0.15);
  },
  win: (c, at) => {
    [523, 659, 784, 1047].forEach((f, i) => tone(c, at + i * 0.09, f, 0.25, 0.15, "triangle"));
    return chips(c, at + 0.4, 6);
  },
  clack: (c, at) => {
    noise(c, at, 0.04, 2200, 0.35, 3);
    return tone(c, at, 900, 0.05, 0.12, "square", 500);
  },
  peng: (c, at) => {
    noise(c, at, 0.04, 2200, 0.35, 3);
    return noise(c, at + 0.07, 0.05, 1800, 0.35, 3);
  },
  gang: (c, at) => {
    let t = at;
    for (let i = 0; i < 3; i++) t = noise(c, at + i * 0.07, 0.04, 2000, 0.35, 3);
    return tone(c, t, 220, 0.2, 0.2, "triangle");
  },
  hu: (c, at) => {
    [523, 659, 784, 1047, 1319].forEach((f, i) => tone(c, at + i * 0.07, f, 0.3, 0.14, "triangle"));
    return at + 0.6;
  },
  zimo: (c, at) => {
    [659, 784, 1047, 1319].forEach((f, i) => tone(c, at + i * 0.07, f, 0.3, 0.14, "triangle"));
    return at + 0.55;
  },
  lose: (c, at) => {
    tone(c, at, 392, 0.18, 0.12, "triangle");
    return tone(c, at + 0.16, 311, 0.3, 0.12, "triangle");
  },
};
