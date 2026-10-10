package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/jicheng1014/okuptime-agent/internal/config"
)

func TestCLI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
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
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0600) {
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
		case "POST /api/v1/projects":
			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload["name"] != "Demo" {
				t.Errorf("wrong project payload: %v, %v", payload, err)
			}
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"data":{"id":2,"name":"Demo"}}`)
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
		{"project", "create", "Demo", "--json"},
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

func TestTokenAuthorizationGuidance(t *testing.T) {
	for _, tc := range []struct {
		name, base, token, body, code string
		status                        int
		jsonOutput, hint              bool
	}{
		{"missing official", "https://www.okuptime.com", "", "", "config_error", 0, true, true},
		{"invalid local", "http://localhost:3000", "invalid", "", "config_error", 0, true, true},
		{"expired custom", "https://staging.example.com", strings.Repeat("a", 64), `{"error":{"code":"unauthorized","message":"expired","details":{"reason":"revoked"}}}`, "unauthorized", 401, true, true},
		{"plain missing", "https://www.okuptime.com", "", "", "config_error", 0, false, true},
		{"json expired without API envelope", "http://localhost:3000", strings.Repeat("a", 64), "Unauthorized", "http_error", 401, true, true},
		{"plain expired", "http://localhost:3000", strings.Repeat("a", 64), "Unauthorized", "http_error", 401, false, true},
		{"other error", "https://www.okuptime.com", strings.Repeat("a", 64), `{"error":{"code":"rate_limited","message":"wait"}}`, "rate_limited", 429, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("APPDATA", t.TempDir())
			t.Setenv("OKUPTIME_BASE_URL", tc.base)
			t.Setenv("OKUPTIME_TOKEN", tc.token)
			previous := http.DefaultTransport
			calls := 0
			http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				w := httptest.NewRecorder()
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
				return w.Result(), nil
			})
			t.Cleanup(func() { http.DefaultTransport = previous })
			args := []string{"project", "list"}
			if tc.jsonOutput {
				args = append(args, "--json")
			}
			var out, errOut bytes.Buffer
			if status := Run(args, strings.NewReader(""), &out, &errOut); status != 1 {
				t.Fatalf("expected failure, got %d", status)
			}
			tokenURL := tc.base + "/api_tokens"
			if tc.jsonOutput {
				var response struct {
					Error struct {
						Code, Message string
						Details       map[string]string
					}
				}
				if err := json.Unmarshal(out.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if response.Error.Code != tc.code || errOut.Len() != 0 {
					t.Fatalf("unexpected error: %s %s", &out, &errOut)
				}
				if tc.hint && response.Error.Details["token_url"] != tokenURL {
					t.Fatalf("missing token URL: %s", &out)
				}
				if !tc.hint && response.Error.Details["token_url"] != "" {
					t.Fatalf("unrelated error has token hint: %s", &out)
				}
				if tc.name == "expired custom" && response.Error.Details["reason"] != "revoked" {
					t.Fatalf("lost server details: %s", &out)
				}
			} else if out.Len() != 0 || !strings.Contains(errOut.String(), tokenURL) || !strings.Contains(errOut.String(), "okuptime config set-token") {
				t.Fatalf("missing terminal guidance: %s %s", &out, &errOut)
			}
			if tc.status == 0 && calls != 0 {
				t.Fatal("missing token made a network request")
			}
			if len(tc.token) == 64 && strings.Contains(out.String()+errOut.String(), tc.token) {
				t.Fatal("token leaked")
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestVersionUpdateAndDailyHint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("OKUPTIME_TOKEN", "")
	t.Setenv("OKUPTIME_BASE_URL", "http://localhost")
	previous := http.DefaultTransport
	checks := 0
	failCheck := false
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		if r.URL.Path == "/api/v1/cli/releases/latest" {
			checks++
			if r.Header.Get("Authorization") != "" {
				t.Fatal("release check must not send token")
			}
			if failCheck {
				w.WriteHeader(503)
				fmt.Fprint(w, `{}`)
			} else {
				fmt.Fprintf(w, `{"data":{"version":"1.1.0","platform":%q,"architecture":%q,"url":"http://localhost/binary","sha256":%q,"size":1}}`, runtime.GOOS, runtime.GOARCH, strings.Repeat("a", 64))
			}
		} else {
			fmt.Fprint(w, `{"data":[]}`)
		}
		return w.Result(), nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	var out, errOut bytes.Buffer
	for _, args := range [][]string{{"version", "--json"}, {"update", "--check", "--json"}} {
		out.Reset()
		if status := RunWithBuildInfo(args, strings.NewReader(""), &out, &errOut, "dev", ""); status != 0 || !json.Valid(out.Bytes()) {
			t.Fatalf("%v: %d %s %s", args, status, out.String(), errOut.String())
		}
	}
	out.Reset()
	if status := RunWithBuildInfo([]string{"update", "--json"}, strings.NewReader(""), &out, &errOut, "dev", ""); status != 1 || !strings.Contains(out.String(), "签名") {
		t.Fatalf("unsigned build installed: %d %s", status, out.String())
	}
	t.Setenv("OKUPTIME_TOKEN", strings.Repeat("a", 64))
	checks = 0
	for i := 0; i < 2; i++ {
		out.Reset()
		if status := RunWithBuildInfo([]string{"project", "list", "--json"}, strings.NewReader(""), &out, &errOut, "1.0.0", ""); status != 0 || !json.Valid(out.Bytes()) {
			t.Fatalf("business failed: %d %s", status, out.String())
		}
	}
	if checks != 1 || !strings.Contains(errOut.String(), "okuptime update") {
		t.Fatalf("daily check: %d %s", checks, errOut.String())
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	checks = 0
	failCheck = true
	errOut.Reset()
	for i := 0; i < 2; i++ {
		out.Reset()
		if status := RunWithBuildInfo([]string{"project", "list", "--json"}, strings.NewReader(""), &out, &errOut, "1.0.0", ""); status != 0 || !json.Valid(out.Bytes()) {
			t.Fatal("update failure broke business")
		}
	}
	if checks != 1 || errOut.Len() != 0 {
		t.Fatalf("failed daily check: %d %s", checks, errOut.String())
	}
}
