package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"
)

type fileConfig struct {
	Token string `json:"token"`
}

var ErrInvalidToken = errors.New("访问令牌必须是 64 位小写十六进制字符")

func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "okuptime", "config.json"), nil
}

func Token() (string, error) {
	if token := os.Getenv("OKUPTIME_TOKEN"); token != "" {
		return token, nil
	}
	path, err := Path()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var saved fileConfig
	if err := json.Unmarshal(data, &saved); err != nil {
		return "", fmt.Errorf("配置文件格式无效: %s", path)
	}
	return saved.Token, nil
}

func SaveToken(in io.Reader, prompt io.Writer) error {
	var data []byte
	var err error
	if in == os.Stdin && term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(prompt, "访问令牌: ")
		data, err = term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(prompt)
	} else {
		data, err = io.ReadAll(io.LimitReader(in, 129))
	}
	if err != nil {
		return err
	}
	token := strings.TrimSpace(string(data))
	if !ValidToken(token) {
		return ErrInvalidToken
	}
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if err := json.NewEncoder(tmp).Encode(fileConfig{Token: token}); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func ValidToken(token string) bool {
	if len(token) != 64 {
		return false
	}
	for _, char := range token {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
