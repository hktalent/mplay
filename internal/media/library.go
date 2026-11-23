package media

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Sidecar 是媒体文件旁边同名的歌词/字幕文件。
type Sidecar struct {
	Path string `json:"path"`
	Ext  string `json:"ext"`
	Lang string `json:"lang,omitempty"` // zh / en / ""（未标注）
	Role string `json:"role"`           // lyric | subtitle
}

// Entry 是曲库里的一个条目（音频或视频）。
type Entry struct {
	ID          string    `json:"id"`
	Path        string    `json:"path"`
	Dir         string    `json:"dir"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"` // audio | video
	Ext         string    `json:"ext"`
	Size        int64     `json:"size"`
	ModTime     int64     `json:"mtime"`
	Title       string    `json:"title"`
	Artist      string    `json:"artist"`
	Album       string    `json:"album,omitempty"`
	AlbumArtist string    `json:"albumArtist,omitempty"`
	Year        string    `json:"year,omitempty"`
	Duration    float64   `json:"duration"`
	MetaSource  string    `json:"metaSource,omitempty"` // id3 / flac / mp4 / filename
	HasCover    bool      `json:"hasCover"`
	CoverMIME   string    `json:"coverMime,omitempty"`
	TagLyrics   bool      `json:"tagLyrics,omitempty"`
	Sidecars    []Sidecar `json:"sidecars,omitempty"`
	LyricSource string    `json:"lyricSource,omitempty"` // sidecar | tag | cache
	Synced      bool      `json:"synced"`
	CueCount    int       `json:"cueCount,omitempty"`
	Bitrate     int       `json:"bitrate,omitempty"`
}

var (
	audioExts = map[string]bool{
		".mp3": true, ".flac": true, ".m4a": true, ".m4b": true, ".aac": true,
		".ogg": true, ".oga": true, ".opus": true, ".wav": true, ".ape": true,
		".wma": true, ".aiff": true, ".aif": true, ".alac": true,
	}
	videoExts = map[string]bool{
		".mp4": true, ".m4v": true, ".mkv": true, ".webm": true, ".mov": true,
		".avi": true, ".ts": true, ".flv": true, ".wmv": true,
	}
	lyricExts = map[string]bool{".lrc": true, ".elrc": true}
	subExts   = map[string]bool{".srt": true, ".vtt": true, ".ass": true, ".ssa": true}
	langRe    = regexp.MustCompile(`^\.[a-z]{2,3}(?:-[a-z0-9]{2,4})?$`)
)

// KindForExt 返回 audio / video / ""（不重要）。
func KindForExt(ext string) string {
	ext = strings.ToLower(ext)
	if audioExts[ext] {
		return "audio"
	}
	if videoExts[ext] {
		return "video"
	}
	return ""
}

// Library 是曲库索引：启动时从缓存恢复，扫描时增量复用未变化的条目。
type Library struct {
	cfg      interface{ MediaDirsList() []string } // 通过小接口避免包依赖环
	dirs     func() []string
	cacheDir string

	mu       sync.RWMutex
	entries  map[string]*Entry
	byPath   map[string]*Entry
	dirFiles map[string][]string
	scanned  time.Time
	scanning atomic.Bool
	errMsg   string
	scanMu   sync.Mutex

	// OnScanned 在每次扫描结束后被调用（server 用它启动后台歌词缓存）。
	OnScanned func()

	// scanQueue 是「待扫描」信号（容量 1）：连续多次改媒体目录时只排一次，
	// 但保证最后一次改动一定会被扫到，不会被「扫描进行中」直接丢掉。
	scanQueue chan struct{}
}

// ErrScanBusy 表示已有扫描在跑。
var ErrScanBusy = errors.New("扫描正在进行中")

type libraryCache struct {
	Version   int      `json:"version"`
	ScannedAt int64    `json:"scannedAt"`
	Entries   []*Entry `json:"entries"`
}

// NewLibrary 创建索引。dirs 是动态取媒体目录的函数（配置热改后自动生效）。
func NewLibrary(cacheDir string, dirs func() []string) *Library {
	l := &Library{
		dirs:      dirs,
		cacheDir:  cacheDir,
		entries:   map[string]*Entry{},
		byPath:    map[string]*Entry{},
		dirFiles:  map[string][]string{},
		scanQueue: make(chan struct{}, 1),
	}
	l.loadCache()
	go l.scanWorker()
	return l
}

