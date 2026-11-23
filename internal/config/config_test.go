package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// 回归：Save 以前用固定的 path+".tmp" 再 rename。两次保存并发时后一次
// rename 会碰到 ENOENT，前端就弹「配置文件写入失败」。改成唯一临时文件后
// 并发保存必须全部成功且落盘内容合法。
func TestSaveConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Default()
	cfg.path = path

	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := cfg.Save(); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("并发保存失败: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	var got Config
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("落盘内容不是合法 JSON: %v", err)
	}
	if got.Addr == "" {
		t.Fatal("落盘内容缺少 addr")
	}
	// 临时文件不应残留。
	if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".config-*.json")); len(leftovers) != 0 {
		t.Fatalf("临时文件残留: %v", leftovers)
	}
}
