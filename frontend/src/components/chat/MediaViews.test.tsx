// Estados de los medios: «Descargando…», «No disponible» y «Reintentar» (sin reintentos automáticos).
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { Media } from "../../api/core/model/models.js";
import Bubble from "./Bubble";
import { AudioPlayer, DocumentView, ImageView, StickerView, VideoView, stickerSize } from "./MediaViews";
import { retryUrl } from "./MediaState";
import { msg } from "./testkit";

const media = (over: Partial<Media> = {}): Media => ({
  id: "m1",
  mime: "image/jpeg",
  fileName: "",
  size: 2048,
  width: 400,
  height: 300,
  duration: 0,
  thumb: "",
  url: "/media/wa-1/m1",
  ...over,
});

const THUMB = "data:image/jpeg;base64,AAAA";
afterEach(cleanup);

describe("retryUrl", () => {
  it("el intento 0 no toca la URL; los siguientes añaden ?r=N (o &r=N si ya hay consulta)", () => {
    expect(retryUrl("/media/wa-1/m1", 0)).toBe("/media/wa-1/m1");
    expect(retryUrl("/media/wa-1/m1", 2)).toBe("/media/wa-1/m1?r=2");
    expect(retryUrl("/media/wa-1/m1?x=1", 1)).toBe("/media/wa-1/m1?x=1&r=1");
    expect(retryUrl("", 3)).toBe("");
  });
});

