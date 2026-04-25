package main

// Auto-play logic. Reads the current game state and emits at most one action
// per call via applyAction. Intentionally imperfect — designed to entertain in
// a tmux pane rather than to set records.

const (
	autoViewAhead       = 30  // world units the auto player looks ahead
	autoLaneSwitchEdge  = 2.0 // lane switch requires this much improvement (anti-jitter)
	autoBrakeDistance   = 5   // brake when a hazard is closer than this
	autoTurboTempLow    = 60  // engage turbo only when temp is below this
	autoTurboTempHigh   = 85  // disengage turbo when temp is above this
	autoMistakeChance   = 0.07
	autoCooldownMin     = 0.08 // min seconds between AI decisions
	autoCooldownJitter  = 0.12 // additional random delay (0..jitter)
	autoBonusViewWindow = 20   // how close a ramp/cool needs to be to attract
)

// autoStep advances the auto-player by dt seconds. It is a no-op when the
// game is not in a state where input would be accepted.
func (g *game) autoStep(dt float64) {
	if !g.autoMode || !g.started || g.finishing || g.finished || g.countdown > 0 || g.crashed {
		return
	}
	if g.autoCooldown > 0 {
		g.autoCooldown -= dt
		return
	}
	if action := chooseAutoAction(g); action != "" {
		g.applyAction(action)
	}
	g.autoCooldown = autoCooldownMin + g.rng.Float64()*autoCooldownJitter
}

type autoLaneInfo struct {
	hazard    float64 // distance to nearest hazard (block/mud/rival), large if none
	bonus     float64 // distance to nearest bonus (ramp/cool)
	bonusKind obstacleKind
	hasBonus  bool
}

// scanLanes returns per-lane info on the nearest hazard and bonus within
// autoViewAhead world units of the player's current X.
func (g *game) scanLanes() []autoLaneInfo {
	info := make([]autoLaneInfo, numLanes)
	for i := range info {
		info[i].hazard = float64(autoViewAhead + 1)
		info[i].bonus = float64(autoViewAhead + 1)
	}
	px := int(g.distance)
	for _, o := range g.obstacles {
		dx := o.x - px
		if dx <= 0 || dx > autoViewAhead {
			continue
		}
		switch o.kind {
		case kindBlock, kindMud:
			if float64(dx) < info[o.lane].hazard {
				info[o.lane].hazard = float64(dx)
			}
		case kindRamp, kindCoolZone:
			if float64(dx) < info[o.lane].bonus {
				info[o.lane].bonus = float64(dx)
				info[o.lane].bonusKind = o.kind
				info[o.lane].hasBonus = true
			}
		}
	}
	for _, rv := range g.rivals {
		dx := int(rv.xf) - px
		if dx <= 0 || dx > autoViewAhead {
			continue
		}
		if float64(dx) < info[rv.lane].hazard {
			info[rv.lane].hazard = float64(dx)
		}
	}
	return info
}

// laneScore rates a lane: bigger hazard distance = safer; nearby ramps and
// (when hot) cool zones add a small bonus to attract the auto player toward
// them.
func (g *game) laneScore(li autoLaneInfo) float64 {
	s := li.hazard
	if li.hasBonus && li.bonus < autoBonusViewWindow {
		switch li.bonusKind {
		case kindRamp:
			s += 5
		case kindCoolZone:
			if g.temp > 50 {
				s += 8
			}
		}
	}
	return s
}

// chooseAutoAction returns the next action the auto player would take, or
// the empty string if no action is appropriate. It reads g (and consumes
// randomness from g.rng for the hesitation roll) but otherwise does not
// mutate game state — apply the result via g.applyAction.
func chooseAutoAction(g *game) string {
	info := g.scanLanes()
	cur := g.playerLane

	bestLane := cur
	bestScore := g.laneScore(info[cur])
	for _, c := range []int{cur - 1, cur + 1} {
		if c < 0 || c >= numLanes {
			continue
		}
		s := g.laneScore(info[c])
		if s > bestScore+autoLaneSwitchEdge {
			bestScore = s
			bestLane = c
		}
	}

	// Lane changes are blocked while jumping; fall through to speed control.
	// Occasionally hesitate to keep the auto player from looking robotic.
	if !g.jumping && bestLane != cur && g.rng.Float64() >= autoMistakeChance {
		if bestLane < cur {
			return "up"
		}
		return "down"
	}

	curHazard := info[cur].hazard
	switch {
	case curHazard < float64(autoBrakeDistance) && !g.jumping:
		return "brake"
	case g.turboOn && g.temp > autoTurboTempHigh:
		return "turbo" // toggle off
	case !g.turboOn && g.temp < autoTurboTempLow && curHazard > 12:
		return "turbo" // toggle on
	default:
		return "accel"
	}
}
