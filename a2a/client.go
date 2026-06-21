// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/scttfrdmn/telos/acs"
	"github.com/scttfrdmn/telos/governor"
)

// Invoker sends one invocation to a remote A2A session, carrying the budget
// envelope and returning the settlement. It is the wire half; the host's
// substrate supplies the session URL. Kept as an interface so tests can fake the
// remote without a server.
type Invoker interface {
	Invoke(ctx context.Context, sessionURL string, env Request) (Response, error)
}

// Request is the A2A invocation envelope: the prompt/input plus the budget that
// bounds the remote child (§9).
type Request struct {
	Prompt string `json:"prompt,omitempty"`
	Input  string `json:"input,omitempty"`
	Budget Budget `json:"budget"`
}

// Response is the A2A invocation result envelope: the output plus the settlement
// the parent reconciles (§9).
type Response struct {
	Output     string     `json:"output"`
	Archetype  string     `json:"archetype,omitempty"`
	Accepted   bool       `json:"accepted"`
	Basis      string     `json:"basis,omitempty"`
	Settlement Settlement `json:"settlement"`
}

// CrossBoundary runs the full conservation-crossing protocol for placing a child
// on a remote session (§9):
//
//  1. Reserve the child's budget against the PARENT grant — FAILS CLOSED if the
//     parent can't fund it (Σ child ≤ parent holds across the boundary).
//  2. Invoke the remote session, sending the reservation as an explicit wire
//     budget and propagating cancel via the request context (the kill-switch
//     crosses the wire).
//  3. SETTLE the returned cost against the parent grant (surplus banks at the
//     parent only if the remote outcome was accepted — lexicographic, §9).
//
// On any failure after reserving, the child grant is released (no charge) so the
// parent reservoir is conserved.
func CrossBoundary(
	ctx context.Context,
	gov governor.Governor,
	parent governor.GrantID,
	req acs.BudgetRequest,
	sessionURL string,
	inv Invoker,
	prompt string,
) (Response, error) {
	// 1. Reserve against the parent — fails closed on conservation breach.
	grant, err := gov.Reserve(ctx, parent, req)
	if err != nil {
		return Response{}, fmt.Errorf("a2a: reserve child against parent (fails closed): %w", err)
	}
	gid := governor.GrantID(grant.GrantID)

	// 2. Invoke the remote session with the explicit budget envelope. The request
	//    context carries the deadline + cancel — cancelling ctx cancels the remote.
	env := Request{
		Prompt: prompt,
		Budget: Budget{
			GrantID:     string(gid),
			Amount:      grant.Budget.Amount,
			Period:      grant.Budget.Period,
			Currency:    grant.Budget.Denomination(),
			CancelToken: string(gid),
		},
	}
	if dl, ok := ctx.Deadline(); ok {
		env.Budget.DeadlineNs = dl.UnixNano()
	}

	resp, err := inv.Invoke(ctx, sessionURL, env)
	if err != nil {
		// The remote didn't produce billable, accepted work: release the escrow.
		_ = gov.Release(context.Background(), gid)
		return Response{}, fmt.Errorf("a2a: remote invoke: %w", err)
	}

	// 3. Settle the remote's actual cost against the parent grant. Surplus banks
	//    only if accepted (the governor enforces the lexicographic gate).
	outcome := governor.Outcome{
		Exit:     exitKind(resp.Settlement.Outcome),
		Accepted: resp.Settlement.Accepted,
	}
	if err := gov.Settle(ctx, gid, resp.Settlement.Cost(), outcome); err != nil {
		return Response{}, fmt.Errorf("a2a: settle remote cost to parent ledger: %w", err)
	}
	return resp, nil
}

// §15 fork #9 — RESOLVED PER-PATH (M5), not globally:
//
//   - HAPPY PATH (CrossBoundary, above): SYNCHRONOUS. The parent blocks, the
//     child returns, settlement is applied inline. This is M4's model, kept —
//     we do NOT pay async latency on every call for the sake of a rare replay.
//
//   - RECOVERY PATH (SettleRemoteEventually, below): EVENTUAL. A settlement that
//     arrives out-of-order, late, or after the parent is being rebuilt from the
//     WAL has nowhere to go in a pure-sync model — so it settles eventually
//     against the replayed parent. It converges on the SAME idempotent governor
//     apply (settle-by-GrantID), so it can never double-count.
//
// Conflating the two — making every call eventual — would tax the common path to
// handle the rare one. The split IS the resolution: happy=sync, recovery=eventual.

// SettleRemoteEventually is the RECOVERY-path settle (§9 / fork #9 eventual side).
// It applies a settlement that could not be settled synchronously — because the
// parent had crashed and was rebuilt from the WAL, or the settlement arrived late
// or out of order — to the child grant identified by gid, against a parent
// governor that may have just been replayed. It is idempotent (governor settle is
// a no-op on an already-closed grant), so a settlement that was in fact already
// applied before the crash leaves the ledger unchanged.
//
// The grant must exist in the (replayed) governor as an OPEN escrow — which it
// will, because CrossBoundary write-ahead-journals the reservation BEFORE
// invoking the remote, so the escrow survives a parent crash (#A1).
func SettleRemoteEventually(ctx context.Context, gov governor.Governor, gid governor.GrantID, s Settlement) error {
	outcome := governor.Outcome{Exit: exitKind(s.Outcome), Accepted: s.Accepted}
	if err := gov.Settle(ctx, gid, s.Cost(), outcome); err != nil {
		return fmt.Errorf("a2a: eventual settle of %q: %w", gid, err)
	}
	return nil
}

func exitKind(s string) governor.ExitKind {
	switch governor.ExitKind(s) {
	case governor.ExitDone, governor.ExitHandoff, governor.ExitNegative, governor.ExitExhausted:
		return governor.ExitKind(s)
	default:
		return governor.ExitDone
	}
}

// HTTPInvoker is the real wire Invoker: it POSTs the envelope to the session's
// /invocations contract endpoint. The request context propagates the deadline
// and cancel to the remote session (the kill-switch crossing the boundary).
type HTTPInvoker struct {
	Client *http.Client
}

// NewHTTPInvoker returns an HTTPInvoker with a sane default client.
func NewHTTPInvoker() *HTTPInvoker {
	return &HTTPInvoker{Client: &http.Client{Timeout: 120 * time.Second}}
}

// Invoke POSTs the envelope to sessionURL + "/invocations".
func (h *HTTPInvoker) Invoke(ctx context.Context, sessionURL string, env Request) (Response, error) {
	body, err := json.Marshal(env)
	if err != nil {
		return Response{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sessionURL+"/invocations", bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.Client.Do(req)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return Response{}, fmt.Errorf("a2a: remote session status %d: %s", resp.StatusCode, string(raw))
	}
	var out Response
	if err := json.Unmarshal(raw, &out); err != nil {
		return Response{}, fmt.Errorf("a2a: decode remote response: %w", err)
	}
	return out, nil
}

var _ Invoker = (*HTTPInvoker)(nil)
