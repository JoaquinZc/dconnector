// Tipos EXPLÍCITOS de los servicios del núcleo que viven en el servidor (docs/14). Una interfaz por servicio con
// la firma exacta de cada método RPC (las de `internal/adapter/api/facade/*.go`). No dependen de
// `frontend/bindings` (que se regenera con `-clean` y solo contiene los servicios del escritorio): los modelos
// viven versionados en `./core`. Si cambia una fachada Go, se actualiza aquí a mano; `svcTypes.test.ts` vigila
// que `api` y estas interfaces sigan alineadas.
import type * as capability$0 from "./core/capability";
import type * as model$0 from "./core/model";
import type * as service$0 from "./core/service";
import type * as stats$0 from "./core/stats";

/** Página de contactos con el total (`Contacts.List`). */
export interface ContactPage {
  contacts: model$0.Contact[] | null;
  total: number;
}

/** Cuentas y sus canales. */
export interface AccountsSvc {
  BeginPairing(id: string, req: model$0.PairRequest): PromiseLike<void>;
  Capabilities(chatKey: string): PromiseLike<capability$0.Set>;
  Connect(id: string): PromiseLike<void>;
  Create(channel: string, label: string, color: string, config: { [_ in string]?: string } | null): PromiseLike<model$0.Account>;
  Delete(id: string): PromiseLike<void>;
  Disconnect(id: string): PromiseLike<void>;
  List(): PromiseLike<service$0.AccountView[] | null>;
  Logout(id: string): PromiseLike<void>;
  Reorder(ids: string[] | null): PromiseLike<void>;
  SetConfig(id: string, config: { [_ in string]?: string } | null): PromiseLike<void>;
  SubmitPairing(id: string, input: model$0.PairInput): PromiseLike<void>;
  Update(id: string, label: string, color: string, enabled: boolean): PromiseLike<void>;
}

/** Bandeja: chats, mensajes y búsqueda. */
export interface InboxSvc {
  Archive(chatKey: string, v: boolean): PromiseLike<void>;
  GetChat(chatKey: string): PromiseLike<model$0.Chat>;
  ListChats(f: model$0.ChatFilter): PromiseLike<model$0.Chat[] | null>;
  LoadOlder(chatKey: string): PromiseLike<void>;
  Messages(chatKey: string, beforeTs: number, limit: number): PromiseLike<model$0.MessagePage>;
  Mute(chatKey: string, until: number): PromiseLike<void>;
  OpenChat(accountID: string, peerNativeID: string): PromiseLike<model$0.Chat>;
  Pin(chatKey: string, v: boolean): PromiseLike<void>;
  Search(query: string, accountIDs: string[] | null, limit: number): PromiseLike<model$0.Message[] | null>;
  UnreadTotals(): PromiseLike<{ [_ in string]?: number } | null>;
}

/** Envío y acciones sobre mensajes (SaveMediaAs lo resuelve el escritorio, no el servidor). */
export interface MessagingSvc {
  Delete(msgKey: string, forEveryone: boolean): PromiseLike<void>;
  Edit(msgKey: string, text: string): PromiseLike<void>;
  Forward(msgKey: string, toChatKey: string): PromiseLike<model$0.Message>;
  MarkRead(chatKey: string): PromiseLike<void>;
  React(msgKey: string, emoji: string): PromiseLike<void>;
  SaveMediaAs(accountID: string, mediaID: string): PromiseLike<string>;
  Send(chatKey: string, out: model$0.Outgoing): PromiseLike<model$0.Message>;
  SetTyping(chatKey: string, typing: boolean): PromiseLike<void>;
  VotePoll(msgKey: string, options: string[] | null): PromiseLike<void>;
}

/** Estados (historias). */
export interface StatusesSvc {
  Authors(accountIDs: string[] | null): PromiseLike<model$0.StatusAuthor[] | null>;
  ByAuthor(acc: string, authorID: string): PromiseLike<model$0.Status[] | null>;
  Delete(acc: string, statusNativeID: string): PromiseLike<void>;
  MarkViewed(acc: string, statusKey: string): PromiseLike<void>;
  Mine(acc: string, $from: number, to: number): PromiseLike<model$0.Status[] | null>;
  Post(acc: string, st: model$0.OutgoingStatus): PromiseLike<model$0.Status>;
  React(acc: string, statusKey: string, emoji: string): PromiseLike<void>;
  Stats(acc: string, rng: stats$0.Range): PromiseLike<stats$0.StatusStats>;
  Viewers(acc: string, statusNativeID: string): PromiseLike<model$0.StatusView[] | null>;
}

