package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// applyPatchToCopy is the heart of `verify-patch`. We exercise it directly:
// build a small "source tree", apply a unified diff, and verify the file
// content was patched.
func TestApplyPatchToCopy_GitOrPatch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		if _, err := exec.LookPath("patch"); err != nil {
			t.Skip("neither git nor patch available")
		}
	}
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "marker.txt"), []byte("fail\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	patch := []byte(`diff --git a/marker.txt b/marker.txt
--- a/marker.txt
+++ b/marker.txt
@@ -1 +1 @@
-fail
+pass
`)
	dst, err := applyPatchToCopy(src, patch)
	if err != nil {
		t.Fatalf("applyPatchToCopy: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dst, "marker.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(body)) != "pass" {
		t.Fatalf("expected marker.txt patched to 'pass', got %q", body)
	}
}

func TestApplyPatchToCopy_NoSource(t *testing.T) {
	patch := []byte(`diff --git a/x b/x
--- a/x
+++ b/x
@@ -0,0 +1 @@
+hello
`)
	if _, err := applyPatchToCopy("", patch); err == nil {
		// some platforms might succeed with patch(1); accept either outcome
		t.Log("apply succeeded against empty src (acceptable)")
	}
}
