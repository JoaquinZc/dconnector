package winnotify

import "testing"

func TestSynthWAV(t *testing.T) {
	b := synthWAV()
	if string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" || string(b[12:16]) != "fmt " || string(b[36:40]) != "data" {
		t.Fatalf("cabecera RIFF/WAVE inválida: %q", b[:44])
	}
	le16 := func(o int) int { return int(b[o]) | int(b[o+1])<<8 }
	le32 := func(o int) int { return le16(o) | le16(o+2)<<16 }
	if le32(4) != len(b)-8 || le32(40) != len(b)-44 || le32(16) != 16 {
		t.Fatalf("tamaños: riff=%d data=%d fmt=%d len=%d", le32(4), le32(40), le32(16), len(b))
	}
	rate, ch, bits := le32(24), le16(22), le16(34)
	if le16(20) != 1 || ch != 1 || rate != 44100 || bits != 16 || le32(28) != rate*ch*bits/8 || le16(32) != ch*bits/8 {
		t.Fatalf("formato: pcm=%d canales=%d hz=%d bits=%d", le16(20), ch, rate, bits)
	}
	samples := (len(b) - 44) / 2
	if ms := samples * 1000 / rate; ms < 250 || ms > 350 {
		t.Fatalf("duración %d ms (debe estar entre 250 y 350)", ms)
	}
	peak := 0
	for i := 0; i < samples; i++ {
		v := int(int16(le16(44 + i*2)))
		if v < 0 {
			v = -v
		}
		if v > peak {
			peak = v
		}
	}
	if peak < 6000 {
		t.Fatalf("el sonido es casi inaudible: pico %d", peak)
	}
	if peak >= 32767*3/4 {
		t.Fatalf("volumen demasiado alto o saturado: pico %d", peak)
	}
	first, last := int(int16(le16(44))), int(int16(le16(44+(samples-1)*2)))
	if first < -200 || first > 200 || last < -200 || last > 200 {
		t.Fatalf("empieza o termina con chasquido: %d … %d", first, last)
	}
}

func TestMessageSoundIsSharedAndStable(t *testing.T) {
	a, b := messageSound(), messageSound()
	if len(a) == 0 || &a[0] != &b[0] {
		t.Fatal("el WAV se genera una sola vez y se comparte")
	}
	if string(a) != string(MessageWAV()) {
		t.Fatal("el sonido debe ser determinista")
	}
}

func TestCustomWAVIsValid(t *testing.T) {
	if !validWAV(customWAV) {
		t.Fatal("message.wav embebido no es un WAV PCM de 16 bits válido")
	}
	if string(MessageWAV()) != string(customWAV) {
		t.Fatal("MessageWAV debe devolver el sonido elegido")
	}
	peak := 0
	for i := 44; i+1 < len(customWAV); i += 2 {
		v := int(int16(uint16(customWAV[i]) | uint16(customWAV[i+1])<<8))
		if v < 0 {
			v = -v
		}
		peak = max(peak, v)
	}
	if peak < 4000 || peak >= 32767*3/4 {
		t.Fatalf("volumen fuera de rango: pico %d", peak)
	}
	if validWAV([]byte("no es un wav")) {
		t.Fatal("una cabecera inválida no debe aceptarse")
	}
}
