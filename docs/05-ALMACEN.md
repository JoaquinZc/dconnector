# 05 · Almacén (SQLite)

## 1. Abrir

`adapter/store/sqlite/open.go`:

```go
import _ "modernc.org/sqlite" // driver "sqlite"

func Open(path string) (*sql.DB, error) {
	dsn := "file:" + filepath.ToSlash(path) +
		"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)" +
		"&_pragma=synchronous(NORMAL)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	// SetMaxOpenConns(8); SetConnMaxIdleTime(5 * time.Minute); db.Ping()
}
```

Migraciones (`migrate.go`): tabla `schema_migrations(version INTEGER PRIMARY KEY, applied_at INTEGER)`.
Una lista `[]string` de SQL por archivo (`appMigrations`, `accountMigrations`); se aplican en orden,
cada una en su transacción, las que tengan índice ≥ versión actual. Nunca se edita una migración
ya escrita: los cambios van en una nueva.

Las listas (`[]string`, etiquetas, ids) se guardan como **JSON en columnas TEXT**; los objetos
(`Outgoing`, `PacingSettings`, filtros) también.

## 2. app.db — migración 1

```sql
CREATE TABLE accounts (
  id TEXT PRIMARY KEY,
  channel TEXT NOT NULL,
  label TEXT NOT NULL,
  color TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  self_id TEXT NOT NULL DEFAULT '',
  self_name TEXT NOT NULL DEFAULT '',
  self_handle TEXT NOT NULL DEFAULT '',
  config_json TEXT NOT NULL DEFAULT '{}',
  sort INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);

CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);

CREATE TABLE contacts (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  phone TEXT NOT NULL DEFAULT '',
  notes TEXT NOT NULL DEFAULT '',
  tags_json TEXT NOT NULL DEFAULT '[]',
  opted_out INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX idx_contacts_phone ON contacts(phone);
CREATE INDEX idx_contacts_name ON contacts(name COLLATE NOCASE);

CREATE TABLE contact_identities (
  contact_id TEXT NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
  account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  channel TEXT NOT NULL,
  peer_native_id TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  phone TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (account_id, peer_native_id)
);
CREATE INDEX idx_identities_contact ON contact_identities(contact_id);

CREATE TABLE lists (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  color TEXT NOT NULL DEFAULT '#64748b',
  created_at INTEGER NOT NULL
);

CREATE TABLE list_members (
  list_id TEXT NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
  contact_id TEXT NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
  PRIMARY KEY (list_id, contact_id)
);

CREATE TABLE campaigns (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  state TEXT NOT NULL,
  text TEXT NOT NULL,
  variants_json TEXT NOT NULL DEFAULT '[]',
  file_path TEXT NOT NULL DEFAULT '',
  list_ids_json TEXT NOT NULL DEFAULT '[]',
  contact_ids_json TEXT NOT NULL DEFAULT '[]',
  start_at INTEGER NOT NULL DEFAULT 0,
  window_json TEXT NOT NULL,
  pacing_json TEXT NOT NULL,
  optout_footer INTEGER NOT NULL DEFAULT 0,
  ab_enabled INTEGER NOT NULL DEFAULT 0,
  brake_reason TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE campaign_queue (
  id TEXT PRIMARY KEY,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  account_id TEXT NOT NULL,
  contact_id TEXT NOT NULL,
  chat_native_id TEXT NOT NULL,
  risk TEXT NOT NULL,
  variant INTEGER NOT NULL DEFAULT 0,
  ab_sample INTEGER NOT NULL DEFAULT 0,
  text TEXT NOT NULL,
  state TEXT NOT NULL,
  send_after INTEGER NOT NULL DEFAULT 0,
  sent_at INTEGER NOT NULL DEFAULT 0,
  message_key TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  sort INTEGER NOT NULL
);
CREATE INDEX idx_queue_campaign_state ON campaign_queue(campaign_id, state, sort);
CREATE INDEX idx_queue_account_sent ON campaign_queue(account_id, sent_at);
CREATE INDEX idx_queue_message ON campaign_queue(message_key);

CREATE TABLE scheduled (
  id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  chat_native_id TEXT NOT NULL DEFAULT '',
  chat_title TEXT NOT NULL DEFAULT '',
  payload_json TEXT NOT NULL,          -- Outgoing u OutgoingStatus
  send_at INTEGER NOT NULL,
  state TEXT NOT NULL,
  sent_at INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  recurrence_json TEXT NOT NULL DEFAULT '',  -- migración 2: '' = una sola vez; si no, model.Recurrence
  series_id TEXT NOT NULL DEFAULT ''         -- migración 2: agrupa las ocurrencias de una serie
);
CREATE INDEX idx_scheduled_due ON scheduled(state, send_at);

CREATE TABLE destinations (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  url TEXT NOT NULL,
  token TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  allow_commands INTEGER NOT NULL DEFAULT 0,
  filter_json TEXT NOT NULL DEFAULT '{}',
  created_at INTEGER NOT NULL
);

CREATE TABLE outbox (
  destination_id TEXT NOT NULL REFERENCES destinations(id) ON DELETE CASCADE,
  event_id TEXT NOT NULL,
  payload BLOB NOT NULL,              -- frame JSON completo listo para enviar
  created_at INTEGER NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (destination_id, event_id)
);
CREATE INDEX idx_outbox_order ON outbox(destination_id, created_at);
```

