// Red de seguridad: si un componente lanza al dibujarse, la app NO se queda en blanco. Muestra un aviso en
// español con «Reintentar» (y «Recargar» en el nivel raíz). `resetKey` re-arma la barrera (p. ej. al cambiar de página).
import { Component, type ErrorInfo, type ReactNode } from "react";

interface Props {
  children: ReactNode;
  /** `root`: pantalla completa con «Recargar»; `page`: dentro del contenido, el menú sigue usable. */
  level?: "root" | "page";
  /** Al cambiar, la barrera se vuelve a armar. */
  resetKey?: unknown;
}

interface State {
  failed: boolean;
}

export default class ErrorBoundary extends Component<Props, State> {
  state: State = { failed: false };

  static getDerivedStateFromError(): State {
    return { failed: true };
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    // Solo el mensaje y la pila de componentes: nunca el contenido de mensajes ni credenciales.
    console.error("Error de interfaz:", error.message, info.componentStack);
  }

  componentDidUpdate(prev: Props): void {
    if (this.state.failed && prev.resetKey !== this.props.resetKey) this.setState({ failed: false });
  }

  render(): ReactNode {
    if (!this.state.failed) return this.props.children;
    const root = this.props.level === "root";
    return (
      <div
        role="alert"
        data-testid="error-boundary"
        className={`flex flex-col items-center justify-center gap-3 p-6 text-center ${root ? "min-h-screen" : "h-full min-h-[240px]"}`}
        style={{ background: "var(--bg, transparent)", color: "var(--fg, inherit)" }}
      >
        <h2 className="text-base font-semibold">Algo salió mal en esta pantalla</h2>
        <p className="max-w-[320px] break-words text-sm opacity-70">
          Tus datos están a salvo. Prueba a reintentar; si sigue igual, recarga la aplicación.
        </p>
        <div className="flex flex-wrap justify-center gap-2">
          <button type="button" className="btn" onClick={() => this.setState({ failed: false })}>
            Reintentar
          </button>
          {root && (
            <button type="button" className="btn" onClick={() => window.location.reload()}>
              Recargar
            </button>
          )}
        </div>
      </div>
    );
  }
}
