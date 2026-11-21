package poker

import (
	"math/bits"
	"math/rand"
	"sort"
)

// Card is 0..51: rank = c/4 (0 = '2' … 12 = 'A'), suit = c%4 (♠♥♦♣).
type Card int

const ranks = "23456789TJQKA"
const suits = "shdc"

func (c Card) Rank() int { return int(c) / 4 }
func (c Card) Suit() int { return int(c) % 4 }
func (c Card) String() string {
	return string(ranks[c.Rank()]) + string(suits[c.Suit()])
}

// MarshalText makes cards appear as "As", "Td" … in JSON.
// A negative card is a face-down card ("??").
func (c Card) MarshalText() ([]byte, error) {
	if c < 0 {
		return []byte("??"), nil
	}
	return []byte(c.String()), nil
}

// ParseCard parses "As"; used by tests.
func ParseCard(s string) Card {
	r, su := -1, -1
	for i := range ranks {
		if ranks[i] == s[0] {
			r = i
		}
	}
	for i := range suits {
		if suits[i] == s[1] {
			su = i
		}
	}
	if r < 0 || su < 0 {
		panic("bad card " + s)
	}
	return Card(r*4 + su)
}

// Hand categories.
const (
	HighCard = iota
	Pair
	TwoPair
	Trips
	Straight
	Flush
	FullHouse
	Quads
	StraightFlush
)

var CategoryNames = []string{"高牌", "一对", "两对", "三条", "顺子", "同花", "葫芦", "四条", "同花顺"}

// Score packs category and up to five tie-break ranks; higher is better.
type Score uint32

func (s Score) Category() int { return int(s >> 20) }

func mkScore(cat int, rs ...int) Score {
	v := uint32(cat) << 20
	for i := 0; i < 5; i++ {
		r := 0
		if i < len(rs) {
			r = rs[i]
		}
		v |= uint32(r) << (16 - 4*i)
	}
	return Score(v)
}

// straightHigh returns the high rank of the best straight in mask, or -1.
func straightHigh(mask uint16) int {
	for hi := 12; hi >= 4; hi-- {
		need := uint16(0x1F) << (hi - 4)
		if mask&need == need {
			return hi
		}
	}
	// wheel A-2-3-4-5
	if mask&0x100F == 0x100F {
		return 3
	}
	return -1
}

func topRanks(mask uint16, n int) []int {
	var out []int
	for r := 12; r >= 0 && len(out) < n; r-- {
		if mask&(1<<r) != 0 {
			out = append(out, r)
		}
	}
	return out
}

// Eval scores the best 5-card hand out of 5..7 cards.
func Eval(cards []Card) Score {
	var counts [13]int
	var suitMask [4]uint16
	var all uint16
	for _, c := range cards {
		counts[c.Rank()]++
		suitMask[c.Suit()] |= 1 << c.Rank()
		all |= 1 << c.Rank()
	}
	for s := 0; s < 4; s++ {
		if bits.OnesCount16(suitMask[s]) >= 5 {
			if hi := straightHigh(suitMask[s]); hi >= 0 {
				return mkScore(StraightFlush, hi)
			}
		}
	}
	var quads, trips, pairs []int
	for r := 12; r >= 0; r-- {
		switch counts[r] {
		case 4:
			quads = append(quads, r)
		case 3:
			trips = append(trips, r)
		case 2:
			pairs = append(pairs, r)
		}
	}
	without := func(rs ...int) uint16 {
		m := all
		for _, r := range rs {
			m &^= 1 << r
		}
		return m
	}
	if len(quads) > 0 {
		return mkScore(Quads, append([]int{quads[0]}, topRanks(without(quads[0]), 1)...)...)
	}
	if len(trips) > 0 && (len(trips) > 1 || len(pairs) > 0) {
		pr := -1
		if len(pairs) > 0 {
			pr = pairs[0]
		}
		if len(trips) > 1 && trips[1] > pr {
			pr = trips[1]
		}
		return mkScore(FullHouse, trips[0], pr)
	}
	for s := 0; s < 4; s++ {
		if bits.OnesCount16(suitMask[s]) >= 5 {
			return mkScore(Flush, topRanks(suitMask[s], 5)...)
		}
	}
	if hi := straightHigh(all); hi >= 0 {
		return mkScore(Straight, hi)
	}
	if len(trips) > 0 {
		return mkScore(Trips, append([]int{trips[0]}, topRanks(without(trips[0]), 2)...)...)
	}
	if len(pairs) >= 2 {
		return mkScore(TwoPair, pairs[0], pairs[1], topRanks(without(pairs[0], pairs[1]), 1)[0])
	}
	if len(pairs) == 1 {
		return mkScore(Pair, append([]int{pairs[0]}, topRanks(without(pairs[0]), 3)...)...)
	}
	return mkScore(HighCard, topRanks(all, 5)...)
}

// Deck returns a shuffled deck.
func Deck(rng *rand.Rand) []Card {
	d := make([]Card, 52)
	for i := range d {
		d[i] = Card(i)
	}
	rng.Shuffle(len(d), func(i, j int) { d[i], d[j] = d[j], d[i] })
	return d
}

// Equity estimates the probability that hole wins (ties split) against
// `opponents` random hands given the board, by Monte Carlo.
func Equity(rng *rand.Rand, hole, board []Card, opponents, iters int) float64 {
	if opponents < 1 {
		return 1
	}
	used := map[Card]bool{}
	for _, c := range hole {
		used[c] = true
	}
	for _, c := range board {
		used[c] = true
	}
	rest := make([]Card, 0, 52)
	for i := 0; i < 52; i++ {
		if !used[Card(i)] {
			rest = append(rest, Card(i))
		}
	}
	need := 5 - len(board) + 2*opponents
	if need > len(rest) {
		return 0
	}
	var total float64
	mine := make([]Card, 0, 7)
	theirs := make([]Card, 0, 7)
	full := make([]Card, 0, 5)
	for it := 0; it < iters; it++ {
		// partial Fisher-Yates for the cards we need
		for i := 0; i < need; i++ {
			j := i + rng.Intn(len(rest)-i)
			rest[i], rest[j] = rest[j], rest[i]
		}
		full = append(full[:0], board...)
		full = append(full, rest[:5-len(board)]...)
		mine = append(append(mine[:0], hole...), full...)
		my := Eval(mine)
		best, ties := true, 1
		off := 5 - len(board)
		for o := 0; o < opponents; o++ {
			theirs = append(append(theirs[:0], rest[off+2*o], rest[off+2*o+1]), full...)
			s := Eval(theirs)
			if s > my {
				best = false
				break
			}
			if s == my {
				ties++
			}
		}
		if best {
			total += 1 / float64(ties)
		}
	}
	return total / float64(iters)
}

// sortCards sorts high to low (for display).
func sortCards(cs []Card) {
	sort.Slice(cs, func(i, j int) bool { return cs[i] > cs[j] })
}
