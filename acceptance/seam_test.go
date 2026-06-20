// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package acceptance_test

import (
	"context"
	"go/build"
	"strings"
	"testing"

	"github.com/scttfrdmn/telos/acceptance"
)

// TestInertNode_RendersNoVerdict confirms the M0 acceptance node is inert: it
// never accepts and never asserts a basis other than not-adjudicated.
func TestInertNode_RendersNoVerdict(t *testing.T) {
	node := acceptance.NewInertNode("judge")
	a, ok := node.(acceptance.Acceptance)
	if !ok {
		t.Fatal("inert node must implement Acceptance")
	}
	v, err := a.Render(context.Background(), acceptance.Record{NodeID: "producer", Content: "X is true"}, "concordant")
	if err != nil {
		t.Fatal(err)
	}
	if v.Accepted {
		t.Fatal("inert node must not accept anything in M0")
	}
	if v.Basis != acceptance.NotAdjudicated {
		t.Fatalf("inert node basis = %q, want not-adjudicated", v.Basis)
	}
}

// TestSeam_AcceptanceImportsNoProducer is the keystone-seam guard (invariant 10,
// issue #9). The acceptance package must not import any production package — if
// it did, acceptance could come to depend on (or reach into) the code that
// produces the results it judges. Acceptance grades direction-neutral facts; it
// produces nothing. This test fails if a forbidden import is added.
func TestSeam_AcceptanceImportsNoProducer(t *testing.T) {
	// Packages that produce or model production graphs. Acceptance judges a
	// Record (a plain value); it must not reach for any of these.
	forbidden := []string{
		"github.com/scttfrdmn/telos/host",
		"github.com/scttfrdmn/telos/acs",
		"github.com/scttfrdmn/telos/planner",
		"github.com/scttfrdmn/telos/binder",
		"github.com/scttfrdmn/telos/gateway",
		"github.com/scttfrdmn/telos/router",
		"github.com/scttfrdmn/telos/domain/research",
	}
	pkg, err := build.Import("github.com/scttfrdmn/telos/acceptance", "", 0)
	if err != nil {
		t.Fatalf("import acceptance: %v", err)
	}
	imports := append([]string{}, pkg.Imports...)
	imports = append(imports, pkg.TestImports...)
	for _, imp := range imports {
		for _, bad := range forbidden {
			if imp == bad || strings.HasPrefix(imp, bad+"/") {
				t.Errorf("acceptance must not import production package %q (invariant 10: separate envelope)", imp)
			}
		}
	}
}
