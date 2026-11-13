// Catálogo `api`: envoltorios finos del servidor (docs/14). Cada método conserva el nombre y los parámetros del
// servicio Go (11 §1) y ahora viaja por HTTP (`POST /api/v1/rpc/{Servicio}/{Método}`). Solo lo que depende de la
// máquina del usuario (diálogos de archivos, notificaciones de Windows, abrir carpetas) pasa por Wails en modo local.
// Ante un error: toast de error en español y se relanza. Las listas que Go devuelve como `null` llegan como `[]`.
import type * as T_ from "./svcTypes";
import { useUI } from "@/store/ui";
import { isRemote, useConnection } from "./connection";
import { ApiError } from "./http";
import { canUseDesktop, desktopCall, rpc } from "./transport";
import type { CapSet, NotifyStatus, PickedFile } from "./types";
import type { UploadedFile } from "../../bindings/dconnector/internal/adapter/ui/wails";
import { UPLOAD_SCHEME, webFileInfo, webPickFiles, webSaveMediaAs, webSaveTemp } from "./webfiles";

export * from "./types";
export { onEvent, onEvents, EV, EVENT_NAMES } from "./events";
export { mediaUrl } from "./media";
export { ApiError } from "./http";

/** Texto legible de un error del servidor (o de Wails). */
export function errorMessage(e: unknown): string {
  let msg = "";
  if (typeof e === "string") msg = e;
  else if (e instanceof Error) msg = e.message;
  else if (e && typeof e === "object" && "message" in e) msg = String((e as { message: unknown }).message);
  msg = msg.trim();
  if (msg.startsWith("{")) {
    try {
      const j = JSON.parse(msg) as { message?: string; cause?: { message?: string } };
      msg = (j.cause?.message || j.message || msg).trim();
    } catch {
      /* no era JSON */
    }
  }
  return msg || "Ocurrió un error inesperado";
}

interface WrapOptions {
  /** No muestra el aviso de error (sí relanza). */
  silent?: boolean;
}

type Fn<A extends unknown[], R> = (...args: A) => PromiseLike<R>;

/** Sin conexión o sin sesión el aviso es la pantalla/banda de conexión, no un toast por llamada. */
function quiet(e: unknown): boolean {
  if (!(e instanceof ApiError)) return false;
  if (e.code === "unauthorized") return true;
  return e.code === "network" && useConnection.getState().link !== "connected";
}

/** El mismo aviso de error no se repite durante unos segundos (varias llamadas fallan a la vez por la misma causa). */
const TOAST_DEDUPE_MS = 6000;
let lastToast = { msg: "", at: 0 };

function report(e: unknown, opts?: WrapOptions): never {
  if (!opts?.silent && !quiet(e)) {
    const msg = errorMessage(e);
    const now = Date.now();
    if (msg !== lastToast.msg || now - lastToast.at > TOAST_DEDUPE_MS) {
      lastToast = { msg, at: now };
      useUI.getState().toast("error", msg);
    }
  }
  throw e;
}

/** Solo para pruebas: olvida el último aviso. */
export function resetToastDedupe(): void {
  lastToast = { msg: "", at: 0 };
}

function wrap<A extends unknown[], R>(fn: Fn<A, R>, opts?: WrapOptions): (...args: A) => Promise<R> {
  return async (...args: A) => {
    try {
      return await fn(...args);
    } catch (e) {
      return report(e, opts);
    }
  };
}

/** Igual que `wrap`, pero convierte `null` en `[]`. */
function wrapList<A extends unknown[], R>(
  fn: Fn<A, R[] | null>,
  opts?: WrapOptions,
): (...args: A) => Promise<R[]> {
  const inner = wrap(fn, opts);
  return async (...args: A) => (await inner(...args)) ?? [];
}

type Remote<T> = { [K in keyof T]: T[K] extends (...a: infer P) => PromiseLike<infer R> ? (...a: P) => Promise<R> : never };

