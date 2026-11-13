// Compositor: texto multilínea, emoji, adjuntos, arrastrar y soltar, pegar imagen, nota de voz,
// barra de respuesta o edición y aviso de «escribiendo…».
import { useCallback, useEffect, useImperativeHandle, useReducer, useRef, useState } from "react";
import { CalendarClock, CornerUpLeft, Paperclip, Pencil, Send, Smile, X } from "lucide-react";
import { api } from "@/api";
import { useChat } from "@/store/chat";
import { useUI } from "@/store/ui";
import type { Chat, Message, Scheduled } from "../../api/core/model/models.js";
import {
  attachOptions,
  canSendAnything,
  type CapsLike,
} from "./chatCaps";
import { messagePreview } from "./chatFormat";
import {
  composerReducer,
  exceedsLimit,
  initialComposer,
  makeAttachment,
  outgoingForAttachment,
  outgoingText,
  refOf,
  shouldPingTyping,
  type Attachment,
} from "./composerReducer";
import AttachmentPreview from "./AttachmentPreview";
import ChatScheduledPanel from "./ChatScheduledPanel";
import ScheduleDialog from "./ScheduleDialog";
import { useChatScheduled } from "./useChatScheduled";
import { ContactForm, LocationForm, PollForm, type Payload } from "./AttachForms";
import LazyEmojiPicker from "./LazyEmojiPicker";
import { IconButton, PopMenu } from "./parts";
import VoiceRecorder, { bytesToBase64 } from "./VoiceRecorder";

export interface ComposerHandle {
  /** Archivos soltados o pegados desde el navegador (se guardan con SaveTemp). */
  addBrowserFiles: (files: File[]) => void;
}

const MAX_BROWSER_FILE = 50 * 1024 * 1024;
const draftKey = (chatKey: string) => `draft:chats:${chatKey}`;

function readDraft(chatKey: string): string {
  try {
    return localStorage.getItem(draftKey(chatKey)) ?? "";
  } catch {
    return "";
  }
}
function writeDraft(chatKey: string, text: string) {
  try {
    if (text) localStorage.setItem(draftKey(chatKey), text);
    else localStorage.removeItem(draftKey(chatKey));
  } catch {
    /* sin almacenamiento */
  }
}

async function fileToBase64(file: File): Promise<string> {
  return bytesToBase64(new Uint8Array(await file.arrayBuffer()));
}

function senderLabel(m: Message): string {
  return m.fromMe ? "Tú" : m.senderName || "Mensaje";
}

