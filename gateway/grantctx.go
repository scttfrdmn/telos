// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package gateway

import (
	"context"

	"github.com/scttfrdmn/telos/governor"
)

// Budget rides in context.Context in-process (architecture §5): a node runs
// under a context carrying the GrantID of the grant its work spends against.
// The gateway reads that parent grant when it escrows a call's worst-case cost.
//
// Across A2A (M4) the grant becomes explicit wire fields; in-process it is this
// context value, set by the host/binder when it invokes a node.

type grantCtxKey struct{}

// WithParentGrant returns a context carrying the parent grant a node's metered
// work should reserve against. The host sets this around a node's execution.
func WithParentGrant(ctx context.Context, id governor.GrantID) context.Context {
	return context.WithValue(ctx, grantCtxKey{}, id)
}

// ParentGrant reads the parent grant from context, defaulting to the run's root
// grant when unset (a node with no explicit grant spends against the envelope).
func ParentGrant(ctx context.Context) governor.GrantID {
	if id, ok := ctx.Value(grantCtxKey{}).(governor.GrantID); ok {
		return id
	}
	return governor.RootGrant
}
