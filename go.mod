module github.com/xmz14/lll

go 1.26.0

replace github.com/giraffesyo/pdf => ./third_party/giraffesyo-pdf

require (
	github.com/jchv/go-webview2 v0.0.0-20260205173254-56598839c808
	github.com/klippa-app/go-pdfium v1.21.1
	github.com/tetratelabs/wazero v1.12.0
)

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/jchv/go-winloader v0.0.0-20250406163304-c1995be93bd1 // indirect
	github.com/jolestar/go-commons-pool/v2 v2.1.2 // indirect
)

require (
	github.com/giraffesyo/pdf v0.7.0
	golang.org/x/sys v0.48.0
	golang.org/x/text v0.42.0
)
