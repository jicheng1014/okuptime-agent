package update

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func mockHTTP(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	previous := http.DefaultTransport
	http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		handler(w, r)
		return w.Result(), nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
}

func signedRelease(t *testing.T, data []byte) (*Metadata, string, ed25519.PrivateKey) {
	t.Helper()
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	m := &Metadata{Version: "1.2.0", Platform: runtime.GOOS, Architecture: runtime.GOARCH, URL: "http://localhost/binary", SHA256: hex.EncodeToString(hash[:]), Size: int64(len(data))}
	m.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(private, m.signedPayload()))
	return m, base64.StdEncoding.EncodeToString(der), private
}

func TestVersionComparison(t *testing.T) {
	for _, tc := range []struct {
		current, candidate string
		newer, invalid     bool
	}{
		{"v1.0.0", "1.0.1", true, false}, {"1.10.0", "1.2.0", false, false}, {"1.2.0", "v1.2.0", false, false},
		{"1.0.0", "2.0.0", true, false}, {"dev", "1.0.0", false, true}, {"1.0.0", "1.0", false, true}, {"1.0.0", "1.0.+1", false, true}, {"1.0.0", "1.01.1", false, true}, {"1.0.0", "4294967296.0.0", false, true},
	} {
		got, err := newer(tc.current, tc.candidate)
		if got != tc.newer || (err != nil) != tc.invalid {
			t.Errorf("%+v: %v %v", tc, got, err)
		}
	}
}

func TestPublicCheck(t *testing.T) {
	t.Setenv("OKUPTIME_BASE_URL", "http://localhost")
	t.Setenv("OKUPTIME_TOKEN", "invalid")
	m, _, _ := signedRelease(t, []byte("new"))
	noRelease := false
	mockHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/cli/releases/latest" || r.URL.Query().Get("platform") != runtime.GOOS || r.URL.Query().Get("architecture") != runtime.GOARCH || r.Header.Get("Authorization") != "" {
			t.Fatalf("wrong public check: %s", r.URL)
		}
		if noRelease {
			w.WriteHeader(404)
			fmt.Fprint(w, `{"error":{"code":"release_unavailable","message":"none"}}`)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": m})
	})
	for _, version := range []string{"1.0.0", "dev"} {
		result, err := Check(version, time.Second)
		if err != nil || !result.Available {
			t.Fatalf("%s: %+v %v", version, result, err)
		}
	}
	noRelease = true
	result, err := Check("1.0.0", time.Second)
	if err != nil || result.Release != nil || result.Available {
		t.Fatalf("404: %+v %v", result, err)
	}
}

func TestApplyAndFailures(t *testing.T) {
	for _, test := range []string{"success", "symlink", "bad-signature", "bad-sha", "short", "long", "oversize", "foreign-host", "redirect-host", "redirect-http", "platform", "not-newer", "bad-key"} {
		t.Run(test, func(t *testing.T) {
			t.Setenv("OKUPTIME_BASE_URL", "http://localhost")
			data := []byte("new-binary")
			m, key, private := signedRelease(t, data)
			dir := t.TempDir()
			executable := filepath.Join(dir, "okuptime")
			if err := os.WriteFile(executable, []byte("old-binary"), 0755); err != nil {
				t.Fatal(err)
			}
			path := executable
			current := "1.0.0"
			switch test {
			case "symlink":
				path = filepath.Join(dir, "link")
				if err := os.Symlink(executable, path); err != nil {
					if runtime.GOOS == "windows" {
						t.Skip("Windows symlinks require developer mode or administrator privileges")
					}
					t.Fatal(err)
				}
			case "bad-signature":
				m.Signature = base64.StdEncoding.EncodeToString(make([]byte, 64))
			case "bad-sha":
				data = []byte("wrong-data")
			case "short":
				data = data[:len(data)-1]
			case "long":
				data = append(data, 'x')
			case "oversize":
				m.Size = 64<<20 + 1
			case "foreign-host":
				m.URL = "https://evil.example/binary"
			case "platform":
				m.Platform = "unsupported"
			case "not-newer":
				current = m.Version
			case "bad-key":
				key = "invalid"
			case "redirect-http":
				t.Setenv("OKUPTIME_BASE_URL", "https://localhost")
				m.URL = "https://localhost/binary"
				m.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(private, m.signedPayload()))
			}
			requests := 0
			mockHTTP(t, func(w http.ResponseWriter, r *http.Request) {
				requests++
				if test == "redirect-host" {
					http.Redirect(w, r, "https://evil.example/binary", 302)
					return
				}
				if test == "redirect-http" {
					http.Redirect(w, r, "http://localhost/binary", 302)
					return
				}
				w.Write(data)
			})
			_, err := applyTo(current, key, m, path, false)
			got, _ := os.ReadFile(executable)
			if test == "success" || test == "symlink" {
				if err != nil || string(got) != "new-binary" {
					t.Fatalf("apply: %s %v", got, err)
				}
				old, err := os.ReadFile(executable + ".previous")
				if err != nil || string(old) != "old-binary" {
					t.Fatalf("backup: %s %v", old, err)
				}
				info, _ := os.Stat(executable)
				if runtime.GOOS != "windows" && info.Mode().Perm() != 0755 {
					t.Fatal("lost executable permissions")
				}
				if test == "symlink" {
					info, err := os.Lstat(path)
					if err != nil || info.Mode()&os.ModeSymlink == 0 {
						t.Fatal("replaced symlink instead of target")
					}
				}
			} else {
				if err == nil || string(got) != "old-binary" {
					t.Fatalf("unsafe failure: %s %v", got, err)
				}
				if _, err := os.Stat(executable + ".previous"); !os.IsNotExist(err) {
					t.Fatal("invalid download touched backup")
				}
				if strings.HasPrefix(test, "redirect") && requests != 1 {
					t.Fatalf("followed unsafe redirect: %d", requests)
				}
			}
		})
	}
}

