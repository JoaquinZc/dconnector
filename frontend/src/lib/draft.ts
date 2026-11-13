// Borradores de formularios largos en localStorage: clave `draft:<pantalla>:<id>` (11 §5).
const key = (screen: string, id: string) => `draft:${screen}:${id}`;

export function loadDraft<T>(screen: string, id: string): T | null {
  try {
    const raw = localStorage.getItem(key(screen, id));
    return raw ? (JSON.parse(raw) as T) : null;
  } catch {
    return null;
  }
}

export function saveDraft<T>(screen: string, id: string, value: T): void {
  try {
    localStorage.setItem(key(screen, id), JSON.stringify(value));
  } catch {
    /* sin almacenamiento */
  }
}

export function clearDraft(screen: string, id: string): void {
  try {
    localStorage.removeItem(key(screen, id));
  } catch {
    /* sin almacenamiento */
  }
}
