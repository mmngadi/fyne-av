//go:build windows && amd64

package ffmpeg

// #cgo CFLAGS: -I${SRCDIR}/../../libs/windows_x86_64/include
// #cgo LDFLAGS: -L${SRCDIR}/../../libs/windows_x86_64/lib -lavformat -lavcodec -lswscale -lswresample -lavutil -lwinpthread -lm -lws2_32 -lbcrypt -lsecur32
import "C"