package mahjong

import "sync"

// Shanten is how many tiles a hand is from ready (0 = ready, -1 = complete),
// the better of four sets + a pair and seven pairs. Void-suit tiles are left
// out: they are discarded first anyway.
func Shanten(c Counts, melds int, que int) int {
	if que >= 0 {
		for t := que * 9; t < que*9+9; t++ {
			c[t] = 0
		}
	}
	s := shantenStd(&c, melds)
	if melds == 0 {
		if p := shantenPairs(&c); p < s {
			s = p
		}
	}
	return s
}

func shantenPairs(c *Counts) int {
	pairs := 0
	for _, v := range c {
		pairs += int(v) / 2 // four of a kind is two pairs here (龙七对)
	}
	return 6 - min(pairs, 7)
}

// part is what one suit splits into: sets, partial sets (two tiles needing
// one more) and whether a pair is used as the head.
type part struct{ m, t, p int8 }

var suitMemo sync.Map // [9]int8 → []part

// suitParts lists the useful ways to split one suit (Pareto-best only).
func suitParts(s [9]int8) []part {
	if v, ok := suitMemo.Load(s); ok {
		return v.([]part)
	}
	seen := map[part]bool{}
	var walk func(i int, cur part)
	walk = func(i int, cur part) {
		for i < 9 && s[i] == 0 {
			i++
		}
		if i == 9 {
			seen[cur] = true
			return
		}
		if s[i] >= 3 {
			s[i] -= 3
			walk(i, part{cur.m + 1, cur.t, cur.p})
			s[i] += 3
		}
		if i <= 6 && s[i+1] > 0 && s[i+2] > 0 {
			s[i]--
			s[i+1]--
			s[i+2]--
			walk(i, part{cur.m + 1, cur.t, cur.p})
			s[i]++
			s[i+1]++
			s[i+2]++
		}
		if s[i] >= 2 {
			s[i] -= 2
			if cur.p == 0 {
				walk(i, part{cur.m, cur.t, 1})
			}
			walk(i, part{cur.m, cur.t + 1, cur.p})
			s[i] += 2
		}
		for _, d := range []int{1, 2} {
			if i+d <= 8 && s[i+d] > 0 {
				s[i]--
				s[i+d]--
				walk(i, part{cur.m, cur.t + 1, cur.p})
				s[i]++
				s[i+d]++
			}
		}
		s[i]-- // leave it as a loose tile
		walk(i, cur)
		s[i]++
	}
	walk(0, part{})
	var out []part
	for a := range seen {
		dominated := false
		for b := range seen {
			if b != a && b.p == a.p && b.m >= a.m && b.t >= a.t { // compare within the same head use
				dominated = true
				break
			}
		}
		if !dominated {
			out = append(out, a)
		}
	}
	suitMemo.Store(s, out)
	return out
}

func shantenStd(c *Counts, melds int) int {
	var ps [3][]part
	for s := 0; s < 3; s++ {
		var k [9]int8
		copy(k[:], c[s*9:s*9+9])
		ps[s] = suitParts(k)
	}
	best := 8
	for _, a := range ps[0] {
		for _, b := range ps[1] {
			for _, d := range ps[2] {
				p := a.p + b.p + d.p
				if p > 1 {
					continue // only one head; the other pairs count among a/b/d's partials
				}
				m := int(a.m+b.m+d.m) + melds
				t := int(a.t + b.t + d.t)
				if m+t > 4 {
					t = 4 - m
				}
				if t < 0 {
					t = 0
				}
				if sh := 8 - 2*m - t - int(p); sh < best {
					best = sh
				}
			}
		}
	}
	return best
}

// ukeire counts the unseen tiles that bring a 13-tile hand closer to ready.
func ukeire(c Counts, melds int, que int, seen *Counts) (n int) {
	sh := Shanten(c, melds, que)
	for t := Tile(0); t < 27; t++ {
		if t.Suit() == que || c[t] >= 4 {
			continue
		}
		left := 4 - int(c[t])
		if seen != nil {
			left = 4 - int(seen[t])
		}
		if left <= 0 {
			continue
		}
		c[t]++
		if Shanten(c, melds, que) < sh {
			n += left
		}
		c[t]--
	}
	return n
}
