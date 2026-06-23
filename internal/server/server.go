// Package server implements the devmem serve local web dashboard. It exposes a
// small JSON API over the artifacts generated under .devmem/ and serves a
// self-contained single-page UI that renders them (module cards, the master
// architecture Mermaid diagram, and the per-commit changelog timeline).
//
// The server is read-only and fully offline: it never calls the Anthropic API
// and reads .devmem/ fresh on every request, so changes made by devmem capture
// show up on a browser refresh.
package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yourusername/devmem/internal/docs"
	"github.com/yourusername/devmem/internal/state"
)

//go:embed assets/dashboard.html
var assets embed.FS

// Server serves the DevMem dashboard for a single repository root.
type Server struct {
	RepoRoot string
}

// New creates a Server rooted at repoRoot.
func New(repoRoot string) *Server {
	return &Server{RepoRoot: repoRoot}
}

// Handler returns the http.Handler exposing the dashboard and its JSON API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/overview", s.handleOverview)
	mux.HandleFunc("/api/modules", s.handleModules)
	mux.HandleFunc("/api/mermaid", s.handleMermaid)
	mux.HandleFunc("/api/changelog", s.handleChangelog)
	return mux
}

// devmemDir returns the absolute path to the repository's .devmem directory.
func (s *Server) devmemDir() string {
	return filepath.Join(s.RepoRoot, ".devmem")
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	page, err := assets.ReadFile("assets/dashboard.html")
	if err != nil {
		http.Error(w, "dashboard asset unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(page)
}

// Overview is the payload for /api/overview.
type Overview struct {
	ProjectName   string   `json:"project_name"`
	Overview      string   `json:"overview"`
	DataFlow      string   `json:"data_flow"`
	TechStack     []string `json:"tech_stack"`
	ModuleCount   int      `json:"module_count"`
	LastCommit    string   `json:"last_commit"`
	LastCapture   string   `json:"last_capture"`
	InitialisedAt string   `json:"initialised_at"`
	Initialised   bool     `json:"initialised"`
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	out := Overview{
		ProjectName: filepath.Base(s.RepoRoot),
		TechStack:   []string{},
	}

	if st, err := state.LoadState(s.RepoRoot); err == nil {
		out.LastCommit = st.LastCommit
		out.LastCapture = st.LastCapture
		out.InitialisedAt = st.InitialisedAt
		out.ModuleCount = st.ModuleCount
	}

	if cfg, err := state.LoadConfig(s.RepoRoot); err == nil && len(cfg.Modules) > out.ModuleCount {
		out.ModuleCount = len(cfg.Modules)
	}

	masterPath := filepath.Join(s.devmemDir(), "docs", "master-architecture.md")
	if content, err := os.ReadFile(masterPath); err == nil {
		out.Initialised = true
		sections := splitMarkdownSections(string(content))
		out.Overview = sections["Overview"]
		out.DataFlow = sections["Data flow"]
		out.TechStack = bulletItems(sections["Tech stack"])
	}

	writeJSON(w, out)
}

// Module is one entry in /api/modules.
type Module struct {
	Name      string   `json:"name"`
	RootPath  string   `json:"root_path"`
	DependsOn []string `json:"depends_on"`
	KeyFiles  []string `json:"key_files"`
	ChangedIn []string `json:"changed_in"`
	Body      string   `json:"body_markdown"`
}

func (s *Server) handleModules(w http.ResponseWriter, r *http.Request) {
	out := []Module{}
	dir := filepath.Join(s.devmemDir(), "docs", "modules")
	entries, err := os.ReadDir(dir)
	if err != nil {
		writeJSON(w, out)
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		content, readErr := os.ReadFile(filepath.Join(dir, e.Name()))
		if readErr != nil {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		m := Module{
			Name:      name,
			DependsOn: []string{},
			KeyFiles:  []string{},
			ChangedIn: []string{},
			Body:      string(content),
		}
		if fm, body, parseErr := docs.ParseFrontmatter(string(content)); parseErr == nil {
			m.Body = strings.TrimLeft(body, "\n")
			if v := frontmatterString(fm, "module"); v != "" {
				m.Name = v
			}
			m.RootPath = frontmatterString(fm, "root_path")
			m.DependsOn = stringSlice(fm["depends_on"])
			m.KeyFiles = stringSlice(fm["key_files"])
			m.ChangedIn = stringSlice(fm["changed_in"])
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, out)
}

// Mermaid is the payload for /api/mermaid.
type Mermaid struct {
	Graph string `json:"graph"`
}

func (s *Server) handleMermaid(w http.ResponseWriter, r *http.Request) {
	path := filepath.Join(s.devmemDir(), "docs", "master-architecture.mermaid")
	content, err := os.ReadFile(path)
	if err != nil {
		writeJSON(w, Mermaid{Graph: ""})
		return
	}
	writeJSON(w, Mermaid{Graph: strings.TrimRight(string(content), "\n")})
}

// ChangelogEntry is one entry in /api/changelog.
type ChangelogEntry struct {
	ID       string   `json:"id"`
	Date     string   `json:"date"`
	Type     string   `json:"type"`
	Breaking bool     `json:"breaking"`
	Modules  []string `json:"modules"`
	Body     string   `json:"body_markdown"`
}

func (s *Server) handleChangelog(w http.ResponseWriter, r *http.Request) {
	out := []ChangelogEntry{}
	dir := filepath.Join(s.devmemDir(), "changelog")
	entries, err := os.ReadDir(dir)
	if err != nil {
		writeJSON(w, out)
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		content, readErr := os.ReadFile(filepath.Join(dir, e.Name()))
		if readErr != nil {
			continue
		}
		entry := ChangelogEntry{
			ID:      strings.TrimSuffix(e.Name(), ".md"),
			Modules: []string{},
			Body:    string(content),
		}
		if fm, body, parseErr := docs.ParseFrontmatter(string(content)); parseErr == nil {
			entry.Body = strings.TrimLeft(body, "\n")
			if v := frontmatterString(fm, "id"); v != "" {
				entry.ID = v
			}
			entry.Date = frontmatterString(fm, "date")
			entry.Type = frontmatterString(fm, "type")
			entry.Breaking = frontmatterBool(fm, "breaking")
			entry.Modules = stringSlice(fm["modules"])
		}
		out = append(out, entry)
	}
	// Newest first: dates are RFC3339 and sort lexicographically; fall back to id.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date > out[j].Date
		}
		return out[i].ID > out[j].ID
	})
	writeJSON(w, out)
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		http.Error(w, fmt.Sprintf("encode response: %v", err), http.StatusInternalServerError)
	}
}

