# DockerPin Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a CLI tool that pins Dockerfile FROM lines to `@sha256:<digest>` and validates pinned digests.

**Architecture:** BuildKit parser extracts FROM instructions and ARG defaults from Dockerfiles. Crane library resolves digests from registries via HEAD requests. Cobra provides the CLI with `pin` and `check` subcommands.

**Tech Stack:** Go, github.com/google/go-containerregistry, github.com/moby/buildkit, github.com/spf13/cobra, github.com/bmatcuk/doublestar/v4

---

## File Structure

```
main.go                              # Entry point, calls cmd.Execute()
cmd/
  root.go                            # Root cobra command + shared flags
  pin.go                             # Pin subcommand
  check.go                           # Check subcommand
  files.go                           # File discovery (--file / --glob)
internal/
  dockerfile/
    parse.go                         # Parse Dockerfile → []FromInstruction
    parse_test.go
    rewrite.go                       # Rewrite FROM lines with digests
    rewrite_test.go
  resolver/
    resolver.go                      # DigestResolver interface + CraneResolver
    resolver_test.go
testdata/
  basic.Dockerfile
  multi_stage.Dockerfile
  with_args.Dockerfile
  already_pinned.Dockerfile
  mixed.Dockerfile
```

---

### Task 1: Project Scaffolding

**Files:**
- Create: `go.mod`
- Create: `main.go`
- Create: `cmd/root.go`

- [ ] **Step 1: Initialize Go module**

Run:
```bash
go mod init github.com/azu/dockerfile-pin
```

- [ ] **Step 2: Create main.go**

```go
// main.go
package main

import (
	"os"

	"github.com/azu/dockerfile-pin/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
```

- [ ] **Step 3: Create cmd/root.go**

```go
// cmd/root.go
package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "dockerfile-pin",
	Short: "Pin Dockerfile images to digests",
	Long:  "A CLI tool that adds @sha256:<digest> to FROM lines in Dockerfiles to prevent supply chain attacks.",
}

func Execute() error {
	return rootCmd.Execute()
}
```

- [ ] **Step 4: Install dependencies**

Run:
```bash
go get github.com/spf13/cobra
go get github.com/google/go-containerregistry
go get github.com/moby/buildkit
go get github.com/bmatcuk/doublestar/v4
```

- [ ] **Step 5: Verify build**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum main.go cmd/root.go
git commit -m "feat: scaffold Go project with cobra root command"
```

---

### Task 2: Dockerfile Parser — FROM Instruction Extraction

**Files:**
- Create: `internal/dockerfile/parse.go`
- Create: `internal/dockerfile/parse_test.go`
- Create: `testdata/basic.Dockerfile`
- Create: `testdata/multi_stage.Dockerfile`

- [ ] **Step 1: Create test data files**

```dockerfile
# testdata/basic.Dockerfile
FROM node:20.11.1
FROM python:3.12-slim
FROM golang:1.22
```

```dockerfile
# testdata/multi_stage.Dockerfile
FROM golang:1.22 AS builder
FROM --platform=linux/amd64 debian:bookworm-slim AS runtime
COPY --from=builder /app /app
FROM scratch
FROM builder AS final
```

- [ ] **Step 2: Write failing tests for Parse()**

```go
// internal/dockerfile/parse_test.go
package dockerfile

import (
	"strings"
	"testing"
)

func TestParse_BasicFromLines(t *testing.T) {
	input := `FROM node:20.11.1
FROM python:3.12-slim
FROM golang:1.22
`
	instructions, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(instructions) != 3 {
		t.Fatalf("expected 3 instructions, got %d", len(instructions))
	}

	tests := []struct {
		idx      int
		imageRef string
		digest   string
		skip     bool
	}{
		{0, "node:20.11.1", "", false},
		{1, "python:3.12-slim", "", false},
		{2, "golang:1.22", "", false},
	}
	for _, tt := range tests {
		inst := instructions[tt.idx]
		if inst.ImageRef != tt.imageRef {
			t.Errorf("[%d] ImageRef = %q, want %q", tt.idx, inst.ImageRef, tt.imageRef)
		}
		if inst.Digest != tt.digest {
			t.Errorf("[%d] Digest = %q, want %q", tt.idx, inst.Digest, tt.digest)
		}
		if inst.Skip != tt.skip {
			t.Errorf("[%d] Skip = %v, want %v", tt.idx, inst.Skip, tt.skip)
		}
	}
}

func TestParse_MultiStage(t *testing.T) {
	input := `FROM golang:1.22 AS builder
FROM --platform=linux/amd64 debian:bookworm-slim AS runtime
FROM scratch
FROM builder AS final
`
	instructions, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(instructions) != 4 {
		t.Fatalf("expected 4 instructions, got %d", len(instructions))
	}

	// golang:1.22 AS builder
	if instructions[0].ImageRef != "golang:1.22" {
		t.Errorf("[0] ImageRef = %q, want %q", instructions[0].ImageRef, "golang:1.22")
	}
	if instructions[0].StageName != "builder" {
		t.Errorf("[0] StageName = %q, want %q", instructions[0].StageName, "builder")
	}
	if instructions[0].Skip {
		t.Error("[0] should not be skipped")
	}

	// --platform=linux/amd64 debian:bookworm-slim AS runtime
	if instructions[1].ImageRef != "debian:bookworm-slim" {
		t.Errorf("[1] ImageRef = %q, want %q", instructions[1].ImageRef, "debian:bookworm-slim")
	}
	if instructions[1].Platform != "linux/amd64" {
		t.Errorf("[1] Platform = %q, want %q", instructions[1].Platform, "linux/amd64")
	}
	if instructions[1].StageName != "runtime" {
		t.Errorf("[1] StageName = %q, want %q", instructions[1].StageName, "runtime")
	}

	// scratch — should be skipped
	if !instructions[2].Skip {
		t.Error("[2] scratch should be skipped")
	}
	if instructions[2].SkipReason != "scratch image" {
		t.Errorf("[2] SkipReason = %q, want %q", instructions[2].SkipReason, "scratch image")
	}

	// builder AS final — stage reference, should be skipped
	if !instructions[3].Skip {
		t.Error("[3] stage reference should be skipped")
	}
	if instructions[3].SkipReason != "stage reference" {
		t.Errorf("[3] SkipReason = %q, want %q", instructions[3].SkipReason, "stage reference")
	}
}

