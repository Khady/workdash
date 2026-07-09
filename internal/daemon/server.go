package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
)

type Server struct {
	state *State
}

func NewServer(configPath string) *Server {
	return &Server{state: NewState(configPath)}
}

func (s *Server) Serve(listener net.Listener) error {
	s.state.Start()
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	var request Request
	if err := json.NewDecoder(conn).Decode(&request); err != nil {
		_ = json.NewEncoder(conn).Encode(Response{OK: false, Error: err.Error()})
		return
	}
	response, err := s.dispatch(context.Background(), request)
	response.ID = request.ID
	if err != nil {
		response.OK = false
		response.Error = err.Error()
	}
	_ = json.NewEncoder(conn).Encode(response)
}

func (s *Server) dispatch(ctx context.Context, request Request) (Response, error) {
	switch request.Type {
	case "snapshot":
		return s.state.Snapshot(ctx)
	case "warmup":
		response, err := s.state.Snapshot(ctx)
		response.Items = nil
		return response, err
	case "ensure_repo":
		return s.state.EnsureRepo(ctx, request.Cwd)
	case "get_pr_actions":
		return s.state.ActionsForPR(ctx, request.URL)
	case "get_pr_summary":
		return s.state.SummaryForPR(ctx, request.URL)
	case "execute_action":
		return s.state.Execute(request.Token, request.Confirmed)
	case "refresh":
		if err := s.state.Refresh(ctx); err != nil {
			return Response{}, err
		}
		return s.state.Snapshot(ctx)
	case "refresh_browser":
		if err := s.state.Refresh(ctx); err != nil {
			return Response{}, err
		}
		response, err := s.state.Snapshot(ctx)
		response.Items = nil
		return response, err
	default:
		return Response{}, fmt.Errorf("unknown request type %q", request.Type)
	}
}

func Listen() (net.Listener, error) {
	if os.Getenv("LISTEN_PID") == strconv.Itoa(os.Getpid()) {
		if count, _ := strconv.Atoi(os.Getenv("LISTEN_FDS")); count > 0 {
			file := os.NewFile(3, "systemd-listener")
			if file == nil {
				return nil, fmt.Errorf("invalid systemd listener")
			}
			return net.FileListener(file)
		}
	}
	path := SocketPath()
	_ = os.Remove(path)
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		listener.Close()
		return nil, err
	}
	return listener, nil
}
