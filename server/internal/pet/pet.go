// Package pet is a small tamagotchi that grows while Claude works.
package pet

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"
	"time"
)

// Wallet is the shared chip balance (the poker stack).
type Wallet interface {
	Spend(n int) error
	Earn(n int)
}

type State struct {
	Name        string    `json:"name"`
	Born        time.Time `json:"born"`
	Satiety     float64   `json:"satiety"` // 0..100
	Mood        float64   `json:"mood"`    // 0..100
	XP          int       `json:"xp"`
	Level       int       `json:"level"`
	Hat         string    `json:"hat"`
	Owned       []string  `json:"owned"`
	WorkMinutes int       `json:"workMinutes"`
	TasksDone   int       `json:"tasksDone"`
	LastTick    time.Time `json:"lastTick"`
	LastPlay    time.Time `json:"lastPlay"`
	LastPet     time.Time `json:"lastPet"`
	Message     string    `json:"message"`
}

type Food struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Emoji   string  `json:"emoji"`
	Cost    int     `json:"cost"`
	Satiety float64 `json:"satiety"`
	Mood    float64 `json:"mood"`
}

type Hat struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Emoji string `json:"emoji"`
	Cost  int    `json:"cost"`
	Level int    `json:"level"`
}

var Foods = []Food{
	{"fish", "小鱼干", "🐟", 20, 15, 2},
	{"cookie", "曲奇", "🍪", 40, 25, 8},
	{"bento", "便当", "🍱", 80, 55, 10},
	{"cake", "蛋糕", "🍰", 150, 40, 35},
}

var Hats = []Hat{
	{"party", "派对帽", "🎉", 200, 2},
	{"glasses", "墨镜", "🕶️", 300, 2},
	{"wizard", "巫师帽", "🧙", 500, 3},
	{"tophat", "礼帽", "🎩", 600, 4},
	{"crown", "皇冠", "👑", 1000, 6},
}

const (
	satietyPerHour = 4.0
	moodPerHour    = 3.0
	floor          = 5.0
	playCooldown   = 5 * time.Minute
	taskReward     = 10 // chips per finished Claude task
)

// XPForLevel is the XP needed to go from level to level+1.
func XPForLevel(level int) int { return 60 * level }

type Pet struct {
	mu     sync.Mutex
	s      State
	wallet Wallet

	Persist  func(State)
	OnChange func(State)
}

func New(s State, w Wallet) *Pet {
	now := time.Now()
	if s.Born.IsZero() {
		s = State{Name: "小克", Born: now, Satiety: 80, Mood: 80, Level: 1, LastTick: now,
			Message: "你好！我是小克，Claude 干活时我也在长大～"}
	}
	p := &Pet{s: s, wallet: w}
	p.decayLocked(now)
	return p
}

func clamp(v float64) float64 { return math.Max(0, math.Min(100, v)) }

func (p *Pet) decayLocked(now time.Time) {
	if p.s.LastTick.IsZero() {
		p.s.LastTick = now
		return
	}
	h := now.Sub(p.s.LastTick).Hours()
	if h <= 0 {
		return
	}
	p.s.LastTick = now
	// never decay below the floor: neglect makes it sad, never kills it
	if p.s.Satiety > floor {
		p.s.Satiety = math.Max(floor, p.s.Satiety-satietyPerHour*h)
	}
	drop := moodPerHour * h
	if p.s.Satiety < 30 {
		drop *= 2
	}
	if p.s.Mood > floor {
		p.s.Mood = math.Max(floor, p.s.Mood-drop)
	}
}

func (p *Pet) addXPLocked(n int) {
	p.s.XP += n
	for p.s.XP >= XPForLevel(p.s.Level) {
		p.s.XP -= XPForLevel(p.s.Level)
		p.s.Level++
		reward := 100 * p.s.Level
		p.s.Message = fmt.Sprintf("升到 %d 级啦！奖励 %d 筹码 🎊", p.s.Level, reward)
		if p.wallet != nil {
			go p.wallet.Earn(reward)
		}
	}
}

func (p *Pet) commit() State {
	s := p.s
	s.Owned = slices.Clone(p.s.Owned)
	if p.Persist != nil {
		p.Persist(s)
	}
	if p.OnChange != nil {
		p.OnChange(s)
	}
	return s
}

// Get returns the current state (decayed to now).
func (p *Pet) Get() State {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.decayLocked(time.Now())
	s := p.s
	s.Owned = slices.Clone(p.s.Owned)
	return s
}

