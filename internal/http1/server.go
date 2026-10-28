package http1

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

const (
	IdleTimeout = 10 * time.Second
)

func ListenAndServe(addr string, handler http.Handler) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()

	if handler == nil {
		handler = DefaultServeMux
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}

		go func(conn net.Conn) {
			if err := HandleConnection(conn, handler); err != nil {
				fmt.Printf("handle connection %s: %v", conn.RemoteAddr(), err)
			}
		}(conn)
	}
}

func HandleConnection(conn net.Conn, mux http.Handler) error {
	defer conn.Close()

	reader := bufio.NewReader(conn)

	for {
		_ = conn.SetReadDeadline(time.Now().Add(IdleTimeout)) // set timeout
		req, err := ParseRequest(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}

			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				return nil
			}

			resp := NewClosingResponse(http.StatusBadRequest)
			resp.Write(conn)
			return nil
		}
		_ = conn.SetReadDeadline(time.Time{})

		validVersion := validateRequestVersion(req)
		if !validVersion {
			resp := NewClosingResponse(http.StatusHTTPVersionNotSupported)
			resp.Write(conn)
			return nil
		}

		validContentLength := validateContentLength(req)
		if !validContentLength {
			resp := NewClosingResponse(http.StatusBadRequest)
			resp.Write(conn)
			return nil
		}

		res := NewResponseWriter(conn, req)

		mux.ServeHTTP(res, req)

		if err := res.Flush(); err != nil {
			return err
		}

		if req.Body != nil {
			_, _ = io.Copy(io.Discard, req.Body)
			_ = req.Body.Close()
		}

		if res.ShouldClose() {
			return nil
		}
	}
}

func validateRequestVersion(req *http.Request) bool {
	if req.ProtoMajor != 1 {
		return false
	}

	if req.ProtoMinor != 0 && req.ProtoMinor != 1 {
		return false
	}

	return true
}

func validateContentLength(req *http.Request) bool {
	clHeaders, exists := req.Header["Content-Length"]
	if exists && len(clHeaders) > 1 {
		return allEqual(clHeaders)
	}
	return true
}

func allEqual[T comparable](slice []T) bool {
	if len(slice) <= 1 {
		return true
	}
	first := slice[0]
	for _, val := range slice[1:] {
		if val != first {
			return false
		}
	}
	return true
}
