/* mplay · 歌词与字幕
   - 解析 LRC / SRT / VTT / ASS → 统一 cue 模型 {s, e, t, tr, words}
   - 渲染同步滚动歌词面板（逐字高亮、偏移校正、点击跳转）
   与后端 internal/lyrics 的模型保持一致：s/e 为秒，t 主行，tr 译文行，
   words = [{t, w}] 为增强型 LRC 的逐字时间点。 */
(function (global) {
  'use strict';

  const M = (global.MPPlay = global.MPPlay || {});

  function pad(n) { return (n < 10 ? '0' : '') + n; }

  function fmtTime(sec) {
    if (!isFinite(sec) || sec < 0) sec = 0;
    const m = Math.floor(sec / 60);
    const s = Math.floor(sec % 60);
    return m + ':' + pad(s);
  }

  /* ── 时间戳解析 ───────────────────────────────────── */
  function parseStamp(s) {
    if (!s) return NaN;
    s = String(s).trim();
    let m = s.match(/^(\d+):(\d{1,2})(?:[.,](\d{1,3}))?$/);
    if (m) {
      let frac = m[3] || '0';
      frac = (frac + '000').slice(0, 3);
      return parseInt(m[1], 10) * 60 + parseInt(m[2], 10) + parseInt(frac, 10) / 1000;
    }
    m = s.match(/^(\d+):(\d{1,2}):(\d{1,2})(?:[.,](\d{1,3}))?$/);
    if (m) {
      let frac = m[4] || '0';
      frac = (frac + '000').slice(0, 3);
      return parseInt(m[1], 10) * 3600 + parseInt(m[2], 10) * 60 +
        parseInt(m[3], 10) + parseInt(frac, 10) / 1000;
    }
    return NaN;
  }

  const TIME_TAG = /<(\d+):(\d{1,2})(?:[.,](\d{1,3}))?>/g;
  const LINE_TIME = /\[(\d+:\d{1,2}(?::\d{1,2})?(?:[.,]\d{1,3})?)\]/g;

  /* ── LRC ──────────────────────────────────────────── */
  function parseLRC(text) {
    const cues = [];
    let offset = 0;
    const meta = {};
    const lines = text.replace(/\r\n?/g, '\n').split('\n');

    for (const raw of lines) {
      const line = raw.trim();
      if (!line) continue;

      const metaRe = /^\[(\w+):([^\]]*)\]$/;
      const mm = line.match(metaRe);
      if (mm && !/\d+:\d/.test(mm[2])) {
        const k = mm[1].toLowerCase();
        if (k === 'offset') offset = (parseFloat(mm[2]) || 0) / 1000;
        else meta[k] = mm[2].trim();
        continue;
      }

      // 多时间戳共享一行文本： [00:12.00][01:30.00]歌词
      const stamps = [];
      LINE_TIME.lastIndex = 0;
      let tm;
      let lastEnd = 0;
      while ((tm = LINE_TIME.exec(line)) !== null) {
        if (tm.index !== lastEnd) break;
        const t = parseStamp(tm[1]);
        if (isFinite(t)) stamps.push(t);
        lastEnd = tm.index + tm[0].length;
      }
      if (!stamps.length) continue;

      let body = line.slice(lastEnd).trim();
      let words = null;
      if (TIME_TAG.test(body)) {
        TIME_TAG.lastIndex = 0;
        const seg = [];
        let prevT = null;
        let cursor = 0;
        let m2;
        let pieces = [];
        while ((m2 = TIME_TAG.exec(body)) !== null) {
          const t = parseStamp(m2[1] + ':' + m2[2] + (m2[3] ? '.' + m2[3] : ''));
          const txt = body.slice(cursor, m2.index);
          if (txt) pieces.push({ t: prevT, w: txt });
          prevT = t;
          cursor = m2.index + m2[0].length;
        }
        const tail = body.slice(cursor);
        if (tail) pieces.push({ t: prevT, w: tail });
        words = pieces.filter(function (p) { return p.w; });
        body = pieces.map(function (p) { return p.w; }).join('');
        TIME_TAG.lastIndex = 0;
        void seg;
      }

      for (const t of stamps) {
        cues.push({ s: t + offset, e: 0, t: body, tr: '', words: words });
      }
    }

    cues.sort(function (a, b) { return a.s - b.s; });
    finish(cues, meta);
    return { format: 'lrc', synced: cues.length > 0, cues: cues, meta: meta };
  }

  function finish(cues, meta) {
    for (let i = 0; i < cues.length; i++) {
      const next = cues[i + 1];
      if (next && next.s > cues[i].s) {
        cues[i].e = Math.min(next.s, cues[i].s + 8);
      } else if (!cues[i].e || cues[i].e <= cues[i].s) {
        cues[i].e = cues[i].s + Math.max(2, Math.min(8, (cues[i].t || '').length * 0.28));
      }
    }
    if (meta && meta.length) {
      const total = parseFloat(meta.length);
      if (isFinite(total) && total > 0 && cues.length) {
        const last = cues[cues.length - 1];
        if (last.e > total + 1) last.e = total;
      }
    }
  }

  /* ── SRT ──────────────────────────────────────────── */
  function parseSRT(text) {
    const norm = text.replace(/\r\n?/g, '\n').replace(/^\uFEFF/, '');
    const blocks = norm.split(/\n{2,}/);
    const cues = [];
    for (const block of blocks) {
      const lines = block.split('\n').filter(function (l) { return l.trim() !== ''; });
      if (!lines.length) continue;
      let idx = 0;
      if (/^\d+$/.test(lines[0].trim())) idx = 1;
      const tl = lines[idx] || '';
      const m = tl.match(/([0-9:,.]{8,})\s*-->\s*([0-9:,.]{8,})/);
      if (!m) continue;
      const s = parseStamp(m[1]);
      const e = parseStamp(m[2]);
      if (!isFinite(s)) continue;
      const bodyLines = lines.slice(idx + 1).join('\n').trim();
      const split = splitBilingual(bodyLines);
      cues.push({ s: s, e: isFinite(e) ? e : s + 3, t: split.t, tr: split.tr, words: null });
    }
    cues.sort(function (a, b) { return a.s - b.s; });
    return { format: 'srt', synced: cues.length > 0, cues: cues };
  }

  function parseVTT(text) {
    const norm = text.replace(/\r\n?/g, '\n').replace(/^\uFEFF/, '');
    const blocks = norm.split(/\n{2,}/);
    const cues = [];
    for (const block of blocks) {
      const lines = block.split('\n').filter(function (l) { return l.trim() !== ''; });
      if (!lines.length) continue;
      if (lines[0].indexOf('WEBVTT') === 0 || /^NOTE/.test(lines[0])) continue;
      let idx = 0;
      if (!/-->/.test(lines[0]) && idx + 1 < lines.length) idx = 1;
      const m = (lines[idx] || '').match(/([0-9:.,]+)\s*-->\s*([0-9:.,]+)/);
      if (!m) continue;
      const s = parseStamp(m[1]);
      const e = parseStamp(m[2]);
      if (!isFinite(s)) continue;
      const body = lines.slice(idx + 1).join('\n')
        .replace(/<[^>]+>/g, '')
        .trim();
      const split = splitBilingual(body);
      cues.push({ s: s, e: isFinite(e) ? e : s + 3, t: split.t, tr: split.tr, words: null });
    }
    cues.sort(function (a, b) { return a.s - b.s; });
    return { format: 'vtt', synced: cues.length > 0, cues: cues };
  }

  /* ── ASS/SSA ──────────────────────────────────────── */
  function parseASS(text) {
    const lines = text.replace(/\r\n?/g, '\n').split('\n');
    const cues = [];
    let inEvents = false;
    for (const raw of lines) {
      const line = raw.trim();
      if (/^\[.*\]$/.test(line)) {
        inEvents = /^\[events\]/i.test(line);
        continue;
      }
      if (!inEvents) continue;
      if (/^Format:/i.test(line)) continue;
      if (!/^Dialogue:/i.test(line)) continue;
      const body = line.replace(/^Dialogue:\s*/i, '');
      const parts = body.split(',');
      if (parts.length < 10) continue;
      const s = parseStamp(parts[1].trim());
      const e = parseStamp(parts[2].trim());
      if (!isFinite(s)) continue;
      const txt = parts.slice(9).join(',')
        .replace(/\{[^}]*\}/g, '')
        .replace(/\\N/g, '\n')
        .replace(/\\n/g, '\n')
        .trim();
      const split = splitBilingual(txt);
      cues.push({ s: s, e: isFinite(e) ? e : s + 3, t: split.t, tr: split.tr, words: null });
    }
    cues.sort(function (a, b) { return a.s - b.s; });
    return { format: 'ass', synced: cues.length > 0, cues: cues };
  }

  /* 双语字幕：两行文本里一行中文一行英文时拆成 主行 + 译文 */
  function splitBilingual(text) {
    if (!text) return { t: '', tr: '' };
    const lines = text.split('\n').map(function (l) { return l.trim(); }).filter(Boolean);
    if (lines.length < 2) return { t: lines.join(' '), tr: '' };
    const cjk = function (s) { return /[\u4e00-\u9fff\u3040-\u30ff\uac00-\ud7af]/.test(s); };
    const zh = lines.find(cjk);
    const other = lines.find(function (l) { return l !== zh; });
    if (zh && other) {
      const first = lines[0];
      if (cjk(first)) return { t: zh, tr: other === zh ? '' : other };
      return { t: zh, tr: other };
    }
    return { t: lines[0], tr: lines.slice(1).join(' ') };
  }

  /* ── 自动识别 ─────────────────────────────────────── */
  function parseAuto(text, extOrFormat) {
    const head = text.slice(0, 400);
    let format = (extOrFormat || '').replace(/^\./, '').toLowerCase();
    if (!format || format === 'txt') {
      if (/^WEBVTT/.test(text.trim())) format = 'vtt';
      else if (/\[\d+:\d{1,2}/.test(text) || LINE_TIME.test(head)) format = 'lrc';
      else if (/-->/i.test(head)) format = /^[0-9]+\s*\n/im.test(text) ? 'srt' : 'vtt';
      else if (/\[Script Info\]/i.test(head) || /\[Events\]/i.test(head)) format = 'ass';
      else format = 'lrc';
    }
    LINE_TIME.lastIndex = 0;
    TIME_TAG.lastIndex = 0;
    let doc;
    if (format === 'srt') doc = parseSRT(text);
    else if (format === 'vtt') doc = parseVTT(text);
    else if (format === 'ass' || format === 'ssa') { doc = parseASS(text); doc.format = 'ass'; }
    else doc = parseLRC(text);
    doc.raw = text;
    return doc;
  }

    /* ── 同步歌词视图 ─────────────────────────────────── */
  function LyricView(el, opts) {
    this.el = el;
    this.opts = opts || {};
    this.doc = null;
    this.lines = [];
    this.active = -1;
    this.offset = 0;
    this.onSeek = this.opts.onSeek || null;
    this._rafPending = false;
    this._big = false;
  }

  LyricView.prototype.clear = function () {
    this.doc = null;
    this.lines = [];
    this.active = -1;
    this.el.innerHTML = '';
    this.el.classList.remove('is-big');
    this._big = false;
  };

  LyricView.prototype.empty = function (title, desc) {
    this.clear();
    const box = document.createElement('div');
    box.className = 'lyric-empty';
    const p1 = document.createElement('p');
    p1.textContent = title || T('还没有歌词');
    const p2 = document.createElement('p');
    p2.className = 'tiny';
    p2.textContent = desc || '';
    box.appendChild(p1);
    if (desc) box.appendChild(p2);
    this.el.appendChild(box);
  };

  LyricView.prototype.setDoc = function (doc) {
    this.clear();
    this.doc = doc || null;
    if (!doc || !doc.cues || !doc.cues.length) return 0;

    const frag = document.createDocumentFragment();
    const cues = doc.cues;
    for (let i = 0; i < cues.length; i++) {
      const c = cues[i];
      const row = document.createElement('div');
      row.className = 'lyric-line' + (isFinite(c.s) ? '' : ' no-time');
      row.dataset.i = String(i);
      if (c.words && c.words.length) {
        for (const w of c.words) {
          const span = document.createElement('span');
          span.className = 'w';
          span.dataset.t = String(w.t || 0);
          span.textContent = w.w;
          row.appendChild(span);
        }
      } else {
        row.appendChild(document.createTextNode(c.t || ''));
      }
      if (c.tr) {
        const tr = document.createElement('span');
        tr.className = 'tr';
        tr.textContent = c.tr;
        row.appendChild(tr);
      }
      const self = this;
      row.addEventListener('click', function () {
        const cue = cues[Number(this.dataset.i)];
        if (cue && isFinite(cue.s) && self.onSeek) self.onSeek(cue.s);
      });
      frag.appendChild(row);
    }
    this.el.appendChild(frag);
    this.lines = Array.prototype.slice.call(this.el.querySelectorAll('.lyric-line'));
    return this.lines.length;
  };

  LyricView.prototype.setOffset = function (sec) {
    this.offset = sec || 0;
    const t = this._lastTime || 0;
    this.active = -1;
    this.tick(t, true);
  };
  LyricView.prototype.nudgeOffset = function (delta) {
    this.setOffset(Math.round((this.offset + delta) * 100) / 100);
    return this.offset;
  };

  LyricView.prototype._find = function (t) {
    const cues = this.doc && this.doc.cues;
    if (!cues || !cues.length) return -1;
    let lo = 0, hi = cues.length - 1, ans = -1;
    while (lo <= hi) {
      const mid = (lo + hi) >> 1;
      if (cues[mid].s + this.offset <= t) { ans = mid; lo = mid + 1; }
      else hi = mid - 1;
    }
    if (ans >= 0) {
      const c = cues[ans];
      if (c.e && c.e + this.offset < t - 0.05) return ans; // 保留上一句到最后一句之间
    }
    return ans;
  };

  LyricView.prototype.tick = function (time, force) {
    this._lastTime = time;
    if (!this.doc || !this.doc.cues || !this.doc.cues.length) return;
    const idx = this._find(time);
    if (idx !== this.active) {
      if (this.active >= 0 && this.lines[this.active]) {
        this.lines[this.active].classList.remove('is-active');
        Array.prototype.forEach.call(this.lines[this.active].querySelectorAll('.w'), function (w) {
          w.classList.remove('is-sung');
        });
      }
      this.active = idx;
      if (idx >= 0 && this.lines[idx]) {
        this.lines[idx].classList.add('is-active');
        this._scrollTo(this.lines[idx], force);
      }
    }
    const line = idx >= 0 ? this.lines[idx] : null;
    if (line) {
      const ws = line.querySelectorAll('.w');
      if (ws.length) {
        for (let i = 0; i < ws.length; i++) {
          const wt = Number(ws[i].dataset.t);
          ws[i].classList.toggle('is-sung', time + this.offset >= wt);
        }
      }
    }
  };

  LyricView.prototype._scrollTo = function (line, force) {
    const self = this;
    if (this._rafPending && !force) return;
    this._rafPending = true;
    const run = function () {
      self._rafPending = false;
      if (!line.parentNode) return;
      const target = line.offsetTop - self.el.clientHeight * 0.34;
      self.el.scrollTo({ top: Math.max(0, target), behavior: force ? 'auto' : 'smooth' });
    };
    if (force) run();
    else requestAnimationFrame(run);
  };

  LyricView.prototype.toggleBig = function () {
    this._big = !this._big;
    this.el.classList.toggle('is-big', this._big);
    return this._big;
  };

  LyricView.prototype.activeCue = function () {
    if (!this.doc || this.active < 0) return null;
    return this.doc.cues[this.active] || null;
  };

  /* 导出为 LRC（默认按 [00:01.23] 精度） */
  LyricView.prototype.toLRC = function (title, artist) {
    if (!this.doc || !this.doc.cues) return '';
    const cues = this.doc.cues;
    let out = '';
    if (title) out += '[ti:' + title + ']\n';
    if (artist) out += '[ar:' + artist + ']\n';
    out += '[by:mplay]\n';
    if (this.offset) out += '[offset:' + Math.round(-this.offset * 1000) + ']\n';
    for (const c of cues) {
      let t = c.s + this.offset;
      if (!isFinite(t) || t < 0) t = 0;
      const m = Math.floor(t / 60);
      const s = (t % 60).toFixed(2);
      out += '[' + pad(m) + ':' + (parseFloat(s) < 10 ? '0' : '') + s + ']' + (c.t || '') + '\n';
    }
    return out;
  };

  M.lyrics = {
    parseAuto: parseAuto,
    parseLRC: parseLRC,
    parseSRT: parseSRT,
    parseVTT: parseVTT,
    parseASS: parseASS,
    parseStamp: parseStamp,
    splitBilingual: splitBilingual,
    fmtTime: fmtTime,
    LyricView: LyricView
  };
})(window);
