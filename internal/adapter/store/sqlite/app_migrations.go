package sqlite

// appMigrations: migraciones de app.db (docs/05-ALMACEN.md §2). Nunca se edita una ya escrita.
var appMigrations = []string{
	// 1
	`
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
  created_at INTEGER NOT NULL
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
`,
	// 2: contactos automáticos, programados recurrentes y progreso del guardado de historial.
	`
ALTER TABLE contacts ADD COLUMN source TEXT NOT NULL DEFAULT 'manual'; -- manual | import | auto

ALTER TABLE scheduled ADD COLUMN recurrence_json TEXT NOT NULL DEFAULT ''; -- '' = una sola vez
ALTER TABLE scheduled ADD COLUMN series_id TEXT NOT NULL DEFAULT '';       -- agrupa los envíos de una serie

CREATE TABLE history_sync (
  account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  chat_native_id TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'pending', -- pending | done | failed | skipped
  requests INTEGER NOT NULL DEFAULT 0,
  empty_streak INTEGER NOT NULL DEFAULT 0,
  added INTEGER NOT NULL DEFAULT 0,
  oldest_ts INTEGER NOT NULL DEFAULT 0,
  last_request_at INTEGER NOT NULL DEFAULT 0,
  done_at INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (account_id, chat_native_id)
);
CREATE INDEX idx_history_sync_state ON history_sync(account_id, state);
`,
	// 3: usuarios y tokens del servidor (docs/14 §3 y §10). Aditiva: 0.2.x ignora estas tablas.
	// Del token solo se guarda su SHA-256; nunca el token.
	`
CREATE TABLE users (
  id TEXT PRIMARY KEY,
  username TEXT NOT NULL COLLATE NOCASE UNIQUE,
  password_hash TEXT NOT NULL,         -- cadena PHC de argon2id
  role TEXT NOT NULL DEFAULT 'admin',
  created_at INTEGER NOT NULL,
  disabled_at INTEGER NOT NULL DEFAULT 0 -- 0 = activo
);

CREATE TABLE auth_tokens (
  token_hash TEXT PRIMARY KEY,         -- SHA-256 (hex) del token
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,                  -- session | media
  created_at INTEGER NOT NULL,
  last_used_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,         -- caducidad deslizante (sesión) o fija (media)
  hard_expires_at INTEGER NOT NULL     -- tope absoluto (30 días)
);
CREATE INDEX idx_auth_tokens_user ON auth_tokens(user_id);
CREATE INDEX idx_auth_tokens_exp ON auth_tokens(expires_at);
`,
}
