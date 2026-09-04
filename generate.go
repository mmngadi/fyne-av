package av

// Prebuilt FFmpeg static libraries are not included in the Go module.
// After `go get`, install the fetch-libs tool and run it:
//
//	go install github.com/mmngadi/fyne-av/cmd/fetch-libs@latest
//	fetch-libs
//
// For cross-compiling, use -os and -arch flags:
//
//	fetch-libs -os android -arch arm64
//
//go:generate fetch-libs