// RequestScan 请求一次后台扫描：不阻塞调用方；如果已有扫描在跑，
// 会把请求排队并在当前扫描结束后立刻补扫一次。
func (l *Library) RequestScan() {
	select {
	case l.scanQueue <- struct{}{}:
	default: // 队列里已经有待扫请求，合并
	}
}

func (l *Library) scanWorker() {
	for range l.scanQueue {
		for {
			err := l.runScanSafely()
			if err == nil {
				break
			}
			if errors.Is(err, ErrScanBusy) {
				// 别的扫描（例如启动扫描）还在跑，稍等再补扫，不能直接丢弃。
				time.Sleep(300 * time.Millisecond)
				continue
			}
			fmt.Fprintf(os.Stderr, "[mplay] 后台扫描失败: %v\n", err)
			break
		}
	}
}

func (l *Library) indexPath() string { return filepath.Join(l.cacheDir, "library.json") }

func (l *Library) loadCache() {
	data, err := os.ReadFile(l.indexPath())
	if err != nil {
		return
	}
	var c libraryCache
	if json.Unmarshal(data, &c) != nil || c.Version != 1 {
		return
	}
	for _, e := range c.Entries {
		if e == nil || e.ID == "" || e.Path == "" {
			continue
		}
		l.entries[e.ID] = e
		l.byPath[e.Path] = e
	}
	if c.ScannedAt > 0 {
		l.scanned = time.Unix(c.ScannedAt, 0)
	}
}

// SaveIndex 把索引写到缓存目录（原子写）。
func (l *Library) SaveIndex() error {
	l.mu.RLock()
	entries := make([]*Entry, 0, len(l.entries))
	for _, e := range l.entries {
		cp := *e
		entries = append(entries, &cp)
	}
	scanned := l.scanned
	l.mu.RUnlock()

	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	c := libraryCache{Version: 1, ScannedAt: scanned.Unix(), Entries: entries}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(l.cacheDir, 0o755); err != nil {
		return err
	}
	// 用唯一临时文件再 rename：固定 .tmp 名字在两份索引同时落盘时会互相踩，
	// 后者 rename 时源文件已被前者挪走 → no such file or directory。
	tmp, err := os.CreateTemp(l.cacheDir, ".library-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, l.indexPath())
}

// Scan 遍历所有媒体目录，重建索引。
// runScanSafely 给后台扫描兜一层 recover：worker 是常驻 goroutine，
// 里面任何 panic（畸形媒体文件、第三方解析器越界）都会直接带走整个进程。
// UpdateLyricInfo 在后台补到歌词后更新条目的歌词标记（只动索引，不重扫文件）。
func (l *Library) UpdateLyricInfo(id, source string, synced bool, cues int) bool {
	l.mu.Lock()
	e, ok := l.entries[id]
	if ok {
		if e.LyricSource == "" || source == "manual" {
			e.LyricSource = source
		}
		if cues > 0 {
			e.CueCount = cues
		}
		e.Synced = synced
	}
	l.mu.Unlock()
	if ok {
		_ = l.SaveIndex()
	}
	return ok
}

func (l *Library) runScanSafely() (err error) {
	defer func() {
		if r := recover(); r != nil {
			l.scanning.Store(false)
			fmt.Fprintf(os.Stderr, "[mplay] 扫描过程中发生异常（已跳过，服务继续运行）: %v\n", r)
			err = fmt.Errorf("扫描异常: %v", r)
		}
	}()
	return l.Scan(context.Background())
}

func (l *Library) Scan(ctx context.Context) error {
	if ctx == nil { // 调用方传 nil 时给个空 context：下面 WalkDir 回调会调 ctx.Err()
		ctx = context.Background()
	}
	if !l.scanning.CompareAndSwap(false, true) {
		return ErrScanBusy
	}
	defer l.scanning.Store(false)
	l.scanMu.Lock()
	defer l.scanMu.Unlock()

	roots := l.dirs()
	if len(roots) == 0 {
		l.mu.Lock()
		l.entries = map[string]*Entry{}
		l.byPath = map[string]*Entry{}
		l.dirFiles = map[string][]string{}
		l.scanned = time.Now()
		l.errMsg = ""
		l.mu.Unlock()
		_ = l.SaveIndex()
		return nil
	}

	files := make([]string, 0, 4096)
	dirFiles := map[string][]string{}
	for _, root := range roots {
		root = filepath.Clean(root)
		if st, err := os.Stat(root); err != nil || !st.IsDir() {
			continue
		}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // 单个目录不可读（权限/断链）不应中断整次扫描
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if d.IsDir() {
				name := d.Name()
				if name == "." || name == ".." {
					return nil
				}
				if strings.HasPrefix(name, ".") && path != root {
					return fs.SkipDir
				}
				switch strings.ToLower(name) {
				case "node_modules", "$recycle.bin", "system volume information", "@eadir":
					return fs.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if audioExts[ext] || videoExts[ext] || lyricExts[ext] || subExts[ext] {
				dirFiles[filepath.Dir(path)] = append(dirFiles[filepath.Dir(path)], path)
			}
			if audioExts[ext] || videoExts[ext] {
				files = append(files, path)
			}
			return nil
		})
		if err != nil && ctx.Err() == nil {
			l.setErr(err.Error())
		}
	}

	workers := runtime.NumCPU()
	if workers < 2 {
		workers = 2
	}
	if workers > 8 {
		workers = 8
	}
	type job struct{ path string }
	jobs := make(chan string, workers*2)
	out := make(chan *Entry, workers*2)

	old := map[string]*Entry{}
	l.mu.RLock()
	for p, e := range l.byPath {
		old[p] = e
	}
	l.mu.RUnlock()

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				if e := buildEntrySafely(path, old[path], dirFiles[filepath.Dir(path)], l.cacheDir); e != nil {
					out <- e
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, p := range files {
			select {
			case <-ctx.Done():
				return
			case jobs <- p:
			}
		}
	}()
	go func() {
		wg.Wait()
		close(out)
	}()

	newEntries := make(map[string]*Entry, len(files))
	newByPath := make(map[string]*Entry, len(files))
	for e := range out {
		newEntries[e.ID] = e
		newByPath[e.Path] = e
	}

	l.mu.Lock()
	l.entries = newEntries
	l.byPath = newByPath
	l.dirFiles = dirFiles
	l.scanned = time.Now()
	l.errMsg = ""
	l.mu.Unlock()
	err := l.SaveIndex()
	// 扫描结束通知上层（server 据此启动后台歌词缓存）；放在锁外，避免回调里再读库时死锁。
	if hook := l.OnScanned; hook != nil {
		hook()
	}
	return err
}

// buildEntrySafely 保证单个文件解析失败只丢这一个条目，不影响整次扫描。
func buildEntrySafely(path string, old *Entry, siblings []string, cacheDir string) (e *Entry) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[mplay] 跳过无法解析的文件 %s: %v\n", path, r)
			e = nil
		}
	}()
	return buildEntry(path, old, siblings, cacheDir)
}

