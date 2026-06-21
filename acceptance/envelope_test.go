// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package acceptance_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scttfrdmn/telos/acceptance"
)

// RUNTIME ENVELOPE SEPARATION (invariant 10, #C4). The verdict path must not be
// able to read the producer's grant/budget — it can only see the Record and the
// standard. This is enforced structurally: the Acceptance.Render signature takes
// only (ctx, Record, StandardOfProof). The same record judged under two very
// different (hypothetical) budget situations is identical, because budget is
// simply not an input.
func TestEnvelope_VerdictIndependentOfBudget(t *testing.T) {
	j := judge()
	rec := acceptance.Record{NodeID: "p", Direction: acceptance.DirectionPositive,
		Sources: []acceptance.Source{indepSource(true), indepSource(true)}}

	// There is no API by which the caller can hand the judge a budget. The best
	// we can do at runtime is confirm the verdict depends only on the record +
	// standard — render twice and require identity. (The stronger guarantee is
	// the absence of any budget parameter, asserted structurally below.)
	v1, _ := j.Render(context.Background(), rec, concordant)
	v2, _ := j.Render(context.Background(), rec, concordant)
	if v1 != v2 {
		t.Fatal("verdict must be a pure function of (record, standard)")
	}
}

// Structural guard: the Acceptance interface's Render method must take ONLY a
// Record and a StandardOfProof — never a grant, budget, reservoir, or governor.
// If someone widens the verdict path to accept the producer's budget context,
// this fails (invariant 10: the judge cannot be swayed by what's at stake).
func TestEnvelope_RenderSignatureExcludesBudget(t *testing.T) {
	// Locate acceptance.go relative to this test file.
	src := "acceptance.go"
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, src, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", src, err)
	}

	forbidden := []string{"Budget", "Grant", "Reservoir", "Governor", "Cost", "Reservation"}
	var checked bool
	ast.Inspect(f, func(n ast.Node) bool {
		it, ok := n.(*ast.InterfaceType)
		if !ok {
			return true
		}
		for _, m := range it.Methods.List {
			if len(m.Names) == 0 || m.Names[0].Name != "Render" {
				continue
			}
			checked = true
			ft := m.Type.(*ast.FuncType)
			var b strings.Builder
			ast.Fprint(&b, fset, ft.Params, nil)
			params := b.String()
			for _, bad := range forbidden {
				if strings.Contains(params, bad) {
					t.Fatalf("Render parameters must not reference %q (invariant 10: verdict path can't read producer budget); params=%s", bad, params)
				}
			}
		}
		return true
	})
	if !checked {
		t.Fatal("did not find the Acceptance.Render method to check")
	}
	_ = filepath.Separator
}
