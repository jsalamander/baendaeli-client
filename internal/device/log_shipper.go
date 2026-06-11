package device

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"
)

type logShipResponse struct {
	Success       bool   `json:"success"`
	AcceptedLines int    `json:"accepted_lines"`
	WrittenBytes  int    `json:"written_bytes"`
	Filename      string `json:"filename"`
}

type shippedLogLine struct {
	Level   string `json:"level"`
	Time    string `json:"time"`
	Message string `json:"message"`
}

type logShipper struct {
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	httpClient *http.Client
	client     *Client

	flushInterval   time.Duration
	batchLines      int
	maxQueueLines   int
	maxLineBytes    int
	maxRequestBytes int

	queueMu sync.Mutex
	queue   []string

	notifyFlush chan struct{}
	disabled    bool

	rngMu sync.Mutex
	rng   *rand.Rand

	diagWriter io.Writer
}

func newLogShipper(parent context.Context, c *Client, httpClient *http.Client, diagWriter io.Writer) *logShipper {
	ctx, cancel := context.WithCancel(parent)
	if diagWriter == nil {
		diagWriter = io.Discard
	}
	seed := time.Now().UnixNano()
	return &logShipper{
		ctx:             ctx,
		cancel:          cancel,
		httpClient:      httpClient,
		client:          c,
		flushInterval:   time.Duration(c.config.LogShippingFlushIntervalMs) * time.Millisecond,
		batchLines:      c.config.LogShippingBatchLines,
		maxQueueLines:   c.config.LogShippingMaxQueueLines,
		maxLineBytes:    c.config.LogShippingMaxLineBytes,
		maxRequestBytes: c.config.LogShippingMaxRequestBytes,
		notifyFlush:     make(chan struct{}, 1),
		rng:             rand.New(rand.NewSource(seed)),
		diagWriter:      diagWriter,
	}
}

func (s *logShipper) start() {
	s.wg.Add(1)
	go s.run()
}

func (s *logShipper) stopAndFlush(timeout time.Duration) {
	s.cancel()
	waitDone := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(waitDone)
	}()

	select {
	case <-waitDone:
	case <-time.After(timeout):
		s.diagf("Device client: log shipper stop timed out with pending lines")
	}

	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	flushCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_ = s.flushOnce(flushCtx)
}

func (s *logShipper) enqueue(rawLine string) {
	if !s.client.config.LogShippingEnabled {
		return
	}
	if s.isDisabled() {
		return
	}

	line := strings.TrimSpace(rawLine)
	if line == "" {
		return
	}

	event := shippedLogLine{
		Level:   inferLogLevel(line),
		Time:    time.Now().UTC().Format(time.RFC3339Nano),
		Message: line,
	}
	serialized, err := json.Marshal(event)
	if err != nil {
		s.diagf("Device client: failed to encode log event for shipping: %v", err)
		return
	}

	if len(serialized) > s.maxLineBytes {
		s.diagf("Device client: skipped oversized log line (%d bytes > %d)", len(serialized), s.maxLineBytes)
		return
	}

	s.queueMu.Lock()
	if len(s.queue) >= s.maxQueueLines {
		s.queue = s.queue[1:]
		s.diagf("Device client: log shipping queue full, dropped oldest line")
	}
	s.queue = append(s.queue, string(serialized))
	queueLen := len(s.queue)
	s.queueMu.Unlock()

	if queueLen >= s.batchLines {
		s.signalFlush()
	}
}

