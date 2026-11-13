// Tipos de la interfaz. Reexporta los de los enlaces generados (frontend/bindings, NO se editan)
// y define los que Wails no genera (conjunto de capacidades, cargas de eventos).
//
// Nota: los enlaces declaran los enums (`Channel`, `ConnState`, `ContentKind`…) como `enum` de
// TypeScript, así que una cadena suelta como "whatsapp" no es asignable a `Channel`. Para eso están
// `ChannelId` y compañía: uniones de literales que sí aceptan cadenas.

import type {
  Account as BAccount,
  AccountID,
  AccountStatus,
  Campaign,
  CampaignTotals,
  Chat,
  ChatFilter as BChatFilter,
  ChatKey,
  Contact,
  ContactCard,
  ContactFilter,
  ContactList,
  Destination,
  DestinationFilter,
  DestinationState,
  Identity,
  Location,
  Media,
  Message,
  MessageKey,
  MessagePage,
  MessageRef,
  Outgoing,
  OutgoingStatus,
  PacingSettings,
  PairInput,
  PairRequest,
  Poll,
  PollOption,
  QueueItem,
  Reaction,
  Scheduled,
  SendWindow,
  Status,
  StatusAudience,
  StatusAuthor,
  StatusView,
} from "./core/model";
import type {
  AccountView as BAccountView,
  AppSettings,
  CSVMapping,
  CampaignReview,
  HistoryStatus,
  ImportResult,
} from "./core/service";
import type {
  ContactConversationStats,
  ConversationOverview,
  GroupsOverview,
  Range,
  StatusStats,
} from "./core/stats";
import type { Estimate, ReviewItem } from "./core/campaign";
import type { ContactPage } from "./svcTypes";
import type {
  NotifyStatus,
  PickedFile,
} from "../../bindings/dconnector/internal/adapter/ui/wails";

export {
  CampaignState,
  Channel,
  ChatKind,
  ConnState,
  ContentKind,
  DeliveryState,
  PairNeed,
} from "./core/model";

export type {
  AccountID,
  AccountStatus,
  AppSettings,
  Campaign,
  CampaignReview,
  CampaignTotals,
  Chat,
  ChatKey,
  Contact,
  ContactCard,
  ContactConversationStats,
  ContactFilter,
  ContactList,
  ContactPage,
  ConversationOverview,
  CSVMapping,
  Destination,
  DestinationFilter,
  DestinationState,
  Estimate,
  GroupsOverview,
  HistoryStatus,
  Identity,
  ImportResult,
  Location,
  Media,
  Message,
  MessageKey,
  MessagePage,
  MessageRef,
  Outgoing,
  OutgoingStatus,
  PacingSettings,
  PairInput,
  PairRequest,
  NotifyStatus,
  PickedFile,
  Poll,
  PollOption,
  QueueItem,
  Range,
  Reaction,
  ReviewItem,
  Scheduled,
  SendWindow,
  Status,
  StatusAudience,
  StatusAuthor,
  StatusStats,
  StatusView,
};

// ---------------------------------------------------------------- literales cómodos

/** Permiso de notificaciones del sistema (`NotifyStatus.state`). */
export type NotifyStateId = "granted" | "denied" | "unknown" | "unsupported";

/** ¿Windows suprime los avisos en pantalla (No molestar, pantalla completa, presentación)? (`NotifyStatus.quiet`). */
export type QuietStateId = "active" | "inactive" | "unknown";

export type ChannelId = "whatsapp" | "telegram" | "messenger" | "instagram";
export type ConnStateId =
  | "disconnected"
  | "connecting"
  | "pairing"
  | "connected"
  | "logged_out"
  | "error";
export type PairNeedId =
  | ""
  | "scan_qr"
  | "enter_code"
  | "phone"
  | "login_code"
  | "password"
  | "cookies";
export type ContentKindId =
  | "text"
  | "image"
  | "video"
  | "audio"
  | "voice"
  | "document"
  | "sticker"
  | "location"
  | "contact"
  | "poll"
  | "system"
  | "unsupported";
export type DeliveryId = "pending" | "sent" | "delivered" | "read" | "played" | "failed";
export type ChatTab = "all" | "unread" | "groups" | "archived";

/** Cuenta tal como la usa la interfaz (canal como cadena). */
export type Account = BAccount;
export type AccountView = BAccountView;

/** Filtro de la lista de chats. Igual que el de Go, pero `channels` admite cadenas sueltas. */
export type ChatFilter = Omit<BChatFilter, "channels" | "tab"> & {
  channels: string[] | null;
  tab: ChatTab | string;
};

