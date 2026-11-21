package poker

import (
	"testing"
	"time"
)

// Chips changing while a bot is thinking (a Claude task finishing awards
// chips) must not freeze the table.
func TestChipChangeDuringBotTurnDoesNotStall(t *testing.T) {
	tb := NewTable(Saved{Chips: 3000})
	tb.BotDelay = func() time.Duration { return 30 * time.Millisecond }
	for hand := 0; hand < 30; hand++ {
		if err := tb.StartHand(); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			v := tb.View()
			if v.Street == "showdown" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("hand %d stalled: street=%s toAct=%d", hand, v.Street, v.ToAct)
			}
			if v.ToAct == Human && v.Legal != nil {
				if v.Legal.CanCheck {
					tb.Act("check", 0)
				} else {
					tb.Act("fold", 0)
				}
				continue
			}
			tb.Earn(1) // e.g. task reward arriving while a bot thinks
			time.Sleep(10 * time.Millisecond)
		}
	}
}
