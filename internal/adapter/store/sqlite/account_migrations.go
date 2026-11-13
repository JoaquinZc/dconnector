package sqlite

// accountMigrations: migraciones de account.db (docs/05-ALMACEN.md §3). Las tablas de la librería
// del canal (p. ej. whatsmeow_*) las crea la librería en el mismo archivo; aquí no se usan esos
// nombres. Nunca se edita una migración ya escrita.
var accountMigrations = []string{
	// 1
	`
CREATE TABLE chats (
  native_id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  avatar_file TEXT NOT NULL DEFAULT '',
  unread_count INTEGER NOT NULL DEFAULT 0,
  last_message_at INTEGER NOT NULL DEFAULT 0,
  last_preview TEXT NOT NULL DEFAULT '',
  last_from_me INTEGER NOT NULL DEFAULT 0,
  archived INTEGER NOT NULL DEFAULT 0,
  pinned INTEGER NOT NULL DEFAULT 0,
  muted_until INTEGER NOT NULL DEFAULT 0,
  read_only INTEGER NOT NULL DEFAULT 0,
  peer_native_id TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_chats_last ON chats(archived, last_message_at DESC);

CREATE TABLE persons (
  native_id TEXT PRIMARY KEY,
  name TEXT NOT NULL DEFAULT '',
  push_name TEXT NOT NULL DEFAULT '',
  phone TEXT NOT NULL DEFAULT '',
  username TEXT NOT NULL DEFAULT '',
  avatar_file TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_persons_phone ON persons(phone);

CREATE TABLE messages (
  chat_native_id TEXT NOT NULL,
  native_id TEXT NOT NULL,
  sender_native_id TEXT NOT NULL DEFAULT '',
  sender_name TEXT NOT NULL DEFAULT '',
  from_me INTEGER NOT NULL,
  ts INTEGER NOT NULL,
  kind TEXT NOT NULL,
  text TEXT NOT NULL DEFAULT '',
  media_id TEXT NOT NULL DEFAULT '',
  extra_json TEXT NOT NULL DEFAULT '{}', -- location, contacts, poll, mentions, quoted*, fromStatusId
  forwarded INTEGER NOT NULL DEFAULT 0,
  edited_at INTEGER NOT NULL DEFAULT 0,
  deleted_at INTEGER NOT NULL DEFAULT 0,
  delivery TEXT NOT NULL DEFAULT 'sent',
  PRIMARY KEY (chat_native_id, native_id)
);
CREATE INDEX idx_messages_chat_ts ON messages(chat_native_id, ts DESC);
CREATE INDEX idx_messages_ts ON messages(ts);

CREATE VIRTUAL TABLE messages_fts USING fts5(
  text, chat_native_id UNINDEXED, native_id UNINDEXED,
  tokenize = 'unicode61 remove_diacritics 2'
);

CREATE TABLE media (
  id TEXT PRIMARY KEY,
  chat_native_id TEXT NOT NULL,
  message_native_id TEXT NOT NULL,
  mime TEXT NOT NULL DEFAULT '',
  file_name TEXT NOT NULL DEFAULT '',
  size INTEGER NOT NULL DEFAULT 0,
  width INTEGER NOT NULL DEFAULT 0,
  height INTEGER NOT NULL DEFAULT 0,
  duration REAL NOT NULL DEFAULT 0,
  thumb TEXT NOT NULL DEFAULT '',
  handle BLOB,
  local_path TEXT NOT NULL DEFAULT ''
);

CREATE TABLE reactions (
  chat_native_id TEXT NOT NULL,
  message_native_id TEXT NOT NULL,
  sender_native_id TEXT NOT NULL,
  sender_name TEXT NOT NULL DEFAULT '',
  emoji TEXT NOT NULL,
  at INTEGER NOT NULL,
  PRIMARY KEY (chat_native_id, message_native_id, sender_native_id, emoji)
);

CREATE TABLE statuses (
  native_id TEXT PRIMARY KEY,
  author_native_id TEXT NOT NULL,
  author_name TEXT NOT NULL DEFAULT '',
  from_me INTEGER NOT NULL,
  posted_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  kind TEXT NOT NULL,
  text TEXT NOT NULL DEFAULT '',
  bg_color TEXT NOT NULL DEFAULT '',
  font INTEGER NOT NULL DEFAULT 0,
  media_id TEXT NOT NULL DEFAULT '',
  viewed INTEGER NOT NULL DEFAULT 0,
  deleted_at INTEGER NOT NULL DEFAULT 0,
  delivery TEXT NOT NULL DEFAULT 'sent'
);
CREATE INDEX idx_statuses_author ON statuses(author_native_id, posted_at);
CREATE INDEX idx_statuses_exp ON statuses(expires_at);

CREATE TABLE status_views (
  status_native_id TEXT NOT NULL,
  viewer_id TEXT NOT NULL,
  viewer_name TEXT NOT NULL DEFAULT '',
  at INTEGER NOT NULL,
  reaction TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (status_native_id, viewer_id)
);

CREATE TABLE kv (key TEXT PRIMARY KEY, value BLOB NOT NULL);
`,
	// 2: reparación de vistas previas: chats con mensajes guardados cuya vista previa quedó vacía
	// (el historial trae la marca de tiempo del chat antes que el mensaje) se rellenan con el último
	// mensaje guardado. La vista previa «bonita» (📷 Foto…) la vuelve a calcular el almacén al guardar.
	`
UPDATE chats SET
  last_message_at = (SELECT MAX(m.ts) FROM messages m WHERE m.chat_native_id = chats.native_id),
  last_from_me = COALESCE((SELECT m.from_me FROM messages m WHERE m.chat_native_id = chats.native_id ORDER BY m.ts DESC LIMIT 1), 0),
  last_preview = COALESCE((SELECT CASE
      WHEN m.deleted_at > 0 THEN 'Mensaje eliminado'
      WHEN TRIM(m.text) <> '' THEN SUBSTR(TRIM(m.text), 1, 80)
      WHEN m.kind = 'image' THEN '📷 Foto'
      WHEN m.kind = 'video' THEN '🎥 Vídeo'
      WHEN m.kind = 'voice' THEN '🎤 Nota de voz'
      WHEN m.kind = 'audio' THEN '🎵 Audio'
      WHEN m.kind = 'document' THEN '📄 Documento'
      WHEN m.kind = 'sticker' THEN 'Sticker'
      WHEN m.kind = 'location' THEN '📍 Ubicación'
      WHEN m.kind = 'contact' THEN '👤 Contacto'
      WHEN m.kind = 'poll' THEN '📊 Encuesta'
      WHEN m.kind = 'system' THEN ''
      ELSE 'Mensaje no compatible' END
    FROM messages m WHERE m.chat_native_id = chats.native_id ORDER BY m.ts DESC LIMIT 1), '')
WHERE last_preview = ''
  AND EXISTS (SELECT 1 FROM messages m WHERE m.chat_native_id = chats.native_id);
`,
}
