package tracker

import (
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteTrackingPixel(t *testing.T) {
	rec := httptest.NewRecorder()
	writeTrackingPixel(rec)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	img, err := png.Decode(rec.Body)
	if err != nil {
		t.Fatalf("decode tracking pixel: %v", err)
	}
	if bounds := img.Bounds(); bounds.Dx() != 1 || bounds.Dy() != 1 {
		t.Fatalf("dimensions = %dx%d, want 1x1", bounds.Dx(), bounds.Dy())
	}
	if _, _, _, alpha := img.At(img.Bounds().Min.X, img.Bounds().Min.Y).RGBA(); alpha != 0 {
		t.Errorf("pixel alpha = %d, want transparent", alpha)
	}
}
