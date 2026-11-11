module github.com/cxnaive/herdr-desktop

go 1.27.3

require (
	github.com/Microsoft/go-winio v0.6.2
	github.com/coder/websocket v1.8.15
	github.com/egoist/mygo v0.8.1
	github.com/egoist/mygo/herdr-terminal v0.8.1
	golang.org/x/image v0.46.0
	golang.org/x/sys v0.49.0
)

require (
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)

tool github.com/egoist/mygo/cmd/mygo

replace github.com/egoist/mygo/herdr-terminal => ./third_party/mygo-terminal
