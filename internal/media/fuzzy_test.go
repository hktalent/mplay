package media

import "testing"

func TestFuzzyScoreBasics(t *testing.T) {
	cases := []struct {
		q      string
		fields []string
		want   bool
	}{
		{"稻香", []string{"稻香", "周杰伦"}, true},
		{"稻香", []string{"周杰伦"}, false},
		{"zhou", []string{"Zhou Jielun"}, true},     // 大小写不敏感
		{"稻香 周杰伦", []string{"稻香", "周杰伦"}, true},     // 多词乱序、跨字段
		{"周杰伦 稻香 专辑", []string{"稻香", "周杰伦"}, false}, // 有一词不命中就整体不命中
		{"周稻", []string{"周杰伦 - 稻香"}, true},          // 松散命中
		{"周稻", []string{"周杰伦", "稻香"}, true},         // 跨字段也允许
		{"稻周", []string{"周杰伦 - 稻香"}, false},         // 顺序不能反
		{"", []string{"任意"}, true},                  // 空查询视为全命中
		{"xyz", []string{""}, false},                // 没有可比字段
	}
	for _, c := range cases {
		if got := FuzzyMatch(c.q, c.fields...); got != c.want {
			t.Errorf("FuzzyMatch(%q, %v) = %v, want %v", c.q, c.fields, got, c.want)
		}
	}
}

// 松散命中要限制间隔，否则一个词的首尾字符就能命中整库里的任意条目。
func TestFuzzyGapLimit(t *testing.T) {
	far := "周" + "华语男歌手经典歌曲合集" + "稻" // 中间隔了 10 个字符以上
	if FuzzyMatch("周稻", far) {
		t.Fatalf("间隔过大不应命中: %q", far)
	}
	near := "周" + "杰伦" + "稻"
	if !FuzzyMatch("周稻", near) {
		t.Fatalf("间隔在阈值内应当命中: %q", near)
	}
}

// 前缀命中要排在连续包含之前：输入歌名的开头比命中中间更相关。
func TestFuzzyScoreRanksPrefixFirst(t *testing.T) {
	prefix := FuzzyScore("稻香", "稻香 (Live)")
	inner := FuzzyScore("稻香", "翻唱 稻香 合集")
	if prefix <= inner {
		t.Fatalf("前缀得分 %d 应高于包含得分 %d", prefix, inner)
	}
}