/** Servicio remoto: cada propiedad es una función que hace `rpc(servicio, método, args)`. Los tipos vienen de `svcTypes.ts` (no de los enlaces de Wails). */
function svc<T>(name: string): Remote<T> {
  const cache = new Map<string, unknown>();
  return new Proxy({} as Remote<T>, {
    get(_t, method) {
      if (typeof method !== "string") return undefined;
      let f = cache.get(method);
      if (!f) {
        f = (...args: unknown[]) => rpc(name, method, args);
        cache.set(method, f);
      }
      return f;
    },
  });
}

const A = svc<T_.AccountsSvc>("Accounts");
const I = svc<T_.InboxSvc>("Inbox");
const M = svc<T_.MessagingSvc>("Messaging");
const S = svc<T_.StatusesSvc>("Statuses");
const C = svc<T_.ContactsSvc>("Contacts");
const K = svc<T_.CampaignsSvc>("Campaigns");
const P = svc<T_.ScheduledSvc>("Scheduled");
const T = svc<T_.StatsSvc>("Stats");
const D = svc<T_.DestinationsSvc>("Destinations");
const G = svc<T_.SettingsSvc>("Settings");
const H = svc<T_.HistorySvc>("History");
const N = svc<T_.NotificationsSvc>("Notifications");

const DESKTOP_ONLY = "Solo está disponible en la aplicación de escritorio con el servidor local.";

/** Escritorio con servidor local: aquí los archivos y las carpetas son los de esta máquina. */
const localDesktop = () => canUseDesktop() && !isRemote();

/**
 * Escritorio con servidor REMOTO: el diálogo es nativo (rutas de este equipo), pero el servidor no puede leer esas
 * rutas; cada archivo se sube con `Files.Upload` y la interfaz usa `upload://<id>` donde iría la ruta.
 */
async function pickAndUpload(kind: string): Promise<PickedFile[]> {
  const picked = (await desktopCall<PickedFile[] | null>("Files", "PickFiles", [kind])) ?? [];
  const out: PickedFile[] = [];
  for (const f of picked) {
    const up = await desktopCall<UploadedFile>("Files", "Upload", [f.path]);
    out.push({ path: `${UPLOAD_SCHEME}${up.id}`, name: up.name || f.name, mime: up.mime || f.mime, size: up.size ?? f.size });
  }
  return out;
}

/** «Guardar como» de un medio: en el escritorio, diálogo nativo + descarga desde el servidor; en la web, el navegador. */
function saveMedia(accountID: string, mediaID: string, suggestedName?: string): Promise<string> {
  if (!canUseDesktop()) return webSaveMediaAs(accountID, mediaID);
  const url = `/media/${encodeURIComponent(accountID)}/${encodeURIComponent(mediaID)}`;
  return desktopCall<string>("Files", "SaveFileAs", [suggestedName || mediaID, url]);
}

function desktopOnly<A extends unknown[], R>(service: string, method: string): (...args: A) => Promise<R> {
  return (...args: A) =>
    localDesktop() ? desktopCall<R>(service, method, args) : Promise.reject(new ApiError("not_available", DESKTOP_ONLY));
}

const NO_DESKTOP_NOTIFY: NotifyStatus = {
  state: "unsupported",
  reason: "Las notificaciones de Windows solo están disponibles en la aplicación de escritorio.",
  listening: false,
  asked: true,
  quiet: "unknown",
  quietCause: "",
  quietReason: "",
  suppressed: false,
  lastDecision: "",
  lastDecisionAt: 0,
  testMessage: "",
  testShown: false,
  testVisible: false,
  testSound: false,
};

/** Notificaciones de Windows: las resuelve el cliente de escritorio, esté el servidor donde esté. */
function notifyDesktop<A extends unknown[]>(method: string): (...args: A) => Promise<NotifyStatus> {
  return (...args: A) =>
    canUseDesktop() ? desktopCall<NotifyStatus>("Notifications", method, args) : Promise.resolve(NO_DESKTOP_NOTIFY);
}

function notifyVoid(method: string): () => Promise<void> {
  return () => (canUseDesktop() ? desktopCall<void>("Notifications", method, []) : Promise.resolve());
}

