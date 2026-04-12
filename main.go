package main

import (
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/gdamore/tcell/v2"
)

const (
	numLanes    = 5
	laneHeight  = 2
	trackLength = 1000
	playerCol   = 12
	headerRows  = 1

	tempMax      = 100.0
	tempHeatRate = 25.0 // temperature rise while turbo is on (units/s)
	tempCoolRate = 8.0  // temperature drop while turbo is off (units/s)

	accelCap        = 40.0
	turboMultiplier = 1.5

	accelRate = 15.0 // acceleration rate (units/s²)
	decelRate = 10.0 // deceleration rate (units/s²)
	idleSpeed = 8.0  // natural speed with no input
	minSpeed  = 5.0  // minimum speed while braking
)

type obstacleKind int

const (
	kindBlock    obstacleKind = iota // crashes on contact
	kindRamp                         // jump ramp
	kindCoolZone                     // cool zone (instantly refills the turbo gauge)
	kindMud                          // mud (speed returns to the default)
)

// randObstacleKind draws an obstacle kind using weighted random selection.
// weights: block=1, mud=1, ramp=2, cool=1, none=1 (total 6)
// Derived from an even block/ramp/cool split (1/3 each) by halving block
// into mud, and halving cool.
func randObstacleKind(r *rand.Rand) (obstacleKind, bool) {
	switch r.Intn(6) {
	case 0:
		return kindBlock, true
	case 1:
		return kindMud, true
	case 2, 3:
		return kindRamp, true
	case 4:
		return kindCoolZone, true
	default:
		return 0, false
	}
}

type obstacle struct {
	x    int
	lane int
	kind obstacleKind
}

type rival struct {
	xf    float64
	lane  int
	speed float64
}

type game struct {
	w, h int
	rng  *rand.Rand

	playerLane int
	playerY    float64
	jumping    bool
	jumpVel    float64

	distance float64
	speed    float64
	temp     float64 // engine temperature: 0 = cool, tempMax = overheat
	accelOn  bool
	brakeOn  bool
	turboOn  bool

	obstacles []obstacle
	rivals    []rival

	elapsed time.Duration

	started    bool
	finished   bool
	crashed    bool
	crashTimer float64

	spaceCooldown float64 // remaining debounce time for turbo toggle (seconds)
	overheatTimer float64 // seconds remaining to show OVERHEAT! label after overheat

	finishing   bool    // true while running off-screen after crossing the finish line
	finishSpeed float64 // speed at the moment of crossing the finish line

	bestTimes    []float64 // top-3 finish times in ascending order (in-memory only)
	lastBestRank int       // rank of the most recent finish (1-3), 0 = not ranked
}

func newGame(w, h int, rng *rand.Rand) *game {
	g := &game{
		w:          w,
		h:          h,
		rng:        rng,
		playerLane: numLanes / 2,
		speed:      25,
		temp:       0,
	}
	g.generateCourse()
	return g
}

func (g *game) generateCourse() {
	r := g.rng
	x := 40
	for x < trackLength-20 {
		if k, ok := randObstacleKind(r); ok {
			g.obstacles = append(g.obstacles, obstacle{
				x:    x,
				lane: r.Intn(numLanes),
				kind: k,
			})
		}
		if r.Intn(3) == 0 {
			if k, ok := randObstacleKind(r); ok {
				g.obstacles = append(g.obstacles, obstacle{
					x:    x,
					lane: r.Intn(numLanes),
					kind: k,
				})
			}
		}
		// Probabilistically place a rival
		if r.Intn(5) == 0 {
			g.rivals = append(g.rivals, rival{
				xf:    float64(x),
				lane:  r.Intn(numLanes),
				speed: 10 + float64(r.Intn(8)), // 10-17 units/sec
			})
		}
		x += r.Intn(7) + 5
	}
}

