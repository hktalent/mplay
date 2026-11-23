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
    if (!err) return '播放失败';
    switch (err.code) {
      case 1: return '播放被中断';
      case 2: return '读取源时出错（网络或地址不可用）';
      case 3: return '解码失败，可能是这个格式当前浏览器/内核不支持';
      case 4: return '格式不支持或文件不可访问（试试换 .mp3/.m4a/.mp4）';
      default: return '播放失败';
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
})(window);
