package mahjong

// The bots play like a decent casual player: void suit first, then the
// discard that leaves the hand fewest tiles from ready (向听) with the most
// unseen tiles that help (进张); once ready, the wait worth most. They lean
// towards one suit (清一色) or seven pairs when the hand points that way, and
// only pong or kong when it doesn't set the hand back.

// lean is the suit a hand is going for as 清一色, or -1.
func lean(c *Counts, melds []Meld, que int) int {
	total := c.total() + 3*len(melds)
	for s := 0; s < 3; s++ {
		if s == que {
			continue
		}
		n := c.suitCount(s)
		ok := true
		for _, m := range melds {
			if m.Tile.Suit() != s {
				ok = false
			}
			n += 3
		}
		if ok && n*10 >= total*7 { // 70% of the hand in one suit
			return s
		}
	}
	return -1
}

// botDiscard picks the tile to throw from a 14-tile (minus melds) hand.
func botDiscard(p *Player, seen *Counts, danger func(Tile) int) Tile {
	c := p.hand
	if p.hasQue() { // void suit first: the loneliest, safest one
		best, bestS := Tile(-1), 1<<30
		for t := Tile(p.Que * 9); t < Tile(p.Que*9+9); t++ {
			if c[t] == 0 {
				continue
			}
			s := int(c[t])*10 + danger(t)
			if s < bestS {
				best, bestS = t, s
			}
		}
		return best
	}
	suit := lean(&c, p.Melds, p.Que)
	best, bestV := Tile(-1), -1<<60
	for t := Tile(0); t < 27; t++ {
		if c[t] == 0 {
			continue
		}
		c[t]--
		sh := Shanten(c, len(p.Melds), p.Que)
		v := -sh * 1_000_000
		if sh == 0 { // ready: the wait worth most
			for _, w := range waitsSeen(c, p.Melds, p.Que, seen) {
				v += w.Left * 100 << w.Fan
			}
		} else {
			v += ukeire(c, len(p.Melds), p.Que, seen) * 100
		}
		if suit >= 0 && t.Suit() != suit {
			v += 2500 // throw the other suit while it costs little
		}
		v -= danger(t) * 40
		c[t]++
		if v > bestV {
			best, bestV = t, v
		}
	}
	return best
}

// bestAfter is the shanten of a hand after its best discard.
func bestAfter(c Counts, melds int, que int) int {
	best := 9
	for t := Tile(0); t < 27; t++ {
		if c[t] == 0 {
			continue
		}
		c[t]--
		best = min(best, Shanten(c, melds, que))
		c[t]++
	}
	return best
}

func pairCount(c *Counts) int {
	n := 0
	for _, v := range c {
		n += int(v) / 2
	}
	return n
}

// botPeng: pong the discard t when it brings the hand closer, or keeps it as
// close without breaking a seven-pairs or one-suit plan.
func botPeng(p *Player, t Tile) bool {
	if len(p.Melds) == 0 && pairCount(&p.hand) >= 4 {
		return false // going for seven pairs
	}
	if s := lean(&p.hand, p.Melds, p.Que); s >= 0 && t.Suit() != s {
		return false
	}
	before := Shanten(p.hand, len(p.Melds), p.Que)
	c := p.hand
	c[t] -= 2
	after := bestAfter(c, len(p.Melds)+1, p.Que)
	return after < before || (after == before && before <= 2)
}

// botKong: kong the discard t (or own tiles) unless it sets the hand back.
// A kong pays on the spot and adds a 根, so it is usually worth it.
func botKong(p *Player, t Tile, fromDiscard bool) bool {
	c := p.hand
	if fromDiscard {
		before := Shanten(c, len(p.Melds), p.Que)
		c[t] -= 3
		return Shanten(c, len(p.Melds)+1, p.Que) <= before
	}
	before := bestAfter(c, len(p.Melds), p.Que)
	if c[t] == 4 {
		c[t] = 0
		return Shanten(c, len(p.Melds)+1, p.Que) <= before
	}
	c[t]-- // 补杠 keeps the pong, the fourth tile just leaves the hand
	return Shanten(c, len(p.Melds), p.Que) <= before
}

// suggestQue is the suit to go void in: the fewest tiles, then the weakest.
func suggestQue(c *Counts) int {
	best, bestS := 0, 1<<30
	for s := 0; s < 3; s++ {
		n := c.suitCount(s)
		score := n*100 + suitStrength(c, s)
		if score < bestS {
			best, bestS = s, score
		}
	}
	return best
}

// suitStrength scores pairs and neighbours within one suit.
func suitStrength(c *Counts, s int) int {
	v := 0
	for r := 0; r < 9; r++ {
		n := int(c[s*9+r])
		if n >= 2 {
			v += 3 * (n - 1)
		}
		if n > 0 && r < 8 && c[s*9+r+1] > 0 {
			v += 2
		}
		if n > 0 && r < 7 && c[s*9+r+2] > 0 {
			v++
		}
	}
	return v
}

// suggestSwap picks three tiles of one suit to pass on: the suit with the
// fewest tiles (at least three), and its least connected tiles.
func suggestSwap(c *Counts) []Tile {
	suit, bestS := -1, 1<<30
	for s := 0; s < 3; s++ {
		n := c.suitCount(s)
		if n < 3 {
			continue
		}
		if score := n*100 + suitStrength(c, s); score < bestS {
			suit, bestS = s, score
		}
	}
	h := *c
	var out []Tile
	for len(out) < 3 {
		best, bestS := Tile(-1), 1<<30
		for r := 0; r < 9; r++ {
			t := Tile(suit*9 + r)
			if h[t] == 0 {
				continue
			}
			s := int(h[t]-1) * 6
			for _, d := range []int{-2, -1, 1, 2} {
				if r+d >= 0 && r+d <= 8 && h[int(t)+d] > 0 {
					s += 3 - abs(d)
				}
			}
			if r == 0 || r == 8 {
				s--
			}
			if s < bestS {
				best, bestS = t, s
			}
		}
		h[best]--
		out = append(out, best)
	}
	return out
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
