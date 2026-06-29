# Built-in Tools

The six capabilities the agent uses to do real work on a project: read, write,
edit, run, search by content, find by name. Each returns a readable result and
each failure mode is a clean error result, never a crash.

## ADDED Requirements

### Requirement: Read a file, optionally a window

The read-file tool SHALL return a file's contents line-referenced so the model can
cite lines, and SHALL support an optional line offset and limit returning only
that window. It SHALL return a clear error — dumping no raw bytes — for a path
that is missing, a directory, unreadable, binary, or an offset past end of file.

#### Scenario: Whole-file read is line-referenced

- **WHEN** a file is read with no window
- **THEN** its contents are returned line-referenced so the model can cite lines

#### Scenario: Windowed read returns only the window

- **WHEN** a file is read with a line offset and limit
- **THEN** only that window of lines is returned

#### Scenario: Bad read targets return a clean error

- **WHEN** the path is missing, a directory, unreadable, binary, or the offset is past end of file
- **THEN** a clear error is returned and no raw bytes are dumped

### Requirement: Create or replace a file

The write-file tool SHALL create or replace a file by path, optionally create
missing parent folders, and return a concise confirmation that does not echo the
content.

#### Scenario: Write with parent creation scaffolds folders

- **WHEN** a file is written with parent-creation enabled and missing parent folders
- **THEN** the missing folders are created and the file is written

#### Scenario: Write replaces existing contents

- **WHEN** a file is written to an existing path
- **THEN** its contents are replaced and the result is a concise confirmation that does not echo the content

### Requirement: Surgical, unambiguous edits

The precise-edit tool SHALL replace a snippet that appears exactly once in place
and write the file. A snippet appearing more than once SHALL be rejected as
ambiguous, reporting the match count, with the file left byte-for-byte unchanged,
unless replace-all is explicitly opted into — in which case every occurrence SHALL
be replaced. A missing target or a no-op (target equals replacement) SHALL be
rejected with a clear reason and the file left unchanged.

#### Scenario: Unique snippet is replaced

- **WHEN** the target snippet appears exactly once
- **THEN** it is replaced in place and the file is written

#### Scenario: Ambiguous snippet is rejected unchanged

- **WHEN** the target snippet appears more than once and replace-all is not opted into
- **THEN** the edit is rejected as ambiguous, the match count is reported, and the file is left byte-for-byte unchanged

#### Scenario: Replace-all replaces every occurrence

- **WHEN** the target snippet appears more than once and replace-all is explicitly opted into
- **THEN** every occurrence is replaced

#### Scenario: Missing target or no-op is rejected unchanged

- **WHEN** the target is not present in the file, or the replacement is identical to the target
- **THEN** the edit is rejected with a clear reason and the file is left unchanged

### Requirement: Run commands, see everything

The shell tool SHALL return a command's combined output and exit code, report a
non-zero exit as an error result carrying the captured output, and enforce a time
budget. A command exceeding its budget SHALL be killed and reported as timed out
with any partial output, leaving no orphan process. A cancelled command SHALL
return a cancellation error result with its child process stopped.

#### Scenario: Combined output and exit code returned

- **WHEN** a command runs to completion
- **THEN** its combined output and exit code are returned

#### Scenario: Non-zero exit is an error result

- **WHEN** a command exits non-zero
- **THEN** an error result with the captured output is returned, not a crash

#### Scenario: Timeout kills the command with partial output

- **WHEN** a command exceeds its time budget
- **THEN** it is killed, the result notes the timeout with any partial output, and no orphan process is left behind

#### Scenario: Cancellation stops the child process

- **WHEN** the surrounding work is cancelled mid-command
- **THEN** the call returns a cancellation error result, the child process is stopped, and nothing leaks

### Requirement: Find text across the project

The content-search tool SHALL return results as `file:line:match` lines, scopable
by path, by file glob, and by case sensitivity. A search matching nothing SHALL
return a successful "no matches" result, not an error. When the backend search
binary is unavailable, the tool SHALL degrade to a clear, actionable error, never
a crash.

#### Scenario: Matches return located results

- **WHEN** a search over a directory containing a known string runs
- **THEN** results are returned as `file:line:match` lines, scopable by path, file glob, and case sensitivity

#### Scenario: No matches is a successful answer

- **WHEN** a content search matches nothing
- **THEN** a successful "no matches" result is returned, not an error

#### Scenario: Missing backend binary degrades cleanly

- **WHEN** the backend content-search binary is unavailable
- **THEN** a clear, actionable error is returned rather than a crash

### Requirement: List files by name pattern

The file-pattern-search tool SHALL return matching paths in stable order with
common noise directories (such as version-control internals) skipped, treat no
matches as a successful "no files matched" result, and return an error for a bad
root.

#### Scenario: Matching paths returned in stable order

- **WHEN** a glob is searched under a valid root
- **THEN** matching paths are returned in stable order with common noise directories skipped

#### Scenario: No matches is a successful answer

- **WHEN** a glob matches nothing
- **THEN** a successful "no files matched" result is returned

#### Scenario: Bad root returns an error

- **WHEN** the search root is invalid
- **THEN** an error is returned