func (s *logShipper) run() {
	defer s.wg.Done()

	interval := s.flushInterval
	if interval <= 0 {
		interval = 3 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	backoff := 1 * time.Second

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		case <-s.notifyFlush:
		}

		if s.isDisabled() {
			continue
		}

		err := s.flushOnce(s.ctx)
		if err == nil {
			backoff = 1 * time.Second
			continue
		}

		if s.ctx.Err() != nil {
			return
		}

		d := backoff + s.randomJitter(250*time.Millisecond)
		timer := time.NewTimer(d)
		select {
		case <-s.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

func (s *logShipper) flushOnce(ctx context.Context) error {
	batch := s.peekBatch()
	if len(batch) == 0 {
		return nil
	}

	resp, statusCode, err := s.sendBatch(ctx, batch)
	if err != nil {
		s.diagf("Device client: failed to ship %d log lines: %v", len(batch), err)
		return err
	}

	switch statusCode {
	case http.StatusOK, http.StatusCreated:
		if !resp.Success {
			s.diagf("Device client: log ship response indicated success=false")
			return fmt.Errorf("log ship response success=false")
		}
		if resp.AcceptedLines < len(batch) {
			s.diagf("Device client: log ship accepted %d/%d lines; dropped by server=%d", resp.AcceptedLines, len(batch), len(batch)-resp.AcceptedLines)
		}
		s.dropFromQueue(len(batch))
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		s.diagf("Device client: log shipping disabled due to auth error (status=%d)", statusCode)
		s.setDisabled(true)
		return nil
	case http.StatusUnprocessableEntity:
		s.diagf("Device client: log ship payload rejected (422), splitting batch")
		if len(batch) <= 1 {
			s.diagf("Device client: dropping invalid log line after 422")
			s.dropFromQueue(1)
			return nil
		}
		half := len(batch) / 2
		if half == 0 {
			half = 1
		}
		firstHalf := batch[:half]
		firstResp, firstCode, firstErr := s.sendBatch(ctx, firstHalf)
		if firstErr == nil && (firstCode == http.StatusOK || firstCode == http.StatusCreated) && firstResp.Success {
			s.dropFromQueue(len(firstHalf))
			return fmt.Errorf("first half accepted after 422 split, retry remaining")
		}
		return fmt.Errorf("422 split retry pending")
	default:
		s.diagf("Device client: transient log ship error status=%d", statusCode)
		return fmt.Errorf("unexpected status code %d", statusCode)
	}
}

func (s *logShipper) sendBatch(ctx context.Context, lines []string) (logShipResponse, int, error) {
	url := s.client.buildURL("/api/v1/device/logs")
	body := strings.Join(lines, "\n")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBufferString(body))
	if err != nil {
		return logShipResponse{}, 0, err
	}
	s.client.setAuthHeader(req)
	req.Header.Set("Content-Type", "application/x-ndjson")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return logShipResponse{}, 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return logShipResponse{}, resp.StatusCode, err
	}

	if len(bytes.TrimSpace(respBody)) == 0 {
		return logShipResponse{}, resp.StatusCode, nil
	}

	var decoded logShipResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return logShipResponse{}, resp.StatusCode, err
	}

	return decoded, resp.StatusCode, nil
}

func (s *logShipper) peekBatch() []string {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()

	if len(s.queue) == 0 {
		return nil
	}

	limit := s.batchLines
	if limit <= 0 || limit > len(s.queue) {
		limit = len(s.queue)
	}

	batch := make([]string, 0, limit)
	usedBytes := 0
	for i := 0; i < limit; i++ {
		line := s.queue[i]
		next := len(line)
		if i > 0 {
			next++
		}
		if s.maxRequestBytes > 0 && usedBytes+next > s.maxRequestBytes {
			break
		}
		batch = append(batch, line)
		usedBytes += next
	}

	if len(batch) == 0 && len(s.queue) > 0 {
		batch = append(batch, s.queue[0])
	}
	return batch
}

func (s *logShipper) dropFromQueue(n int) {
	if n <= 0 {
		return
	}
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	if n > len(s.queue) {
		n = len(s.queue)
	}
	s.queue = s.queue[n:]
}

func (s *logShipper) signalFlush() {
	select {
	case s.notifyFlush <- struct{}{}:
	default:
	}
}

func (s *logShipper) setDisabled(v bool) {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	s.disabled = v
}

func (s *logShipper) isDisabled() bool {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	return s.disabled
}

func (s *logShipper) randomJitter(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	s.rngMu.Lock()
	defer s.rngMu.Unlock()
	return time.Duration(s.rng.Int63n(max.Nanoseconds() + 1))
}

func (s *logShipper) diagf(format string, args ...any) {
	_, _ = fmt.Fprintf(s.diagWriter, format+"\n", args...)
}

func inferLogLevel(message string) string {
	m := strings.ToLower(message)
	switch {
	case strings.Contains(m, "error"), strings.Contains(m, "failed"), strings.Contains(m, "panic"):
		return "error"
	case strings.Contains(m, "warn"):
		return "warn"
	default:
		return "info"
	}
}
