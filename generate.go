package av

// Prebuilt FFmpeg static libraries are not included in the Go module.
// After `go get`, download them for your target platform:
//
//	go run github.com/mmngadi/fyne-av/cmd/fetch-libs
//
// For cross-compiling, set GOOS/GOARCH first:
//
//	GOOS=android GOARCH=arm64 go run github.com/mmngadi/fyne-av/cmd/fetch-libs
//
//go:generate go run ./cmd/fetch-libs