// Package compaction implements a token-budget compaction algorithm for
// conversation history. Messages are progressively degraded through a
// four-phase state machine (kept → trim25 → trim10 → drop) until the
// total token cost fits within the requested budget.
//
// When a Message carries a non-empty Parts slice the trim states use a
// split → categorise → filter → join strategy:
//
//	trim25  retains parts whose Cat >= CatMid  (drop low-value segments)
//	trim10  retains parts whose Cat >= CatHigh (keep only high-value segments)
//
// Messages without Parts fall back to the legacy 25 %/10 % percentage
// approximations (controlled by the Trunc flag, as before).
package compaction

import (
	"math"
	"sort"
)

// ------------------------------------------------------------------ types

// State represents the compaction state of a single message.
type State string

const (
	StateKept   State = "kept"
	StateTrim25 State = "trim25" // part-filter: keep Cat >= CatMid  (or legacy 25 %)
	StateTrim10 State = "trim10" // part-filter: keep Cat >= CatHigh (or legacy 10 %)
	StateDrop   State = "drop"   // discard entirely
)

// Category is the importance tier of a message or message part.
type Category string

const (
	CatSystem  Category = "system"
	CatHighest Category = "highest"
	CatHigh    Category = "high"
	CatMid     Category = "mid"
	CatLow     Category = "low"
	CatLowest  Category = "lowest"
	CatGarbage Category = "garbage"
)

// Part is a content segment of a Message produced by the split/categorise
// phase.  Each part carries its own token count and importance category so
// that the compaction algorithm can retain high-value segments while
// discarding low-value ones instead of truncating by a fixed percentage.
//
// Typical splitting strategies:
//   - prose paragraphs vs. code blocks
//   - preamble / body / epilogue
//   - any domain-specific chunking the caller finds meaningful
//
// The caller is responsible for populating Parts; the compaction algorithm
// treats them as opaque read-only data.
type Part struct {
	Tok int
	Cat Category
}

// Message is a single entry in the conversation history.
type Message struct {
	ID    int
	Role  string
	Age   int      // turns since this message (0 = most recent)
	Tok   int      // baseline token count (sum of all parts when Parts is set)
	Cat   Category // importance category of the message as a whole
	Shout bool     // written in ALL-CAPS / emphatic phrasing
	Trunc bool     // legacy flag: contains a trimmable code block (used when Parts is empty)
	Text  string

	// Parts is the result of splitting this message into individually-
	// categorised segments.  When non-empty the trim states filter by part
	// category:
	//   StateTrim25  keeps parts with Cat >= trim25Cat (CatMid)
	//   StateTrim10  keeps parts with Cat >= trim10Cat (CatHigh)
	//
	// When Parts is empty the legacy Trunc/percentage fallback is used.
	Parts []Part
}

// Result is returned by Compact.
type Result struct {
	States     map[int]State // per-message compaction state
	PassNumber map[int]int   // relaxation pass (1-based) at which each message was touched; 0 = untouched
	Used       int           // total tokens after compaction
	Floor      int           // hard-floor: minimum achievable tokens
	MaxRelax   int           // highest relaxation level reached (0-based)
	Unreachable bool         // true when budget could not be met even at max relaxation
}

// ------------------------------------------------------------------ constants

const maxAge = 12

// trim25Cat / trim10Cat are the minimum part categories retained in the
// corresponding trim states when a message has a non-empty Parts slice.
const (
	trim25Cat = CatMid  // StateTrim25: keep Mid, High, Highest parts
	trim10Cat = CatHigh // StateTrim10: keep High, Highest parts only
)

// priority scores used by dispScore; higher = more important = harder to drop.
var priority = map[Category]float64{
	CatSystem:  7.0,
	CatHighest: 5.5,
	CatHigh:    4.0,
	CatMid:     3.0,
	CatLow:     2.0,
	CatLowest:  1.0,
	CatGarbage: 0.0,
}

// shoutUpgrade promotes a message's effective category one tier when it is
// flagged as a shout. "highest" is already the ceiling.
var shoutUpgrade = map[Category]Category{
	CatGarbage: CatLowest,
	CatLowest:  CatLow,
	CatLow:     CatMid,
	CatMid:     CatHigh,
	CatHigh:    CatHighest,
	CatHighest: CatHighest,
}

// ------------------------------------------------------------------ helpers

// effectiveCat returns the category to use for threshold comparisons.
// When a message is flagged as a shout its category is promoted one tier.
func effectiveCat(m Message) Category {
	if m.Shout {
		if up, ok := shoutUpgrade[m.Cat]; ok {
			return up
		}
	}
	return m.Cat
}

