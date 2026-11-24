package party

import (
	"bytes"
	"image/color"
	"image/png"
	"net/http"

	"github.com/skip2/go-qrcode"
)

var (
	qrDark  = color.RGBA{R: 28, G: 18, B: 12, A: 255}
	qrLight = color.RGBA{R: 255, G: 244, B: 220, A: 255}
)

func qrPNG(content string) ([]byte, error) {
	code, err := qrcode.New(content, qrcode.Medium)
	if err != nil {
		return nil, err
	}
	code.DisableBorder = false
	code.ForegroundColor = qrDark
	code.BackgroundColor = qrLight
	img := code.Image(360)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func handleShareQR(w http.ResponseWriter, r *http.Request) {
	p := hostParty(w, r)
	if p == nil {
		return
	}
	pngBytes, err := qrPNG(joinURL(r, p.Code))
	if err != nil {
		http.Error(w, "could not draw the QR code", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(pngBytes)
}
