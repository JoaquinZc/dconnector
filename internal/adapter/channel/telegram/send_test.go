package telegram

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

func TestCheckOutgoing(t *testing.T) {
	long := strings.Repeat("a", 4097)
	longCap := strings.Repeat("a", 1025)
	poll := func(opts ...string) *model.Poll {
		p := &model.Poll{Question: "¿?"}
		for _, o := range opts {
			p.Options = append(p.Options, model.PollOption{Name: o})
		}
		return p
	}
	valid := []model.Outgoing{
		{Kind: model.KindText, Text: "hola"},
		{Text: "sin tipo"},
		{Kind: model.KindText, Text: strings.Repeat("a", 4096)},
		{Kind: model.KindImage, FilePath: "a.jpg", Text: "pie"},
		{Kind: model.KindVideo, FilePath: "a.mp4"},
		{Kind: model.KindAudio, FilePath: "a.mp3"},
		{Kind: model.KindVoice, FilePath: "a.ogg"},
		{Kind: model.KindDocument, FilePath: "a.pdf"},
		{Kind: model.KindSticker, FilePath: "a.webp"},
		{Kind: model.KindSticker, FilePath: "a.bin", Mime: "image/webp"},
		{Kind: model.KindLocation, Location: &model.Location{Lat: -0.18, Lng: -78.47}},
		{Kind: model.KindContact, Contact: &model.ContactCard{Name: "Luis", Phone: "+593991234567"}},
		{Kind: model.KindContact, Contact: &model.ContactCard{VCard: "BEGIN:VCARD"}},
		{Kind: model.KindPoll, Poll: poll("a", "b")},
		{Kind: model.KindPoll, Poll: poll("1", "2", "3", "4", "5", "6", "7", "8", "9", "10")},
	}
	for i, o := range valid {
		assert.NoError(t, checkOutgoing(o), "válido %d (%s)", i, o.Kind)
	}
	invalid := []model.Outgoing{
		{Kind: model.KindText, Text: "   "},
		{Kind: model.KindText, Text: long},
		{Kind: model.KindImage},
		{Kind: model.KindImage, FilePath: "a.jpg", Text: longCap},
		{Kind: model.KindSticker, FilePath: "a.png"},
		{Kind: model.KindLocation},
		{Kind: model.KindLocation, Location: &model.Location{Lat: 91}},
		{Kind: model.KindLocation, Location: &model.Location{Lng: -181}},
		{Kind: model.KindContact},
		{Kind: model.KindContact, Contact: &model.ContactCard{Name: "sin teléfono"}},
		{Kind: model.KindPoll},
		{Kind: model.KindPoll, Poll: &model.Poll{Options: []model.PollOption{{Name: "a"}, {Name: "b"}}}},
		{Kind: model.KindPoll, Poll: poll("solo una")},
		{Kind: model.KindPoll, Poll: poll("1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11")},
		{Kind: model.KindPoll, Poll: poll("a", " ")},
		{Kind: model.KindPoll, Poll: &model.Poll{Question: strings.Repeat("q", 301), Options: []model.PollOption{{Name: "a"}, {Name: "b"}}}},
	}
	for i, o := range invalid {
		assert.ErrorIs(t, checkOutgoing(o), port.ErrInvalid, "inválido %d (%s)", i, o.Kind)
	}
	assert.ErrorIs(t, checkOutgoing(model.Outgoing{Kind: model.KindSystem}), port.ErrUnsupported)
	assert.ErrorIs(t, checkOutgoing(model.Outgoing{Kind: model.KindUnsupported}), port.ErrUnsupported)
}

func TestSplitName(t *testing.T) {
	f, l := splitName("Luis Mora Pérez")
	assert.Equal(t, "Luis", f)
	assert.Equal(t, "Mora Pérez", l)
	f, l = splitName("  Ana ")
	assert.Equal(t, "Ana", f)
	assert.Equal(t, "", l)
	f, l = splitName("")
	assert.Equal(t, "", f+l)
}

func TestDetectMimeAndDefaults(t *testing.T) {
	dir := t.TempDir()
	assert.Equal(t, "image/x-custom", detectMime("image/x-custom", "a.jpg"), "el pedido manda")
	assert.Equal(t, "image/jpeg", detectMime("", "foto.jpg"))
	assert.Equal(t, "application/pdf", detectMime("", "doc.PDF"))

	// Sin extensión conocida se mira el contenido.
	png := filepath.Join(dir, "sin_extension")
	require.NoError(t, os.WriteFile(png, []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), 0o600))
	assert.Equal(t, "image/png", detectMime("", png))
	assert.Equal(t, "", detectMime("", filepath.Join(dir, "no_existe")))

	assert.Equal(t, "image/jpeg", defaultMime(model.KindImage))
	assert.Equal(t, "video/mp4", defaultMime(model.KindVideo))
	assert.Equal(t, "audio/mpeg", defaultMime(model.KindAudio))
	assert.Equal(t, "audio/ogg", defaultMime(model.KindVoice))
	assert.Equal(t, "image/webp", defaultMime(model.KindSticker))
	assert.Equal(t, "application/octet-stream", defaultMime(model.KindDocument))
}