func (g *game) handleKey(ev *tcell.EventKey) bool {
	if !g.started {
		if ev.Key() == tcell.KeyEscape || ev.Rune() == 'q' {
			return true
		}
		if ev.Key() == tcell.KeyEnter || ev.Rune() == ' ' {
			g.started = true
		}
		return false
	}
	if g.finishing {
		if ev.Key() == tcell.KeyEnter || ev.Rune() == ' ' {
			best := g.bestTimes
			*g = *newGame(g.w, g.h, g.rng)
			g.bestTimes = best
			g.started = true
			return false
		}
		if ev.Key() == tcell.KeyEscape || ev.Rune() == 'q' {
			return true
		}
		return false
	}
	switch ev.Key() {
	case tcell.KeyEscape:
		return true
	case tcell.KeyUp:
		// Cannot change lanes while jumping
		if g.playerLane > 0 && !g.crashed && !g.jumping {
			g.playerLane--
		}
	case tcell.KeyDown:
		if g.playerLane < numLanes-1 && !g.crashed && !g.jumping {
			g.playerLane++
		}
	case tcell.KeyRight:
		if !g.crashed {
			g.accelOn = true
			g.brakeOn = false
		}
	case tcell.KeyLeft:
		if !g.crashed {
			g.brakeOn = true
			g.accelOn = false
		}
	}
	switch ev.Rune() {
	case 'q':
		return true
	case ' ':
		if !g.crashed && g.spaceCooldown <= 0 {
			// Debounce to prevent fast toggling from key repeat
			g.spaceCooldown = 0.15
			if g.turboOn {
				g.turboOn = false
			} else if g.temp < tempMax {
				g.turboOn = true
			}
		}
	}
	return false
}

func (g *game) update(dt float64) {
	if !g.started || g.finished {
		return
	}

	// Handle finishing run-off: player and rivals keep moving at their finish speeds
	if g.finishing {
		g.distance += g.finishSpeed * dt
		for i := range g.rivals {
			g.rivals[i].xf += g.rivals[i].speed * dt
		}
		// Player has run off the right edge of the screen
		playerScreenX := playerCol + int(g.distance) - trackLength
		if playerScreenX >= g.w {
			g.finished = true
		}
		return
	}

	// Elapsed time keeps advancing during a crash
	g.elapsed += time.Duration(dt * float64(time.Second))

	if g.spaceCooldown > 0 {
		g.spaceCooldown -= dt
	}
	if g.overheatTimer > 0 {
		g.overheatTimer -= dt
	}

	if g.crashed {
		g.crashTimer -= dt
		if g.crashTimer <= 0 {
			g.crashed = false
			g.accelOn = false
			g.brakeOn = false
			g.turboOn = false
			g.speed = idleSpeed
		}
		return
	}

	// Update engine temperature
	if g.turboOn {
		g.temp += tempHeatRate * dt
		if g.temp >= tempMax {
			// Overheat: auto-disable turbo and reset speed to default (same as mud)
			g.temp = tempMax
			g.turboOn = false
			g.speed = idleSpeed
			g.accelOn = false
			g.brakeOn = false
			g.overheatTimer = 1.0
		}
	} else {
		g.temp -= tempCoolRate * dt
		if g.temp < 0 {
			g.temp = 0
		}
	}

	// Determine target speed and ease toward it
	targetSpeed := idleSpeed
	switch {
	case g.turboOn:
		targetSpeed = accelCap * turboMultiplier // 60
	case g.accelOn:
		targetSpeed = accelCap // 40
	case g.brakeOn:
		targetSpeed = minSpeed
	}

	// While jumping, hold current speed unless accel/brake/turbo is active
	if g.jumping && !g.brakeOn && !g.accelOn && !g.turboOn {
		targetSpeed = g.speed
	}

	if g.speed < targetSpeed {
		rate := accelRate
		if g.turboOn {
			rate *= 3 // 3x acceleration while turbo is on
		}
		g.speed += rate * dt
		if g.speed > targetSpeed {
			g.speed = targetSpeed
		}
	} else if g.speed > targetSpeed {
		g.speed -= decelRate * dt
		if g.speed < targetSpeed {
			g.speed = targetSpeed
		}
	}

	// Move rivals (slower than the player)
	for i := range g.rivals {
		g.rivals[i].xf += g.rivals[i].speed * dt
	}

	// Distance
	prev := int(g.distance)
	g.distance += g.speed * dt
	curr := int(g.distance)

	if g.distance >= trackLength {
		g.distance = trackLength
		g.finishing = true
		g.finishSpeed = g.speed
		g.recordTime(g.elapsed.Seconds())
		return
	}

	// Jump physics
	if g.jumping {
		g.playerY += g.jumpVel * dt
		g.jumpVel -= 35 * dt
		if g.playerY <= 0 {
			g.playerY = 0
			g.jumping = false
			g.jumpVel = 0
		}
	}

	// Collision detection: obstacles
	for i := prev + 1; i <= curr; i++ {
		for _, o := range g.obstacles {
			if o.x != i || o.lane != g.playerLane {
				continue
			}
			switch o.kind {
			case kindBlock:
				if !g.jumping {
					g.crashed = true
					g.crashTimer = 1.2
					g.speed = 0
				}
			case kindRamp:
				if !g.jumping {
					g.jumping = true
					g.jumpVel = 18
				}
			case kindCoolZone:
				// Cool zone: instantly cool the engine temperature (only on ground)
				if !g.jumping {
					g.temp = 0
				}
			case kindMud:
				// Mud: speed resets to default (no crash).
				// Can be jumped over while airborne.
				if !g.jumping {
					g.speed = idleSpeed
					g.accelOn = false
					g.brakeOn = false
					g.turboOn = false
				}
			}
		}
	}

	// Collision detection: rivals (collided rivals disappear)
	for i := len(g.rivals) - 1; i >= 0; i-- {
		rv := g.rivals[i]
		rx := int(rv.xf)
		if rv.lane == g.playerLane && rx > prev && rx <= curr {
			if !g.jumping {
				g.crashed = true
				g.crashTimer = 1.0
				g.speed = 0
				g.rivals = append(g.rivals[:i], g.rivals[i+1:]...)
			}
		}
	}
}

