package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/antonholmquist/jason"
	"github.com/mingruimingrui/batcher"
	"golang.org/x/net/netutil"
)

var (
	addr        string
	backendAddr string

	maxBatchSize int
	batchTimeout time.Duration
	idleTimeout  time.Duration

	maxConcurrentConns int

	ctx            context.Context
	requestBatcher *batcher.RequestBatcher
)

// ParseArgs parses the user provided commandline/environmental arguments
func ParseArgs() {
	// Define flags
	addrPtr := flag.String(
		"bind", "0.0.0.0:8000",
		"Address to bind service.",
	)
	backendPtr := flag.String(
		"backend", "",
		"Address of backend service.",
	)

	maxBatchSizePtr := flag.Int(
		"max-batch-size", 32,
		"Maximum size of each batch.",
	)
	batchTimeoutMillisPtr := flag.Int(
		"batch-timeout-millis", 10,
		"Maximum wait time before batch is sent.",
	)
	idleTimeoutMillisPtr := flag.Int(
		"idle-timeout-millis", 60000,
		"Maximum wait time before idle timeout while waiting for response",
	)

	maxConcurrentConnsPtr := flag.Int(
		"max-concurrent-conns", 1024,
		"Maximum number of clients connected to this service at a time",
	)

	showVersionPtr := flag.Bool("version", false, "Show version")

	// Parse command line and environmental variables
	argv := os.Args[1:]
	argv = append(argv, strings.Split(os.Getenv("BATCHER_CMD_ARGS"), " ")...)
	flag.CommandLine.Parse(argv)

	if *showVersionPtr {
		fmt.Printf("http-batching-service %s\n", batcher.Version)
		os.Exit(0)
	}

	// Assign variables to global scope
	addr = *addrPtr
	backendAddr = *backendPtr

	if backendAddr == "" {
		log.Fatal("[ERROR] -backend must be provided")
	}

	log.Printf("Service listening on %v", addr)
	log.Printf("Batches will be sent to %v", backendAddr)

	maxBatchSize = *maxBatchSizePtr
	batchTimeout = time.Duration(*batchTimeoutMillisPtr * 1000000)
	idleTimeout = time.Duration(*idleTimeoutMillisPtr * 1000000)

	if batchTimeout >= idleTimeout {
		log.Fatal("Batch timeout should be longer than idle timeout")
	}

	log.Printf("Maximum batch size: %v", maxBatchSize)
	log.Printf("Batch timeout: %v", batchTimeout)
	log.Printf("Idle timeout: %v", idleTimeout)

	maxConcurrentConns = *maxConcurrentConnsPtr

	log.Printf("Maximum concurrent connections: %v", maxConcurrentConns)
}

// SendF takes a batch request and sends it to the backend
func SendF(batchReq *[]interface{}) (*[]interface{}, error) {
	// Write body into buffer
	buf := bytes.NewBuffer([]byte{})
	buf.WriteByte(byte(91)) // "["

	buf.Write((*batchReq)[0].([]byte)) // First item
	for i := 1; i < len(*batchReq); i++ {
		buf.WriteByte(byte(44)) // ","
		buf.Write((*batchReq)[i].([]byte))
	}

	buf.WriteByte(byte(93)) // "]"

	// Send request
	backendRes, err := http.Post(backendAddr, "application/json", buf)
	if err != nil {
		return nil, err
	}

	// Parse into JSON array
	jasonRes, err := jason.NewValueFromReader(backendRes.Body)
	if err != nil {
		return nil, err
	}

	jasonResArr, err := jasonRes.Array()
	if err != nil {
		return nil, err
	}

	// Cast into array of bytes and return as response
	var batchRes []interface{}
	for _, v := range jasonResArr {
		v, err := v.Marshal()
		if err != nil {
			return nil, err
		}
		batchRes = append(batchRes, v)
	}

	return &batchRes, nil
}

// RootHandler is the handler for the "/" route
func RootHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.Write([]byte(fmt.Sprintf("batcher for %v", backendAddr)))
		return
	}

	// Parse body as JSON
	jasonBody, err := jason.NewValueFromReader(r.Body)
	if err != nil {
		msg := "Expecting request body in JSON format"
		http.Error(w, msg, http.StatusBadRequest)
		return
	}

	var body interface{}
	body, err = jasonBody.Marshal()
	if err != nil {
		msg := "Error converting json body into []byte"
		http.Error(w, msg, http.StatusBadRequest)
		return
	}

	// Register to batcher and wait on response
	res, err := requestBatcher.SendRequestWithTimeout(&body, idleTimeout)
	if err != nil {
		msg := err.Error()
		statusCode := http.StatusBadRequest

		if strings.Contains(msg, "timeout") {
			statusCode = http.StatusRequestTimeout
		}

		http.Error(w, msg, statusCode)
		return
	}
	w.Write(res.([]byte))
}

func main() {
	ParseArgs()

	ctx = context.Background()
	requestBatcher = batcher.NewRequestBatcher(
		ctx, &batcher.BatchingConfig{
			MaxBatchSize: maxBatchSize,
			BatchTimeout: batchTimeout,
			SendF:        SendF,
		},
	)

	// Server settings
	server := http.Server{
		Addr:         addr,
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
		IdleTimeout:  idleTimeout,
	}

	// Routes
	http.HandleFunc("/", RootHandler)

	// Serve application
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()
	listener = netutil.LimitListener(listener, maxConcurrentConns)
	log.Fatal(server.Serve(listener))
}
