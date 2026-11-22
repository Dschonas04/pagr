package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // WebP lesen; geschrieben wird JPEG oder PNG
)

var bildBreiten = []int{480, 960, 1600}

const bildPunkteMax = 40_000_000

// höchstens zwei Bilder gleichzeitig rechnen
var bildPlaetze = make(chan struct{}, 2)

var errOriginal = errors.New("Original ausliefern")

// verkleinert liefert die Datei in der gewünschten Breite und legt sie daneben
// ab. Geht das nicht, bleibt es beim Original.
func verkleinert(original, typ, breite string) (pfad, neuerTyp string, err error) {
	b, err := strconv.Atoi(breite)
	if err != nil || !slices.Contains(bildBreiten, b) {
		return "", "", errOriginal
	}
	switch typ {
	case "image/jpeg":
		neuerTyp = "image/jpeg"
	case "image/png", "image/webp":
		neuerTyp = "image/png"
	default:
		return "", "", errOriginal // GIF verlöre die Bewegung, AVIF liest Go nicht
	}
	pfad = original + "-" + strconv.Itoa(b)
	if _, err := os.Stat(pfad); err == nil {
		return pfad, neuerTyp, nil
	}
	bildPlaetze <- struct{}{}
	defer func() { <-bildPlaetze }()

	f, err := os.Open(original)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	c, _, err := image.DecodeConfig(f)
	if err != nil || c.Width <= b || c.Height <= 0 || int64(c.Width)*int64(c.Height) > bildPunkteMax {
		return "", "", errOriginal
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", "", err
	}
	// Gedrehte Handyfotos verlören beim Neuschreiben ihre Drehung.
	if typ == "image/jpeg" && gedreht(f) {
		return "", "", errOriginal
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", "", err
	}
	quelle, _, err := image.Decode(f)
	if err != nil {
		return "", "", errOriginal
	}
	ziel := image.NewRGBA(image.Rect(0, 0, b, max(1, int(int64(c.Height)*int64(b)/int64(c.Width)))))
	draw.ApproxBiLinear.Scale(ziel, ziel.Bounds(), quelle, quelle.Bounds(), draw.Src, nil)

	t, err := os.CreateTemp(filepath.Dir(original), ".bild-*")
	if err != nil {
		return "", "", err
	}
	defer os.Remove(t.Name())
	if neuerTyp == "image/jpeg" {
		err = jpeg.Encode(t, ziel, &jpeg.Options{Quality: 84})
	} else {
		err = png.Encode(t, ziel)
	}
	if e := t.Close(); err == nil {
		err = e
	}
	if err != nil {
		return "", "", err
	}
	if err := os.Rename(t.Name(), pfad); err != nil {
		return "", "", err
	}
	return pfad, neuerTyp, nil
}

// gedreht liest die Ausrichtung aus dem EXIF-Teil eines JPEG.
func gedreht(r io.Reader) bool {
	b := bufio.NewReader(r)
	var kopf [4]byte
	if _, err := io.ReadFull(b, kopf[:2]); err != nil || kopf[0] != 0xff || kopf[1] != 0xd8 {
		return false
	}
	for range 32 {
		if _, err := io.ReadFull(b, kopf[:]); err != nil || kopf[0] != 0xff || kopf[1] == 0xda {
			return false
		}
		n := int(binary.BigEndian.Uint16(kopf[2:])) - 2
		if n < 0 {
			return false
		}
		d := make([]byte, n)
		if _, err := io.ReadFull(b, d); err != nil {
			return false
		}
		if kopf[1] != 0xe1 || n < 14 || string(d[:6]) != "Exif\x00\x00" {
			continue
		}
		t := d[6:]
		var o binary.ByteOrder = binary.BigEndian
		if string(t[:2]) == "II" {
			o = binary.LittleEndian
		}
		ab := int64(o.Uint32(t[4:]))
		if ab+2 > int64(len(t)) {
			return false
		}
		p := int(ab)
		anzahl := int(o.Uint16(t[p:]))
		for i := 0; i < anzahl; i++ {
			e := p + 2 + 12*i
			if e+12 > len(t) {
				return false
			}
			if o.Uint16(t[e:]) == 0x0112 {
				return o.Uint16(t[e+8:]) > 1
			}
		}
		return false
	}
	return false
}