func (l *Library) setErr(msg string) {
	l.mu.Lock()
	l.errMsg = msg
	l.mu.Unlock()
}

func makeID(path string) string {
	sum := sha1.Sum([]byte(path))
	return hex.EncodeToString(sum[:])[:12]
}

// buildEntry 组合标签 / 文件名 / 同名歌词字幕。
// prev 命中（size+mtime 未变）时复用标签解析结果，避免每次启动重新读几千个文件。
func buildEntry(path string, prev *Entry, dirFiles []string, cacheDir string) *Entry {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return nil
	}
	ext := strings.ToLower(filepath.Ext(path))
	kind := KindForExt(ext)
	if kind == "" {
		return nil
	}
	e := &Entry{
		ID:      makeID(path),
		Path:    path,
		Dir:     filepath.Dir(path),
		Name:    filepath.Base(path),
		Kind:    kind,
		Ext:     ext,
		Size:    st.Size(),
		ModTime: st.ModTime().UnixNano(),
	}
	if prev != nil && prev.Size == e.Size && prev.ModTime == e.ModTime {
		e.Title = prev.Title
		e.Artist = prev.Artist
		e.Album = prev.Album
		e.AlbumArtist = prev.AlbumArtist
		e.Year = prev.Year
		e.Duration = prev.Duration
		e.MetaSource = prev.MetaSource
		e.HasCover = prev.HasCover
		e.CoverMIME = prev.CoverMIME
		e.TagLyrics = prev.TagLyrics
		e.Bitrate = prev.Bitrate
	} else {
		tags := ReadTags(path, ext)
		e.Title = strings.TrimSpace(tags.Title)
		e.Artist = strings.TrimSpace(tags.Artist)
		e.Album = strings.TrimSpace(tags.Album)
		e.AlbumArtist = strings.TrimSpace(tags.AlbumArtist)
		e.Year = strings.TrimSpace(tags.Year)
		e.Duration = tags.Duration
		e.HasCover = len(tags.Cover) > 0
		e.CoverMIME = tags.CoverMIME
		e.TagLyrics = tags.Lyrics != "" || tags.SyncedLyrics != ""
		e.MetaSource = tags.Source
		if e.Title == "" || e.Artist == "" {
			ft, fa := ParseFilename(path)
			if e.Title == "" {
				e.Title = ft
			}
			if e.Artist == "" && fa != "" {
				e.Artist = fa
			}
			if tags.Source == "" {
				e.MetaSource = "filename"
			} else {
				e.MetaSource += "+filename"
			}
			if e.Title == "" {
				e.Title = baseNoExt(path)
			}
		}
	}

	e.Sidecars = findSidecars(path, dirFiles)
	// 歌词优先级：同名 lrc > 同名字幕 > 容器内嵌 > 远端缓存
	hasLRC := false
	var firstSub *Sidecar
	for i := range e.Sidecars {
		sc := &e.Sidecars[i]
		if sc.Role == "lyric" {
			hasLRC = true
			break
		}
		if firstSub == nil && sc.Role == "subtitle" {
			firstSub = sc
		}
	}
	switch {
	case hasLRC:
		e.LyricSource = "sidecar"
		e.Synced, e.CueCount = sniffLyricFile(findRole(e.Sidecars, "lyric"))
	case firstSub != nil:
		e.LyricSource = "sidecar"
		e.Synced, e.CueCount = sniffLyricFile(firstSub.Path)
	case e.TagLyrics:
		e.LyricSource = "tag"
		e.Synced = true
	default:
		if _, err := os.Stat(filepath.Join(cacheDir, "lyrics", e.ID+".json")); err == nil {
			e.LyricSource = "cache"
			e.Synced = true
		}
	}
	return e
}

