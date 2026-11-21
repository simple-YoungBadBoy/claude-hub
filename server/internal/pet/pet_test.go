package pet

import (
	"errors"
	"testing"
	"time"
)

type wallet struct{ chips int }

func (w *wallet) Spend(n int) error {
	if w.chips < n {
		return errors.New("poor")
	}
	w.chips -= n
	return nil
}
func (w *wallet) Earn(n int) { w.chips += n }

func TestDecayHasFloor(t *testing.T) {
	p := New(State{Born: time.Now(), Satiety: 80, Mood: 80, Level: 1, LastTick: time.Now().Add(-30 * 24 * time.Hour)}, nil)
	s := p.Get()
	if s.Satiety != floor || s.Mood != floor {
		t.Fatalf("after a month away: %+v", s)
	}
}

func TestFeedSpendsChipsAndLevelsUp(t *testing.T) {
	w := &wallet{chips: 100}
	p := New(State{}, w)
	p.s.Satiety = 10
	if _, err := p.Feed("bento"); err != nil {
		t.Fatal(err)
	}
	if w.chips != 20 {
		t.Fatalf("chips %d", w.chips)
	}
	if _, err := p.Feed("cake"); err == nil {
		t.Fatal("bought cake without chips")
	}
	for i := 0; i < 60; i++ {
		p.Tick(1)
	}
	if p.Get().Level < 2 {
		t.Fatalf("no level up: %+v", p.Get())
	}
}
