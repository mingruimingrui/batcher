# HTTP Batching Service

This is a service that batches incoming individual requests before sending
them to a backend service for batch processing.
The typical usage pattern is to use this service as a reverse proxy.

This service expects the following
- Client to submit POST request with jsonifiable request objects.
- The backend service to expect a list of request objects received from
  multiple clients.
- The backend service to return a list of jsonifiable response objects.
  Also the order of this list of response objects must correspond with
  the order of the request objects.


# Example Usage Patterns

Explaining the expected inputs and outputs can best be done using and example.

Suppose you are deploying a service that translates sentences from
english to german and requires the following usage pattern:

```json
// Example client request and response

// Request Body
{
    "texts": ["Hello World!", "One two three"]
}

// Response body
{
    "texts": ["Hallo Welt!", "Eins zwei drei"]
}

```

The service would group multiple request objects into a list and send it to
a backend service.
It would then expect to receive a corresponding list of response objects from
the backend service.

```json
// Example backend request and response

// Request body
[
    {"texts": ["Hello World!", "One two three"]},
    {"texts": ["The quick brown fox jumped over the lazy dog."]}
]

// Response body
[
    {"texts": ["Hallo Welt!", "Eins zwei drei"]},
    {"texts": ["Der schnelle braune Fuchs sprang über den faulen Hund."]}
]
```


# Command Line / Environmental Options

The behavior of the batching service can be defined using command line flags.
Alternatively, the `BATCHER_CMD_ARGS` environmental variable can be used.

- **`-backend`** `[string] (required)` <br>
    The address to the backend batch processing service.

- **`-bind`** `[string] (default: 0.0.0.0:8000)` <br>
    The address to bind this service to.

- **`-max-batch-size`** `[int] (default: 32)` <br>
    The maximum size of each batch.

- **`-batch-timeout-millis`** `[int] (default: 30)` <br>
    The maximum wait time before a batch is sent to backend service.

- **`-idle-timeout-millis`** `[int] (default: 10000)` <br>
    The timeout period for each individual request.

- **`-max-concurrent-conns`** `[int] (default: 1024)` <br>
    The maximum number of clients connected to this service at any given time.

- **`-version`** `[int] (default: 1024)` <br>
    The maximum number of clients connected to this service at any given time.

If the `BATCHER_CMD_ARGS` environmental variable is set, additional flags
will be taken from there.
Here's an example of the expected format of `BATCHER_CMD_ARGS`.

```bash
BATCHER_CMD_ARGS='
-backend http://batch-translation-service:5498
-batch-timeout-millis 50
'
```


# Response

| Status Code | Message | Description |
| - | - | - |
| 200 |  | Response will depend on backend service |
| 400| `Expecting request body in JSON format` | Malformed request |
| 400 | `[ERROR] Failed to process request` | Error with backend service |
| 502 | `Request timeout` | Backed on `idleTimeout` |


# Limitations and Caveats

There are a number of things to be mindful of when deploying highly optimized
production workflows.

- This service would not rate limit the transactions received or sent
  to the backend service.
- This service uses JSON payloads which is not optimal for both
  performance and security.
- This service also does not perform health check on the backend process.
