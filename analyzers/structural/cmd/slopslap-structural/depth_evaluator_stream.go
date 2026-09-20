package main

import (
	"encoding/json"
	"errors"
	"io"

	"slopslap.dev/structural/internal/depth"
	"slopslap.dev/structural/internal/facts"
	"slopslap.dev/structural/internal/metrics"
)

// The synchronous evaluator remains the default protocol. Streaming callers
// receive the same assessments and scores, before the remaining boundaries run.
func runDepthEvaluatorStream(reader io.Reader, writer io.Writer) error {
	input, err := readDepthEvaluatorInput(reader)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(&depthStreamWriter{writer: writer, remaining: depthEvaluatorMaxBytes})
	encoder.SetEscapeHTML(false)
	var writeErr error
	emit := func(assessment facts.BoundaryAssessment) {
		if writeErr != nil {
			return
		}
		assessment = depth.ApplyBoundaryRoles(assessment)
		score, scoreErr := metrics.ScoreBoundary(assessment)
		if scoreErr != nil {
			score.State, score.HKnown, score.Shallow = facts.KnowledgeUnavailable, false, nil
			score.Reasons = append(score.Reasons, facts.Reason{Code: "malformed_facts", Dimension: "boundary", Message: scoreErr.Error()})
		}
		writeErr = encoder.Encode(struct {
			Type       string                   `json:"type"`
			Assessment facts.BoundaryAssessment `json:"assessment"`
			Score      metrics.DepthScore       `json:"score"`
		}{"score", assessment, score})
	}
	if len(input.Depth.Flows) > 0 {
		depth.AssessFlowBoundariesProgress(input.Depth, emit)
	} else {
		for _, assessment := range input.Depth.Boundaries {
			emit(assessment)
		}
	}
	if writeErr != nil {
		return writeErr
	}
	return encoder.Encode(map[string]any{"type": "done", "schema_version": 1})
}

// Bound aggregate output just as the non-streaming protocol does, without
// retaining the complete response in a second buffer.
type depthStreamWriter struct {
	writer    io.Writer
	remaining int
}

func (writer *depthStreamWriter) Write(payload []byte) (int, error) {
	if len(payload) > writer.remaining {
		return 0, errors.New("depth evaluator output exceeds 64 MiB")
	}
	count, err := writer.writer.Write(payload)
	writer.remaining -= count
	return count, err
}
