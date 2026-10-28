package http1

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type ResponseWriterImpl struct {
	conn        net.Conn
	header      http.Header
	statusCode  int
	request     *http.Request
	wroteHeader bool
	bodyBuf     bytes.Buffer
}

func NewResponseWriter(conn net.Conn, request *http.Request) *ResponseWriterImpl {
	return &ResponseWriterImpl{
		conn:       conn,
		header:     make(http.Header),
		statusCode: http.StatusOK,
		request:    request,
	}
}

func (r *ResponseWriterImpl) Header() http.Header {
	return r.header
}

func (r *ResponseWriterImpl) Write(data []byte) (n int, err error) {
	return r.bodyBuf.Write(data)
}

func (r *ResponseWriterImpl) WriteHeader(statusCode int) {
	if !r.wroteHeader {
		r.statusCode = statusCode
	}
}

func (r *ResponseWriterImpl) Flush() error {
	if r.wroteHeader {
		return nil
	}
	r.wroteHeader = true

	if r.statusCode == 0 {
		r.statusCode = http.StatusOK
	}

	if r.header.Get("Content-Length") == "" && r.header.Get("Transfer-Encoding") == "" {
		r.header.Set("Content-Length", strconv.Itoa(r.bodyBuf.Len()))
	}

	r.applyDefaultHeaders()

	statusText := http.StatusText(r.statusCode)
	if statusText == "" {
		statusText = "Unknown Status"
	}

	if _, err := fmt.Fprintf(r.conn, "HTTP/1.1 %d %s\r\n", r.statusCode, statusText); err != nil {
		return err
	}
	if err := r.header.Write(r.conn); err != nil {
		return err
	}
	if _, err := fmt.Fprint(r.conn, "\r\n"); err != nil {
		return err
	}

	if r.bodyBuf.Len() > 0 {
		if _, err := r.conn.Write(r.bodyBuf.Bytes()); err != nil {
			return err
		}
	}
	return nil
}

func (r *ResponseWriterImpl) ShouldClose() bool {
	if r.request == nil {
		return true
	}

	connHeader := strings.ToLower(strings.TrimSpace(r.request.Header.Get("Connection")))

	if r.request.ProtoMajor == 1 && r.request.ProtoMinor == 0 {
		return connHeader != "keep-alive"
	}

	return connHeader == "close"
}

func (r *ResponseWriterImpl) applyDefaultHeaders() {
	if r.header.Get("Date") == "" {
		r.header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
	}
	if r.header.Get("Server") == "" {
		r.header.Set("Server", "ALDI Sucks/1.0")
	}
	if r.ShouldClose() {
		r.header.Set("Connection", "close")
	} else {
		r.header.Set("Connection", "keep-alive")
	}
	if r.header.Get("Content-Type") == "" {
		r.header.Set("Content-Type", "text/plain; charset=utf-8")
	}
	if r.header.Get("X-Content-Type-Options") == "" {
		r.header.Set("X-Content-Type-Options", "nosniff")
	}
}

func NewClosingResponse(statusCode int) http.Response {
	return http.Response{
		StatusCode: statusCode,
		ProtoMajor: 1,
		ProtoMinor: 1,
		Close:      true,
	}
}
