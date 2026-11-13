import { describe, expect, it } from "vitest";
import { buildContact, buildLocation, buildPoll, parseCoord } from "./AttachForms";
import { highlightParts, snippetAround } from "./QuickSearchHelpers";
import { chatFiles } from "./chatFiles";
import { msg } from "./testkit";

describe("formularios de adjuntos", () => {
  it("coordenadas con coma o punto, con rangos", () => {
    expect(parseCoord("-0,18")).toBe(-0.18);
    expect(buildLocation("-0.18", "-78,47", " Oficina ", "")?.location).toMatchObject({ lat: -0.18, lng: -78.47, name: "Oficina", live: false });
    expect(buildLocation("100", "0", "", "")).toBeNull();
    expect(buildLocation("", "0", "", "")).toBeNull();
  });

  it("contacto exige nombre y teléfono y arma la vCard", () => {
    expect(buildContact("", "+593")).toBeNull();
    const c = buildContact("Marta", "+593999000111");
    expect(c?.contact?.vcard).toContain("FN:Marta");
    expect(c?.contact?.vcard).toContain("TEL;type=CELL:+593999000111");
  });

  it("encuesta: pregunta y al menos dos opciones distintas", () => {
    expect(buildPoll("¿Qué?", ["Sí", ""], false)).toBeNull();
    expect(buildPoll("¿Qué?", ["Sí", "sí "], false)).not.toBeNull();
    expect(buildPoll("¿Qué?", ["Sí", "Sí"], false)).toBeNull();
    expect(buildPoll("", ["a", "b"], false)).toBeNull();
    const p = buildPoll("¿Qué?", ["Sí", "No", "No"], true);
    expect(p?.poll).toEqual({ question: "¿Qué?", multi: true, options: [{ name: "Sí", voters: [] }, { name: "No", voters: [] }] });
  });
});

describe("búsqueda rápida", () => {
  it("resalta sin distinguir mayúsculas ni tildes", () => {
    expect(highlightParts("Dirección de entrega", "DIRECCION")).toEqual([
      { text: "Dirección", hit: true },
      { text: " de entrega", hit: false },
    ]);
    expect(highlightParts("hola", "")).toEqual([{ text: "hola", hit: false }]);
    expect(highlightParts("a b a", "a").filter((p) => p.hit)).toHaveLength(2);
  });
  it("recorta alrededor de la coincidencia", () => {
    const long = "x".repeat(200) + " clave " + "y".repeat(200);
    const s = snippetAround(long, "clave", 20);
    expect(s.startsWith("…")).toBe(true);
    expect(s.endsWith("…")).toBe(true);
    expect(s).toContain("clave");
    expect(snippetAround("corto", "co")).toBe("corto");
  });
});

describe("archivos del chat", () => {
  it("separa fotos/vídeos de documentos, de más reciente a más antiguo, sin borrados", () => {
    const media = { id: "x", mime: "", fileName: "f", size: 1, width: 0, height: 0, duration: 0, thumb: "", url: "" };
    const f = chatFiles([
      msg({ key: "1", kind: "image", media }),
      msg({ key: "2", kind: "document", media }),
      msg({ key: "3", kind: "video", media }),
      msg({ key: "4", kind: "image", media, deletedAt: 5 }),
      msg({ key: "5", kind: "text" }),
    ]);
    expect(f.visual.map((m) => m.key)).toEqual(["3", "1"]);
    expect(f.documents.map((m) => m.key)).toEqual(["2"]);
  });
});
