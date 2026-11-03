package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlanManifestOrderIndependentFileDirConflict(t *testing.T) {
	cases := [][]EntryInput{
		{{Type: "file", Path: "A"}, {Type: "dir", Path: "a"}},
		{{Type: "dir", Path: "a"}, {Type: "file", Path: "A"}},
		{{Type: "file", Path: "Folder"}, {Type: "file", Path: "folder/x"}},
		{{Type: "file", Path: "folder/x"}, {Type: "file", Path: "Folder"}},
		{{Type: "file", Path: "a."}, {Type: "dir", Path: "a"}},
		{{Type: "dir", Path: "a"}, {Type: "file", Path: "a."}},
	}
	for i, entries := range cases {
		paths, reason, err := PlanManifest(entries, "windows")
		if err != nil {
			t.Fatalf("case %d: unexpected error: %v", i, err)
		}
		if reason != "file_dir_conflict" {
			t.Errorf("case %d: expected file_dir_conflict, got reason=%q paths=%v", i, reason, paths)
		}
	}
}

func TestPlanManifestReservesDirectoryNamespace(t *testing.T) {
	entries := []EntryInput{
		{Type: "file", Path: "a"},
		{Type: "file", Path: "A"},
		{Type: "dir", Path: "A (1)"},
	}
	paths, reason, err := PlanManifest(entries, "windows")
	if err != nil || reason != "" {
		t.Fatalf("unexpected rejection: reason=%q err=%v", reason, err)
	}
	want := []string{"a", "A (2)", "A (1)"}
	for i := range want {
		if paths[i] != want[i] {
			t.Errorf("path %d = %q, want %q", i, paths[i], want[i])
		}
	}
}

func TestSafeJoinRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"../x", "a/../../x", "/etc/passwd", "C:/x", "a\\b", "", "./x"} {
		if _, err := SafeJoin(root, rel); err == nil {
			t.Errorf("SafeJoin accepted unsafe path %q", rel)
		}
	}
	got, err := SafeJoin(root, "docs/readme.txt")
	if err != nil {
		t.Fatalf("SafeJoin rejected safe path: %v", err)
	}
	if !IsWithin(root, got) {
		t.Fatalf("SafeJoin returned unconfined path %q for root %q", got, root)
	}
}

func TestSafeJoinRejectsSymlinkedDirectory(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink creation unavailable on this platform: %v", err)
	}
	if _, err := SafeJoin(root, "link/escape.txt"); err == nil {
		t.Fatal("SafeJoin accepted a path through a symlinked directory")
	}
}

func TestCommitNoReplaceNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(dst, []byte("existing"), 0644); err != nil {
		t.Fatal(err)
	}

	src1 := filepath.Join(dir, "file.txt.lvpart")
	if err := os.WriteFile(src1, []byte("first"), 0644); err != nil {
		t.Fatal(err)
	}
	committed1, err := CommitNoReplace(src1, dst)
	if err != nil {
		t.Fatalf("first commit failed: %v", err)
	}
	if committed1 == dst {
		t.Fatal("commit replaced an existing destination")
	}
	got, _ := os.ReadFile(committed1)
	if string(got) != "first" {
		t.Fatalf("committed content = %q", got)
	}
	orig, _ := os.ReadFile(dst)
	if string(orig) != "existing" {
		t.Fatalf("original destination was modified: %q", orig)
	}
	if _, err := os.Stat(src1); !os.IsNotExist(err) {
		t.Fatalf("source part file still exists after commit: %v", err)
	}

	src2 := filepath.Join(dir, "file.txt.lvpart")
	if err := os.WriteFile(src2, []byte("second"), 0644); err != nil {
		t.Fatal(err)
	}
	committed2, err := CommitNoReplace(src2, dst)
	if err != nil {
		t.Fatalf("second commit failed: %v", err)
	}
	if committed2 == committed1 {
		t.Fatalf("second commit reused %q", committed1)
	}
	got2, _ := os.ReadFile(committed2)
	if string(got2) != "second" {
		t.Fatalf("second committed content = %q", got2)
	}
}
