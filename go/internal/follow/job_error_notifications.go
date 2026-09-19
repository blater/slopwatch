package follow

import (
	"fmt"
	"github.com/blater/slopwatch/internal/fix"
)

func showJobErrors(model *Model, jobs []fix.JobPresentation) {
	previous := make(map[fix.JobID]fix.JobIssue)
	for _, job := range model.agents.Jobs {
		if job.Issue != nil {
			previous[job.ID] = *job.Issue
		}
	}
	for _, job := range jobs {
		if job.Issue == nil {
			continue
		}
		if old, ok := previous[job.ID]; ok && old == *job.Issue {
			continue
		}
		showRuntimeError(model, fmt.Errorf("Job %s: %s\n%s", job.ID, job.Issue.Summary, job.Issue.Detail))
	}
}
