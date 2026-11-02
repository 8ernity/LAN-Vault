package fsutil

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func findProjectRoot(t *testing.T) string {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "docs", "protocol-constants.json")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find project root from %s", wd)
		}
		dir = parent
	}
}

func TestPathCorpus(t *testing.T) {
	root := findProjectRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "testvectors", "security", "path-corpus.json"))
	if err != nil {
		t.Fatalf("read path-corpus.json failed: %v", err)
	}

	var corpus struct {
		Paths []struct {
			ID       string `json:"id"`
			Path     string `json:"path"`
			Validate string `json:"validate"` // "ok" or "reject"
			Reason   string `json:"reason"`
			Windows  string `json:"windows"`
			Posix    string `json:"posix"`
		} `json:"paths"`
		Manifests []map[string]any `json:"manifests"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatalf("unmarshal path-corpus.json failed: %v", err)
	}

	t.Run("SinglePaths", func(t *testing.T) {
		for _, tc := range corpus.Paths {
			t.Run(tc.ID, func(t *testing.T) {
				why := ValidatePath(tc.Path)
				if tc.Validate == "reject" {
					if why == "" {
						t.Errorf("[%s] expected path %q to reject with reason %q, but validate passed", tc.ID, tc.Path, tc.Reason)
					} else if tc.Reason != "" && why != tc.Reason {
						t.Errorf("[%s] reject reason mismatch for %q: got %q, want %q", tc.ID, tc.Path, why, tc.Reason)
					}
					return
				}

				if why != "" {
					t.Fatalf("[%s] validate unexpectedly rejected valid path %q with reason %q", tc.ID, tc.Path, why)
				}

				// Check Windows plan
				winPath, winReject, err := PlanSingle(tc.Path, "windows")
				if err != nil {
					t.Fatalf("[%s] windows PlanSingle err: %v", tc.ID, err)
				}
				if winReject != "" {
					t.Fatalf("[%s] windows PlanSingle unexpectedly rejected: %s", tc.ID, winReject)
				}
				if winPath != tc.Windows {
					t.Errorf("[%s] windows path mismatch:\ngot:  %q\nwant: %q", tc.ID, winPath, tc.Windows)
				}

				// Check Posix plan
				posixPath, posixReject, err := PlanSingle(tc.Path, "posix")
				if err != nil {
					t.Fatalf("[%s] posix PlanSingle err: %v", tc.ID, err)
				}
				if posixReject != "" {
					t.Fatalf("[%s] posix PlanSingle unexpectedly rejected: %s", tc.ID, posixReject)
				}
				if posixPath != tc.Posix {
					t.Errorf("[%s] posix path mismatch:\ngot:  %q\nwant: %q", tc.ID, posixPath, tc.Posix)
				}
			})
		}
	})

	t.Run("Manifests", func(t *testing.T) {
		for _, m := range corpus.Manifests {
			id := m["id"].(string)
			t.Run(id, func(t *testing.T) {
				rawEntries := m["entries"].([]any)
				entries := make([]EntryInput, len(rawEntries))
				for i, re := range rawEntries {
					rem := re.(map[string]any)
					entries[i] = EntryInput{
						Type: rem["type"].(string),
						Path: rem["path"].(string),
					}
				}

				profiles := []string{"windows", "posix"}
				for _, prof := range profiles {
					paths, reject, err := PlanManifest(entries, prof)
					if err != nil {
						t.Fatalf("[%s-%s] PlanManifest error: %v", id, prof, err)
					}

					var wantReject bool
					var wantReason string
					var wantPaths []string

					if expectedRaw, hasExpected := m["expected"]; hasExpected {
						expectedMap := expectedRaw.(map[string]any)
						profExpect := expectedMap[prof].(map[string]any)
						if profExpect["result"] == "reject" {
							wantReject = true
							wantReason = profExpect["reason"].(string)
						} else {
							rawP := profExpect["paths"].([]any)
							wantPaths = make([]string, len(rawP))
							for i, v := range rawP {
								wantPaths[i] = v.(string)
							}
						}
					} else {
						if m["result"] == "reject" {
							wantReject = true
							wantReason = m["reason"].(string)
						} else {
							rawP := m[prof].([]any)
							wantPaths = make([]string, len(rawP))
							for i, v := range rawP {
								wantPaths[i] = v.(string)
							}
						}
					}

					if wantReject {
						if reject == "" {
							t.Fatalf("[%s-%s] expected manifest to reject with %q, but succeeded with %v", id, prof, wantReason, paths)
						}
						if wantReason != "" && reject != wantReason {
							t.Errorf("[%s-%s] reject reason mismatch: got %q, want %q", id, prof, reject, wantReason)
						}
					} else {
						if reject != "" {
							t.Fatalf("[%s-%s] manifest unexpectedly rejected with %q", id, prof, reject)
						}
						if !reflect.DeepEqual(paths, wantPaths) {
							t.Errorf("[%s-%s] paths mismatch:\ngot:  %v\nwant: %v", id, prof, paths, wantPaths)
						}
					}
				}
			})
		}
	})
}
