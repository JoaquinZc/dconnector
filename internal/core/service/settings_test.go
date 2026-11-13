package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"dconnector/internal/core/port"
)

func TestSettingsDefaults(t *testing.T) {
	app := newMemApp()
	s := NewSettings(app)
	ctx := context.Background()

	// sin nada guardado, All devuelve los valores por defecto
	got, err := s.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !got.CloseToTray || got.StartWithWindows || got.StartMinimized || !got.Notifications || got.Theme != "system" || got.VerboseLog {
		t.Fatalf("por defecto: %+v", got)
	}
	if !got.AutoHistory {
		t.Fatalf("la sincronización automática del historial debe venir encendida: %+v", got)
	}
	if got.OptOutKeywords != "baja,stop,no más,no mas,cancelar,cancelar suscripción,unsubscribe,no quiero recibir" ||
		got.OptOutFooterText != "Responde BAJA para no recibir más mensajes." {
		t.Fatalf("textos por defecto: %+v", got)
	}
	want := map[string][2]any{"image": {true, 20.0}, "audio": {true, 20.0}, "voice": {true, 20.0},
		"video": {false, 50.0}, "document": {false, 20.0}, "sticker": {true, 5.0}}
	if len(got.AutoDownload) != len(want) {
		t.Fatalf("descarga automática: %+v", got.AutoDownload)
	}
	for k, w := range want {
		m, ok := got.AutoDownload[k].(map[string]any)
		if !ok || m["enabled"] != w[0] || m["maxMB"] != w[1] {
			t.Fatalf("%s: %+v", k, got.AutoDownload[k])
		}
	}

	if err := s.EnsureDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{
		"close_to_tray": "true", "start_with_windows": "false", "start_minimized": "false",
		"notifications": "true", "theme": "system", "verbose_log": "false", "auto_history": "true",
		"optout_footer_text": "Responde BAJA para no recibir más mensajes.",
	} {
		if g, _ := app.GetSetting(ctx, k); g != v {
			t.Errorf("%s = %q, quería %q", k, g, v)
		}
	}
	raw, _ := app.GetSetting(ctx, "auto_download_media")
	var m map[string]map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil || m["video"]["maxMB"] != 50.0 {
		t.Fatalf("auto_download_media: %q %v", raw, err)
	}
	id, _ := app.GetSetting(ctx, "instance_id")
	if len(id) != 26 {
		t.Fatalf("instance_id debe ser un ULID: %q", id)
	}
}

func TestSettingsEnsureDefaultsOnlyWritesMissing(t *testing.T) {
	app := newMemApp()
	ctx := context.Background()
	_ = app.SetSetting(ctx, "theme", "dark")
	_ = app.SetSetting(ctx, "close_to_tray", "false")
	_ = app.SetSetting(ctx, "instance_id", "ID-FIJO")
	s := NewSettings(app)
	if err := s.EnsureDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := app.GetSetting(ctx, "theme"); v != "dark" {
		t.Fatalf("theme pisado: %q", v)
	}
	if v, _ := app.GetSetting(ctx, "close_to_tray"); v != "false" {
		t.Fatalf("close_to_tray pisado: %q", v)
	}
	if v, _ := app.GetSetting(ctx, "instance_id"); v != "ID-FIJO" {
		t.Fatalf("instance_id pisado: %q", v)
	}
	if v, _ := app.GetSetting(ctx, "notifications"); v != "true" {
		t.Fatalf("lo que faltaba debe escribirse: %q", v)
	}
	got, _ := s.All(ctx)
	if got.Theme != "dark" || got.CloseToTray {
		t.Fatalf("All: %+v", got)
	}
}

