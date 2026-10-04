// Package metadata provides the small HTTP service used by guests to retrieve
// their Juju bootstrap data.
package metadata

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const shutdownTimeout = 5 * time.Second

// MetadataServer serves instance-specific user data.
//
// The server is safe for concurrent registration and requests. RegisterPayload
// copies userData, and handlers copy the selected payload before writing it, so
// callers cannot mutate data while it is being served.
type MetadataServer struct {
	mu       sync.RWMutex
	payloads map[string][]byte
	server   *http.Server
	listener net.Listener
	done     chan struct{}
}

// NewMetadataServer constructs an empty metadata server.
func NewMetadataServer() *MetadataServer {
	return &MetadataServer{payloads: make(map[string][]byte)}
}

// RegisterPayload associates userData with instanceID. Both an existing entry
// and a new entry are replaced atomically. An empty instance ID is ignored.
func (s *MetadataServer) RegisterPayload(instanceID string, userData []byte) {
	if instanceID == "" {
		return
	}
	payload := append([]byte(nil), userData...)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.payloads == nil {
		s.payloads = make(map[string][]byte)
	}
	s.payloads[instanceID] = payload
}

// Start binds to localhost on port. Port 0 asks the operating system for an
// ephemeral port. Start returns after the listener is ready; serving continues
// in the background. Starting an already-running server returns an error.
func (s *MetadataServer) Start(port int) error {
	if port < 0 || port > 65535 {
		return fmt.Errorf("invalid port %d", port)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return errors.New("metadata server is already running")
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return err
	}
	s.listener = listener
	s.server = &http.Server{Handler: http.HandlerFunc(s.handle)}
	s.done = make(chan struct{})
	server := s.server
	done := s.done
	go func() {
		_ = server.Serve(listener)
		close(done)
	}()
	return nil
}

// Addr returns the bound address, or an empty string before Start or after
// Stop. It is useful when Start was called with port 0.
func (s *MetadataServer) Addr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// Stop gracefully shuts down the server. It is safe to call multiple times,
// including before Start. A shutdown is given five seconds to finish active
// requests.
func (s *MetadataServer) Stop() error {
	s.mu.Lock()
	server, done := s.server, s.done
	if server == nil {
		s.mu.Unlock()
		return nil
	}
	// Clear state before Shutdown so concurrent Stop calls are idempotent.
	s.server, s.listener, s.done = nil, nil, nil
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	err := server.Shutdown(ctx)
	if done != nil {
		<-done
	}
	return err
}

func (s *MetadataServer) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	switch r.URL.Path {
	case "/latest/user-data":
		s.userData(w, r)
	case "/latest/meta-data/instance-id":
		s.instanceID(w, r, "")
	default:
		const prefix = "/latest/meta-data/instance-id/"
		if strings.HasPrefix(r.URL.Path, prefix) {
			s.instanceID(w, r, strings.TrimPrefix(r.URL.Path, prefix))
			return
		}
		http.NotFound(w, r)
	}
}

// instanceID routing contract: the canonical endpoint may carry the ID in
// ?instance= or X-Instance-ID; alternatively /instance-id/<id> is accepted.
// The path form is checked first, then query, then header. A request without
// an ID is rejected because there is no safe way to infer a guest identity.
func (s *MetadataServer) instanceID(w http.ResponseWriter, r *http.Request, pathID string) {
	id := pathID
	if id == "" {
		id = r.URL.Query().Get("instance")
	}
	if id == "" {
		id = r.Header.Get("X-Instance-ID")
	}
	if id == "" {
		http.Error(w, "instance ID is required", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(id))
}

func (s *MetadataServer) userData(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("instance")
	if id == "" {
		id = r.Header.Get("X-Instance-ID")
	}
	if id == "" {
		http.Error(w, "instance ID is required", http.StatusBadRequest)
		return
	}
	s.mu.RLock()
	payload, ok := s.payloads[id]
	payload = append([]byte(nil), payload...)
	s.mu.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}
