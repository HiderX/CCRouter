module ccrouter

go 1.27.3

require (
	github.com/fsnotify/fsevents v0.2.0
	github.com/fsnotify/fsnotify v1.9.0
	github.com/mattn/go-sqlite3 v1.14.27
	github.com/progrium/darwinkit v0.5.1-0.20240715194340-61b9e31a12fa
	golang.org/x/sys v0.13.0
	golang.org/x/text v0.25.0
)

// Pinned DarwinKit with the reviewed compatibility fixes documented in
// third_party/darwinkit/CCROUTER_PATCHES.md.
replace github.com/progrium/darwinkit => ./third_party/darwinkit
