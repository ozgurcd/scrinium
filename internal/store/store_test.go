package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreRejectsTraversalAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	st, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := st.Read(context.Background(), "../../etc/passwd"); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("expected traversal error, got %v", err)
	}

	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlink not available: %v", err)
	}
	if err := st.Write(context.Background(), "linked/escape.md", []byte("outside"), 0644); err == nil {
		t.Fatal("expected symlink-parent escape to be rejected")
	}
	if _, err := os.Stat(filepath.Join(outside, "escape.md")); !os.IsNotExist(err) {
		t.Fatalf("outside file must not be created, stat err=%v", err)
	}
}

func TestStoreWriteAtomicallyReplacesContent(t *testing.T) {
	root := t.TempDir()
	st, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := st.Write(ctx, "notes/test.md", []byte("first"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(ctx, "notes/test.md", []byte("second"), 0644); err != nil {
		t.Fatal(err)
	}
	data, err := st.Read(ctx, "notes/test.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "second" {
		t.Fatalf("unexpected content %q", data)
	}
	entries, err := os.ReadDir(filepath.Join(root, "notes"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".scrinium-") {
			t.Fatalf("temporary file leaked: %s", entry.Name())
		}
	}
}
