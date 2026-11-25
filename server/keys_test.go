package server

import (
	"os"
	"path/filepath"
	"testing"
)

// 密钥目录可以不在可执行文件旁边（Docker 形态把它挂在独立的数据卷上）：首次启动要能自己
// 把目录建出来并在里面生成 jwt.key，之后沿用同一把；数据目录里不留任何密钥文件。
func TestJWTKeyLivesInKeyDirOnly(t *testing.T) {
	keyDir := filepath.Join(t.TempDir(), "keys", "nested") // 故意不存在
	dataDir := t.TempDir()

	k1, err := loadOrCreateJWTKey(keyDir, dataDir)
	if err != nil || len(k1) != 32 {
		t.Fatalf("首次加载：len=%d err=%v", len(k1), err)
	}
	if _, err := os.Stat(filepath.Join(keyDir, "jwt.key")); err != nil {
		t.Fatalf("jwt.key 没有写进密钥目录：%v", err)
	}
	k2, err := loadOrCreateJWTKey(keyDir, dataDir)
	if err != nil || string(k2) != string(k1) {
		t.Fatalf("第二次加载应沿用同一把密钥：err=%v", err)
	}
	if entries, err := os.ReadDir(dataDir); err != nil || len(entries) != 0 {
		t.Fatalf("数据目录里不应出现密钥文件：entries=%v err=%v", entries, err)
	}
}

// 旧版本把密钥放在数据目录里；换到还不存在的密钥目录时要把旧密钥带过去，而不是另生成一把。
func TestJWTKeyMigratesIntoFreshKeyDir(t *testing.T) {
	dataDir := t.TempDir()
	keyDir := filepath.Join(t.TempDir(), "keys")
	legacy := []byte("0123456789abcdef0123456789abcdef")
	if err := os.WriteFile(filepath.Join(dataDir, "jwt.key"), legacy, 0o600); err != nil {
		t.Fatal(err)
	}

	k, err := loadOrCreateJWTKey(keyDir, dataDir)
	if err != nil || string(k) != string(legacy) {
		t.Fatalf("应迁移旧密钥：err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "jwt.key")); !os.IsNotExist(err) {
		t.Fatalf("数据目录里的旧密钥应被移除：err=%v", err)
	}
}

func TestKeyDirWarning(t *testing.T) {
	base := t.TempDir()
	data := filepath.Join(base, "data")
	cases := []struct {
		name        string
		keyDir      string
		wantWarning bool
	}{
		{"密钥目录就是数据目录", data, true},
		{"密钥目录在数据目录之下", filepath.Join(data, "keys"), true},
		{"默认布局：密钥目录是数据目录的上级", base, false},
		{"互不相关", filepath.Join(base, "elsewhere"), false},
	}
	for _, c := range cases {
		if got := keyDirWarning(c.keyDir, data) != ""; got != c.wantWarning {
			t.Errorf("%s：告警=%v，期望 %v", c.name, got, c.wantWarning)
		}
	}
	if keyDirWarning("", data) != "" || keyDirWarning(base, "") != "" {
		t.Errorf("任一目录为空时不告警")
	}
}
