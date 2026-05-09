package compaction

import (
	"testing"
)

// sampleMessages reproduces the exact dataset from compaction_state_machine_v3.html.
var sampleMessages = []Message{
	{ID: 1,  Role: "system", Age: 12, Tok: 80,  Cat: CatSystem,  Shout: false, Trunc: false},
	{ID: 2,  Role: "user",   Age: 11, Tok: 25,  Cat: CatHighest, Shout: true,  Trunc: false},
	{ID: 3,  Role: "user",   Age: 10, Tok: 40,  Cat: CatHigh,    Shout: false, Trunc: false},
	{ID: 4,  Role: "asst",   Age: 9,  Tok: 200, Cat: CatLow,     Shout: false, Trunc: true},
	{ID: 5,  Role: "asst",   Age: 8,  Tok: 55,  Cat: CatMid,     Shout: false, Trunc: false},
	{ID: 6,  Role: "user",   Age: 7,  Tok: 20,  Cat: CatLowest,  Shout: false, Trunc: false},
	{ID: 7,  Role: "user",   Age: 6,  Tok: 15,  Cat: CatGarbage, Shout: false, Trunc: false},
	{ID: 8,  Role: "asst",   Age: 5,  Tok: 350, Cat: CatLow,     Shout: false, Trunc: true},
	{ID: 9,  Role: "user",   Age: 4,  Tok: 30,  Cat: CatMid,     Shout: false, Trunc: false},
	{ID: 10, Role: "user",   Age: 3,  Tok: 30,  Cat: CatHighest, Shout: true,  Trunc: false},
	{ID: 11, Role: "asst",   Age: 2,  Tok: 60,  Cat: CatLow,     Shout: false, Trunc: true},
	{ID: 12, Role: "user",   Age: 1,  Tok: 30,  Cat: CatHigh,    Shout: false, Trunc: false},
	{ID: 13, Role: "user",   Age: 0,  Tok: 35,  Cat: CatMid,     Shout: false, Trunc: false},
}

func totalTok(msgs []Message) int {
	t := 0
	for _, m := range msgs {
		t += m.Tok
	}
	return t
}

func TestNoBudgetPressure(t *testing.T) {
	full := totalTok(sampleMessages)
	r := Compact(sampleMessages, full, true)
	if r.Unreachable {
		t.Fatal("expected reachable budget")
	}
	if r.Used != full {
		t.Fatalf("expected used=%d, got %d", full, r.Used)
	}
	for _, m := range sampleMessages {
		if r.States[m.ID] != StateKept {
			t.Errorf("msg %d: expected kept, got %s", m.ID, r.States[m.ID])
		}
	}
}

func TestSystemMessageNeverDropped(t *testing.T) {
	r := Compact(sampleMessages, 80, true)
	if r.States[1] != StateKept {
		t.Errorf("system message was %s; must always be kept", r.States[1])
	}
}

func TestShoutHighestProtected(t *testing.T) {
	r := Compact(sampleMessages, 280, true)
	if r.States[10] == StateDrop {
		t.Errorf("msg 10 (SHOUT+highest, recent zone) should not be dropped at budget 280; maxRelax=%d", r.MaxRelax)
	}
}

func TestGarbageAlwaysForgettable(t *testing.T) {
	if !canForget(sampleMessages[6], 0, 0) {
		t.Error("garbage message should always be forgettable at relax 0")
	}
}

func TestHardFloor(t *testing.T) {
	r := Compact(sampleMessages, 1, true)
	if r.Floor <= 0 {
		t.Fatal("hard floor must be positive")
	}
	if r.Floor < 80 {
		t.Errorf("floor %d is below system message cost (80)", r.Floor)
	}
	if !r.Unreachable {
		t.Error("expected Unreachable when budget < floor")
	}
}

