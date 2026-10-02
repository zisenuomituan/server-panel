// Package version 保存构建时注入的版本号。
package version

// Version 由 Makefile 通过 -ldflags 覆盖成 VERSION 文件里的值。
var Version = "dev"
