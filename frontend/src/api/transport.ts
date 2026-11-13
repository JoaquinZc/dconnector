// Costura de transporte de `api`: todo pasa por aquí. Las pruebas de componentes la sustituyen
// (`vi.mock("@/api/transport")`) para no hablar con ningún servidor.
import { callWails, hasWails } from "./desktop";
import { rpcCall } from "./http";

/** Llamada a un método del núcleo por HTTP (`/rpc/{servicio}/{método}`). */
export function rpc<T = unknown>(service: string, method: string, args: unknown[]): Promise<T> {
  return rpcCall<T>(service, method, args);
}

/** ¿Hay escritorio con enlaces de Wails para los métodos que dependen de la máquina? */
export function canUseDesktop(): boolean {
  return hasWails();
}

/** Llamada a un método de los servicios de escritorio (Files incl. Upload y SaveFileAs, Notifications, Settings.Open*). */
export function desktopCall<T = unknown>(service: string, method: string, args: unknown[]): Promise<T> {
  return callWails<T>(service, method, args);
}
