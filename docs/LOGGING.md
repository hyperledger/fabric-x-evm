# Logging

This project uses [`github.com/hyperledger/fabric-lib-go/common/flogging`](https://github.com/hyperledger/fabric-lib-go) for structured, levelled logging.

## Declaring a logger

Declare one package-level logger per file (or per logical component) using `MustGetLogger`:

```go
var logger = flogging.MustGetLogger("gateway.api.eth")
```

If a file needs a name that avoids colliding with `logger` elsewhere in the same package, use a descriptive prefix:

```go
var apiLogger  = flogging.MustGetLogger("gateway.api")
var batchLogger = flogging.MustGetLogger("gateway.core.batch_submitter")
```

## Logger naming convention

Logger names mirror the Go package path under `gateway/`, using dots as separators:

| Package path                  | Logger name                       |
|-------------------------------|-----------------------------------|
| `gateway/api`                 | `gateway.api`                     |
| `gateway/api/eth.go`          | `gateway.api.eth`                 |
| `gateway/api/filters`         | `gateway.api.filters`             |
| `gateway/core`                | `gateway.core`                    |
| `gateway/core/batch_submitter`| `gateway.core.batch_submitter`    |
| `gateway/storage/…`           | `gateway.storage.<component>`     |

Short one-off loggers (e.g. in tests or CLI entry points) may use a flat name like `"testnode"`.

## Severity levels

| Level     | When to use |
|-----------|-------------|
| `Debugf`  | Entry/exit of API methods, parameter values, return values — verbose detail useful only during development or troubleshooting. Off by default in production. |
| `Infof`   | Normal operational milestones that are worth recording even in production: server started, configuration loaded, significant state changes. |
| `Warnf`   | Errors returned to a caller — bad input, not-found, validation failures, EVM reverts. The server handled it correctly, but it is worth surfacing at a glance. |
| `Errorf`  | Unexpected failures the server itself cannot recover from: background goroutine errors, data corruption, unrecoverable state. |

### API handler pattern

Every externally-facing handler follows this pattern:

```go
func (api *EthAPI) GetFoo(ctx context.Context, ...) (..., error) {
    logger.Debugf("EthAPI.GetFoo() called with ...")   // entry
    result, err := api.b.Foo(ctx, ...)
    if err != nil {
        logger.Warnf("EthAPI.GetFoo() returning error: %v", err)  // error path
        return nil, err
    }
    logger.Debugf("EthAPI.GetFoo() returning: ...")    // success path
    return result, nil
}
```

Key rules:
- Log entry on every public method (`called`).
- Log every error return with `Warnf` and the error value.
- Log the successful return value with `Debugf`.
- No log level higher than `Warn` inside API handlers; reserve `Error` for the core/background layer.

## Controlling log levels at runtime

### Environment variable

Set `FABRIC_LOGGING_SPEC` before starting the process:

```bash
# Everything at debug
FABRIC_LOGGING_SPEC=debug ./fxevm start --config gateway.yaml

# Only the API layer at debug; everything else at info
FABRIC_LOGGING_SPEC="gateway.api=debug:info" ./fxevm start --config gateway.yaml

# Just the eth handler
FABRIC_LOGGING_SPEC="gateway.api.eth=debug:info" ./fxevm start --config gateway.yaml
```

The spec syntax is `[logger=level:]*[defaultLevel]` — colon-separated entries, with an optional bare default level at the end.

### Config file

The same spec string goes under `logging.spec` in the process yaml:

```yaml
logging:
  spec: "gateway.api.eth=debug:info"
```

### Programmatically (tests / runtime)

Call `flogging.ActivateSpec` at any point — useful in tests or for live adjustment:

```go
// Turn on debug for the eth API only
flogging.ActivateSpec("gateway.api.eth=debug:info")

// ... exercise the code ...

// Reset to info
flogging.ActivateSpec("info")
```

This takes effect immediately for all loggers already created via `MustGetLogger`.

### Useful spec examples

| Goal | Spec |
|---|---|
| All debug | `debug` |
| All info, API at debug | `gateway.api=debug:info` |
| Only `gateway.api.eth` at debug | `gateway.api.eth=debug:info` |
| Silence everything below warnings | `warning` |
| Per-component mix | `gateway.core=debug:gateway.api=warn:info` |
