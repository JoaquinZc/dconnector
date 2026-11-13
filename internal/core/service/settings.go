package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// Claves de app_settings que usa Settings (cada campo de AppSettings tiene la suya).
// Otros servicios (Router, Messaging, Campaigns) leen estas mismas claves.
//
// Formato del valor: los booleanos van como "true"/"false"; los textos van tal cual
// (sin comillas JSON); auto_download_media es un objeto JSON; instance_id es un ULID.
const (
	SettingCloseToTray      = "close_to_tray"
	SettingStartWithWindows = "start_with_windows"
	SettingStartMinimized   = "start_minimized"
	SettingNotifications    = "notifications"
	SettingTheme            = "theme"
	SettingVerboseLog       = "verbose_log"
	SettingAutoDownload     = "auto_download_media"
	SettingOptOutKeywords   = "optout_keywords"
	SettingOptOutFooter     = "optout_footer_text"
	SettingInstanceID       = "instance_id"
	// SettingAutoHistory: sincronización automática del historial al conectar (por defecto encendida).
	SettingAutoHistory = "auto_history"
	// SettingNotificationsSound: el aviso de un mensaje nuevo suena (por defecto sí).
	SettingNotificationsSound = "notifications_sound"
	// SettingNotificationsPreview: la notificación muestra el texto del mensaje (por defecto sí).
	SettingNotificationsPreview = "notifications_preview"
	// SettingNotificationsAsked: ya se comprobó el permiso de notificaciones en el primer arranque.
	// No es un campo de AppSettings: lo escribe MarkNotificationsAsked y Save no lo toca.
	SettingNotificationsAsked = "notifications_asked"
	// SettingAccountOrder: JSON con la lista de ids de cuenta en el orden elegido.
	SettingAccountOrder = "account_order"
)

const (
	defaultsOptOutKeywords = "baja,stop,no más,no mas,cancelar,cancelar suscripción,unsubscribe,no quiero recibir"
	defaultsOptOutFooter   = "Responde BAJA para no recibir más mensajes."
	defaultsAutoDownload   = `{"image":{"enabled":true,"maxMB":20},"audio":{"enabled":true,"maxMB":20},` +
		`"voice":{"enabled":true,"maxMB":20},"video":{"enabled":false,"maxMB":50},` +
		`"document":{"enabled":false,"maxMB":20},"sticker":{"enabled":true,"maxMB":5}}`
)

// defaultAutoDownloadMap devuelve la configuración de descarga automática por defecto.
func defaultAutoDownloadMap() map[string]any {
	var m map[string]any
	_ = json.Unmarshal([]byte(defaultsAutoDownload), &m)
	return m
}

// Settings: ajustes de la aplicación (06 §12).
type Settings struct {
	app port.AppStore
	mu  sync.Mutex // serializa la creación del instance_id
}

func NewSettings(app port.AppStore) *Settings { return &Settings{app: app} }

type settingDefault struct{ key, value string }

func settingDefaults() []settingDefault {
	return []settingDefault{
		{SettingCloseToTray, "true"},
		{SettingStartWithWindows, "false"},
		{SettingStartMinimized, "false"},
		{SettingNotifications, "true"},
		{SettingTheme, "system"},
		{SettingVerboseLog, "false"},
		{SettingAutoHistory, "true"},
		{SettingNotificationsSound, "true"},
		{SettingNotificationsPreview, "true"},
		{SettingAutoDownload, defaultsAutoDownload},
		{SettingOptOutKeywords, defaultsOptOutKeywords},
		{SettingOptOutFooter, defaultsOptOutFooter},
	}
}

// EnsureDefaults escribe solo los ajustes que faltan y crea el instance_id si no existe.
func (s *Settings) EnsureDefaults(ctx context.Context) error {
	for _, d := range settingDefaults() {
		v, err := s.app.GetSetting(ctx, d.key)
		if err != nil {
			return fmt.Errorf("ajustes: leer %s: %w", d.key, err)
		}
		if v != "" {
			continue
		}
		if err := s.app.SetSetting(ctx, d.key, d.value); err != nil {
			return fmt.Errorf("ajustes: escribir %s: %w", d.key, err)
		}
	}
	if s.InstanceID(ctx) == "" {
		return fmt.Errorf("ajustes: no se pudo crear el identificador de la instalación")
	}
	return nil
}

func (s *Settings) get(ctx context.Context, key string) (string, error) {
	v, err := s.app.GetSetting(ctx, key)
	if err != nil {
		return "", fmt.Errorf("ajustes: leer %s: %w", key, err)
	}
	return v, nil
}

func parseBoolSetting(v string, def bool) bool {
	if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil && v != "" {
		return b
	}
	return def
}

