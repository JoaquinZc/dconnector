// Grabación de notas de voz: MediaRecorder `audio/webm;codecs=opus` → webmToOgg → base64.
import { useEffect, useRef, useState } from "react";
import { Mic, Send, Trash2 } from "lucide-react";
import { webmToOgg } from "@/lib/webmToOgg";
import { useUI } from "@/store/ui";
import { formatDuration } from "./chatFormat";
import { IconButton } from "./parts";

export const VOICE_MIME = "audio/webm;codecs=opus";
const MAX_SECONDS = 15 * 60;

export function bytesToBase64(bytes: Uint8Array): string {
  let binary = "";
  for (let i = 0; i < bytes.length; i += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  }
  return btoa(binary);
}

export default function VoiceRecorder({
  disabled,
  onRecordingChange,
  onSend,
}: {
  disabled?: boolean;
  onRecordingChange?: (recording: boolean) => void;
  /** Recibe el audio OGG/Opus en base64. */
  onSend: (oggBase64: string, seconds: number) => Promise<void>;
}) {
  const [recording, setRecording] = useState(false);
  const [seconds, setSeconds] = useState(0);
  const [busy, setBusy] = useState(false);
  const recorderRef = useRef<MediaRecorder | null>(null);
  const chunksRef = useRef<Blob[]>([]);
  const timerRef = useRef<number | null>(null);
  const sendOnStopRef = useRef(false);
  const secondsRef = useRef(0);

  const cleanup = () => {
    if (timerRef.current) window.clearInterval(timerRef.current);
    timerRef.current = null;
    recorderRef.current?.stream.getTracks().forEach((t) => t.stop());
    recorderRef.current = null;
    setRecording(false);
    onRecordingChange?.(false);
  };

  useEffect(() => () => cleanup(), []); // eslint-disable-line react-hooks/exhaustive-deps

  const fail = (msg: string) => useUI.getState().toast("error", msg);

  const start = async () => {
    try {
      if (!navigator.mediaDevices?.getUserMedia || typeof MediaRecorder === "undefined") {
        throw new Error("Este equipo no puede grabar audio.");
      }
      const stream = await navigator.mediaDevices.getUserMedia({
        audio: { echoCancellation: true, noiseSuppression: true },
      });
      if (!MediaRecorder.isTypeSupported(VOICE_MIME)) {
        stream.getTracks().forEach((t) => t.stop());
        throw new Error("Este equipo no puede grabar audio en Opus.");
      }
      const recorder = new MediaRecorder(stream, { mimeType: VOICE_MIME, audioBitsPerSecond: 32000 });
      chunksRef.current = [];
      sendOnStopRef.current = false;
      recorder.ondataavailable = (e) => {
        if (e.data.size) chunksRef.current.push(e.data);
      };
      recorder.onstop = async () => {
        const send = sendOnStopRef.current;
        const blob = new Blob(chunksRef.current, { type: VOICE_MIME });
        const secs = secondsRef.current;
        cleanup();
        if (!send || blob.size === 0) return;
        setBusy(true);
        try {
          const ogg = webmToOgg(new Uint8Array(await blob.arrayBuffer()));
          await onSend(bytesToBase64(ogg), secs);
        } catch (e) {
          fail(`No se pudo enviar la nota de voz. ${(e as Error)?.message ?? ""}`.trim());
        } finally {
          setBusy(false);
        }
      };
      recorder.start(1000);
      recorderRef.current = recorder;
      secondsRef.current = 0;
      setSeconds(0);
      setRecording(true);
      onRecordingChange?.(true);
      timerRef.current = window.setInterval(() => {
        secondsRef.current += 1;
        setSeconds(secondsRef.current);
        if (secondsRef.current >= MAX_SECONDS) {
          sendOnStopRef.current = true;
          recorderRef.current?.stop();
        }
      }, 1000);
    } catch (e) {
      const name = (e as Error)?.name;
      fail(
        name === "NotAllowedError" || name === "SecurityError"
          ? "Permite el acceso al micrófono en la configuración de privacidad de Windows."
          : name === "NotFoundError"
            ? "No se encontró ningún micrófono conectado."
            : ((e as Error)?.message ?? "No se pudo usar el micrófono."),
      );
      cleanup();
    }
  };

  const stop = (send: boolean) => {
    sendOnStopRef.current = send;
    if (recorderRef.current?.state === "recording") recorderRef.current.stop();
    else cleanup();
  };

  if (!recording) {
    return (
      <IconButton title="Grabar nota de voz" onClick={() => void start()} disabled={disabled || busy}>
        {busy ? (
          <span className="h-4 w-4 animate-spin rounded-full border-2 border-[var(--muted)]/40 border-t-[var(--muted)]" />
        ) : (
          <Mic size={22} />
        )}
      </IconButton>
    );
  }

  return (
    <div className="flex min-w-0 flex-1 items-center gap-3 px-1">
      <IconButton title="Descartar" onClick={() => stop(false)}>
        <Trash2 size={20} />
      </IconButton>
      <span className="h-2.5 w-2.5 animate-pulse rounded-full bg-[var(--danger)]" />
      <span className="text-[14px] font-medium tabular-nums">{formatDuration(seconds)}</span>
      <span className="flex-1" />
      <button
        type="button"
        onClick={() => stop(true)}
        title="Enviar nota de voz"
        className="flex h-10 w-10 items-center justify-center rounded-full bg-[var(--accent)] text-white"
      >
        <Send size={18} />
      </button>
    </div>
  );
}
