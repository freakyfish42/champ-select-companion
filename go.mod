module github.com/freakyfish42/champ-select-companion

go 1.22

require (
	fyne.io/systray v1.11.0
	github.com/jchv/go-webview2 v0.0.0-20250406165304-0bcfea011047
	golang.org/x/sys v0.30.0
)

require (
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/jchv/go-winloader v0.0.0-20250406163304-c1995be93bd1 // indirect
)

// Dependencies are vendored from source in third_party/ (see build.sh).
replace (
	fyne.io/systray => ./third_party/systray
	github.com/godbus/dbus/v5 => ./third_party/dbus
	github.com/jchv/go-webview2 => ./third_party/go-webview2
	github.com/jchv/go-winloader => ./third_party/go-winloader
	golang.org/x/sys => ./third_party/sys
)
