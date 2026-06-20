// Package agentcore is the AgentCore substrate adapter: an Actuator that
// launches A2A sessions and an Observer that reports their readiness/lifecycle.
//
// "The launch is easy; the Observer is the design" (invariant 7). Each substrate
// adapter is defined by its readiness/lifecycle signal, not its launch call. An
// AgentCore session is its own microVM with active-CPU + peak-mem metering and
// 8 GB / 8 h caps — the second rung of the transport ladder (architecture §7).
//
// Status: stub. The second transport (A2A-to-new-session, with budget/deadline/
// cancel and settlement on the wire) lands in M4. Deploying the host itself to
// AgentCore is the gated final step of M0.
package agentcore
