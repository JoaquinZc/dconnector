import { describe, expect, it } from "vitest";
import { parseWhatsApp, wrapSelection } from "./whatsappFormat";

describe("formato de WhatsApp", () => {
  it("reconoce negrita, cursiva, tachado y monoespaciado", () => {
    expect(parseWhatsApp("Hola *mundo*")).toEqual([
      { type: "text", text: "Hola " },
      { type: "bold", children: [{ type: "text", text: "mundo" }] },
    ]);
    expect(parseWhatsApp("_a_ y ~b~")).toEqual([
      { type: "italic", children: [{ type: "text", text: "a" }] },
      { type: "text", text: " y " },
      { type: "strike", children: [{ type: "text", text: "b" }] },
    ]);
    expect(parseWhatsApp("```x *no* y```")).toEqual([{ type: "mono", text: "x *no* y" }]);
  });

  it("`mono` con una sola comilla", () => {
    expect(parseWhatsApp("usa `npm test` ahora")).toEqual([
      { type: "text", text: "usa " },
      { type: "mono", text: "npm test" },
      { type: "text", text: " ahora" },
    ]);
    expect(parseWhatsApp("`*no*`")).toEqual([{ type: "mono", text: "*no*" }]);
  });

  it("anida formatos", () => {
    expect(parseWhatsApp("*hola _tú_*")).toEqual([
      {
        type: "bold",
        children: [
          { type: "text", text: "hola " },
          { type: "italic", children: [{ type: "text", text: "tú" }] },
        ],
      },
    ]);
  });

  it("no aplica formato si el marcador no pega al texto o parte palabras", () => {
    expect(parseWhatsApp("2 * 3 * 4")).toEqual([{ type: "text", text: "2 * 3 * 4" }]);
    expect(parseWhatsApp("archivo_de_prueba.pdf")).toEqual([{ type: "text", text: "archivo_de_prueba.pdf" }]);
    expect(parseWhatsApp("*sin cierre")).toEqual([{ type: "text", text: "*sin cierre" }]);
    expect(parseWhatsApp("* a *")).toEqual([{ type: "text", text: "* a *" }]);
  });

  it("no cruza saltos de línea", () => {
    expect(parseWhatsApp("*uno\ndos*")).toEqual([{ type: "text", text: "*uno\ndos*" }]);
  });

  it("enlaces y correos, sin romper sus guiones bajos", () => {
    expect(parseWhatsApp("Mira https://www.instagram.com/papis_ecu?igsh=MX== ya")).toEqual([
      { type: "text", text: "Mira " },
      {
        type: "link",
        text: "https://www.instagram.com/papis_ecu?igsh=MX==",
        href: "https://www.instagram.com/papis_ecu?igsh=MX==",
      },
      { type: "text", text: " ya" },
    ]);
    expect(parseWhatsApp("Escribe a ventas@papis.ec.")).toEqual([
      { type: "text", text: "Escribe a " },
      { type: "link", text: "ventas@papis.ec", href: "mailto:ventas@papis.ec" },
      { type: "text", text: "." },
    ]);
    expect(parseWhatsApp("www.papis.ec")[0]).toMatchObject({ href: "https://www.papis.ec" });
  });

  it("nunca produce HTML: un <script> queda como texto", () => {
    expect(parseWhatsApp("<script>alert(1)</script>")).toEqual([{ type: "text", text: "<script>alert(1)</script>" }]);
  });

  it("texto vacío o nulo", () => {
    expect(parseWhatsApp("")).toEqual([]);
    expect(parseWhatsApp(undefined as unknown as string)).toEqual([]);
  });

  it("envuelve la selección dejando fuera los espacios", () => {
    expect(wrapSelection("hola mundo", 5, 10, "*")).toEqual({
      value: "hola *mundo*",
      selectionStart: 6,
      selectionEnd: 11,
    });
    expect(wrapSelection("hola mundo ", 4, 11, "_").value).toBe("hola _mundo_ ");
    expect(wrapSelection("ab", 1, 1, "~")).toEqual({ value: "a~~b", selectionStart: 2, selectionEnd: 2 });
  });
});