func (g *game) recordTime(t float64) {
	g.bestTimes = append(g.bestTimes, t)
	// Sort ascending
	// Use <= so a new tied time bubbles ahead of existing equal entries,
	// ensuring the new record is kept (and ranked) over the older one.
	for i := len(g.bestTimes) - 1; i > 0 && g.bestTimes[i] <= g.bestTimes[i-1]; i-- {
		g.bestTimes[i], g.bestTimes[i-1] = g.bestTimes[i-1], g.bestTimes[i]
	}
	if len(g.bestTimes) > 3 {
		g.bestTimes = g.bestTimes[:3]
	}
	// Find rank of this time (1-indexed), 0 if not in top-3
	g.lastBestRank = 0
	for i, v := range g.bestTimes {
		if v == t {
			g.lastBestRank = i + 1
			break
		}
	}
}

// cameraX returns the camera scroll position. During the finishing run-off
// the camera freezes at the finish line so characters visibly run off-screen.
func (g *game) cameraX() int {
	if g.finishing || g.finished {
		return trackLength
	}
	return int(g.distance)
}

// ----- Rendering -----

func (g *game) draw(s tcell.Screen) {
	s.Clear()
	w, h := s.Size()
	g.w, g.h = w, h

	if !g.started {
		g.drawOpening(s)
		return
	}

	g.drawHeader(s)
	g.drawTrack(s)
	g.drawObstacles(s)
	g.drawRivals(s)
	g.drawPlayer(s)
	g.drawFooter(s)

	if g.finishing {
		g.drawFinish(s)
	}
}

func (g *game) drawOpening(s tcell.Screen) {
	bg := tcell.StyleDefault.Background(tcell.ColorBlack)
	for y := 0; y < g.h; y++ {
		for x := 0; x < g.w; x++ {
			s.SetContent(x, y, ' ', nil, bg)
		}
	}
	// Title logo: defines a 5-row bitmap per letter and concatenates them.
	// '#' is drawn as a blank cell with a background color, avoiding
	// environment-dependent wide-character issues.
	font := map[rune][]string{
		'E': {"#####", "#    ", "#### ", "#    ", "#####"},
		'I': {"###", " # ", " # ", " # ", "###"},
		'T': {"#####", "  #  ", "  #  ", "  #  ", "  #  "},
		'B': {"#### ", "#   #", "#### ", "#   #", "#### "},
		'K': {"#   #", "#  # ", "###  ", "#  # ", "#   #"},
		'R': {"#### ", "#   #", "#### ", "#  # ", "#   #"},
		'M': {"#   #", "## ##", "# # #", "#   #", "#   #"},
		'N': {"#   #", "##  #", "# # #", "#  ##", "#   #"},
		'A': {" ### ", "#   #", "#####", "#   #", "#   #"},
		'L': {"#    ", "#    ", "#    ", "#    ", "#####"},
		' ': {"  ", "  ", "  ", "  ", "  "},
	}
	const glyphRows = 5
	word := "TERMINALBIKE"
	title := make([]string, glyphRows)
	for idx, ch := range word {
		glyph := font[ch]
		for r := 0; r < glyphRows; r++ {
			if idx > 0 {
				title[r] += " "
			}
			title[r] += glyph[r]
		}
	}
	blockStyle := tcell.StyleDefault.Background(tcell.ColorYellow)
	startY := g.h/2 - glyphRows/2 - 3
	for i, line := range title {
		x := (g.w - len(line)) / 2
		for j, ch := range line {
			if ch == '#' {
				s.SetContent(x+j, startY+i, ' ', nil, blockStyle)
			}
		}
	}

	subY := startY + len(title) + 2
	prompt := "Press  [Enter]  or  [Space]  to Start"
	drawString(s, (g.w-len(prompt))/2, subY, bg.Foreground(tcell.ColorWhite).Bold(true), prompt)

	quitMsg := "q / Esc : Quit"
	drawString(s, (g.w-len(quitMsg))/2, subY+1, bg.Foreground(tcell.ColorGray), quitMsg)

	controls := []string{
		"↑ ↓ : Lane change    → : Accel    ← : Brake    Space : Turbo",
		"#=Block   /=Ramp   »=CoolZone   ~=Mud   @=Rival",
	}
	cstyle := bg.Foreground(tcell.ColorAqua)
	for i, line := range controls {
		drawString(s, (g.w-len(line))/2, subY+3+i, cstyle, line)
	}
}

