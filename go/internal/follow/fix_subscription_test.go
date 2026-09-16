package follow

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixapp"
	tea "github.com/charmbracelet/bubbletea"
)

func TestFixSubscriptionRetryUsesLatestGenerationAndOrdering(t *testing.T) {
	service := &trackingSubscriptionService{fakeFixService: &fakeFixService{}}
	oldSubscription := &trackingSubscription{name: "old", events: &service.events}
	model := fixTestModel(service, 80, 24)
	model.fixUpdates.subscription = oldSubscription
	model.fixNotice = "keep until retry"
	model.agents.Jobs = []fix.JobPresentation{{ID: "old", Phase: fix.PhaseRunning}}

	firstGeneration, latestGeneration := subscriptionFailureGenerations(t, &model)
	assertStaleRetryIgnored(t, &model, service, firstGeneration)

	newJobs := []fix.JobPresentation{{ID: "new", Phase: fix.PhaseCompleted}}
	service.snapshots = []fixapp.JobListSnapshot{{Jobs: newJobs}, {Jobs: newJobs}}
	command := model.retryFixSubscription(fixRetrySubscriptionMsg{generation: latestGeneration})
	assertSubscriptionRestored(t, &model, service, command)
	assertSubscriptionWait(t, command, service)
}

func subscriptionFailureGenerations(t *testing.T, model *Model) (uint64, uint64) {
	t.Helper()
	if model.fixUpdateError(errors.New("first")) == nil {
		t.Fatal("first subscription failure did not schedule recovery")
	}
	firstGeneration := model.fixUpdates.retryGeneration
	if model.fixUpdateError(errors.New("second")) == nil {
		t.Fatal("second subscription failure did not schedule recovery")
	}
	latestGeneration := model.fixUpdates.retryGeneration
	if firstGeneration == latestGeneration {
		t.Fatalf("retry generation did not advance: first=%d latest=%d", firstGeneration, latestGeneration)
	}
	return firstGeneration, latestGeneration
}

func assertStaleRetryIgnored(t *testing.T, model *Model, service *trackingSubscriptionService, generation uint64) {
	t.Helper()
	notice := model.fixNotice
	if command := model.retryFixSubscription(fixRetrySubscriptionMsg{generation: generation}); command != nil {
		t.Fatal("stale retry returned a command")
	}
	if len(service.events) != 0 || model.fixNotice != notice || !model.fixUpdates.stale || len(model.agents.Jobs) != 1 || model.agents.Jobs[0].ID != "old" {
		t.Fatalf("stale retry changed state: events=%v notice=%q stale=%t jobs=%v", service.events, model.fixNotice, model.fixUpdates.stale, model.agents.Jobs)
	}
}

func assertSubscriptionRestored(t *testing.T, model *Model, service *trackingSubscriptionService, command tea.Cmd) {
	t.Helper()
	if command == nil {
		t.Fatal("latest retry did not return a wait command")
	}
	want := []string{"close:old", "subscribe:sub-1", "jobs"}
	if !slices.Equal(service.events, want) {
		t.Fatalf("retry ordering=%v, want %v", service.events, want)
	}
	if model.fixUpdates.stale || model.fixNotice != "Fix updates restored" || len(model.agents.Jobs) != 1 || model.agents.Jobs[0].ID != "new" {
		t.Fatalf("latest retry did not restore state: stale=%t notice=%q jobs=%v", model.fixUpdates.stale, model.fixNotice, model.agents.Jobs)
	}
}

func assertSubscriptionWait(t *testing.T, command tea.Cmd, service *trackingSubscriptionService) {
	t.Helper()
	message, ok := command().(fixJobsMsg)
	if !ok || message.err != nil || len(message.jobs) != 1 || message.jobs[0].ID != "new" {
		t.Fatalf("wait command message=%#v", message)
	}
	want := []string{"close:old", "subscribe:sub-1", "jobs", "wait:sub-1", "jobs"}
	if !slices.Equal(service.events, want) {
		t.Fatalf("wait ordering=%v, want %v", service.events, want)
	}
}

type trackingSubscriptionService struct {
	*fakeFixService
	events    []string
	snapshots []fixapp.JobListSnapshot
	count     int
}

func (service *trackingSubscriptionService) Subscribe() fixapp.Subscription {
	service.count++
	name := "sub-" + strconv.Itoa(service.count)
	service.events = append(service.events, "subscribe:"+name)
	return &trackingSubscription{name: name, events: &service.events}
}

func (service *trackingSubscriptionService) Jobs(fixapp.JobFilter) fixapp.JobListSnapshot {
	service.events = append(service.events, "jobs")
	if len(service.snapshots) == 0 {
		return service.fakeFixService.jobs
	}
	snapshot := service.snapshots[0]
	service.snapshots = service.snapshots[1:]
	return snapshot
}

type trackingSubscription struct {
	name   string
	events *[]string
}

func (subscription *trackingSubscription) Wait(context.Context) error {
	*subscription.events = append(*subscription.events, "wait:"+subscription.name)
	return nil
}

func (subscription *trackingSubscription) Close() error {
	*subscription.events = append(*subscription.events, "close:"+subscription.name)
	return nil
}