func TestSettingsSaveRoundTrip(t *testing.T) {
	app := newMemApp()
	s := NewSettings(app)
	ctx := context.Background()
	in := AppSettings{
		CloseToTray: false, StartWithWindows: true, StartMinimized: true, Notifications: false,
		Theme: "light", VerboseLog: true, AutoHistory: false,
		AutoDownload:   map[string]any{"image": map[string]any{"enabled": false, "maxMB": 3.0}},
		OptOutKeywords: " salir , basta ", OptOutFooterText: "Escribe SALIR",
	}
	if err := s.Save(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, err := s.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.CloseToTray || !got.StartWithWindows || !got.StartMinimized || got.Notifications || got.Theme != "light" || !got.VerboseLog {
		t.Fatalf("bools y tema: %+v", got)
	}
	if got.OptOutKeywords != "salir , basta" || got.OptOutFooterText != "Escribe SALIR" {
		t.Fatalf("textos: %+v", got)
	}
	if got.AutoHistory {
		t.Fatalf("auto_history apagado se debe conservar apagado: %+v", got)
	}
	if v, _ := app.GetSetting(ctx, "auto_history"); v != "false" {
		t.Fatalf("auto_history crudo: %q", v)
	}
	img := got.AutoDownload["image"].(map[string]any)
	if img["enabled"] != false || img["maxMB"] != 3.0 || len(got.AutoDownload) != 1 {
		t.Fatalf("descarga automática: %+v", got.AutoDownload)
	}
	// Los textos se guardan tal cual (sin comillas JSON) para que otros servicios los lean directo.
	if v, _ := app.GetSetting(ctx, "optout_keywords"); v != "salir , basta" {
		t.Fatalf("optout_keywords crudo: %q", v)
	}
	if v, _ := app.GetSetting(ctx, "theme"); v != "light" {
		t.Fatalf("theme crudo: %q", v)
	}
}

func TestSettingsSaveValidatesAndFillsBlanks(t *testing.T) {
	app := newMemApp()
	s := NewSettings(app)
	ctx := context.Background()
	if err := s.Save(ctx, AppSettings{Theme: "neon"}); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("tema desconocido: %v", err)
	}
	if err := s.Save(ctx, AppSettings{CloseToTray: true}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.All(ctx)
	if got.Theme != "system" || got.OptOutKeywords == "" || got.OptOutFooterText == "" || len(got.AutoDownload) != 6 {
		t.Fatalf("los vacíos toman el valor por defecto: %+v", got)
	}
}

func TestSettingsCorruptAutoDownloadFallsBack(t *testing.T) {
	app := newMemApp()
	ctx := context.Background()
	_ = app.SetSetting(ctx, "auto_download_media", "{no es json")
	_ = app.SetSetting(ctx, "close_to_tray", "quizá")
	got, err := NewSettings(app).All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.AutoDownload) != 6 || !got.CloseToTray {
		t.Fatalf("valores corruptos deben dar los por defecto: %+v", got)
	}
}

func TestSettingsInstanceIDStable(t *testing.T) {
	app := newMemApp()
	s := NewSettings(app)
	ctx := context.Background()
	a := s.InstanceID(ctx)
	if a == "" || s.InstanceID(ctx) != a || NewSettings(app).InstanceID(ctx) != a {
		t.Fatal("el instance_id se crea una vez y no cambia")
	}
}

func TestSettingsAutoHistoryToggle(t *testing.T) {
	app := newMemApp()
	s := NewSettings(app)
	ctx := context.Background()
	// Encendido por defecto, también tras EnsureDefaults (que no pisa lo que ya hay).
	if err := s.EnsureDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.All(ctx); !got.AutoHistory {
		t.Fatalf("por defecto debe estar encendido: %+v", got)
	}
	// Apagarlo y volver a encenderlo.
	in, _ := s.All(ctx)
	in.AutoHistory = false
	if err := s.Save(ctx, in); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.All(ctx); got.AutoHistory {
		t.Fatalf("debe quedar apagado: %+v", got)
	}
	if err := s.EnsureDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.All(ctx); got.AutoHistory {
		t.Fatalf("EnsureDefaults no debe volver a encender lo apagado: %+v", got)
	}
	in.AutoHistory = true
	if err := s.Save(ctx, in); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.All(ctx); !got.AutoHistory {
		t.Fatalf("debe quedar encendido: %+v", got)
	}
	// Un valor ilegible vuelve al valor por defecto (encendido).
	_ = app.SetSetting(ctx, "auto_history", "quizá")
	if got, _ := s.All(ctx); !got.AutoHistory {
		t.Fatalf("valor ilegible: debe tomar el defecto (encendido): %+v", got)
	}
}

