package javaadapter

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"

	"slopslap.dev/structural/internal/facts"
)

type streamCallbacks struct {
	syntax func(*facts.Program)
	depth  func(*facts.DepthFacts, []string)
	stage  func(string, int, int)
}

func depthCallback(options map[string]any) func(*facts.DepthFacts, []string) {
	callback, _ := options["depth_progress"].(func(*facts.DepthFacts, []string))
	return callback
}

func stageCallback(options map[string]any) func(string, int, int) {
	callback, _ := options["analysis_progress"].(func(string, int, int))
	return callback
}

func readDepthStream(data *bufio.Reader, callbacks streamCallbacks) (*facts.DepthFacts, error) {
	result := &facts.DepthFacts{}
	pending := map[string]*facts.DepthFacts{}
	groupArtifacts := map[string]map[string]int{}
	artifacts := map[string]int{}
	seen := map[string]bool{}
	for {
		payload, err := readDepthFrame(data)
		if err != nil {
			return nil, fmt.Errorf("truncated Java depth stream: %w", err)
		}
		var frame struct {
			Type      string   `json:"type"`
			Payload   string   `json:"payload"`
			Group     string   `json:"group"`
			Paths     []string `json:"paths"`
			Stage     string   `json:"stage"`
			Completed int      `json:"completed"`
			Total     int      `json:"total"`
		}
		if err := json.Unmarshal([]byte(payload), &frame); err != nil {
			return nil, err
		}
		switch frame.Type {
		case "depth":
			var chunk facts.DepthFacts
			if err := json.Unmarshal([]byte(frame.Payload), &chunk); err != nil {
				return nil, err
			}
			result.Boundaries = append(result.Boundaries, chunk.Boundaries...)
			result.Creations = append(result.Creations, chunk.Creations...)
			result.Reasons = append(result.Reasons, chunk.Reasons...)
			for _, flow := range chunk.Flows {
				if err := mergeDepthFlow(result, artifacts, flow); err != nil {
					return nil, err
				}
			}
			if len(chunk.Boundaries) > 0 || len(chunk.Flows) > 0 {
				if pending[frame.Group] == nil {
					pending[frame.Group] = &facts.DepthFacts{}
					groupArtifacts[frame.Group] = map[string]int{}
				}
				group := pending[frame.Group]
				group.Boundaries = append(group.Boundaries, chunk.Boundaries...)
				for _, flow := range chunk.Flows {
					if err := mergeDepthFlow(group, groupArtifacts[frame.Group], flow); err != nil {
						return nil, err
					}
				}
			}
		case "group_done":
			group := pending[frame.Group]
			if group == nil {
				group = &facts.DepthFacts{}
			}
			paths := map[string]bool{}
			for _, path := range frame.Paths {
				if path == "" || seen[path] {
					return nil, fmt.Errorf("invalid Java completed file %q", path)
				}
				paths[path], seen[path] = true, true
			}
			for _, boundary := range group.Boundaries {
				for _, path := range boundary.Files {
					if !paths[path] {
						return nil, fmt.Errorf("Java boundary crosses completed group")
					}
				}
			}
			if callbacks.depth != nil {
				callbacks.depth(group, frame.Paths)
			}
			delete(pending, frame.Group)
			delete(groupArtifacts, frame.Group)
		case "progress":
			if callbacks.stage != nil {
				callbacks.stage(frame.Stage, frame.Completed, frame.Total)
			}
		case "done":
			if len(pending) != 0 {
				return nil, fmt.Errorf("Java depth stream ended before file completion")
			}
			if _, err := data.Peek(1); err != io.EOF {
				return nil, fmt.Errorf("trailing Java depth output")
			}
			if len(result.Boundaries) == 0 && len(result.Flows) == 0 && len(result.Reasons) == 0 {
				return nil, nil
			}
			return result, nil
		default:
			return nil, fmt.Errorf("unknown Java depth frame %q", frame.Type)
		}
	}
}

func readDepthFrame(data *bufio.Reader) (string, error) {
	size, err := readUint32(data)
	if err != nil {
		return "", err
	}
	if size > 64<<20 {
		return "", fmt.Errorf("Java depth frame exceeds 64 MiB")
	}
	payload := make([]byte, int(size))
	_, err = io.ReadFull(data, payload)
	return string(payload), err
}
