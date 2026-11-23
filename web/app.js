/* mplay · 前端主逻辑
   路由：#/library #/search #/player #/settings
   Web 站点与 urlbox 桌面窗口共用同一份代码，桌面额外能力通过 /api/config 的 desktop 字段探测。 */
(function () {
  'use strict';

  const L = window.MPPlay.lyrics;
  const $ = function (id) { return document.getElementById(id); };
  const $$ = function (sel, root) { return Array.prototype.slice.call((root || document).querySelectorAll(sel)); };

  const LS = {
    get: function (k, d) {
      try {
        const v = localStorage.getItem('mplay.' + k);
        return v === null ? d : JSON.parse(v);
      } catch (e) { return d; }
    },
    set: function (k, v) {
      try { localStorage.setItem('mplay.' + k, JSON.stringify(v)); } catch (e) { /* 隐私模式 */ }
    }
  };

  const AUDIO_EXT = ['mp3', 'flac', 'm4a', 'm4b', 'aac', 'ogg', 'oga', 'opus', 'wav', 'ape', 'wma', 'aiff', 'aif', 'alac'];
  const VIDEO_EXT = ['mp4', 'm4v', 'mkv', 'webm', 'mov', 'avi', 'ts', 'flv', 'wmv'];

  const S = {
    view: 'library',
    token: (function () {
      const q = new URLSearchParams(location.search).get('token');
      return q || LS.get('token', '') || '';
    })(),
    config: null,
    desktop: false,
    lib: { items: [], total: 0, stats: null, roots: [] },
    filter: 'all',
    sort: LS.get('sort', 'title'),
    q: '',
    queue: [],
    qi: -1,
    current: null,
    doc: null,
    searchMode: 'song',
    lastQuery: '',
    seeking: false,
    files: [],
    blockedShown: false
  };

  const player = new window.MPPlay.Player($('video'));
  const LV = new L.LyricView($('lyrics'), { onSeek: function (t) { player.seekTo(t); } });

  /* ── 工具 ─────────────────────────────────────────── */
  function esc(s) {
    return String(s == null ? '' : s).replace(/[&<>"']/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
  }
  function fmtTime(s) { return L.fmtTime(s); }
  /* 时间点：不足 10 秒时保留一位小数，避免「跳到 0:00」看起来像没定位 */
  function fmtAt(s) {
    if (!isFinite(s)) return '';
    return s < 10 ? (Math.round(s * 10) / 10).toFixed(1) + 's' : fmtTime(s);
  }
  function fmtSize(n) {
    if (!n) return '';
    const u = ['B', 'KB', 'MB', 'GB'];
    let i = 0, v = n;
    while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
    return (v >= 10 || i === 0 ? Math.round(v) : v.toFixed(1)) + u[i];
  }
  function parseName(name) {
    const base = name.replace(/\.(mp3|flac|m4a|m4b|aac|ogg|oga|opus|wav|ape|wma|aiff|aif|alac|mp4|m4v|mkv|webm|mov|avi|ts|flv|wmv)$/i, '');
    const clean = base.replace(/[\[\(（【]\s*(320k|128k|flac|aac|ogg|hi-?res|无损|高音质|高清)\s*[\]\)）】]/gi, '')
      .replace(/^\s*\d{1,3}\s*[.\-、_]\s*/, '')
      .replace(/_/g, ' ')
      .trim();
    let title = clean, artist = '';
    const dl = clean.match(/《([^》]+)》/);
    if (dl) title = dl[1].trim();
    const parts = clean.split(/\s+-\s+|\s*-\s*/);
    if (parts.length >= 2 && !dl) {
      const a = parts[0].trim(), b = parts.slice(1).join(' - ').trim();
      // 中文歌手在前更常见；若第一段是英文而第二段是中文，也按「歌手 - 歌名」
      title = b; artist = a;
    }
    const by = clean.match(/\bby\s+(\S+)$/i);
    if (by && !artist) { artist = by[1]; title = clean.replace(by[0], '').trim(); }
    return { title: title || base, artist: artist };
  }
  function authQS() { return S.token ? ('token=' + encodeURIComponent(S.token)) : ''; }
  function withToken(url) {
    if (!S.token) return url;
    return url + (url.indexOf('?') >= 0 ? '&' : '?') + authQS();
  }
  function hue(str) {
    let h = 0;
    for (let i = 0; i < String(str).length; i++) h = (h * 31 + String(str).charCodeAt(i)) % 360;
    return h;
  }
  let toastTimer = 0;
  function toast(msg, isErr) {
    const el = $('toast');
    el.textContent = msg;
    el.className = 'toast' + (isErr ? ' err' : '');
    el.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(function () { el.hidden = true; }, isErr ? 5200 : 2400);
  }

  async function api(path, opts) {
    opts = opts || {};
    const headers = Object.assign({}, opts.headers || {});
    if (S.token) headers['X-Mplay-Token'] = S.token;
    let body = opts.body;
    if (body != null && typeof body !== 'string') {
      headers['Content-Type'] = 'application/json';
      body = JSON.stringify(body);
    }
    const res = await fetch(path, { method: opts.method || 'GET', headers: headers, body: body });
    if (res.status === 401 || res.status === 403) {
      const err = new Error('需要访问令牌，请在「设置」里填入 token');
      err.auth = true;
      throw err;
    }
    const ct = res.headers.get('content-type') || '';
    let data = null;
    if (ct.indexOf('application/json') >= 0) data = await res.json();
    else data = await res.text();
    if (!res.ok) {
      const msg = (data && data.error) ? data.error : (typeof data === 'string' ? data.slice(0, 140) : '请求失败 ' + res.status);
      throw new Error(msg);
    }
    if (data && data.ok === false && data.error) {
      const err = new Error(data.error);
      err.payload = data;
      throw err;
    }
    return data;
  }

  /* ── 布局：头尾固定，正文留位 ──────────────────────── */
  function measureBars() {
    const head = document.querySelector('header.topbar');
    const foot = document.querySelector('footer.site-footer');
    const hh = head ? head.getBoundingClientRect().height : 64;
    const fh = foot ? foot.getBoundingClientRect().height : 48;
    document.documentElement.style.setProperty('--header-h', Math.round(hh) + 'px');
    document.documentElement.style.setProperty('--footer-h', Math.round(fh) + 'px');
  }

  /* ── 路由与视图 ───────────────────────────────────── */
  const VIEWS = ['library', 'search', 'player', 'settings'];
  function parseHash() {
    const raw = (location.hash || '#/library').replace(/^#/, '');
    const qi = raw.indexOf('?');
    const path = (qi >= 0 ? raw.slice(0, qi) : raw).replace(/\//g, '');
    const params = new URLSearchParams(qi >= 0 ? raw.slice(qi + 1) : '');
    return { view: VIEWS.indexOf(path) >= 0 ? path : 'library', params: params };
  }

  function show(view, opts) {
    opts = opts || {};
    if (VIEWS.indexOf(view) < 0) view = 'library';
    S.view = view;
    VIEWS.forEach(function (v) {
      const el = $('view-' + v);
      if (el) el.hidden = (v !== view);
    });
    $$('#tabs a').forEach(function (a) {
      a.classList.toggle('is-on', a.dataset.tab === view);
    });
    renderMini();
    if (!opts.keepScroll) window.scrollTo(0, 0);
    if (view === 'settings') renderSettings();
  }

  function go(view, params) {
    let h = '#/' + view;
    if (params) {
      const qs = new URLSearchParams(params).toString();
      if (qs) h += '?' + qs;
    }
    if (location.hash === h) { show(view); return; }
    location.hash = h;
  }

  window.addEventListener('hashchange', function () {
    const r = parseHash();
    if (r.view === 'search' && r.params.get('q')) {
      $('q').value = r.params.get('q');
      S.searchMode = r.params.get('mode') === 'lyric' ? 'lyric' : 'song';
      syncSegmented();
      runSearch(r.params.get('q'));
    }
    if (r.view === 'player' && r.params.get('play')) deepPlay(r.params);
    show(r.view, { keepScroll: true });
  });

  /* ── 曲库 ─────────────────────────────────────────── */
  async function loadLibrary() {
    const params = new URLSearchParams();
    if (S.q) params.set('q', S.q);
    if (S.filter === 'audio' || S.filter === 'video') params.set('kind', S.filter);
    if (S.filter === 'lyric' || S.filter === 'nolyric') params.set('filter', S.filter);
    params.set('sort', S.sort);
    params.set('limit', '600');
    const data = await api('/api/library?' + params.toString());
    S.lib.items = data.items || [];
    S.lib.total = data.total || 0;
    S.lib.stats = data.stats || {};
    S.lib.roots = data.roots || [];
    renderLibrary();
    renderSettingsDirs();
  }

  function renderLibrary() {
    const st = S.lib.stats || {};
    const scanning = st.scanning;
    $('lib-stats').textContent = scanning
      ? '正在扫描曲库…'
      : (st.audio || 0) + ' 首音频 · ' + (st.video || 0) + ' 个视频 · ' + (st.withLyric || 0) + ' 条已有歌词 · ' + fmtSize(st.bytes || 0);

    $('lib-chips').innerHTML =
      '<span>显示 ' + S.lib.items.length + ' / ' + S.lib.total + '</span>' +
      (S.lib.roots && S.lib.roots.length ? '<span>' + esc(S.lib.roots.join(' · ')) + '</span>' : '<span>未配置媒体目录</span>');

    const list = $('lib-list');
    list.innerHTML = '';
    const items = S.lib.items;
    if (!items.length) {
      $('lib-empty').hidden = false;
      $('lib-empty-title').textContent = S.q ? '没找到匹配的曲目' : '曲库还是空的';
      $('lib-empty-desc').textContent = S.q
        ? '换个关键词，或者到「搜索」里找在线音源。'
        : '在设置里添加媒体目录后会自动扫描；也可以直接用「打开本地文件」。';
      return;
    }
    $('lib-empty').hidden = true;

    const frag = document.createDocumentFragment();
    for (const e of items) {
      frag.appendChild(trackRow(e));
    }
    list.appendChild(frag);
  }

  function lyricBadge(e) {
    if (!e.lyricSource) return '<span class="badge none">待配词</span>';
    if (e.synced && e.cueCount) return '<span class="badge lyric">同步 ' + e.cueCount + ' 行</span>';
    if (e.lyricSource === 'sidecar' || e.lyricSource === 'tag') return '<span class="badge lyric">本地词</span>';
    return '<span class="badge remote">在线词</span>';
  }

  function trackRow(e) {
    const row = document.createElement('div');
    row.className = 'track-row';
    row.dataset.id = e.id;
    if (S.current && S.current.mediaId === e.id) row.classList.add('is-playing');

    const thumb = document.createElement('div');
    thumb.className = 'thumb';
    if (e.hasCover) {
      const img = document.createElement('img');
      img.loading = 'lazy';
      img.alt = '';
      img.src = withToken('/cover/' + encodeURIComponent(e.id));
      thumb.appendChild(img);
    } else {
      const ph = document.createElement('div');
      ph.className = 'ph';
      ph.style.setProperty('--hue', String(hue(e.title || e.name)));
      ph.textContent = (e.title || e.name || '?').trim().charAt(0).toUpperCase() || '♪';
      thumb.appendChild(ph);
    }
    const kind = document.createElement('span');
    kind.className = 'kind';
    kind.textContent = e.kind === 'video' ? '视频' : (e.ext || '').replace('.', '').toUpperCase();
    thumb.appendChild(kind);

    const meta = document.createElement('div');
    meta.className = 'track-meta';
    const title = document.createElement('div');
    title.className = 'track-title';
    title.appendChild(document.createTextNode(e.title || e.name));
    const badgeWrap = document.createElement('span');
    badgeWrap.className = 'badge-wrap';
    badgeWrap.innerHTML = lyricBadge(e);
    title.appendChild(badgeWrap);
    const sub = document.createElement('div');
    sub.className = 'track-sub';
    const bits = [e.artist || '未知歌手', e.album, (e.sidecars && e.sidecars.length ? e.sidecars.length + ' 个字幕/歌词文件' : '')]
      .filter(Boolean);
    sub.textContent = bits.join(' · ');
    meta.appendChild(title);
    meta.appendChild(sub);

    const side = document.createElement('div');
    side.className = 'track-side';
    const dur = document.createElement('span');
    dur.className = 'track-dur';
    dur.textContent = e.duration ? fmtTime(e.duration) : '';
    side.appendChild(dur);

    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'play-btn';
    btn.setAttribute('aria-label', '播放 ' + (e.title || e.name));
    btn.innerHTML = '<svg viewBox="0 0 24 24"><path d="M8 5l11 7-11 7z"/></svg>';
    btn.addEventListener('click', function (ev) {
      ev.stopPropagation();
      playFromLibrary(e.id);
    });
    side.appendChild(btn);

    row.appendChild(thumb);
    row.appendChild(meta);
    row.appendChild(side);
    row.addEventListener('click', function () { playFromLibrary(e.id); });
    row.addEventListener('contextmenu', function (ev) {
      ev.preventDefault();
      openTrackMenu(e.id);
    });
    return row;
  }

  function itemFromEntry(e) {
    return {
      key: 'lib:' + e.id,
      mediaId: e.id,
      kind: e.kind === 'video' ? 'video' : 'audio',
      url: withToken('/media/' + encodeURIComponent(e.id)),
      title: e.title || e.name,
      artist: e.artist || '',
      album: e.album || '',
      duration: e.duration || 0,
      cover: e.hasCover ? withToken('/cover/' + encodeURIComponent(e.id)) : '',
      source: 'library'
    };
  }

  function playFromLibrary(id, seekTo) {
    const items = S.lib.items;
    const idx = items.findIndex(function (x) { return x.id === id; });
    if (idx < 0) return;
    const queue = items.map(itemFromEntry);
    playItem(queue[idx], queue, idx, seekTo);
  }

  /* ── 播放 ─────────────────────────────────────────── */
  function playItem(item, queue, index, seekTo) {
    S.queue = queue || [item];
    S.qi = typeof index === 'number' ? index : 0;
    S.current = item;
    S.blockedShown = false;

    const stage = $('stage');
    stage.classList.toggle('is-video', item.kind === 'video');
    stage.classList.toggle('is-audio', item.kind !== 'video');
    $('art').hidden = item.kind === 'video';
    $('subtitle-overlay').hidden = true;
    $('stage-msg').hidden = true;

    $('art-title').textContent = item.title || '';
    $('art-artist').textContent = [item.artist, item.album].filter(Boolean).join(' · ');
    renderArtCover(item);
    $('art-glow').style.backgroundImage = item.cover ? 'url("' + item.cover.replace(/"/g, '%22') + '")' : '';

    player.load(item, true);
    player.setVolume(player._volume);
    player.setRate(player._rate);

    $('lyric-source').textContent = '歌词';
    $('lyric-count').textContent = '';
    LV.empty('正在找歌词…', item.kind === 'video' ? '先找同名 .srt/.vtt，再按片名检索字幕库' : '先找同名 .lrc，再按歌名/歌手去歌词库匹配');

    loadLyricFor(item, false, 0);
    renderBadges(item, null, '');
    renderMini();
    if (S.view !== 'player') go('player');
    else show('player');
    window.scrollTo(0, 0);
    if (typeof seekTo === 'number' && seekTo > 0) {
      setTimeout(function () { player.seekTo(seekTo); }, 300);
    }
    LS.set('last', item.key);
  }

  function renderArtCover(item) {
    const inner = $('art-inner');
    inner.innerHTML = '';
    if (item.cover) {
      const img = document.createElement('img');
      img.alt = '';
      img.src = item.cover;
      img.addEventListener('error', function () {
        inner.innerHTML = '<div class="art-letter">' + esc((item.title || '♪').charAt(0)) + '</div>';
      });
      inner.appendChild(img);
    } else {
      const letter = document.createElement('div');
      letter.className = 'art-letter';
      letter.textContent = (item.title || '♪').trim().charAt(0).toUpperCase() || '♪';
      inner.appendChild(letter);
    }
    const bars = document.createElement('div');
    bars.className = 'bars';
    bars.setAttribute('aria-hidden', 'true');
    for (let i = 0; i < 5; i++) bars.appendChild(document.createElement('i'));
    inner.appendChild(bars);
  }

  function renderBadges(item, doc, source) {
    const box = $('stage-badges');
    box.innerHTML = '';
    const add = function (text, cls) {
      const b = document.createElement('span');
      b.className = 'badge ' + (cls || '');
      b.textContent = text;
      box.appendChild(b);
    };
    if (item.kind === 'video') add('视频');
    else add((item.source === 'library' ? '本地' : '在线'));
    if (doc && doc.cues && doc.cues.length) {
      add(sourceName(source) + ' · ' + doc.cues.length + ' 行', doc.synced ? 'lyric' : '');
    }
    if (item.preview) add('试听', 'remote');
    if (item.note) add(item.note);
  }

  function sourceName(src) {
    const map = {
      sidecar: '同名词幕',
      tag: '文件内嵌',
      cache: '本地缓存',
      lrclib: 'LRCLIB',
      opensubtitles: '字幕库',
      manual: '手动导入',
      remote: '在线歌词',
      file: '本地文件'
    };
    return map[src] || (src ? src : '歌词');
  }

  async function loadLyricFor(item, force, pick) {
    try {
      $('btn-lyric-refetch').disabled = true;
      let data;
      if (item.mediaId) {
        const p = [];
        if (force) p.push('force=1');
        if (pick) p.push('pick=' + pick);
        data = await api('/api/lyric/' + encodeURIComponent(item.mediaId) + (p.length ? '?' + p.join('&') : ''));
      } else if (item.lyricText) {
        data = { ok: true, source: 'file', doc: L.parseAuto(item.lyricText, item.lyricExt) };
      } else {
        const q = new URLSearchParams();
        q.set('track', item.title || '');
        q.set('artist', item.artist || '');
        if (item.album) q.set('album', item.album);
        if (item.duration) q.set('duration', String(Math.round(item.duration)));
        if (pick) q.set('pick', String(pick));
        data = await api('/api/remote/lyric?' + q.toString());
      }
      applyLyric(data, item);
    } catch (e) {
      LV.empty('没找到歌词', e && e.payload && e.payload.hint ? e.payload.hint : (e.message || ''));
      $('lyric-source').textContent = '无歌词';
      $('lyric-count').textContent = '';
      renderBadges(item, null, '');
      if (e && e.auth) toast(e.message, true);
    } finally {
      $('btn-lyric-refetch').disabled = false;
    }
  }

  function applyLyric(data, item) {
    const doc = (data && data.doc) || null;
    if (!doc || !doc.cues || !doc.cues.length) {
      S.doc = null;
      LV.empty('没找到歌词', '可以点「导入」放一份 .lrc/.srt，或点「选择」手动挑一份在线歌词。');
      $('lyric-source').textContent = '无歌词';
      $('lyric-count').textContent = '';
      renderBadges(item, null, '');
      return;
    }
    S.doc = doc;
    LV.setOffset(LV.offset);
    LV.setDoc(doc);
    const synced = doc.synced && doc.cues.some(function (c) { return c.s > 0; });
    const src = (data && data.source) || doc.source || 'remote';
    $('lyric-source').textContent = sourceName(src) + (synced ? ' · 同步' : ' · 文本');
    $('lyric-count').textContent = doc.cues.length + ' 行';
    renderBadges(item, doc, src);
    const t = player.el.currentTime || 0;
    LV.tick(t, true);

    if (item.kind === 'video') {
      const hasTime = doc.cues.some(function (c) { return isFinite(c.s) && c.s > 0; });
      $('subtitle-overlay').hidden = !hasTime;
    }
  }

  function updatePlayhead(t) {
    const d = player.duration || (player.el && player.el.duration) || 0;
    const pct = d > 0 ? Math.min(100, (t / d) * 100) : 0;
    if (!S.seeking) {
      $('seek').value = String(Math.round(pct * 10));
      $('seek').style.setProperty('--p', pct.toFixed(2) + '%');
    }
    $('t-cur').textContent = fmtTime(t);
    $('t-dur').textContent = fmtTime(d);
    LV.tick(t);

    if (S.current && S.current.kind === 'video') {
      const cue = LV.activeCue();
      const line = $('sub-line'), tr = $('sub-trans');
      if (cue && cue.t) {
        line.textContent = cue.t;
        tr.textContent = cue.tr || '';
        $('subtitle-overlay').hidden = false;
      } else {
        line.textContent = '';
        tr.textContent = '';
        $('subtitle-overlay').hidden = true;
      }
    }

    const miniBar = $('mini-bar');
    if (miniBar) miniBar.style.width = pct.toFixed(1) + '%';
    $('mini-time').textContent = fmtTime(t);
  }

  function setPlayIcon(playing) {
    const ico = playing
      ? '<path d="M9 5v14M15 5v14"/>'
      : '<path d="M8 5l11 7-11 7z"/>';
    $('ico-play').innerHTML = ico;
    $('mini-ico').innerHTML = ico;
    $('stage').classList.toggle('is-playing', playing);
    $('btn-play').setAttribute('aria-label', playing ? '暂停' : '播放');
  }

  function renderMini() {
    const mini = $('mini');
    const item = S.current;
    const visible = !!(item && S.view !== 'player');
    mini.hidden = !visible;
    document.documentElement.style.setProperty('--mini-h', visible ? '58px' : '0px');
    if (!item) return;
    $('mini-title').textContent = item.title || '';
    $('mini-artist').textContent = item.artist || (item.kind === 'video' ? '视频' : '');
    const cover = $('mini-cover');
    if (item.cover) {
      cover.style.backgroundImage = 'url("' + item.cover.replace(/"/g, '%22') + '")';
      cover.textContent = '';
    } else {
      cover.style.backgroundImage = '';
      cover.textContent = (item.title || '♪').trim().charAt(0).toUpperCase() || '♪';
    }
  }

  function next(delta) {
    if (!S.queue.length) return;
    let i = S.qi + (delta || 1);
    if (i >= S.queue.length) i = 0;
    if (i < 0) i = S.queue.length - 1;
    playItem(S.queue[i], S.queue, i);
  }

  /* ── 在线搜索 ─────────────────────────────────────── */
  function syncSegmented() {
    $$('#search-mode button').forEach(function (b) {
      b.classList.toggle('is-on', b.dataset.mode === S.searchMode);
    });
    $('search-hint').textContent = S.searchMode === 'lyric'
      ? '输入记得的一句歌词，从本地曲库、网易云和歌词库里反查歌曲；命中后可直接跳转到那句的时间点。'
      : '按歌名/歌手搜索在线音源；标「试听」的是 30 秒官方预览，标「完整」的可以完整播放。';
  }

  async function runSearch(q) {
    q = (q || '').trim();
    S.lastQuery = q;
    const box = $('search-results');
    if (!q) {
      box.innerHTML = '<div class="empty"><div class="empty-art">⌕</div><h2>想听点什么？</h2>' +
        '<p>在上面的搜索框输入歌名、歌手，或者输入一句歌词。</p></div>';
      $('search-sub').textContent = '在线找歌，或输入一句歌词反查歌曲';
      return;
    }
    box.innerHTML = '<div class="empty"><p>正在搜索「' + esc(q) + '」…</p></div>';
    try {
      if (S.searchMode === 'lyric') {
        const data = await api('/api/search/lyrics?q=' + encodeURIComponent(q));
        renderLyricSearch(data);
      } else {
        const data = await api('/api/search?q=' + encodeURIComponent(q) + '&limit=30');
        renderSongSearch(data);
      }
    } catch (e) {
      box.innerHTML = '<div class="empty"><h2>搜索失败</h2><p>' + esc(e.message || '') + '</p></div>';
    }
  }

  function songCard(r) {
    const card = document.createElement('div');
    card.className = 'song-card';
    const thumb = document.createElement('div');
    thumb.className = 'thumb';
    if (r.cover) {
      const img = document.createElement('img');
      img.loading = 'lazy';
      img.alt = '';
      img.src = r.cover;
      img.addEventListener('error', function () { img.remove(); thumb.innerHTML = '<div class="ph" style="--hue:' + hue(r.title) + '">♪</div>'; });
      thumb.appendChild(img);
    } else {
      const ph = document.createElement('div');
      ph.className = 'ph';
      ph.style.setProperty('--hue', String(hue(r.title)));
      ph.textContent = (r.title || '♪').charAt(0).toUpperCase();
      thumb.appendChild(ph);
    }
    const meta = document.createElement('div');
    meta.className = 'meta';
    const t = document.createElement('div');
    t.className = 'title';
    t.textContent = r.title || '';
    const a = document.createElement('div');
    a.className = 'artist';
    a.textContent = [r.artist, r.year, r.duration ? fmtTime(r.duration) : ''].filter(Boolean).join(' · ');
    const tags = document.createElement('div');
    tags.className = 'tags';
    const src = document.createElement('span');
    src.className = 'badge';
    src.textContent = r.sourceName || r.source || '音源';
    tags.appendChild(src);
    if (r.preview) {
      const p = document.createElement('span');
      p.className = 'badge remote';
      p.textContent = '试听';
      tags.appendChild(p);
    } else if (r.stream) {
      const p = document.createElement('span');
      p.className = 'badge lyric';
      p.textContent = '完整';
      tags.appendChild(p);
    }
    meta.appendChild(t);
    meta.appendChild(a);
    meta.appendChild(tags);

    const goBtn = document.createElement('button');
    goBtn.type = 'button';
    goBtn.className = 'go';
    goBtn.setAttribute('aria-label', '播放');
    goBtn.innerHTML = '<svg viewBox="0 0 24 24"><path d="M8 5l11 7-11 7z"/></svg>';
    goBtn.addEventListener('click', function () { playRemote(r); });

    card.appendChild(thumb);
    card.appendChild(meta);
    card.appendChild(goBtn);
    if (r.stream) {
      card.addEventListener('click', function () { playRemote(r); });
    } else if (r.page) {
      card.addEventListener('click', function () { window.open(r.page, '_blank', 'noopener'); });
    }
    return card;
  }

  function renderSongSearch(data) {
    const box = $('search-results');
    box.innerHTML = '';
    const results = data.results || [];
    window.__lastSongResults = results;
    const sources = (data.sources || []).filter(function (x) { return !x || x.enabled !== false; })
      .map(function (x) { return (x && (x.name || x.id)) || String(x); }).join(' · ');
    $('search-sub').textContent = '「' + esc(data.query || '') + '」 ' + results.length + ' 条结果' + (sources ? ' · 音源：' + sources : '');

    if (!results.length) {
      box.innerHTML = '<div class="empty"><div class="empty-art">∅</div><h2>没有搜到</h2><p>' +
        esc((data.errors && data.errors.length) ? data.errors.join('；') : '换个写法，或者到设置里填一个自定义音源。') + '</p>' +
        '<div class="empty-actions"><a class="btn" href="#/search?mode=lyric&q=' + encodeURIComponent(S.lastQuery) + '">改用歌词片段搜</a></div></div>';
      return;
    }
    const grid = document.createElement('div');
    grid.className = 'cards-grid';
    results.forEach(function (r) { grid.appendChild(songCard(r)); });
    const sec = document.createElement('div');
    sec.className = 'result-section';
    sec.appendChild(grid);
    box.appendChild(sec);
    if (data.errors && data.errors.length) {
      const err = document.createElement('p');
      err.className = 'tiny';
      err.textContent = '部分音源失败：' + data.errors.join('；');
      box.appendChild(err);
    }
  }

  function playRemote(r) {
    if (!r.stream) {
      if (r.page) window.open(r.page, '_blank', 'noopener');
      else toast('这条结果没有可播放地址');
      return;
    }
    const item = {
      key: 'remote:' + (r.key || r.stream),
      kind: 'audio',
      url: r.stream,
      title: r.title || '',
      artist: r.artist || '',
      album: r.album || '',
      duration: r.duration || 0,
      cover: r.cover || '',
      preview: !!r.preview,
      note: r.sourceName || r.source || '',
      source: 'remote'
    };
    const queue = $$('.song-card').length
      ? (window.__lastSongResults || []).map(function (x) {
        return {
          key: 'remote:' + (x.key || x.stream), kind: 'audio', url: x.stream,
          title: x.title, artist: x.artist, album: x.album, duration: x.duration,
          cover: x.cover, preview: x.preview, note: x.sourceName || x.source, source: 'remote'
        };
      }).filter(function (x) { return x.url; })
      : [item];
    const idx = Math.max(0, queue.findIndex(function (x) { return x.key === item.key; }));
    playItem(item, queue, idx);
    if (!item.preview && !item.duration) {
      toast('正在解析时长与歌词…');
    }
  }

  function renderLyricSearch(data) {
    const box = $('search-results');
    box.innerHTML = '';
    $('search-sub').textContent = '「' + esc(data.query || '') + '」本地 ' + (data.local || []).length +
      ' 条 · 网易云 ' + (data.remote || []).length + ' 条 · 歌词库 ' + (data.library || []).length + ' 条' +
      (data.indexLines ? '（本地索引 ' + data.indexLines + ' 行）' : '');

    const chips = document.createElement('div');
    chips.className = 'stats-line';
    chips.innerHTML = '<span>本地命中优先，点击直接跳到那句</span><span>远端结果只给元信息，不取音频地址</span>';
    box.appendChild(chips);

    // 1) 本地曲库命中
    const local = data.local || [];
    if (local.length) {
      const sec = document.createElement('div');
      sec.className = 'result-section';
      sec.innerHTML = '<h3>本地曲库</h3>';
      local.forEach(function (hit) {
        sec.appendChild(snippetItem({
          title: hit.title,
          who: hit.artist,
          line: hit.text + (hit.trans ? ' / ' + hit.trans : ''),
          at: hit.start,
          primary: '播放并跳到 ' + fmtAt(hit.start),
          onPrimary: function () { playFromLibrary(hit.mediaId, hit.start); },
          extra: {
            label: '看歌词',
            onClick: function () { previewLibraryLyric(hit.mediaId); }
          }
        }));
      });
      box.appendChild(sec);
    }

    // 2) 网易云：只有元信息
    const remote = data.remote || [];
    if (remote.length) {
      const sec = document.createElement('div');
      sec.className = 'result-section';
      sec.innerHTML = '<h3>网易云 · 歌词命中</h3>';
      remote.forEach(function (r) {
        sec.appendChild(snippetItem({
          title: r.title,
          who: r.artist,
          line: r.matched || '',
          at: r.duration,
          primary: '在曲库找这首歌',
          onPrimary: function () { findInLibrary(r.title + ' ' + (r.artist || '')); },
          extra: r.page ? { label: '打开主页', href: r.page } : null
        }));
      });
      box.appendChild(sec);
    }

    // 3) 歌词库（LRCLIB）
    const library = data.library || [];
    if (library.length) {
      const sec = document.createElement('div');
      sec.className = 'result-section';
      sec.innerHTML = '<h3>歌词库 · 歌名匹配</h3>';
      library.forEach(function (r) {
        sec.appendChild(snippetItem({
          title: r.trackName,
          who: r.artistName,
          line: r.snippet || (r.matched || []).join(' / '),
          at: r.duration,
          primary: '拉这份歌词',
          onPrimary: function () { previewRemoteLyric(r); },
          extra: { label: '按歌名搜在线音频', onClick: function () { go('search', { q: r.trackName, mode: 'song' }); } }
        }));
      });
      box.appendChild(sec);
    }

    if (!local.length && !remote.length && !library.length) {
      box.innerHTML += '<div class="empty"><div class="empty-art">♪</div><h2>没有反查到歌曲</h2>' +
        '<p>' + esc((data.errors && data.errors.length ? data.errors.join('；') : '试试更完整的一句歌词，或换成歌名搜索。')) + '</p></div>';
    }
  }

  function snippetItem(o) {
    const box = document.createElement('div');
    box.className = 'snippet-item';
    const main = document.createElement('div');
    main.className = 'snippet-main';
    const song = document.createElement('div');
    song.className = 'snippet-song';
    song.innerHTML = esc(o.title) + (o.who ? '<span class="who">' + esc(o.who) + '</span>' : '');
    main.appendChild(song);
    if (o.line) {
      const line = document.createElement('div');
      line.className = 'snippet-line';
      line.textContent = o.line;
      main.appendChild(line);
    }
    if (o.at) {
      const at = document.createElement('p');
      at.className = 'tiny';
      at.textContent = '时间点 ' + fmtAt(o.at);
      main.appendChild(at);
    }
    const actions = document.createElement('div');
    actions.className = 'snippet-actions';
    if (o.primary) {
      const b = document.createElement('button');
      b.className = 'mini-btn';
      b.type = 'button';
      b.textContent = o.primary;
      b.addEventListener('click', o.onPrimary);
      actions.appendChild(b);
    }
    if (o.extra) {
      if (o.extra.href) {
        const a = document.createElement('a');
        a.className = 'mini-btn';
        a.href = o.extra.href;
        a.target = '_blank';
        a.rel = 'noopener';
        a.textContent = o.extra.label;
        actions.appendChild(a);
      } else {
        const b = document.createElement('button');
        b.className = 'mini-btn';
        b.type = 'button';
        b.textContent = o.extra.label;
        b.addEventListener('click', o.extra.onClick);
        actions.appendChild(b);
      }
    }
    box.appendChild(main);
    box.appendChild(actions);
    return box;
  }

  function findInLibrary(q) {
    S.q = q.trim();
    S.filter = 'all';
    syncFilters();
    go('library');
    loadLibrary().then(function () {
      if (!S.lib.items.length) toast('曲库里没有「' + q + '」，可以到「搜索」里找在线音源');
      else toast('曲库里找到 ' + S.lib.total + ' 条匹配');
    }).catch(function (e) { toast(e.message, true); });
  }

  async function previewLibraryLyric(id) {
    try {
      const data = await api('/api/lyric/' + encodeURIComponent(id));
      openLyricPreview(data);
    } catch (e) { toast(e.message, true); }
  }

  async function previewRemoteLyric(r) {
    try {
      const q = new URLSearchParams();
      q.set('track', r.trackName || '');
      q.set('artist', r.artistName || '');
      if (r.albumName) q.set('album', r.albumName);
      if (r.duration) q.set('duration', String(Math.round(r.duration)));
      const data = await api('/api/remote/lyric?' + q.toString());
      openLyricPreview(data, r.trackName + ' · ' + (r.artistName || ''));
    } catch (e) { toast(e.message, true); }
  }

  function openLyricPreview(data, title) {
    const doc = data && data.doc;
    if (!doc || !doc.cues || !doc.cues.length) {
      toast((data && data.error) || '这份歌词拉不下来', true);
      return;
    }
    const html = doc.cues.slice(0, 200).map(function (c) {
      return '<div class="cand" style="display:block"><div class="t">' + fmtTime(c.s) + '  ' + esc(c.t || '') +
        '</div>' + (c.tr ? '<div class="m">' + esc(c.tr) + '</div>' : '') + '</div>';
    }).join('');
    openModal(title || doc.title || '歌词预览', '<div>' + html + '</div>' +
      (doc.cues.length > 200 ? '<p class="tiny">只显示前 200 行</p>' : ''));
  }

  /* 深链：#/player?play=<id>&t=<秒> 直接播放某一首（分享/桌面快捷方式用） */
  function deepPlay(params) {
    if (!params) return false;
    const id = params.get('play');
    if (!id) return false;
    const t = Number(params.get('t') || 0);
    if (!S.lib.items.some(function (x) { return x.id === id; })) {
      toast('链接里的曲目不在当前曲库，可能已被移动', true);
      return false;
    }
    playFromLibrary(id, isFinite(t) ? t : 0);
    return true;
  }

  /* ── 本地文件（不写曲库） ───────────────────────────── */
  function extOf(name) {
    const m = String(name).match(/\.([a-z0-9]+)$/i);
    return m ? m[1].toLowerCase() : '';
  }

  function acceptFiles(fileList) {
    const files = Array.prototype.slice.call(fileList || []);
    if (!files.length) return;
    const lyricMap = {};
    files.forEach(function (f) {
      const ext = extOf(f.name);
      if (['lrc', 'srt', 'vtt', 'ass', 'ssa', 'txt'].indexOf(ext) >= 0) {
        const base = f.name.replace(/\.([a-z0-9]+)$/i, '').toLowerCase();
        lyricMap[base] = f;
      }
    });

    const media = [];
    files.forEach(function (f) {
      const ext = extOf(f.name);
      const isAudio = AUDIO_EXT.indexOf(ext) >= 0;
      const isVideo = VIDEO_EXT.indexOf(ext) >= 0;
      if (!isAudio && !isVideo) return;
      const parsed = parseName(f.name);
      const track = {
        key: 'file:' + f.name + ':' + f.size + ':' + f.lastModified,
        kind: isVideo ? 'video' : 'audio',
        url: URL.createObjectURL(f),
        title: parsed.title,
        artist: parsed.artist,
        album: '',
        cover: '',
        duration: 0,
        source: 'file',
        note: fmtSize(f.size),
        _file: f
      };
      const base = f.name.replace(/\.([a-z0-9]+)$/i, '').toLowerCase();
      const lf = lyricMap[base];
      if (lf) {
        media.push(track);
        track._lyricFile = lf;
      } else {
        media.push(track);
      }
    });

    if (!media.length) { toast('没有可播放的音频或视频文件', true); return; }

    const queue = [];
    const jobs = media.map(function (t) {
      const lyricFile = t._lyricFile;
      delete t._lyricFile;
      if (!lyricFile) { queue.push(t); return Promise.resolve(); }
      return lyricFile.text().then(function (text) {
        t.lyricText = text;
        t.lyricExt = extOf(lyricFile.name);
        queue.push(t);
      }).catch(function () { queue.push(t); });
    });
    Promise.all(jobs).then(function () {
      playItem(queue[0], queue, 0);
      toast('已打开 ' + queue.length + ' 个本地文件（不写入曲库）');
    });
  }

  function defaultDirPlaceholder() {
    const p = $('dir-input');
    if (!p) return;
    const pf = S.config && S.config.platform ? S.config.platform : '';
    if (/win/i.test(pf)) p.placeholder = 'D:\\Music';
    else p.placeholder = '/Users/you/Music 或 /home/you/Music';
  }

  /* ── 设置 ─────────────────────────────────────────── */
  async function loadConfig() {
    try {
      S.config = await api('/api/config');
      S.desktop = !!S.config.desktop;
      if (S.config.authRequired && !S.token) {
        toast('这个实例需要访问令牌，请在设置里填入', true);
      }
    } catch (e) {
      if (e.auth) S.config = null;
      toast('实例配置读取失败：' + e.message, true);
    }
    renderSettings();
    renderSettingsDirs();
  }

  function renderSettings() {
    const c = S.config || {};
    $('settings-sub').textContent = c.version
      ? 'v' + c.version + ' · ' + (c.platform || '') + (c.desktop ? ' · 桌面模式' : ' · Web 模式')
      : '媒体目录、歌词源与桌面能力';

    const set = function (id, val) {
      const el = $(id);
      if (!el) return;
      el.checked = !!val;
    };
    set('sw-lrclib', c.enableLRCLIB);
    set('sw-netease', c.enableNeteaseLyric);
    set('sw-sidecar', c.writeSidecar);
    set('sw-itunes', c.enableITunes);
    set('sw-archive', c.enableArchive);

    if ($('in-os-key') && !$('in-os-key').dataset.touched) {
      $('in-os-key').placeholder = c.subtitlesConfigured ? '已配置（留空保持不变）' : '未配置，视频无同名字幕时不会去字幕库检索';
    }
    if ($('in-jamendo') && !$('in-jamendo').dataset.touched) {
      $('in-jamendo').placeholder = c.jamendoConfigured ? '已配置（留空保持不变）' : '未配置';
    }
    if ($('in-sources') && !$('in-sources').dataset.touched) {
      $('in-sources').value = c.sources && c.sources.length ? JSON.stringify(c.sources, null, 2) : '';
    }
    if ($('in-token') && !$('in-token').dataset.touched) {
      $('in-token').placeholder = c.authRequired ? '已设置，留空保持不变' : '留空表示不校验';
    }
    set('sw-ai', c.aiIdentify);
    set('sw-ai-audio', c.aiUploadAudio);
    if ($('tag-ai')) {
      $('tag-ai').textContent = !c.aiIdentify ? '未启用' : (c.aiConfigured ? '已启用' : '缺 API Key');
    }
    if ($('in-ai-base') && !$('in-ai-base').dataset.touched) $('in-ai-base').value = c.aiBaseUrl || '';
    if ($('in-ai-model') && !$('in-ai-model').dataset.touched) $('in-ai-model').value = c.aiModel || '';
    if ($('in-ai-key') && !$('in-ai-key').dataset.touched) {
      $('in-ai-key').placeholder = c.aiConfigured ? '已配置（留空保持不变）' : '未配置';
    }
    if ($('ai-hint')) {
      $('ai-hint').textContent = c.aiIdentify
        ? (c.aiConfigured ? 'AI 识别已启用；音频片段上传：' + (c.aiUploadAudio ? '允许' : '关闭') : '已开启但还没有 Key，识别会退回本地规则 + 歌词库校验')
        : '当前只走本地规则 + 歌词库校验，不调用任何外部 AI。';
    }

    const kv = $('server-kv');
    kv.innerHTML = '';
    const rows = [
      ['监听地址', c.addr || ''],
      ['配置文件', c.configPath || ''],
      ['局域网', c.allowLan ? '允许' : '仅本机'],
      ['访问令牌', c.authRequired ? '已启用' : '未启用'],
      ['桌面模式', c.desktop ? '是（urlbox 内嵌）' : '否（浏览器访问）'],
      ['本机播放器', S.desktop ? '可用' : 'Web 模式不可用']
    ];
    rows.forEach(function (r) {
      const k = document.createElement('div');
      k.className = 'k';
      k.textContent = r[0];
      const v = document.createElement('div');
      v.className = 'v';
      v.textContent = r[1];
      kv.appendChild(k);
      kv.appendChild(v);
    });

    $('btn-reveal').hidden = !S.desktop;
    $('btn-open-player').hidden = !S.desktop;
    $('btn-clear-files').hidden = !S.files.length;
  }

  function renderSettingsDirs() {
    const list = $('dir-list');
    if (!list) return;
    list.innerHTML = '';
    const dirs = (S.config && S.config.mediaDirs) || S.lib.roots || [];
    $('tag-dirs').textContent = dirs.length + ' 个';
    if (!dirs.length) {
      list.innerHTML = '<li class="none">还没有目录，添加一个就会自动扫描</li>';
      return;
    }
    dirs.forEach(function (d) {
      const li = document.createElement('li');
      const span = document.createElement('span');
      span.textContent = d;
      const rm = document.createElement('button');
      rm.className = 'rm';
      rm.type = 'button';
      rm.textContent = '移除';
      rm.disabled = dirsSaving; // 保存中重绘出来的新按钮也要保持禁用
      rm.addEventListener('click', function () { removeDir(d); });
      li.appendChild(span);
      li.appendChild(rm);
      list.appendChild(li);
    });
  }

  async function patchConfig(patch, done) {
    try {
      S.config = await api('/api/config', { method: 'POST', body: patch });
      renderSettings();
      renderSettingsDirs();
      if (done) done(null);
    } catch (e) {
      toast(e.message || '保存失败', true);
      if (done) done(e);
    }
  }

  // 连续点「移除」时，闭包里那份 dirs 是上一次渲染的快照：
  // 两次请求都基于同一份旧数组，后到的会把先前的删除覆盖回去。所以这里
  // 每次都从最新配置重算，并且在保存期间锁住按钮，避免并发 POST。
  let dirsSaving = false;

  function setDirButtonsDisabled(v) {
    const list = $('dir-list');
    if (!list) return;
    Array.prototype.forEach.call(list.querySelectorAll('button.rm'), function (b) { b.disabled = v; });
  }

  function removeDir(d) {
    if (dirsSaving) return;
    const cur = ((S.config && S.config.mediaDirs) || []).slice();
    const i = cur.indexOf(d);
    if (i < 0) return;
    cur.splice(i, 1);
    saveDirs(cur);
  }

  function saveDirs(dirs) {
    if (dirsSaving) return;
    dirsSaving = true;
    setDirButtonsDisabled(true);
    patchConfig({ mediaDirs: dirs }, function (err) {
      if (err) {
        dirsSaving = false;
        setDirButtonsDisabled(false);
        return;
      }
      // 成功后多锁 300ms：列表重排后按钮位置会变，手快的人第二下很容易
      // 点到「下一行」把另一个目录也删了。这段时间内忽略后续点击。
      setTimeout(function () {
        dirsSaving = false;
        setDirButtonsDisabled(false);
      }, 300);
      toast('已更新媒体目录，正在后台扫描');
      setTimeout(function () { loadLibrary().catch(function () {}); }, 800);
    });
  }

  /* ── 弹层 ─────────────────────────────────────────── */
  function openModal(title, html) {
    $('modal-title').textContent = title;
    $('modal-body').innerHTML = html;
    $('modal').hidden = false;
  }
  function closeModal() { $('modal').hidden = true; }

  function openCandidates() {
    const item = S.current;
    if (!item) return;
    if (!item.mediaId) { toast('在线曲目没有候选歌词列表，可以直接点「重拉」'); return; }
    api('/api/lyric/' + encodeURIComponent(item.mediaId) + '/candidates').then(function (data) {
      const cands = data.candidates || [];
      if (!cands.length) {
        openModal('在线歌词候选', '<p class="tiny">' + esc((data && data.error) || '没有候选，确认设置里的 LRCLIB 已开启') + '</p>');
        return;
      }
      openModal('挑一份歌词', '<div id="cand-list"></div>');
      const box = $('cand-list');
      cands.forEach(function (c) {
        const row = document.createElement('div');
        row.className = 'cand';
        row.innerHTML = '<div><div class="t">' + esc(c.trackName) + '</div>' +
          '<div class="m">' + esc([c.artistName, c.albumName, c.duration ? fmtTime(c.duration) : '', c.hasSynced ? '同步' : '文本', '评分 ' + (c.score || 0).toFixed(2)].filter(Boolean).join(' · ')) + '</div></div>';
        const b = document.createElement('button');
        b.className = 'pick';
        b.type = 'button';
        b.textContent = '用这份';
        b.addEventListener('click', function () {
          closeModal();
          loadLyricFor(S.current, true, c.id);
          toast('已改用候选：' + c.trackName);
        });
        row.appendChild(b);
        box.appendChild(row);
      });
    }).catch(function (e) { toast(e.message, true); });
  }

  function openTrackMenu(id) {
    const e = S.lib.items.find(function (x) { return x.id === id; });
    if (!e) return;
    const rows = [];
    rows.push(['播放', function () { playFromLibrary(e.id); }]);
    rows.push(['重新拉取歌词', function () { playFromLibrary(e.id); setTimeout(function () { loadLyricFor(S.current, true, 0); }, 400); }]);
    if (S.desktop) rows.push(['在文件夹中显示', function () { api('/api/open', { method: 'POST', body: { id: e.id, action: 'reveal' } }).catch(function (x) { toast(x.message, true); }); }]);
    if (S.desktop) rows.push(['用系统播放器打开', function () { api('/api/open', { method: 'POST', body: { id: e.id, action: 'player' } }).catch(function (x) { toast(x.message, true); }); }]);
    rows.push(['复制播放地址', function () {
      const url = withToken(location.origin + '/media/' + e.id);
      navigator.clipboard.writeText(url).then(function () { toast('已复制'); }, function () { toast(url); });
    }]);
    const html = rows.map(function (r, i) { return '<button class="cand" style="width:100%;cursor:pointer" data-i="' + i + '"><div class="t">' + esc(r[0]) + '</div></button>'; }).join('');
    openModal(e.title || e.name, html);
    $$('#modal-body .cand').forEach(function (b) {
      b.addEventListener('click', function () {
        closeModal();
        rows[Number(b.dataset.i)][1]();
      });
    });
  }

  function openCurrentMenu() {
    const item = S.current;
    if (!item) { toast('先播放一首'); return; }
    const rows = [
      ['复制播放地址', function () {
        navigator.clipboard.writeText(item.url).then(function () { toast('已复制'); }, function () { toast(item.url); });
      }],
      ['导出歌词 LRC', exportLRC],
      ['重新拉取歌词', function () { loadLyricFor(item, true, 0); }]
    ];
    if (item.mediaId && S.desktop) rows.push(['在文件夹中显示', function () {
      api('/api/open', { method: 'POST', body: { id: item.mediaId, action: 'reveal' } }).catch(function (x) { toast(x.message, true); });
    }]);
    if (item.mediaId && S.desktop) rows.push(['用系统播放器打开', function () {
      api('/api/open', { method: 'POST', body: { id: item.mediaId, action: 'player' } }).catch(function (x) { toast(x.message, true); });
    }]);
    if (!item.mediaId) rows.push(['在浏览器里打开来源页', function () {
      if (item.page) window.open(item.page, '_blank', 'noopener');
      else toast('这条在线结果没有来源页');
    }]);
    const html = rows.map(function (r, i) { return '<button class="cand" style="width:100%;cursor:pointer" data-i="' + i + '"><div class="t">' + esc(r[0]) + '</div></button>'; }).join('');
    openModal(item.title || '播放器操作', html);
    $$('#modal-body .cand').forEach(function (b) {
      b.addEventListener('click', function () {
        closeModal();
        rows[Number(b.dataset.i)][1]();
      });
    });
  }

  function exportLRC() {
    if (!S.doc) { toast('当前没有歌词可导出', true); return; }
    const text = LV.toLRC(S.current && S.current.title, S.current && S.current.artist);
    const blob = new Blob([text], { type: 'text/plain;charset=utf-8' });
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = ((S.current && S.current.title) || 'lyrics') + '.lrc';
    a.click();
    setTimeout(function () { URL.revokeObjectURL(a.href); }, 1000);
  }

  /* ── 事件绑定 ─────────────────────────────────────── */
  function syncFilters() {
    $$('#lib-filters .chip').forEach(function (c) {
      c.classList.toggle('is-on', c.dataset.filter === S.filter);
    });
  }

  function bind() {
    $('search-form').addEventListener('submit', function (ev) {
      ev.preventDefault();
      const q = $('q').value.trim();
      if (!q) { go('library'); return; }
      if (S.searchMode === 'song' && S.view === 'library') {
        // 顶栏搜索在曲库里默认先按本地过滤；想在线搜就去搜索页
      }
      go('search', { q: q, mode: S.searchMode });
    });

    $$('#lib-filters .chip').forEach(function (c) {
      c.addEventListener('click', function () {
        S.filter = c.dataset.filter;
        syncFilters();
        loadLibrary().catch(function (e) { toast(e.message, true); });
      });
    });

    $('lib-sort').value = S.sort;
    $('lib-sort').addEventListener('change', function () {
      S.sort = this.value;
      LS.set('sort', S.sort);
      loadLibrary().catch(function (e) { toast(e.message, true); });
    });

    $('btn-rescan').addEventListener('click', function () {
      api('/api/library/scan', { method: 'POST' }).then(function () {
        toast('正在重新扫描…');
        setTimeout(function () { loadLibrary().catch(function () {}); }, 1200);
      }).catch(function (e) { toast(e.message, true); });
    });

    const openFiles = function () { $('file-input').click(); };
    $('btn-open-files').addEventListener('click', openFiles);
    $('btn-open-files-2').addEventListener('click', openFiles);
    $('file-input').addEventListener('change', function () {
      acceptFiles(this.files);
      this.value = '';
    });

    $$('#search-mode button').forEach(function (b) {
      b.addEventListener('click', function () {
        S.searchMode = b.dataset.mode;
        syncSegmented();
        const q = $('q').value.trim() || S.lastQuery;
        if (q) {
          $('q').value = q;
          go('search', { q: q, mode: S.searchMode });
          runSearch(q);
        }
      });
    });

    // 播放器
    $('btn-play').addEventListener('click', function () { player.toggle(); });
    $('mini-play').addEventListener('click', function (ev) { ev.stopPropagation(); player.toggle(); });
    $('btn-prev').addEventListener('click', function () { next(-1); });
    $('btn-next').addEventListener('click', function () { next(1); });
    $('mini-next').addEventListener('click', function (ev) { ev.stopPropagation(); next(1); });
    $('btn-back10').addEventListener('click', function () { player.nudge(-10); });
    $('btn-fwd10').addEventListener('click', function () { player.nudge(10); });
    $('btn-full').addEventListener('click', function () {
      if (S.current && S.current.kind === 'video') player.requestFullscreen();
      else document.documentElement.requestFullscreen && document.documentElement.requestFullscreen().catch(function () {});
    });
    $('btn-more').addEventListener('click', openCurrentMenu);
    $('mini-cover').addEventListener('click', function () { go('player'); });
    $('mini-open').addEventListener('click', function () { go('player'); });

    $('seek').addEventListener('pointerdown', function () { S.seeking = true; });
    $('seek').addEventListener('input', function () {
      const pct = Number(this.value) / 10;
      this.style.setProperty('--p', pct.toFixed(2) + '%');
      const d = player.duration || 0;
      $('t-cur').textContent = fmtTime(d * pct / 100);
    });
    $('seek').addEventListener('change', function () {
      const d = player.duration || 0;
      player.seekTo(d * (Number(this.value) / 1000));
      S.seeking = false;
    });
    $('seek').addEventListener('pointerup', function () { S.seeking = false; });

    const vol = LS.get('volume', 0.8);
    player.setVolume(vol);
    $('volume').value = String(Math.round(vol * 100));
    $('volume').style.setProperty('--p', Math.round(vol * 100) + '%');
    $('volume').addEventListener('input', function () {
      const v = Number(this.value) / 100;
      player.setVolume(v);
      this.style.setProperty('--p', Number(this.value) + '%');
      LS.set('volume', v);
    });

    const rate = LS.get('rate', 1);
    $('rate').value = String(rate);
    player.setRate(rate);
    $('rate').addEventListener('change', function () {
      player.setRate(this.value);
      LS.set('rate', player._rate);
    });

    $('btn-lyric-refetch').addEventListener('click', function () {
      if (!S.current) return;
      LV.offset = 0;
      loadLyricFor(S.current, true, 0);
      toast('已强制重新拉取歌词');
    });
    $('btn-lyric-pick').addEventListener('click', openCandidates);
    $('btn-offset-up').addEventListener('click', function () { toast('歌词偏移 ' + LV.nudgeOffset(0.1).toFixed(1) + 's'); });
    $('btn-offset-down').addEventListener('click', function () { toast('歌词偏移 ' + LV.nudgeOffset(-0.1).toFixed(1) + 's'); });
    $('btn-lyric-mode').addEventListener('click', function () {
      const big = LV.toggleBig();
      this.textContent = big ? '普通模式' : '大字模式';
    });
    $('btn-lyric-export').addEventListener('click', exportLRC);
    $('btn-lyric-import').addEventListener('click', function () {
      if (!S.current) { toast('先播放一首'); return; }
      $('lyric-file-input').click();
    });
    $('lyric-file-input').addEventListener('change', function () {
      const f = this.files && this.files[0];
      this.value = '';
      if (!f || !S.current) return;
      const ext = extOf(f.name);
      if (S.current.mediaId) {
        f.text().then(function (text) {
          return api('/api/lyric/' + encodeURIComponent(S.current.mediaId) + '?ext=' + encodeURIComponent(ext), {
            method: 'POST',
            body: text,
            headers: { 'Content-Type': 'text/plain; charset=utf-8' }
          });
        }).then(function (data) {
          applyLyric({ ok: true, doc: data.doc, source: 'manual' }, S.current);
          toast('已导入并缓存，' + (data.doc.cues ? data.doc.cues.length : 0) + ' 行');
        }).catch(function (e) { toast(e.message, true); });
      } else {
        f.text().then(function (text) {
          S.current.lyricText = text;
          S.current.lyricExt = ext;
          applyLyric({ ok: true, doc: L.parseAuto(text, ext), source: 'file' }, S.current);
          toast('已载入歌词 ' + f.name);
        });
      }
    });

    $('btn-reveal').addEventListener('click', function () {
      if (!S.current || !S.current.mediaId) return;
      api('/api/open', { method: 'POST', body: { id: S.current.mediaId, action: 'reveal' } }).catch(function (e) { toast(e.message, true); });
    });
    $('btn-open-player').addEventListener('click', function () {
      if (!S.current || !S.current.mediaId) return;
      api('/api/open', { method: 'POST', body: { id: S.current.mediaId, action: 'player' } }).catch(function (e) { toast(e.message, true); });
    });

    $('modal-close').addEventListener('click', closeModal);
    $('modal').addEventListener('click', function (ev) {
      if (ev.target === this) closeModal();
    });

    // 设置
    const toggle = function (id, field) {
      const el = $(id);
      if (!el) return;
      el.addEventListener('change', function () {
        const patch = {};
        patch[field] = !!this.checked;
        patchConfig(patch, function (err) {
          if (!err) toast('已保存');
        });
      });
    };
    toggle('sw-lrclib', 'enableLRCLIB');
    toggle('sw-netease', 'enableNeteaseLyricSearch');
    toggle('sw-sidecar', 'writeSidecar');
    toggle('sw-itunes', 'enableITunes');
    toggle('sw-archive', 'enableArchive');
    toggle('sw-ai', 'aiIdentify');
    toggle('sw-ai-audio', 'aiUploadAudio');

    ['in-os-key', 'in-jamendo', 'in-sources', 'in-token', 'in-ai-base', 'in-ai-model', 'in-ai-key'].forEach(function (id) {
      const el = $(id);
      if (el) el.addEventListener('input', function () { this.dataset.touched = '1'; });
    });

    $('dir-form').addEventListener('submit', function (ev) {
      ev.preventDefault();
      const v = $('dir-input').value.trim();
      if (!v) return;
      const dirs = ((S.config && S.config.mediaDirs) || []).slice();
      if (dirs.indexOf(v) >= 0) { toast('这个目录已经在列表里'); return; }
      dirs.push(v);
      $('dir-input').value = '';
      saveDirs(dirs);
    });

    $('btn-save-sources').addEventListener('click', function () {
      let arr;
      const raw = $('in-sources').value.trim();
      if (!raw) arr = [];
      else {
        try { arr = JSON.parse(raw); } catch (e) { toast('JSON 格式不对：' + e.message, true); return; }
        if (!Array.isArray(arr)) { toast('必须是一个 JSON 数组', true); return; }
      }
      const patch = { sources: arr };
      if ($('in-jamendo').dataset.touched) patch.jamendoClientId = $('in-jamendo').value.trim();
      if ($('in-os-key').dataset.touched) patch.opensubtitlesApiKey = $('in-os-key').value.trim();
      patchConfig(patch, function (err) {
        if (err) return;
        $('in-jamendo').dataset.touched = '';
        $('in-os-key').dataset.touched = '';
        toast('音源配置已保存');
      });
    });

    $('btn-save-ai').addEventListener('click', function () {
      const patch = {
        aiIdentify: !!$('sw-ai').checked,
        aiUploadAudio: !!$('sw-ai-audio').checked
      };
      if ($('in-ai-base').dataset.touched) patch.aiBaseUrl = $('in-ai-base').value.trim();
      if ($('in-ai-model').dataset.touched) patch.aiModel = $('in-ai-model').value.trim();
      if ($('in-ai-key').dataset.touched && $('in-ai-key').value.trim()) patch.aiApiKey = $('in-ai-key').value.trim();
      patchConfig(patch, function (err) {
        if (err) return;
        ['in-ai-base', 'in-ai-model', 'in-ai-key'].forEach(function (id) { $(id).dataset.touched = ''; });
        $('in-ai-key').value = '';
        toast('AI 识别设置已保存');
      });
    });

    $('btn-ai-test').addEventListener('click', function () {
      const btn = this;
      btn.disabled = true;
      api('/api/ai/check', { method: 'POST' }).then(function (d) {
        toast(d.ok ? '连通正常（' + (d.model || '模型') + '）' : (d.hint || '接口不可用'), !d.ok);
      }).catch(function (e) { toast(e.message, true); }).then(function () { btn.disabled = false; });
    });

    $('btn-identify-batch').addEventListener('click', function () {
      const btn = this;
      if (btn.disabled) return;
      btn.disabled = true;
      toast('正在逐条识别未匹配曲目…');
      api('/api/library/identify', { method: 'POST', body: { limit: 20, apply: true, onlyMissing: true } })
        .then(function (d) {
          const extra = d.failed ? '，' + d.failed + ' 条没把握' : '';
          toast('识别完成：处理 ' + d.processed + ' 条，写回 ' + d.updated + ' 条' + extra);
          return loadLibrary();
        })
        .catch(function (e) { toast(e.message, true); })
        .then(function () { btn.disabled = false; });
    });

    $('btn-identify').addEventListener('click', function () {
      const item = S.current;
      if (!item || !item.mediaId) { toast('先播放一首曲库里的歌'); return; }
      const btn = this;
      if (btn.disabled) return;
      btn.disabled = true;
      toast('正在识别这首歌…');
      api('/api/track/' + encodeURIComponent(item.mediaId) + '/identify', {
        method: 'POST',
        body: { apply: true, audio: true, fetchLyrics: true }
      }).then(function (d) {
        const best = d.best || {};
        if (d.item) {
          item.title = d.item.title || item.title;
          item.artist = d.item.artist || item.artist;
          item.album = d.item.album || item.album;
          $('art-title').textContent = item.title;
          $('art-artist').textContent = [item.artist, item.album].filter(Boolean).join(' · ');
        }
        if (d.apply) {
          toast('已识别为 ' + [best.title, best.artist].filter(Boolean).join(' - ') +
            (best.verified ? '' : '（未经歌词库校验，建议核对）'));
          loadLyricFor(item, false, 0);
          loadLibrary().catch(function () {});
        } else {
          toast(d.error || ('没找到可靠结果' + (best.title ? '，最接近：' + best.title : '')), true);
        }
      }).catch(function (e) {
        toast(e.message, true);
      }).then(function () { btn.disabled = false; });
    });

    $('btn-save-token').addEventListener('click', function () {
      const v = $('in-token').value.trim();
      const patch = { authToken: v };
      patchConfig(patch, function (err) {
        if (err) return;
        if (v) { S.token = v; LS.set('token', v); }
        $('in-token').value = '';
        $('in-token').dataset.touched = '';
        toast(v ? '访问令牌已设置，后续请求会带上' : '已关闭访问令牌');
      });
    });

    $('btn-clear-files').addEventListener('click', function () {
      S.files = [];
      S.queue = S.queue.filter(function (x) { return x.source !== 'file'; });
      renderSettings();
      toast('已清空本次打开的本地文件');
    });

    // 键盘
    document.addEventListener('keydown', function (ev) {
      const tag = (ev.target && ev.target.tagName) || '';
      const typing = /INPUT|TEXTAREA|SELECT/.test(tag) || (ev.target && ev.target.isContentEditable);
      if (ev.key === 'Escape') { closeModal(); return; }
      if (typing) return;
      if (ev.code === 'Space' && S.current) { ev.preventDefault(); player.toggle(); }
      else if (ev.key === 'ArrowLeft' && S.current) { ev.preventDefault(); player.nudge(-5); }
      else if (ev.key === 'ArrowRight' && S.current) { ev.preventDefault(); player.nudge(5); }
      else if (ev.key === 'ArrowUp' && S.current) {
        ev.preventDefault();
        const v = Math.min(1, player._volume + 0.05);
        player.setVolume(v);
        $('volume').value = String(Math.round(v * 100));
        $('volume').style.setProperty('--p', Math.round(v * 100) + '%');
        LS.set('volume', v);
      } else if (ev.key === 'ArrowDown' && S.current) {
        ev.preventDefault();
        const v = Math.max(0, player._volume - 0.05);
        player.setVolume(v);
        $('volume').value = String(Math.round(v * 100));
        $('volume').style.setProperty('--p', Math.round(v * 100) + '%');
        LS.set('volume', v);
      } else if (ev.key === '/') { ev.preventDefault(); $('q').focus(); }
    });

    // 拖拽文件
    let dragDepth = 0;
    const mask = document.createElement('div');
    mask.className = 'drop-mask';
    mask.hidden = true;
    mask.innerHTML = '<div class="box">松手即可播放<br><span class="tiny">音频 / 视频 / 同名 .lrc .srt .vtt .ass</span></div>';
    document.body.appendChild(mask);
    window.addEventListener('dragenter', function (ev) {
      if (!ev.dataTransfer || Array.prototype.indexOf.call(ev.dataTransfer.types || [], 'Files') < 0) return;
      dragDepth++;
      mask.hidden = false;
    });
    window.addEventListener('dragover', function (ev) { ev.preventDefault(); });
    window.addEventListener('dragleave', function () {
      dragDepth = Math.max(0, dragDepth - 1);
      if (!dragDepth) mask.hidden = true;
    });
    window.addEventListener('drop', function (ev) {
      ev.preventDefault();
      dragDepth = 0;
      mask.hidden = true;
      if (ev.dataTransfer && ev.dataTransfer.files && ev.dataTransfer.files.length) {
        acceptFiles(ev.dataTransfer.files);
      }
    });

    // 播放器事件
    player.on('time', updatePlayhead);
    player.on('duration', function (d) {
      if (!d && S.current && S.current.duration) d = S.current.duration;
      $('t-dur').textContent = fmtTime(d);
      if (S.current && !S.current.duration && d > 0) {
        S.current.duration = d;
        if (S.current.mediaId) {
          api('/api/track/' + encodeURIComponent(S.current.mediaId) + '/duration', {
            method: 'POST', body: { duration: d }
          }).catch(function () {});
        }
      }
    });
    player.on('playstate', function (playing) {
      setPlayIcon(playing);
      if (playing && S.blockedShown) { S.blockedShown = false; }
      $('stage-msg').hidden = true;
    });
    player.on('ended', function () { next(1); });
    player.on('error', function (msg) {
      const box = $('stage-msg');
      box.innerHTML = '<span>' + esc(msg || '播放失败') + '</span>';
      const retry = document.createElement('button');
      retry.className = 'mini-btn';
      retry.textContent = '用系统播放器打开';
      retry.addEventListener('click', function () {
        if (S.current && S.current.mediaId && S.desktop) {
          api('/api/open', { method: 'POST', body: { id: S.current.mediaId, action: 'player' } }).catch(function (e) { toast(e.message, true); });
        } else {
          toast('浏览器内核不支持这个容器（如 .mkv/.avi），桌面端可用系统播放器', true);
        }
      });
      box.appendChild(retry);
      box.hidden = false;
    });
    player.on('blocked', function () {
      if (S.blockedShown) return;
      S.blockedShown = true;
      const box = $('stage-msg');
      box.innerHTML = '<span>浏览器拦截了自动播放，点一下播放键继续</span>';
      box.hidden = false;
      setPlayIcon(false);
    });
    player.on('load', function (item) {
      $('art-title').textContent = item.title || '';
      $('art-artist').textContent = [item.artist, item.album].filter(Boolean).join(' · ');
      renderMini();
      renderLibrary();
    });

    window.addEventListener('resize', measureBars);
    if (window.ResizeObserver) {
      const ro = new ResizeObserver(measureBars);
      const head = document.querySelector('header.topbar');
      const foot = document.querySelector('footer.site-footer');
      if (head) ro.observe(head);
      if (foot) ro.observe(foot);
    }
  }

  /* ── 启动 ─────────────────────────────────────────── */
  function footerFallback() {
    const box = $('foot-links');
    if (!box) return;
    setTimeout(function () {
      if (box.innerHTML.trim()) return;
      box.innerHTML = '<a href="/">mplay</a><a href="https://github.com/hktalent" target="_blank" rel="noopener">GitHub</a>' +
        '<span class="tiny">本地与在线歌词同步的音视频播放器</span>';
      measureBars();
    }, 900);
  }

  async function init() {
    measureBars();
    bind();
    syncSegmented();
    syncFilters();
    defaultDirPlaceholder();
    footerFallback();
    setPlayIcon(false);
    renderMini();

    const r = parseHash();
    show(r.view);
    if (r.view === 'search' && r.params.get('q')) {
      $('q').value = r.params.get('q');
      S.searchMode = r.params.get('mode') === 'lyric' ? 'lyric' : 'song';
      syncSegmented();
      runSearch(r.params.get('q'));
    }

    try {
      await loadConfig();
      defaultDirPlaceholder();
    } catch (e) { /* 已提示 */ }
    try {
      await loadLibrary();
    } catch (e) {
      toast(e.message || '曲库读取失败', true);
    }
    deepPlay(r.params);
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
