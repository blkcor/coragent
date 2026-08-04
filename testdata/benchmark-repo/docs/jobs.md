# Jobs

The `submit` command accepts a request ID and name. Mercury validates the
request, rejects an already-seen request ID, creates a job ID in the service,
and persists the queued job. `inspect` renders the job ID, string status, and
name as stable text output.

## JobRecord construction paths

This section is intentionally incomplete until recovery task R01 updates it.
