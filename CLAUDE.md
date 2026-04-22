# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Testing

@~/.claude/TESTING.md

## Commands

Releases: `scripts/release-tag.sh vX.Y.Z` requires a clean tree with `HEAD == origin/main` before it will create and push the annotated tag that triggers `.github/workflows/release.yml`.

## Architecture

**The `game` struct is the single source of truth.** All state — player position, speed, temperature, obstacles, rivals, timers, best times, phase flags — lives on it. `update` runs physics, movement, collisions, and phase transitions in one method, early-returning for each non-gameplay phase.

**Coordinate model:** the player is rendered at a fixed screen column `playerCol`; the world scrolls. `g.distance` is the player's world-X. `cameraX()` returns `int(g.distance)` during play but freezes at `trackLength` during the finishing run-off so the player sprite visibly runs off the right edge before `finished` is set.

**Game phases** are encoded as flags rather than an enum, and `update`/`handleKey` branch on them in this order:
1. `!started` — opening screen; Enter/Space transitions to started + 3s countdown.
2. `finishing` — post-finish-line run-off; camera frozen, player and rivals keep moving at their last speed until the player leaves the screen, then `finished = true`.
3. `countdown > 0` — input ignored except quit.
4. `crashed` — input ignored; `crashTimer` counts down, then state is reset to idle.
5. Normal play.

**Collision detection** iterates the integer x-positions the player crossed this tick (`prev+1..curr`) to avoid tunneling at high speed. Jumping (set by hitting a ramp) makes the player invulnerable to ground obstacles and rivals until `playerY` returns to 0 — all obstacle cases check `!g.jumping` before applying effects.

**Turbo / temperature coupling:** hitting `tempMax` auto-disables turbo, resets `speed` to `idleSpeed`, and starts `overheatTimer` (purely for the OVERHEAT! label). Mud applies the same speed reset without crashing; cool-zone obstacles instantly zero `temp`. Mud and overheat intentionally share the soft-reset behavior.

**Best times retry:** on retry, `handleKey`'s finishing branch copies `bestTimes` off the old game, replaces `*g` with a fresh `newGame`, then restores them — otherwise best times would be lost.