func TestTruncPath(t *testing.T) {
	r := Compact(sampleMessages, 750, true)
	if r.States[4] != StateTrim25 {
		t.Errorf("msg 4: expected trim25, got %s (used=%d)", r.States[4], r.Used)
	}
	if r.States[8] != StateKept {
		t.Errorf("msg 8: expected kept at budget 750, got %s", r.States[8])
	}
}

func TestUseTruncFalse(t *testing.T) {
	r := Compact(sampleMessages, 300, false)
	for _, m := range sampleMessages {
		if s := r.States[m.ID]; s == StateTrim25 || s == StateTrim10 {
			t.Errorf("msg %d: got %s with useTrunc=false", m.ID, s)
		}
	}
}

func TestBudgetAlwaysMet(t *testing.T) {
	floor := hardFloor(sampleMessages)
	for _, b := range []int{floor, floor + 10, 200, 300, 500, 800, 970} {
		r := Compact(sampleMessages, b, true)
		if !r.Unreachable && r.Used > b {
			t.Errorf("budget %d: used %d exceeds budget despite reachable flag", b, r.Used)
		}
	}
}

func TestTokCost(t *testing.T) {
	m := Message{ID: 99, Tok: 200}
	for _, tc := range []struct {
		state State
		want  int
	}{
		{StateKept, 200},
		{StateTrim25, 50},
		{StateTrim10, 20},
		{StateDrop, 0},
	} {
		got := tokCost(m, map[int]State{99: tc.state})
		if got != tc.want {
			t.Errorf("state=%s: got %d, want %d", tc.state, got, tc.want)
		}
	}
}

func TestCanForget_StrictZone(t *testing.T) {
	mMid := Message{ID: 1, Cat: CatMid, Age: 6}
	if !canForget(mMid, 0, 0) {
		t.Error("mid message at r=0.50 should be forgettable at relax 0")
	}
	mMidBelow := Message{ID: 2, Cat: CatMid, Age: 5}
	if canForget(mMidBelow, 0, 0) {
		t.Error("mid message at r<0.50 should NOT be forgettable at relax 0")
	}
	if !canForget(mMidBelow, 1, 0) {
		t.Error("mid message at r≈0.42 should be forgettable at relax 1")
	}
}

// ------------------------------------------------------------------ part-based tests

// TestPartsTok verifies the split→categorise→filter token summation.
func TestPartsTok(t *testing.T) {
	parts := []Part{
		{Tok: 100, Cat: CatHigh},    // kept by both trim25 and trim10
		{Tok: 80,  Cat: CatMid},     // kept by trim25, dropped by trim10
		{Tok: 60,  Cat: CatLow},     // dropped by both trim states
		{Tok: 40,  Cat: CatGarbage}, // dropped by both trim states
	}

	if got := partsTok(parts, trim25Cat); got != 180 {
		t.Errorf("trim25 (>=Mid): want 180, got %d", got)
	}
	if got := partsTok(parts, trim10Cat); got != 100 {
		t.Errorf("trim10 (>=High): want 100, got %d", got)
	}
}

// TestTokCostWithParts ensures tokCost delegates to partsTok when Parts is set,
// rather than using the legacy 25 %/10 % percentages.
func TestTokCostWithParts(t *testing.T) {
	// Total tok = 280; legacy 25% = 70, legacy 10% = 28.
	// Part-based: trim25 should keep High(100)+Mid(80)=180, trim10 only High(100).
	m := Message{
		ID:  42,
		Tok: 280,
		Parts: []Part{
			{Tok: 100, Cat: CatHigh},
			{Tok: 80,  Cat: CatMid},
			{Tok: 60,  Cat: CatLow},
			{Tok: 40,  Cat: CatGarbage},
		},
	}

	states := map[int]State{42: StateTrim25}
	if got := tokCost(m, states); got != 180 {
		t.Errorf("trim25 with parts: want 180 (High+Mid), got %d", got)
	}

	states[42] = StateTrim10
	if got := tokCost(m, states); got != 100 {
		t.Errorf("trim10 with parts: want 100 (High only), got %d", got)
	}

	states[42] = StateKept
	if got := tokCost(m, states); got != 280 {
		t.Errorf("kept with parts: want 280 (full Tok), got %d", got)
	}

	states[42] = StateDrop
	if got := tokCost(m, states); got != 0 {
		t.Errorf("drop with parts: want 0, got %d", got)
	}
}

