package whatsapp

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

func fakeUpload() whatsmeow.UploadResponse {
	return whatsmeow.UploadResponse{
		URL: "https://mmg.whatsapp.net/x", DirectPath: "/v/t62/x", MediaKey: []byte("mediakey"),
		FileEncSHA256: []byte("encsha"), FileSHA256: []byte("sha"), FileLength: 42,
	}
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func TestImageInfoAndThumb(t *testing.T) {
	w, h, thumb := imageInfo(pngBytes(t, 200, 100))
	assert.Equal(t, 200, w)
	assert.Equal(t, 100, h)
	require.NotEmpty(t, thumb)
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(thumb))
	require.NoError(t, err)
	assert.Equal(t, 72, cfg.Width) // lado mayor = 72
	assert.Equal(t, 36, cfg.Height)

	// Vertical: el alto es el lado mayor.
	_, _, tall := imageInfo(pngBytes(t, 50, 300))
	cfg, err = jpeg.DecodeConfig(bytes.NewReader(tall))
	require.NoError(t, err)
	assert.Equal(t, 72, cfg.Height)
	assert.Equal(t, 12, cfg.Width)

	// Una imagen chica no se agranda.
	_, _, small := imageInfo(pngBytes(t, 10, 10))
	cfg, err = jpeg.DecodeConfig(bytes.NewReader(small))
	require.NoError(t, err)
	assert.Equal(t, 10, cfg.Width)

	// Algo que no es imagen: sin datos, sin pánico.
	w, h, thumb = imageInfo([]byte("no soy una imagen"))
	assert.Zero(t, w)
	assert.Zero(t, h)
	assert.Nil(t, thumb)
}

func TestBuildMediaMessage(t *testing.T) {
	ci := &waE2E.ContextInfo{StanzaID: proto.String("Q")}
	img := pngBytes(t, 64, 32)

	m, err := buildMediaMessage(model.KindImage, preparedFile{data: img, mime: "image/png", up: fakeUpload()}, "pie", ci)
	require.NoError(t, err)
	im := m.GetImageMessage()
	require.NotNil(t, im)
	assert.Equal(t, "https://mmg.whatsapp.net/x", im.GetURL())
	assert.Equal(t, "/v/t62/x", im.GetDirectPath())
	assert.Equal(t, []byte("mediakey"), im.GetMediaKey())
	assert.Equal(t, []byte("encsha"), im.GetFileEncSHA256())
	assert.Equal(t, []byte("sha"), im.GetFileSHA256())
	assert.EqualValues(t, 42, im.GetFileLength())
	assert.Equal(t, "image/png", im.GetMimetype())
	assert.Equal(t, "pie", im.GetCaption())
	assert.EqualValues(t, 64, im.GetWidth())
	assert.EqualValues(t, 32, im.GetHeight())
	assert.NotEmpty(t, im.GetJPEGThumbnail())
	assert.Equal(t, "Q", im.GetContextInfo().GetStanzaID())

	// Sin pie, el campo queda sin poner.
	m, _ = buildMediaMessage(model.KindImage, preparedFile{data: img, mime: "image/png", up: fakeUpload()}, "", nil)
	assert.Nil(t, m.GetImageMessage().Caption)

	v, err := buildMediaMessage(model.KindVideo, preparedFile{data: []byte("v"), mime: "video/mp4", up: fakeUpload()}, "clip", nil)
	require.NoError(t, err)
	assert.Equal(t, "video/mp4", v.GetVideoMessage().GetMimetype())
	assert.Equal(t, "clip", v.GetVideoMessage().GetCaption())

	d, err := buildMediaMessage(model.KindDocument, preparedFile{data: []byte("d"), mime: "application/pdf", name: "informe.pdf", up: fakeUpload()}, "adjunto", nil)
	require.NoError(t, err)
	assert.Equal(t, "informe.pdf", d.GetDocumentMessage().GetFileName())
	assert.Equal(t, "informe.pdf", d.GetDocumentMessage().GetTitle())
	assert.Equal(t, "adjunto", d.GetDocumentMessage().GetCaption())

	a, err := buildMediaMessage(model.KindAudio, preparedFile{data: []byte("a"), mime: "audio/mpeg", up: fakeUpload()}, "", nil)
	require.NoError(t, err)
	assert.False(t, a.GetAudioMessage().GetPTT())
	assert.Equal(t, "audio/mpeg", a.GetAudioMessage().GetMimetype())

	s, err := buildMediaMessage(model.KindSticker, preparedFile{data: []byte("s"), mime: "image/webp", up: fakeUpload()}, "", nil)
	require.NoError(t, err)
	assert.Equal(t, "image/webp", s.GetStickerMessage().GetMimetype())

	_, err = buildMediaMessage(model.KindText, preparedFile{}, "", nil)
	assert.ErrorIs(t, err, port.ErrUnsupported)
}

