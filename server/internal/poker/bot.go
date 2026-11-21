package poker

// style tunes a bot: rel = equity × (opponents+1) is how much better than an
// average hand it is; raise above raiseRel, call when equity+callMargin beats
// pot odds, bluff with probability bluff.
type style struct {
	raiseRel   float64
	callMargin float64
	bluff      float64
	sizing     float64 // fraction of pot for bets/raises
}

var styles = map[string]style{
	"rock":   {raiseRel: 1.9, callMargin: 0.02, bluff: 0.01, sizing: 0.6},
	"tight":  {raiseRel: 1.6, callMargin: -0.01, bluff: 0.04, sizing: 0.7},
	"loose":  {raiseRel: 1.4, callMargin: -0.06, bluff: 0.08, sizing: 0.6},
	"maniac": {raiseRel: 1.2, callMargin: -0.08, bluff: 0.18, sizing: 1.0},
}

// decide picks an action for bot p (called with t.mu held).
func (t *Table) decide(p *Player) (string, int) {
	st, ok := styles[p.Style]
	if !ok {
		st = styles["tight"]
	}
	opps := len(t.live()) - 1
	eq := Equity(t.rng, p.Hole, t.board, opps, 400)
	eq += (t.rng.Float64() - 0.5) * 0.08 // a little unpredictability
	rel := eq * float64(opps+1)
	toCall := t.curBet - p.Bet
	pot := t.pot()
	stack := p.Bet + p.Chips

	raiseTo := func(frac float64) int {
		size := int(float64(pot+toCall) * frac * (0.8 + 0.4*t.rng.Float64()))
		to := t.curBet + max(size, t.minRaise)
		to = max(to, t.minRaiseTo(p))
		// round to 10s
		to = (to + 9) / 10 * 10
		if to > stack {
			to = stack
		}
		return to
	}
	bluff := t.rng.Float64() < st.bluff

	if toCall <= 0 {
		if rel > st.raiseRel || bluff {
			// monsters sometimes slow-play
			if rel > st.raiseRel+1 && t.rng.Float64() < 0.25 {
				return "check", 0
			}
			return "raise", raiseTo(st.sizing)
		}
		return "check", 0
	}

	potOdds := float64(toCall) / float64(pot+toCall)
	// a call that risks most of the stack needs a real hand
	if toCall > p.Chips/2 && eq < 0.5 && rel < st.raiseRel+0.3 && !bluff {
		if eq+st.callMargin < potOdds+0.1 {
			return "fold", 0
		}
	}
	switch {
	case rel > st.raiseRel+0.4 && t.rng.Float64() < 0.65:
		return "raise", raiseTo(st.sizing)
	case eq+st.callMargin >= potOdds:
		return "call", 0
	case bluff && t.rng.Float64() < 0.5:
		return "raise", raiseTo(st.sizing)
	default:
		return "fold", 0
	}
}
