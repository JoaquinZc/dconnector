// Responder a un estado: abre el chat directo con su autor y deja el estado como cita del compositor.
import type { Message, Status } from "@/api/types";
import { useChat } from "@/store/chat";
import { useUI } from "@/store/ui";

/** Chat directo con el autor del estado. */
export function chatKeyForStatus(s: Pick<Status, "accountId" | "authorNativeId">): string {
  return `${s.accountId}|${s.authorNativeId}`;
}

/** El estado como si fuera un mensaje, para usarlo de cita (`useChat.setReplyTo`). */
export function statusAsQuote(s: Status): Message {
  const preview = s.text?.trim() || (String(s.kind) === "video" ? "🎥 Vídeo" : String(s.kind) === "image" ? "📷 Foto" : "Estado");
  return {
    key: `${s.accountId}|${s.authorNativeId}|${s.nativeId}`,
    chatKey: chatKeyForStatus(s),
    accountId: s.accountId,
    channel: s.channel,
    nativeId: s.nativeId,
    chatNativeId: s.authorNativeId,
    senderNativeId: s.authorNativeId,
    senderName: s.authorName,
    fromMe: false,
    timestamp: s.postedAt,
    kind: s.kind,
    text: preview,
    media: s.media,
    forwarded: false,
    editedAt: 0,
    deletedAt: 0,
    delivery: "read",
    fromStatusId: s.nativeId,
  } as unknown as Message;
}

const WAIT_MS = 6000;

/**
 * Pide abrir el chat del autor y, en cuanto `ChatsPage` lo abre, pone el estado como cita.
 * Devuelve la función que cancela la espera.
 */
export function replyToStatus(s: Status): () => void {
  const key = chatKeyForStatus(s);
  const quote = statusAsQuote(s);
  let done = false;
  let off: () => void = () => undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;

  const finish = () => {
    done = true;
    off();
    if (timer) clearTimeout(timer);
  };
  const apply = () => {
    finish(); // antes de tocar el store, que vuelve a avisar a los suscritos
    useChat.getState().setReplyTo(quote);
  };

  useUI.getState().openChat(key);
  if (useChat.getState().chatKey === key) {
    // Ya estaba abierto: no habrá cambio que esperar.
    apply();
    return finish;
  }
  off = useChat.subscribe((state) => {
    if (!done && state.chatKey === key) apply();
  });
  timer = setTimeout(finish, WAIT_MS);
  return finish;
}
