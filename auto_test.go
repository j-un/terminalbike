package main

import (
	"math/rand"
	"testing"
)

// newAutoGame builds a game ready for auto-play decisions: started, in
// auto mode, on the middle lane, with a deterministic rng seed.
func newAutoGame() *game {
	return &game{
		w:          80,
		h:          24,
		rng:        rand.New(rand.NewSource(1)),
		playerLane: numLanes / 2,
		speed:      idleSpeed,
		started:    true,
		autoMode:   true,
	}
}

// ----- autoStep guards -----

func TestAutoStep_NoOpWhenCrashed(t *testing.T) {
	g := newAutoGame()
	g.crashed = true
	g.crashTimer = 1.0

	g.autoStep(0.05)

	if g.accelOn || g.brakeOn || g.turboOn {
		t.Error("auto should not act while crashed")
	}
}

func TestAutoStep_NoOpDuringCountdown(t *testing.T) {
	g := newAutoGame()
	g.countdown = 2.0

	g.autoStep(0.05)

	if g.accelOn || g.brakeOn || g.turboOn {
		t.Error("auto should not act during countdown")
	}
}

func TestAutoStep_NoOpWhenNotAutoMode(t *testing.T) {
	g := newAutoGame()
	g.autoMode = false

	g.autoStep(0.05)

	if g.accelOn || g.brakeOn || g.turboOn {
		t.Error("auto should not act when autoMode is off")
	}
}

func TestAutoStep_NoOpWhileFinishing(t *testing.T) {
	g := newAutoGame()
	g.finishing = true

	g.autoStep(0.05)

	if g.accelOn || g.brakeOn || g.turboOn {
		t.Error("auto should not act during finishing run-off")
	}
}

func TestAutoStep_NoOpWhenFinished(t *testing.T) {
	g := newAutoGame()
	g.finished = true

	g.autoStep(0.05)

	if g.accelOn || g.brakeOn || g.turboOn {
		t.Error("auto should not act after finished")
	}
}

func TestAutoStep_RespectsCooldown(t *testing.T) {
	g := newAutoGame()
	g.autoCooldown = 0.5

	g.autoStep(0.05)

	if g.accelOn || g.brakeOn || g.turboOn {
		t.Error("auto should defer decisions while cooldown is active")
	}
	if g.autoCooldown <= 0.4 {
		t.Errorf("autoCooldown should decrement by dt, got %v", g.autoCooldown)
	}
}

// ----- chooseAutoAction (pure decision logic) -----

func TestChooseAutoAction_DodgesBlockOnPlayerLane(t *testing.T) {
	g := newAutoGame()
	g.playerLane = 2
	g.distance = 0
	g.obstacles = []obstacle{{x: 6, lane: 2, kind: kindBlock}}

	action := chooseAutoAction(g)

	if action != "up" && action != "down" {
		t.Errorf("auto should dodge block by changing lane, got %q", action)
	}
}

func TestChooseAutoAction_DodgesRivalOnPlayerLane(t *testing.T) {
	g := newAutoGame()
	g.playerLane = 2
	g.distance = 0
	g.rivals = []rival{{xf: 6, lane: 2, speed: 0}}

	action := chooseAutoAction(g)

	if action != "up" && action != "down" {
		t.Errorf("auto should dodge rival by changing lane, got %q", action)
	}
}

func TestChooseAutoAction_DodgesMudOnPlayerLane(t *testing.T) {
	g := newAutoGame()
	g.playerLane = 2
	g.distance = 0
	g.obstacles = []obstacle{{x: 6, lane: 2, kind: kindMud}}

	action := chooseAutoAction(g)

	if action != "up" && action != "down" {
		t.Errorf("auto should treat mud as a hazard and dodge, got %q", action)
	}
}

func TestChooseAutoAction_BrakesWhenCornered(t *testing.T) {
	g := newAutoGame()
	g.playerLane = 2
	g.distance = 0
	g.obstacles = []obstacle{
		{x: 3, lane: 1, kind: kindBlock},
		{x: 3, lane: 2, kind: kindBlock},
		{x: 3, lane: 3, kind: kindBlock},
	}

	action := chooseAutoAction(g)

	if action != "brake" {
		t.Errorf("auto should brake when no escape lane is available, got %q", action)
	}
}

func TestChooseAutoAction_TurboInOpenTrack(t *testing.T) {
	g := newAutoGame()
	g.playerLane = 2
	g.distance = 0
	g.temp = 10

	action := chooseAutoAction(g)

	if action != "turbo" {
		t.Errorf("auto should engage turbo on open track when cool, got %q", action)
	}
}

func TestChooseAutoAction_DisablesTurboWhenOverheating(t *testing.T) {
	g := newAutoGame()
	g.playerLane = 2
	g.distance = 0
	g.temp = 90
	g.turboOn = true

	action := chooseAutoAction(g)

	if action != "turbo" {
		t.Errorf("auto should toggle turbo off when too hot, got %q", action)
	}
}

