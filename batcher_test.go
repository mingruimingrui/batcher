package batcher

import (
	"context"
	"encoding/json"
	"math/rand"
	"testing"
	"time"
)

var (
	ctx              context.Context
	delayedBatcher   *RequestBatcher
	immediateBatcher *RequestBatcher

	idleTimeout         time.Duration
	nonEmptyRequestBody interface{}
)

func dummySendF(body *[]interface{}) (*[]interface{}, error) {
	return body, nil
}

func init() {
	ctx = context.Background()

	delayedBatcher = NewRequestBatcher(ctx, &BatchingConfig{
		MaxBatchSize: 32,
		BatchTimeout: time.Millisecond,
		SendF:        dummySendF,
	})

	immediateBatcher = NewRequestBatcher(ctx, &BatchingConfig{
		MaxBatchSize: 32,
		BatchTimeout: time.Duration(0),
		SendF:        dummySendF,
	})

	idleTimeout = time.Minute
	nonEmptyRequestBody = "This is some string"
}

// TestValidRequestBody checks if RequestBatcher accepts valid requests
// In this test, all test cases are guaranteed to be JSONifiable
func TestValidRequestBody(t *testing.T) {
	testCases := []struct {
		desc string
		body interface{}
	}{
		{
			desc: "String",
			body: "Hello World",
		},
		{
			desc: "Empty bytestring",
			body: []byte{},
		},
		{
			desc: "Non-empty bytestring",
			body: []byte("Hello World"),
		},
		{
			desc: "Integer",
			body: 1,
		},
		{
			desc: "Float",
			body: 1.23,
		},
		{
			desc: "Object",
			body: struct {
				Word   string `json:"word"`
				Number int    `json:"num"`
			}{
				Word:   "Word",
				Number: 3,
			},
		},
		{
			desc: "Empty object",
			body: struct{}{},
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			resp, err := delayedBatcher.SendRequestWithTimeout(&tC.body, idleTimeout)

			// Ensure no error
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			A, _ := json.Marshal(resp)
			B, _ := json.Marshal(tC.body)

			// Ensure response is consistent
			if string(A) != string(B) {
				t.Errorf(
					"SendRequestWithTimeout output inconsistent."+
						" Expecting %v, got %v",
					string(A), string(B),
				)
			}

		})
	}
}

// TestInvalidRequestBody checks if RequestBatcher rejects invalid requests
func TestInvalidRequestBody(t *testing.T) {
	_, err := delayedBatcher.SendRequestWithTimeout(nil, idleTimeout)
	if err == nil {
		t.Error("Expecting error when sending with `nil` body")
	}
}

// TestTimeoutTooShort checks if timeouts that are too short are rejected
func TestTimeoutTooShort(t *testing.T) {
	_, err := delayedBatcher.SendRequestWithTimeout(
		&nonEmptyRequestBody,
		delayedBatcher.BatchTimeout,
	)
	if err == nil {
		t.Errorf(
			"Expecting error when timeout too short %v",
			delayedBatcher.BatchTimeout,
		)
	}
}

// BenchmarkSendRequestOverhead benchmarks the overhead costs of the
// RequestBatcher
func BenchmarkSendRequestOverhead(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		var reqBody interface{}
		for pb.Next() {
			reqBody = rand.Float32()
			resp, err := immediateBatcher.SendRequestWithTimeout(
				&reqBody, idleTimeout)
			if err != nil {
				b.Errorf("Unexpected error: %v", err)
			}
			if resp != reqBody {
				b.Error("Response not the same")
			}
		}
	})
}

// BenchmarkSendRequestParallel benchmarks the parallelism of the
// RequestBatcher
func BenchmarkSendRequestParallel(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		var reqBody interface{}
		for pb.Next() {
			reqBody = rand.Float32()
			resp, err := delayedBatcher.SendRequestWithTimeout(&reqBody, idleTimeout)
			if err != nil {
				b.Errorf("Unexpected error: %v", err)
			}
			if resp != reqBody {
				b.Error("Response not the same")
			}
		}
	})
}
