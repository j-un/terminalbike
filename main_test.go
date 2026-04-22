package main

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// newTestGame builds a minimal game suitable for update/handleKey tests.
// The player is placed on the middle lane with no obstacles or rivals, so
// individual tests can set up only the state they care about.
func newTestGame() *game {
	return &game{
		w:          80,
		h:          24,
		rng:        rand.New(rand.NewSource(1)),
		playerLane: numLanes / 2,
		speed:      idleSpeed,
		started:    true,
	}
}

// placeObstacleOnPlayerLane positions the player a few cells before a single
// obstacle of the given kind on the same lane, so that g.update(1.0) at
// idleSpeed crosses the obstacle exactly once.
func placeObstacleOnPlayerLane(g *game, kind obstacleKind) {
	g.playerLane = 2
	g.distance = 4.5
	g.obstacles = []obstacle{{x: 8, lane: 2, kind: kind}}
}

func TestRandObstacleKind_Distribution(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	counts := map[obstacleKind]int{}
	none := 0
	const n = 60000
	for i := 0; i < n; i++ {
		if k, ok := randObstacleKind(r); ok {
			counts[k]++
		} else {
			none++
		}
	}
	// Expected weights — block:mud:ramp:cool:none = 1:1:2:1:1 out of 6.
	approx := func(got, wantNum int) bool {
		want := float64(n) * float64(wantNum) / 6.0
		diff := float64(got) - want
		if diff < 0 {
			diff = -diff
		}
		return diff/want < 0.05 // within 5%
	}
	if !approx(counts[kindBlock], 1) {
		t.Errorf("block count out of range: got %d", counts[kindBlock])
	}
	if !approx(counts[kindMud], 1) {
		t.Errorf("mud count out of range: got %d", counts[kindMud])
	}
	if !approx(counts[kindRamp], 2) {
		t.Errorf("ramp count out of range: got %d", counts[kindRamp])
	}
	if !approx(counts[kindCoolZone], 1) {
		t.Errorf("cool count out of range: got %d", counts[kindCoolZone])
	}
	if !approx(none, 1) {
		t.Errorf("none count out of range: got %d", none)
	}
}

func TestUpdate_TurboOverheats(t *testing.T) {
	g := newTestGame()
	g.turboOn = true
	// Run enough ticks to push temp from 0 to tempMax (100) at 25 units/s.
	for i := 0; i < 100; i++ {
		g.update(0.05)
		if !g.turboOn {
			break
		}
	}
	if g.turboOn {
		t.Errorf("turbo should auto-disable after overheat, temp=%v", g.temp)
	}
	if g.temp < tempMax {
		t.Errorf("temp should have reached tempMax, got %v", g.temp)
	}
	if g.speed != idleSpeed {
		t.Errorf("speed should reset to idleSpeed after overheat, got %v", g.speed)
	}
}

func TestUpdate_CrashRecovery(t *testing.T) {
	g := newTestGame()
	g.crashed = true
	g.crashTimer = 1.2
	g.speed = 0
	g.accelOn = true
	g.turboOn = true

	// Not yet expired.
	g.update(0.5)
	if !g.crashed {
		t.Fatal("should still be crashed at 0.5s")
	}

	// Expires.
	g.update(1.0)
	if g.crashed {
		t.Error("should recover from crash after crashTimer elapses")
	}
	if g.speed != idleSpeed {
		t.Errorf("speed should reset to idleSpeed on recovery, got %v", g.speed)
	}
	if g.accelOn || g.turboOn {
		t.Error("input flags should reset on recovery")
	}
}

func TestUpdate_BlockCollision_Crashes(t *testing.T) {
	g := newTestGame()
	placeObstacleOnPlayerLane(g, kindBlock)

	g.update(1.0)

	if !g.crashed {
		t.Fatal("expected crash when hitting a block on player's lane")
	}
	if g.speed != 0 {
		t.Errorf("speed should be 0 on crash, got %v", g.speed)
	}
}