// All lee todos los ajustes; lo que falte toma su valor por defecto.
func (s *Settings) All(ctx context.Context) (AppSettings, error) {
	out := AppSettings{
		CloseToTray: true, Notifications: true, Theme: "system", AutoHistory: true,
		NotificationsSound: true, NotificationsPreview: true,
		AutoDownload:   defaultAutoDownloadMap(),
		OptOutKeywords: defaultsOptOutKeywords, OptOutFooterText: defaultsOptOutFooter,
	}
	bools := []struct {
		key string
		dst *bool
	}{
		{SettingCloseToTray, &out.CloseToTray},
		{SettingStartWithWindows, &out.StartWithWindows},
		{SettingStartMinimized, &out.StartMinimized},
		{SettingNotifications, &out.Notifications},
		{SettingVerboseLog, &out.VerboseLog},
		{SettingAutoHistory, &out.AutoHistory},
		{SettingNotificationsSound, &out.NotificationsSound},
		{SettingNotificationsPreview, &out.NotificationsPreview},
	}
	for _, b := range bools {
		v, err := s.get(ctx, b.key)
		if err != nil {
			return AppSettings{}, err
		}
		*b.dst = parseBoolSetting(v, *b.dst)
	}
	strs := []struct {
		key string
		dst *string
	}{
		{SettingTheme, &out.Theme},
		{SettingOptOutKeywords, &out.OptOutKeywords},
		{SettingOptOutFooter, &out.OptOutFooterText},
	}
	for _, p := range strs {
		v, err := s.get(ctx, p.key)
		if err != nil {
			return AppSettings{}, err
		}
		if v != "" {
			*p.dst = v
		}
	}
	v, err := s.get(ctx, SettingAutoDownload)
	if err != nil {
		return AppSettings{}, err
	}
	if v != "" {
		var m map[string]any
		if json.Unmarshal([]byte(v), &m) == nil && m != nil {
			out.AutoDownload = m
		}
	}
	return out, nil
}

// Save guarda todos los campos. Un texto vacío vuelve al valor por defecto (el almacén no
// distingue «vacío» de «no existe»).
func (s *Settings) Save(ctx context.Context, v AppSettings) error {
	switch v.Theme {
	case "":
		v.Theme = "system"
	case "system", "light", "dark":
	default:
		return fmt.Errorf("%w: tema desconocido %q", port.ErrInvalid, v.Theme)
	}
	if strings.TrimSpace(v.OptOutKeywords) == "" {
		v.OptOutKeywords = defaultsOptOutKeywords
	}
	if strings.TrimSpace(v.OptOutFooterText) == "" {
		v.OptOutFooterText = defaultsOptOutFooter
	}
	if v.AutoDownload == nil {
		v.AutoDownload = defaultAutoDownloadMap()
	}
	ad, err := json.Marshal(v.AutoDownload)
	if err != nil {
		return fmt.Errorf("%w: descarga automática: %v", port.ErrInvalid, err)
	}
	pairs := []settingDefault{
		{SettingCloseToTray, strconv.FormatBool(v.CloseToTray)},
		{SettingStartWithWindows, strconv.FormatBool(v.StartWithWindows)},
		{SettingStartMinimized, strconv.FormatBool(v.StartMinimized)},
		{SettingNotifications, strconv.FormatBool(v.Notifications)},
		{SettingTheme, v.Theme},
		{SettingVerboseLog, strconv.FormatBool(v.VerboseLog)},
		{SettingAutoHistory, strconv.FormatBool(v.AutoHistory)},
		{SettingNotificationsSound, strconv.FormatBool(v.NotificationsSound)},
		{SettingNotificationsPreview, strconv.FormatBool(v.NotificationsPreview)},
		{SettingAutoDownload, string(ad)},
		{SettingOptOutKeywords, strings.TrimSpace(v.OptOutKeywords)},
		{SettingOptOutFooter, strings.TrimSpace(v.OptOutFooterText)},
	}
	for _, p := range pairs {
		if err := s.app.SetSetting(ctx, p.key, p.value); err != nil {
			return fmt.Errorf("ajustes: guardar %s: %w", p.key, err)
		}
	}
	return nil
}

// InstanceID devuelve el identificador de esta instalación (un ULID creado una sola vez).
// Devuelve "" si no se puede leer ni crear.
func (s *Settings) InstanceID(ctx context.Context) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := s.app.GetSetting(ctx, SettingInstanceID)
	if err != nil {
		return ""
	}
	if id != "" {
		return id
	}
	id = model.NewID()
	if err := s.app.SetSetting(ctx, SettingInstanceID, id); err != nil {
		return ""
	}
	return id
}

// NotificationsAsked dice si ya se comprobó el permiso de notificaciones en el primer arranque.
func (s *Settings) NotificationsAsked(ctx context.Context) bool {
	v, err := s.app.GetSetting(ctx, SettingNotificationsAsked)
	return err == nil && parseBoolSetting(v, false)
}

// MarkNotificationsAsked anota que el permiso ya se comprobó (solo se hace una vez por instalación).
func (s *Settings) MarkNotificationsAsked(ctx context.Context) error {
	if err := s.app.SetSetting(ctx, SettingNotificationsAsked, "true"); err != nil {
		return fmt.Errorf("ajustes: guardar %s: %w", SettingNotificationsAsked, err)
	}
	return nil
}
