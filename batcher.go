/*
Package batcher is a library for batching requests.

This library encapsualtes the process of grouping requests into batches
for batch processing in an asynchronous manner.

This implementation is adapted from the Google batch API
https://github.com/terraform-providers/terraform-provider-google/blob/master/google/batcher.go

However there are a number of notable differences
- Usage assumes 1 batcher for 1 API (instead of 1 batcher for multiple APIs)
- Clients should only receive their own response, and not the batch response
  their request is sent with
- Config conventions follow the framework as defined in
  https://github.com/tensorflow/serving/tree/master/tensorflow_serving/batching#batch-scheduling-parameters-and-tuning
*/

package batcher

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

/*
RequestBatcher handles receiving of new requests, and all the background
asynchronous tasks to batch and send batch.

A new RequestBatcher should be created using the NewRequestBatcher function.

Expected usage pattern of the RequestBatcher involves declaring a
RequestBatcher on global scope and calling SendRequestWithTimeout from
multiple goroutines.
*/
type RequestBatcher struct {
	sync.Mutex

	*BatchingConfig
	running   bool
	parentCtx context.Context
	curBatch  *startedBatch
}

/*
BatchingConfig determines how batching is done in a RequestBatcher.
*/
type BatchingConfig struct {
	// Maximum request size of each batch.
	MaxBatchSize int

	// Maximum wait time before batch should be executed.
	BatchTimeout time.Duration

	// User defined SendF for sending a batch request.
	// See SendFunc for type definition of this function.
	SendF SendFunc
}

/*
SendFunc is a function type for sending a batch of requests.
A batch of requests is a slice of inputs to SendRequestWithTimeout.
*/
type SendFunc func(body *[]interface{}) (*[]interface{}, error)

// startedBatch refers to a batch awaiting for more requests to come in
// before having SendFunc applied to it's content
type startedBatch struct {
	// Combined batch request
	body []interface{}

	// subscribers is a registry of the requests (batchSubscriber)
	// combined to make this batch
	subscribers []batchSubscriber

	// timer for keeping track of BatchTimeout
	timer *time.Timer
}

// singleResponse represents a single response received from SendF
type singleResponse struct {
	body interface{}
	err  error
}

// batchSubscriber contains the response queue to awaits for a singleResponse
type batchSubscriber struct {
	// singleRequestBody is the original request this subscriber represents
	singleRequestBody interface{}

	// respCh is the channel created to communicate the result to a waiting
	// goroutine
	respCh chan *singleResponse
}

/*
NewRequestBatcher creates a new RequestBatcher
from a Context and a BatchingConfig.

In the typical usage pattern, a RequestBatcher should always be alive so it
is safe and recommended to use the background context.
*/
func NewRequestBatcher(
	ctx context.Context,
	config *BatchingConfig,
) *RequestBatcher {
	batcher := &RequestBatcher{
		BatchingConfig: config,
		parentCtx:      ctx,
		running:        true,
	}

	if batcher.SendF == nil {
		log.Fatal("Expecting SendF")
	}

	go func(b *RequestBatcher) {
		<-b.parentCtx.Done()
		log.Printf("Parent context cancelled")
		b.stop()
	}(batcher)

	return batcher
}

// stop would safely releases all batcher allocated resources
func (b *RequestBatcher) stop() {
	b.Lock()
	defer b.Unlock()
	log.Println("Stopping batcher")

	b.running = false
	if b.curBatch != nil {
		b.curBatch.timer.Stop()
		for i := len(b.curBatch.subscribers) - 1; i >= 0; i-- {
			close(b.curBatch.subscribers[i].respCh)
		}
	}
	log.Println("Batcher stopped")
}