func TestUpdate_BlockCollision_IgnoredOnDifferentLane(t *testing.T) {
	g := newTestGame()
	g.playerLane = 2
	g.distance = 4.5
	g.obstacles = []obstacle{{x: 8, lane: 0, kind: kindBlock}}

	g.update(1.0)

	if g.crashed {
		t.Error("should not crash when block is on a different lane")
	}
}

func TestUpdate_BlockCollision_AvoidedWhileJumping(t *testing.T) {
	g := newTestGame()
	placeObstacleOnPlayerLane(g, kindBlock)
	g.jumping = true
	g.jumpVel = 18

	g.update(1.0)

	if g.crashed {
		t.Error("should not crash over a block while jumping")
	}
}

func TestUpdate_RampTriggersJump(t *testing.T) {
	g := newTestGame()
	placeObstacleOnPlayerLane(g, kindRamp)

	g.update(1.0)

	if !g.jumping {
		t.Error("ramp should trigger jump")
	}
	if g.crashed {
		t.Error("ramp should not crash")
	}
}

func TestUpdate_MudResetsSpeed(t *testing.T) {
	g := newTestGame()
	placeObstacleOnPlayerLane(g, kindMud)
	g.speed = 50
	g.accelOn = true
	g.turboOn = true

	g.update(0.2)

	if g.speed != idleSpeed {
		t.Errorf("mud should reset speed to idleSpeed, got %v", g.speed)
	}
	if g.accelOn || g.turboOn || g.brakeOn {
		t.Error("mud should clear input flags")
	}
	if g.crashed {
		t.Error("mud should not crash")
	}
}

func TestUpdate_CoolZoneResetsTemp(t *testing.T) {
	g := newTestGame()
	placeObstacleOnPlayerLane(g, kindCoolZone)
	g.temp = 80

	g.update(1.0)

	if g.temp != 0 {
		t.Errorf("cool zone should reset temp to 0, got %v", g.temp)
	}
}

func TestUpdate_CoolZoneSkippedWhileJumping(t *testing.T) {
	g := newTestGame()
	placeObstacleOnPlayerLane(g, kindCoolZone)
	g.temp = 80
	g.jumping = true
	g.jumpVel = 18

	g.update(1.0)

	if g.temp == 0 {
		t.Error("cool zone should NOT reset temp while jumping")
	}
}

func TestUpdate_Finish_EntersFinishing(t *testing.T) {
	g := newTestGame()
	g.distance = float64(trackLength - 5)
	g.speed = 40
	g.accelOn = true // hold accel so speed stays at accelCap

	g.update(0.2)

	if !g.finishing {
		t.Error("expected finishing=true after crossing trackLength")
	}
	if g.finished {
		t.Error("finished should remain false until player runs off-screen")
	}
	if g.distance != float64(trackLength) {
		t.Errorf("distance should clamp to trackLength, got %v", g.distance)
	}
	if g.finishSpeed != 40 {
		t.Errorf("finishSpeed should be 40, got %v", g.finishSpeed)
	}
}

func TestUpdate_FinishingRunOff(t *testing.T) {
	g := newTestGame()
	g.finishing = true
	g.finishSpeed = 40
	g.distance = float64(trackLength)
	g.rivals = []rival{{xf: float64(trackLength + 5), lane: 1, speed: 15}}

	g.update(1.0)

	// Player should advance beyond trackLength at finishSpeed
	wantDist := float64(trackLength) + 40.0
	if g.distance != wantDist {
		t.Errorf("distance should be %v, got %v", wantDist, g.distance)
	}
	// Rival should also keep moving
	wantRivalX := float64(trackLength+5) + 15.0
	if g.rivals[0].xf != wantRivalX {
		t.Errorf("rival xf should be %v, got %v", wantRivalX, g.rivals[0].xf)
	}
	// Not yet off-screen (playerCol + 40 < 80)
	if g.finished {
		t.Error("finished should remain false while player is still on-screen")
	}
}

