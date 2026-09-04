package av

import (
	"image"
	"image/color"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// AspectRatio describes how the VideoPlayer sizes itself within the
// available canvas area. The video is never stretched: it is centered
// and letterboxed (black bars) to preserve the declared ratio.
type AspectRatio float64

const (
	// Aspect16_9 is the standard widescreen ratio (1.777…).
	Aspect16_9 AspectRatio = 16.0 / 9.0
	// Aspect4_3 is the classic TV ratio (1.333…).
	Aspect4_3 AspectRatio = 4.0 / 3.0
	// Aspect21_9 is the cinematic ratio (2.370…).
	Aspect21_9 AspectRatio = 21.0 / 9.0
	// Aspect1_1 is a square.
	Aspect1_1 AspectRatio = 1.0
	// AspectAuto derives the ratio from the decoded video frame size.
	// The widget recomputes it whenever a new frame arrives.
	AspectAuto AspectRatio = 0
)

// VideoPlayer is a Fyne widget that renders video frames from a Controller.
type VideoPlayer struct {
	widget.BaseWidget
	ctrl    *Controller
	raster  *canvas.Raster
	bgColor color.Color

	mu       sync.Mutex
	curImg   *image.RGBA
	curW     int
	curH     int
	ratio    AspectRatio
	autoRatio bool
}

// NewVideoPlayer creates a VideoPlayer bound to the given Controller.
// The default aspect ratio is AspectAuto — derived from the video frame.
func NewVideoPlayer(ctrl *Controller) *VideoPlayer {
	vp := &VideoPlayer{
		ctrl:      ctrl,
		bgColor:   color.NRGBA{R: 0, G: 0, B: 0, A: 255},
		ratio:     AspectAuto,
		autoRatio: true,
	}
	vp.ExtendBaseWidget(vp)

	vp.raster = canvas.NewRaster(vp.generate)
	vp.raster.ScaleMode = canvas.ImageScaleSmooth

	if ctrl != nil && ctrl.HasVideo() {
		go vp.frameListener()
	}

	return vp
}

// SetController binds a new Controller to this player.
func (vp *VideoPlayer) SetController(ctrl *Controller) {
	vp.mu.Lock()
	vp.ctrl = ctrl
	vp.curImg = nil
	vp.mu.Unlock()
	if ctrl != nil && ctrl.HasVideo() {
		go vp.frameListener()
	}
	if vp.raster != nil {
		vp.raster.Refresh()
	}
}

// SetAspectRatio sets the aspect ratio the video area enforces. Use
// AspectAuto to derive it from the decoded frame dimensions.
func (vp *VideoPlayer) SetAspectRatio(r AspectRatio) {
	vp.mu.Lock()
	if r == AspectAuto {
		vp.autoRatio = true
		vp.ratio = 0
	} else {
		vp.autoRatio = false
		vp.ratio = r
	}
	vp.mu.Unlock()
	vp.Refresh()
}

// AspectRatio returns the currently effective aspect ratio. When set to
// AspectAuto and a frame has been received, returns the frame's ratio.
func (vp *VideoPlayer) AspectRatio() AspectRatio {
	vp.mu.Lock()
	defer vp.mu.Unlock()
	if vp.autoRatio && vp.curW > 0 && vp.curH > 0 {
		return AspectRatio(float64(vp.curW) / float64(vp.curH))
	}
	return vp.ratio
}

// frameListener listens for frame signals and refreshes the raster.
func (vp *VideoPlayer) frameListener() {
	sig := vp.ctrl.FrameSignal()
	for range sig {
		vf, ok := vp.ctrl.VideoFrame()
		if !ok {
			continue
		}
		vp.mu.Lock()
		vp.curImg = imageFromRGBA(vf.RGBA, vf.Width, vf.Height)
		vp.curW = vf.Width
		vp.curH = vf.Height
		ratioChanged := vp.autoRatio
		vp.mu.Unlock()

		// When auto-deriving the ratio, a layout refresh on the first
		// frame (or on a ratio change) makes the widget recompute its
		// letterboxed area.
		if ratioChanged {
			fyne.Do(func() {
				vp.BaseWidget.Refresh()
			})
		}

		if vp.raster != nil {
			fyne.Do(func() {
				vp.raster.Refresh()
			})
		}
	}
}

// generate is the Raster generator function.
func (vp *VideoPlayer) generate(w, h int) image.Image {
	vp.mu.Lock()
	img := vp.curImg
	bg := vp.bgColor
	vp.mu.Unlock()

	if img == nil || vp.ctrl == nil || !vp.ctrl.HasVideo() {
		if w <= 0 || h <= 0 {
			w = 1
			h = 1
		}
		bgImg := image.NewRGBA(image.Rect(0, 0, w, h))
		drawFill(bgImg, bg)
		return bgImg
	}

	return img
}

// CreateRenderer returns the widget renderer. It uses a custom layout
// that letterboxes the video into the available area while preserving
// the declared aspect ratio.
func (vp *VideoPlayer) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.NRGBA{R: 0, G: 0, B: 0, A: 255})
	return &videoRenderer{
		widget: vp,
		bg:     bg,
		raster: vp.raster,
	}
}

// imageFromRGBA wraps a raw RGBA byte slice as an image.RGBA.
func imageFromRGBA(rgba []byte, w, h int) *image.RGBA {
	if w <= 0 || h <= 0 {
		return nil
	}
	return &image.RGBA{
		Pix:    rgba,
		Stride: w * 4,
		Rect:   image.Rect(0, 0, w, h),
	}
}

// drawFill fills an RGBA image with a solid color.
func drawFill(img *image.RGBA, c color.Color) {
	r, g, b, a := c.RGBA()
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)})
		}
	}
}

// videoRenderer lays out a black background filling the whole widget area
// and the video raster centered and letterboxed to the aspect ratio.
type videoRenderer struct {
	widget *VideoPlayer
	bg     *canvas.Rectangle
	raster *canvas.Raster
}

func (r *videoRenderer) Destroy() {}

func (r *videoRenderer) Objects() []fyne.CanvasObject {
	if r.raster == nil {
		return []fyne.CanvasObject{r.bg}
	}
	return []fyne.CanvasObject{r.bg, r.raster}
}

func (r *videoRenderer) Refresh() {
	if r.raster != nil {
		r.raster.Refresh()
	}
	r.bg.Refresh()
}

func (r *videoRenderer) MinSize() fyne.Size {
	return fyne.NewSize(1, 1)
}

func (r *videoRenderer) Layout(size fyne.Size) {
	// Black background fills everything (letterbox bars).
	r.bg.Resize(size)
	r.bg.Move(fyne.NewPos(0, 0))

	if r.raster == nil {
		r.raster = r.widget.raster
	}

	ratio := float64(r.widget.AspectRatio())
	if ratio <= 0 {
		// No ratio known yet — fill everything (will recompute on first frame).
		r.raster.Resize(size)
		r.raster.Move(fyne.NewPos(0, 0))
		return
	}

	availW := float64(size.Width)
	availH := float64(size.Height)
	videoW := availW
	videoH := videoW / ratio
	if videoH > availH {
		videoH = availH
		videoW = videoH * ratio
	}

	x := (availW - videoW) / 2
	y := (availH - videoH) / 2
	r.raster.Resize(fyne.NewSize(float32(videoW), float32(videoH)))
	r.raster.Move(fyne.NewPos(float32(x), float32(y)))
}