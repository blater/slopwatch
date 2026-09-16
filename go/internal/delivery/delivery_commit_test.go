package delivery

import (
	"context"
	"testing"
)

func TestGitServiceCreatesExactAbsentLocalAndRemoteRefs(t *testing.T) {
	fixture := newPublicationFixture(t)
	result, err := fixture.service.PublishCommit(context.Background(), fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	fixture.assertPublished(t, result)
	fixture.assertRejectsReuse(t)
}
