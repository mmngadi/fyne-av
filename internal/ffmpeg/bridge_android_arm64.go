//go:build android && arm64

package ffmpeg

// #cgo CFLAGS: -I${SRCDIR}/../../libs/android_arm64/include
// #cgo LDFLAGS: -L${SRCDIR}/../../libs/android_arm64/lib -lavformat -lavcodec -lswscale -lswresample -lavutil -llog -lOpenSLES -lz -lm
import "C"