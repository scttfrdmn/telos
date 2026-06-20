// Package gateway is the one work chokepoint (invariant 5).
//
// No agent gets raw model or compute access. Every metered unit — a model call
// or a synthesized computation — routes, escrows, meters, and settles here. It
// is the only place local models and off-platform compute can be metered.
//
// The interface (architecture §5):
//
//	Invoke(ctx, acs.ModelBinding, ModelRequest) (ModelResponse, Cost, error) // model call
//	RunWork(ctx, WorkloadSpec) (WorkResult, Cost, error)                     // synthesized compute
//
// Status: stub. The model path lands in M1 (Bedrock + one local endpoint); the
// compute path (RunWork → spore.host MCP) lands in M6.
package gateway