// ---------------------------------------------------------------- capacidades

/**
 * Conjunto de capacidades (03 · capability.Set). Los enlaces lo declaran `any` porque Go lo
 * serializa a mano; aquí va con su forma real. Las ventanas llegan en milisegundos (0 = sin límite).
 */
export interface CapSet {
  sendText: boolean;
  sendImage: boolean;
  sendVideo: boolean;
  sendAudio: boolean;
  sendVoice: boolean;
  sendDocument: boolean;
  sendSticker: boolean;
  sendLocation: boolean;
  sendContact: boolean;
  sendPoll: boolean;
  votePoll: boolean;

  reply: boolean;
  forward: boolean;
  react: boolean;
  maxReactionsPerUser: number;

  edit: boolean;
  editWindowMs: number;
  deleteForEveryone: boolean;
  deleteWindowMs: number;

  readReceipts: boolean;
  typing: boolean;
  mentions: boolean;
  groups: boolean;
  /** Se puede iniciar una conversación nueva (WhatsApp, Telegram); Messenger e Instagram solo continúan las que existen. */
  startChat: boolean;

  statuses: boolean;
  postStatus: boolean;
  statusViewers: boolean;
  statusAudience: boolean;
  statusReact: boolean;

  e2eeChats: boolean;
  historyOnDemand: boolean;

  maxFileSize: number;
  maxImageSize: number;
  maxVideoSize: number;
  maxTextLen: number;
}

// ---------------------------------------------------------------- eventos (11 §2)

export interface Person {
  accountId: string;
  nativeId: string;
  name: string;
  pushName: string;
  phone: string;
  username: string;
  avatarUrl: string;
}

export interface ReceiptEvent {
  chatKey: string;
  messageKeys: string[] | null;
  delivery: DeliveryId | string;
  readerId: string;
  at: number;
}

export interface TypingEvent {
  chatKey: string;
  peerId: string;
  typing: boolean;
  recording: boolean;
}

export interface AccountCapsEvent {
  accountId: string;
  capabilities: CapSet;
}

export interface ChatRemovedEvent {
  chatKey: string;
}

export interface UnreadTotalEvent {
  total: number;
  byAccount: Record<string, number>;
}

export interface HistoryProgressEvent {
  accountId: string;
  progress: number;
}

/** Progreso de una campaña (lo emite Campaigns.Progress()). Campos extra se ignoran. */
export interface CampaignProgressEvent {
  campaignId?: string;
  state?: string;
  totals?: CampaignTotals;
  [k: string]: unknown;
}

/** Primer mensaje del WebSocket (docs/14 §5). */
export interface HelloEvent {
  version: string;
  apiVersion: number;
  protocol: number;
  serverTime: number;
  user: string;
  connId: string;
}

/** El servidor ya decidió que hay que avisar; el escritorio solo lo muestra (docs/14 §5.1). */
export interface NotifyEvent {
  chatKey: string;
  accountId: string;
  channel: string;
  title: string;
  body: string;
  preview: string;
  silent: boolean;
  sound: boolean;
  reason: string;
}

export interface NotifyDecisionEvent {
  chatKey: string;
  shown: boolean;
  reason: string;
}

/** Evento INTERNO del cliente (no viene del servidor): estado del enlace en vivo. */
export interface ConnectionEvent {
  state: "connected" | "reconnecting" | "offline";
  /** true si es una reconexión (o se perdieron eventos): hay que volver a cargar el estado. */
  reconnected: boolean;
}

/** Datos de cada evento en vivo, por nombre. */
export interface EventPayloads {
  "account:status": AccountStatus;
  "account:caps": AccountCapsEvent;
  "chat:upsert": Chat[];
  "chat:removed": ChatRemovedEvent;
  "message:upsert": Message[];
  "message:update": Message;
  receipt: ReceiptEvent;
  typing: TypingEvent;
  "person:upsert": Person;
  "status:upsert": Status;
  "status:view": StatusView;
  "history:progress": HistoryProgressEvent;
  "history:sync": HistoryStatus;
  "campaign:progress": CampaignProgressEvent;
  "destination:state": DestinationState;
  "unread:total": UnreadTotalEvent;
  "ui:open-chat": string;
  hello: HelloEvent;
  notify: NotifyEvent;
  "notify:decision": NotifyDecisionEvent;
  connection: ConnectionEvent;
}

export type EventName = keyof EventPayloads;
