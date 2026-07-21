package sftp

import "testing"

func TestWithinRootAllowsFilesystemRoot(t *testing.T) {
	ok, err := withinRoot("/", "/")
	if err != nil || !ok {
		t.Fatalf("withinRoot(/,/)=%v err=%v", ok, err)
	}
	ok, err = withinRoot("/", "/home")
	if err != nil || !ok {
		t.Fatalf("withinRoot(/,/home)=%v err=%v", ok, err)
	}
	ok, _ = withinRoot("/home", "/")
	if ok {
		t.Fatal("expected / outside /home")
	}
}

func TestNormalizeRootSentinel(t *testing.T) {
	if got := normalizeRemotePath("root"); got != "/root" {
		t.Fatalf("normalizeRemotePath(root)=%q (CreateAdapter must map sentinel separately)", got)
	}
	if got := normalizeRemotePath("/"); got != "/" {
		t.Fatalf("normalizeRemotePath(/)=%q", got)
	}
}