func findRole(list []Sidecar, role string) string {
	for _, s := range list {
		if s.Role == role {
			return s.Path
		}
	}
	return ""
}

// sniffLyricFile 快速判断是否是「带时间戳」的歌词，并数一下行数。
// 只读前 256KB，扫描上千个文件也不会慢。
func sniffLyricFile(path string) (synced bool, cues int) {
	f, err := os.Open(path)
	if err != nil {
		return false, 0
	}
	defer f.Close()
	buf := make([]byte, 256<<10)
	n, _ := f.Read(buf)
	buf = buf[:n]
	text := decodeText(buf)

	if strings.Contains(text, "-->") {
		return true, strings.Count(text, "-->") // SRT/VTT 的条数
	}
	// LRC：数真正的时间戳（[offset:0]、[ti:...] 这类元信息不算）
	stamps := lrcStampRe.FindAllString(text, -1)
	if len(stamps) > 0 {
		return true, len(stamps)
	}
	return false, strings.Count(text, "\n")
}

var lrcStampRe = regexp.MustCompile(`\[\s*\d{1,3}:\d{1,2}(?::\d{1,2})?(?:[.,]\d{1,3})?\s*\]`)

// findSidecars 在媒体文件同目录里找同名歌词/字幕，支持 xxx.zh.srt、xxx.en.lrc 这种语言后缀。
func findSidecars(mediaPath string, dirFiles []string) []Sidecar {
	base := baseNoExt(mediaPath)
	baseLower := strings.ToLower(base)
	var out []Sidecar
	for _, p := range dirFiles {
		if p == mediaPath {
			continue
		}
		ext := strings.ToLower(filepath.Ext(p))
		if lyricExts[ext] || subExts[ext] {
			nb := baseNoExt(p)
			lb := strings.ToLower(nb)
			lang := ""
			switch {
			case lb == baseLower:
			case strings.HasPrefix(lb, baseLower+".") && langRe.MatchString(lb[len(baseLower):]):
				lang = strings.TrimPrefix(lb[len(baseLower):], ".")
			default:
				continue
			}
			role := "subtitle"
			if lyricExts[ext] {
				role = "lyric"
			}
			out = append(out, Sidecar{Path: p, Ext: ext, Lang: lang, Role: role})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		// lrc 优先，其次无语言后缀的，最后按名字
		ri, rj := out[i].Role == "lyric", out[j].Role == "lyric"
		if ri != rj {
			return ri
		}
		if (out[i].Lang == "") != (out[j].Lang == "") {
			return out[i].Lang == ""
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// List 返回快照，按标题排序。
func (l *Library) List() []Entry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]Entry, 0, len(l.entries))
	for _, e := range l.entries {
		cp := *e
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Title), strings.ToLower(out[j].Title)
		if a == b {
			return out[i].Artist < out[j].Artist
		}
		return a < b
	})
	return out
}

