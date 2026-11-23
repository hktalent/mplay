package music

// netease.go 实现「按歌词片段找歌」。
//
// 必须说明边界：这里只调用网易云的公开搜索接口拿**歌曲元信息**（歌名/歌手/专辑/
// 时长/官方页面地址），不取音频地址、不做盗链。找到歌之后仍然回到 iTunes /
// Internet Archive / Jamendo / 自定义音源里找合法可播放版本；找不到就只给官方
// 页面链接。这样「输入一句歌词能搜到歌」可用，但不会变成音乐盗链工具。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// LyricSnippet 是一条「歌词片段」搜索结果。
type LyricSnippet struct {
	Key      string  `json:"key"`
	Source   string  `json:"source"`
	Title    string  `json:"title"`
	Artist   string  `json:"artist"`
	Album    string  `json:"album,omitempty"`
	Duration float64 `json:"duration"`
	Page     string  `json:"page,omitempty"`
	Cover    string  `json:"cover,omitempty"`
	Matched  string  `json:"matched,omitempty"`
}

// SearchLyricSnippets 用歌词片段搜索歌曲（网易云 cloudsearch type=1006）。
func (s *Searcher) SearchLyricSnippets(ctx context.Context, q string, limit int) ([]LyricSnippet, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, fmt.Errorf("缺少歌词片段")
	}
	if !s.cfg.EnableNeteaseLyric {
		return nil, fmt.Errorf("网易云歌词搜索已在设置里关闭")
	}
	if limit <= 0 || limit > 20 {
		limit = 10
	}

	form := url.Values{}
	form.Set("s", q)
	form.Set("type", "1006") // 1006 = 按歌词搜索
	form.Set("limit", strconv.Itoa(limit))
	form.Set("offset", "0")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://music.163.com/api/cloudsearch/pc", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "https://music.163.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120 Safari/537.36")
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("网易云搜索 HTTP %d", resp.StatusCode)
	}

	var body struct {
		Result struct {
			Songs []struct {
				Name    string `json:"name"`
				ID      int64  `json:"id"`
				DT      int64  `json:"dt"`
				Artists []struct {
					Name string `json:"name"`
				} `json:"ar"`
				Album struct {
					Name   string `json:"name"`
					PicURL string `json:"picUrl"`
				} `json:"al"`
				Origin *struct {
					Name    string `json:"name"`
					Artists []struct {
						Name string `json:"name"`
					} `json:"artists"`
					AlbumMeta struct {
						Name string `json:"name"`
					} `json:"albumMeta"`
				} `json:"originSongSimpleData"`
			} `json:"songs"`
		} `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&body); err != nil {
		return nil, err
	}

	out := make([]LyricSnippet, 0, len(body.Result.Songs))
	for _, song := range body.Result.Songs {
		artist := ""
		if len(song.Artists) > 0 {
			names := make([]string, 0, len(song.Artists))
			for _, a := range song.Artists {
				names = append(names, a.Name)
			}
			artist = strings.Join(names, " / ")
		}
		album := song.Album.Name
		// 翻唱版本：把原曲信息也带上，方便用户理解「搜到的是哪首」
		note := ""
		if song.Origin != nil && song.Origin.Name != "" {
			origArtist := ""
			if len(song.Origin.Artists) > 0 {
				origArtist = song.Origin.Artists[0].Name
			}
			if song.Origin.Name != song.Name {
				note = fmt.Sprintf("原曲：%s - %s", song.Origin.Name, origArtist)
				album = song.Origin.AlbumMeta.Name
			}
		}
		out = append(out, LyricSnippet{
			Key:      fmt.Sprintf("netease:%d", song.ID),
			Source:   "netease",
			Title:    song.Name,
			Artist:   artist,
			Album:    album,
			Duration: float64(song.DT) / 1000,
			Page:     fmt.Sprintf("https://music.163.com/#/song?id=%d", song.ID),
			Cover:    song.Album.PicURL,
			Matched:  note,
		})
	}

	// 只给前 5 条补「命中的那句歌词」做展示，失败不影响搜索结果
	fillMatchedLines(ctx, s.http, out, q)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Duration > out[j].Duration })
	return out, nil
}

var lrcTagRe = regexp.MustCompile(`\[[^\]]*\]`)

func fillMatchedLines(ctx context.Context, client *http.Client, list []LyricSnippet, q string) {
	n := len(list)
	if n > 5 {
		n = 5
	}
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := strings.TrimPrefix(list[i].Key, "netease:")
			u := "https://music.163.com/api/song/lyric?id=" + url.QueryEscape(id) + "&lv=1&kv=1&tv=-1"
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
			if err != nil {
				return
			}
			req.Header.Set("Referer", "https://music.163.com/")
			req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/120 Safari/537.36")
			resp, err := client.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			var body struct {
				LRC struct {
					Lyric string `json:"lyric"`
				} `json:"lrc"`
			}
			if json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&body) != nil {
				return
			}
			nq := strings.ToLower(strings.TrimSpace(q))
			for _, line := range strings.Split(body.LRC.Lyric, "\n") {
				line = strings.TrimSpace(lrcTagRe.ReplaceAllString(line, ""))
				if line == "" {
					continue
				}
				if strings.Contains(strings.ToLower(line), nq) {
					list[i].Matched = line
					return
				}
			}
		}(i)
	}
	wg.Wait()
}
