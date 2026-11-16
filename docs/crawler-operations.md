# Crawl operations

Start `go run .` and open http://127.0.0.1:8080. Workspace owns definitions, revisions, desired state, and asynchronous operations. Supervisor executes each run. No singleton crawler API or streaming listener remains.

Start creates a run; Resume opens the same run; Restart creates a replacement. Pause drains active work before suspending workers. A pause failure remains visible and can be resumed or stopped. Stop closes workers and storage. Final snapshots are captured after shutdown. Channel queues are transient; durable queues retain pending messages.

Commands serialize per crawl. Idempotency IDs cannot be reused for a different action or record. Interrupted operations are marked failed after restart. Definitions use optimistic revisions; saving a draft does not alter an active run. Apply and restart activates the prepared replacement only after the old runtime closes.

Failed-record inspection/retry uses the active queue owner. Replay retains message identity and staged outputs and resets retry eligibility. Corrupt records stay inspectable. Start or resume a runtime before queue recovery. Queues and output sinks provide at-least-once delivery.

See [workspace configuration](workspace.md), [API routes](api.md), [execution limits](fetch-throughput.md), and [frontier storage](frontier.md).