func TestBuildVoiceMessage(t *testing.T) {
	data := oggBytes(3, 312)
	m, err := buildMediaMessage(model.KindVoice, preparedFile{data: data, mime: "audio/ogg", up: fakeUpload()}, "ignorado", nil)
	require.NoError(t, err)
	au := m.GetAudioMessage()
	assert.True(t, au.GetPTT())
	assert.Equal(t, "audio/ogg; codecs=opus", au.GetMimetype())
	assert.EqualValues(t, 3, au.GetSeconds())
}

// oggBytes arma un OGG/Opus mínimo con la duración dada en segundos.
func oggBytes(seconds int, preSkip uint16) []byte {
	var b bytes.Buffer
	b.WriteString("OggS\x00\x02")
	b.Write(make([]byte, 8+4+4+4+1)) // granule, serial, seq, crc, segmentos
	b.WriteString("\x00OpusHead\x01\x01")
	_ = binary.Write(&b, binary.LittleEndian, preSkip)
	b.Write(make([]byte, 16))
	// Última página con la posición de grano final.
	b.WriteString("OggS\x00\x04")
	_ = binary.Write(&b, binary.LittleEndian, int64(seconds)*48000+int64(preSkip))
	b.Write(make([]byte, 4+4+4+1))
	return b.Bytes()
}

func TestOggOpusSeconds(t *testing.T) {
	assert.Equal(t, 3, oggOpusSeconds(oggBytes(3, 312)))
	assert.Equal(t, 1, oggOpusSeconds(oggBytes(1, 0)))
	assert.Equal(t, 0, oggOpusSeconds(nil))
	assert.Equal(t, 0, oggOpusSeconds([]byte("OggS")))
	assert.Equal(t, 0, oggOpusSeconds([]byte("no es ogg")))
}

func TestSendVCardAndPhone(t *testing.T) {
	v := buildVCard("Ana Pérez", "+593 99 123-4567")
	assert.Equal(t, "BEGIN:VCARD\nVERSION:3.0\nN:;Ana Pérez;;;\nFN:Ana Pérez\nTEL;type=CELL;waid=593991234567:+593991234567\nEND:VCARD", v)
	// La vCard armada se vuelve a leer igual.
	assert.Equal(t, "+593991234567", phoneFromVCard(v))
	assert.NotContains(t, buildVCard("Sin tel", ""), "TEL")
	assert.Equal(t, "", phoneFromVCard(""))
}

func TestCheckSize(t *testing.T) {
	assert.NoError(t, checkSize(model.KindImage, 16<<20))
	assert.ErrorIs(t, checkSize(model.KindImage, 16<<20+1), port.ErrInvalid)
	assert.NoError(t, checkSize(model.KindVideo, 64<<20))
	assert.ErrorIs(t, checkSize(model.KindVideo, 64<<20+1), port.ErrInvalid)
	assert.NoError(t, checkSize(model.KindDocument, 100<<20))
	assert.ErrorIs(t, checkSize(model.KindDocument, 100<<20+1), port.ErrInvalid)
	assert.ErrorIs(t, checkSize(model.KindDocument, 0), port.ErrInvalid)
}

func TestDetectMime(t *testing.T) {
	assert.Equal(t, "image/x-custom", detectMime("image/x-custom", "a.png", nil))
	assert.Equal(t, "image/png", detectMime("", "foto.PNG", nil))
	assert.Equal(t, "application/pdf", detectMime("", "x.pdf", nil))
	assert.Equal(t, "image/png", detectMime("", "sin_extension", pngBytes(t, 4, 4)))
}

func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, data, 0o644))
		return p
	}

	// Sticker solo .webp.
	_, err := readFile(model.KindSticker, model.Outgoing{FilePath: write("s.png", pngBytes(t, 4, 4))})
	require.ErrorIs(t, err, port.ErrInvalid)
	assert.Contains(t, err.Error(), "El sticker debe ser .webp")
	pf, err := readFile(model.KindSticker, model.Outgoing{FilePath: write("s.webp", []byte("RIFFxxxxWEBP"))})
	require.NoError(t, err)
	assert.Equal(t, "image/webp", pf.mime)

	// Voz: siempre OGG/Opus.
	pf, err = readFile(model.KindVoice, model.Outgoing{FilePath: write("v.ogg", oggBytes(1, 0)), Mime: "audio/webm"})
	require.NoError(t, err)
	assert.Equal(t, "audio/ogg; codecs=opus", pf.mime)

	// Documento: nombre del archivo o el pedido.
	pf, err = readFile(model.KindDocument, model.Outgoing{FilePath: write("d.pdf", []byte("%PDF-1.4"))})
	require.NoError(t, err)
	assert.Equal(t, "d.pdf", pf.name)
	assert.Equal(t, "application/pdf", pf.mime)
	pf, err = readFile(model.KindDocument, model.Outgoing{FilePath: write("d2.bin", []byte("xyz")), FileName: "Contrato.docx", Mime: "application/vnd.x"})
	require.NoError(t, err)
	assert.Equal(t, "Contrato.docx", pf.name)
	assert.Equal(t, "application/vnd.x", pf.mime)

	// Errores.
	_, err = readFile(model.KindImage, model.Outgoing{})
	assert.ErrorIs(t, err, port.ErrInvalid)
	_, err = readFile(model.KindImage, model.Outgoing{FilePath: filepath.Join(dir, "no-existe.png")})
	assert.ErrorIs(t, err, port.ErrNotFound)
	_, err = readFile(model.KindImage, model.Outgoing{FilePath: write("vacio.png", nil)})
	assert.ErrorIs(t, err, port.ErrInvalid)
}

