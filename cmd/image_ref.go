package cmd

import (
	"github.com/azu/dockerfile-pin/internal/actions"
	"github.com/azu/dockerfile-pin/internal/compose"
	"github.com/azu/dockerfile-pin/internal/dockerfile"
)

// imageReference is a common representation of an image reference across file types
// (Dockerfile, docker-compose, GitHub Actions).
type imageReference struct {
	ImageRef   string
	Digest     string
	Line       int
	Skip       bool
	SkipReason string
	Original   string
}

func dockerfileToImageRefs(insts []dockerfile.FromInstruction) []imageReference {
	refs := make([]imageReference, len(insts))
	for i, inst := range insts {
		refs[i] = imageReference{
			ImageRef:   inst.ImageRef,
			Digest:     inst.Digest,
			Line:       inst.StartLine,
			Skip:       inst.Skip,
			SkipReason: inst.SkipReason,
			Original:   inst.Original,
		}
	}
	return refs
}

func composeToImageRefs(crefs []compose.ComposeImageRef) []imageReference {
	refs := make([]imageReference, len(crefs))
	for i, ref := range crefs {
		refs[i] = imageReference{
			ImageRef:   ref.ImageRef,
			Digest:     ref.Digest,
			Line:       ref.Line,
			Skip:       ref.Skip,
			SkipReason: ref.SkipReason,
			Original:   "image: " + ref.RawRef,
		}
	}
	return refs
}

func actionsToImageRefs(arefs []actions.ActionsImageRef) []imageReference {
	refs := make([]imageReference, len(arefs))
	for i, ref := range arefs {
		refs[i] = imageReference{
			ImageRef:   ref.ImageRef,
			Digest:     ref.Digest,
			Line:       ref.Line,
			Skip:       ref.Skip,
			SkipReason: ref.SkipReason,
			Original:   actionsOriginal(ref),
		}
	}
	return refs
}
