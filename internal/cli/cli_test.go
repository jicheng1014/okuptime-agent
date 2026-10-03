package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jicheng1014/okuptime-agent/internal/config"
)

func TestCLI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OKUPTIME_TOKEN", "")
	token := strings.Repeat("a", 64)
	var out, errOut bytes.Buffer
	if err := run([]string{"config", "set-token"}, strings.NewReader(token+"\n"), &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), token) {
		t.Fatal("token leaked to output")
	}
	path, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("config permissions: %v, %v", info, err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("missing bearer token")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error":{"code":"unauthorized","message":"bad token"}}`)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/projects":
			fmt.Fprint(w, `{"data":[{"id":1,"name":"Default"}]}`)
		case "POST /api/v1/monitors":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload["url"] != "https://example.com" || payload["project_id"] != float64(1) {
				t.Errorf("wrong payload: %v, %v", payload, err)
			}
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"data":{"id":3,"url":"https://example.com"}}`)
		case "GET /api/v1/monitors":
			if r.URL.Query().Get("after_id") != "3" || r.URL.Query().Has("project_id") {
				t.Errorf("unexpected global list query: %s", r.URL.RawQuery)
			}
			fmt.Fprint(w, `{"data":[{"id":4,"url":"https://example.com","check_status":"ok"}],"next_cursor":null}`)
		case "GET /api/v1/projects/1/monitors":
			if r.URL.Query().Get("after_id") != "3" {
				t.Errorf("missing project cursor: %s", r.URL.RawQuery)
			}
			fmt.Fprint(w, `{"data":[{"id":4,"url":"https://example.com","check_status":"ok"}],"next_cursor":4}`)
		case "GET /api/v1/monitors/3":
			fmt.Fprint(w, `{"data":{"id":3,"url":"https://example.com","check_status":"checking"}}`)
		case "POST /api/v1/monitors/3/check":
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprint(w, `{"data":{"id":3,"check_status":"checking"}}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	previousTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)
		return recorder.Result(), nil
	})
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	t.Setenv("OKUPTIME_BASE_URL", "http://localhost")

	for _, args := range [][]string{
		{"project", "list"},
		{"monitor", "add", "--project-id", "1", "https://example.com", "--json"},
		{"monitor", "list", "--after-id", "3"},
		{"project", "monitors", "1", "--after-id", "3"},
		{"monitor", "show", "3"},
		{"monitor", "check", "3", "--json"},
	} {
		out.Reset()
		errOut.Reset()
		if err := run(args, strings.NewReader(""), &out, &errOut); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if out.Len() == 0 {
			t.Fatalf("%v: no output", args)
		}
		if args[0] == "project" && args[1] == "monitors" && !strings.Contains(errOut.String(), "okuptime project monitors 1 --after-id 4") {
			t.Fatalf("next page lost project path: %s", errOut.String())
		}
	}
	if err := run([]string{"project", "monitors", "0"}, strings.NewReader(""), &out, &errOut); err == nil {
		t.Fatal("expected invalid project ID to fail")
	}
	if !strings.Contains(out.String(), `"check_status": "checking"`) {
		t.Fatalf("check should report queued status: %s", out.String())
	}
}

func TestRejectInsecureRemoteBaseURL(t *testing.T) {
	t.Setenv("OKUPTIME_TOKEN", strings.Repeat("a", 64))
	t.Setenv("OKUPTIME_BASE_URL", "http://example.com")
	_, err := request(http.MethodGet, "/projects", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "HTTPS") || !strings.Contains(err.Error(), "本机") {
		t.Fatalf("expected base URL rejection, got %v", err)
	}
	var out, errOut bytes.Buffer
	if status := Run([]string{"project", "list", "--json"}, strings.NewReader(""), &out, &errOut); status != 1 {
		t.Fatalf("expected failure exit status, got %d", status)
	}
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil || response.Error.Code != "config_error" || errOut.Len() != 0 {
		t.Fatalf("expected machine-readable error, got stdout=%s stderr=%s: %v", out.String(), errOut.String(), err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
