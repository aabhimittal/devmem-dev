package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// populatedRepo creates a temp repo with a minimal .devmem tree.
func populatedRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dm := filepath.Join(root, ".devmem")

	writeFile(t, filepath.Join(dm, "state.json"), `{
  "initialised_at": "2026-06-01T00:00:00Z",
  "last_commit": "abcdef1234567890",
  "last_capture": "2026-06-02T00:00:00Z",
  "module_count": 1
}`)

	writeFile(t, filepath.Join(dm, "docs", "master-architecture.md"), `# Architecture - demo

Generated at: 2026-06-01T00:00:00Z

## Overview

A demo project.

## Data flow

CLI to docs.

## Tech stack

- Go
- Cobra

## Modules

| Name | Summary |
| --- | --- |
| foo | does foo |
`)

	writeFile(t, filepath.Join(dm, "docs", "master-architecture.mermaid"),
		"graph TD\n    foo[foo]\n    bar[bar]\n    foo --> bar\n")

	writeFile(t, filepath.Join(dm, "docs", "modules", "foo.md"), `---
module: foo
root_path: internal/foo
key_files:
  - internal/foo/foo.go
depends_on:
  - bar
changed_in:
  - abcdef1234567890
generated_at: 2026-06-01T00:00:00Z
---

# foo

The foo module.

## Purpose

Does foo things.
`)

	writeFile(t, filepath.Join(dm, "changelog", "abcdef1234567890.md"), `---
id: abcdef1234567890
date: 2026-06-02T00:00:00Z
type: feature
breaking: false
modules:
  - foo
---

Added the foo feature.
`)

	return root
}

func get(t *testing.T, h http.Handler, path string) (*httptest.ResponseRecorder, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec, rec.Body.Bytes()
}

func TestEndpointsPopulated(t *testing.T) {
	h := New(populatedRepo(t)).Handler()

	rec, body := get(t, h, "/api/overview")
	if rec.Code != http.StatusOK {
		t.Fatalf("overview status = %d", rec.Code)
	}
	var ov Overview
	if err := json.Unmarshal(body, &ov); err != nil {
		t.Fatalf("overview decode: %v", err)
	}
	if !ov.Initialised {
		t.Errorf("expected initialised=true")
	}
	if ov.Overview != "A demo project." {
		t.Errorf("overview text = %q", ov.Overview)
	}
	if ov.DataFlow != "CLI to docs." {
		t.Errorf("data flow = %q", ov.DataFlow)
	}
	if len(ov.TechStack) != 2 || ov.TechStack[0] != "Go" {
		t.Errorf("tech stack = %v", ov.TechStack)
	}
	if ov.LastCommit != "abcdef1234567890" {
		t.Errorf("last commit = %q", ov.LastCommit)
	}

	rec, body = get(t, h, "/api/modules")
	if rec.Code != http.StatusOK {
		t.Fatalf("modules status = %d", rec.Code)
	}
	var mods []Module
	if err := json.Unmarshal(body, &mods); err != nil {
		t.Fatalf("modules decode: %v", err)
	}
	if len(mods) != 1 {
		t.Fatalf("expected 1 module, got %d", len(mods))
	}
	if mods[0].Name != "foo" || mods[0].RootPath != "internal/foo" {
		t.Errorf("module = %+v", mods[0])
	}
	if len(mods[0].DependsOn) != 1 || mods[0].DependsOn[0] != "bar" {
		t.Errorf("depends_on = %v", mods[0].DependsOn)
	}

	rec, body = get(t, h, "/api/mermaid")
	if rec.Code != http.StatusOK {
		t.Fatalf("mermaid status = %d", rec.Code)
	}
	var mm Mermaid
	if err := json.Unmarshal(body, &mm); err != nil {
		t.Fatalf("mermaid decode: %v", err)
	}
	if mm.Graph == "" || mm.Graph[:7] != "graph T" {
		t.Errorf("mermaid graph = %q", mm.Graph)
	}

	rec, body = get(t, h, "/api/changelog")
	if rec.Code != http.StatusOK {
		t.Fatalf("changelog status = %d", rec.Code)
	}
	var entries []ChangelogEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		t.Fatalf("changelog decode: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 changelog entry, got %d", len(entries))
	}
	if entries[0].Type != "feature" || entries[0].Breaking {
		t.Errorf("entry = %+v", entries[0])
	}
	if len(entries[0].Modules) != 1 || entries[0].Modules[0] != "foo" {
		t.Errorf("entry modules = %v", entries[0].Modules)
	}

	rec, _ = get(t, h, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("index status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct[:9] != "text/html" {
		t.Errorf("index content-type = %q", ct)
	}
}

func TestEndpointsEmptyRepo(t *testing.T) {
	// A directory with no .devmem at all must not 500; arrays must be empty.
	h := New(t.TempDir()).Handler()

	for _, path := range []string{"/api/modules", "/api/changelog"} {
		rec, body := get(t, h, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d", path, rec.Code)
		}
		var arr []json.RawMessage
		if err := json.Unmarshal(body, &arr); err != nil {
			t.Fatalf("%s decode: %v", path, err)
		}
		if len(arr) != 0 {
			t.Errorf("%s expected empty array, got %d", path, len(arr))
		}
	}

	rec, body := get(t, h, "/api/overview")
	if rec.Code != http.StatusOK {
		t.Fatalf("overview status = %d", rec.Code)
	}
	var ov Overview
	if err := json.Unmarshal(body, &ov); err != nil {
		t.Fatalf("overview decode: %v", err)
	}
	if ov.Initialised {
		t.Errorf("expected initialised=false for empty repo")
	}

	rec, body = get(t, h, "/api/mermaid")
	if rec.Code != http.StatusOK {
		t.Fatalf("mermaid status = %d", rec.Code)
	}
	var mm Mermaid
	if err := json.Unmarshal(body, &mm); err != nil {
		t.Fatalf("mermaid decode: %v", err)
	}
	if mm.Graph != "" {
		t.Errorf("expected empty graph, got %q", mm.Graph)
	}
}