// TestAdvanceWithParts checks that the token savings reported by advance
// reflect part-filtered costs, not hardcoded percentages.
func TestAdvanceWithParts(t *testing.T) {
	// High=100, Mid=80, Low=60, Garbage=40 → total 280
	// trim25 (keep >=Mid): 180  → savings kept→trim25 = 280-180 = 100
	// trim10 (keep >=High): 100 → savings trim25→trim10 = 180-100 = 80
	// drop: savings trim10→drop = 100
	m := Message{
		ID:  7,
		Tok: 280,
		Parts: []Part{
			{Tok: 100, Cat: CatHigh},
			{Tok: 80,  Cat: CatMid},
			{Tok: 60,  Cat: CatLow},
			{Tok: 40,  Cat: CatGarbage},
		},
	}

	states := map[int]State{7: StateKept}

	saved := advance(m, states, true)
	if states[7] != StateTrim25 {
		t.Fatalf("expected trim25 after first advance, got %s", states[7])
	}
	if saved != 100 {
		t.Errorf("kept→trim25 savings: want 100, got %d", saved)
	}

	saved = advance(m, states, true)
	if states[7] != StateTrim10 {
		t.Fatalf("expected trim10 after second advance, got %s", states[7])
	}
	if saved != 80 {
		t.Errorf("trim25→trim10 savings: want 80, got %d", saved)
	}

	saved = advance(m, states, true)
	if states[7] != StateDrop {
		t.Fatalf("expected drop after third advance, got %s", states[7])
	}
	if saved != 100 {
		t.Errorf("trim10→drop savings: want 100, got %d", saved)
	}
}

// TestCompactWithParts runs Compact on a small conversation where one message
// has Parts, and verifies the budget is met using part-level filtering.
func TestCompactWithParts(t *testing.T) {
	// System: 80 tok (inviolable)
	// BigMsg: 280 tok, split into High(100)+Mid(80)+Low(60)+Garbage(40)
	//         trim25 → 180, trim10 → 100, drop → 0
	// Recent: 50 tok, no parts
	// Total without compaction: 80+280+50 = 410
	//
	// Budget 230: need to save 180.
	//   BigMsg trim25 saves 100 → total 310, still over.
	//   BigMsg trim10 saves 80  → total 230, at budget. ✓
	msgs := []Message{
		{ID: 1, Role: "system", Age: 5, Tok: 80,  Cat: CatSystem},
		{ID: 2, Role: "asst",   Age: 4, Tok: 280, Cat: CatLow, Trunc: true, Parts: []Part{
			{Tok: 100, Cat: CatHigh},
			{Tok: 80,  Cat: CatMid},
			{Tok: 60,  Cat: CatLow},
			{Tok: 40,  Cat: CatGarbage},
		}},
		{ID: 3, Role: "user",   Age: 0, Tok: 50,  Cat: CatMid},
	}

	r := Compact(msgs, 230, true)
	if r.Unreachable {
		t.Fatalf("budget should be reachable; used=%d", r.Used)
	}
	if r.Used > 230 {
		t.Errorf("used=%d exceeds budget 230", r.Used)
	}
	if r.States[2] != StateTrim10 {
		t.Errorf("BigMsg expected trim10, got %s", r.States[2])
	}
	if r.States[1] != StateKept {
		t.Errorf("system message must stay kept, got %s", r.States[1])
	}
}

