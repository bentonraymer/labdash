package icons

import (
	"net/url"
	"reflect"
	"testing"
)

func TestFindIconLinks(t *testing.T) {
	base, _ := url.Parse("https://jf.example.com/web/index.html")
	html := []byte(`<head>
		<link rel="stylesheet" href="/x.css">
		<link rel="icon" type="image/png" sizes="32x32" href="favicon-32.png">
		<link rel="apple-touch-icon" href="/touchicon.png">
		<link href="/icon-192.png" rel="icon" sizes="192x192">
		<link rel="shortcut icon" href="https://cdn.example.com/f.ico">
	</head>`)
	got := findIconLinks(html, base)
	want := []string{
		"https://jf.example.com/icon-192.png",
		"https://jf.example.com/touchicon.png",
		"https://jf.example.com/web/favicon-32.png",
		"https://cdn.example.com/f.ico",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func TestSniffImage(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	if ext, err := SniffImage(png); err != nil || ext != ".png" {
		t.Fatalf("png: %v %v", ext, err)
	}
	svg := []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"></svg>`)
	if ext, err := SniffImage(svg); err != nil || ext != ".svg" {
		t.Fatalf("svg: %v %v", ext, err)
	}
	if _, err := SniffImage([]byte("<html>nope</html>")); err == nil {
		t.Fatal("html should be rejected")
	}
}