func (g *game) drawHeader(s tcell.Screen) {
	style := tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(tcell.ColorYellow).Bold(true)
	title := "  TERMINALBIKE  ( ↑↓: lane   →: accel   ←: brake   space: turbo   q: quit )"
	// Fill the entire header row with a black background
	for x := 0; x < g.w; x++ {
		s.SetContent(x, 0, ' ', nil, style)
	}
	drawString(s, 0, 0, style, title)
}

func (g *game) drawTrack(s tcell.Screen) {
	top := headerRows + 1
	trackH := numLanes * laneHeight
	bottom := top + trackH

	bg := tcell.StyleDefault.Background(tcell.ColorBlack)

	// Fill the entire track area (including fences) with a black background
	for y := top - 1; y <= bottom; y++ {
		for x := 0; x < g.w; x++ {
			s.SetContent(x, y, ' ', nil, bg)
		}
	}

	// Top and bottom fences
	cam := g.cameraX()
	for x := 0; x < g.w; x++ {
		ch := '='
		if (x+cam)%4 < 2 {
			ch = '-'
		}
		s.SetContent(x, top-1, ch, nil, bg.Foreground(tcell.ColorGreen))
		s.SetContent(x, bottom, ch, nil, bg.Foreground(tcell.ColorGreen))
	}

	// Lane separators (dashed lines, scrolling effect)
	for lane := 1; lane < numLanes; lane++ {
		y := top + lane*laneHeight - 1
		for x := 0; x < g.w; x++ {
			if (x+cam)%6 < 3 {
				s.SetContent(x, y, '·', nil, bg.Foreground(tcell.ColorGray))
			}
		}
	}

	// Start / finish lines
	startScreenX := playerCol - cam
	if startScreenX >= 0 && startScreenX < g.w {
		for lane := 0; lane < numLanes; lane++ {
			y := top + lane*laneHeight
			s.SetContent(startScreenX, y, '|', nil, bg.Foreground(tcell.ColorWhite).Bold(true))
		}
	}
	finishScreenX := trackLength + playerCol - cam
	if finishScreenX >= 0 && finishScreenX < g.w {
		for lane := 0; lane < numLanes; lane++ {
			y := top + lane*laneHeight
			s.SetContent(finishScreenX, y, '|', nil, bg.Foreground(tcell.ColorWhite).Bold(true))
			if finishScreenX+1 < g.w {
				s.SetContent(finishScreenX+1, y, '|', nil, bg.Foreground(tcell.ColorRed).Bold(true))
			}
		}
	}
}

func (g *game) drawObstacles(s tcell.Screen) {
	top := headerRows + 1
	cam := g.cameraX()
	for _, o := range g.obstacles {
		sx := o.x + playerCol - cam
		if sx < 0 || sx >= g.w {
			continue
		}
		y := top + o.lane*laneHeight
		var ch rune
		var style tcell.Style
		bg := tcell.StyleDefault.Background(tcell.ColorBlack)
		switch o.kind {
		case kindBlock:
			ch = '#'
			style = bg.Foreground(tcell.ColorRed).Bold(true)
		case kindRamp:
			ch = '/'
			style = bg.Foreground(tcell.ColorAqua).Bold(true)
		case kindCoolZone:
			ch = '»'
			style = bg.Foreground(tcell.ColorTeal).Bold(true)
		case kindMud:
			ch = '~'
			style = bg.Foreground(tcell.ColorOlive).Bold(true)
		}
		s.SetContent(sx, y, ch, nil, style)
	}
}

