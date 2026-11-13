// Selector de emojis (emoji-picker-react). Este módulo se importa siempre con `React.lazy` desde
// `LazyEmojiPicker.tsx`, para que el paquete pesado no entre en la carga inicial.
import EmojiPicker, { Theme } from "emoji-picker-react";

export interface EmojiPanelProps {
  onPick: (emoji: string) => void;
  dark: boolean;
  height?: number;
  width?: number;
}

export default function EmojiPanel({ onPick, dark, height = 380, width = 320 }: EmojiPanelProps) {
  return (
    <EmojiPicker
      onEmojiClick={(e) => onPick(e.emoji)}
      theme={dark ? Theme.DARK : Theme.LIGHT}
      lazyLoadEmojis
      skinTonesDisabled
      searchPlaceholder="Buscar"
      previewConfig={{ showPreview: false }}
      height={height}
      width={width}
    />
  );
}
