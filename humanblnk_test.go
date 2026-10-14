package humanblnk_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"testing"
)

// TestReexportsEverything fails when package ledger gains an exported name
// that humanblnk.go doesn't re-export, so importing the module root never
// misses part of the API.
func TestReexportsEverything(t *testing.T) {
	ledgerNames := exported(t, "ledger")
	rootNames := exported(t, ".")
	renamed := map[string]string{"New": "NewService"} // root New takes a URL

	var missing []string
	for name := range ledgerNames {
		if r, ok := renamed[name]; ok {
			name = r
		}
		if !rootNames[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("humanblnk.go doesn't re-export: %s", strings.Join(missing, ", "))
	}
}

func exported(t *testing.T, dir string) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, p := range pkgs {
		for _, f := range p.Files {
			for _, d := range f.Decls {
				switch d := d.(type) {
				case *ast.FuncDecl:
					if d.Recv == nil && d.Name.IsExported() {
						names[d.Name.Name] = true
					}
				case *ast.GenDecl:
					for _, s := range d.Specs {
						switch s := s.(type) {
						case *ast.TypeSpec:
							if s.Name.IsExported() {
								names[s.Name.Name] = true
							}
						case *ast.ValueSpec:
							for _, n := range s.Names {
								if n.IsExported() {
									names[n.Name] = true
								}
							}
						}
					}
				}
			}
		}
	}
	return names
}