func (g *game) drawRivals(s tcell.Screen) {
	top := headerRows + 1
	cam := g.cameraX()
	for _, rv := range g.rivals {
		sx := int(rv.xf) + playerCol - cam
		if sx < 0 || sx >= g.w {
			continue
		}
		y := top + rv.lane*laneHeight
		s.SetContent(sx, y, '@', nil, tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(tcell.ColorFuchsia).Bold(true))
	}
}

func (g *game) drawPlayer(s tcell.Screen) {
	top := headerRows + 1
	y := top + g.playerLane*laneHeight
	if g.jumping {
		y -= 1
	}
	bg := tcell.StyleDefault.Background(tcell.ColorBlack)
	style := bg.Foreground(tcell.ColorLime).Bold(true)
	ch := rune('>')
	if g.crashed {
		style = bg.Foreground(tcell.ColorRed).Bold(true)
		ch = '*'
	} else if g.jumping {
		style = bg.Foreground(tcell.ColorAqua).Bold(true)
		ch = '^'
	} else if g.turboOn {
		style = bg.Foreground(tcell.ColorYellow).Bold(true)
	}
	x := playerCol
	if g.finishing {
		x = playerCol + int(g.distance) - trackLength
	}
	if x >= g.w {
		return
	}
	if x-1 >= 0 {
		s.SetContent(x-1, y, '=', nil, style)
	}
	s.SetContent(x, y, ch, nil, style)
}

func (g *game) drawFooter(s tcell.Screen) {
	baseY := headerRows + 1 + numLanes*laneHeight + 1
	if baseY >= g.h {
		return
	}

	// Fill the footer area with a black background
	bgStyle := tcell.StyleDefault.Background(tcell.ColorBlack)
	for y := baseY; y < g.h && y <= baseY+2; y++ {
		for x := 0; x < g.w; x++ {
			s.SetContent(x, y, ' ', nil, bgStyle)
		}
	}

	st := bgStyle.Foreground(tcell.ColorWhite)
	modeStr := "Idle "
	switch {
	case g.turboOn:
		modeStr = "Turbo"
	case g.accelOn:
		modeStr = "Accel"
	case g.brakeOn:
		modeStr = "Brake"
	}
	displayDist := int(g.distance)
	if g.finishing {
		displayDist = trackLength
	}
	info := fmt.Sprintf("  Time: %5.1fs   Speed: %3.0f   Mode: %s   Dist: %3d/%d   TEMP ",
		g.elapsed.Seconds(), g.speed, modeStr, displayDist, trackLength)
	drawString(s, 0, baseY, st, info)

	// TEMP gauge: red background, filled with green from the left based on cooling.
	// temp=0 -> all green, temp=tempMax -> all red (overheat)
	barX := len(info)
	barW := 20
	green := tempMax - g.temp // remaining cooling capacity
	for i := 0; i < barW; i++ {
		cellStart := float64(i) / float64(barW) * tempMax
		var bg tcell.Color
		if cellStart < green {
			bg = tcell.ColorGreen
		} else {
			bg = tcell.ColorRed
		}
		if barX+i < g.w {
			s.SetContent(barX+i, baseY, ' ', nil, tcell.StyleDefault.Background(bg))
		}
	}

	if g.temp >= tempMax || g.overheatTimer > 0 {
		label := " OVERHEAT!"
		lx := barX + barW + 1
		drawString(s, lx, baseY, bgStyle.Foreground(tcell.ColorRed).Bold(true), label)
	}

	legendY := baseY + 1
	if legendY < g.h {
		lst := bgStyle.Foreground(tcell.ColorGray)
		legend := "  #=Block  /=Ramp  »=CoolZone  ~=Mud  @=Rival"
		drawString(s, 0, legendY, lst, legend)
	}
	if g.crashed && legendY+1 < g.h {
		drawString(s, 0, legendY+1, bgStyle.Foreground(tcell.ColorRed).Bold(true), "  CRASH!! Recovering...")
	}
}

