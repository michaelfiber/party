package party

import (
	"bytes"
	"image/color"
	"image/png"
	"net/http"
	"strconv"

	"github.com/skip2/go-qrcode"
)

var (
	qrDark  = color.RGBA{R: 43, G: 16, B: 85, A: 255}
	qrLight = color.RGBA{R: 255, G: 255, B: 255, A: 255}
)

func qrPNG(content string) ([]byte, error) {
	return qrPNGSize(content, 360)
}

func qrPNGSize(content string, size int) ([]byte, error) {
	code, err := qrcode.New(content, qrcode.Medium)
	if err != nil {
		return nil, err
	}
	code.DisableBorder = false
	code.ForegroundColor = qrDark
	code.BackgroundColor = qrLight
	img := code.Image(size)
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
	size := 360
	if n, err := strconv.Atoi(r.URL.Query().Get("size")); err == nil {
		size = min(max(n, 128), 1200)
	}
	writeQRSize(w, joinURL(r, p.Code), size)
}

func writeQR(w http.ResponseWriter, content string) {
	writeQRSize(w, content, 360)
}

func writeQRSize(w http.ResponseWriter, content string, size int) {
	pngBytes, err := qrPNGSize(content, size)
	if err != nil {
		http.Error(w, "could not draw the QR code", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(pngBytes)
}
