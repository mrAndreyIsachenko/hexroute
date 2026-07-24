package ipc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	RootSocketPath = "/var/run/hexroute-observe/hexrouted.sock"
	ioTimeout      = 15 * time.Second
)

type Handler interface {
	HandleIPC(context.Context, Request) Response
}

type RejectionReporter interface {
	ReportIPCRejection(error)
}

type Server struct {
	listener   *net.UnixListener
	allowedUID uint32
	handler    Handler
	reporter   RejectionReporter
}

type Client struct {
	Path    string
	Timeout time.Duration
}

func UserSocketPath(home string) (string, error) {
	if !filepath.IsAbs(home) || filepath.Clean(home) != home {
		return "", ErrInvalidSocketPath
	}
	return filepath.Join(
		home,
		"Library/Application Support/Hexroute/observe-user/state/userd.sock",
	), nil
}

var (
	ErrInvalidSocketPath = errors.New("invalid IPC socket path")
	ErrSocketInUse       = errors.New("IPC socket path is not replaceable")
	ErrResponseMismatch  = errors.New("IPC response does not match request")
)

func Listen(
	path string,
	socketUID uint32,
	allowedUID uint32,
	handler Handler,
	reporter RejectionReporter,
) (*Server, error) {
	if handler == nil || !validSocketPath(path) {
		return nil, ErrInvalidSocketPath
	}
	if err := validateSocketDirectory(filepath.Dir(path)); err != nil {
		return nil, err
	}
	if err := removeStaleSocket(path, socketUID); err != nil {
		return nil, err
	}

	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("listen on IPC socket: %w", err)
	}
	listener.SetUnlinkOnClose(true)
	cleanup := func(cause error) (*Server, error) {
		_ = listener.Close()
		return nil, cause
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return cleanup(fmt.Errorf("protect IPC socket: %w", err))
	}
	if err := os.Chown(path, int(socketUID), -1); err != nil {
		return cleanup(fmt.Errorf("set IPC socket owner: %w", err))
	}
	return &Server{
		listener:   listener,
		allowedUID: allowedUID,
		handler:    handler,
		reporter:   reporter,
	}, nil
}

func (server *Server) Serve(ctx context.Context) error {
	if server == nil || server.listener == nil || server.handler == nil || ctx == nil {
		return ErrInvalidSocketPath
	}
	go func() {
		<-ctx.Done()
		_ = server.listener.Close()
	}()

	const maxConcurrentConnections = 8
	limit := make(chan struct{}, maxConcurrentConnections)
	var active sync.WaitGroup
	defer active.Wait()
	for {
		connection, err := server.listener.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accept IPC connection: %w", err)
		}
		select {
		case limit <- struct{}{}:
			active.Add(1)
			go func() {
				defer active.Done()
				defer func() { <-limit }()
				server.serveConnection(ctx, connection)
			}()
		case <-ctx.Done():
			_ = connection.Close()
			return nil
		}
	}
}

func (server *Server) Close() error {
	if server == nil || server.listener == nil {
		return nil
	}
	return server.listener.Close()
}

func (server *Server) serveConnection(ctx context.Context, connection *net.UnixConn) {
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(ioTimeout))
	if _, err := AuthorizePeer(connection, server.allowedUID); err != nil {
		server.report(err)
		return
	}
	request, err := ReadRequest(connection)
	if err != nil {
		server.report(err)
		return
	}
	if ctx.Err() != nil {
		return
	}
	requestCtx, cancel := context.WithTimeout(ctx, ioTimeout)
	defer cancel()
	response := server.handler.HandleIPC(requestCtx, request)
	if err := response.Validate(); err != nil {
		server.report(err)
		response = Response{
			Version:   ProtocolVersion,
			RequestID: request.RequestID,
			Error:     ErrorInternal,
		}
	}
	if err := WriteFrame(connection, response); err != nil {
		server.report(err)
	}
}

func (server *Server) report(err error) {
	if server.reporter != nil {
		server.reporter.ReportIPCRejection(err)
	}
}

func (client Client) Do(ctx context.Context, request Request) (Response, error) {
	if ctx == nil || !validSocketPath(client.Path) {
		return Response{}, ErrInvalidSocketPath
	}
	if err := request.Validate(); err != nil {
		return Response{}, err
	}
	timeout := client.Timeout
	if timeout <= 0 || timeout > 30*time.Second {
		timeout = ioTimeout
	}
	dialer := net.Dialer{Timeout: timeout}
	raw, err := dialer.DialContext(ctx, "unix", client.Path)
	if err != nil {
		return Response{}, fmt.Errorf("connect to IPC socket: %w", err)
	}
	connection, ok := raw.(*net.UnixConn)
	if !ok {
		raw.Close()
		return Response{}, ErrInvalidSocketPath
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(timeout))
	if err := WriteFrame(connection, request); err != nil {
		return Response{}, err
	}
	response, err := ReadResponse(connection)
	if err != nil {
		return Response{}, err
	}
	if response.RequestID != request.RequestID {
		return Response{}, ErrResponseMismatch
	}
	return response, nil
}

func validSocketPath(path string) bool {
	return filepath.IsAbs(path) &&
		filepath.Clean(path) == path &&
		strings.HasSuffix(filepath.Base(path), ".sock")
}

func validateSocketDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("%w: socket directory", ErrInvalidSocketPath)
	}
	if !info.IsDir() ||
		info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm()&0o022 != 0 {
		return ErrInvalidSocketPath
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		return ErrInvalidSocketPath
	}
	return nil
}

func removeStaleSocket(path string, socketUID uint32) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil ||
		info.Mode()&os.ModeSymlink != 0 ||
		info.Mode()&os.ModeSocket == 0 {
		return ErrSocketInUse
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || (stat.Uid != socketUID && int(stat.Uid) != os.Geteuid()) {
		return ErrSocketInUse
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale IPC socket: %w", err)
	}
	return nil
}
