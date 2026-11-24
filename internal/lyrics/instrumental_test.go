package lyrics

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsInstrumentalText(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"纯音乐，请欣赏", true},
		{"[纯音乐，请欣赏]", true},
		{"[qq:]\r\n[total:320317]\r\n\n[00:01.58]纯音乐，请欣赏", true},
		{"[00:01.58]纯音乐，请欣赏", true},
		{"[00:01.00]纯音乐", true},
		{"暂无歌词", true},
		{"Instrumental", true},
		{"", false},
		// 真歌词里出现“纯音乐”字样不算占位
		{"我们在这纯音乐的世界里跳舞 一直到天亮 一直到你累了为止 你说过不会走", false},
		{"我独饮晚风作酒 叹一生痴情入喉 饮不尽红尘的泪 又怎能一醉方休 你用那半盏浮生", false},
	}
	for _, c := range cases {
		if got := isInstrumentalText(c.text); got != c.want {
			t.Errorf("isInstrumentalText(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestMigrateNoLyricCache(t *testing.T) {
	dir := t.TempDir()
	lyrDir := filepath.Join(dir, "lyrics")
	if err := os.MkdirAll(lyrDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 一条占位“歌词”（老版本写下的）
	placeholder := `{"format":"plain","synced":false,"source":"kugou","cues":[{"s":0,"e":0,"t":"纯音乐，请欣赏"}]}`
	if err := os.WriteFile(filepath.Join(lyrDir, "abc123.json"), []byte(placeholder), 0o644); err != nil {
		t.Fatal(err)
	}
	// 一条真歌词，必须原样保留
	real := `{"format":"lrc","synced":true,"source":"kugou","cues":[{"s":1,"e":2,"t":"我独饮晚风作酒"},{"s":2,"e":3,"t":"叹一生痴情入喉"}]}`
	if err := os.WriteFile(filepath.Join(lyrDir, "def456.json"), []byte(real), 0o644); err != nil {
		t.Fatal(err)
	}

	c := NewClient("", dir, true)
	if n := c.MigrateNoLyricCache(); n != 1 {
		t.Fatalf("迁移条数 = %d, want 1", n)
	}
	if _, err := os.Stat(filepath.Join(lyrDir, "abc123.json")); err == nil {
		t.Error("占位歌词缓存应该被删掉")
	}
	nl, err := c.ReadNoLyric("abc123")
	if err != nil || nl == nil {
		t.Fatalf("结论文件没写出来: %v", err)
	}
	if nl.Reason != "instrumental" || nl.Note != NoLyricNote {
		t.Errorf("结论内容不对: %+v", nl)
	}
	if _, err := os.Stat(filepath.Join(lyrDir, "def456.json")); err != nil {
		t.Error("真歌词缓存不该被删")
	}
	if _, err := c.ReadNoLyric("def456"); err == nil {
		t.Error("真歌词不该被标成纯音乐")
	}
}

func TestInstrumentalDocIsNotLyric(t *testing.T) {
	doc := InstrumentalDoc("潇湘水云", "巫娜")
	if !doc.Instrumental || len(doc.Cues) != 0 {
		t.Fatalf("纯音乐文档不该带歌词行: %+v", doc)
	}
	if doc.Note != NoLyricNote {
		t.Errorf("Note = %q, want %q", doc.Note, NoLyricNote)
	}
}
