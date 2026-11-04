package tv

import (
	"bytes"
	"strings"
	"testing"
)

func TestQRSVG(t *testing.T) {
	const payload = "WIFI:T:WPA;S:TravelNet;P:hunter2;H:false;;"
	first, err := qrSVG(payload)
	if err != nil {
		t.Fatal(err)
	}
	second, err := qrSVG(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("QR output is not deterministic")
	}
	svg := string(first)
	for _, want := range []string{`<svg`, `viewBox="0 0 `, `<path fill="#000"`, `</svg>`} {
		if !strings.Contains(svg, want) {
			t.Fatalf("QR SVG missing %q: %s", want, svg)
		}
	}
	if strings.Contains(svg, "hunter2") || strings.Contains(svg, "WIFI:") {
		t.Fatal("QR SVG leaked its payload")
	}
}