export default function Composer({
  chat,
  caps,
  handleRef,
}: {
  chat: Chat;
  caps: CapsLike | null | undefined;
  handleRef?: React.Ref<ComposerHandle>;
}) {
  const [state, dispatch] = useReducer(composerReducer, initialComposer);
  const replyTo = useChat((s) => s.replyTo);
  const editing = useChat((s) => s.editing);
  const setReplyTo = useChat((s) => s.setReplyTo);
  const setEditing = useChat((s) => s.setEditing);
  const addMessages = useChat((s) => s.addMessages);
  const toast = useUI((s) => s.toast);

  const taRef = useRef<HTMLTextAreaElement>(null);
  const lastTypingRef = useRef(0);
  const typingOnRef = useRef(false);
  const stashRef = useRef("");
  const [emojiOpen, setEmojiOpen] = useState(false);
  const [attachMenu, setAttachMenu] = useState<{ x: number; y: number } | null>(null);
  const [form, setForm] = useState<"location" | "contact" | "poll" | null>(null);
  const [recording, setRecording] = useState(false);
  // «Programar»: panel de los programados del chat y diálogo (nuevo o edición).
  const [panelOpen, setPanelOpen] = useState(false);
  const [schedule, setSchedule] = useState<{ item: Scheduled | null; fromComposer: boolean; reopenPanel: boolean } | null>(null);
  const chatKey = chat.key;
  const scheduled = useChatScheduled(chat.accountId, chat.nativeId, chatKey);
  const opts = attachOptions(caps);

  // --- borrador por chat
  useEffect(() => {
    dispatch({ type: "reset" });
    dispatch({ type: "load", text: readDraft(chatKey) });
    taRef.current?.focus(); // al abrir un chat (también uno recién creado con «Nuevo chat») se puede escribir ya
    setEmojiOpen(false);
    setPanelOpen(false);
    setSchedule(null);
    return () => stopTyping();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [chatKey]);

  // --- edición: carga el texto del mensaje; al cancelar vuelve el borrador
  useEffect(() => {
    if (editing) {
      stashRef.current = state.text;
      dispatch({ type: "load", text: editing.text ?? "" });
      taRef.current?.focus();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [editing?.key]);

  useEffect(() => {
    if (replyTo) taRef.current?.focus();
  }, [replyTo?.key]);

  // --- altura automática del cuadro
  useEffect(() => {
    const el = taRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 140)}px`;
  }, [state.text]);

  function stopTyping() {
    if (typingOnRef.current) {
      typingOnRef.current = false;
      lastTypingRef.current = 0;
      void api.messaging.SetTyping(chatKey, false).catch(() => undefined);
    }
  }

  const onChange = (text: string) => {
    dispatch({ type: "text", text });
    if (!editing) writeDraft(chatKey, text);
    if (!text) return stopTyping();
    if (caps?.typing === false) return;
    const now = Date.now();
    if (shouldPingTyping(lastTypingRef.current, now)) {
      lastTypingRef.current = now;
      typingOnRef.current = true;
      void api.messaging.SetTyping(chatKey, true).catch(() => undefined);
    }
  };

  const addFiles = useCallback(
    (files: Attachment[]) => {
      const ok = files.filter((a) => {
        if (exceedsLimit(a, caps)) {
          toast("error", `«${a.name}» supera el tamaño máximo que admite este canal.`);
          return false;
        }
        return true;
      });
      if (ok.length) dispatch({ type: "addFiles", files: ok });
    },
    [caps, toast],
  );

  const addBrowserFiles = useCallback(
    async (files: File[]) => {
      const made: Attachment[] = [];
      for (const f of files) {
        if (f.size > MAX_BROWSER_FILE) {
          toast("error", `«${f.name || "archivo"}» es demasiado grande para soltarlo aquí (máx. 50 MB).`);
          continue;
        }
        try {
          const name = f.name || `imagen-${Date.now()}.${(f.type.split("/")[1] || "png").split("+")[0]}`;
          const picked = await api.files.SaveTemp(name, await fileToBase64(f));
          made.push(makeAttachment({ ...picked, mime: picked.mime || f.type }));
        } catch {
          /* el aviso ya salió */
        }
      }
      addFiles(made);
    },
    [addFiles, toast],
  );

  useImperativeHandle(handleRef, () => ({ addBrowserFiles: (f) => void addBrowserFiles(f) }), [addBrowserFiles]);

  const pick = async (kind: "media" | "document" | "sticker") => {
    try {
      const files = await api.files.PickFiles(kind);
      addFiles(files.map((f) => makeAttachment(f, kind === "sticker" ? "sticker" : undefined)));
    } catch {
      /* el aviso ya salió */
    }
  };

  const afterSend = (m: Message | null | undefined) => {
    if (m && m.key) addMessages([m]);
  };

  const sendText = async () => {
    const text = state.text.trim();
    if (!text || state.sending) return;
    dispatch({ type: "sending", value: true });
    try {
      if (editing) {
        if (text !== (editing.text ?? "").trim()) await api.messaging.Edit(editing.key, text);
        setEditing(null);
        dispatch({ type: "load", text: stashRef.current });
      } else {
        const reply = replyTo;
        const m = await api.messaging.Send(chatKey, outgoingText(text, reply));
        afterSend(m);
        setReplyTo(null);
        dispatch({ type: "text", text: "" });
        writeDraft(chatKey, "");
      }
      stopTyping();
    } catch {
      /* se conserva el texto para reintentar; el aviso ya salió */
    } finally {
      dispatch({ type: "sending", value: false });
    }
  };

  const sendAttachments = async () => {
    if (state.sending || state.attachments.length === 0) return;
    dispatch({ type: "sending", value: true });
    let reply = replyTo;
    try {
      for (const a of state.attachments) {
        const m = await api.messaging.Send(chatKey, outgoingForAttachment(a, reply));
        afterSend(m);
        reply = null;
        dispatch({ type: "removeFile", id: a.id });
      }
      setReplyTo(null);
    } catch {
      /* los que faltan quedan en la vista previa */
    } finally {
      dispatch({ type: "sending", value: false });
    }
  };

  const sendPayload = async (p: Payload) => {
    setForm(null);
    try {
      const m = await api.messaging.Send(chatKey, { ...p, replyTo: replyTo ? refOf(replyTo) : null });
      afterSend(m);
      setReplyTo(null);
    } catch {
      /* el aviso ya salió */
    }
  };

  const sendVoice = async (b64: string) => {
    const file = await api.files.SaveTemp(`nota-de-voz-${Date.now()}.ogg`, b64);
    const m = await api.messaging.Send(chatKey, {
      kind: "voice" as never,
      text: "",
      filePath: file.path,
      fileName: file.name,
      mime: "audio/ogg; codecs=opus",
      replyTo: replyTo ? refOf(replyTo) : null,
    });
    afterSend(m);
    setReplyTo(null);
  };

  const insertEmoji = (emoji: string) => {
    const el = taRef.current;
    const at = el ? el.selectionStart : state.text.length;
    dispatch({ type: "insert", at, value: emoji });
    onChange(state.text.slice(0, at) + emoji + state.text.slice(at));
    requestAnimationFrame(() => {
      el?.focus();
      el?.setSelectionRange(at + emoji.length, at + emoji.length);
    });
  };

  const openSchedule = (item: Scheduled | null, reopenPanel = false) => {
    setPanelOpen(false);
    setEmojiOpen(false);
    // Solo se programa «desde el compositor» lo que haya escrito o adjuntado en él.
    setSchedule({ item, fromComposer: !item && (state.text.trim() !== "" || state.attachments.length > 0), reopenPanel });
  };

  const closeSchedule = () => {
    if (schedule?.reopenPanel) setPanelOpen(true);
    setSchedule(null);
  };

  const onScheduled = () => {
    // El borrador del compositor se vacía solo si lo programado salió de él.
    if (schedule?.fromComposer) {
      dispatch({ type: "text", text: "" });
      dispatch({ type: "clearFiles" });
      writeDraft(chatKey, "");
      stopTyping();
    }
    void scheduled.reload();
  };

  const cancelEdit = () => {
    setEditing(null);
    dispatch({ type: "load", text: stashRef.current });
  };

  // --- chat de solo lectura
  if (chat.readOnly || !canSendAnything(caps)) {
    return (
      <div className="flex h-16 shrink-0 items-center justify-center border-t border-[var(--border)] bg-[var(--panel-2)] px-4 text-[13.5px] text-[var(--muted)]">
        Solo los administradores pueden enviar mensajes
      </div>
    );
  }

  const canText = caps?.sendText !== false;
  const hasText = state.text.trim() !== "";
  const showMic = !hasText && !editing && !!caps?.sendVoice;
  const anyAttach = opts.media || opts.document || opts.sticker || opts.location || opts.contact || opts.poll;

  const menuItems = [
    opts.media && { label: "Fotos y vídeos", onClick: () => void pick("media") },
    opts.document && { label: "Documento", onClick: () => void pick("document") },
    opts.sticker && { label: "Sticker", onClick: () => void pick("sticker") },
    opts.location && { label: "Ubicación", onClick: () => setForm("location") },
    opts.contact && { label: "Contacto", onClick: () => setForm("contact") },
    opts.poll && { label: "Encuesta", onClick: () => setForm("poll") },
  ].filter(Boolean) as { label: string; onClick: () => void }[];

  return (
    <div className="relative shrink-0 border-t border-[var(--border)] bg-[var(--panel-2)]">
      {(replyTo || editing) && (
        <div className="flex items-center gap-2 border-b border-[var(--border)] px-3 py-2">
          <div className="min-w-0 flex-1 rounded-md border-l-4 border-[var(--accent)] bg-black/5 px-2.5 py-1 dark:bg-white/5">
            <div className="flex items-center gap-1 text-[12.5px] font-semibold text-[var(--accent)]">
              {editing ? <Pencil size={12} /> : <CornerUpLeft size={12} />}
              {editing ? "Editando mensaje" : senderLabel(replyTo!)}
            </div>
            <div className="truncate text-[13px] text-[var(--muted)]" title={messagePreview((editing ?? replyTo)!)}>
              {messagePreview((editing ?? replyTo)!)}
            </div>
          </div>
          <IconButton title="Cancelar" onClick={() => (editing ? cancelEdit() : setReplyTo(null))}>
            <X size={18} />
          </IconButton>
        </div>
      )}

      <div className="flex items-end gap-1 px-3 py-2">
        {!recording && (
          <>
            <IconButton title="Emojis" active={emojiOpen} onClick={() => setEmojiOpen((v) => !v)}>
              <Smile size={22} />
            </IconButton>
            {anyAttach && !editing && (
              <IconButton
                title="Adjuntar"
                active={!!attachMenu}
                onClick={(e) => {
                  const r = e.currentTarget.getBoundingClientRect();
                  setAttachMenu(attachMenu ? null : { x: r.left, y: r.top - 8 - menuItems.length * 40 });
                }}
              >
                <Paperclip size={22} />
              </IconButton>
            )}
            {!editing && (
              <>
                <IconButton title="Programar mensaje" active={!!schedule} onClick={() => openSchedule(null)}>
                  <CalendarClock size={22} />
                </IconButton>
                {(scheduled.pending > 0 || scheduled.failed > 0) && (
                  <button
                    type="button"
                    aria-label={`Programados de este chat: ${scheduled.pending} pendientes${scheduled.failed ? `, ${scheduled.failed} fallidos` : ""}`}
                    title="Programados de este chat"
                    aria-haspopup="dialog"
                    aria-expanded={panelOpen}
                    data-testid="scheduled-badge"
                    onClick={() => {
                      setEmojiOpen(false);
                      void scheduled.reload();
                      setPanelOpen((v) => !v);
                    }}
                    className={`-ml-0.5 mr-0.5 flex h-6 min-w-6 shrink-0 items-center justify-center rounded-full px-1.5 text-[12px] font-semibold focus-visible:outline-2 focus-visible:outline-[var(--accent)] ${
                      scheduled.failed > 0 ? "bg-[var(--danger)]/15 text-[var(--danger)]" : "bg-[var(--accent)]/15 text-[var(--accent)]"
                    }`}
                  >
                    {scheduled.pending > 0 ? scheduled.pending : "!"}
                  </button>
                )}
              </>
            )}
            <textarea
              ref={taRef}
              rows={1}
              value={state.text}
              disabled={!canText}
              placeholder={canText ? "Escribe un mensaje" : "Solo se admiten adjuntos"}
              onChange={(e) => onChange(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
                  e.preventDefault();
                  void sendText();
                } else if (e.key === "Escape" && editing) {
                  cancelEdit();
                }
              }}
              onPaste={(e) => {
                const files = Array.from(e.clipboardData?.files ?? []);
                if (files.length) {
                  e.preventDefault();
                  void addBrowserFiles(files);
                }
              }}
              onBlur={stopTyping}
              className="mx-1 max-h-[140px] min-h-[40px] flex-1 resize-none rounded-lg bg-[var(--panel)] px-3 py-2.5 text-[14.5px] leading-5 outline-none"
            />
          </>
        )}
        {showMic || recording ? (
          <VoiceRecorder disabled={state.sending} onRecordingChange={setRecording} onSend={sendVoice} />
        ) : (
          <button
            type="button"
            title={editing ? "Guardar cambios" : "Enviar"}
            disabled={!hasText || state.sending}
            onClick={() => void sendText()}
            className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full text-[var(--muted)] enabled:text-[var(--accent)] disabled:opacity-40"
          >
            <Send size={22} />
          </button>
        )}
      </div>

      {emojiOpen && (
        <>
          <div className="fixed inset-0 z-30" onMouseDown={() => setEmojiOpen(false)} />
          <div className="absolute bottom-full left-2 z-40 mb-1 overflow-hidden rounded-xl shadow-2xl">
            <LazyEmojiPicker onPick={insertEmoji} />
          </div>
        </>
      )}
      {panelOpen && (
        <ChatScheduledPanel
          items={scheduled.items}
          loaded={scheduled.loaded}
          onClose={() => setPanelOpen(false)}
          onNew={() => openSchedule(null, true)}
          onEdit={(s) => openSchedule(s, true)}
          onChanged={() => void scheduled.reload()}
        />
      )}
      {schedule && (
        <ScheduleDialog
          key={schedule.item?.id ?? "new"}
          target={{ accountId: chat.accountId, chatNativeId: chat.nativeId, chatTitle: chat.title }}
          initialText={schedule.fromComposer ? state.text : ""}
          files={schedule.fromComposer ? state.attachments : []}
          item={schedule.item}
          maxTextLen={(caps as { maxTextLen?: number } | null | undefined)?.maxTextLen}
          onClose={closeSchedule}
          onSaved={onScheduled}
          onChanged={() => void scheduled.reload()}
        />
      )}
      {attachMenu && (
        <PopMenu
          at={attachMenu}
          onClose={() => setAttachMenu(null)}
          items={menuItems}
        />
      )}
      {form === "location" && <LocationForm onClose={() => setForm(null)} onSend={(p) => void sendPayload(p)} />}
      {form === "contact" && <ContactForm onClose={() => setForm(null)} onSend={(p) => void sendPayload(p)} />}
      {form === "poll" && <PollForm onClose={() => setForm(null)} onSend={(p) => void sendPayload(p)} />}
      {state.attachments.length > 0 && (
        <AttachmentPreview
          attachments={state.attachments}
          active={state.active}
          sending={state.sending}
          onSelect={(i) => dispatch({ type: "select", index: i })}
          onRemove={(id) => dispatch({ type: "removeFile", id })}
          onCaption={(id, caption) => dispatch({ type: "caption", id, caption })}
          onAddMore={opts.media ? () => void pick("media") : undefined}
          onCancel={() => dispatch({ type: "clearFiles" })}
          onSend={() => void sendAttachments()}
        />
      )}
    </div>
  );
}
