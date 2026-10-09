package modulesdk

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigWithoutHostFileSaysSo(t *testing.T) {
	t.Setenv(ConfigFileEnv, "")
	if _, err := ModuleConfig(); !errors.Is(err, ErrConfigNotProvided) {
		t.Fatalf("ModuleConfig() = %v, want ErrConfigNotProvided", err)
	}
	var target struct{}
	if err := LoadModuleConfig(&target); !errors.Is(err, ErrConfigNotProvided) {
		t.Fatalf("LoadModuleConfig() = %v, want ErrConfigNotProvided", err)
	}
}

func TestConfigReadsTheEffectiveFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "io.example.camera.effective.json")
	if err := os.WriteFile(path, []byte(`{"camera_url":"rtsp://10.0.0.5/stream","fps":15}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ConfigFileEnv, path)

	values, err := ModuleConfig()
	if err != nil {
		t.Fatal(err)
	}
	if values["camera_url"] != "rtsp://10.0.0.5/stream" || values["fps"] != float64(15) {
		t.Fatalf("ModuleConfig() = %v", values)
	}

	var typed struct {
		CameraURL string `json:"camera_url"`
		FPS       int    `json:"fps"`
	}
	if err := LoadModuleConfig(&typed); err != nil {
		t.Fatal(err)
	}
	if typed.CameraURL != "rtsp://10.0.0.5/stream" || typed.FPS != 15 {
		t.Fatalf("LoadModuleConfig() = %+v", typed)
	}

	var renamed struct {
		CameraURL string `json:"camera"`
	}
	if err := LoadModuleConfig(&renamed); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("a field the file does not match must fail loudly, got %v", err)
	}
}
