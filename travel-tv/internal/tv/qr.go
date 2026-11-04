package tv

import (
	"fmt"
	"strings"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/qr"
)

// qrModuleBorder is the quiet zone, in modules, around the QR symbol.
const qrModuleBorder = 4

// qrSVG encodes content as an SVG QR code. It is generated at startup from the
// Ansible-rendered config, so the Wi-Fi PSK stays in the config file and the
// process's memory rather than being rendered as text or passed to a helper.
func qrSVG(content string) ([]byte, error) {
	code, err := qr.Encode(content, qr.M, qr.Auto)
	if err != nil {
		return nil, fmt.Errorf("encode QR code: %w", err)
	}
	return qrSVGFromBarcode(code), nil
}

func qrSVGFromBarcode(code barcode.Barcode) []byte {
	bounds := code.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	size := max(width, height) + qrModuleBorder*2

	var path strings.Builder
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, _, _, _ := code.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			if r >= 0x8000 {
				continue
			}
			fmt.Fprintf(&path, "M%d %dh1v1h-1z", x+qrModuleBorder, y+qrModuleBorder)
		}
	}

	var svg strings.Builder
	fmt.Fprintf(&svg, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges" role="img">`, size, size)
	fmt.Fprintf(&svg, `<rect width="%d" height="%d" fill="#fff"/>`, size, size)
	fmt.Fprintf(&svg, `<path fill="%s" d="%s"/></svg>`, "#000", path.String())
	return []byte(svg.String())
}
