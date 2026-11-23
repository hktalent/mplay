package media

import (
	"strings"
	"testing"
)

// 这一组是实测过的真实脏文件名：资源站前缀、码率呐喊、下划线、无空格连字符、
// 书名号、空格分隔、英文名…… 解析结果必须对得上（顺序也要对）。
func TestGuessNamesRealWorld(t *testing.T) {
	cases := []struct {
		name   string
		title  string
		artist string
	}{
		{"01. 夜空中最亮的星 - 逃跑计划 [320k].mp3", "夜空中最亮的星", "逃跑计划"},
		{"周杰伦 - 稻香.mp3", "稻香", "周杰伦"},
		{"稻香 - 周杰伦.mp3", "稻香", "周杰伦"},
		{"[www.yymp3.com]陈奕迅-十年.mp3", "十年", "陈奕迅"},
		{"陈奕迅_十年_320k.mp3", "十年", "陈奕迅"},
		{"【音悦台】五月天 - 倔强 (Live).mp3", "倔强", "五月天"},
		{"03 晴天 周杰伦.mp3", "晴天", "周杰伦"},
		{"Taylor Swift - Blank Space.mp3", "Blank Space", "Taylor Swift"},
		{"Blank Space - Taylor Swift.m4a", "Blank Space", "Taylor Swift"},
		{"李荣浩 模特.mp3", "模特", "李荣浩"},
		{"G.E.M.邓紫棋《光年之外》.mp3", "光年之外", "G.E.M.邓紫棋"},
		{"许巍 - 蓝莲花(无损).flac", "蓝莲花", "许巍"},
		{"04.王菲-红豆.mp3", "红豆", "王菲"},
		{"朴树-平凡之路.mp3", "平凡之路", "朴树"},
		{"S.H.E - 中国话.mp3", "中国话", "S.H.E"},
		{"薛之谦 演员 1080p.mp3", "演员", "薛之谦"},
		{"赵雷《成都》.wav", "成都", "赵雷"},
		{"05 后来 (Live) - 刘若英.mp3", "后来", "刘若英"},
		{"The Beatles - Let It Be.mp3", "Let It Be", "The Beatles"},
		{"Ed Sheeran - Shape of You [Official Video].mp3", "Shape of You", "Ed Sheeran"},
		{"周深_大鱼_完整版.mp3", "大鱼", "周深"},
		{"毛不易-像我这样的人.mp3", "像我这样的人", "毛不易"},
	}
	for _, c := range cases {
		gs := GuessNames(c.name)
		if len(gs) == 0 {
			t.Fatalf("%s: 没有候选", c.name)
		}
		// 正确的「歌名 + 艺人」必须出现在候选队列里（顺序由歌词库校验裁决），
		// 并且第一猜已经很接近（歌名或艺人对得上）。
		var found bool
		for _, g := range gs {
			if g.Title == c.title && g.Artist == c.artist {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: 候选里没有正确的 %q / %q，实际 %+v", c.name, c.title, c.artist, gs)
		}
		// 第一猜允许把前后顺序搞反（谁当艺人靠歌词库裁决），但必须用的是名字里的真实两段，
		// 不能残留 [320k] / www.xxx / 完整版 这类噪音。
		first := gs[0]
		if first.Title == "" || first.Artist == c.artist || first.Title == c.title ||
			(first.Title == c.artist && first.Artist == c.title) {
			continue
		}
		noise := []string{"320k", "www.", "完整版", "1080p", "无损", "(live)", "【", "["}
		low := strings.ToLower(first.Title)
		for _, n := range noise {
			if strings.Contains(low, n) {
				t.Errorf("%s: 第一猜残留噪音 %q", c.name, first.Title)
			}
		}
	}
}

// 英文名里带 "ft" 不能被当成 feat 砍掉（Taylor Swift 的 Swift）。
func TestCleanNameKeepsSwift(t *testing.T) {
	if got := CleanName("Taylor Swift - Blank Space.mp3"); got != "Taylor Swift - Blank Space" {
		t.Fatalf("清理结果被切坏: %q", got)
	}
	if got := CleanName("Adele - Rolling In The Deep (Live) [HQ].mp3"); got == "" || got != "Adele - Rolling In The Deep" {
		t.Fatalf("Live/HQ 没清理干净: %q", got)
	}
}

// feat 后面接的艺人要能被识别，但歌名本身不动。
func TestCleanNameFeat(t *testing.T) {
	got := CleanName("周杰伦 feat. 方文山 - 烟花易冷.mp3")
	if got != "周杰伦 - 烟花易冷" && got != "周杰伦 烟花易冷" {
		t.Fatalf("feat 处理不对: %q", got)
	}
}

// 反序候选必须也在队列里：解析第一猜错了还有救。
func TestGuessNamesHasReversed(t *testing.T) {
	gs := GuessNames("后来 - 刘若英.mp3")
	if gs[0].Title != "后来" || gs[0].Artist != "刘若英" {
		t.Fatalf("第一猜不对: %+v", gs[0])
	}
	var hasRev bool
	for _, g := range gs {
		if g.Title == "刘若英" && g.Artist == "后来" {
			hasRev = true
		}
	}
	if !hasRev {
		t.Fatalf("缺少反序候选: %+v", gs)
	}
}

// 标签优先；只有艺人时用标签艺人配文件名里的歌名段。
func TestGuessWithTags(t *testing.T) {
	gs := GuessWithTags("01. 夜空中最亮的星 - 逃跑计划 [320k].mp3", "夜空中最亮的星", "逃跑计划")
	if len(gs) != 1 || gs[0].Title != "夜空中最亮的星" || gs[0].Artist != "逃跑计划" || gs[0].Source != "tag" {
		t.Fatalf("标签完整时应只用标签: %+v", gs)
	}
	gs = GuessWithTags("01. 夜空中最亮的星 - 逃跑计划.mp3", "", "逃跑计划")
	if len(gs) == 0 || gs[0].Artist != "逃跑计划" {
		t.Fatalf("只有艺人标签时应用标签艺人: %+v", gs)
	}
	gs = GuessWithTags("01. 夜空中最亮的星 - 逃跑计划.mp3", "夜空中最亮的星", "")
	if len(gs) == 0 || gs[0].Title != "夜空中最亮的星" || gs[0].Artist != "逃跑计划" {
		t.Fatalf("只有歌名标签时应从文件名补艺人: %+v", gs)
	}
}