// Get 按 ID 取条目。
func (l *Library) Get(id string) (Entry, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	e, ok := l.entries[id]
	if !ok {
		return Entry{}, false
	}
	cp := *e
	return cp, true
}

// UpdateMeta 用识别结果更新条目元信息（只改内存索引与缓存，不动用户文件）。
// 返回是否命中条目。
func (l *Library) UpdateMeta(id, title, artist, album string) bool {
	return l.UpdateMetaFrom(id, title, artist, album, "ai")
}

// UpdateMetaFrom 同 UpdateMeta，但记下这次元信息是谁给的（ai / lrclib / manual）。
func (l *Library) UpdateMetaFrom(id, title, artist, album, source string) bool {
	title = strings.TrimSpace(title)
	artist = strings.TrimSpace(artist)
	album = strings.TrimSpace(album)
	if title == "" && artist == "" {
		return false
	}
	l.mu.Lock()
	e, ok := l.entries[id]
	if ok {
		if title != "" {
			e.Title = title
		}
		if artist != "" {
			e.Artist = artist
		}
		if album != "" {
			e.Album = album
		}
		e.MetaSource = source
	}
	l.mu.Unlock()
	if ok {
		_ = l.SaveIndex()
	}
	return ok
}

// UpdateDuration 记录前端实测出来的时长（服务端无法解码时很有用）。
func (l *Library) UpdateDuration(id string, d float64) {
	if d <= 0 || d > 86400 {
		return
	}
	l.mu.Lock()
	if e, ok := l.entries[id]; ok {
		e.Duration = d
	}
	l.mu.Unlock()
	_ = l.SaveIndex()
}

// Stats 返回曲库概览。
func (l *Library) Stats() map[string]any {
	l.mu.RLock()
	defer l.mu.RUnlock()
	var audio, video, withLyric int
	var bytes int64
	for _, e := range l.entries {
		if e.Kind == "video" {
			video++
		} else {
			audio++
		}
		if e.LyricSource != "" {
			withLyric++
		}
		bytes += e.Size
	}
	return map[string]any{
		"audio":     audio,
		"video":     video,
		"withLyric": withLyric,
		"bytes":     bytes,
		"roots":     len(l.dirs()),
		"scannedAt": l.scanned.Unix(),
		"scanning":  l.scanning.Load(),
		"error":     l.errMsg,
	}
}

// Scanning 报告是否正在扫描。
func (l *Library) Scanning() bool { return l.scanning.Load() }

// CacheDir 暴露缓存目录给上层写歌词缓存。
func (l *Library) CacheDir() string { return l.cacheDir }

// Cover 取出内嵌封面字节；第一次读会落盘缓存，之后直接读文件。
func (l *Library) Cover(e Entry) (data []byte, mime string, err error) {
	cacheBase := filepath.Join(l.cacheDir, "covers", e.ID)
	candidates, _ := filepath.Glob(cacheBase + ".*")
	for _, p := range candidates {
		if b, err := os.ReadFile(p); err == nil {
			return b, sniffImage(b), nil
		}
	}
	tags := ReadTags(e.Path, e.Ext)
	if len(tags.Cover) == 0 {
		return nil, "", os.ErrNotExist
	}
	mime = tags.CoverMIME
	if mime == "" {
		mime = sniffImage(tags.Cover)
	}
	if err := os.MkdirAll(filepath.Dir(cacheBase), 0o755); err == nil {
		_ = os.WriteFile(cacheBase+extForMIME(mime), tags.Cover, 0o644)
	}
	return tags.Cover, mime, nil
}

// FindByPath 供 web 模式把外部路径映射成条目（仅本机使用时）。
func (l *Library) FindByPath(path string) (Entry, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	e, ok := l.byPath[filepath.Clean(path)]
	if !ok {
		return Entry{}, false
	}
	cp := *e
	return cp, true
}
