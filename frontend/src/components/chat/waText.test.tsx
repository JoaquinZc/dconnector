import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { parseWaText, WaText } from "./waText";

describe("parseWaText", () => {
  it("negrita, cursiva, tachado y mono", () => {
    expect(parseWaText("*a* _b_ ~c~ `d`")).toEqual([
      { t: "b", children: [{ t: "text", v: "a" }] },
      { t: "text", v: " " },
      { t: "i", children: [{ t: "text", v: "b" }] },
      { t: "text", v: " " },
      { t: "s", children: [{ t: "text", v: "c" }] },
      { t: "text", v: " " },
      { t: "code", v: "d" },
    ]);
  });

  it("anida marcas y no formatea dentro de código", () => {
    const [n] = parseWaText("*hola _tú_*");
    expect(n.t).toBe("b");
    expect(parseWaText("`*no*`")).toEqual([{ t: "code", v: "*no*" }]);
  });

  it("no formatea guiones bajos dentro de palabras ni marcas sin cierre", () => {
    expect(parseWaText("snake_case_name")).toEqual([{ t: "text", v: "snake_case_name" }]);
    expect(parseWaText("2 * 3 = 6")).toEqual([{ t: "text", v: "2 * 3 = 6" }]);
    expect(parseWaText("*sin cierre")).toEqual([{ t: "text", v: "*sin cierre" }]);
  });

  it("enlaces: quita la puntuación final y completa www.", () => {
    expect(parseWaText("mira https://ejemplo.com/a?b=1.")).toEqual([
      { t: "text", v: "mira " },
      { t: "link", href: "https://ejemplo.com/a?b=1", v: "https://ejemplo.com/a?b=1" },
      { t: "text", v: "." },
    ]);
    expect(parseWaText("www.darkmelon.com")).toEqual([{ t: "link", href: "https://www.darkmelon.com", v: "www.darkmelon.com" }]);
  });

  it("bloque de código", () => {
    expect(parseWaText("```a\nb```")).toEqual([{ t: "pre", v: "a\nb" }]);
  });
});

describe("WaText", () => {
  it("pinta enlaces con target=_blank y no inyecta HTML", () => {
    const { container } = render(<WaText text={'*hola* <b>x</b> https://ejemplo.com'} />);
    expect(container.querySelector("strong")?.textContent).toBe("hola");
    expect(container.querySelector("b")).toBeNull();
    const a = container.querySelector("a")!;
    expect(a.getAttribute("href")).toBe("https://ejemplo.com");
    expect(a.getAttribute("target")).toBe("_blank");
    expect(container.textContent).toContain("<b>x</b>");
  });
});