export const api = {
  accounts: {
    List: wrapList(A.List),
    Create: wrap(A.Create),
    Update: wrap(A.Update),
    SetConfig: wrap(A.SetConfig),
    Connect: wrap(A.Connect),
    Disconnect: wrap(A.Disconnect),
    BeginPairing: wrap(A.BeginPairing),
    SubmitPairing: wrap(A.SubmitPairing),
    Logout: wrap(A.Logout),
    Delete: wrap(A.Delete),
    Reorder: wrap(A.Reorder),
    Capabilities: wrap(A.Capabilities) as (chatKey: string) => Promise<CapSet>,
  },
  inbox: {
    ListChats: wrapList(I.ListChats),
    GetChat: wrap(I.GetChat),
    Messages: wrap(I.Messages),
    Search: wrapList(I.Search),
    UnreadTotals: wrap(I.UnreadTotals),
    /** Pide más historial al canal; sin aviso si el canal no lo admite. */
    LoadOlder: wrap(I.LoadOlder, { silent: true }),
    Archive: wrap(I.Archive),
    Pin: wrap(I.Pin),
    Mute: wrap(I.Mute),
    /** «Nuevo chat»: devuelve o crea el chat vacío con una persona (sin enviar nada). */
    OpenChat: wrap(I.OpenChat),
  },
  messaging: {
    Send: wrap(M.Send),
    React: wrap(M.React),
    Edit: wrap(M.Edit),
    Delete: wrap(M.Delete),
    Forward: wrap(M.Forward),
    VotePoll: wrap(M.VotePoll),
    MarkRead: wrap(M.MarkRead),
    /** El «escribiendo…» nunca molesta con un aviso. */
    SetTyping: wrap(M.SetTyping, { silent: true }),
    /** Escritorio: `Files.SaveFileAs` (diálogo nativo + descarga del servidor); web: descarga y guarda con el navegador. "" si se cancela. */
    SaveMediaAs: wrap<[accountID: string, mediaID: string, suggestedName?: string], string>(saveMedia),
  },
  statuses: {
    Authors: wrapList(S.Authors),
    ByAuthor: wrapList(S.ByAuthor),
    Mine: wrapList(S.Mine),
    Post: wrap(S.Post),
    Delete: wrap(S.Delete),
    MarkViewed: wrap(S.MarkViewed, { silent: true }),
    React: wrap(S.React),
    Viewers: wrapList(S.Viewers),
    Stats: wrap(S.Stats),
  },
  contacts: {
    List: wrap(C.List),
    Get: wrap(C.Get),
    Save: wrap(C.Save),
    Delete: wrap(C.Delete),
    FromChat: wrap(C.FromChat),
    Merge: wrap(C.Merge),
    SplitIdentity: wrap(C.SplitIdentity),
    ImportFromAccount: wrap(C.ImportFromAccount),
    SuggestMerges: wrapList(C.SuggestMerges),
    Lists: wrapList(C.Lists),
    SaveList: wrap(C.SaveList),
    DeleteList: wrap(C.DeleteList),
    SetListMembers: wrap(C.SetListMembers),
    ImportCSV: wrap(C.ImportCSV),
    /** Crea los contactos automáticos que falten; devuelve cuántos nuevos. */
    SyncAll: wrap(C.SyncAll),
  },
  campaigns: {
    List: wrapList(K.List),
    Get: wrap(K.Get),
    Save: wrap(K.Save),
    Review: wrap(K.Review),
    Launch: wrap(K.Launch),
    Pause: wrap(K.Pause),
    Resume: wrap(K.Resume),
    Cancel: wrap(K.Cancel),
    Delete: wrap(K.Delete),
    Queue: wrapList(K.Queue),
    GetPacing: wrap(K.GetPacing),
    SetPacing: wrap(K.SetPacing),
  },
  scheduled: {
    List: wrapList(P.List),
    Save: wrap(P.Save),
    Cancel: wrap(P.Cancel),
    Delete: wrap(P.Delete),
    SendNow: wrap(P.SendNow),
    /** Pendientes de un chat y, con `includeDone`, los cerrados de los últimos 7 días. Sin aviso: se consulta en segundo plano. */
    ListForChat: wrapList(P.ListForChat, { silent: true }),
    Update: wrap(P.Update),
    UpdateSeries: wrap(P.UpdateSeries),
    CancelSeries: wrap(P.CancelSeries),
    DeleteSeries: wrap(P.DeleteSeries),
    /** Próximas fechas de una regla; si la regla no vale, falla con el motivo (sin aviso: lo muestra el editor). */
    PreviewRecurrence: wrapList(P.PreviewRecurrence, { silent: true }),
    /** Variantes sin aviso de error: el diálogo «Programar» muestra el motivo junto a los campos. */
    SaveQuiet: wrap(P.Save, { silent: true }),
    UpdateQuiet: wrap(P.Update, { silent: true }),
    UpdateSeriesQuiet: wrap(P.UpdateSeries, { silent: true }),
  },
  stats: {
    Conversations: wrap(T.Conversations),
    Chat: wrap(T.Chat),
    Groups: wrap(T.Groups),
  },
  destinations: {
    List: wrapList(D.List),
    Save: wrap(D.Save),
    Delete: wrap(D.Delete),
    States: wrapList(D.States),
    Test: wrap(D.Test),
    /** Documento completo del protocolo v1 (Markdown), embebido en el programa. */
    ProtocolDoc: wrap(D.ProtocolDoc),
  },
  settings: {
    Get: wrap(G.Get),
    Save: wrap(G.Save),
    DataDir: wrap(G.DataDir),
    /** Solo escritorio con servidor local; en remoto se rechazan (la interfaz oculta los botones). */
    OpenDataDir: wrap(desktopOnly<[], void>("Settings", "OpenDataDir")),
    OpenLogsDir: wrap(desktopOnly<[], void>("Settings", "OpenLogsDir")),
    Version: wrap(G.Version),
  },
  history: {
    Start: wrap(H.Start),
    Stop: wrap(H.Stop),
    Reset: wrap(H.Reset),
    Status: wrap(H.Status),
    StatusAll: wrapList(H.StatusAll),
    /** Pide más mensajes de un solo chat; el aviso de error solo lo da la interfaz del chat. */
    LoadMore: wrap(H.LoadMore, { silent: true }),
  },
  notifications: {
    /** Sin aviso de error: lo lee la sección de Ajustes y el aviso del primer arranque, que tienen su propio texto. */
    Status: wrap(notifyDesktop("Status"), { silent: true }),
    RequestPermission: wrap(notifyDesktop("RequestPermission")),
    /** Notificación de prueba y sonido; `testMessage` dice con honestidad qué pasó (No molestar, bloqueado…). */
    SendTest: wrap(notifyDesktop("SendTest")),
    OpenSystemSettings: wrap(notifyVoid("OpenSystemSettings")),
    /** Anota que ya se comprobó el permiso en el primer arranque. */
    MarkAsked: wrap(notifyVoid("MarkAsked"), { silent: true }),
    /** Qué chat está abierto y si la ventana tiene el foco (por RPC: el servidor decide con todas las conexiones). */
    SetFocus: wrap(N.SetFocus, { silent: true }),
  },
  files: {
    /** Local: rutas de este equipo. Remoto con escritorio: diálogo nativo + subida. Web: `<input type=file>` + subida. */
    PickFiles: wrapList<[kind: string], PickedFile>((kind) => {
      if (!canUseDesktop()) return webPickFiles(kind);
      return localDesktop() ? desktopCall<PickedFile[] | null>("Files", "PickFiles", [kind]) : pickAndUpload(kind);
    }),
    SaveTemp: wrap<[name: string, b64: string], PickedFile>((name, b64) =>
      localDesktop() ? desktopCall<PickedFile>("Files", "SaveTemp", [name, b64]) : webSaveTemp(name, b64),
    ),
    Info: wrap<[path: string], PickedFile>((path) =>
      localDesktop() ? desktopCall<PickedFile>("Files", "Info", [path]) : webFileInfo(path),
    ),
  },
};

export type Api = typeof api;
