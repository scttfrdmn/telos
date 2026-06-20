// Package placer maps a bound ACS node to a transport and a substrate.
//
// Transport is a placement decision, not a code change (invariant 6): the same
// agenkit object becomes a goroutine, an A2A session, or an instance depending
// on the node's Trust and Gravity — never its cost alone. The transport ladder
// (architecture §7): goroutine (default) → AgentCore session → spore.host
// instance. First trigger that fires wins; the justification bar rises per rung.
//
// The interface (architecture §5):
//
//	Place(ctx, *acs.Node) (acs.Placement, error)
//
// Status: stub. M0 places everything in-process (substrate/inproc). The second
// transport (A2A-to-new-session) and a real Place() land in M4.
package placer
