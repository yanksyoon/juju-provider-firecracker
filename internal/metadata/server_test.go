package metadata

import (
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMetadataServerServesCopiedPayloadAndStops(t *testing.T) {
	s := NewMetadataServer()
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
	payload := []byte("#!/bin/bash\necho test\n")
	s.RegisterPayload("i-123", payload)
	payload[0] = 'X'
	if err := s.Start(0); err != nil {
		t.Fatal(err)
	}
	addr := s.Addr()
	if addr == "" {
		t.Fatal("Start did not expose a listener address")
	}

	resp, err := http.Get("http://" + addr + "/latest/user-data?instance=i-123")
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if resp.StatusCode != http.StatusOK || string(body) != "#!/bin/bash\necho test\n" {
		t.Fatalf("response = %d %q", resp.StatusCode, body)
	}

	start := time.Now()
	for i := 0; i < 10; i++ {
		response, err := http.Get("http://" + addr + "/latest/user-data?instance=i-123")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("request %d returned status %d", i, response.StatusCode)
		}
	}
	if elapsed := time.Since(start); elapsed >= 100*time.Millisecond {
		t.Fatalf("ten local requests took %s", elapsed)
	}

	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(); err != nil {
		t.Fatal("second Stop: ", err)
	}
	if s.Addr() != "" {
		t.Fatal("address remains after Stop")
	}
	client := &http.Client{Timeout: 500 * time.Millisecond}
	if _, err := client.Get("http://" + addr + "/latest/user-data?instance=i-123"); err == nil {
		t.Fatal("request succeeded after Stop")
	}
}

func TestMetadataServerMissingPayloadAndMethods(t *testing.T) {
	s := NewMetadataServer()
	if err := s.Start(0); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	base := "http://" + s.Addr()

	resp, err := http.Get(base + "/latest/user-data?instance=missing")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("missing payload status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
	_ = resp.Body.Close()

	req, err := http.NewRequest(http.MethodPost, base+"/latest/user-data", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusMethodNotAllowed || resp.Header.Get("Allow") != http.MethodGet {
		t.Errorf("POST response = %d Allow=%q", resp.StatusCode, resp.Header.Get("Allow"))
	}
	_ = resp.Body.Close()
}

func TestMetadataServerInstanceIDRoutingContract(t *testing.T) {
	s := NewMetadataServer()
	if err := s.Start(0); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	base := "http://" + s.Addr()
	cases := []struct {
		name   string
		url    string
		header string
		want   string
		code   int
	}{
		{"query", base + "/latest/meta-data/instance-id?instance=i-query", "", "i-query", http.StatusOK},
		{"path", base + "/latest/meta-data/instance-id/i-path", "", "i-path", http.StatusOK},
		{"header", base + "/latest/meta-data/instance-id", "i-header", "i-header", http.StatusOK},
		{"missing", base + "/latest/meta-data/instance-id", "", "instance ID is required\n", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, tc.url, nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.header != "" {
				req.Header.Set("X-Instance-ID", tc.header)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != tc.code || string(body) != tc.want {
				t.Fatalf("response = %d %q, want %d %q", resp.StatusCode, body, tc.code, tc.want)
			}
		})
	}
}

func TestMetadataServerConcurrentRegistration(t *testing.T) {
	s := NewMetadataServer()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "i-" + strconv.Itoa(i)
			s.RegisterPayload(id, []byte(strings.Repeat("x", i)))
		}(i)
	}
	wg.Wait()

	if err := s.Start(0); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	for i := 0; i < 100; i++ {
		resp, err := http.Get("http://" + s.Addr() + "/latest/user-data?instance=" + url.QueryEscape("i-"+strconv.Itoa(i)))
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil || resp.StatusCode != http.StatusOK || len(body) != i {
			t.Fatalf("instance %d: status=%d len=%d err=%v", i, resp.StatusCode, len(body), readErr)
		}
	}
}
