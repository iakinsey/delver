# Custom components

The [compiled custom application](../examples/custom/main.go) registers a word-count extractor, an in-memory metadata resource, and a pass-through worker. Build it with:

```sh
go build -o build/custom ./examples/custom
```

Custom resource and worker parameter values are passed through unchanged, including fields named `timeout` or `interval`. Register components before calling `application.Load` or `application.Decode`, because validation uses the registries. Add `word_count` alongside `text` in the example extractor list to use the custom feature. Resource factories receive raw parameter JSON. Worker factories receive parameter JSON plus the application's named resources; custom factories must validate their own parameters and interface references.

## Worker contract

Implement `worker.Worker`:

```go
OnMessage(types.Message) (interface{}, error)
OnComplete()
```

Return nil for no output, a supported built-in message, an explicit `types.Message` for arbitrary message types, or `types.MultiMessage` for a batch. Explicit envelopes preserve their IDs. Otherwise the manager derives stable output IDs from the input and output position. Return errors for retryable failures. A persistent input is acknowledged only after all required outputs are accepted.

A worker instance may serve several execution loops; protect mutable state. `OnComplete` runs once after the manager's loops exit. Workers own resources they construct privately, but must not close injected shared resources.

Implement `worker.ContextWorker.OnMessageContext(context.Context, types.Message)` to receive cancellation during processing. Implement `worker.MessageCommitter.OnCommit(types.Message)` for post-acknowledgment cleanup. Cleanup must be idempotent: a crash after acknowledgment can leave an orphan, so persistent resource implementations should also support reconciliation.

## Extractor contract

Implement `extractors.Extractor` with `Name`, `Requires`, and `Perform(*os.File, message.CompositeAnalysis)`. Register the name, dependency list, and factory through `extractors.Register`. The registration and implementation must agree on name and dependencies.

Implement the optional `extractors.ContextExtractor` interface to receive cancellation. Native dependency stages have configurable deadlines. Legacy calls are tracked until they finish; a shutdown timeout retains ownership and prevents a replacement runtime. Worker and extractor panics become processing errors.

Every enabled dependency must be configured explicitly. Factories are instantiated once per composite worker. Independent extractors run concurrently and receive independent feature-map snapshots; treat nested feature values as read-only. Use `analysis.Load` to convert a feature into your desired Go type. Return a JSON-serializable result; an error is recorded as an extractor failure.

## Queues and persistence

A queue implements `queue.Queue`: prepare/start/stop, receive channel, publication, transaction completion, and length. `types.Message.ID` identifies application work; `DeliveryID` is internal to the queue and must not become a new application identity. Successful transaction completion removes or completes an input; failed completion makes it retryable or dead-letters it according to the queue's policy.

Optional interfaces extend persistence and cancellation:

- `PutContext(context.Context, types.Message, int) error` allows blocked publication to stop with the manager.
- `queue.OutputStore` loads/saves an input's serialized output batch for replay after partial publication.
- `ReferencedObjects() ([]types.UUID, error)` participates in startup body reconciliation.
- `Close() error` releases resource ownership at application shutdown.

Custom queues without `OutputStore` may recompute outputs on retries. Channel queues intentionally have no durable transaction semantics. Make these guarantees explicit for any new backend.