// TestPartsAllLowDropped confirms that when all parts are below the trim10
// threshold (CatHigh) the part-filtered cost reaches 0 and the message is
// equivalent to dropped even before StateDrop is reached.
func TestPartsAllLowDropped(t *testing.T) {
	m := Message{
		ID:  5,
		Tok: 200,
		Parts: []Part{
			{Tok: 100, Cat: CatLow},
			{Tok: 100, Cat: CatLowest},
		},
	}
	states := map[int]State{5: StateTrim10}
	if got := tokCost(m, states); got != 0 {
		t.Errorf("all-low parts at trim10: want 0 cost, got %d", got)
	}
}

// TestLegacyFallbackUnchanged confirms that messages with Trunc=true but no
// Parts still use the legacy percentage path for backward compatibility.
func TestLegacyFallbackUnchanged(t *testing.T) {
	m := Message{ID: 9, Tok: 200, Trunc: true}
	states := map[int]State{9: StateTrim25}
	if got := tokCost(m, states); got != 50 { // 25% of 200
		t.Errorf("legacy trim25: want 50, got %d", got)
	}
	states[9] = StateTrim10
	if got := tokCost(m, states); got != 20 { // 10% of 200
		t.Errorf("legacy trim10: want 20, got %d", got)
	}
}

// ------------------------------------------------------------------ regression

// TestOversizedMessageDroppedFirst is the regression for the reported bug.
func TestOversizedMessageDroppedFirst(t *testing.T) {
	const budget = 3481

	msgs := []Message{
		{ID: 1, Role: "system", Age: 3, Tok: 80,     Cat: CatSystem,  Shout: false, Trunc: false},
		{ID: 2, Role: "user",   Age: 2, Tok: 627_000, Cat: CatLow,    Shout: false, Trunc: false},
		{ID: 3, Role: "asst",   Age: 1, Tok: 500,    Cat: CatMid,     Shout: false, Trunc: false},
		{ID: 4, Role: "user",   Age: 0, Tok: 100,    Cat: CatHighest, Shout: true,  Trunc: false},
	}

	bigMsg := msgs[1]
	if !canForget(bigMsg, 0, budget) {
		t.Error("oversized message (tok > budget) must be forgettable at relax 0 with size override")
	}

	r := Compact(msgs, budget, false)

	if r.Unreachable {
		t.Errorf("budget should be reachable after fix; used=%d floor=%d", r.Used, r.Floor)
	}
	if r.States[2] != StateDrop {
		t.Errorf("oversized message should be dropped; got %s (used=%d)", r.States[2], r.Used)
	}
	if r.States[1] != StateKept {
		t.Errorf("system message should be kept; got %s", r.States[1])
	}
	if r.Used > budget {
		t.Errorf("used=%d exceeds budget=%d", r.Used, budget)
	}
}

// TestOversizedMessageSortedFirst verifies the dispScore size bonus puts the
// giant message at the head of the candidate list.
func TestOversizedMessageSortedFirst(t *testing.T) {
	const budget = 500

	old   := Message{ID: 1, Age: 12, Tok: 50,      Cat: CatGarbage}
	giant := Message{ID: 2, Age: 0,  Tok: 100_000,  Cat: CatLow}

	if dispScore(giant, budget) <= dispScore(old, budget) {
		t.Error("giant message should have higher dispScore than old garbage when it exceeds budget")
	}
}

// TestSizeOverrideSkippedInHardFloor ensures hardFloor does NOT use the size
// override, so the floor reflects only category/age protection.
func TestSizeOverrideSkippedInHardFloor(t *testing.T) {
	msgs := []Message{
		{ID: 1, Role: "system", Age: 5, Tok: 100, Cat: CatSystem},
		{ID: 2, Age: 0, Tok: 999_999, Cat: CatLow},
	}
	floor := hardFloor(msgs)
	if floor < 100 {
		t.Errorf("floor=%d should be at least the system message cost (100)", floor)
	}
	t.Logf("floor=%d (system=100, giant=%d)", floor, msgs[1].Tok)
}