func TestPermissionError(t *testing.T) {
	err := replacementError(os.ErrPermission)
	if !strings.Contains(err.Error(), "权限") || !strings.Contains(err.Error(), "sudo") {
		t.Fatal(err)
	}
}

func TestNotifyRecoversStaleLock(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("OKUPTIME_BASE_URL", "http://localhost")
	dir, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	dir = filepath.Join(dir, "okuptime")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(dir, "update-check.lock")
	if err := os.WriteFile(lock, nil, 0600); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lock, past, past); err != nil {
		t.Fatal(err)
	}
	calls := 0
	mockHTTP(t, func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(404) })
	Notify("1.0.0", io.Discard)
	Notify("1.0.0", io.Discard)
	if calls != 1 {
		t.Fatalf("stale lock prevented daily check: %d", calls)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatal("lock not removed")
	}
}

func TestWindowsStaging(t *testing.T) {
	t.Setenv("OKUPTIME_BASE_URL", "http://localhost")
	data := []byte("verified-binary")
	m, key, _ := signedRelease(t, data)
	executable := filepath.Join(t.TempDir(), "okuptime.exe")
	if err := os.WriteFile(executable, []byte("old-binary"), 0755); err != nil {
		t.Fatal(err)
	}
	mockHTTP(t, func(w http.ResponseWriter, r *http.Request) { w.Write(data) })
	executable, err := filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := applyTo("1.0.0", key, m, executable, true)
	if err != nil || staged != executable+".pending.exe" {
		t.Fatalf("stage: %s %v", staged, err)
	}
	old, _ := os.ReadFile(executable)
	pending, _ := os.ReadFile(staged)
	if string(old) != "old-binary" || string(pending) != string(data) {
		t.Fatalf("stage replaced running binary: %s %s", old, pending)
	}
	if _, err := os.Stat(executable + ".previous"); !os.IsNotExist(err) {
		t.Fatal("staging touched backup")
	}
	command := FinalizeCommand(`C:\Users\O'Brien\OK Uptime\okuptime.exe.pending.exe`, m.SHA256)
	for _, part := range []string{"O''Brien", "Get-FileHash -LiteralPath", m.SHA256, "Copy-Item -LiteralPath", "Move-Item -LiteralPath", "catch", "version --json"} {
		if !strings.Contains(command, part) {
			t.Fatalf("missing %s in command: %s", part, command)
		}
	}
}

func TestWindowsFinalizeCommand(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell finalization requires native Windows")
	}
	dir := filepath.Join(t.TempDir(), "O'Brien")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "okuptime.exe")
	staged := target + ".pending.exe"
	if err := os.WriteFile(target, []byte("old-binary"), 0600); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", staged, "-ldflags", "-X main.version=1.2.0", "../../cmd/okuptime")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fixture: %v %s", err, output)
	}
	verified, err := os.ReadFile(staged)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(verified)
	finish := func(checksum string) ([]byte, error) {
		return exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", FinalizeCommand(staged, checksum)).CombinedOutput()
	}
	output, err := finish(hex.EncodeToString(hash[:]))
	if err != nil || !strings.Contains(string(output), "1.2.0") {
		t.Fatalf("finalize: %v %s", err, output)
	}
	got, _ := os.ReadFile(target)
	previous, _ := os.ReadFile(target + ".previous")
	if string(got) != string(verified) || string(previous) != "old-binary" {
		t.Fatal("finalize failed to preserve old binary")
	}
	if err := os.WriteFile(staged, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := finish(hex.EncodeToString(hash[:])); err == nil {
		t.Fatalf("bad checksum accepted: %s", output)
	}
	got, _ = os.ReadFile(target)
	if string(got) != string(verified) {
		t.Fatal("checksum failure changed installed CLI")
	}
	corruptHash := sha256.Sum256([]byte("corrupt"))
	if output, err := finish(hex.EncodeToString(corruptHash[:])); err == nil {
		t.Fatalf("invalid executable accepted: %s", output)
	}
	got, _ = os.ReadFile(target)
	if string(got) != string(verified) {
		t.Fatal("failed executable did not roll back")
	}
}