/*
SendRequestWithTimeout is a method to make a single request.
It manages registering the request into the batcher,
and waiting on the response.

Arguments:
	newRequestBody {*interface{}} -- A request body. SendF will expect
		a slice of objects like newRequestBody.

Returns:
	interface{} -- A response body. SendF's output is expected to be a slice
		of objects like this.
	error       -- Error
*/
func (b *RequestBatcher) SendRequestWithTimeout(
	newRequestBody *interface{},
	timeout time.Duration,
) (interface{}, error) {
	// Check that request is valid
	if newRequestBody == nil {
		return nil, fmt.Errorf("Received `nil` request")
	}

	if timeout <= b.BatchTimeout {
		errmsg := fmt.Sprintf(
			"Timeout period should be longer than batch timout, %v",
			b.BatchTimeout,
		)
		return nil, fmt.Errorf(errmsg)
	}

	respCh, err := b.registerRequest(newRequestBody)
	if err != nil {
		log.Printf("[ERROR] Failed to register request: %v", err)
		return nil, fmt.Errorf("Failed to register request")
	}

	ctx, cancel := context.WithTimeout(b.parentCtx, timeout)
	defer cancel()

	select {
	case resp := <-respCh:
		if resp.err != nil {
			log.Printf("[ERROR] Failed to process request: %v", resp.err)
			return nil, resp.err
		}
		return resp.body, nil

	case <-ctx.Done():
		return nil, fmt.Errorf("Request timeout after %v", timeout)
	}
}

// registerRequest safely determines if new request should be
// added to existing batch or to a new batch
func (b *RequestBatcher) registerRequest(
	newRequestBody *interface{},
) (<-chan *singleResponse, error) {
	respCh := make(chan *singleResponse, 1)
	sub := batchSubscriber{
		singleRequestBody: *newRequestBody,
		respCh:            respCh,
	}

	b.Lock()
	defer b.Unlock()

	if b.curBatch != nil {
		// Check if new request can be appended to curBatch
		if len(b.curBatch.body) < b.MaxBatchSize {
			// Append request to current batch
			b.curBatch.body = append(b.curBatch.body, *newRequestBody)
			b.curBatch.subscribers = append(b.curBatch.subscribers, sub)

			// Check if current batch is full
			if len(b.curBatch.body) >= b.MaxBatchSize {
				// Send current batch
				b.curBatch.timer.Stop()
				b.sendCurBatch()
			}

			return respCh, nil
		}

		// Send current batch
		b.curBatch.timer.Stop()
		b.sendCurBatch()
	}

	// Create new batch from request
	b.curBatch = &startedBatch{
		body:        []interface{}{*newRequestBody},
		subscribers: []batchSubscriber{sub},
	}

	// Start a timer to send request after batch timeout
	b.curBatch.timer = time.AfterFunc(b.BatchTimeout, b.sendCurBatchWithSafety)

	return respCh, nil
}

// sendCurBatch pops curBatch and sends it without mutex
func (b *RequestBatcher) sendCurBatch() {
	// Acquire batch
	batch := b.curBatch
	b.curBatch = nil

	if batch != nil {
		go func() {
			b.send(batch)
		}()
	}
}

// sendCurBatchWithSafety pops curBatch and sends it with mutex
func (b *RequestBatcher) sendCurBatchWithSafety() {
	// Acquire batch
	b.Lock()
	batch := b.curBatch
	b.curBatch = nil
	b.Unlock()

	if batch != nil {
		go func() {
			b.send(batch)
		}()
	}
}

// send calls SendF on a startedBatch
func (b *RequestBatcher) send(batch *startedBatch) {

	// Attempt to apply SendF
	batchResp, err := b.SendF(&batch.body)
	if err != nil {
		for i := len(batch.subscribers) - 1; i >= 0; i-- {
			batch.subscribers[i].respCh <- &singleResponse{
				body: nil,
				err:  err,
			}
			close(batch.subscribers[i].respCh)
		}
		return
	}

	// Raise error if number of entries mismatch
	if len(*batchResp) != len(batch.body) {
		log.Printf("[ERROR] SendF returned different number of entries.")
		for i := len(batch.subscribers) - 1; i >= 0; i-- {
			batch.subscribers[i].respCh <- &singleResponse{
				body: nil,
				err:  fmt.Errorf("API error"),
			}
			close(batch.subscribers[i].respCh)
		}
		return
	}

	// On success, place response into subscribed response queues.
	for i := len(batch.subscribers) - 1; i >= 0; i-- {
		batch.subscribers[i].respCh <- &singleResponse{
			body: (*batchResp)[i],
			err:  nil,
		}
		close(batch.subscribers[i].respCh)
	}
}
