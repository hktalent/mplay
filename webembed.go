package main

import "embed"

// 整个 web/ 目录编译进二进制：部署时只需要一个文件，
// 也避免 urlbox 桌面壳启动时还要依赖旁边的一堆 js/css。
//
//go:embed all:web
var embeddedWeb embed.FS
