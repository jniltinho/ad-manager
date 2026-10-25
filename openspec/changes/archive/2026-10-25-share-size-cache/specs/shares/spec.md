## ADDED Requirements

### Requirement: Folder sizes measured in the background
The agent SHALL measure the folder size of each share in a background job, never inside an HTTP request: at start
when its last full scan is older than 4 hours, then every 4 hours (with jitter) after the previous full scan ends.
Only one `du` SHALL run at a time, at idle CPU and I/O priority, each share with its own timeout. The result SHALL be
kept in memory and on disk in the agent's state directory.

#### Scenario: Periodic scan
- **WHEN** 4 hours (± jitter) have passed since the previous full scan ended
- **THEN** the agent measures every share again and updates each share's size and measurement time

#### Scenario: The cache survives a restart
- **WHEN** the agent restarts after a full scan that ended less than 4 hours ago
- **THEN** the sizes are served from the file in the state directory and no full scan starts before the jittered
  deadline (4 hours ± 10% after that scan ended)

#### Scenario: A request ending does not cancel the scan
- **WHEN** the browser leaves the page or the panel's request times out while a scan is running
- **THEN** the scan continues and its results are cached as measured

#### Scenario: Agent shutdown during a scan
- **WHEN** the agent stops while a scan is running
- **THEN** the running `du` is killed, the shares measured before it stay in the file, and the next start resumes the
  schedule from the last full scan

#### Scenario: Share created through the panel
- **WHEN** a `share:create` task completes
- **THEN** the agent measures that share right after (after any scan already running), without a full rescan and
  without moving the next full scan

#### Scenario: Share path is a symlink
- **WHEN** a share's path is a symbolic link to a folder
- **THEN** the size measured is the size of the folder it points to, and symlinks inside that folder are not followed

### Requirement: Failed and partial measures
A share whose measure fails or is partial SHALL keep its last complete size and measurement time; the latest attempt's
error, and the partial total when `du` printed one, SHALL be reported for that share; the other shares SHALL be
measured normally.

#### Scenario: A share fails to measure
- **WHEN** a share's folder cannot be measured because it is missing or its measure exceeds the per-share timeout
- **THEN** the share keeps its last complete size and time, the error of the latest attempt is returned for it, and
  the scan goes on with the next share once the killed `du` has exited

#### Scenario: A du that cannot be killed
- **WHEN** a killed `du` does not exit (stuck in uninterruptible I/O on a dead network mount)
- **THEN** no other `du` starts, and the API keeps reporting the scan as running on that share since its start time

#### Scenario: Symlink retargeted
- **WHEN** a share's path is a symlink that now points to another folder and the new measure fails
- **THEN** the share has no size (the old folder's size is not kept) and the error is returned for it

#### Scenario: Partial measure
- **WHEN** `du` reports a total but exits with an error because part of the tree could not be read
- **THEN** that total is returned as the share's partial size with the error, and the last complete size is kept
  unchanged

#### Scenario: Error flood
- **WHEN** `du` writes millions of error lines for one share
- **THEN** the agent keeps only the first error line and its memory does not grow with the output

### Requirement: Cached folder sizes API
`GET /api/sizes` SHALL answer from the cache without listing the shares or measuring: per share, the path and
resolved path, the complete size and when it was measured, the partial size, the last attempt and its error; the
total without double-counting nested shares; whether the total is complete; the scan progress; the next scan time.

#### Scenario: Sizes come from the cache immediately
- **WHEN** the Shares page asks for the sizes of a server whose agent measured its shares earlier
- **THEN** the agent answers at once with the cached sizes and the time each one was measured, without running `du`

#### Scenario: Manual refresh does not block
- **WHEN** support presses Refresh on the Shares page and no scan is running
- **THEN** the call returns immediately with the cached sizes and a full scan marked as running

#### Scenario: Only one scan at a time
- **WHEN** a refresh arrives while a scan is running
- **THEN** no second scan starts and the answer reports the running scan's progress

#### Scenario: Reading does not retry failures
- **WHEN** a share keeps failing and the page reads the sizes repeatedly without Refresh
- **THEN** no scan is started by those reads

#### Scenario: Nested shares in the total
- **WHEN** one share's resolved path is inside, or equal to, another measured share's resolved path
- **THEN** the total counts the outer share only, while each share still shows its own size

#### Scenario: Unmeasured outer share
- **WHEN** an outer share has no size yet and a share inside it has one
- **THEN** the inner share's size is counted in the total and the total is marked incomplete

#### Scenario: Share list changed since the last scan
- **WHEN** the page lists a share with no entry in the cache, or the cache has an entry for a share no longer listed
- **THEN** the page marks the total as incomplete

#### Scenario: Older client
- **WHEN** a client that only knows `sizes` and `at` reads the sizes from a new agent
- **THEN** it gets every share with a complete size in `sizes` and the end of the last full scan in `at`

### Requirement: Shares page shows how fresh the sizes are
The web and desktop Shares pages SHALL show, next to the total, when the last full scan ended and whether the total is
incomplete or includes values older than 6 hours; SHALL mark a share's size as stale, partial or failed with the
reason and the measurement time on hover; and SHALL show the scan progress while a scan runs, without blocking the
page.

#### Scenario: Measurement time
- **WHEN** the last full scan ended 2 hours ago and every share has a complete size from it
- **THEN** the header shows the total with "scanned 2 h ago", the exact time on hover, and no warning

#### Scenario: Incomplete or old total
- **WHEN** a share has no size, a partial size, an error, or a size older than 6 hours
- **THEN** the header shows a warning whose tooltip says that some sizes are missing, partial or old

#### Scenario: Stale or failed size
- **WHEN** a share's size is older than 6 hours, or its latest attempt failed or was partial
- **THEN** the size is shown muted and its tooltip tells the measurement time and the reason

#### Scenario: Only a partial size
- **WHEN** a share has a partial size and no complete one
- **THEN** the cell shows "≥" followed by the partial size

#### Scenario: Scan in progress
- **WHEN** a scan is running on the selected server
- **THEN** the page shows "measuring N/M…", re-reads the cached sizes every few seconds, updates each share as it is
  measured, and stops polling when the scan ends or the page or server changes
