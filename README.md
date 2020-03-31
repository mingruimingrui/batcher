`batcher` is a package for batching individual requests into batch
requests.

For some live services, batching is necessary to maximize throughput.
In particular, services that require disk I/O or leverages GPUs would
require batching as the overhead cost associated to each request is long and
near constant.
The typical way that batching is achieved is with the help of a queue system.

`batcher` takes this design pattern and formalizes it into a template for
developers to conveniently incorporate batching to their live services.

- [Installation and Docs](#installation-and-docs)
- [Usage](#usage)
- [BatchingConfig](#batchingconfig)


# Installation and Docs

Within of a module, this package can be installed with `go get`.

```
go get github.com/mingruimingrui/batcher
```

Auto-generated documentation is available at
https://pkg.go.dev/github.com/mingruimingrui/batcher.


# Usage

Usage of this library typically begins with creation of a new `RequestBatcher`
variable.
Typically this is done on the global scope.

```golang
var (
    requestBatcher *batcher.RequestBatcher
    ...
)

...

func main() {
    requestBatcher = batcher.RequestBatcher(
        context.Background(),
        &batcher.BatchingConfig{
            MaxBatchSize: ...,
            BatchTimeout: ...,

            // SendF is a function that handles a batch of requests
            SendF: func(body *[]interface{}) (*[]interface{}, error) {
                ...
            }
        },
    )

    ...
}
```

To submit a request, simply use the `SendRequestWithTimeout` method

```golang
resp, err := requestBatcher.SendRequestWithTimeout(&someRequestBody, someTimeoutDuration)
```

When multiple requests are send together from multiple goroutines,
the `RequestBatcher` would group those requests together into a single batch
so `SendF` would be called minimally.


# BatchingConfig

`batcher.BatchingConfig` controls batching behavior and accepts the
following parameters.

- **`MaxBatchSize`** `{int}` <br/>
  The maximum number of requests per batch

- **`BatchTimeout`** `{time.Duration}` <br/>
  The maximum wait time before batch is sent

- **`SendF`** `{func(body *[]interface{}) (*[]interface{}, error)}` <br/>
  A function for the user to define how to handle a batch