// Tick is called once a minute with the number of working Claude sessions.
func (p *Pet) Tick(working int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.decayLocked(time.Now())
	if working > 0 {
		p.s.WorkMinutes += working
		// working costs a bit of food, gives xp; watching Claude work is fun
		p.s.Satiety = clamp(p.s.Satiety - 0.1*float64(working))
		p.s.Mood = clamp(p.s.Mood + 0.3)
		mult := 1
		if p.s.Satiety >= 30 {
			mult = 2 // a fed pet learns faster
		}
		p.addXPLocked(working * mult)
	}
	p.commit()
}

// TaskDone is called when a Claude session finishes a turn.
func (p *Pet) TaskDone(session string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.decayLocked(time.Now())
	p.s.TasksDone++
	p.s.Mood = clamp(p.s.Mood + 3)
	p.addXPLocked(5)
	if p.wallet != nil {
		go p.wallet.Earn(taskReward)
	}
	p.s.Message = fmt.Sprintf("%s 干完活了！+%d 筹码", session, taskReward)
	p.commit()
}

func (p *Pet) Feed(id string) (State, error) {
	i := slices.IndexFunc(Foods, func(f Food) bool { return f.ID == id })
	if i < 0 {
		return State{}, errors.New("没有这种食物")
	}
	f := Foods[i]
	p.mu.Lock()
	defer p.mu.Unlock()
	p.decayLocked(time.Now())
	if p.s.Satiety >= 98 {
		return p.s, errors.New("吃不下啦")
	}
	if p.wallet != nil {
		if err := p.wallet.Spend(f.Cost); err != nil {
			return p.s, err
		}
	}
	p.s.Satiety = clamp(p.s.Satiety + f.Satiety)
	p.s.Mood = clamp(p.s.Mood + f.Mood)
	p.addXPLocked(1)
	p.s.Message = fmt.Sprintf("吃了%s %s，好香！", f.Name, f.Emoji)
	return p.commit(), nil
}

func (p *Pet) Play() (State, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	p.decayLocked(now)
	if wait := playCooldown - now.Sub(p.s.LastPlay); wait > 0 {
		return p.s, fmt.Errorf("玩累了，%d 秒后再来", int(wait.Seconds())+1)
	}
	if p.s.Satiety < 15 {
		return p.s, errors.New("太饿了，先喂点吃的")
	}
	p.s.LastPlay = now
	p.s.Mood = clamp(p.s.Mood + 15)
	p.s.Satiety = clamp(p.s.Satiety - 3)
	p.addXPLocked(3)
	p.s.Message = "玩得好开心！"
	return p.commit(), nil
}

// Pat is a free tiny mood boost (rate-limited).
func (p *Pet) Pat() State {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	p.decayLocked(now)
	if now.Sub(p.s.LastPet) > 20*time.Second {
		p.s.LastPet = now
		p.s.Mood = clamp(p.s.Mood + 2)
		p.s.Message = "嘿嘿～"
	}
	return p.commit()
}

func (p *Pet) BuyHat(id string) (State, error) {
	i := slices.IndexFunc(Hats, func(h Hat) bool { return h.ID == id })
	if i < 0 {
		return State{}, errors.New("没有这个装扮")
	}
	h := Hats[i]
	p.mu.Lock()
	defer p.mu.Unlock()
	if slices.Contains(p.s.Owned, id) {
		return p.s, errors.New("已经有了")
	}
	if p.s.Level < h.Level {
		return p.s, fmt.Errorf("%d 级解锁", h.Level)
	}
	if p.wallet != nil {
		if err := p.wallet.Spend(h.Cost); err != nil {
			return p.s, err
		}
	}
	p.s.Owned = append(p.s.Owned, id)
	p.s.Hat = id
	p.s.Message = fmt.Sprintf("戴上了%s %s", h.Name, h.Emoji)
	return p.commit(), nil
}

func (p *Pet) Wear(id string) (State, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if id != "" && !slices.Contains(p.s.Owned, id) {
		return p.s, errors.New("还没买")
	}
	p.s.Hat = id
	return p.commit(), nil
}

func (p *Pet) Rename(name string) (State, error) {
	r := []rune(name)
	if len(r) == 0 || len(r) > 12 {
		return p.s, errors.New("名字 1–12 个字")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.s.Name = string(r)
	return p.commit(), nil
}

// Say sets the pet's speech bubble (e.g. reacting to a Claude session).
func (p *Pet) Say(msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.s.Message = msg
	p.commit()
}
