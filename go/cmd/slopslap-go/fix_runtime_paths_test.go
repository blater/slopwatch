package main

import "testing"

func TestCanonicalInstallationExecutableRejectsAmbientAndRepositoryPaths(t *testing.T) {
	root, repository, executable := securityExecutableFixture(t)
	assertTrustedExecutable(t, repository, executable)
	assertRejectsRelativeExecutable(t, repository)
	assertRejectsRepositoryExecutable(t, repository)
	assertRejectsWritableExecutable(t, root, repository)
}