// oggOpus arma un OGG mínimo con cabecera OpusHead y una última página con la posición de grano dada.
func oggOpus(granule uint64) []byte {
	page := func(g uint64, payload string) []byte {
		h := make([]byte, 27)
		copy(h, "OggS")
		binary.LittleEndian.PutUint64(h[6:14], g)
		return append(h, payload...)
	}
	data := page(0, "OpusHead")
	return append(data, page(granule, "datos")...)
}

func TestOggOpusDuration(t *testing.T) {
	assert.Equal(t, 3, oggOpusDuration(oggOpus(48000*3)))
	assert.Equal(t, 4, oggOpusDuration(oggOpus(48000*3+100)), "se redondea hacia arriba")
	assert.Equal(t, 0, oggOpusDuration(oggOpus(0)))
	assert.Equal(t, 0, oggOpusDuration(oggOpus(^uint64(0))))
	assert.Equal(t, 0, oggOpusDuration([]byte("no es ogg")))
	assert.Equal(t, 0, oggOpusDuration(nil))
	// OGG que no es Opus.
	vorbis := oggOpus(48000)
	copy(vorbis[27:], "vorbisXX")
	assert.Equal(t, 0, oggOpusDuration(vorbis))
}

func TestSentMessageOf(t *testing.T) {
	// UpdateShortSentMessage: se arma el mensaje con lo que trae.
	short := &tg.UpdateShortSentMessage{ID: 77, Date: 1700000000, Out: true}
	m, _, ok := sentMessageOf(short, "user:100", "hola")
	require.True(t, ok)
	assert.Equal(t, 77, m.ID)
	assert.Equal(t, "hola", m.Message)
	assert.True(t, m.Out)
	assert.Equal(t, "user:100", nativeOf(m.PeerID))
	got := newConv().message(m)
	assert.Equal(t, "user:1", got.SenderNativeID)
	assert.True(t, got.FromMe)

	_, _, ok = sentMessageOf(short, "raro", "x")
	assert.False(t, ok)

	// Updates completo: se busca el mensaje nuevo (también el de canal) y se traen las entidades.
	full := &tg.Message{ID: 78, Out: true, PeerID: &tg.PeerUser{UserID: otherUID}, Date: 1700000001, Message: "con medio"}
	upd := &tg.Updates{
		Updates: []tg.UpdateClass{&tg.UpdateMessageID{ID: 78, RandomID: 1}, &tg.UpdateNewMessage{Message: full}},
		Users:   []tg.UserClass{&tg.User{ID: otherUID, FirstName: "Ana"}},
	}
	m, ents, ok := sentMessageOf(upd, "user:100", "")
	require.True(t, ok)
	assert.Equal(t, 78, m.ID)
	assert.Contains(t, ents.Users, otherUID)

	ch := &tg.Message{ID: 5, PeerID: &tg.PeerChannel{ChannelID: 20}}
	m, _, ok = sentMessageOf(&tg.UpdatesCombined{Updates: []tg.UpdateClass{&tg.UpdateNewChannelMessage{Message: ch}}}, "channel:20", "")
	require.True(t, ok)
	assert.Equal(t, 5, m.ID)

	// Sin mensajes nuevos.
	_, _, ok = sentMessageOf(&tg.Updates{Updates: []tg.UpdateClass{&tg.UpdateMessageID{ID: 1}}}, "user:100", "")
	assert.False(t, ok)
	_, _, ok = sentMessageOf(&tg.UpdatesTooLong{}, "user:100", "")
	assert.False(t, ok)
	// Un mensaje vacío no sirve.
	_, _, ok = sentMessageOf(&tg.Updates{Updates: []tg.UpdateClass{&tg.UpdateNewMessage{Message: &tg.MessageEmpty{ID: 1}}}}, "user:100", "")
	assert.False(t, ok)
}

func TestPollOptionBytes(t *testing.T) {
	rec := recordOfPoll(samplePoll()) // Multi = true
	b, err := pollOptionBytes(rec, []string{"Hoy", "Nunca"})
	require.NoError(t, err)
	assert.Equal(t, [][]byte{[]byte("0"), []byte("2")}, b)

	b, err = pollOptionBytes(rec, nil)
	require.NoError(t, err)
	assert.Empty(t, b, "lista vacía: retirar el voto")

	_, err = pollOptionBytes(rec, []string{"Pasado"})
	assert.ErrorIs(t, err, port.ErrInvalid)
	assert.Contains(t, err.Error(), "Pasado")

	single := pollRecord{Options: rec.Options}
	_, err = pollOptionBytes(single, []string{"Hoy", "Nunca"})
	assert.ErrorIs(t, err, port.ErrInvalid)
	b, err = pollOptionBytes(single, []string{"Mañana"})
	require.NoError(t, err)
	assert.Equal(t, [][]byte{[]byte("1")}, b)
}

