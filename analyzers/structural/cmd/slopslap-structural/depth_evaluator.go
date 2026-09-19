package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	"slopslap.dev/structural/internal/depth"
	"slopslap.dev/structural/internal/facts"
	"slopslap.dev/structural/internal/metrics"
)

const depthEvaluatorMaxBytes = 64 << 20

type depthEvaluatorRequest struct {
	SchemaVersion int               `json:"schema_version"`
	Depth         *facts.DepthFacts `json:"depth"`
}

type depthEvaluatorResponse struct {
	SchemaVersion int                        `json:"schema_version"`
	Assessments   []facts.BoundaryAssessment `json:"assessments"`
	Scores        []metrics.DepthScore       `json:"scores"`
}

type depthEvaluatorBuffer struct {
	bytes.Buffer
	limit int
}

func (b *depthEvaluatorBuffer) Write(value []byte) (int, error) {
	if len(value) > b.limit-b.Len() {
		return 0, errors.New("depth evaluator output exceeds 64 MiB")
	}
	return b.Buffer.Write(value)
}

// runDepthEvaluator evaluates one normalized v4 payload and writes one JSON response.
func runDepthEvaluator(reader io.Reader, writer io.Writer) error {
	input, err := readDepthEvaluatorInput(reader)
	if err != nil {
		return err
	}
	return writeDepthEvaluatorResponse(writer, evaluateDepth(input.Depth))
}

func readDepthEvaluatorInput(reader io.Reader) (*depthEvaluatorRequest, error) {
	payload, err := io.ReadAll(io.LimitReader(reader, depthEvaluatorMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read depth evaluator input: %w", err)
	}
	if len(payload) > depthEvaluatorMaxBytes {
		return nil, errors.New("depth evaluator input exceeds 64 MiB")
	}

	return decodeDepthEvaluatorInput(payload)
}

func decodeDepthEvaluatorInput(payload []byte) (*depthEvaluatorRequest, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var input depthEvaluatorRequest
	if err := decoder.Decode(&input); err != nil {
		return nil, fmt.Errorf("decode depth evaluator input: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return nil, fmt.Errorf("decode depth evaluator trailing data: %w", err)
		}
		return nil, errors.New("depth evaluator input contains extra JSON records")
	}
	if input.SchemaVersion != 1 {
		return nil, fmt.Errorf("depth evaluator schema_version %d is unsupported", input.SchemaVersion)
	}
	if input.Depth == nil {
		return nil, errors.New("depth evaluator input is missing depth")
	}

	return &input, nil
}

func evaluateDepth(input *facts.DepthFacts) depthEvaluatorResponse {
	assessments := make([]facts.BoundaryAssessment, len(input.Boundaries))
	copy(assessments, input.Boundaries)
	if len(input.Flows) != 0 {
		assessments = depth.AssessFlowBoundaries(input)
	}
	sort.SliceStable(assessments, func(left, right int) bool {
		return assessments[left].Identity.String() < assessments[right].Identity.String()
	})
	scores := make([]metrics.DepthScore, 0, len(assessments))
	for index, assessment := range assessments {
		assessment = depth.ApplyBoundaryRoles(assessment)
		assessments[index] = assessment
		score, scoreErr := metrics.ScoreBoundary(assessment)
		if scoreErr != nil {
			score.State = facts.KnowledgeUnavailable
			score.HKnown = false
			score.Shallow = nil
			score.Reasons = append(score.Reasons, facts.Reason{
				Code: "malformed_facts", Dimension: "boundary", Message: scoreErr.Error(),
			})
		}
		scores = append(scores, score)
	}
	sort.SliceStable(scores, func(left, right int) bool {
		return scores[left].Boundary.String() < scores[right].Boundary.String()
	})

	return depthEvaluatorResponse{SchemaVersion: 1, Assessments: assessments, Scores: scores}
}

func writeDepthEvaluatorResponse(writer io.Writer, response depthEvaluatorResponse) error {
	var encoded depthEvaluatorBuffer
	encoded.limit = depthEvaluatorMaxBytes
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(response); err != nil {
		return fmt.Errorf("encode depth evaluator output: %w", err)
	}
	written, err := writer.Write(encoded.Bytes())
	if err != nil {
		return fmt.Errorf("write depth evaluator output: %w", err)
	}
	if written != encoded.Len() {
		return io.ErrShortWrite
	}
	return nil
}