func TestChooseAutoAction_PreferLaneWithCoolZoneWhenHot(t *testing.T) {
	// With temp above 50, a cool zone on an adjacent lane should pull the
	// auto player toward it. Use a deterministic seed to avoid hesitation.
	g := newAutoGame()
	g.rng = rand.New(rand.NewSource(99))
	g.playerLane = 2
	g.distance = 0
	g.temp = 70
	g.obstacles = []obstacle{{x: 8, lane: 3, kind: kindCoolZone}}

	action := chooseAutoAction(g)

	if action != "down" {
		t.Errorf("hot auto player should bias toward cool-zone lane (down), got %q", action)
	}
}

func TestChooseAutoAction_IgnoresObstaclesBeyondView(t *testing.T) {
	g := newAutoGame()
	g.playerLane = 2
	g.distance = 0
	g.obstacles = []obstacle{{x: autoViewAhead + 10, lane: 2, kind: kindBlock}}

	action := chooseAutoAction(g)

	// No hazard in view → no dodge, no brake. Cool engine + clear lane → turbo.
	if action == "up" || action == "down" || action == "brake" {
		t.Errorf("auto should ignore far-off block, got %q", action)
	}
}

func TestChooseAutoAction_IgnoresObstaclesBehind(t *testing.T) {
	g := newAutoGame()
	g.playerLane = 2
	g.distance = 20
	// x < distance means the obstacle is behind the player.
	g.obstacles = []obstacle{{x: 5, lane: 2, kind: kindBlock}}

	action := chooseAutoAction(g)

	if action == "up" || action == "down" || action == "brake" {
		t.Errorf("auto should ignore obstacle behind player, got %q", action)
	}
}

func TestChooseAutoAction_StaysOnLaneWhenAdjacentMarginallySafer(t *testing.T) {
	// Adjacent lane is only 1 unit safer (less than autoLaneSwitchEdge=2.0)
	// so the hysteresis should keep the player on its current lane.
	g := newAutoGame()
	g.playerLane = 2
	g.distance = 0
	g.obstacles = []obstacle{
		{x: 20, lane: 2, kind: kindBlock},
		{x: 21, lane: 1, kind: kindBlock},
		{x: 21, lane: 3, kind: kindBlock},
	}

	action := chooseAutoAction(g)

	if action == "up" || action == "down" {
		t.Errorf("auto should hold lane when adjacent only marginally safer, got %q", action)
	}
}

// ----- scanLanes (state inspection) -----

func TestScanLanes_RecordsClosestHazardPerLane(t *testing.T) {
	g := newAutoGame()
	g.playerLane = 2
	g.distance = 0
	g.obstacles = []obstacle{
		{x: 5, lane: 2, kind: kindBlock},  // closest
		{x: 10, lane: 2, kind: kindBlock}, // farther one — should not override
	}

	info := g.scanLanes()

	if info[2].hazard != 5 {
		t.Errorf("expected hazard=5 on lane 2, got %v", info[2].hazard)
	}
	if info[0].hazard <= float64(autoViewAhead) {
		t.Errorf("lane 0 should report no hazard, got %v", info[0].hazard)
	}
}

func TestScanLanes_RecordsBonusKind(t *testing.T) {
	g := newAutoGame()
	g.playerLane = 2
	g.distance = 0
	g.obstacles = []obstacle{
		{x: 8, lane: 1, kind: kindRamp},
		{x: 12, lane: 3, kind: kindCoolZone},
	}

	info := g.scanLanes()

	if !info[1].hasBonus || info[1].bonusKind != kindRamp || info[1].bonus != 8 {
		t.Errorf("lane 1 ramp not recorded correctly: %+v", info[1])
	}
	if !info[3].hasBonus || info[3].bonusKind != kindCoolZone || info[3].bonus != 12 {
		t.Errorf("lane 3 cool zone not recorded correctly: %+v", info[3])
	}
}

func TestScanLanes_IgnoresOutOfView(t *testing.T) {
	g := newAutoGame()
	g.playerLane = 2
	g.distance = 50
	g.obstacles = []obstacle{
		{x: 20, lane: 2, kind: kindBlock},                     // behind
		{x: 50 + autoViewAhead + 1, lane: 2, kind: kindBlock}, // beyond view
	}

	info := g.scanLanes()

	if info[2].hazard <= float64(autoViewAhead) {
		t.Errorf("scanLanes should ignore out-of-view obstacles on lane 2, got %v", info[2].hazard)
	}
}

func TestScanLanes_TracksRivalAsHazard(t *testing.T) {
	g := newAutoGame()
	g.playerLane = 2
	g.distance = 0
	g.rivals = []rival{{xf: 7, lane: 2, speed: 0}}

	info := g.scanLanes()

	if info[2].hazard != 7 {
		t.Errorf("expected rival to register as hazard at distance 7, got %v", info[2].hazard)
	}
}