describe("ImageView", () => {
  it("mientras llega: miniatura visible y «Descargando…»; al cargar desaparece el aviso", () => {
    const { container } = render(<ImageView media={media({ thumb: THUMB })} onOpen={() => undefined} />);
    expect(screen.getByTestId("image-view")).toHaveAttribute("data-phase", "loading");
    expect(screen.getByText("Descargando…")).toBeInTheDocument();
    expect(container.querySelector(`img[src="${THUMB}"]`)).not.toBeNull();

    fireEvent.load(screen.getByAltText("Foto"));
    expect(screen.getByTestId("image-view")).toHaveAttribute("data-phase", "ready");
    expect(screen.queryByText("Descargando…")).toBeNull();
  });

  it("el marcador tiene el tamaño de la foto (sin saltos al cargar)", () => {
    render(<ImageView media={media({ width: 400, height: 300 })} onOpen={() => undefined} />);
    expect(screen.getByTestId("image-view")).toHaveStyle({ width: "320px", height: "240px" });
  });

  it("si falla: «No disponible» y «Reintentar»; la miniatura sigue y ya no se pide la foto", () => {
    const { container } = render(<ImageView media={media({ thumb: THUMB })} onOpen={() => undefined} />);
    fireEvent.error(screen.getByAltText("Foto"));
    expect(screen.getByTestId("image-view")).toHaveAttribute("data-phase", "error");
    expect(screen.getByText("No disponible")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Reintentar" })).toBeInTheDocument();
    expect(container.querySelector(`img[src="${THUMB}"]`)).not.toBeNull();
    // nada de reintentos en bucle: la imagen del servidor ya no está en el árbol
    expect(screen.queryByAltText("Foto")).toBeNull();
    expect(screen.queryByTitle("Ver foto")).toBeNull();
  });

  it("sin miniatura también dice «No disponible»", () => {
    render(<ImageView media={media()} onOpen={() => undefined} />);
    fireEvent.error(screen.getByAltText("Foto"));
    expect(screen.getByText("No disponible")).toBeInTheDocument();
  });

  it("«Reintentar» vuelve a pedir con ?r=N y a «Descargando…»; cada fallo exige otro clic", () => {
    render(<ImageView media={media()} onOpen={() => undefined} />);
    expect(screen.getByAltText("Foto")).toHaveAttribute("src", "/media/wa-1/m1");
    fireEvent.error(screen.getByAltText("Foto"));
    fireEvent.click(screen.getByRole("button", { name: "Reintentar" }));
    expect(screen.getByAltText("Foto")).toHaveAttribute("src", "/media/wa-1/m1?r=1");
    expect(screen.getByText("Descargando…")).toBeInTheDocument();

    fireEvent.error(screen.getByAltText("Foto"));
    fireEvent.click(screen.getByRole("button", { name: "Reintentar" }));
    expect(screen.getByAltText("Foto")).toHaveAttribute("src", "/media/wa-1/m1?r=2");

    fireEvent.load(screen.getByAltText("Foto"));
    expect(screen.getByTestId("image-view")).toHaveAttribute("data-phase", "ready");
  });

  it("el clic en la foto abre el visor, también mientras llega; «Reintentar» no lo abre", () => {
    const onOpen = vi.fn();
    render(<ImageView media={media()} onOpen={onOpen} />);
    fireEvent.click(screen.getByTitle("Ver foto"));
    expect(onOpen).toHaveBeenCalledTimes(1);
    fireEvent.error(screen.getByAltText("Foto"));
    fireEvent.click(screen.getByRole("button", { name: "Reintentar" }));
    expect(onOpen).toHaveBeenCalledTimes(1);
  });

  it("las fotos se piden de forma perezosa (lazy)", () => {
    render(<ImageView media={media()} onOpen={() => undefined} />);
    expect(screen.getByAltText("Foto")).toHaveAttribute("loading", "lazy");
  });

  it("otra foto en el mismo lugar empieza de cero", () => {
    const { rerender } = render(<ImageView media={media()} onOpen={() => undefined} />);
    fireEvent.error(screen.getByAltText("Foto"));
    rerender(<ImageView media={media({ id: "m2", url: "/media/wa-1/m2" })} onOpen={() => undefined} />);
    expect(screen.getByTestId("image-view")).toHaveAttribute("data-phase", "loading");
    expect(screen.getByAltText("Foto")).toHaveAttribute("src", "/media/wa-1/m2");
  });
});

describe("StickerView", () => {
  it("tamaño natural con tope de 160 px; sin medidas, 128×128", () => {
    expect(stickerSize({ width: 512, height: 512 })).toEqual({ width: 160, height: 160 });
    expect(stickerSize({ width: 100, height: 80 })).toEqual({ width: 100, height: 80 });
    expect(stickerSize({ width: 640, height: 320 })).toEqual({ width: 160, height: 80 });
    expect(stickerSize({ width: 0, height: 0 })).toEqual({ width: 128, height: 128 });
  });

  it("marcador de 128×128 con «Descargando…» mientras llega y se ve al cargar", () => {
    render(<StickerView media={media({ width: 0, height: 0, mime: "image/webp" })} />);
    expect(screen.getByTestId("sticker-view")).toHaveStyle({ width: "128px", height: "128px" });
    expect(screen.getByText("Descargando…")).toBeInTheDocument();
    fireEvent.load(screen.getByAltText("Sticker"));
    expect(screen.getByTestId("sticker-view")).toHaveAttribute("data-phase", "ready");
    expect(screen.queryByText("Descargando…")).toBeNull();
  });

  it("si falla: «No disponible», «Reintentar» y la URL con ?r=1", () => {
    render(<StickerView media={media({ width: 512, height: 512 })} />);
    fireEvent.error(screen.getByAltText("Sticker"));
    expect(screen.getByText("No disponible")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Reintentar" }));
    expect(screen.getByAltText("Sticker")).toHaveAttribute("src", "/media/wa-1/m1?r=1");
  });

  it("en la burbuja el sticker va sin fondo y al tamaño natural", () => {
    const { container } = render(
      <Bubble
        msg={msg({ kind: "sticker", text: "", media: media({ width: 512, height: 512 }) })}
        showSender={false}
        selfId="me"
        canVote
        onMenu={() => undefined}
        onOpenImage={() => undefined}
        onGoToQuoted={() => undefined}
        onSaveAs={() => undefined}
        onVote={() => undefined}
      />,
    );
    expect(container.innerHTML).not.toContain("bg-[var(--bubble-in)]");
    expect(container.innerHTML).not.toContain("bg-[var(--bubble-out)]");
    expect(screen.getByTestId("sticker-view")).toHaveStyle({ width: "160px", height: "160px" });
  });
});

describe("VideoView", () => {
  it("no pide el vídeo hasta reproducirlo (preload none) y conserva la miniatura como póster", () => {
    const { container } = render(<VideoView media={media({ mime: "video/mp4", thumb: THUMB })} />);
    const v = container.querySelector("video")!;
    expect(v).toHaveAttribute("preload", "none");
    expect(v).toHaveAttribute("poster", THUMB);
    expect(screen.queryByText("Descargando…")).toBeNull();
  });

  it("al reproducir dice «Descargando…» hasta que hay datos", () => {
    const { container } = render(<VideoView media={media({ mime: "video/mp4" })} />);
    const v = container.querySelector("video")!;
    fireEvent.play(v);
    expect(screen.getByText("Descargando…")).toBeInTheDocument();
    fireEvent.canPlay(v);
    expect(screen.queryByText("Descargando…")).toBeNull();
  });

  it("si falla: «No disponible» con la miniatura debajo; «Reintentar» recarga con ?r=1", () => {
    const { container } = render(<VideoView media={media({ mime: "video/mp4", thumb: THUMB })} />);
    fireEvent.error(container.querySelector("video")!);
    expect(screen.getByText("No disponible")).toBeInTheDocument();
    expect(container.querySelector(`img[src="${THUMB}"]`)).not.toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Reintentar" }));
    expect(container.querySelector("video")).toHaveAttribute("src", "/media/wa-1/m1?r=1");
  });
});

describe("AudioPlayer", () => {
  let play: ReturnType<typeof vi.fn<() => Promise<void>>>;
  beforeEach(() => {
    play = vi.fn<() => Promise<void>>().mockResolvedValue(undefined);
    vi.spyOn(HTMLMediaElement.prototype, "play").mockImplementation(play);
    vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => undefined);
  });
  afterEach(() => vi.restoreAllMocks());

  const audio = () => document.querySelector("audio")!;

  it("no pide el archivo hasta reproducir; entonces «Descargando…» hasta que puede sonar", () => {
    render(<AudioPlayer media={media({ mime: "audio/ogg", duration: 12 })} voice />);
    expect(audio()).toHaveAttribute("preload", "none");
    expect(screen.getByText("0:12")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Reproducir" }));
    expect(play).toHaveBeenCalledTimes(1);
    expect(screen.getByText("Descargando…")).toBeInTheDocument();
    fireEvent.canPlay(audio());
    expect(screen.queryByText("Descargando…")).toBeNull();
  });

  it("si falla: «No disponible» y «Reintentar» (URL ?r=1 y vuelve a reproducir)", async () => {
    render(<AudioPlayer media={media({ mime: "audio/ogg", duration: 12 })} voice />);
    fireEvent.click(screen.getByRole("button", { name: "Reproducir" }));
    fireEvent.error(audio());
    expect(screen.getByText(/No disponible/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Reintentar" }));
    await waitFor(() => expect(audio()).toHaveAttribute("src", "/media/wa-1/m1?r=1"));
    await waitFor(() => expect(play).toHaveBeenCalledTimes(2));
    expect(screen.getByText("Descargando…")).toBeInTheDocument();
  });

  it("un play() rechazado porque el origen no sirve también cuenta como fallo", async () => {
    play.mockRejectedValueOnce(Object.assign(new Error("x"), { name: "NotSupportedError" }));
    render(<AudioPlayer media={media({ mime: "audio/ogg", duration: 5 })} voice />);
    fireEvent.click(screen.getByRole("button", { name: "Reproducir" }));
    expect(await screen.findByText(/No disponible/)).toBeInTheDocument();
  });

  it("pausar a medias (AbortError) no es un fallo", async () => {
    play.mockRejectedValueOnce(Object.assign(new Error("x"), { name: "AbortError" }));
    render(<AudioPlayer media={media({ mime: "audio/ogg", duration: 5 })} voice />);
    fireEvent.click(screen.getByRole("button", { name: "Reproducir" }));
    await act(async () => undefined);
    expect(screen.queryByText(/No disponible/)).toBeNull();
  });
});

describe("DocumentView", () => {
  const doc = media({ mime: "application/pdf", fileName: "factura.pdf", size: 2048 });

  it("mientras se guarda: «Descargando…» y el botón no se repite", async () => {
    let done!: (ok: boolean) => void;
    const onSaveAs = vi.fn(() => new Promise<boolean>((r) => (done = r)));
    render(<DocumentView media={doc} onSaveAs={onSaveAs} />);
    fireEvent.click(screen.getByTitle("Guardar como"));
    expect(screen.getByText("Descargando…")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Guardar como/ })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: /Guardar como/ }));
    expect(onSaveAs).toHaveBeenCalledTimes(1);
    await act(async () => done(true));
    expect(screen.getByText("2,0 KB")).toBeInTheDocument();
  });

  it("si no se pudo: «No disponible» y «Reintentar» (a mano)", async () => {
    const onSaveAs = vi.fn().mockResolvedValueOnce(false).mockResolvedValueOnce(true);
    render(<DocumentView media={doc} onSaveAs={onSaveAs} />);
    fireEvent.click(screen.getByTitle("Guardar como"));
    expect(await screen.findByText("No disponible")).toBeInTheDocument();
    expect(onSaveAs).toHaveBeenCalledTimes(1); // no se reintenta solo

    fireEvent.click(screen.getByRole("button", { name: "Reintentar" }));
    await waitFor(() => expect(onSaveAs).toHaveBeenCalledTimes(2));
    expect(await screen.findByText("2,0 KB")).toBeInTheDocument();
  });

  it("una promesa rechazada también es «No disponible»", async () => {
    render(<DocumentView media={doc} onSaveAs={() => Promise.reject(new Error("x"))} />);
    fireEvent.click(screen.getByTitle("Guardar como"));
    expect(await screen.findByText("No disponible")).toBeInTheDocument();
  });

  it("una función sin promesa (void) deja todo como estaba", async () => {
    const onSaveAs = vi.fn();
    render(<DocumentView media={doc} onSaveAs={onSaveAs} />);
    fireEvent.click(screen.getByTitle("Guardar como"));
    await waitFor(() => expect(screen.getByTestId("document-view")).toHaveAttribute("data-phase", "idle"));
    expect(onSaveAs).toHaveBeenCalledTimes(1);
  });
});