func TestUpdate_FinishingRunOff_OffScreen(t *testing.T) {
	g := newTestGame()
	g.finishing = true
	g.finishSpeed = 40
	// Place player far enough that one tick pushes it off-screen (w=80)
	g.distance = float64(trackLength + g.w)

	g.update(1.0)

	if !g.finished {
		t.Error("expected finished=true after player runs off-screen")
	}
}

func TestUpdate_RivalCollision_RemovesRival(t *testing.T) {
	g := newTestGame()
	g.playerLane = 2
	g.distance = 4.5
	g.rivals = []rival{{xf: 8.0, lane: 2, speed: 0}}

	g.update(1.0)

	if !g.crashed {
		t.Error("expected crash on rival collision")
	}
	if len(g.rivals) != 0 {
		t.Errorf("collided rival should be removed, got %d remaining", len(g.rivals))
	}
}

func TestUpdate_CountdownPausesGameplay(t *testing.T) {
	g := newTestGame()
	g.countdown = 2.0
	g.speed = 25

	g.update(0.5)

	if g.distance != 0 {
		t.Errorf("distance should not advance during countdown, got %v", g.distance)
	}
	if g.elapsed != 0 {
		t.Errorf("elapsed should not advance during countdown, got %v", g.elapsed)
	}
	if g.countdown != 1.5 {
		t.Errorf("countdown should tick down by dt, want 1.5, got %v", g.countdown)
	}
}

func TestUpdate_CountdownExpiresAndResumes(t *testing.T) {
	g := newTestGame()
	g.countdown = 0.1

	// Overshooting dt should clamp countdown at 0 (no negative value).
	g.update(0.2)
	if g.countdown != 0 {
		t.Errorf("countdown should clamp to 0, got %v", g.countdown)
	}

	// Once the countdown has expired, the next tick should advance distance.
	g.update(1.0)
	if g.distance == 0 {
		t.Error("distance should advance once countdown expires")
	}
}

func TestHandleKey_WASDControls(t *testing.T) {
	g := newTestGame()
	startLane := g.playerLane

	// w = lane up
	g.handleKey(tcell.NewEventKey(tcell.KeyRune, 'w', tcell.ModNone))
	if g.playerLane != startLane-1 {
		t.Errorf("'w' should move lane up: want %d, got %d", startLane-1, g.playerLane)
	}

	// s = lane down (back to start)
	g.handleKey(tcell.NewEventKey(tcell.KeyRune, 's', tcell.ModNone))
	if g.playerLane != startLane {
		t.Errorf("'s' should move lane down: want %d, got %d", startLane, g.playerLane)
	}

	// d = accel
	g.handleKey(tcell.NewEventKey(tcell.KeyRune, 'd', tcell.ModNone))
	if !g.accelOn || g.brakeOn {
		t.Error("'d' should enable accel and clear brake")
	}

	// a = brake
	g.handleKey(tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone))
	if !g.brakeOn || g.accelOn {
		t.Error("'a' should enable brake and clear accel")
	}
}

func TestHandleKey_HJKLControls(t *testing.T) {
	g := newTestGame()
	startLane := g.playerLane

	// k = lane up
	g.handleKey(tcell.NewEventKey(tcell.KeyRune, 'k', tcell.ModNone))
	if g.playerLane != startLane-1 {
		t.Errorf("'k' should move lane up: want %d, got %d", startLane-1, g.playerLane)
	}

	// j = lane down (back to start)
	g.handleKey(tcell.NewEventKey(tcell.KeyRune, 'j', tcell.ModNone))
	if g.playerLane != startLane {
		t.Errorf("'j' should move lane down: want %d, got %d", startLane, g.playerLane)
	}

	// l = accel
	g.handleKey(tcell.NewEventKey(tcell.KeyRune, 'l', tcell.ModNone))
	if !g.accelOn || g.brakeOn {
		t.Error("'l' should enable accel and clear brake")
	}

	// h = brake
	g.handleKey(tcell.NewEventKey(tcell.KeyRune, 'h', tcell.ModNone))
	if !g.brakeOn || g.accelOn {
		t.Error("'h' should enable brake and clear accel")
	}
}

