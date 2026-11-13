import { useEffect } from "react";
import { useConnection } from "@/api/connection";
import { closeTopModal } from "@/components/common/Modal";
import { ConnectingScreen, ConnectionBanner, LoginScreen, OfflineScreen } from "@/components/connection/Screens";
import ErrorBoundary from "@/components/common/ErrorBoundary";
import NotifyNotice from "@/components/common/NotifyNotice";
import UpdateNotice from "@/components/common/UpdateNotice";
import Shell from "@/components/layout/Shell";
import { PageView, preloadPages } from "@/pages/registry";
import { startApp } from "@/store/app";
import { PAGE_ORDER, applyTheme, useUI } from "@/store/ui";

/** Evento que A5 (QuickSearch) escucha para abrir la búsqueda global. */
export const QUICK_SEARCH_EVENT = "dc:quick-search";

export default function App() {
  const page = useUI((s) => s.page);
  const ready = useConnection((s) => s.ready);
  const needLogin = useConnection((s) => s.needLogin);
  const link = useConnection((s) => s.link);

  // Primero la conexión; la interfaz solo se arma cuando el servidor respondió.
  useEffect(() => {
    void useConnection.getState().boot();
  }, []);

  // Los estados y el flujo en vivo arrancan al estar lista la conexión.
  useEffect(() => {
    if (!ready) return;
    preloadPages(); // solo código de las páginas (sin consultas), para que cambiar de pestaña sea instantáneo
    return startApp();
  }, [ready]);

  useEffect(() => {
    applyTheme(useUI.getState().theme);

    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        if (closeTopModal()) e.preventDefault();
        return;
      }
      if (!e.ctrlKey || e.altKey || e.shiftKey || e.metaKey) return;
      if (e.key >= "1" && e.key <= "9") {
        e.preventDefault();
        useUI.getState().setPage(PAGE_ORDER[Number(e.key) - 1]);
      } else if (e.key.toLowerCase() === "k") {
        e.preventDefault();
        const ui = useUI.getState();
        const fire = () => window.dispatchEvent(new CustomEvent(QUICK_SEARCH_EVENT));
        if (ui.page === "chats") fire();
        else {
          ui.setPage("chats");
          setTimeout(fire, 120); // da tiempo a que ChatsPage se monte y escuche
        }
      }
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
    };
  }, []);

  if (!ready) {
    if (needLogin) return <LoginScreen />;
    return link === "offline" ? <OfflineScreen /> : <ConnectingScreen />;
  }

  return (
    <>
      <Shell>
        <ErrorBoundary level="page" resetKey={page}>
          <PageView page={page} />
        </ErrorBoundary>
      </Shell>
      <NotifyNotice />
      <UpdateNotice />
      <ConnectionBanner />
      {needLogin && <LoginScreen overlay />}
    </>
  );
}