// splitMarkdownSections returns a map of "## Heading" -> trimmed section body for
// the master architecture document.
func splitMarkdownSections(content string) map[string]string {
	sections := map[string]string{}
	lines := strings.Split(content, "\n")
	current := ""
	var buf []string
	flush := func() {
		if current != "" {
			sections[current] = strings.TrimSpace(strings.Join(buf, "\n"))
		}
		buf = buf[:0]
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			flush()
			current = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			continue
		}
		buf = append(buf, line)
	}
	flush()
	return sections
}

// bulletItems extracts "- item" lines from a markdown section body.
func bulletItems(section string) []string {
	out := []string{}
	for _, line := range strings.Split(section, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") {
			item := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			if item != "" {
				out = append(out, item)
			}
		}
	}
	return out
}

func frontmatterString(fm map[string]interface{}, key string) string {
	v, ok := fm[key]
	if !ok || v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return strings.TrimSpace(val)
	case []string, []interface{}:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", val))
	}
}

func frontmatterBool(fm map[string]interface{}, key string) bool {
	switch val := fm[key].(type) {
	case bool:
		return val
	case string:
		return strings.EqualFold(strings.TrimSpace(val), "true")
	default:
		return false
	}
}

// stringSlice normalises a frontmatter value into a string slice.
func stringSlice(v interface{}) []string {
	switch value := v.(type) {
	case nil:
		return []string{}
	case []string:
		return append([]string(nil), value...)
	case []interface{}:
		out := make([]string, 0, len(value))
		for _, item := range value {
			out = append(out, fmt.Sprintf("%v", item))
		}
		return out
	default:
		s := strings.TrimSpace(fmt.Sprintf("%v", value))
		if s == "" || s == "[]" {
			return []string{}
		}
		return []string{s}
	}
}
