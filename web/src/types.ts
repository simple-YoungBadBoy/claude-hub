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
  pet: PetState;
  foods: Food[];
  hats: Hat[];
  dirs: string[];
  hooks: { installed: number; total: number; lastEvent?: number };
};
