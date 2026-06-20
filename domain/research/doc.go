// Package research is the research domain pack — the ONLY research-aware
// component. It maps question archetypes to ACS shapes, each carrying a
// verification structure that sets cascade aggressiveness and budget shape.
//
// Archetypes (architecture §10):
//   - Evidence synthesis  — retrieve fan-out → parallel extraction → supervisor
//     consolidation. Large verification gap; cascades well.
//   - Mechanistic/causal  — hypothesis decomposition → adversarial for/against →
//     reconciliation that may return *contested*. Replan-capable.
//   - Comparative         — parallel per-arm → normalize → difference.
//   - Quantitative/repro  — ComputeSynthesis nodes → spore.host rungs.
//     Replan-capable.
//   - Exploratory/open    — weak verification; emit a scoping node first.
//
// Composite questions are normal (evidence-synthesis substrate → mechanistic
// head). Keeping research-awareness confined here keeps the core generic.
//
// Status: stub. The first archetype (evidence-synthesis) + composite detection
// land in M3; the rest are additive.
package research
