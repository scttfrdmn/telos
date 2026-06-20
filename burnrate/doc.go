// Package burnrate is the landing controller: a reservoir-over-clock thermostat
// that modulates the DEFAULT standard of proof so the grant lands near-zero at
// the deadline — neither starving early nor dying rich.
//
// This is the time half of invariant 4 made active. A clinical-grade answer that
// is affordable in month 2 of a grant is declined in month 11. burnrate watches
// reservoir-over-clock and sets the default standard of proof accordingly; the
// user/archetype sets the baseline, burnrate modulates it.
//
// The interface (architecture §5):
//
//	DefaultStandard(Reservoir, Clock) StandardOfProof
//
// Status: stub. Lands in M2. The thermostat curve itself is an open fork
// (architecture §15 #2) — to be priced by the built system, not pre-spec'd.
package burnrate