// canForget returns true when a message is eligible for compaction at the
// given relaxation level.
//
//	relax == 0  strict zone rules
//	relax == 1  relax mid/low/lowest thresholds  (+0.15)
//	relax == 2  also relax high threshold         (+0.20)
//	relax >= 3  shout+highest can be dropped
//	relax == 4  used for hard-floor calculation only (maximum pressure)
//
// budget is the token target being compacted toward. A non-system message
// whose token count alone exceeds budget is always forgettable regardless of
// age or category — keeping it makes the target permanently unreachable.
// Pass budget=0 to disable this override (e.g. when computing hardFloor).
func canForget(m Message, relax int, budget int) bool {
	if m.Cat == CatSystem {
		return false
	}

	// Size-based override: if one message alone exceeds the entire budget, age
	// thresholds are irrelevant — it must go before anything else can help.
	if budget > 0 && m.Tok > budget {
		return true
	}

	r := float64(m.Age) / float64(maxAge)
	cat := effectiveCat(m)

	// Threshold shifts applied as relaxation increases.
	sh := 0.0
	if relax >= 1 {
		sh = 0.15 // mid / low / lowest relief
	}
	sh2 := 0.0
	if relax >= 2 {
		sh2 = 0.20 // high relief
	}

	switch cat {
	case CatHighest:
		if m.Shout && relax < 3 {
			return false // SHOUT+highest is nearly inviolable
		}
		return r >= math.Max(0, 0.85-sh2*1.5)
	case CatHigh:
		return r >= math.Max(0, 0.70-sh2)
	case CatMid:
		return r >= math.Max(0, 0.50-sh)
	case CatLow:
		return r >= math.Max(0, 0.25-sh)
	case CatLowest:
		return r >= math.Max(0, 0.12-sh*0.5)
	default: // garbage
		return true
	}
}

// dispScore computes a disposability score for a message.
// Higher score → disposed of first when sorting candidates.
//
// budget is used to compute a size bonus: messages whose token count greatly
// exceeds the budget are boosted toward the front of the drop queue so the
// algorithm clears the biggest cost centres first rather than last.
func dispScore(m Message, budget int) float64 {
	r := float64(m.Age) / float64(maxAge)
	ep := priority[m.Cat]
	if m.Shout {
		ep += 1.5
	}
	base := (1-ep/9)*0.6 + r*0.4

	// Size boost: a message that alone exceeds the budget must sort before
	// every normally-scored message. Normal scores live in [0, 1]; we return
	// 1 + a proportional bonus so that any oversized message unconditionally
	// precedes all others in the candidate list, with larger messages first.
	if budget > 0 && m.Tok > budget {
		// Ratio of how many times over budget this message is (capped at 1 for
		// the bonus term so the ordering is stable across extreme sizes).
		sizeFactor := math.Min(1.0, float64(m.Tok)/float64(budget)-1.0)
		return 1.0 + sizeFactor
	}

	return base
}

// partsTok returns the total token cost of all parts whose category meets or
// exceeds minCat. It implements the "filter" step of the split→categorise→
// filter→join pipeline used by the trim states when Parts is non-empty.
func partsTok(parts []Part, minCat Category) int {
	minPri := priority[minCat]
	total := 0
	for _, p := range parts {
		if priority[p.Cat] >= minPri {
			total += p.Tok
		}
	}
	return total
}

// canTrimMessage reports whether a message supports intermediate trim states
// (trim25/trim10) as opposed to being dropped outright.  A message can be
// trimmed if it either has a populated Parts slice or carries the legacy Trunc
// flag, and useTrunc is enabled.
func canTrimMessage(m Message, useTrunc bool) bool {
	return useTrunc && (len(m.Parts) > 0 || m.Trunc)
}

// tokCost returns the effective token cost of m under the given state map.
//
// For trim states the cost is determined by the part-filtering pipeline when
// Parts is populated, or by the legacy percentage approximation otherwise:
//
//	StateTrim25  parts: sum tokens of parts with Cat >= CatMid
//	             legacy: 25 % of m.Tok
//	StateTrim10  parts: sum tokens of parts with Cat >= CatHigh
//	             legacy: 10 % of m.Tok
func tokCost(m Message, states map[int]State) int {
	switch states[m.ID] {
	case StateDrop:
		return 0

	case StateTrim25:
		if len(m.Parts) > 0 {
			return partsTok(m.Parts, trim25Cat)
		}
		return int(math.Round(float64(m.Tok) * 0.25))

	case StateTrim10:
		if len(m.Parts) > 0 {
			return partsTok(m.Parts, trim10Cat)
		}
		return int(math.Round(float64(m.Tok) * 0.10))

	default: // kept
		return m.Tok
	}
}

