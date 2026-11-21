import { useState } from "react";
import { api } from "../api";
import type { Food, Hat, PetState } from "../types";
import PetSprite, { moodOf, type Signals } from "./PetSprite";

type Props = {
  pet: PetState;
  foods: Food[];
  hats: Hat[];
  chips: number;
  signals: Signals;
  onToast: (m: string, err?: boolean) => void;
};

const xpFor = (lv: number) => 60 * lv;

function Bar({ label, value, max = 100, color }: { label: string; value: number; max?: number; color: string }) {
  return (
    <div className="bar">
      <span className="bar-label">{label}</span>
      <div className="bar-track">
        <div className="bar-fill" style={{ width: `${Math.min(100, (value / max) * 100)}%`, background: color }} />
      </div>
      <span className="bar-num">
        {Math.round(value)}
        {max !== 100 ? `/${max}` : ""}
      </span>
    </div>
  );
}

export function PetBars({ pet }: { pet: PetState }) {
  return (
    <>
      <Bar label="饱食" value={pet.satiety} color="#e5a04a" />
      <Bar label="心情" value={pet.mood} color="#e86f8c" />
      <Bar label="经验" value={pet.xp} max={xpFor(pet.level)} color="#6fb6e8" />
    </>
  );
}

export function MiniPet({ pet, hats, signals, onClick }: { pet: PetState; hats: Hat[]; signals: Signals; onClick: () => void }) {
  return (
    <div className="mini-pet" onClick={onClick} title="去看看它">
      <PetSprite mood={moodOf(pet, signals)} hat={pet.hat} hats={hats} size={56} />
      <div className="mini-pet-info">
        <div className="mini-pet-name">
          {pet.name} <span className="muted">Lv.{pet.level}</span>
        </div>
        <div className="mini-pet-msg">{pet.message}</div>
      </div>
    </div>
  );
}

export default function Pet({ pet, foods, hats, chips, signals, onToast }: Props) {
  const [bounce, setBounce] = useState(0);
  const act = async (path: string, body?: object) => {
    try {
      await api(`/api/pet/${path}`, body ?? {});
      setBounce((b) => b + 1);
    } catch (e) {
      onToast((e as Error).message, true);
    }
  };
  const age = Math.max(0, Math.floor((Date.now() - new Date(pet.born).getTime()) / 86400000));
  const owned = pet.owned ?? [];
  return (
    <div className="pet-page">
      <div className="pet-stage">
        <div className="pet-bubble">{pet.message || "……"}</div>
        <div key={bounce} className="pet-click" onClick={() => act("pat")}>
          <PetSprite mood={moodOf(pet, signals)} hat={pet.hat} hats={hats} size={200} />
        </div>
        <div className="pet-title">
          <b
            className="rename"
            title="点击改名"
            onClick={() => {
              const n = prompt("给它起个名字", pet.name);
              if (n) void act("rename", { name: n });
            }}
          >
            {pet.name}
          </b>{" "}
          <span className="lv">Lv.{pet.level}</span>
        </div>
        <div className="pet-bars">
          <PetBars pet={pet} />
        </div>
        <div className="muted small">
          出生 {age} 天 · 陪 Claude 工作 {pet.workMinutes} 分钟 · 完成 {pet.tasksDone} 个任务
        </div>
        <div className="muted small">Claude 干活时它涨经验；每完成一个任务 +10 筹码；升级还有筹码奖励。</div>
      </div>

      <div className="pet-side">
        <section>
          <h4>
            喂食 <span className="muted small">筹码 {chips.toLocaleString()}</span>
          </h4>
          <div className="shop">
            {foods.map((f) => (
              <button key={f.id} className="shop-item" onClick={() => act("feed", { id: f.id })} disabled={chips < f.cost}>
                <span className="shop-emoji">{f.emoji}</span>
                <span>{f.name}</span>
                <span className="muted small">
                  饱食+{f.satiety}
                  {f.mood ? ` 心情+${f.mood}` : ""}
                </span>
                <span className="cost">{f.cost}</span>
              </button>
            ))}
          </div>
        </section>
        <section>
          <h4>互动</h4>
          <div className="row">
            <button className="primary" onClick={() => act("play")}>
              🧶 陪它玩
            </button>
            <button className="ghost" onClick={() => act("pat")}>
              ✋ 摸摸头
            </button>
          </div>
        </section>
        <section>
          <h4>装扮</h4>
          <div className="shop">
            {hats.map((h) => {
              const has = owned.includes(h.id);
              const wearing = pet.hat === h.id;
              const locked = pet.level < h.level;
              return (
                <button
                  key={h.id}
                  className={`shop-item ${wearing ? "on" : ""}`}
                  disabled={!has && (locked || chips < h.cost)}
                  onClick={() => (has ? act("wear", { id: wearing ? "" : h.id }) : act("buy", { id: h.id }))}
                >
                  <span className="shop-emoji">{h.emoji}</span>
                  <span>{h.name}</span>
                  <span className="muted small">{has ? (wearing ? "穿戴中（点击取下）" : "点击穿戴") : locked ? `${h.level} 级解锁` : ""}</span>
                  {!has && <span className="cost">{h.cost}</span>}
                </button>
              );
            })}
          </div>
        </section>
      </div>
    </div>
  );
}
