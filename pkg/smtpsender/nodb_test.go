package smtpsender

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// forbiddenImports are the packages that reaching the database goes through.
var forbiddenImports = []string{
	"database/sql",
	"github.com/jackc/pgx",
	"github.com/kannon-email/kannon/internal/db",
}

// forbiddenCalls are the Container accessors that hand out a database handle.
// The sender takes the same *container.Container as every other component, so
// its restraint is not enforced by what it is given, only by what it asks for.
var forbiddenCalls = []string{"DB", "Queries"}

// TestSenderNeverTalksToTheDatabase keeps ADR 0013 true.
//
// The sender is NATS in, SMTP out, NATS out, and that is a constraint rather
// than an accident: it is what lets a sender pod be deployed on its own, scaled
// with outbound volume instead of with database capacity, and go on delivering
// mail Kannon has already accepted while Postgres is unavailable. Every one of
// those properties is lost by the first query, and the first query is usually
// the cheapest way to write whatever feature asks for it — so the rule needs a
// test and not just a paragraph.
//
// It reads the package's own imports and the accessors it calls, which is the
// boundary where the rule is checkable. It is deliberately not an assertion
// about the transitive import graph: Kannon is one binary and the Container is
// shared, so pgx is linked into every process, sender-only ones included. A
// driver that is never called costs nothing; a call does.
func TestSenderNeverTalksToTheDatabase(t *testing.T) {
	for _, file := range packageFiles(t) {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("cannot parse %s: %v", file, err)
		}

		for _, spec := range f.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("cannot read import path in %s: %v", file, err)
			}
			for _, forbidden := range forbiddenImports {
				if path == forbidden || strings.HasPrefix(path, forbidden+"/") {
					t.Errorf("%s imports %s: the sender must not talk to the database (ADR 0013). "+
						"Put what the send needs on the Envelope, or in a JetStream key/value bucket.",
						filepath.Base(file), path)
				}
			}
		}

		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			for _, forbidden := range forbiddenCalls {
				if sel.Sel.Name == forbidden {
					t.Errorf("%s:%d calls .%s(): the sender must not talk to the database (ADR 0013)",
						filepath.Base(file), fset.Position(sel.Pos()).Line, forbidden)
				}
			}
			return true
		})
	}
}

// packageFiles returns the non-test sources of this package: the rule is about
// what a sender pod does, so a test helper reaching for a database would be
// noise rather than a violation.
func packageFiles(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("cannot read package directory: %v", err)
	}

	var files []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files = append(files, name)
	}
	if len(files) == 0 {
		t.Fatal("no package sources found: the guard would pass by reading nothing")
	}
	return files
}
