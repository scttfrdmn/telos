// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package gateway

import "errors"

// ErrComputePathNotImplemented is returned by RunWork in M1. The synthesized-
// compute path (spore.host via MCP) lands in M6; the model path is M1.
var ErrComputePathNotImplemented = errors.New("gateway: compute path (RunWork) not implemented until M6")

// ErrNoBackend is returned when a binding names a provider the gateway has no
// backend for.
var ErrNoBackend = errors.New("gateway: no backend registered for binding provider")

// ErrReservationDenied wraps a governor refusal — the gateway fails closed when
// worst-case cost cannot be escrowed (conservation breach or exhausted grant).
var ErrReservationDenied = errors.New("gateway: governor denied reservation (fails closed)")
