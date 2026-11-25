export type Session = {
  name: string;
  created: number;
  attached: number;
  command: string;
  path: string;
  claude: boolean;
  status: "" | "idle" | "working" | "waiting" | "error";
  paneId: string;
  activity: string;
  context?: { tokens: number; limit: number; pct: number; model?: string };
  hooked: boolean;
  prompt?: { question: string; detail?: string[]; options: { key: string; label: string }[] };
  summary?: { title?: string; last?: string };
};

export type Player = {
  id: number;
  name: string;
  bot: boolean;
  style?: string;
  chips: number;
  bet: number;
  total: number;
  folded: boolean;
  allIn: boolean;
  lastAction: string;
  hole: string[];
};

export type PokerView = {
  hand: number;
  street: "idle" | "preflop" | "flop" | "turn" | "river" | "showdown";
  board: string[];
  pot: number;
  curBet: number;
  dealer: number;
  toAct: number;
  players: Player[];
  winners: { id: number; amount: number; hand: string }[] | null;
  log: string[];
  legal?: { canCheck: boolean; callAmount: number; minRaiseTo: number; maxRaiseTo: number };
  stats: { chips: number; lastRelief: string; hands: number; won: number; biggestPot: number };
  canRelief: boolean;
  myHand?: string;
};

export type MjMeld = { kind: "peng" | "gang" | "angang"; tile: number; from?: number };
export type MjFan = { name: string; fan: number };
export type MjWait = { tile: number; left: number; fan: number };
export type MjPlayer = {
  id: number;
  name: string;
  bot: boolean;
  que: number;
  hu: boolean;
  huOrder?: number;
  huName?: string;
  huItems?: MjFan[];
  huFan: number;
  huTile: number;
  huZimo?: boolean;
  net: number;
  melds: MjMeld[];
  discards: number[];
  count: number;
  hand?: number[];
  lastAction?: string;
  swapped?: boolean;
  passHu?: boolean;
  ready?: boolean;
  pig?: boolean;
};
export type MahjongView = {
  phase: "idle" | "swap" | "dingque" | "play" | "end";
  round: number;
  dealer: number;
  turn: number;
  wall: number;
  players: MjPlayer[];
  drawn: number;
  last?: { from: number; tile: number };
  options?: { discard: boolean; hu: boolean; gang?: number[]; peng: boolean; pass: boolean; tile: number; rob?: boolean; mustQue: boolean };
  queHint: number;
  swapHint?: number[];
  swapDir?: number;
  got?: number[];
  hints?: { tile: number; waits: MjWait[] }[];
  waits?: MjWait[];
  sheet?: { text: string; amount: number }[][];
  stake: number;
  log: string[];
  stats: { rounds: number; wins: number; net: number; bestFan: number; bestName?: string };
};

export type PetState = {
  name: string;
  born: string;
  satiety: number;
  mood: number;
  xp: number;
  level: number;
  hat: string;
  owned: string[] | null;
  workMinutes: number;
  tasksDone: number;
  lastPlay: string;
  message: string;
};

export type Food = { id: string; name: string; emoji: string; cost: number; satiety: number; mood: number };
export type Hat = { id: string; name: string; emoji: string; cost: number; level: number };

export type HubState = {
  sessions: Session[];
  poker: PokerView;
  mahjong: MahjongView;
  pet: PetState;
  foods: Food[];
  hats: Hat[];
  dirs: string[];
  hooks: { installed: number; total: number; lastEvent?: number };
};

export type UsageCounts = { input: number; output: number; cacheRead: number; cacheWrite: number; messages: number };
export type UsageReport = {
  today: UsageCounts;
  todaySub: UsageCounts;
  models: (UsageCounts & { name: string })[];
  projects: (UsageCounts & { name: string })[];
  days: (UsageCounts & { day: string })[];
  scannedAt: number;
};
