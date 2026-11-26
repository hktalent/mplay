package media

import "strings"

// 模糊匹配：曲库搜索与「当前列表内搜索」共用一套规则。
//
// 规则（越靠前权重越高）：
//  1. 查询按空白切词，每个词都必须命中（顺序随意、可跨字段）——
//     「稻香 周杰伦」和「周杰伦 稻香」结果一样。
//  2. 单个词命中三种情况：前缀命中 > 连续包含 > 松散命中。
//  3. 松散命中 = 词的字符按顺序出现，中间最多隔 FuzzyMaxGap 个字符——
//     「周稻」能命中「周杰伦 - 稻香」，但限制间隔是为了不把整库都搜出来。

// FuzzyMaxGap 是松散命中允许的最大间隔（按 rune 计）。
const FuzzyMaxGap = 6

// FuzzyScore 返回 q 对 fields 的匹配得分，0 表示不匹配。
// q 为空时返回 1（视为全部命中），方便调用方统一写 score == 0 过滤。
func FuzzyScore(q string, fields ...string) int {
	tokens := strings.Fields(strings.ToLower(strings.TrimSpace(q)))
	if len(tokens) == 0 {
		return 1
	}
	hay := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != "" {
			hay = append(hay, strings.ToLower(f))
		}
	}
	if len(hay) == 0 {
		return 0
	}
	// 跨字段兜底：「周稻」这种把歌手和歌名拼在一起的输入，单看一个字段对不上。
	joined := strings.Join(hay, " ")
	total := 0
	for _, tok := range tokens {
		best := 0
		for _, h := range hay {
			if s := tokenScore(tok, h); s > best {
				best = s
			}
		}
		if best == 0 {
			if strings.Contains(joined, tok) {
				best = 4
			} else if looseHit(tok, joined) {
				best = 3
			}
		}
		if best == 0 {
			return 0
		}
		total += best
	}
	return total
}

// FuzzyMatch 是 FuzzyScore 的布尔版本。
func FuzzyMatch(q string, fields ...string) bool { return FuzzyScore(q, fields...) > 0 }

// tokenScore 给单个词的命中方式打分。
func tokenScore(tok, hay string) int {
	switch {
	case tok == "":
		return 1
	case strings.HasPrefix(hay, tok):
		return 5
	case strings.Contains(hay, tok):
		return 4
	case looseHit(tok, hay):
		return 3
	}
	return 0
}

// looseHit 判断 tok 的字符能否在 hay 里按顺序出现，且间隔不超过 FuzzyMaxGap。
// 逐个起点试匹配：前一个起点失败不代表后面的起点也失败。
func looseHit(tok, hay string) bool {
	t := []rune(tok)
	h := []rune(hay)
	if len(t) == 0 || len(h) == 0 || len(t) > len(h) {
		return false
	}
	for start := 0; start < len(h); start++ {
		if h[start] != t[0] {
			continue
		}
		i, last := 1, start
		ok := true
		for i < len(t) {
			found := false
			for pos := last + 1; pos < len(h) && pos <= last+FuzzyMaxGap+1; pos++ {
				if h[pos] == t[i] {
					last, i, found = pos, i+1, true
					break
				}
			}
			if !found {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}