func TestHandleKey_WASDBlockedWhileJumping(t *testing.T) {
	g := newTestGame()
	g.jumping = true
	start := g.playerLane

	g.handleKey(tcell.NewEventKey(tcell.KeyRune, 'w', tcell.ModNone))
	if g.playerLane != start {
		t.Error("'w' should not change lane while jumping")
	}
	g.handleKey(tcell.NewEventKey(tcell.KeyRune, 's', tcell.ModNone))
	if g.playerLane != start {
		t.Error("'s' should not change lane while jumping")
	}
}

func TestHandleKey_HJKLBlockedWhileJumping(t *testing.T) {
	g := newTestGame()
	g.jumping = true
	start := g.playerLane

	g.handleKey(tcell.NewEventKey(tcell.KeyRune, 'k', tcell.ModNone))
	if g.playerLane != start {
		t.Error("'k' should not change lane while jumping")
	}
	g.handleKey(tcell.NewEventKey(tcell.KeyRune, 'j', tcell.ModNone))
	if g.playerLane != start {
		t.Error("'j' should not change lane while jumping")
	}
}

func TestHandleKey_WASDBlockedWhileCrashed(t *testing.T) {
	g := newTestGame()
	g.crashed = true
	start := g.playerLane

	g.handleKey(tcell.NewEventKey(tcell.KeyRune, 'w', tcell.ModNone))
	if g.playerLane != start {
		t.Error("'w' should not change lane while crashed")
	}
	g.handleKey(tcell.NewEventKey(tcell.KeyRune, 'd', tcell.ModNone))
	if g.accelOn {
		t.Error("'d' should not enable accel while crashed")
	}
}

func TestHandleKey_LaneUpClampedAtTop(t *testing.T) {
	g := newTestGame()
	g.playerLane = 0

	g.handleKey(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone))
	if g.playerLane != 0 {
		t.Errorf("lane should not go below 0, got %d", g.playerLane)
	}

	g.handleKey(tcell.NewEventKey(tcell.KeyRune, 'w', tcell.ModNone))
	if g.playerLane != 0 {
		t.Errorf("'w' should not push lane below 0, got %d", g.playerLane)
	}
}

func TestHandleKey_LaneDownClampedAtBottom(t *testing.T) {
	g := newTestGame()
	g.playerLane = numLanes - 1

	g.handleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	if g.playerLane != numLanes-1 {
		t.Errorf("lane should not exceed numLanes-1, got %d", g.playerLane)
	}

	g.handleKey(tcell.NewEventKey(tcell.KeyRune, 's', tcell.ModNone))
	if g.playerLane != numLanes-1 {
		t.Errorf("'s' should not push lane past numLanes-1, got %d", g.playerLane)
	}
}

func TestHandleKey_NoLaneChangeWhileJumping(t *testing.T) {
	g := newTestGame()
	g.jumping = true
	start := g.playerLane

	g.handleKey(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone))
	if g.playerLane != start {
		t.Errorf("lane should not change while jumping, went %d -> %d", start, g.playerLane)
	}

	g.handleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	if g.playerLane != start {
		t.Errorf("lane should not change while jumping (down), went %d -> %d", start, g.playerLane)
	}
}

func TestHandleKey_NoLaneChangeWhileCrashed(t *testing.T) {
	g := newTestGame()
	g.crashed = true
	start := g.playerLane

	g.handleKey(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone))
	if g.playerLane != start {
		t.Errorf("lane should not change while crashed")
	}
}