/** Contactos unificados y listas. */
export interface ContactsSvc {
  Delete(id: string): PromiseLike<void>;
  DeleteList(id: string): PromiseLike<void>;
  FromChat(chatKey: string): PromiseLike<model$0.Contact>;
  Get(id: string): PromiseLike<model$0.Contact>;
  ImportCSV(path: string, mapping: service$0.CSVMapping): PromiseLike<service$0.ImportResult>;
  ImportFromAccount(acc: string): PromiseLike<service$0.ImportResult>;
  List(f: model$0.ContactFilter): PromiseLike<ContactPage>;
  Lists(): PromiseLike<model$0.ContactList[] | null>;
  Merge(keepID: string, mergeID: string): PromiseLike<model$0.Contact>;
  Save(ct: model$0.Contact): PromiseLike<model$0.Contact>;
  SaveList(l: model$0.ContactList): PromiseLike<model$0.ContactList>;
  SetListMembers(listID: string, add: string[] | null, remove: string[] | null): PromiseLike<void>;
  SplitIdentity(contactID: string, acc: string, peer: string): PromiseLike<model$0.Contact>;
  SuggestMerges(): PromiseLike<string[][] | null>;
  SyncAll(): PromiseLike<number>;
}

/** Campañas de envío. */
export interface CampaignsSvc {
  Cancel(id: string): PromiseLike<void>;
  Delete(id: string): PromiseLike<void>;
  Get(id: string): PromiseLike<model$0.Campaign>;
  GetPacing(acc: string): PromiseLike<model$0.PacingSettings>;
  Launch(id: string): PromiseLike<void>;
  List(): PromiseLike<model$0.Campaign[] | null>;
  Pause(id: string): PromiseLike<void>;
  Queue(id: string, states: string[] | null, limit: number): PromiseLike<model$0.QueueItem[] | null>;
  Resume(id: string): PromiseLike<void>;
  Review(cp: model$0.Campaign): PromiseLike<service$0.CampaignReview>;
  Save(cp: model$0.Campaign): PromiseLike<model$0.Campaign>;
  SetPacing(acc: string, p: model$0.PacingSettings): PromiseLike<void>;
}

/** Mensajes programados y series. */
export interface ScheduledSvc {
  Cancel(id: string): PromiseLike<void>;
  CancelSeries(seriesID: string): PromiseLike<void>;
  Delete(id: string): PromiseLike<void>;
  DeleteSeries(seriesID: string): PromiseLike<void>;
  List($from: number, to: number): PromiseLike<model$0.Scheduled[] | null>;
  ListForChat(accountID: string, chatNativeID: string, includeDone: boolean): PromiseLike<model$0.Scheduled[] | null>;
  PreviewRecurrence(r: model$0.Recurrence, first: number, n: number): PromiseLike<number[] | null>;
  Save(it: model$0.Scheduled): PromiseLike<model$0.Scheduled>;
  SendNow(id: string): PromiseLike<void>;
  Update(it: model$0.Scheduled): PromiseLike<model$0.Scheduled>;
  UpdateSeries(seriesID: string, it: model$0.Scheduled): PromiseLike<model$0.Scheduled>;
}

/** Estadísticas de conversaciones. */
export interface StatsSvc {
  Chat(chatKey: string, rng: stats$0.Range): PromiseLike<stats$0.ContactConversationStats>;
  Conversations(accountIDs: string[] | null, rng: stats$0.Range): PromiseLike<stats$0.ConversationOverview>;
  Groups(accountIDs: string[] | null, rng: stats$0.Range): PromiseLike<stats$0.GroupsOverview>;
}

/** Destinos de webhook. */
export interface DestinationsSvc {
  Delete(id: string): PromiseLike<void>;
  List(): PromiseLike<model$0.Destination[] | null>;
  ProtocolDoc(): PromiseLike<string>;
  Save(dst: model$0.Destination): PromiseLike<model$0.Destination>;
  States(): PromiseLike<model$0.DestinationState[] | null>;
  Test(id: string): PromiseLike<void>;
}

/** Ajustes del servidor (OpenDataDir/OpenLogsDir son del escritorio). */
export interface SettingsSvc {
  DataDir(): PromiseLike<string>;
  Get(): PromiseLike<service$0.AppSettings>;
  Save(v: service$0.AppSettings): PromiseLike<void>;
  Version(): PromiseLike<string>;
}

/** Sincronización del historial. */
export interface HistorySvc {
  LoadMore(accountID: string, chatNativeID: string): PromiseLike<void>;
  Reset(accountID: string): PromiseLike<void>;
  Start(accountID: string): PromiseLike<void>;
  Status(accountID: string): PromiseLike<service$0.HistoryStatus>;
  StatusAll(): PromiseLike<service$0.HistoryStatus[] | null>;
  Stop(accountID: string): PromiseLike<void>;
}

/** Foco de la ventana para decidir los avisos (los avisos de Windows son del escritorio). */
export interface NotificationsSvc {
  SetFocus(chatKey: string, windowFocused: boolean): PromiseLike<void>;
}

