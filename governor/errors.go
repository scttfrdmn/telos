// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package governor

import "errors"

// errConservation is the sentinel for a fails-closed conservation breach: a
// reservation that would push Σ(children) past the parent's remaining reservoir.
// Callers (e.g. the gateway, which fails closed) can match it with errors.Is.
var errConservation = errors.New("governor: conservation breach (Σ child > parent remaining)")

// ErrConservation exposes the conservation sentinel for callers that need to
// distinguish a fails-closed refusal from other errors.
var ErrConservation = errConservation