func TestHandleKey_TurboDebounce(t *testing.T) {
	g := newTestGame()
	space := tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone)

	g.handleKey(space)
	if !g.turboOn {
		t.Fatal("first space should enable turbo")
	}

	// Second press within debounce window is ignored.
	g.handleKey(space)
	if !g.turboOn {
		t.Error("second space within debounce should not toggle turbo off")
	}

	// Let the cooldown expire.
	g.update(0.2)

	g.handleKey(space)
	if g.turboOn {
		t.Error("space after cooldown should toggle turbo off")
	}
}

func TestHandleKey_TurboBlockedWhenOverheated(t *testing.T) {
	g := newTestGame()
	g.temp = tempMax
	space := tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone)

	g.handleKey(space)
	if g.turboOn {
		t.Error("turbo should not engage at tempMax")
	}
}

func TestHandleKey_RestartOnFinish(t *testing.T) {
	g := newTestGame()
	g.finishing = true
	g.distance = float64(trackLength)
	g.elapsed = 123
	g.bestTimes = []float64{10.10, 20.20, 30.30}
	g.lastBestRank = 1

	g.handleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))

	if g.finishing {
		t.Error("restart should clear finishing")
	}
	if !g.started {
		t.Error("restart should set started=true")
	}
	if g.countdown != 3.0 {
		t.Errorf("restart should arm the 3s countdown, got %v", g.countdown)
	}
	if g.distance != 0 {
		t.Errorf("distance should reset, got %v", g.distance)
	}
	if g.elapsed != 0 {
		t.Errorf("elapsed should reset, got %v", g.elapsed)
	}
	// A fresh course should have been generated.
	if len(g.obstacles) == 0 {
		t.Error("restart should regenerate the course")
	}
	// Previously recorded best times must survive the restart.
	want := []float64{10.10, 20.20, 30.30}
	if len(g.bestTimes) != len(want) {
		t.Fatalf("bestTimes should be preserved across restart, got %v", g.bestTimes)
	}
	for i, v := range want {
		if g.bestTimes[i] != v {
			t.Errorf("bestTimes[%d] = %v, want %v", i, g.bestTimes[i], v)
		}
	}
}

func TestHandleKey_FinishingBlocksGameplay(t *testing.T) {
	g := newTestGame()
	g.finishing = true
	startLane := g.playerLane

	// Gameplay keys should be ignored
	g.handleKey(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone))
	if g.playerLane != startLane {
		t.Error("lane change should be blocked during finishing")
	}
	g.handleKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))
	if g.accelOn {
		t.Error("accel should be blocked during finishing")
	}

	// Quit should still work
	if !g.handleKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)) {
		t.Error("Escape should signal quit during finishing")
	}
}

func TestHandleKey_BlockedDuringCountdown(t *testing.T) {
	g := newTestGame()
	g.countdown = 2.0
	startLane := g.playerLane

	g.handleKey(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone))
	if g.playerLane != startLane {
		t.Error("lane change should be blocked during countdown")
	}
	g.handleKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))
	if g.accelOn {
		t.Error("accel should be blocked during countdown")
	}
	g.handleKey(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone))
	if g.brakeOn {
		t.Error("brake should be blocked during countdown")
	}
	g.handleKey(tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone))
	if g.turboOn {
		t.Error("turbo should be blocked during countdown")
	}
}

func TestHandleKey_QuitDuringCountdown(t *testing.T) {
	g := newTestGame()
	g.countdown = 2.0

	if !g.handleKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)) {
		t.Error("Escape should signal quit during countdown")
	}
	if !g.handleKey(tcell.NewEventKey(tcell.KeyRune, 'q', tcell.ModNone)) {
		t.Error("'q' should signal quit during countdown")
	}
}

