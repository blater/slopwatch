package delivery

import "github.com/blater/slopwatch/internal/fix"

const testDeliveryOutputBytes = int64(4 << 20)

var (
	pushNewBranch        = fix.DeliveryPlan{Workspace: fix.WorkspaceWorktree, Git: fix.GitCommitNewBranch, Publish: fix.PublishPush}
	pullRequestNewBranch = fix.DeliveryPlan{Workspace: fix.WorkspaceWorktree, Git: fix.GitCommitNewBranch, Publish: fix.PublishPullRequest}
)
