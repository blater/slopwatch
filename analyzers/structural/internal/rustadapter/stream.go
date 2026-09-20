package rustadapter

import (
	"encoding/json"
	"fmt"
	"io"

	"slopslap.dev/structural/internal/facts"
)

func decodeFactStream(reader io.Reader, options map[string]any, syntax func(*facts.Program)) (factResponse, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	result := factResponse{}
	seen := map[string]bool{}
	allowed := map[string]bool{}
	depth, _ := options["depth_progress"].(func(*facts.DepthFacts, []string))
	stage, _ := options["analysis_progress"].(func(string, int, int))
	for {
		var frame struct {
			Type          string            `json:"type"`
			Program       *facts.Program    `json:"program"`
			Depth         *facts.DepthFacts `json:"depth"`
			Path          string            `json:"path"`
			Stage         string            `json:"stage"`
			Completed     int               `json:"completed"`
			Total         int               `json:"total"`
			SchemaVersion int               `json:"schema_version"`
			Error         *string           `json:"error"`
		}
		if err := decoder.Decode(&frame); err != nil {
			return result, fmt.Errorf("truncated Rust fact stream: %w", err)
		}
		switch frame.Type {
		case "syntax":
			if result.Program != nil || frame.Program == nil {
				return result, fmt.Errorf("invalid Rust syntax frame")
			}
			result.Program = frame.Program
			if options["depth_profile"] == "responsibility-v4" {
				result.Program.Depth = &facts.DepthFacts{}
			}
			if err := result.Program.LinkTypeMethods(); err != nil {
				return result, err
			}
			for _, path := range result.Program.Files {
				allowed[path] = true
			}
			syntax(result.Program)
		case "depth":
			if result.Program == nil || frame.Depth == nil || !allowed[frame.Path] || seen[frame.Path] {
				return result, fmt.Errorf("invalid Rust depth frame")
			}
			for _, boundary := range frame.Depth.Boundaries {
				for _, path := range boundary.Files {
					if path != frame.Path {
						return result, fmt.Errorf("Rust boundary crosses completed file")
					}
				}
			}
			seen[frame.Path] = true
			if result.Program.Depth == nil {
				result.Program.Depth = &facts.DepthFacts{}
			}
			result.Program.Depth.Boundaries = append(result.Program.Depth.Boundaries, frame.Depth.Boundaries...)
			result.Program.Depth.Flows = append(result.Program.Depth.Flows, frame.Depth.Flows...)
			if depth != nil {
				depth(frame.Depth, []string{frame.Path})
			}
		case "progress":
			if stage != nil {
				stage(frame.Stage, frame.Completed, frame.Total)
			}
		case "done":
			result.SchemaVersion, result.Error = frame.SchemaVersion, frame.Error
			var extra any
			if err := decoder.Decode(&extra); err != io.EOF {
				return result, fmt.Errorf("trailing Rust stream output")
			}
			return result, nil
		default:
			return result, fmt.Errorf("unknown Rust frame %q", frame.Type)
		}
	}
}