func TestParse_AlreadyPinned(t *testing.T) {
	input := `FROM node:20.11.1@sha256:d938c1761e3afbae9242848ffbb95b9cc1cb0a24d889f8bd955204d347a7266e
`
	instructions, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(instructions) != 1 {
		t.Fatalf("expected 1 instruction, got %d", len(instructions))
	}
	if instructions[0].ImageRef != "node:20.11.1" {
		t.Errorf("ImageRef = %q, want %q", instructions[0].ImageRef, "node:20.11.1")
	}
	if instructions[0].Digest != "sha256:d938c1761e3afbae9242848ffbb95b9cc1cb0a24d889f8bd955204d347a7266e" {
		t.Errorf("Digest = %q", instructions[0].Digest)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/dockerfile/ -v -run TestParse`
Expected: compilation error — `Parse` not defined

- [ ] **Step 4: Implement Parse()**

```go
// internal/dockerfile/parse.go
package dockerfile

import (
	"io"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

// FromInstruction represents a parsed FROM line in a Dockerfile.
type FromInstruction struct {
	// ImageRef is the image reference without digest, after ARG expansion.
	// e.g., "node:20.11.1"
	ImageRef string

	// RawRef is the image reference as written in the Dockerfile.
	// May contain ${VARIABLE} references. Includes digest if present.
	// e.g., "node:${NODE_VERSION}" or "node:20.11.1@sha256:abc..."
	RawRef string

	// Digest is the existing digest if present, empty otherwise.
	// e.g., "sha256:abc..."
	Digest string

	// Platform is the --platform flag value if specified.
	Platform string

	// StageName is the AS clause name if specified.
	StageName string

	// StartLine is the 1-based line number in the Dockerfile.
	StartLine int

	// Original is the full original FROM line text.
	Original string

	// Skip indicates this instruction should be skipped for pinning.
	Skip bool

	// SkipReason explains why this instruction is skipped.
	SkipReason string
}

// Parse reads a Dockerfile and returns all FROM instructions.
func Parse(r io.Reader) ([]FromInstruction, error) {
	result, err := parser.Parse(r)
	if err != nil {
		return nil, err
	}

	argDefaults := make(map[string]string)
	stageNames := make(map[string]bool)
	beforeFirstFrom := true

	var instructions []FromInstruction

	for _, node := range result.AST.Children {
		switch strings.ToLower(node.Value) {
		case "arg":
			if beforeFirstFrom && node.Next != nil {
				parseArgNode(node, argDefaults)
			}
		case "from":
			beforeFirstFrom = false
			inst := parseFromNode(node, argDefaults, stageNames)
			instructions = append(instructions, inst)
			if inst.StageName != "" {
				stageNames[strings.ToLower(inst.StageName)] = true
			}
		}
	}

	return instructions, nil
}

func parseArgNode(node *parser.Node, defaults map[string]string) {
	if node.Next == nil {
		return
	}
	arg := node.Next.Value
	if eqIdx := strings.Index(arg, "="); eqIdx >= 0 {
		key := arg[:eqIdx]
		value := arg[eqIdx+1:]
		defaults[key] = value
	}
}

func parseFromNode(node *parser.Node, argDefaults map[string]string, stageNames map[string]bool) FromInstruction {
	inst := FromInstruction{
		StartLine: node.StartLine,
		Original:  node.Original,
	}

	// Parse --platform flag
	for _, flag := range node.Flags {
		if strings.HasPrefix(flag, "--platform=") {
			inst.Platform = strings.TrimPrefix(flag, "--platform=")
		}
	}

	if node.Next == nil {
		inst.Skip = true
		inst.SkipReason = "malformed FROM"
		return inst
	}

	rawRef := node.Next.Value
	inst.RawRef = rawRef

	// Parse AS clause
	for n := node.Next.Next; n != nil; n = n.Next {
		if strings.EqualFold(n.Value, "as") && n.Next != nil {
			inst.StageName = n.Next.Value
			break
		}
	}

	// Check scratch
	if strings.EqualFold(rawRef, "scratch") {
		inst.ImageRef = rawRef
		inst.Skip = true
		inst.SkipReason = "scratch image"
		return inst
	}

	// Expand ARG variables
	expanded, hasUnresolved := expandVars(rawRef, argDefaults)

	// Check stage reference (after expansion)
	if stageNames[strings.ToLower(expanded)] {
		inst.ImageRef = expanded
		inst.Skip = true
		inst.SkipReason = "stage reference"
		return inst
	}

	if hasUnresolved {
		inst.ImageRef = rawRef
		inst.Skip = true
		inst.SkipReason = "unresolved ARG variable"
		return inst
	}

	// Split digest from image ref
	if atIdx := strings.Index(expanded, "@"); atIdx >= 0 {
		inst.ImageRef = expanded[:atIdx]
		inst.Digest = expanded[atIdx+1:]
	} else {
		inst.ImageRef = expanded
	}

	return inst
}

// expandVars replaces ${VAR} and $VAR with values from the defaults map.
// Returns the expanded string and true if any variable was unresolved.
func expandVars(s string, defaults map[string]string) (string, bool) {
	hasUnresolved := false
	result := &strings.Builder{}
	i := 0
	for i < len(s) {
		if s[i] == '$' {
			if i+1 < len(s) && s[i+1] == '{' {
				// ${VAR} syntax
				end := strings.Index(s[i:], "}")
				if end == -1 {
					result.WriteByte(s[i])
					i++
					continue
				}
				varName := s[i+2 : i+end]
				if val, ok := defaults[varName]; ok {
					result.WriteString(val)
				} else {
					result.WriteString(s[i : i+end+1])
					hasUnresolved = true
				}
				i += end + 1
			} else {
				// $VAR syntax
				j := i + 1
				for j < len(s) && (isAlphaNumUnderscore(s[j])) {
					j++
				}
				if j == i+1 {
					result.WriteByte(s[i])
					i++
					continue
				}
				varName := s[i+1 : j]
				if val, ok := defaults[varName]; ok {
					result.WriteString(val)
				} else {
					result.WriteString(s[i:j])
					hasUnresolved = true
				}
				i = j
			}
		} else {
			result.WriteByte(s[i])
			i++
		}
	}
	return result.String(), hasUnresolved
}

func isAlphaNumUnderscore(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/dockerfile/ -v -run TestParse`
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add internal/dockerfile/parse.go internal/dockerfile/parse_test.go testdata/
git commit -m "feat: implement Dockerfile FROM instruction parser"
```

---

### Task 3: Dockerfile Parser — ARG Variable Expansion

**Files:**
- Modify: `internal/dockerfile/parse_test.go`
- Create: `testdata/with_args.Dockerfile`

- [ ] **Step 1: Create test data**

```dockerfile
# testdata/with_args.Dockerfile
ARG NODE_VERSION=20.11.1
FROM node:${NODE_VERSION}

ARG REGISTRY=docker.io
FROM ${REGISTRY}/python:3.12-slim AS builder

ARG BASE_IMAGE
FROM ${BASE_IMAGE}

FROM --platform=$BUILDPLATFORM golang:1.22
```

- [ ] **Step 2: Write failing tests for ARG expansion**

Add to `internal/dockerfile/parse_test.go`:

```go
func TestParse_ArgExpansion(t *testing.T) {
	input := `ARG NODE_VERSION=20.11.1
FROM node:${NODE_VERSION}
`
	instructions, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(instructions) != 1 {
		t.Fatalf("expected 1 instruction, got %d", len(instructions))
	}
	if instructions[0].ImageRef != "node:20.11.1" {
		t.Errorf("ImageRef = %q, want %q", instructions[0].ImageRef, "node:20.11.1")
	}
	if instructions[0].RawRef != "node:${NODE_VERSION}" {
		t.Errorf("RawRef = %q, want %q", instructions[0].RawRef, "node:${NODE_VERSION}")
	}
	if instructions[0].Skip {
		t.Error("should not be skipped (ARG has default value)")
	}
}

func TestParse_ArgNoDefault(t *testing.T) {
	input := `ARG BASE_IMAGE
FROM ${BASE_IMAGE}
`
	instructions, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(instructions) != 1 {
		t.Fatalf("expected 1 instruction, got %d", len(instructions))
	}
	if !instructions[0].Skip {
		t.Error("should be skipped (ARG has no default)")
	}
	if instructions[0].SkipReason != "unresolved ARG variable" {
		t.Errorf("SkipReason = %q", instructions[0].SkipReason)
	}
}

func TestParse_PlatformVariable(t *testing.T) {
	input := `FROM --platform=$BUILDPLATFORM golang:1.22
`
	instructions, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(instructions) != 1 {
		t.Fatalf("expected 1 instruction, got %d", len(instructions))
	}
	// Image ref is static, platform has variable — should NOT be skipped
	if instructions[0].ImageRef != "golang:1.22" {
		t.Errorf("ImageRef = %q, want %q", instructions[0].ImageRef, "golang:1.22")
	}
	if instructions[0].Skip {
		t.Error("should not be skipped (only platform is variable)")
	}
}

func TestExpandVars(t *testing.T) {
	defaults := map[string]string{
		"VERSION": "3.12",
		"REG":     "ghcr.io",
	}
	tests := []struct {
		input      string
		want       string
		unresolved bool
	}{
		{"python:${VERSION}", "python:3.12", false},
		{"${REG}/app:latest", "ghcr.io/app:latest", false},
		{"${UNKNOWN}/app", "${UNKNOWN}/app", true},
		{"node:20", "node:20", false},
		{"$VERSION-slim", "3.12-slim", false},
	}
	for _, tt := range tests {
		got, unresolved := expandVars(tt.input, defaults)
		if got != tt.want {
			t.Errorf("expandVars(%q) = %q, want %q", tt.input, got, tt.want)
		}
		if unresolved != tt.unresolved {
			t.Errorf("expandVars(%q) unresolved = %v, want %v", tt.input, unresolved, tt.unresolved)
		}
	}
}
```

- [ ] **Step 3: Run tests to verify they pass**

Run: `go test ./internal/dockerfile/ -v -run "TestParse_Arg|TestParse_Platform|TestExpandVars"`
Expected: all PASS (implementation is already in Task 2)

- [ ] **Step 4: Commit**

```bash
git add internal/dockerfile/parse_test.go testdata/with_args.Dockerfile
git commit -m "test: add ARG expansion and platform variable tests"
```

---

### Task 4: Digest Resolver — Interface and Crane Implementation

**Files:**
- Create: `internal/resolver/resolver.go`
- Create: `internal/resolver/resolver_test.go`

- [ ] **Step 1: Write failing test for MockResolver (verify interface)**

```go
// internal/resolver/resolver_test.go
package resolver

import (
	"context"
	"testing"
)

func TestMockResolver_Resolve(t *testing.T) {
	mock := &MockResolver{
		Digests: map[string]string{
			"node:20.11.1":    "sha256:abc123",
			"python:3.12-slim": "sha256:def456",
		},
	}
	ctx := context.Background()

	digest, err := mock.Resolve(ctx, "node:20.11.1")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if digest != "sha256:abc123" {
		t.Errorf("Resolve() = %q, want %q", digest, "sha256:abc123")
	}

	_, err = mock.Resolve(ctx, "unknown:latest")
	if err == nil {
		t.Error("Resolve() expected error for unknown image")
	}
}

func TestMockResolver_Exists(t *testing.T) {
	mock := &MockResolver{
		Digests: map[string]string{
			"node:20.11.1@sha256:abc123": "sha256:abc123",
		},
	}
	ctx := context.Background()

	exists, err := mock.Exists(ctx, "node:20.11.1@sha256:abc123")
	if err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if !exists {
		t.Error("Exists() = false, want true")
	}

	exists, err = mock.Exists(ctx, "node:20.11.1@sha256:nonexistent")
	if err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if exists {
		t.Error("Exists() = true, want false")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/resolver/ -v`
Expected: compilation error — types not defined

- [ ] **Step 3: Implement resolver interface, CraneResolver, and MockResolver**

```go
// internal/resolver/resolver.go
package resolver

import (
	"context"
	"fmt"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// DigestResolver resolves image digests from a container registry.
type DigestResolver interface {
	// Resolve returns the digest for the given image reference (e.g., "node:20.11.1").
	// Returns the digest string like "sha256:abc123...".
	Resolve(ctx context.Context, imageRef string) (string, error)

	// Exists checks whether the given image reference exists in the registry.
	// The imageRef should include a digest (e.g., "node:20.11.1@sha256:abc123").
	Exists(ctx context.Context, imageRef string) (bool, error)
}

// CraneResolver resolves digests using go-containerregistry (crane).
type CraneResolver struct{}

func (r *CraneResolver) Resolve(ctx context.Context, imageRef string) (string, error) {
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return "", fmt.Errorf("parsing reference %q: %w", imageRef, err)
	}

	desc, err := remote.Head(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain), remote.WithContext(ctx))
	if err != nil {
		return "", fmt.Errorf("resolving digest for %q: %w", imageRef, err)
	}

	return desc.Digest.String(), nil
}

func (r *CraneResolver) Exists(ctx context.Context, imageRef string) (bool, error) {
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return false, fmt.Errorf("parsing reference %q: %w", imageRef, err)
	}

	_, err = remote.Head(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain), remote.WithContext(ctx))
	if err != nil {
		return false, nil
	}

	return true, nil
}

// MockResolver is a test double for DigestResolver.
type MockResolver struct {
	Digests map[string]string
}

func (r *MockResolver) Resolve(_ context.Context, imageRef string) (string, error) {
	digest, ok := r.Digests[imageRef]
	if !ok {
		return "", fmt.Errorf("unknown image: %s", imageRef)
	}
	return digest, nil
}

func (r *MockResolver) Exists(_ context.Context, imageRef string) (bool, error) {
	_, ok := r.Digests[imageRef]
	return ok, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/resolver/ -v`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/resolver/resolver.go internal/resolver/resolver_test.go
git commit -m "feat: implement DigestResolver interface with crane and mock"
```

---

### Task 5: Dockerfile Rewriter

**Files:**
- Create: `internal/dockerfile/rewrite.go`
- Create: `internal/dockerfile/rewrite_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/dockerfile/rewrite_test.go
package dockerfile

import (
	"testing"
)

func TestAddDigest(t *testing.T) {
	tests := []struct {
		name     string
		original string
		rawRef   string
		digest   string
		want     string
	}{
		{
			name:     "simple tag",
			original: "FROM node:20.11.1",
			rawRef:   "node:20.11.1",
			digest:   "sha256:abc123",
			want:     "FROM node:20.11.1@sha256:abc123",
		},
		{
			name:     "with AS clause",
			original: "FROM python:3.12-slim AS builder",
			rawRef:   "python:3.12-slim",
			digest:   "sha256:def456",
			want:     "FROM python:3.12-slim@sha256:def456 AS builder",
		},
		{
			name:     "with platform",
			original: "FROM --platform=linux/amd64 golang:1.22",
			rawRef:   "golang:1.22",
			digest:   "sha256:ghi789",
			want:     "FROM --platform=linux/amd64 golang:1.22@sha256:ghi789",
		},
		{
			name:     "with ARG variable",
			original: "FROM node:${NODE_VERSION}",
			rawRef:   "node:${NODE_VERSION}",
			digest:   "sha256:abc123",
			want:     "FROM node:${NODE_VERSION}@sha256:abc123",
		},
		{
			name:     "update existing digest",
			original: "FROM node:20.11.1@sha256:olddigest",
			rawRef:   "node:20.11.1@sha256:olddigest",
			digest:   "sha256:newdigest",
			want:     "FROM node:20.11.1@sha256:newdigest",
		},
		{
			name:     "with platform and AS",
			original: "FROM --platform=linux/amd64 golang:1.22 AS builder",
			rawRef:   "golang:1.22",
			digest:   "sha256:abc123",
			want:     "FROM --platform=linux/amd64 golang:1.22@sha256:abc123 AS builder",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AddDigest(tt.original, tt.rawRef, tt.digest)
			if got != tt.want {
				t.Errorf("AddDigest() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRewriteFile(t *testing.T) {
	content := `# My Dockerfile
FROM node:20.11.1
RUN npm install
FROM python:3.12-slim AS builder
RUN pip install -r requirements.txt
FROM scratch
`
	instructions := []FromInstruction{
		{ImageRef: "node:20.11.1", RawRef: "node:20.11.1", StartLine: 2, Original: "FROM node:20.11.1"},
		{ImageRef: "python:3.12-slim", RawRef: "python:3.12-slim", StartLine: 4, Original: "FROM python:3.12-slim AS builder"},
		{ImageRef: "scratch", RawRef: "scratch", StartLine: 6, Original: "FROM scratch", Skip: true, SkipReason: "scratch image"},
	}
	digests := map[int]string{
		0: "sha256:abc123",
		1: "sha256:def456",
	}

	got := RewriteFile(content, instructions, digests)
	want := `# My Dockerfile
FROM node:20.11.1@sha256:abc123
RUN npm install
FROM python:3.12-slim@sha256:def456 AS builder
RUN pip install -r requirements.txt
FROM scratch
`
	if got != want {
		t.Errorf("RewriteFile() =\n%s\nwant:\n%s", got, want)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/dockerfile/ -v -run "TestAddDigest|TestRewriteFile"`
Expected: compilation error — `AddDigest` and `RewriteFile` not defined

- [ ] **Step 3: Implement AddDigest and RewriteFile**

```go
// internal/dockerfile/rewrite.go
package dockerfile

import (
	"strings"
)

// AddDigest inserts or replaces a digest in a FROM line.
// rawRef is the image reference as written in the original line.
// digest is the new digest string (e.g., "sha256:abc123").
func AddDigest(original string, rawRef string, digest string) string {
	if atIdx := strings.Index(rawRef, "@"); atIdx >= 0 {
		// Replace existing digest: swap rawRef with baseRef@newDigest
		baseRef := rawRef[:atIdx]
		newRef := baseRef + "@" + digest
		return strings.Replace(original, rawRef, newRef, 1)
	}
	// Insert digest after rawRef
	newRef := rawRef + "@" + digest
	return strings.Replace(original, rawRef, newRef, 1)
}

// RewriteFile applies digest pins to a Dockerfile's content.
// digests maps instruction index to digest string.
// Instructions marked as Skip are ignored even if present in digests.
func RewriteFile(content string, instructions []FromInstruction, digests map[int]string) string {
	lines := strings.Split(content, "\n")

	for i, inst := range instructions {
		digest, ok := digests[i]
		if !ok || inst.Skip {
			continue
		}
		lineIdx := inst.StartLine - 1
		if lineIdx >= 0 && lineIdx < len(lines) {
			lines[lineIdx] = AddDigest(lines[lineIdx], inst.RawRef, digest)
		}
	}

	return strings.Join(lines, "\n")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/dockerfile/ -v -run "TestAddDigest|TestRewriteFile"`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/dockerfile/rewrite.go internal/dockerfile/rewrite_test.go
git commit -m "feat: implement FROM line rewriter with digest insertion"
```

---

### Task 6: File Discovery (--file and --glob)

**Files:**
- Create: `cmd/files.go`
- Create: `cmd/files_test.go`

- [ ] **Step 1: Write failing tests**

```go
// cmd/files_test.go
package cmd

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestFindFiles_SingleFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Dockerfile")
	os.WriteFile(path, []byte("FROM node:20"), 0644)

	files, err := FindFiles(path, "")
	if err != nil {
		t.Fatalf("FindFiles() error = %v", err)
	}
	if len(files) != 1 || files[0] != path {
		t.Errorf("FindFiles() = %v, want [%s]", files, path)
	}
}

func TestFindFiles_GlobPattern(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "services", "api")
	os.MkdirAll(sub, 0755)
	os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM node:20"), 0644)
	os.WriteFile(filepath.Join(sub, "Dockerfile"), []byte("FROM python:3"), 0644)
	os.WriteFile(filepath.Join(sub, "Dockerfile.dev"), []byte("FROM python:3"), 0644)

	files, err := FindFiles("", filepath.Join(dir, "**", "Dockerfile*"))
	if err != nil {
		t.Fatalf("FindFiles() error = %v", err)
	}
	sort.Strings(files)
	if len(files) != 3 {
		t.Errorf("FindFiles() returned %d files, want 3: %v", len(files), files)
	}
}

func TestFindFiles_DefaultFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM node:20"), 0644)

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	files, err := FindFiles("", "")
	if err != nil {
		t.Fatalf("FindFiles() error = %v", err)
	}
	if len(files) != 1 {
		t.Errorf("FindFiles() = %v, want 1 file", files)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/ -v -run TestFindFiles`
Expected: compilation error — `FindFiles` not defined

- [ ] **Step 3: Implement FindFiles**

```go
// cmd/files.go
package cmd

import (
	"fmt"
	"os"

	"github.com/bmatcuk/doublestar/v4"
)

// FindFiles returns a list of Dockerfile paths based on flags.
// If filePath is set, returns that single file.
// If globPattern is set, returns all matching files.
// If neither is set, defaults to "./Dockerfile".
func FindFiles(filePath string, globPattern string) ([]string, error) {
	if filePath != "" {
		if _, err := os.Stat(filePath); err != nil {
			return nil, fmt.Errorf("file not found: %s", filePath)
		}
		return []string{filePath}, nil
	}

	if globPattern != "" {
		matches, err := doublestar.FilepathGlob(globPattern)
		if err != nil {
			return nil, fmt.Errorf("invalid glob pattern: %w", err)
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("no files matched pattern: %s", globPattern)
		}
		return matches, nil
	}

	// Default: ./Dockerfile
	defaultPath := "Dockerfile"
	if _, err := os.Stat(defaultPath); err != nil {
		return nil, fmt.Errorf("no Dockerfile found in current directory")
	}
	return []string{defaultPath}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ -v -run TestFindFiles`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/files.go cmd/files_test.go
git commit -m "feat: implement file discovery with glob support"
```

---

### Task 7: Pin Command

**Files:**
- Create: `cmd/pin.go`

- [ ] **Step 1: Implement pin subcommand**

```go
// cmd/pin.go
package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/azu/dockerfile-pin/internal/dockerfile"
	"github.com/azu/dockerfile-pin/internal/resolver"
	"github.com/spf13/cobra"
)

var pinCmd = &cobra.Command{
	Use:   "pin",
	Short: "Pin FROM images to their digests",
	Long:  "Parse Dockerfile FROM lines and add @sha256:<digest> to each image reference.",
	RunE:  runPin,
}

var (
	pinFile     string
	pinGlob     string
	pinDryRun   bool
	pinUpdate   bool
	pinPlatform string
)

func init() {
	pinCmd.Flags().StringVarP(&pinFile, "file", "f", "", "Dockerfile path (default: ./Dockerfile)")
	pinCmd.Flags().StringVar(&pinGlob, "glob", "", "Glob pattern to find Dockerfiles")
	pinCmd.Flags().BoolVar(&pinDryRun, "dry-run", false, "Show changes without writing files")
	pinCmd.Flags().BoolVar(&pinUpdate, "update", false, "Update existing digests")
	pinCmd.Flags().StringVar(&pinPlatform, "platform", "", "Platform for multi-arch images (e.g., linux/amd64)")
	rootCmd.AddCommand(pinCmd)
}

func runPin(cmd *cobra.Command, args []string) error {
	files, err := FindFiles(pinFile, pinGlob)
	if err != nil {
		return err
	}

	ctx := context.Background()
	res := &resolver.CraneResolver{}
	hasChanges := false

	for _, filePath := range files {
		changed, err := pinFile_(ctx, filePath, res, pinDryRun, pinUpdate)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error processing %s: %v\n", filePath, err)
			continue
		}
		if changed {
			hasChanges = true
		}
	}

	if pinDryRun && hasChanges {
		return nil
	}
	return nil
}

func pinFile_(ctx context.Context, filePath string, res resolver.DigestResolver, dryRun bool, update bool) (bool, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", filePath, err)
	}

	instructions, err := dockerfile.Parse(strings.NewReader(string(content)))
	if err != nil {
		return false, fmt.Errorf("parsing %s: %w", filePath, err)
	}

	digests := make(map[int]string)
	for i, inst := range instructions {
		if inst.Skip {
			if inst.SkipReason == "unresolved ARG variable" {
				fmt.Fprintf(os.Stderr, "WARN  %s:%d  %s  %s\n", filePath, inst.StartLine, inst.Original, inst.SkipReason)
			}
			continue
		}
		if inst.Digest != "" && !update {
			continue
		}

		digest, err := res.Resolve(ctx, inst.ImageRef)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARN  %s:%d  %s  failed to resolve: %v\n", filePath, inst.StartLine, inst.Original, err)
			continue
		}
		digests[i] = digest
	}

	if len(digests) == 0 {
		return false, nil
	}

	result := dockerfile.RewriteFile(string(content), instructions, digests)

	if dryRun {
		fmt.Printf("--- %s\n", filePath)
		fmt.Println(result)
		return true, nil
	}

	if err := os.WriteFile(filePath, []byte(result), 0644); err != nil {
		return false, fmt.Errorf("writing %s: %w", filePath, err)
	}
	fmt.Printf("pinned %d image(s) in %s\n", len(digests), filePath)
	return true, nil
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 3: Manual test with dry-run**

Create a temporary test Dockerfile and run:
```bash
echo 'FROM alpine:3.19' > /tmp/test-Dockerfile
go run . pin -f /tmp/test-Dockerfile --dry-run
```
Expected: output shows the Dockerfile with `@sha256:...` appended to `alpine:3.19`

- [ ] **Step 4: Commit**

```bash
git add cmd/pin.go
git commit -m "feat: implement pin subcommand"
```

---

### Task 8: Check Command

**Files:**
- Create: `cmd/check.go`

- [ ] **Step 1: Implement check subcommand**

```go
// cmd/check.go
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/azu/dockerfile-pin/internal/dockerfile"
	"github.com/azu/dockerfile-pin/internal/resolver"
	"github.com/spf13/cobra"
)

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Check if FROM images are pinned to digests",
	Long:  "Validate that Dockerfile FROM lines have @sha256:<digest> and that digests exist in the registry.",
	RunE:  runCheck,
}

var (
	checkFile        string
	checkGlob        string
	checkSyntaxOnly  bool
	checkFormat      string
	checkIgnore      []string
	checkExitCode    int
)

func init() {
	checkCmd.Flags().StringVarP(&checkFile, "file", "f", "", "Dockerfile path (default: ./Dockerfile)")
	checkCmd.Flags().StringVar(&checkGlob, "glob", "", "Glob pattern to find Dockerfiles")
	checkCmd.Flags().BoolVar(&checkSyntaxOnly, "syntax-only", false, "Skip registry checks")
	checkCmd.Flags().StringVar(&checkFormat, "format", "text", "Output format: text or json")
	checkCmd.Flags().StringSliceVar(&checkIgnore, "ignore-images", nil, "Images to ignore (e.g., scratch)")
	checkCmd.Flags().IntVar(&checkExitCode, "exit-code", 1, "Exit code on failure")
	rootCmd.AddCommand(checkCmd)
}

// CheckResult represents the result of checking a single FROM instruction.
type CheckResult struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Image    string `json:"image"`
	Status   string `json:"status"` // "ok", "fail", "skip", "warn"
	Message  string `json:"message"`
	Original string `json:"original"`
}

func runCheck(cmd *cobra.Command, args []string) error {
	files, err := FindFiles(checkFile, checkGlob)
	if err != nil {
		return err
	}

	ctx := context.Background()
	res := &resolver.CraneResolver{}
	var results []CheckResult
	hasFail := false

	for _, filePath := range files {
		fileResults, err := checkFile_(ctx, filePath, res, checkSyntaxOnly, checkIgnore)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error processing %s: %v\n", filePath, err)
			continue
		}
		results = append(results, fileResults...)
		for _, r := range fileResults {
			if r.Status == "fail" {
				hasFail = true
			}
		}
	}

	switch checkFormat {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(results)
	default:
		for _, r := range results {
			var prefix string
			switch r.Status {
			case "ok":
				prefix = "OK   "
			case "fail":
				prefix = "FAIL "
			case "skip":
				prefix = "SKIP "
			case "warn":
				prefix = "WARN "
			}
			fmt.Printf("%-5s %s:%-4d %-50s %s\n", prefix, r.File, r.Line, r.Original, r.Message)
		}
	}

	if hasFail {
		os.Exit(checkExitCode)
	}
	return nil
}

func checkFile_(ctx context.Context, filePath string, res resolver.DigestResolver, syntaxOnly bool, ignoreImages []string) ([]CheckResult, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", filePath, err)
	}

	instructions, err := dockerfile.Parse(strings.NewReader(string(content)))
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", filePath, err)
	}

	var results []CheckResult

	for _, inst := range instructions {
		if inst.Skip {
			results = append(results, CheckResult{
				File:     filePath,
				Line:     inst.StartLine,
				Image:    inst.ImageRef,
				Status:   "skip",
				Message:  inst.SkipReason,
				Original: inst.Original,
			})
			continue
		}

		if isIgnored(inst.ImageRef, ignoreImages) {
			results = append(results, CheckResult{
				File:     filePath,
				Line:     inst.StartLine,
				Image:    inst.ImageRef,
				Status:   "skip",
				Message:  "ignored",
				Original: inst.Original,
			})
			continue
		}

		if inst.Digest == "" {
			results = append(results, CheckResult{
				File:     filePath,
				Line:     inst.StartLine,
				Image:    inst.ImageRef,
				Status:   "fail",
				Message:  "missing digest",
				Original: inst.Original,
			})
			continue
		}

		if syntaxOnly {
			results = append(results, CheckResult{
				File:     filePath,
				Line:     inst.StartLine,
				Image:    inst.ImageRef,
				Status:   "ok",
				Message:  "",
				Original: inst.Original,
			})
			continue
		}

		// Registry existence check
		fullRef := inst.ImageRef + "@" + inst.Digest
		exists, err := res.Exists(ctx, fullRef)
		if err != nil {
			results = append(results, CheckResult{
				File:     filePath,
				Line:     inst.StartLine,
				Image:    inst.ImageRef,
				Status:   "warn",
				Message:  fmt.Sprintf("registry check failed: %v", err),
				Original: inst.Original,
			})
			continue
		}
		if !exists {
			results = append(results, CheckResult{
				File:     filePath,
				Line:     inst.StartLine,
				Image:    inst.ImageRef,
				Status:   "fail",
				Message:  "digest not found in registry",
				Original: inst.Original,
			})
			continue
		}

		results = append(results, CheckResult{
			File:     filePath,
			Line:     inst.StartLine,
			Image:    inst.ImageRef,
			Status:   "ok",
			Message:  "",
			Original: inst.Original,
		})
	}

	return results, nil
}

func isIgnored(imageRef string, patterns []string) bool {
	for _, pattern := range patterns {
		if strings.Contains(imageRef, pattern) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 3: Manual test**

```bash
echo 'FROM alpine:3.19' > /tmp/test-Dockerfile
go run . check -f /tmp/test-Dockerfile --syntax-only
```
Expected: `FAIL  /tmp/test-Dockerfile:1  FROM alpine:3.19  missing digest`

- [ ] **Step 4: Commit**

```bash
git add cmd/check.go
git commit -m "feat: implement check subcommand with syntax and registry validation"
```

---

### Task 9: End-to-End Test

**Files:**
- Create: `testdata/mixed.Dockerfile`
- Create: `e2e_test.go`

- [ ] **Step 1: Create test fixture**

```dockerfile
# testdata/mixed.Dockerfile
ARG NODE_VERSION=20.11.1
FROM node:${NODE_VERSION}
FROM python:3.12-slim AS builder
FROM --platform=linux/amd64 golang:1.22
FROM scratch
FROM builder AS final
FROM node:20.11.1@sha256:d938c1761e3afbae9242848ffbb95b9cc1cb0a24d889f8bd955204d347a7266e
```

- [ ] **Step 2: Write end-to-end test for pin (using mock resolver)**

```go
// e2e_test.go
package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/azu/dockerfile-pin/internal/dockerfile"
	"github.com/azu/dockerfile-pin/internal/resolver"
)

func TestPinEndToEnd(t *testing.T) {
	input := `ARG NODE_VERSION=20.11.1
FROM node:${NODE_VERSION}
FROM python:3.12-slim AS builder
FROM --platform=linux/amd64 golang:1.22
FROM scratch
FROM builder AS final
FROM node:20.11.1@sha256:existingdigest
`

	mock := &resolver.MockResolver{
		Digests: map[string]string{
			"node:20.11.1":        "sha256:aaa111",
			"python:3.12-slim":    "sha256:bbb222",
			"golang:1.22":         "sha256:ccc333",
		},
	}

	instructions, err := dockerfile.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	ctx := context.Background()
	digests := make(map[int]string)
	for i, inst := range instructions {
		if inst.Skip || inst.Digest != "" {
			continue
		}
		digest, err := mock.Resolve(ctx, inst.ImageRef)
		if err != nil {
			t.Logf("skipping %s: %v", inst.ImageRef, err)
			continue
		}
		digests[i] = digest
	}

	result := dockerfile.RewriteFile(input, instructions, digests)

	// Verify pinned lines
	if !strings.Contains(result, "FROM node:${NODE_VERSION}@sha256:aaa111") {
		t.Error("expected node ARG line to be pinned")
	}
	if !strings.Contains(result, "FROM python:3.12-slim@sha256:bbb222 AS builder") {
		t.Error("expected python line to be pinned with AS clause preserved")
	}
	if !strings.Contains(result, "FROM --platform=linux/amd64 golang:1.22@sha256:ccc333") {
		t.Error("expected golang line to be pinned with platform preserved")
	}

	// Verify skipped lines
	if !strings.Contains(result, "FROM scratch") {
		t.Error("scratch should be preserved")
	}
	if !strings.Contains(result, "FROM builder AS final") {
		t.Error("stage reference should be preserved")
	}

	// Verify already-pinned line is NOT changed (no --update)
	if !strings.Contains(result, "FROM node:20.11.1@sha256:existingdigest") {
		t.Error("already-pinned line should be preserved without --update")
	}
}

func TestCheckEndToEnd(t *testing.T) {
	input := `FROM node:20.11.1
FROM python:3.12-slim@sha256:validdigest
FROM golang:1.22@sha256:invaliddigest
FROM scratch
`

	mock := &resolver.MockResolver{
		Digests: map[string]string{
			"python:3.12-slim@sha256:validdigest": "sha256:validdigest",
		},
	}

	instructions, err := dockerfile.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	ctx := context.Background()
	type checkResult struct {
		imageRef string
		status   string
	}
	var results []checkResult

	for _, inst := range instructions {
		if inst.Skip {
			results = append(results, checkResult{inst.ImageRef, "skip"})
			continue
		}
		if inst.Digest == "" {
			results = append(results, checkResult{inst.ImageRef, "fail-missing"})
			continue
		}
		fullRef := inst.ImageRef + "@" + inst.Digest
		exists, _ := mock.Exists(ctx, fullRef)
		if exists {
			results = append(results, checkResult{inst.ImageRef, "ok"})
		} else {
			results = append(results, checkResult{inst.ImageRef, "fail-notfound"})
		}
	}

	expected := []checkResult{
		{"node:20.11.1", "fail-missing"},
		{"python:3.12-slim", "ok"},
		{"golang:1.22", "fail-notfound"},
		{"scratch", "skip"},
	}

	if len(results) != len(expected) {
		t.Fatalf("expected %d results, got %d", len(expected), len(results))
	}
	for i, want := range expected {
		if results[i] != want {
			t.Errorf("[%d] got %+v, want %+v", i, results[i], want)
		}
	}
}

func TestPinFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Dockerfile")
	content := "FROM alpine:3.19\nRUN echo hello\n"
	os.WriteFile(path, []byte(content), 0644)

	instructions, err := dockerfile.Parse(strings.NewReader(content))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	digests := map[int]string{0: "sha256:testdigest123"}
	result := dockerfile.RewriteFile(content, instructions, digests)

	os.WriteFile(path, []byte(result), 0644)
	written, _ := os.ReadFile(path)

	if !strings.Contains(string(written), "FROM alpine:3.19@sha256:testdigest123") {
		t.Errorf("round-trip failed: %s", string(written))
	}
	if !strings.Contains(string(written), "RUN echo hello") {
		t.Error("non-FROM lines should be preserved")
	}
}
```

- [ ] **Step 3: Run tests to verify they pass**

Run: `go test ./... -v`
Expected: all PASS

- [ ] **Step 4: Create test fixture file**

Write the mixed.Dockerfile to testdata/ as shown in Step 1.

- [ ] **Step 5: Commit**

```bash
git add e2e_test.go testdata/mixed.Dockerfile
git commit -m "test: add end-to-end tests for pin and check workflows"
```

---

### Task 10: Final Cleanup and .gitignore

**Files:**
- Create: `.gitignore`
- Modify: `cmd/root.go` (add version flag)

- [ ] **Step 1: Create .gitignore**

```gitignore
# .gitignore
/dockerfile-pin
dist/
```

- [ ] **Step 2: Add version info to root command**

Update `cmd/root.go`:

```go
// cmd/root.go
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var version = "dev"

var rootCmd = &cobra.Command{
	Use:   "dockerfile-pin",
	Short: "Pin Dockerfile images to digests",
	Long:  "A CLI tool that adds @sha256:<digest> to FROM lines in Dockerfiles to prevent supply chain attacks.",
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

func Execute() error {
	return rootCmd.Execute()
}
```

- [ ] **Step 3: Run full test suite**

Run: `go test ./... -v`
Expected: all PASS

- [ ] **Step 4: Build and verify CLI**

Run:
```bash
go build -o dockerfile-pin .
./dockerfile-pin --help
./dockerfile-pin pin --help
./dockerfile-pin check --help
./dockerfile-pin version
```

Expected: help text and version output displayed correctly

- [ ] **Step 5: Commit**

```bash
git add .gitignore cmd/root.go
git commit -m "chore: add .gitignore and version command"
```
