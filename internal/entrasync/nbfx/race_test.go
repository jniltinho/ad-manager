//go:build race

package nbfx

// raceAlloc: the race detector adds its own allocations to the memory bound tests.
const raceAlloc = 2
