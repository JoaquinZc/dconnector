package winnotify

import (
	_ "embed"
	"encoding/binary"
	"math"
	"sync"
)

// customWAV: el sonido de mensaje elegido por el usuario (message.wav: PCM, 44,1 kHz, mono, 16 bits, ~1 s).
// Si faltara o no fuera válido, suena el «pop» generado de abajo.
//
//go:embed message.wav
var customWAV []byte

// El sonido de mensaje de dConnector se genera aquí, en memoria: dos notas descendentes cortas tipo «pop».
// No depende del esquema de sonidos de Windows (que el usuario puede tener en «sin sonido») ni de que el
// aviso sea visible («No molestar» silencia el sonido de los avisos, no este).
const (
	soundRate     = 44100 // Hz
	soundDuration = 0.32  // segundos
	soundGain     = 0.60  // volumen moderado: pico de ~47 % del máximo, sin saturar
)

// pop: una nota con ataque de 4 ms y caída exponencial.
type pop struct {
	freq  float64 // Hz
	start float64 // s
	decay float64 // constante de caída (1/s)
	amp   float64
}

var popNotes = []pop{
	{freq: 1318.5, start: 0.000, decay: 22, amp: 1.0}, // Mi6
	{freq: 987.8, start: 0.110, decay: 17, amp: 0.85}, // Si5
}

// synthWAV genera el «pop» de respaldo como un archivo WAV completo (PCM, 44,1 kHz, mono, 16 bits).
func synthWAV() []byte {
	n := int(soundRate * soundDuration)
	pcm := make([]int16, n)
	for i := range pcm {
		t := float64(i) / soundRate
		var v float64
		for _, p := range popNotes {
			dt := t - p.start
			if dt < 0 {
				continue
			}
			env := math.Exp(-p.decay * dt)
			if dt < 0.004 {
				env *= dt / 0.004 // ataque
			}
			ph := 2 * math.Pi * p.freq * dt
			// fundamental + un poco de segundo armónico para un timbre más redondo
			v += p.amp * env * (math.Sin(ph) + 0.18*math.Sin(2*ph))
		}
		// fundido de salida de los últimos 20 ms (sin chasquido al cortar)
		if tail := soundDuration - t; tail < 0.020 {
			v *= tail / 0.020
		}
		pcm[i] = int16(math.Round(v * soundGain * math.MaxInt16 / 1.5))
	}
	return wavBytes(pcm)
}

// wavBytes envuelve muestras PCM mono de 16 bits en una cabecera RIFF/WAVE.
func wavBytes(pcm []int16) []byte {
	dataLen := len(pcm) * 2
	b := make([]byte, 44+dataLen)
	copy(b[0:], "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+dataLen))
	copy(b[8:], "WAVE")
	copy(b[12:], "fmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)          // tamaño del bloque fmt
	binary.LittleEndian.PutUint16(b[20:], 1)           // PCM
	binary.LittleEndian.PutUint16(b[22:], 1)           // mono
	binary.LittleEndian.PutUint32(b[24:], soundRate)   // frecuencia
	binary.LittleEndian.PutUint32(b[28:], soundRate*2) // bytes por segundo
	binary.LittleEndian.PutUint16(b[32:], 2)           // alineación de bloque
	binary.LittleEndian.PutUint16(b[34:], 16)          // bits por muestra
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(dataLen))
	for i, s := range pcm {
		binary.LittleEndian.PutUint16(b[44+i*2:], uint16(s))
	}
	return b
}

var (
	wavOnce sync.Once
	wavData []byte
)

// messageSound devuelve el WAV compartido: se genera una sola vez y se conserva vivo toda la ejecución
// (PlaySound con SND_MEMORY|SND_ASYNC lee el búfer mientras suena). Es de solo lectura: seguro entre goroutines.
func messageSound() []byte {
	wavOnce.Do(func() { wavData = MessageWAV() })
	return wavData
}

// validWAV: cabecera RIFF/WAVE de PCM de 16 bits y menos de 3 s (PlaySound lo reproduce desde memoria).
func validWAV(b []byte) bool {
	if len(b) < 44 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" || string(b[12:16]) != "fmt " {
		return false
	}
	if binary.LittleEndian.Uint16(b[20:22]) != 1 || binary.LittleEndian.Uint16(b[34:36]) != 16 {
		return false
	}
	byteRate := int(binary.LittleEndian.Uint32(b[28:32]))
	return string(b[36:40]) == "data" && byteRate > 0 && (len(b)-44)/byteRate < 3
}

// MessageWAV devuelve el sonido de mensaje: el elegido por el usuario o, si no es válido, el «pop» generado.
func MessageWAV() []byte {
	if validWAV(customWAV) {
		return customWAV
	}
	return synthWAV()
}
