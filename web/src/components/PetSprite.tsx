import type { Hat } from "../types";

export type Mood = "happy" | "normal" | "sad" | "hungry" | "working" | "sleepy" | "worried";
export type Signals = { working: boolean; waiting: boolean; error: boolean };

// 16×15 pixel body. B body, L highlight, D shade, G leaf.
const BODY = [
  ".......GG.......",
  "......G.........",
  ".....BBBBBB.....",
  "...BBBBBBBBBB...",
  "..BLLBBBBBBBBB..",
  "..BLBBBBBBBBBB..",
  ".BBBBBBBBBBBBBB.",
  ".BBBBBBBBBBBBBB.",
  "BBBBBBBBBBBBBBBB",
  "BBBBBBBBBBBBBBBB",
  ".BBBBBBBBBBBBBB.",
  ".DBBBBBBBBBBBBD.",
  "..DDBBBBBBBBDD..",
  "....DDDDDDDD....",
  "...BB......BB...",
];

const COLORS: Record<string, string> = {
  B: "#e07a4f",
  L: "#f4a985",
  D: "#b45a35",
  G: "#7bbf6a",
  E: "#2a1f1a",
  W: "#ffffff",
  P: "#f08aa0",
  T: "#7cc4f0",
  K: "#9aa0a6",
  S: "#5c6268",
  C: "#8fd3ff",
};

type Px = [number, number, string];

function face(m: Mood): Px[] {
  const eye = (x: number, y: number): Px[] => [
    [x, y, "E"], [x + 1, y, "E"], [x, y + 1, "E"], [x + 1, y + 1, "E"], [x, y, "W"],
  ];
  switch (m) {
    case "happy":
      return [
        [4, 7, "E"], [5, 6, "E"], [6, 7, "E"],
        [9, 7, "E"], [10, 6, "E"], [11, 7, "E"],
        [6, 9, "E"], [7, 10, "E"], [8, 10, "E"], [9, 9, "E"],
        [3, 9, "P"], [12, 9, "P"],
      ];
    case "sad":
      return [
        ...eye(4, 7), ...eye(10, 7),
        [6, 10, "E"], [7, 9, "E"], [8, 9, "E"], [9, 10, "E"],
        [4, 9, "T"], [4, 10, "T"],
      ];
    case "hungry":
      return [
        ...eye(4, 6), ...eye(10, 6),
        [7, 9, "E"], [8, 9, "E"], [7, 10, "E"], [8, 10, "E"], [8, 11, "T"],
      ];
    case "worried":
      // wide eyes, small "o" mouth, sweat drop: someone is waiting for you
      return [
        ...eye(4, 6), ...eye(10, 6), [5, 7, "W"], [11, 7, "W"],
        [7, 9, "E"], [8, 9, "E"], [7, 10, "E"], [8, 10, "E"],
        [13, 4, "T"], [13, 5, "T"], [12, 5, "T"],
      ];
    case "sleepy":
      return [
        [4, 7, "E"], [5, 7, "E"], [10, 7, "E"], [11, 7, "E"],
        [7, 10, "E"], [8, 10, "E"],
      ];
    case "working":
      return [
        ...eye(4, 7), ...eye(10, 7),
        // laptop seen from behind: lid (S), glowing edge (C), base (K)
        ...[4, 5, 6, 7, 8, 9, 10, 11].map((x): Px => [x, 10, "C"]),
        ...[4, 5, 6, 7, 8, 9, 10, 11].map((x): Px => [x, 11, "S"]),
        ...[4, 5, 6, 7, 8, 9, 10, 11].map((x): Px => [x, 12, "S"]),
        ...[2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13].map((x): Px => [x, 13, "K"]),
      ];
    default:
      return [...eye(4, 6), ...eye(10, 6), [7, 9, "E"], [8, 9, "E"], [3, 9, "P"], [12, 9, "P"]];
  }
}

export default function PetSprite({ mood, hat, size = 96, hats }: { mood: Mood; hat?: string; size?: number; hats?: Hat[] }) {
  const px: Px[] = [];
  BODY.forEach((row, y) => [...row].forEach((c, x) => c !== "." && px.push([x, y, c])));
  const hatEmoji = hat ? hats?.find((h) => h.id === hat)?.emoji : undefined;
  return (
    <div className={`pet-sprite mood-${mood}`} style={{ width: size, height: size }}>
      {hatEmoji && (
        <span className="pet-hat" style={{ fontSize: size * 0.34 }}>
          {hatEmoji}
        </span>
      )}
      <svg viewBox="0 0 16 15" width={size} height={(size * 15) / 16} shapeRendering="crispEdges">
        {[...px, ...face(mood)].map(([x, y, c], i) => (
          <rect key={i} x={x} y={y} width={1.02} height={1.02} fill={COLORS[c]} />
        ))}
      </svg>
      {mood === "sleepy" && <span className="zzz">z</span>}
      {mood === "working" && <span className="typing">⌨</span>}
      {mood === "worried" && <span className="alert-mark">!</span>}
    </div>
  );
}

export function moodOf(p: { satiety: number; mood: number }, sig: Signals): Mood {
  if (sig.waiting) return "worried";
  if (sig.error) return "sad";
  if (p.satiety < 25) return "hungry";
  if (p.mood < 25) return "sad";
  if (sig.working) return "working";
  const h = new Date().getHours();
  if (h >= 1 && h < 7) return "sleepy";
  if (p.mood > 70 && p.satiety > 50) return "happy";
  return "normal";
}
