//go:build linux && amd64 && !android

package ffmpeg

// #cgo CFLAGS: -I${SRCDIR}/../../libs/linux_x86_64/include
// #cgo LDFLAGS: -L${SRCDIR}/../../libs/linux_x86_64/lib -lavformat -lavcodec -lswscale -lswresample -lavutil -lm -lpthread -lz -lbz2 -ldl -lrt -ldrm
import "C"