func (g *game) drawFinish(s tcell.Screen) {
	medals := []string{"  1st", "  2nd", "  3rd"}
	type row struct {
		text      string
		highlight bool // true = this game's ranked time
	}
	rows := []row{
		{fmt.Sprintf("  FINISH!  Time: %.2fs  ", g.elapsed.Seconds()), false},
		{"", false},
		{"  Best Times:", false},
	}
	for i, t := range g.bestTimes {
		rows = append(rows, row{
			text:      fmt.Sprintf("  %s  %.2fs", medals[i], t),
			highlight: g.lastBestRank == i+1,
		})
	}
	rows = append(rows, row{"", false}, row{"  Enter : Retry    q : Quit  ", false})

	// Determine modal dimensions
	innerW := 0
	for _, r := range rows {
		if len(r.text) > innerW {
			innerW = len(r.text)
		}
	}
	innerH := len(rows)
	boxW := innerW + 2 // left/right border
	boxH := innerH + 2 // top/bottom border

	trackTop := headerRows + 1
	trackBottom := trackTop + numLanes*laneHeight
	trackCenterY := (trackTop + trackBottom) / 2

	left := (g.w - boxW) / 2
	top := trackCenterY - boxH/2

	bgModal := tcell.StyleDefault.Background(tcell.ColorNavy)
	bgHighlight := tcell.StyleDefault.Background(tcell.ColorPurple)
	border := bgModal.Foreground(tcell.ColorYellow).Bold(true)

	// Top border
	s.SetContent(left, top, '╔', nil, border)
	for x := left + 1; x < left+boxW-1; x++ {
		s.SetContent(x, top, '═', nil, border)
	}
	s.SetContent(left+boxW-1, top, '╗', nil, border)

	// Side borders + content rows
	for i, r := range rows {
		y := top + 1 + i
		s.SetContent(left, y, '║', nil, border)
		// Fill inner area
		rowBg := bgModal
		if r.highlight {
			rowBg = bgHighlight
		}
		for x := left + 1; x < left+boxW-1; x++ {
			s.SetContent(x, y, ' ', nil, rowBg)
		}
		s.SetContent(left+boxW-1, y, '║', nil, border)
		// Draw line text
		var st tcell.Style
		switch {
		case i == 0:
			st = bgModal.Foreground(tcell.ColorYellow).Bold(true)
		case r.highlight:
			st = bgHighlight.Foreground(tcell.ColorYellow).Bold(true)
		default:
			st = rowBg.Foreground(tcell.ColorWhite)
		}
		drawString(s, left+1, y, st, r.text)
	}

	// Bottom border
	y := top + boxH - 1
	s.SetContent(left, y, '╚', nil, border)
	for x := left + 1; x < left+boxW-1; x++ {
		s.SetContent(x, y, '═', nil, border)
	}
	s.SetContent(left+boxW-1, y, '╝', nil, border)
}

func drawString(s tcell.Screen, x, y int, style tcell.Style, str string) {
	for i, r := range str {
		s.SetContent(x+i, y, r, nil, style)
	}
}

// ----- Main loop -----

func main() {
	screen, err := tcell.NewScreen()
	if err != nil {
		fmt.Fprintln(os.Stderr, "screen error:", err)
		os.Exit(1)
	}
	if err := screen.Init(); err != nil {
		fmt.Fprintln(os.Stderr, "init error:", err)
		os.Exit(1)
	}
	defer screen.Fini()

	screen.SetStyle(tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(tcell.ColorWhite))
	screen.Clear()

	w, h := screen.Size()
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	g := newGame(w, h, rng)

	events := make(chan tcell.Event, 16)
	quit := make(chan struct{})
	go func() {
		for {
			ev := screen.PollEvent()
			if ev == nil {
				close(quit)
				return
			}
			events <- ev
		}
	}()

	ticker := time.NewTicker(33 * time.Millisecond) // ~30fps
	defer ticker.Stop()
	last := time.Now()

loop:
	for {
		select {
		case <-quit:
			break loop
		case ev := <-events:
			switch e := ev.(type) {
			case *tcell.EventKey:
				if g.handleKey(e) {
					break loop
				}
			case *tcell.EventResize:
				screen.Sync()
			}
		case now := <-ticker.C:
			dt := now.Sub(last).Seconds()
			last = now
			g.update(dt)
			g.draw(screen)
			screen.Show()
		}
	}
}
