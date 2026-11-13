package telegram

// Sondeo mínimo de MP4: duración y dimensiones para los atributos de vídeo de una historia (no hay
// ffmpeg). Lee las cajas «moov» → «mvhd» (duración) y «trak» → «tkhd» (ancho y alto).

import (
	"encoding/binary"
	"io"
	"os"
)

// mp4Info: duración en segundos y dimensiones; ok es false si no se pudo leer nada útil.
type mp4Info struct {
	Duration float64
	W, H     int
	OK       bool
}

// probeMP4File lee el principio y el final del archivo (la caja moov puede ir en cualquiera).
func probeMP4File(path string) mp4Info {
	f, err := os.Open(path)
	if err != nil {
		return mp4Info{}
	}
	defer f.Close()
	// Se recorren las cajas de primer nivel saltando las grandes (mdat) sin leerlas.
	var off int64
	hdr := make([]byte, 16)
	for i := 0; i < 64; i++ {
		if _, err := f.ReadAt(hdr[:8], off); err != nil {
			return mp4Info{}
		}
		size := int64(binary.BigEndian.Uint32(hdr[:4]))
		typ := string(hdr[4:8])
		headLen := int64(8)
		if size == 1 {
			if _, err := f.ReadAt(hdr[8:16], off+8); err != nil {
				return mp4Info{}
			}
			size = int64(binary.BigEndian.Uint64(hdr[8:16]))
			headLen = 16
		}
		if size == 0 {
			st, err := f.Stat()
			if err != nil {
				return mp4Info{}
			}
			size = st.Size() - off
		}
		if size < headLen {
			return mp4Info{}
		}
		if typ == "moov" {
			if size > 32<<20 {
				return mp4Info{}
			}
			buf := make([]byte, size-headLen)
			if _, err := f.ReadAt(buf, off+headLen); err != nil && err != io.EOF {
				return mp4Info{}
			}
			return probeMoov(buf)
		}
		off += size
	}
	return mp4Info{}
}

// probeMoov recorre el contenido de una caja moov.
func probeMoov(b []byte) mp4Info {
	var info mp4Info
	walkBoxes(b, func(typ string, body []byte) {
		switch typ {
		case "mvhd":
			if len(body) >= 20 {
				ver := body[0]
				if ver == 1 && len(body) >= 32 {
					ts := binary.BigEndian.Uint32(body[20:24])
					dur := binary.BigEndian.Uint64(body[24:32])
					if ts > 0 {
						info.Duration = float64(dur) / float64(ts)
					}
				} else if ver == 0 {
					ts := binary.BigEndian.Uint32(body[12:16])
					dur := binary.BigEndian.Uint32(body[16:20])
					if ts > 0 {
						info.Duration = float64(dur) / float64(ts)
					}
				}
			}
		case "trak":
			if info.W != 0 {
				return
			}
			walkBoxes(body, func(t string, tb []byte) {
				if t != "tkhd" || len(tb) < 4 {
					return
				}
				// El ancho y el alto son los últimos 8 bytes (punto fijo 16.16).
				if len(tb) >= 8 {
					w := int(binary.BigEndian.Uint32(tb[len(tb)-8:len(tb)-4]) >> 16)
					h := int(binary.BigEndian.Uint32(tb[len(tb)-4:]) >> 16)
					if w > 0 && h > 0 {
						info.W, info.H = w, h
					}
				}
			})
		}
	})
	info.OK = info.Duration > 0 || info.W > 0
	return info
}

// walkBoxes llama a f con cada caja (tipo y contenido) de un bloque.
func walkBoxes(b []byte, f func(typ string, body []byte)) {
	for len(b) >= 8 {
		size := int(binary.BigEndian.Uint32(b[:4]))
		typ := string(b[4:8])
		head := 8
		if size == 1 && len(b) >= 16 {
			size = int(binary.BigEndian.Uint64(b[8:16]))
			head = 16
		}
		if size == 0 {
			size = len(b)
		}
		if size < head || size > len(b) {
			return
		}
		f(typ, b[head:size])
		b = b[size:]
	}
}
