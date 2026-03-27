package compose

import "testing"

func TestParseCompose_BasicServices(t *testing.T) {
	input := []byte("services:\n  web:\n    image: node:20.11.1\n    ports:\n      - \"3000:3000\"\n  db:\n    image: postgres:16.2\n")
	refs, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs, got %d", len(refs))
	}
	if refs[0].ServiceName != "web" {
		t.Errorf("[0] ServiceName = %q", refs[0].ServiceName)
	}
	if refs[0].ImageRef != "node:20.11.1" {
		t.Errorf("[0] ImageRef = %q", refs[0].ImageRef)
	}
	if refs[1].ServiceName != "db" {
		t.Errorf("[1] ServiceName = %q", refs[1].ServiceName)
	}
	if refs[1].ImageRef != "postgres:16.2" {
		t.Errorf("[1] ImageRef = %q", refs[1].ImageRef)
	}
}

func TestParseCompose_SkipBuild(t *testing.T) {
	input := []byte("services:\n  app:\n    build: .\n    image: myapp:latest\n  web:\n    image: node:20\n")
	refs, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs, got %d", len(refs))
	}
	if !refs[0].Skip {
		t.Error("[0] service with build should be skipped")
	}
	if refs[0].SkipReason != "has build directive" {
		t.Errorf("[0] SkipReason = %q", refs[0].SkipReason)
	}
	if refs[1].Skip {
		t.Error("[1] service without build should not be skipped")
	}
}

func TestParseCompose_AlreadyPinned(t *testing.T) {
	input := []byte("services:\n  web:\n    image: node:20.11.1@sha256:abc123\n")
	refs, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}
	if refs[0].ImageRef != "node:20.11.1" {
		t.Errorf("ImageRef = %q", refs[0].ImageRef)
	}
	if refs[0].Digest != "sha256:abc123" {
		t.Errorf("Digest = %q", refs[0].Digest)
	}
}

func TestParseCompose_NoImageKey(t *testing.T) {
	input := []byte("services:\n  builder:\n    build:\n      context: .\n      dockerfile: Dockerfile\n")
	refs, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("expected 0 refs, got %d", len(refs))
	}
}