func TestMsgID(t *testing.T) {
	id, err := msgID(model.MessageRef{NativeID: "42"})
	require.NoError(t, err)
	assert.Equal(t, 42, id)
	for _, bad := range []string{"", "x", "0", "-3"} {
		_, err := msgID(model.MessageRef{NativeID: bad})
		assert.ErrorIs(t, err, port.ErrInvalid, bad)
	}
}

func TestRandIDIsPositiveAndVaries(t *testing.T) {
	seen := map[int64]bool{}
	for i := 0; i < 20; i++ {
		id := randID()
		assert.Positive(t, id)
		seen[id] = true
	}
	assert.Greater(t, len(seen), 15)
}

// ---------------------------------------------------------------------------------------------
// Historias: privacidad y sondeo de vídeo

func TestPrivacyRules(t *testing.T) {
	a, _ := newTestAdapter(t, nil)
	a.peers.feed(bg, testEntities())

	rules, err := a.privacyRules(bg, model.StatusAudience{Mode: "contacts"})
	require.NoError(t, err)
	require.Len(t, rules, 1)
	assert.IsType(t, &tg.InputPrivacyValueAllowContacts{}, rules[0])
	rules, _ = a.privacyRules(bg, model.StatusAudience{})
	assert.IsType(t, &tg.InputPrivacyValueAllowContacts{}, rules[0], "por defecto, contactos")

	rules, err = a.privacyRules(bg, model.StatusAudience{Mode: "allow", PeerIDs: []string{"user:100", "user:101", "user:555"}})
	require.NoError(t, err)
	require.Len(t, rules, 1)
	allow, ok := rules[0].(*tg.InputPrivacyValueAllowUsers)
	require.True(t, ok)
	require.Len(t, allow.Users, 2, "el que no se conoce se omite")
	assert.Equal(t, int64(11), allow.Users[0].(*tg.InputUser).AccessHash)

	rules, err = a.privacyRules(bg, model.StatusAudience{Mode: "deny", PeerIDs: []string{"user:101"}})
	require.NoError(t, err)
	require.Len(t, rules, 2)
	assert.IsType(t, &tg.InputPrivacyValueAllowContacts{}, rules[0])
	deny, ok := rules[1].(*tg.InputPrivacyValueDisallowUsers)
	require.True(t, ok)
	require.Len(t, deny.Users, 1)
	// «deny» sin nadie: solo contactos.
	rules, _ = a.privacyRules(bg, model.StatusAudience{Mode: "deny"})
	assert.Len(t, rules, 1)

	_, err = a.privacyRules(bg, model.StatusAudience{Mode: "allow"})
	assert.ErrorIs(t, err, port.ErrInvalid, "elegir a nadie")
	_, err = a.privacyRules(bg, model.StatusAudience{Mode: "allow", PeerIDs: []string{"user:555"}})
	assert.ErrorIs(t, err, port.ErrInvalid, "ninguno conocido")
	_, err = a.privacyRules(bg, model.StatusAudience{Mode: "raro"})
	assert.ErrorIs(t, err, port.ErrInvalid)
}

func box(typ string, body []byte) []byte {
	b := make([]byte, 8, 8+len(body))
	binary.BigEndian.PutUint32(b[:4], uint32(8+len(body)))
	copy(b[4:], typ)
	return append(b, body...)
}

func TestProbeMP4(t *testing.T) {
	mvhd := make([]byte, 100)                      // versión 0
	binary.BigEndian.PutUint32(mvhd[12:16], 1000)  // escala de tiempo
	binary.BigEndian.PutUint32(mvhd[16:20], 12500) // duración: 12,5 s
	tkhd := make([]byte, 84)
	binary.BigEndian.PutUint32(tkhd[76:80], 720<<16)
	binary.BigEndian.PutUint32(tkhd[80:84], 1280<<16)
	moov := box("moov", append(box("mvhd", mvhd), box("trak", box("tkhd", tkhd))...))
	file := append(append(box("ftyp", []byte("isom0000")), box("mdat", make([]byte, 5000))...), moov...)

	path := filepath.Join(t.TempDir(), "v.mp4")
	require.NoError(t, os.WriteFile(path, file, 0o600))
	info := probeMP4File(path)
	require.True(t, info.OK)
	assert.InDelta(t, 12.5, info.Duration, 0.001)
	assert.Equal(t, 720, info.W)
	assert.Equal(t, 1280, info.H)

	bad := filepath.Join(t.TempDir(), "no.mp4")
	require.NoError(t, os.WriteFile(bad, []byte("esto no es un mp4 de verdad, solo texto"), 0o600))
	assert.False(t, probeMP4File(bad).OK)
	assert.False(t, probeMP4File(filepath.Join(t.TempDir(), "no_existe")).OK)
}

func tgerrNew(code string) error { return tgerr.New(400, code) }

func TestStoryReasonOf(t *testing.T) {
	assert.Equal(t, "hace falta Telegram Premium", reasonOf(tgerrNew("PREMIUM_ACCOUNT_REQUIRED")))
	assert.Equal(t, "llegaste al límite de historias", reasonOf(tgerrNew("STORIES_TOO_MUCH")))
	assert.NotContains(t, reasonOf(tgerrNew("OTRA_COSA")), "593991234567")
}
