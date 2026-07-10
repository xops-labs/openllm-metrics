// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"sync"
)

// maxSSEChunkBytes caps the size of a single SSE / event-stream chunk
// that the observer is allowed to retain for usage parsing. The proxy
// streams the bytes to the client first; this slice is only used to feed
// the per-provider usage parser at completion time.
const maxSSEChunkBytes = 16 * 1024

// usageSink is a tiny goroutine-safe holder for the last sampled chunk
// (streaming) or the (capped) full body (non-streaming). The proxy hands
// the sink to a tappingReader, then reads sink.Snapshot() once the
// upstream body has been fully drained by httputil.
//
// The sink is intentionally small. Streaming responses keep only the
// most recent qualifying SSE / event-stream chunk; non-streaming
// responses keep the prefix up to maxBuf bytes. We never buffer the full
// streaming body.
type usageSink struct {
	mu       sync.Mutex
	last     bytes.Buffer
	buffered bytes.Buffer
	maxBuf   int

	// model holds the model identifier captured from the FIRST streaming
	// chunk that carried one. It surfaces the model for streaming responses
	// (notably Anthropic, whose trailing usage chunk omits the model). Only
	// the scalar string is retained — never any response body.
	model string
}

func newUsageSink(maxBuf int) *usageSink {
	if maxBuf <= 0 {
		maxBuf = 256 * 1024
	}
	return &usageSink{maxBuf: maxBuf}
}

// Snapshot returns the most recent usage-bearing chunk. For streaming
// responses this is the trailing chunk; for buffered responses it is the
// (capped) body prefix.
func (s *usageSink) Snapshot() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last.Len() > 0 {
		out := make([]byte, s.last.Len())
		copy(out, s.last.Bytes())
		return out
	}
	out := make([]byte, s.buffered.Len())
	copy(out, s.buffered.Bytes())
	return out
}

func (s *usageSink) appendBuffered(p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.buffered.Len() >= s.maxBuf {
		return
	}
	room := s.maxBuf - s.buffered.Len()
	if room > len(p) {
		room = len(p)
	}
	s.buffered.Write(p[:room])
}

func (s *usageSink) setLast(line []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last.Reset()
	if len(line) > maxSSEChunkBytes {
		line = line[:maxSSEChunkBytes]
	}
	s.last.Write(line)
}

// setModelOnce records the model from the first chunk that carries one.
// Subsequent calls are no-ops, so the earliest (message_start) model wins.
func (s *usageSink) setModelOnce(m string) {
	if m == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.model == "" {
		s.model = m
	}
}

// Model returns the model captured from the streaming chunks, or "" if no
// chunk carried one.
func (s *usageSink) Model() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.model
}

// tappingReader wraps an upstream io.ReadCloser and lets bytes flow
// through unchanged (so httputil.ReverseProxy can copy them straight to
// the inbound ResponseWriter). As a side effect it samples bytes into a
// usageSink for usage parsing — but it NEVER intercepts the byte stream
// itself. The pass-through is exact: every Read returns the same bytes
// upstream produced, in the same chunks.
//
// For streaming responses (text/event-stream, AWS event-stream-ish
// passthrough) we parse SSE line framing from the byte stream so we can
// snapshot only the trailing `usage` chunk. For non-streaming responses
// we copy the prefix into a capped buffer.
type tappingReader struct {
	src       io.ReadCloser
	sink      *usageSink
	streaming bool

	// lineBuf is a small accumulator that re-assembles SSE lines across
	// successive Read calls. It NEVER grows above maxSSEChunkBytes*4 —
	// oversized lines are summarily truncated.
	lineBuf []byte
}

func newTappingReader(src io.ReadCloser, sink *usageSink, streaming bool) *tappingReader {
	return &tappingReader{src: src, sink: sink, streaming: streaming}
}

func (t *tappingReader) Read(p []byte) (int, error) {
	n, err := t.src.Read(p)
	if n > 0 {
		if t.streaming {
			t.sampleStreaming(p[:n])
		} else {
			t.sink.appendBuffered(p[:n])
		}
	}
	return n, err
}

func (t *tappingReader) Close() error { return t.src.Close() }

// sampleStreaming feeds bytes into a line-framed accumulator and snapshots
// each completed line into the sink if it looks like a usage-bearing
// chunk.
func (t *tappingReader) sampleStreaming(chunk []byte) {
	t.lineBuf = append(t.lineBuf, chunk...)
	for {
		i := bytes.IndexByte(t.lineBuf, '\n')
		if i < 0 {
			// Cap the accumulator so a malformed upstream cannot drive
			// unbounded growth. We keep only the tail.
			if len(t.lineBuf) > maxSSEChunkBytes*4 {
				t.lineBuf = t.lineBuf[len(t.lineBuf)-maxSSEChunkBytes:]
			}
			return
		}
		line := t.lineBuf[:i]
		t.lineBuf = t.lineBuf[i+1:]
		t.consider(line)
	}
}

func (t *tappingReader) consider(line []byte) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return
	}
	if bytes.HasPrefix(trimmed, []byte("event:")) || bytes.HasPrefix(trimmed, []byte(":")) {
		// SSE comment / event-name lines — skip.
		return
	}
	data := trimmed
	if bytes.HasPrefix(trimmed, []byte("data:")) {
		data = bytes.TrimSpace(bytes.TrimPrefix(trimmed, []byte("data:")))
	}
	if bytes.Equal(data, []byte("[DONE]")) {
		return
	}
	if len(data) == 0 || data[0] != '{' {
		return
	}
	// READ-ONLY side effect: peek the chunk for a model and record the
	// first one seen. This runs for every JSON chunk (including
	// message_start, which carries the model but no usage) and never
	// alters the pass-through byte flow or the usage sampling below.
	t.sink.setModelOnce(extractStreamModel(data))
	t.sink.setLast(data)
}

// extractStreamModel peeks a single decoded SSE/event JSON chunk for a
// model identifier. It checks the top-level "model" field (OpenAI/Azure
// streaming chunks) and the Anthropic message_start shape
// {"type":"message_start","message":{"model":"..."}}. Returns "" when no
// model is present. It only reads the bytes — never logs or retains them.
func extractStreamModel(data []byte) string {
	if len(data) == 0 || data[0] != '{' {
		return ""
	}
	var chunk struct {
		Model   string `json:"model"`
		Message *struct {
			Model string `json:"model"`
		} `json:"message"`
	}
	if err := json.Unmarshal(data, &chunk); err != nil {
		return ""
	}
	if chunk.Model != "" {
		return chunk.Model
	}
	if chunk.Message != nil {
		return chunk.Message.Model
	}
	return ""
}

// flushTail snapshots any trailing partial line (no terminating newline)
// once the upstream body has been fully read. Called by the proxy after
// ServeHTTP returns so a usage chunk emitted right at EOF is captured.
func (t *tappingReader) flushTail() {
	if !t.streaming || len(t.lineBuf) == 0 {
		return
	}
	t.consider(t.lineBuf)
	t.lineBuf = nil
}

// IsStreamingContentType returns true if the upstream response Content-Type
// indicates a streaming media type the gateway must pass through with
// flushing (OpenAI SSE, Anthropic SSE, Bedrock AWS event-stream).
func IsStreamingContentType(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(ct))
	if ct == "" {
		return false
	}
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	switch ct {
	case "text/event-stream",
		"application/vnd.amazon.eventstream",
		"application/x-ndjson":
		return true
	}
	return false
}
