package follow

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/blater/slopwatch/internal/candidate"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixapp"
)

func loadJobReader(ctx context.Context, service FixService, kind OverlayKind, jobID fix.JobID, path fix.RepoPath, cursor fixapp.LogCursor, increment bool) jobReaderMsg {
	switch kind {
	case OverlayJobLog:
		return loadJobLog(ctx, service, jobID, cursor, increment)
	case OverlayJobDiff:
		return loadJobDiff(ctx, service, jobID, path)
	case OverlayCandidateSource:
		return loadCandidateSource(ctx, service, jobID, path)
	default:
		return jobReaderMsg{}
	}
}

func loadJobLog(ctx context.Context, service FixService, jobID fix.JobID, cursor fixapp.LogCursor, increment bool) jobReaderMsg {
	page, err := loadTranscript(ctx, service, jobID, cursor)
	if err != nil {
		return jobReaderMsg{err: err}
	}
	lines := make([]string, 0, len(page.Entries)+1)
	for _, entry := range page.Entries {
		lines = append(lines, transcriptEntryLine(entry))
	}
	if !increment {
		if job, ok := service.Job(jobID); ok && agentJobFinished(job.Phase) {
			summary := strings.Join(nonemptyStrings(agentPhaseText(job), agentActivityText(job)), " · ")
			lines = append(lines, fmt.Sprintf("%s  %-16s %s", job.UpdatedAt.Format("15:04:05"), "result", summary))
		}
	}
	return jobReaderMsg{lines: lines, logCursor: page.Next}
}

func transcriptEntryLine(entry fixapp.LogEntry) string {
	if entry.Text != "" {
		return jobLogDisplayLine(cleanAgentText(entry.Text))
	}
	return fmt.Sprintf("%s  %-16s %s", entry.At.Format("15:04:05"), entry.Kind, cleanAgentText(entry.Summary))
}

func loadJobDiff(ctx context.Context, service FixService, jobID fix.JobID, path fix.RepoPath) jobReaderMsg {
	lines := []string{}
	fingerprint, offset := "", 0
	for {
		page, err := service.Diff(ctx, jobID, fixapp.DiffRequest{Offset: offset, Limit: 200})
		if err != nil {
			return jobReaderMsg{lines: lines, err: err}
		}
		if fingerprint != "" && page.Fingerprint != fingerprint {
			return jobReaderMsg{lines: lines, truncated: true, err: errors.New("candidate diff changed while loading; press r to refresh")}
		}
		if fingerprint == "" {
			fingerprint = page.Fingerprint
		}
		for _, file := range page.Files {
			if path == "" || file.Path == path {
				lines = append(lines, diffFileLine(file))
			}
		}
		if page.Complete {
			return jobReaderMsg{lines: lines}
		}
		if page.NextOffset <= offset {
			return jobReaderMsg{lines: lines, truncated: true, err: errors.New("diff pagination did not advance")}
		}
		offset = page.NextOffset
	}
}

func diffFileLine(file candidate.DiffFile) string {
	label := fmt.Sprintf("%-10s %s", cleanAgentText(file.Status), file.Path)
	if file.Additions > 0 || file.Deletions > 0 {
		label += fmt.Sprintf("  +%d -%d", file.Additions, file.Deletions)
	}
	if file.Previous != "" {
		label += "  from " + file.Previous.String()
	}
	if file.Binary {
		label += "  [binary]"
	}
	return label
}

func loadCandidateSource(ctx context.Context, service FixService, jobID fix.JobID, path fix.RepoPath) jobReaderMsg {
	file, err := service.CandidateFile(ctx, jobID, path)
	if err != nil {
		return jobReaderMsg{err: err}
	}
	lines := strings.Split(strings.ReplaceAll(string(file.Contents), "\r\n", "\n"), "\n")
	return jobReaderMsg{lines: lines, truncated: file.Truncated}
}
