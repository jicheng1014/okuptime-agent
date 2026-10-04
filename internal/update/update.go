package update

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/jicheng1014/okuptime-agent/internal/api"
)

type Metadata struct {
	Version      string `json:"version"`
	Platform     string `json:"platform"`
	Architecture string `json:"architecture"`
	URL          string `json:"url"`
	SHA256       string `json:"sha256"`
	Size         int64  `json:"size"`
	Signature    string `json:"signature"`
	ReleasedAt   string `json:"released_at"`
	Notes        string `json:"notes"`
}

type Result struct {
	CurrentVersion string    `json:"current_version"`
	Available      bool      `json:"available"`
	Updated        bool      `json:"updated"`
	Release        *Metadata `json:"release"`
}

func Check(current string, timeout time.Duration) (*Result, error) {
	result := &Result{CurrentVersion: current}
	client, err := api.NewPublic(timeout)
	if err != nil {
		return nil, err
	}
	body, err := client.Do(http.MethodGet, "/cli/releases/latest", url.Values{"platform": {runtime.GOOS}, "architecture": {runtime.GOARCH}}, nil)
	if err != nil {
		var apiErr *api.Error
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
			return result, nil
		}
		return nil, err
	}
	var response struct {
		Data *Metadata `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.Data == nil {
		return nil, fmt.Errorf("无效的 CLI 发布信息")
	}
	if _, err := response.Data.validate(); err != nil {
		return nil, err
	}
	result.Release = response.Data
	if current == "dev" {
		result.Available = true
	} else {
		result.Available, err = newer(current, response.Data.Version)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func newer(current, candidate string) (bool, error) {
	parse := func(value string) ([3]uint64, error) {
		var numbers [3]uint64
		parts := strings.Split(strings.TrimPrefix(value, "v"), ".")
		if len(parts) != 3 {
			return numbers, fmt.Errorf("无效版本号: %s", value)
		}
		for i, part := range parts {
			if part == "" || (len(part) > 1 && part[0] == '0') || strings.IndexFunc(part, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
				return numbers, fmt.Errorf("无效版本号: %s", value)
			}
			number, err := strconv.ParseUint(part, 10, 32)
			if err != nil {
				return numbers, fmt.Errorf("无效版本号: %s", value)
			}
			numbers[i] = number
		}
		return numbers, nil
	}
	a, err := parse(current)
	if err != nil {
		return false, err
	}
	b, err := parse(candidate)
	if err != nil {
		return false, err
	}
	for i := range a {
		if a[i] != b[i] {
			return b[i] > a[i], nil
		}
	}
	return false, nil
}

func trustedURL(value string) error {
	base, err := api.BaseURL()
	if err != nil {
		return err
	}
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || !strings.EqualFold(u.Hostname(), base.Hostname()) || (u.Scheme != "https" && !(u.Scheme == "http" && base.Scheme == "http")) {
		return fmt.Errorf("更新下载必须使用官网同域 HTTPS 地址（仅本机开发允许 HTTP）")
	}
	return nil
}

func (m *Metadata) validate() ([]byte, error) {
	if m.Platform != runtime.GOOS || m.Architecture != runtime.GOARCH {
		return nil, fmt.Errorf("更新包平台不匹配")
	}
	if _, err := newer("0.0.0", m.Version); err != nil {
		return nil, err
	}
	if m.Size < 1 || m.Size > 64<<20 {
		return nil, fmt.Errorf("无效更新包大小，最大 64 MiB")
	}
	if err := trustedURL(m.URL); err != nil {
		return nil, err
	}
	checksum, err := hex.DecodeString(m.SHA256)
	if err != nil || len(checksum) != sha256.Size {
		return nil, fmt.Errorf("无效更新包 SHA256")
	}
	return checksum, nil
}

func (m *Metadata) signedPayload() []byte {
	return []byte(strings.Join([]string{m.Version, m.Platform, m.Architecture, m.URL, m.SHA256, strconv.FormatInt(m.Size, 10), ""}, "\n"))
}

func Apply(current, publicKey string, release *Metadata) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("Windows 暂不支持原位更新，请手动安装")
	}
	if publicKey == "" || current == "dev" {
		return fmt.Errorf("开发构建未配置正式更新签名，允许检查版本但拒绝安装；请安装官方签名构建")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	return applyTo(current, publicKey, release, executable)
}

func applyTo(current, encodedKey string, release *Metadata, executable string) error {
	if release == nil {
		return fmt.Errorf("缺少更新发布信息")
	}
	checksum, err := release.validate()
	if err != nil {
		return err
	}
	isNewer, err := newer(current, release.Version)
	if err != nil || !isNewer {
		return fmt.Errorf("更新版本必须高于当前版本")
	}
	der, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil {
		return fmt.Errorf("无效更新公钥")
	}
	parsedKey, err := x509.ParsePKIXPublicKey(der)
	key, ok := parsedKey.(ed25519.PublicKey)
	if err != nil || !ok {
		return fmt.Errorf("无效更新公钥")
	}
	signature, err := base64.StdEncoding.DecodeString(release.Signature)
	if err != nil || !ed25519.Verify(key, release.signedPayload(), signature) {
		return fmt.Errorf("更新签名校验失败")
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return err
	}
	info, err := os.Lstat(executable)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("CLI 可执行文件必须是普通文件")
	}
	tmp, err := os.CreateTemp(filepath.Dir(executable), ".okuptime-update-*")
	if err != nil {
		return replacementError(err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	client := &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("更新下载重定向过多")
		}
		return trustedURL(req.URL.String())
	}}
	resp, err := client.Get(release.URL)
	if err != nil {
		return fmt.Errorf("更新下载失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("更新下载 HTTP %d", resp.StatusCode)
	}
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(resp.Body, release.Size+1))
	if err != nil {
		return fmt.Errorf("更新下载失败: %w", err)
	}
	if n != release.Size {
		return fmt.Errorf("更新包大小校验失败")
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), hex.EncodeToString(checksum)) {
		return fmt.Errorf("更新包 SHA256 校验失败")
	}
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		return replacementError(err)
	}
	if err := tmp.Sync(); err != nil {
		return replacementError(err)
	}
	if err := tmp.Close(); err != nil {
		return replacementError(err)
	}
	// Preserve the running binary before an atomic rename over its original path.
	if err := backup(executable, info.Mode().Perm()); err != nil {
		return replacementError(err)
	}
	if err := os.Rename(tmp.Name(), executable); err != nil {
		return replacementError(err)
	}
	return nil
}

func backup(executable string, mode os.FileMode) error {
	old, err := os.Open(executable)
	if err != nil {
		return err
	}
	defer old.Close()
	tmp, err := os.CreateTemp(filepath.Dir(executable), ".okuptime-previous-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if _, err := io.Copy(tmp, old); err != nil {
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), executable+".previous")
}

func replacementError(err error) error {
	if errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("更新目录或文件无写入权限，请由有权限的用户手动安装；不会自动使用 sudo: %w", err)
	}
	return fmt.Errorf("更新替换失败，当前可执行文件保持原状: %w", err)
}

// Notify only checks metadata; installation always requires the update command.
func Notify(current string, out io.Writer) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return
	}
	dir = filepath.Join(dir, "okuptime")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return
	}
	path := filepath.Join(dir, "update-check")
	if info, err := os.Stat(path + ".lock"); err == nil && time.Since(info.ModTime()) > time.Minute {
		_ = os.Remove(path + ".lock")
	}
	lock, err := os.OpenFile(path+".lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return
	}
	lock.Close()
	defer os.Remove(path + ".lock")
	if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) < 24*time.Hour {
		return
	}
	// Record attempts too, so offline commands do not retry all day.
	if err := os.WriteFile(path, nil, 0600); err != nil {
		return
	}
	result, err := Check(current, 2*time.Second)
	if err == nil && result.Available {
		fmt.Fprintf(out, "Okuptime 新版本 %s 可用；运行 okuptime update 安装（当前 %s）\n", result.Release.Version, current)
	}
}
