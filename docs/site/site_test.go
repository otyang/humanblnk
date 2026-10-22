package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// The published pages must be what the chapters and examples build today:
// if an example changes, run go run ./docs/site and commit the result.
func TestSiteIsCurrent(t *testing.T) {
	out := t.TempDir()
	if err := build("..", out); err != nil {
		t.Fatal(err)
	}
	built, err := filepath.Glob(filepath.Join(out, "*.html"))
	if err != nil {
		t.Fatal(err)
	}
	if len(built) != len(chapters) {
		t.Fatalf("built %d pages, want %d", len(built), len(chapters))
	}
	for _, f := range append(built, filepath.Join(out, "assets", "style.css"), filepath.Join(out, "assets", "app.js")) {
		rel, _ := filepath.Rel(out, f)
		want, _ := os.ReadFile(f)
		got, err := os.ReadFile(filepath.Join("..", rel))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("docs/%s is out of date: run go run ./docs/site", rel)
		}
	}
}

// Every chapter's directives point at snippets that exist.
func TestEverySnippetResolves(t *testing.T) {
	for _, c := range chapters {
		src, err := os.ReadFile(filepath.Join("chapters", c.File))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := render("..", string(src)); err != nil {
			t.Errorf("%s: %v", c.File, err)
		}
	}
}
