package compose

import (
	"strings"
	"testing"
)

func TestRewriteCompose(t *testing.T) {
	input := "services:\n  web:\n    image: node:20.11.1\n    ports:\n      - \"3000:3000\"\n  db:\n    image: postgres:16.2\n    environment:\n      POSTGRES_PASSWORD: secret\n  app:\n    build: .\n    image: myapp:latest\n"
	refs := []ComposeImageRef{
		{ServiceName: "web", ImageRef: "node:20.11.1", RawRef: "node:20.11.1", Line: 3},
		{ServiceName: "db", ImageRef: "postgres:16.2", RawRef: "postgres:16.2", Line: 7},
		{ServiceName: "app", ImageRef: "myapp:latest", RawRef: "myapp:latest", Line: 12, Skip: true, SkipReason: "has build directive"},
	}
	digests := map[int]string{
		0: "sha256:aaa111",
		1: "sha256:bbb222",
	}
	got := RewriteFile(input, refs, digests)
	if !strings.Contains(got, "image: node:20.11.1@sha256:aaa111") {
		t.Errorf("expected node pinned, got:\n%s", got)
	}
	if !strings.Contains(got, "image: postgres:16.2@sha256:bbb222") {
		t.Errorf("expected postgres pinned, got:\n%s", got)
	}
	if !strings.Contains(got, "image: myapp:latest") {
		t.Errorf("expected myapp NOT pinned, got:\n%s", got)
	}
}

func TestRewriteCompose_UpdateExisting(t *testing.T) {
	input := "services:\n  web:\n    image: node:20.11.1@sha256:olddigest\n"
	refs := []ComposeImageRef{
		{ServiceName: "web", ImageRef: "node:20.11.1", RawRef: "node:20.11.1@sha256:olddigest", Digest: "sha256:olddigest", Line: 3},
	}
	digests := map[int]string{0: "sha256:newdigest"}
	got := RewriteFile(input, refs, digests)
	if !strings.Contains(got, "image: node:20.11.1@sha256:newdigest") {
		t.Errorf("expected digest updated, got:\n%s", got)
	}
}
