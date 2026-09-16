package fixapp

import (
	"time"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/candidate"
	"github.com/blater/slopwatch/internal/delivery"
	"github.com/blater/slopwatch/internal/fixanalysis"
	"github.com/blater/slopwatch/internal/jobstore"
	"github.com/blater/slopwatch/internal/publisher"
)

type Dependencies struct {
	Config            appconfig.Resolver
	Analysis          fixanalysis.Service
	Candidates        candidate.Service
	ScopePlanner      candidate.ScopePlanner
	Agents            *agent.Registry
	Store             jobstore.Store
	Delivery          delivery.SagaService
	DeliveryPreflight delivery.PreflightService
	Publisher         publisher.Service
}

type Options struct {
	MaxAgents    int
	MaxVerifiers int
	JobIndexPath string
	Clock        func() time.Time
}
