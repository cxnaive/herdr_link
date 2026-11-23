package mathtex

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"strconv"
	"strings"

	"github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/fontscan"
)

// MyGo's SVG renderer deliberately omits <text>. MathJax supplies paths for
// math glyphs; turn its Unicode fallback text into system-font paths too.
type fontOutlines struct{ fonts *fontscan.FontMap }

func attr(e xml.StartElement, name string) string {
	for _, a := range e.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}
func (f *fontOutlines) textPath(text string, size float64) (string, error) {
	if f.fonts == nil {
		f.fonts = fontscan.NewFontMap(log.New(io.Discard, "", 0))
		if err := f.fonts.UseSystemFonts(""); err != nil {
			f.fonts = nil
			return "", err
		}
	}
	var path strings.Builder
	x := float64(0)
	for _, ch := range text {
		face := f.fonts.ResolveFace(ch)
		if face == nil {
			return "", errors.New("no font for formula text")
		}
		glyph, ok := face.NominalGlyph(ch)
		if !ok {
			return "", errors.New("missing formula glyph")
		}
		outline, ok := face.GlyphDataOutline(glyph)
		if !ok {
			return "", errors.New("missing formula glyph outline")
		}
		scale := size / float64(face.Upem())
		for i, seg := range outline.Segments {
			if seg.Op == opentype.SegmentOpMoveTo && i > 0 {
				path.WriteByte('Z')
			}
			path.WriteByte("MLQC"[seg.Op])
			for _, p := range seg.ArgsSlice() {
				fmt.Fprintf(&path, "%.3f %.3f ", x+float64(p.X)*scale, -float64(p.Y)*scale)
			}
		}
		if len(outline.Segments) > 0 {
			path.WriteByte('Z')
		}
		x += float64(face.HorizontalAdvance(glyph)) * scale
	}
	return path.String(), nil
}
func (f *fontOutlines) convert(svg string) (*Formula, error) {
	dec := xml.NewDecoder(strings.NewReader(svg))
	var out bytes.Buffer
	enc := xml.NewEncoder(&out)
	formula := &Formula{}
	root := false
	for {
		token, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if start, ok := token.(xml.StartElement); ok {
			// encoding/xml otherwise repeats namespaces on every element.
			start.Name.Space = ""
			switch start.Name.Local {
			case "svg":
				var x, y, w, h float64
				if root {
					break
				}
				root = true
				if n, _ := fmt.Sscanf(attr(start, "viewBox"), "%f %f %f %f", &x, &y, &w, &h); n != 4 || x != 0 || w <= 0 || h <= 0 || math.IsNaN(w+h+y) || math.IsInf(w+h+y, 0) || w > 100000 || h > 100000 {
					return nil, errors.New("invalid formula dimensions")
				}
				formula.Width, formula.Height, formula.Depth = float32(w/1000), float32(-y/1000), float32((h+y)/1000)
				attrs := []xml.Attr{{Name: xml.Name{Local: "viewBox"}, Value: attr(start, "viewBox")}, {Name: xml.Name{Local: "width"}, Value: fmt.Sprint(w)}, {Name: xml.Name{Local: "height"}, Value: fmt.Sprint(h)}}
				start.Attr = attrs
			case "text":
				var text string
				if err := dec.DecodeElement(&text, &start); err != nil {
					return nil, err
				}
				size, err := strconv.ParseFloat(strings.TrimSuffix(attr(start, "font-size"), "px"), 64)
				if err != nil || size <= 0 || size > 10000 {
					return nil, errors.New("invalid text size")
				}
				path, err := f.textPath(text, size)
				if err != nil {
					return nil, err
				}
				group := xml.StartElement{Name: xml.Name{Local: "g"}, Attr: []xml.Attr{{Name: xml.Name{Local: "transform"}, Value: attr(start, "transform")}}}
				if err = enc.EncodeToken(group); err != nil {
					return nil, err
				}
				p := xml.StartElement{Name: xml.Name{Local: "path"}, Attr: []xml.Attr{{Name: xml.Name{Local: "d"}, Value: path}}}
				for _, t := range []xml.Token{p, p.End(), group.End()} {
					if err = enc.EncodeToken(t); err != nil {
						return nil, err
					}
				}
				if out.Len() > maxSVG {
					return nil, errors.New("formula outline too large")
				}
				continue
			}
			token = start
		} else if end, ok := token.(xml.EndElement); ok {
			end.Name.Space = ""
			token = end
		}
		if err := enc.EncodeToken(token); err != nil {
			return nil, err
		}
	}
	if err := enc.Flush(); err != nil {
		return nil, err
	}
	if !root {
		return nil, errors.New("missing formula svg")
	}
	if out.Len() > maxSVG {
		return nil, errors.New("formula outline too large")
	}
	formula.SVG = out.Bytes()
	return formula, nil
}
