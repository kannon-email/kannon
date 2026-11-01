package archtest

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const module = "github.com/kannon-email/kannon/"

// databaseImports are the packages that reaching Postgres goes through.
var databaseImports = []string{
	"database/sql",
	"github.com/jackc/pgx",
	module + "internal/db",
}

// mayTalkToTheDatabase are the directories whose production code is allowed to import a database
// package for good: the sqlc adapters themselves, the schema and migrations, the composition root
// that builds the pool, and the fixture that starts a Postgres for tests.
var mayTalkToTheDatabase = []string{
	"db",
	"internal/db",
	"internal/tests",
	"x/container",
}

// stillWiresItsOwnAdapters are the directories that still build sqlc adapters from a pool
// themselves, instead of being handed the Repositories they need. Each is debt, not permission:
// the list may only shrink, and TestTheDebtListOnlyShrinks fails once an entry is paid off and not
// removed.
var stillWiresItsOwnAdapters = []string{
	"pkg/api",
	"pkg/api/adminapi",
	"pkg/api/mailapi",
	"pkg/audit",
	"pkg/dispatcher",
	"pkg/stats",
	"pkg/validator",
}

// TestOnlyTheDatabaseSeamTalksToTheDatabase keeps every Postgres access behind a Repository: a
// domain package declares the interface and its specification, internal/db holds the sqlc adapter,
// and everything else is handed an interface. That is what lets the tests above the seam run in
// memory, and confines Postgres to the one test suite that checks the adapters against the specs.
//
// Like TestSenderNeverTalksToTheDatabase (ADR 0013), it reads each package's own non-test imports,
// not the transitive graph: Kannon is one binary, so the driver is linked everywhere, and what
// matters is who calls it.
func TestOnlyTheDatabaseSeamTalksToTheDatabase(t *testing.T) {
	for dir, imports := range offenders(t) {
		if slices.Contains(mayTalkToTheDatabase, dir) || slices.Contains(stillWiresItsOwnAdapters, dir) {
			continue
		}
		t.Errorf("%s imports %s. Declare a Repository in the domain package, put the sqlc adapter "+
			"in internal/db with its specification, and take the interface as a dependency.",
			dir, strings.Join(imports, ", "))
	}
}

func TestTheDebtListOnlyShrinks(t *testing.T) {
	found := offenders(t)
	for _, dir := range stillWiresItsOwnAdapters {
		if _, ok := found[dir]; !ok {
			t.Errorf("%s no longer imports a database package: remove it from stillWiresItsOwnAdapters "+
				"so it cannot start again unnoticed", dir)
		}
	}
}

// offenders maps each directory, relative to the module root, to the database packages its
// non-test sources import.
func offenders(t *testing.T) map[string][]string {
	t.Helper()
	root := moduleRoot(t)

	found := map[string][]string{}
	scanned := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); path != root && (strings.HasPrefix(name, ".") || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		scanned++
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		for _, spec := range f.Imports {
			imp, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			for _, forbidden := range databaseImports {
				if (imp == forbidden || strings.HasPrefix(imp, forbidden+"/")) && !slices.Contains(found[rel], imp) {
					found[rel] = append(found[rel], imp)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("cannot scan the module: %v", err)
	}
	if scanned == 0 {
		t.Fatal("no sources found: the guard would pass by reading nothing")
	}
	return found
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test's directory")
		}
		dir = parent
	}
}
