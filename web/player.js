/* mplay · 播放引擎
   一个 <video> 元素同时承担音频与视频播放（音频时隐藏画面层），
   对外暴露统一状态与事件，Web / 桌面（urlbox 内嵌）共用。 */
(function (global) {
  'use strict';

  const M = (global.MPPlay = global.MPPlay || {});

  function clamp(v, min, max) { return Math.max(min, Math.min(max, v)); }
  const fmt = M.lyrics ? M.lyrics.fmtTime : function (s) { return String(s); };

  function Player(videoEl) {
    this.el = videoEl;
    this.el.preload = 'metadata';
    this.item = null;
    this.handlers = Object.create(null);
    this.duration = 0;
    this.isVideo = false;
    this._volume = 0.8;
    this._rate = 1;
    this._muted = false;
    this._bind();
  }

  Player.prototype.on = function (name, fn) {
    (this.handlers[name] = this.handlers[name] || []).push(fn);
    return this;
  };

  Player.prototype.emit = function (name, payload) {
    const list = this.handlers[name];
    if (list) for (let i = 0; i < list.length; i++) list[i](payload);
  };

  Player.prototype._bind = function () {
    const self = this;
    const el = this.el;

    el.addEventListener('loadedmetadata', function () {
      self.duration = isFinite(el.duration) ? el.duration : (self.item && self.item.duration) || 0;
      self.emit('duration', self.duration);
      self.emit('state', self.state());
    });
    el.addEventListener('durationchange', function () {
      self.duration = isFinite(el.duration) ? el.duration : 0;
      self.emit('duration', self.duration);
    });
    el.addEventListener('timeupdate', function () {
      self.emit('time', el.currentTime);
    });
    el.addEventListener('seeking', function () { self.emit('time', el.currentTime); });
    el.addEventListener('play', function () { self.emit('playstate', true); });
    el.addEventListener('pause', function () { self.emit('playstate', false); });
    el.addEventListener('ended', function () { self.emit('ended'); });
    el.addEventListener('waiting', function () { self.emit('waiting', true); });
    el.addEventListener('playing', function () { self.emit('waiting', false); });
    el.addEventListener('canplay', function () { self.emit('waiting', false); });
    el.addEventListener('error', function () {
      self.emit('error', self.errorText());
    });
    el.addEventListener('click', function () {
      if (self.isVideo) self.toggle();
    });
    el.addEventListener('dblclick', function () {
      if (self.isVideo) self.requestFullscreen();
    });
  };

  Player.prototype.errorText = function () {
    const el = this.el;
    const err = el.error;
    if (!err) return T('播放失败');
    switch (err.code) {
      case 1: return T('播放被中断');
      case 2: return T('读取源时出错（网络或地址不可用）');
      case 3: return T('解码失败，可能是这个格式当前浏览器/内核不支持');
      case 4: return T('格式不支持或文件不可访问（试试换 .mp3/.m4a/.mp4）');
      default: return T('播放失败');
    }
  };

  Player.prototype.state = function () {
    return {
      playing: !this.el.paused && !this.el.ended,
      time: this.el.currentTime || 0,
      duration: this.duration,
      volume: this._volume,
      muted: this._muted,
      rate: this._rate,
      item: this.item,
      isVideo: this.isVideo
    };
  };

  /* 载入一条曲目。item = {id,url,kind,title,artist,cover,duration,isLocalFile} */
  Player.prototype.load = function (item, autoplay) {
    const self = this;
    this.item = item;
    this.isVideo = item.kind === 'video';
    const el = this.el;
    this.duration = item.duration || 0;
    this.emit('duration', this.duration);

    if (this.isVideo) {
      el.hidden = false;
      el.removeAttribute('hidden');
    } else {
      el.hidden = true;
      el.setAttribute('hidden', '');
    }
    el.setAttribute('playsinline', '');
    el.src = item.url;
    el.load();
    if (this.isVideo) {
      el.style.objectFit = 'contain';
    }
    if (autoplay !== false) {
      const p = el.play();
      if (p && p.catch) {
        p.catch(function (e) {
          // 浏览器自动播放策略：需要用户手势
          self.emit('blocked', e && e.name ? e.name : 'NotAllowedError');
        });
      }
    }
    this.emit('load', item);
    this.emit('state', this.state());
    return this;
  };

  Player.prototype.play = function () {
    const p = this.el.play();
    if (p && p.catch) {
      const self = this;
      p.catch(function (e) { self.emit('blocked', e && e.name ? e.name : 'NotAllowedError'); });
    }
  };
  Player.prototype.pause = function () { this.el.pause(); };
  Player.prototype.toggle = function () {
    if (!this.item) return;
    if (this.el.paused) this.play(); else this.pause();
  };

  Player.prototype.seekTo = function (sec) {
    if (!isFinite(sec)) return;
    const d = this.duration || this.el.duration || 0;
    this.el.currentTime = clamp(sec, 0, d > 0 ? d : sec);
    this.emit('time', this.el.currentTime);
  };
  Player.prototype.nudge = function (delta) { this.seekTo((this.el.currentTime || 0) + delta); };

  Player.prototype.setVolume = function (v) {
    this._volume = clamp(v, 0, 1);
    this.el.volume = this._muted ? 0 : this._volume;
    this.emit('volume', this._volume);
    this.emit('state', this.state());
  };
  Player.prototype.toggleMute = function () {
    this._muted = !this._muted;
    this.el.volume = this._muted ? 0 : this._volume;
    this.emit('volume', this._muted ? 0 : this._volume);
    this.emit('state', this.state());
    return this._muted;
  };
  Player.prototype.setRate = function (r) {
    this._rate = clamp(Number(r) || 1, 0.5, 3);
    this.el.playbackRate = this._rate;
    this.emit('rate', this._rate);
    this.emit('state', this.state());
  };
  Player.prototype.requestFullscreen = function () {
    const el = this.el;
    try {
      if (document.documentElement.requestFullscreen && el.requestFullscreen) {
        el.requestFullscreen().catch(function () {});
      } else if (el.webkitRequestFullscreen) {
        el.webkitRequestFullscreen();
      } else if (el.webkitEnterFullscreen) {
        el.webkitEnterFullscreen();
      } else {
        return false;
      }
      return true;
    } catch (e) {
      return false;
    }
  };

  M.Player = Player;

  /* ── 音频可视化 ─────────────────────────────────────
     一处分析、多处显示：Spectrum 负责取频谱（整页只有一个），
     Bars 负责把 levels 画成竖条，播放页大条带和迷你条的小频谱共用同一份数据。
     为了不把主播放元素绑进 WebAudio（跨域音源会被静音，且不可撤销），
     分析用一只隐藏的 <audio> 拉同一路流，输出接 gain=0，听不到第二次声音。
     拿不到真实频谱（跨域 / 无 WebAudio / 持续无信号）时退回装饰动画。 */
  const VIZ_N = 48;

  function canAnalyze(url) {
    if (!url) return false;
    if (String(url).slice(0, 5) === 'blob:') return true;
    try {
      const u = new URL(url, location.href);
      return u.origin === location.origin;
    } catch (e) {
      return false;
    }
  }

  /* 竖条渲染器：n 根柱子，draw(levels) 把 48 个频段采样成自己的根数 */
  function Bars(el, n) {
    this.el = el;
    this.n = n;
    this.bars = [];
    this.el.hidden = true;
    const frag = document.createDocumentFragment();
    for (let i = 0; i < n; i++) {
      const b = document.createElement('i');
      b.style.animationDelay = (-(0.15 + (i % 9) * 0.11)).toFixed(2) + 's';
      frag.appendChild(b);
      this.bars.push(b);
    }
    this.el.innerHTML = '';
    this.el.appendChild(frag);
  }

  Bars.prototype.clear = function () {
    for (let i = 0; i < this.n; i++) {
      if (this.bars[i]) this.bars[i].style.height = '8%';
    }
  };

  Bars.prototype.draw = function (levels) {
    const len = levels.length;
    for (let i = 0; i < this.n; i++) {
      const lo = Math.floor((i / this.n) * len);
      const hi = Math.max(lo + 1, Math.floor(((i + 1) / this.n) * len));
      let sum = 0, c = 0;
      for (let j = lo; j < hi && j < len; j++) { sum += levels[j]; c++; }
      const v = c ? sum / c : 0;
      const h = Math.max(6, Math.min(100, v * 100));
      if (this.bars[i]) this.bars[i].style.height = h.toFixed(1) + '%';
    }
  };

  Bars.prototype.setVisible = function (v) { this.el.hidden = !v; };

  Bars.prototype.setPlaying = function (playing) {
    this.el.classList.toggle('is-playing', !!playing);
  };

  Bars.prototype.setFake = function (fake) {
    this.el.classList.toggle('is-fake', !!fake);
  };

  /* 频谱数据源：一个页面只建一份，首次真正需要时才建 AudioContext */
  function Spectrum(player) {
    this.player = player;
    this.levels = new Array(VIZ_N).fill(0);
    this.bars = [];
    this.playing = false;
    this.mode = 'fake'; // fake | real
    this.raf = 0;
    this.silent = 0;
    this.ctx = null;
    this.analyser = null;
    this.data = null;
    this.anEl = null;
  }

  Spectrum.prototype.attach = function (renderer) {
    this.bars.push(renderer);
    return renderer;
  };

  Spectrum.prototype.ensureGraph = function () {
    if (this.analyser) return true;
    const AC = window.AudioContext || window.webkitAudioContext;
    if (!AC) return false;
    try {
      this.ctx = new AC();
      this.anEl = document.createElement('audio');
      this.anEl.preload = 'auto';
      this.anEl.style.cssText = 'position:absolute;left:-9999px;width:0;height:0;opacity:0;pointer-events:none';
      document.body.appendChild(this.anEl);
      const src = this.ctx.createMediaElementSource(this.anEl);
      const an = this.ctx.createAnalyser();
      an.fftSize = 1024;
      an.smoothingTimeConstant = 0.72;
      const mute = this.ctx.createGain();
      mute.gain.value = 0; // 只取频谱，不出声
      src.connect(an);
      an.connect(mute);
      mute.connect(this.ctx.destination);
      this.analyser = an;
      this.data = new Uint8Array(an.frequencyBinCount);
      return true;
    } catch (e) {
      this.analyser = null;
      return false;
    }
  };

  Spectrum.prototype.reset = function (item) {
    this.stop();
    this.levelClear();
    this.anElStop();
    const isAudio = !!(item && item.kind !== 'video');
    this.bars.forEach(function (r) { r.setVisible(isAudio); r.clear(); r.setFake(false); });
  };

  Spectrum.prototype.levelClear = function () {
    this.levels = new Array(VIZ_N).fill(0);
    this.bars.forEach(function (r) { r.clear(); });
  };

  Spectrum.prototype.anElStop = function () {
    if (this.anEl) {
      try { this.anEl.pause(); } catch (e) {}
    }
  };

  Spectrum.prototype.stop = function () {
    this.playing = false;
    if (this.raf) { cancelAnimationFrame(this.raf); this.raf = 0; }
    this.bars.forEach(function (r) { r.setPlaying(false); });
  };

  Spectrum.prototype.pause = function () {
    this.stop();
    this.anElStop();
    this.levelClear();
  };

  Spectrum.prototype.toFake = function () {
    this.mode = 'fake';
    this.anElStop();
    this.levelClear();
    const playing = this.playing;
    this.bars.forEach(function (r) {
      r.setFake(true);
      r.setPlaying(playing);
    });
  };

  Spectrum.prototype.start = function () {
    const item = this.player.item;
    if (!item || item.kind === 'video') return;
    this.playing = true;

    if (canAnalyze(item.url) && this.ensureGraph()) {
      if (this.ctx.state === 'suspended' && this.ctx.resume) {
        this.ctx.resume().catch(function () {});
      }
      try {
        this.anEl.src = item.url;
        this.anEl.currentTime = this.player.el.currentTime || 0;
        const p = this.anEl.play();
        if (p && p.catch) p.catch(function () {});
      } catch (e) {}
      this.mode = 'real';
      this.bars.forEach(function (r) {
        r.setFake(false);
        r.setPlaying(true);
        r.setVisible(true);
      });
      this.silent = 0;
      this.loop();
      return;
    }
    // 跨域 / 无 WebAudio：装饰动画，节奏仍跟播放状态走
    this.mode = 'fake';
    this.bars.forEach(function (r) {
      r.setFake(true);
      r.setPlaying(true);
      r.setVisible(true);
    });
  };

  Spectrum.prototype.loop = function () {
    const self = this;
    const step = function () {
      if (!self.playing || self.mode !== 'real') return;
      self.raf = requestAnimationFrame(step);
      const an = self.analyser;
      if (!an || !self.data) return;
      an.getByteFrequencyData(self.data);
      const bins = self.data.length;
      let peak = 0;
      for (let i = 0; i < VIZ_N; i++) {
        // 低频占更多格子（人耳对低频更敏感，视觉上也更像频谱）
        const lo = Math.floor(Math.pow(i / VIZ_N, 1.7) * bins);
        const hi = Math.min(bins, Math.max(lo + 1, Math.floor(Math.pow((i + 1) / VIZ_N, 1.7) * bins)));
        let sum = 0, c = 0;
        for (let j = lo; j < hi; j++) { sum += self.data[j]; c++; }
        let v = c ? sum / c / 255 : 0;
        v = Math.pow(v, 0.8);
        if (v > peak) peak = v;
        const prev = self.levels[i];
        self.levels[i] = v > prev ? v : prev * 0.86; // 上升快、下落慢
      }
      self.bars.forEach(function (r) { r.draw(self.levels); });
      // 连续 5 秒完全没有信号（多半是解码拿不到数据），退回装饰动画
      if (peak < 0.004) {
        if (++self.silent > 300) self.toFake();
      } else {
        self.silent = 0;
      }
    };
    step();
  };

  /* 把频谱接到若干容器上：main 是播放页大条带，mini 是最小化时的迷你条 */
  Player.prototype.setupViz = function (mainEl, miniEl) {
    if (!mainEl) return;
    const self = this;
    this.spectrum = new Spectrum(this);
    const main = this.spectrum.attach(new Bars(mainEl, 48));
    const mini = miniEl ? this.spectrum.attach(new Bars(miniEl, 12)) : null;
    this.on('load', function (item) { self.spectrum.reset(item); });
    this.on('playstate', function (playing) { if (playing) self.spectrum.start(); else self.spectrum.pause(); });
    this.on('ended', function () { self.spectrum.pause(); });
    return { main: main, mini: mini };
  };

  M.Viz = Spectrum;
  M.Bars = Bars;
})(window);