Ajustes y valores por defecto (los crea `service.Settings` si faltan; valor en JSON):

| Clave | Por defecto |
| --- | --- |
| `instance_id` | ULID nuevo (id de este dConnector en el protocolo) |
| `close_to_tray` | `true` |
| `start_with_windows` | `false` |
| `start_minimized` | `false` |
| `notifications` | `true` |
| `theme` | `"system"` (`"light"`/`"dark"`) |
| `verbose_log` | `false` |
| `auto_download_media` | `{"image":true,"voice":true,"audio":false,"video":false,"document":false,"maxMB":16}` |
| `pacing:<accountId>` | `model.DefaultPacing` (ritmo de cada cuenta; se crea al añadir la cuenta) |
| `warmup_started_at:<accountId>` | `0` (se fija con el primer envío de campaña de esa cuenta) |
| `optout_keywords` | `"BAJA,STOP,NO MAS,NO MÁS,CANCELAR"` (igual que la base) |
| `optout_footer_text` | el valor por defecto de `optout_footer_text` en `../dconnector/src/main/database/schema.ts` |

## 3. account.db — migración 1

Las tablas de la librería del canal (whatsmeow `whatsmeow_*`) las crea la librería en el **mismo
archivo**. Las nuestras:

```sql
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
-- se mantiene a mano en UpsertMessage / ApplyUpdate (insert/delete por chat_native_id+native_id)

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
-- WhatsApp (MaxReactionsPerUser = 1): ApplyReaction borra las del mismo emisor antes de insertar.
-- emoji "" = borrar las del emisor.

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
```

Reglas de `AccountStore`:
- `UpsertMessage`: `INSERT … ON CONFLICT(chat_native_id, native_id) DO UPDATE` sin pisar `delivery`
  con un valor menor (orden `pending<sent<delivered<read<played`; `failed` solo pisa `pending`),
  y sin borrar `media.local_path`. Si el mensaje es más reciente que `chats.last_message_at`,
  actualiza `last_message_at`, `last_preview` (11-INTERFAZ §6 «vista previa») y `last_from_me`; si
  no es mío y no es histórico, suma 1 a `unread_count`. **El histórico no suma no leídos**: el
  Router pasa la marca `history` (ver 06 §2) y `UpsertMessages` nunca suma.
- `ListMessages`: `ORDER BY ts DESC LIMIT n` y se devuelve invertido; `HasMore` si llegaron `n`.
- `ListChats` con `Tab`: `unread` → `unread_count>0`; `groups` → `kind='group'`; `archived` →
  `archived=1`; `all` → `archived=0`. `Query` → `title LIKE %q%` (sin distinguir mayúsculas).
  Orden: `pinned DESC, last_message_at DESC`.
- `Media.URL` se calcula al leer: `/media/<accountId>/<mediaId>`. `AvatarURL`:
  `/avatar/<accountId>/<avatar_file>` si `avatar_file != ''`.

## 4. Carpeta de datos

```
<data>/
  app.db
  logs/dconnector-AAAA-MM-DD.log
  campaign-media/<campaignId>/<archivo>
  status-media/<ulid>.<ext>          copias de lo que se programa para estados
  tmp/                               subidas desde la interfaz (se vacía al arrancar)
  accounts/<accountId>/account.db
  accounts/<accountId>/media/<mediaId>.<ext>
  accounts/<accountId>/avatars/<archivo>.jpg
```