// totalCost sums tokCost across all messages.
func totalCost(messages []Message, states map[int]State) int {
	total := 0
	for _, m := range messages {
		total += tokCost(m, states)
	}
	return total
}

// advance moves m one step forward in the state machine and returns the number
// of tokens saved.
//
// When the message supports trimming (canTrimMessage == true) the path is:
//
//	kept → trim25 → trim10 → drop
//
// The cost at each trim state is computed by tokCost, which delegates to
// partsTok for messages with Parts or to the legacy percentage otherwise.
//
// When trimming is not available the message is dropped immediately from
// whatever state it is currently in.
//
// Returns 0 if the message is already dropped.
func advance(m Message, states map[int]State, useTrunc bool) int {
	cur := states[m.ID]
	if cur == StateDrop {
		return 0
	}

	if canTrimMessage(m, useTrunc) {
		switch cur {
		case StateKept:
			fullCost := m.Tok
			states[m.ID] = StateTrim25
			return fullCost - tokCost(m, states)

		case StateTrim25:
			wasCost := tokCost(m, states)
			states[m.ID] = StateTrim10
			nowCost := tokCost(m, states)
			return wasCost - nowCost

		case StateTrim10:
			wasCost := tokCost(m, states)
			states[m.ID] = StateDrop
			return wasCost
		}
	} else {
		// No trimming path: drop immediately regardless of current state.
		was := tokCost(m, states)
		states[m.ID] = StateDrop
		return was
	}

	return 0
}

// hardFloor returns the minimum token cost achievable at maximum relaxation
// (relax == 4). Messages that cannot be forgotten even under maximum pressure
// contribute their full baseline cost.
func hardFloor(messages []Message) int {
	floor := 0
	for _, m := range messages {
		if !canForget(m, 4, 0) {
			floor += m.Tok
		}
	}
	return floor
}

// ------------------------------------------------------------------ core

// Compact runs the compaction algorithm against messages, targeting budget
// tokens. useTrunc controls whether the trim states (trim25/trim10) are
// permitted; when false every advance drops the message outright.
//
// For messages with a non-empty Parts slice the trim states apply the
// split→categorise→filter→join pipeline:
//
//	trim25  keeps parts whose Cat >= CatMid
//	trim10  keeps parts whose Cat >= CatHigh
//
// The algorithm iterates over four relaxation phases (P1–P4):
//
//	P1 (relax 0)  strict zone thresholds
//	P2 (relax 1)  relax mid/low/lowest thresholds
//	P3 (relax 2)  also relax high thresholds
//	P4 (relax 3)  SHOUT+highest messages at oldest zone are now eligible
//
// Within each phase, candidates are sorted by descending disposability score
// and advanced one state-machine step at a time, repeating until the budget
// is met or no further progress is possible at that relaxation level. The
// algorithm then moves to the next phase.
func Compact(messages []Message, budget int, useTrunc bool) Result {
	states := make(map[int]State, len(messages))
	passNumber := make(map[int]int, len(messages))
	for _, m := range messages {
		states[m.ID] = StateKept
		passNumber[m.ID] = 0
	}

	maxRelax := 0

	for relax := 0; relax <= 4; relax++ {
		if totalCost(messages, states) <= budget {
			break
		}
		maxRelax = relax

		// Iterate at this relaxation level until no progress can be made.
		anyChange := true
		for anyChange && totalCost(messages, states) > budget {
			anyChange = false

			// Build the candidate list: not yet dropped AND forgettable at
			// this relaxation level, sorted by descending disposability score.
			candidates := make([]Message, 0, len(messages))
			for _, m := range messages {
				if states[m.ID] != StateDrop && canForget(m, relax, budget) {
					candidates = append(candidates, m)
				}
			}
			sort.SliceStable(candidates, func(i, j int) bool {
				return dispScore(candidates[i], budget) > dispScore(candidates[j], budget)
			})

			for _, m := range candidates {
				if totalCost(messages, states) <= budget {
					break
				}
				prev := states[m.ID]
				advance(m, states, useTrunc)
				if states[m.ID] != prev {
					passNumber[m.ID] = relax + 1 // 1-based pass number
					anyChange = true
				}
			}
		}
	}

	used := totalCost(messages, states)
	floor := hardFloor(messages)

	return Result{
		States:      states,
		PassNumber:  passNumber,
		Used:        used,
		Floor:       floor,
		MaxRelax:    maxRelax,
		Unreachable: used > budget,
	}
}