func TestSettingsNotificationsDefaults(t *testing.T) {
	app := newMemApp()
	s := NewSettings(app)
	ctx := context.Background()
	got, err := s.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Notifications || !got.NotificationsSound || !got.NotificationsPreview {
		t.Fatalf("notificaciones, sonido y vista previa vienen encendidos: %+v", got)
	}
	if err := s.EnsureDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"notifications": "true", "notifications_sound": "true", "notifications_preview": "true"} {
		if v, _ := app.GetSetting(ctx, k); v != want {
			t.Errorf("%s = %q, quería %q", k, v, want)
		}
	}
	// el aviso de primer arranque no es un ajuste con valor por defecto
	if v, _ := app.GetSetting(ctx, "notifications_asked"); v != "" {
		t.Errorf("notifications_asked no debe crearse solo: %q", v)
	}
}

func TestSettingsNotificationsSaveRoundTrip(t *testing.T) {
	app := newMemApp()
	s := NewSettings(app)
	ctx := context.Background()
	in := AppSettings{CloseToTray: true, Notifications: true, NotificationsSound: false, NotificationsPreview: false}
	if err := s.Save(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, _ := s.All(ctx)
	if !got.Notifications || got.NotificationsSound || got.NotificationsPreview {
		t.Fatalf("sonido y vista previa apagados deben conservarse: %+v", got)
	}
	if v, _ := app.GetSetting(ctx, "notifications_sound"); v != "false" {
		t.Fatalf("notifications_sound crudo: %q", v)
	}
	if v, _ := app.GetSetting(ctx, "notifications_preview"); v != "false" {
		t.Fatalf("notifications_preview crudo: %q", v)
	}
	// EnsureDefaults no vuelve a encender lo apagado
	if err := s.EnsureDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.All(ctx); got.NotificationsSound || got.NotificationsPreview {
		t.Fatalf("EnsureDefaults pisó lo apagado: %+v", got)
	}
	// y los vuelve a encender
	in.NotificationsSound, in.NotificationsPreview = true, true
	if err := s.Save(ctx, in); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.All(ctx); !got.NotificationsSound || !got.NotificationsPreview {
		t.Fatalf("deben quedar encendidos: %+v", got)
	}
	// un valor ilegible vuelve al defecto (encendido)
	_ = app.SetSetting(ctx, "notifications_sound", "quizá")
	if got, _ := s.All(ctx); !got.NotificationsSound {
		t.Fatalf("valor ilegible: debe tomar el defecto: %+v", got)
	}
}

func TestSettingsNotificationsAskedOnce(t *testing.T) {
	app := newMemApp()
	s := NewSettings(app)
	ctx := context.Background()
	if s.NotificationsAsked(ctx) {
		t.Fatal("al principio no se ha preguntado")
	}
	if err := s.MarkNotificationsAsked(ctx); err != nil {
		t.Fatal(err)
	}
	if !s.NotificationsAsked(ctx) || !NewSettings(app).NotificationsAsked(ctx) {
		t.Fatal("debe recordarse")
	}
	// guardar ajustes no lo borra
	if err := s.Save(ctx, AppSettings{CloseToTray: true}); err != nil {
		t.Fatal(err)
	}
	if !s.NotificationsAsked(ctx) {
		t.Fatal("Save no debe tocar notifications_asked")
	}
}
