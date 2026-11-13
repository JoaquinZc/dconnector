import { describe, expect, it } from "vitest";

import { isInsecureRemote } from "./session";
describe("isInsecureRemote", () => {
  it("avisa solo de http fuera de esta máquina", () => {
    expect(isInsecureRemote("http://192.168.1.5:8080")).toBe(true);
    expect(isInsecureRemote("http://servidor.com")).toBe(true);
    expect(isInsecureRemote("https://servidor.com")).toBe(false);
    expect(isInsecureRemote("http://127.0.0.1:5000")).toBe(false);
    expect(isInsecureRemote("http://localhost:5000")).toBe(false);
    expect(isInsecureRemote("http://[::1]:5000")).toBe(false);
    expect(isInsecureRemote("")).toBe(false);
  });
});
