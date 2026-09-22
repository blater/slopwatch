# macOS watcher implementation record

The approved scope is [macos-watcher-plan.md](macos-watcher-plan.md).

The macOS backend now uses `github.com/fsnotify/fsevents v0.2.0`, with recursive
subscriptions that do not open individual source files. A dedicated receiver
drains the library's synchronous callback/channel handoff into the existing
pending-event collector. Linux and Windows retain fsnotify. The common Makefile
enables cgo for the application build and tests on macOS to link FSEvents.

Platform-neutral intake has no pending-path limit. Repeated operations combine
flags by path, and entries are removed as they are delivered. A separate channel
retains at most 16 ordinary errors; exhausted error delivery reports notification loss
without requesting discovery. Intake starts before inventory registration and
continues during an explicit rescan. Exclusion filtering uses loaded rule state
only, without reading files or caching policy for unknown paths.

The explicit rescan serializes inventory registration and the same startup
analysis command with ordinary event application. It reloads current ignore
settings, preserves native collection, and retains the existing UI and results
until analysis completes. A loss generation distinguishes notifications lost
during a scan from earlier loss that a successful scan can clear.

Verification passed through the common `make build` path, including packaged
distribution smoke tests. Native FSEvents tests require access to the system
notification service, so validation ran outside the execution sandbox. The focused macOS
regression uses 1,024 files under a 256-descriptor process limit and checks native
subscription count, descriptor growth, edit delivery and cleanup. This is not an
enterprise-readiness claim. Existing follow tests, including directory replacement,
passed along with the notification and manual-rescan tests.