func TestHandleKey_StartTriggersCountdown(t *testing.T) {
	// Enter from the opening screen starts the game with a 3s countdown.
	g := newTestGame()
	g.started = false

	g.handleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if !g.started {
		t.Error("Enter from opening should set started=true")
	}
	if g.countdown != 3.0 {
		t.Errorf("Enter from opening should arm 3s countdown, got %v", g.countdown)
	}

	// Space from the opening should behave the same way.
	g2 := newTestGame()
	g2.started = false
	g2.handleKey(tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone))
	if !g2.started || g2.countdown != 3.0 {
		t.Errorf("Space from opening should start+arm countdown, started=%v countdown=%v", g2.started, g2.countdown)
	}
}

func TestRecordTime_FirstFinish(t *testing.T) {
	g := newTestGame()
	g.recordTime(42.5)

	if len(g.bestTimes) != 1 || g.bestTimes[0] != 42.5 {
		t.Errorf("expected [42.5], got %v", g.bestTimes)
	}
	if g.lastBestRank != 1 {
		t.Errorf("first finish should rank 1, got %d", g.lastBestRank)
	}
}

func TestRecordTime_SortsAscending(t *testing.T) {
	g := newTestGame()
	g.recordTime(50.12)
	g.recordTime(30.45)
	g.recordTime(40.78)

	want := []float64{30.45, 40.78, 50.12}
	if len(g.bestTimes) != 3 {
		t.Fatalf("expected 3 entries, got %v", g.bestTimes)
	}
	for i, v := range want {
		if g.bestTimes[i] != v {
			t.Errorf("bestTimes[%d] = %v, want %v", i, g.bestTimes[i], v)
		}
	}
}

func TestRecordTime_KeepsTopThree(t *testing.T) {
	g := newTestGame()
	for _, t0 := range []float64{50.11, 30.22, 40.33, 20.44, 60.55} {
		g.recordTime(t0)
	}

	want := []float64{20.44, 30.22, 40.33}
	if len(g.bestTimes) != 3 {
		t.Fatalf("should retain only top 3, got %v", g.bestTimes)
	}
	for i, v := range want {
		if g.bestTimes[i] != v {
			t.Errorf("bestTimes[%d] = %v, want %v", i, g.bestTimes[i], v)
		}
	}
}

func TestRecordTime_OutOfTopThreeRanksZero(t *testing.T) {
	g := newTestGame()
	g.recordTime(10.11)
	g.recordTime(20.22)
	g.recordTime(30.33)

	g.recordTime(99.99) // slower than all existing entries

	if g.lastBestRank != 0 {
		t.Errorf("out-of-top-3 time should rank 0, got %d", g.lastBestRank)
	}
	// Top 3 should be unchanged.
	want := []float64{10.11, 20.22, 30.33}
	for i, v := range want {
		if g.bestTimes[i] != v {
			t.Errorf("bestTimes[%d] = %v, want %v", i, g.bestTimes[i], v)
		}
	}
}

func TestRecordTime_NewBestUpdatesRank(t *testing.T) {
	g := newTestGame()
	g.recordTime(40.40)
	g.recordTime(30.30)
	g.recordTime(20.20)

	g.recordTime(15.15)
	if g.lastBestRank != 1 {
		t.Errorf("new fastest time should rank 1, got %d", g.lastBestRank)
	}

	// bestTimes is now [15.15, 20.20, 30.30]; adding 25.25 yields
	// [15.15, 20.20, 25.25] (rank 3).
	g.recordTime(25.25)
	if g.lastBestRank != 3 {
		t.Errorf("25.25 should rank 3 in [15.15,20.20,25.25], got %d", g.lastBestRank)
	}
}

func TestRecordTime_TieWithWorstKeepsNewEntry(t *testing.T) {
	g := newTestGame()
	g.recordTime(10.10)
	g.recordTime(20.20)
	g.recordTime(30.30)

	// Tie with the current 3rd-place time while the board is full.
	// The new entry should take the 3rd slot and the old 30.30 should
	// fall off, keeping bestTimes and lastBestRank consistent.
	g.recordTime(30.30)

	want := []float64{10.10, 20.20, 30.30}
	if len(g.bestTimes) != 3 {
		t.Fatalf("expected 3 entries, got %v", g.bestTimes)
	}
	for i, v := range want {
		if g.bestTimes[i] != v {
			t.Errorf("bestTimes[%d] = %v, want %v", i, g.bestTimes[i], v)
		}
	}
	if g.lastBestRank != 3 {
		t.Errorf("tied worst time should rank 3, got %d", g.lastBestRank)
	}
}

