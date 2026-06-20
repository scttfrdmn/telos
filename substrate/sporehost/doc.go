// Package sporehost is the spore.host substrate adapter: an Actuator/Observer
// over owned hardware, and the compute backend reached via MCP.
//
// The third rung of the transport ladder (architecture §7): GPU-in-process, huge
// memory, local models, sovereign data, or heavy synthesized compute. The memory
// rectangle is sunk, not metered; lifecycle is spored. It is registered as an
// AgentCore Gateway target, with Cedar/LKI gating compute-spend authority.
//
// The compute suite to expose as tools (architecture §8): truffle (rungs),
// reckoner (frontier), spawn/queuezero/cohort (provision + run), spored
// (observe), teardown (lifecycle).
//
// Status: stub. Lands in M6 (ComputeSynthesis → WorkloadSpec → spore.host MCP).
package sporehost