func TestForwardedCopy(t *testing.T) {
	// Texto simple: pasa a ExtendedTextMessage con la marca de reenviado.
	f := forwardedCopy(txt("hola"))
	require.NotNil(t, f)
	assert.Equal(t, "hola", f.GetExtendedTextMessage().GetText())
	assert.True(t, f.GetExtendedTextMessage().GetContextInfo().GetIsForwarded())
	assert.EqualValues(t, 1, f.GetExtendedTextMessage().GetContextInfo().GetForwardingScore())

	// Foto con una cita vieja: la cita se reemplaza, el archivo ya subido se conserva.
	orig := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		DirectPath: proto.String("/p"), MediaKey: []byte("k"),
		ContextInfo: &waE2E.ContextInfo{StanzaID: proto.String("vieja")},
	}}
	f = forwardedCopy(orig)
	require.NotNil(t, f)
	assert.Equal(t, "/p", f.GetImageMessage().GetDirectPath())
	assert.Equal(t, "", f.GetImageMessage().GetContextInfo().GetStanzaID())
	assert.True(t, f.GetImageMessage().GetContextInfo().GetIsForwarded())
	assert.Equal(t, "vieja", orig.GetImageMessage().GetContextInfo().GetStanzaID()) // el original no cambia

	// Ubicación y contacto también.
	assert.NotNil(t, forwardedCopy(&waE2E.Message{LocationMessage: &waE2E.LocationMessage{}}))
	assert.NotNil(t, forwardedCopy(&waE2E.Message{ContactMessage: &waE2E.ContactMessage{}}))

	// Encuestas, reacciones y protocolo no se reenvían.
	poll := &waE2E.Message{PollCreationMessage: &waE2E.PollCreationMessage{Name: proto.String("?")}}
	assert.Nil(t, forwardedCopy(poll))
	assert.Nil(t, forwardedCopy(&waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{}}))
	assert.Nil(t, forwardedCopy(nil))
}

func TestWithContextStripsAndKeepsType(t *testing.T) {
	orig := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text: proto.String("x"), ContextInfo: &waE2E.ContextInfo{StanzaID: proto.String("Q")},
	}}
	stripped := withContext(orig, nil)
	require.NotNil(t, stripped)
	assert.Nil(t, stripped.GetExtendedTextMessage().GetContextInfo())
	assert.Equal(t, "x", stripped.GetExtendedTextMessage().GetText())
	assert.NotNil(t, orig.GetExtendedTextMessage().GetContextInfo())

	// Un Conversation sin contexto sigue siendo Conversation.
	assert.Equal(t, "hola", withContext(txt("hola"), nil).GetConversation())
}

func TestBuildStatusText(t *testing.T) {
	m, err := buildStatusText(model.OutgoingStatus{Kind: model.KindText, Text: "hola", BgColor: "#112233", Font: 2})
	require.NoError(t, err)
	et := m.GetExtendedTextMessage()
	assert.Equal(t, "hola", et.GetText())
	assert.Equal(t, uint32(0xFF112233), et.GetBackgroundArgb())
	assert.Equal(t, waE2E.ExtendedTextMessage_FB_SCRIPT, et.GetFont())

	// Color y fuente inválidos: valores por defecto.
	m, err = buildStatusText(model.OutgoingStatus{Kind: model.KindText, Text: "hola", BgColor: "rojo", Font: 99})
	require.NoError(t, err)
	assert.Equal(t, uint32(0xFF128C7E), m.GetExtendedTextMessage().GetBackgroundArgb())
	assert.Equal(t, waE2E.ExtendedTextMessage_SYSTEM, m.GetExtendedTextMessage().GetFont())

	_, err = buildStatusText(model.OutgoingStatus{Kind: model.KindText, Text: "   "})
	assert.ErrorIs(t, err, port.ErrInvalid)
}

func TestQuotedTextOf(t *testing.T) {
	c := testConv()
	assert.Equal(t, "hola", c.quotedTextOf("c", "m", txt("hola")))
	long := strings.Repeat("a", 500)
	got := c.quotedTextOf("c", "m", txt(long))
	assert.Len(t, []rune(got), quotedTextMaxRunes)
	assert.True(t, strings.HasSuffix(got, "…"))
	assert.Equal(t, "📷 Foto", c.quotedTextOf("c", "m", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{}}))
	assert.Equal(t, "", c.quotedTextOf("c", "m", nil))
}

func TestNativeRefs(t *testing.T) {
	// Los ids de medio no dependen del orden de los argumentos.
	assert.NotEqual(t, mediaID("a", "b"), mediaID("b", "a"))
}