func TestGenerateCourse_NoDuplicateObstaclesOnSameLane(t *testing.T) {
	// Run multiple seeds to exercise different random paths.
	for seed := int64(0); seed < 50; seed++ {
		rng := rand.New(rand.NewSource(seed))
		g := newGame(80, 24, rng)

		type pos struct{ x, lane int }
		seen := make(map[pos]bool)
		for _, o := range g.obstacles {
			p := pos{o.x, o.lane}
			if seen[p] {
				t.Errorf("seed %d: duplicate obstacle at x=%d lane=%d", seed, o.x, o.lane)
			}
			seen[p] = true
		}
	}
}

// newSimScreen returns an initialized simulation screen sized to the test
// game's default (80x24). The caller is responsible for calling Fini().
func newSimScreen(t *testing.T) tcell.SimulationScreen {
	t.Helper()
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil {
		t.Fatalf("sim screen init: %v", err)
	}
	s.SetSize(80, 24)
	return s
}

func TestDraw_PlayerRendersAtFixedColumnDuringPlay(t *testing.T) {
	g := newTestGame()
	g.distance = 100
	s := newSimScreen(t)
	defer s.Fini()

	g.draw(s)

	laneY := headerRows + 1 + g.playerLane*laneHeight
	mainc, _, _, _ := s.GetContent(playerCol, laneY)
	if mainc != '>' {
		t.Errorf("player '>' should render at col %d lane-y %d, got %q", playerCol, laneY, mainc)
	}
}

func TestDraw_PlayerMovesRightDuringFinishing(t *testing.T) {
	// During the finishing run-off the camera freezes at trackLength, so
	// the player sprite is expected to appear to the right of playerCol.
	g := newTestGame()
	g.finishing = true
	g.distance = float64(trackLength + 10)
	s := newSimScreen(t)
	defer s.Fini()

	g.draw(s)

	laneY := headerRows + 1 + g.playerLane*laneHeight
	wantX := playerCol + 10
	mainc, _, _, _ := s.GetContent(wantX, laneY)
	if mainc != '>' {
		t.Errorf("player '>' should render at col %d during finishing, got %q", wantX, mainc)
	}
	// Sanity: player is no longer at playerCol.
	mainc, _, _, _ = s.GetContent(playerCol, laneY)
	if mainc == '>' {
		t.Error("player should have moved off playerCol during finishing")
	}
}

func TestDraw_OpeningScreenBeforeStart(t *testing.T) {
	g := newTestGame()
	g.started = false
	s := newSimScreen(t)
	defer s.Fini()

	g.draw(s)
	s.Show()

	// The opening screen renders the start prompt with plain text; pick a
	// distinctive substring and look for it anywhere on the screen.
	const needle = "Enter"
	cells, w, h := s.GetContents()
	found := false
	for y := 0; y < h && !found; y++ {
		var row []rune
		for x := 0; x < w; x++ {
			row = append(row, cells[y*w+x].Runes...)
		}
		if strings.Contains(string(row), needle) {
			found = true
		}
	}
	if !found {
		t.Error("opening screen should render the start prompt containing 'Enter'")
	}
}

func TestHandleKey_QuitReturnsTrue(t *testing.T) {
	g := newTestGame()
	if !g.handleKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)) {
		t.Error("Escape should signal quit")
	}
	if !g.handleKey(tcell.NewEventKey(tcell.KeyRune, 'q', tcell.ModNone)) {
		t.Error("'q' should signal quit")
	}
}
