package logging

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// maxBodyLog is the largest body that is logged. A larger body is omitted
	// entirely rather than truncated, because redacting half a document could
	// leave part of a secret in the log.
	maxBodyLog = 32 << 10

	correlationHeader = "X-Correlation-ID"
)

// RoundTripper wraps next so that every request is logged to the default slog
// logger: one summary record at info, and headers and bodies at debug, all
// redacted. A nil next means http.DefaultTransport. The wrapper never changes
// what the server receives or what the caller reads.
func RoundTripper(next http.RoundTripper) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}
	return &roundTripper{next: next}
}

type roundTripper struct {
	next http.RoundTripper
}

func (rt *roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	logger := slog.Default()
	debug := logger.Enabled(ctx, slog.LevelDebug)

	if !debug && !logger.Enabled(ctx, slog.LevelInfo) {
		return rt.next.RoundTrip(req)
	}

	start := time.Now()
	out := req

	var reqBody string
	if debug {
		out, reqBody = captureRequest(req)
	}

	resp, err := rt.next.RoundTrip(out)
	elapsed := time.Since(start)

	base := []any{
		"method", req.Method,
		"scheme", req.URL.Scheme,
		"host", req.URL.Host,
		"path", req.URL.Path,
	}

	if err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "http request failed", attrs(append(base,
			"duration_ms", elapsed.Milliseconds(),
			"error", err,
		))...)
		return nil, err
	}

	info := append(base, "status", resp.StatusCode, "duration_ms", elapsed.Milliseconds())
	if id := resp.Header.Get(correlationHeader); id != "" {
		info = append(info, "correlation_id", id)
	}
	logger.LogAttrs(ctx, slog.LevelInfo, "http request", attrs(info)...)

	if debug {
		detail := []any{
			"method", req.Method,
			"host", req.URL.Host,
			"path", req.URL.Path,
		}
		if q := req.URL.RawQuery; q != "" {
			detail = append(detail, "query", redactForm(q))
		}
		detail = append(detail,
			slog.Group("request_headers", headerAttrs(req.Header)...),
			slog.Group("response_headers", headerAttrs(resp.Header)...),
		)
		if reqBody != "" {
			detail = append(detail, "request_body", reqBody)
		}
		logger.LogAttrs(ctx, slog.LevelDebug, "http request detail", attrs(detail)...)

		resp.Body = captureResponse(ctx, logger, req, resp)
	}

	return resp, nil
}

// attrs converts alternating key/value pairs and slog.Attr values to []slog.Attr.
func attrs(args []any) []slog.Attr {
	var out []slog.Attr
	for i := 0; i < len(args); {
		switch v := args[i].(type) {
		case slog.Attr:
			out = append(out, v)
			i++
		case string:
			if i+1 < len(args) {
				out = append(out, slog.Any(v, args[i+1]))
			}
			i += 2
		default:
			i++
		}
	}
	return out
}

// headerAttrs returns one attribute per header, in a stable order. Values are
// redacted later by the logger's ReplaceAttr, which sees each header name as a
// key.
func headerAttrs(h http.Header) []any {
	names := make([]string, 0, len(h))
	for k := range h {
		names = append(names, k)
	}
	sort.Strings(names)

	out := make([]any, 0, len(names))
	for _, k := range names {
		out = append(out, slog.String(k, strings.Join(h[k], ", ")))
	}
	return out
}

// isTextual reports whether a content type may be logged: JSON, form-encoded
// and text types. Anything else is never written to a log.
func isTextual(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	return mt == "application/json" ||
		strings.HasSuffix(mt, "+json") ||
		mt == "application/x-www-form-urlencoded" ||
		strings.HasPrefix(mt, "text/")
}

// formatBody redacts a body that has already been judged loggable.
func formatBody(contentType string, data []byte) string {
	mt, _, _ := mime.ParseMediaType(contentType)
	switch {
	case mt == "application/json" || strings.HasSuffix(mt, "+json"):
		if s, ok := redactJSON(data); ok {
			return s
		}
		return redactText(string(data))
	case mt == "application/x-www-form-urlencoded":
		return redactForm(string(data))
	default:
		return redactText(string(data))
	}
}

func omitted(reason string) string { return "[body omitted: " + reason + "]" }

// captureRequest reads a small, textual request body so it can be logged, and
// returns a copy of req that carries an identical body. The original request
// is left untouched, as the http.RoundTripper contract requires.
func captureRequest(req *http.Request) (*http.Request, string) {
	if req.Body == nil || req.Body == http.NoBody {
		return req, ""
	}

	ct := req.Header.Get("Content-Type")
	switch {
	case !isTextual(ct):
		return req, omitted(fmt.Sprintf("%s, %d bytes", contentTypeOrUnknown(ct), req.ContentLength))
	case req.ContentLength <= 0:
		// For an outgoing request, 0 with a non-nil body also means "unknown".
		return req, omitted("unknown length")
	case req.ContentLength > maxBodyLog:
		return req, omitted("larger than 32 KiB")
	}

	data, err := io.ReadAll(req.Body)
	_ = req.Body.Close()
	if err != nil {
		// Hand the caller's transport a body that reports the same failure.
		out := req.Clone(req.Context())
		out.Body = io.NopCloser(io.MultiReader(bytes.NewReader(data), errReader{err}))
		return out, omitted("read error")
	}

	out := req.Clone(req.Context())
	out.Body = io.NopCloser(bytes.NewReader(data))
	out.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }
	return out, formatBody(ct, data)
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

func contentTypeOrUnknown(ct string) string {
	if ct == "" {
		return "unknown content type"
	}
	return ct
}

// captureResponse wraps the response body so that up to maxBodyLog bytes are
// kept as the caller reads, and logs them when the body is closed. Nothing is
// read ahead, so streamed responses behave as before.
func captureResponse(ctx context.Context, logger *slog.Logger, req *http.Request, resp *http.Response) io.ReadCloser {
	if resp.Body == nil || resp.Body == http.NoBody || resp.ContentLength == 0 {
		return resp.Body
	}

	emit := func(body string) {
		logger.LogAttrs(ctx, slog.LevelDebug, "http response body",
			slog.String("host", req.URL.Host),
			slog.String("path", req.URL.Path),
			slog.Int("status", resp.StatusCode),
			slog.String("response_body", body),
		)
	}

	ct := resp.Header.Get("Content-Type")
	switch {
	case !isTextual(ct):
		emit(omitted(fmt.Sprintf("%s, %d bytes", contentTypeOrUnknown(ct), resp.ContentLength)))
		return resp.Body
	case resp.ContentLength > maxBodyLog:
		emit(omitted("larger than 32 KiB"))
		return resp.Body
	}

	return &captureBody{
		ReadCloser: resp.Body,
		emit: func(data []byte, over bool) {
			if over {
				emit(omitted("larger than 32 KiB"))
				return
			}
			emit(formatBody(ct, data))
		},
	}
}

type captureBody struct {
	io.ReadCloser
	emit func(data []byte, over bool)

	buf  bytes.Buffer
	over bool
	once sync.Once
}

func (c *captureBody) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	if n > 0 && !c.over {
		if c.buf.Len()+n > maxBodyLog {
			c.over = true
			c.buf.Reset()
		} else {
			c.buf.Write(p[:n])
		}
	}
	return n, err
}

func (c *captureBody) Close() error {
	c.once.Do(func() { c.emit(c.buf.Bytes(), c.over) })
	return c.ReadCloser.Close()
}
