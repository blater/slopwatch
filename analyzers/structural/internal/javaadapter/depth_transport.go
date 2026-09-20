package javaadapter

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"

	"slopslap.dev/structural/internal/facts"
)

func analyzeProfile(java, jar, workspace string, paths []string, includeTests bool, options map[string]any, progress ...streamCallbacks) (*facts.Program, error) {
	if options["depth_profile"] == "responsibility-v4" {
		// Attribution must see the whole supplied semantic unit, including helpers.
		return analyzeBatchProfile(java, jar, workspace, paths, includeTests, true, progress...)
	}
	return analyzePaths(paths, func(batch []string) (*facts.Program, error) {
		return analyzeBatch(java, jar, workspace, batch, includeTests)
	})
}

func readDepth(data *bufio.Reader, progress ...streamCallbacks) (*facts.DepthFacts, error) {
	count, err := readUint32(data)
	if err != nil {
		return nil, err
	}
	if count == ^uint32(0) {
		var callbacks streamCallbacks
		if len(progress) > 0 {
			callbacks = progress[0]
		}
		return readDepthStream(data, callbacks)
	}
	if count == 0 {
		return nil, nil
	}
	if count > 1_000_000 {
		return nil, fmt.Errorf("Java depth chunk count exceeds limit")
	}
	depth := &facts.DepthFacts{}
	artifacts := make(map[string]int)
	for index := uint32(0); index < count; index++ {
		chunk, err := readDepthChunk(data)
		if err != nil {
			return nil, fmt.Errorf("Java depth chunk %d: %w", index, err)
		}
		depth.Boundaries = append(depth.Boundaries, chunk.Boundaries...)
		depth.Creations = append(depth.Creations, chunk.Creations...)
		depth.Reasons = append(depth.Reasons, chunk.Reasons...)
		for _, flow := range chunk.Flows {
			if err := mergeDepthFlow(depth, artifacts, flow); err != nil {
				return nil, err
			}
		}
	}
	return depth, nil
}

// Frames bound temporary transport allocations per boundary, rather than
// imposing one payload limit on every file in the supplied semantic unit.
func readDepthChunk(data *bufio.Reader) (*facts.DepthFacts, error) {
	size, err := readUint32(data)
	if err != nil {
		return nil, err
	}
	if size > 64<<20 {
		return nil, fmt.Errorf("Java depth payload exceeds 64 MiB")
	}
	payload := make([]byte, int(size))
	if _, err := io.ReadFull(data, payload); err != nil {
		return nil, err
	}
	var depth facts.DepthFacts
	if err := json.Unmarshal(payload, &depth); err != nil {
		return nil, fmt.Errorf("invalid Java depth facts: %w", err)
	}
	return &depth, nil
}

func mergeDepthFlow(depth *facts.DepthFacts, artifacts map[string]int, incoming facts.FlowArtifact) error {
	index, exists := artifacts[incoming.Artifact]
	if !exists {
		artifacts[incoming.Artifact] = len(depth.Flows)
		depth.Flows = append(depth.Flows, incoming)
		return nil
	}
	current := &depth.Flows[index]
	if current.Language != incoming.Language || current.BuildSelection != incoming.BuildSelection {
		return fmt.Errorf("conflicting Java depth artifact metadata for %q", incoming.Artifact)
	}
	current.Functions = append(current.Functions, incoming.Functions...)
	current.Types = append(current.Types, incoming.Types...)
	current.PublicRoutes = append(current.PublicRoutes, incoming.PublicRoutes...)
	current.Provenance = append(current.Provenance, incoming.Provenance...)
	return nil
}
