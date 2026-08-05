package llamabroker

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

// brokerError is the inline error body llama-line sends with HTTP 200 when the
// upstream ollama connection fails mid-stream.
// Kept for backward compatibility with older llama-line versions.
type brokerError struct {
	Error string `json:"error"`
}

// PeekBrokerError reads up to 512 bytes from r, checks whether the body is a
// broker error JSON object, and returns (errMsg, remainingReader).
// If not an error body, remainingReader replays the peeked bytes + rest of r.
//
// Deprecated: llama-line now signals errors as SSE events with status="ollama_unavailable".
// StripSSEPreamble handles these. PeekBrokerError is retained for compatibility.
func PeekBrokerError(r io.Reader) (string, io.Reader) {
	buf := make([]byte, 512)
	n, _ := io.ReadAtLeast(r, buf, 1)
	peeked := buf[:n]
	remaining := io.MultiReader(bytes.NewReader(peeked), r)

	trimmed := bytes.TrimSpace(peeked)
	if bytes.HasPrefix(trimmed, []byte(`{"error"`)) {
		var be brokerError
		if json.Unmarshal(trimmed, &be) == nil && be.Error != "" {
			return be.Error, remaining
		}
	}
	return "", remaining
}

// StatusUpdate is an SSE status payload from llama-line.
type StatusUpdate struct {
	RequestID   string `json:"request_id"`
	Position    int    `json:"position"`
	WaitSeconds int    `json:"wait_seconds"`
	Status      string `json:"status"`
	Message     string `json:"message"` // error detail when status is "ollama_unavailable"
}

// brokerErrorBody builds an OpenAI-format error JSON string from a broker StatusUpdate.
func brokerErrorBody(u StatusUpdate) string {
	detail := u.Status
	if u.Message != "" {
		detail = u.Status + ": " + u.Message
	}
	return `{"error":{"message":"llama-line broker error: ` + detail + `","type":"broker_error","code":"broker_error"}}`
}

// StripSSEPreamble removes leading broker SSE status events and returns a reader
// positioned at the first actual response payload (unwrapped from its data: line).
// Use this for non-streaming requests where the final body is a single JSON object.
func StripSSEPreamble(r io.Reader) io.Reader {
	br := bufio.NewReader(r)

	for {
		line, err := br.ReadString('\n')
		if line == "" && err != nil {
			return br
		}

		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			if err != nil {
				return br
			}
			continue
		}

		if strings.HasPrefix(trimmed, "data: ") {
			payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data: "))
			if payload == "" || payload == "[DONE]" {
				if err != nil {
					return br
				}
				continue
			}
			// Check if this is a broker status event.
			var update StatusUpdate
			if jsonErr := json.Unmarshal([]byte(payload), &update); jsonErr == nil && update.Status != "" {
				if update.Status == "queued" || update.Status == "running" {
					// Normal heartbeat — discard and continue.
					if err != nil {
						return br
					}
					continue
				}
				// ollama_unavailable or any other error status.
				return strings.NewReader(brokerErrorBody(update))
			}
			// It's the actual response payload wrapped in SSE — return just the JSON.
			return io.MultiReader(strings.NewReader(payload+"\n"), br)
		}

		return io.MultiReader(strings.NewReader(line), br)
	}
}

// StripBrokerHeartbeats removes leading broker SSE status events and returns a reader
// that replays the remaining SSE stream intact (data: lines preserved).
// Use this for streaming requests where the body is a sequence of SSE chunks.
func StripBrokerHeartbeats(r io.Reader) io.Reader {
	br := bufio.NewReader(r)
	var buf strings.Builder

	for {
		line, err := br.ReadString('\n')
		if line == "" && err != nil {
			return io.MultiReader(strings.NewReader(buf.String()), br)
		}

		trimmed := strings.TrimRight(line, "\r\n")

		if strings.HasPrefix(trimmed, "data: ") {
			payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data: "))
			if payload != "" && payload != "[DONE]" {
				var update StatusUpdate
				if jsonErr := json.Unmarshal([]byte(payload), &update); jsonErr == nil && update.Status != "" {
					if update.Status == "queued" || update.Status == "running" {
						// Broker heartbeat — discard.
						if err != nil {
							return io.MultiReader(strings.NewReader(buf.String()), br)
						}
						continue
					}
					// Broker error — synthesise an SSE error event the SDK can surface.
					errLine := "data: " + brokerErrorBody(update) + "\n\n"
					return io.MultiReader(strings.NewReader(errLine), br)
				}
			}
		}

		// Non-broker line (actual SSE chunk, blank separator, [DONE], etc.) — keep it.
		buf.WriteString(line)
		if err != nil {
			return io.MultiReader(strings.NewReader(buf.String()), br)
		}
	}
}

type readCloser struct {
	io.Reader
	io.Closer
}

func (r readCloser) Close() error { return r.Closer.Close() }

// StripSSEPreambleReadCloser strips the SSE preamble and preserves Close().
// Use for non-streaming requests.
func StripSSEPreambleReadCloser(rc io.ReadCloser) io.ReadCloser {
	if rc == nil {
		return nil
	}
	return readCloser{Reader: StripSSEPreamble(rc), Closer: rc}
}

// StripBrokerHeartbeatsReadCloser strips broker heartbeats and preserves Close().
// Use for streaming requests.
func StripBrokerHeartbeatsReadCloser(rc io.ReadCloser) io.ReadCloser {
	if rc == nil {
		return nil
	}
	return readCloser{Reader: StripBrokerHeartbeats(rc), Closer: rc}
}
