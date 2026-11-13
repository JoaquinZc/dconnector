import { describe, expect, it } from "vitest";
import type { AccountView, Campaign, CampaignReview } from "@/api/types";
import {
  applyProgress,
  campaignAccounts,
  campaignActions,
  campaignFromForm,
  emptyForm,
  firstInvalidStep,
  formFromCampaign,
  formatEstimate,
  formatWhen,
  hhmmToMinutes,
  launchBlockers,
  levelTone,
  minutesToHHmm,
  previewText,
  progressOf,
  queueCount,
  reviewKey,
  riskCounts,
  skippedCounts,
  skippedTotal,
  sortCampaigns,
  stateInfo,
  statusLine,
  toggleWeekday,
  validateStep,
  waitingLabel,
  weekdaysLabel,
  windowSummary,
  type CampaignForm,
} from "./campaignModel";
import { DEFAULT_PACING } from "./pacingModel";

const NOW = new Date(2026, 10, 11, 10, 0, 0).getTime();

const camp = (over: Partial<Record<keyof Campaign, unknown>> = {}): Campaign =>
  ({
    id: "c1",
    name: "Promo",
    accountId: "wa-1",
    state: "running",
    text: "Hola {nombre}",
    variants: null,
    filePath: "",
    listIds: ["l1"],
    contactIds: null,
    startAt: 0,
    window: { fromMinute: 540, toMinute: 1080, weekdays: [1, 2, 3, 4, 5] },
    pacing: DEFAULT_PACING,
    optOutFooter: true,
    abEnabled: false,
    brakeReason: "",
    createdAt: 1,
    updatedAt: 2,
    totals: { queued: 6, sent: 3, failed: 1, skipped: 0, replied: 1, optOut: 0 },
    ...over,
  }) as unknown as Campaign;

const review = (over: Partial<CampaignReview> = {}): CampaignReview => ({
  recipients: 10,
  risks: { warm: 5, lukewarm: 3, cold: 2 },
  requiredVariants: 1,
  items: [],
  level: "green",
  estimate: { seconds: 600, days: 1 },
  warnings: null,
  skipped: { optedOut: 1, noIdentity: 2 },
  ...over,
});

function validForm(over: Partial<CampaignForm> = {}): CampaignForm {
  return { ...emptyForm(NOW), name: "Promo", accountId: "wa-1", listIds: ["l1"], text: "Hola", ...over };
}

describe("ventana horaria", () => {
  it("minutos ↔ HH:mm", () => {
    expect(minutesToHHmm(540)).toBe("09:00");
    expect(minutesToHHmm(0)).toBe("00:00");
    expect(minutesToHHmm(1439)).toBe("23:59");
    expect(minutesToHHmm(-5)).toBe("00:00");
    expect(minutesToHHmm(5000)).toBe("23:59");
    expect(hhmmToMinutes("09:00")).toBe(540);
    expect(hhmmToMinutes("9:05")).toBe(545);
    expect(hhmmToMinutes("23:59")).toBe(1439);
  });

  it("rechaza horas que no valen", () => {
    expect(hhmmToMinutes("")).toBeNull();
    expect(hhmmToMinutes("24:00")).toBeNull();
    expect(hhmmToMinutes("12:60")).toBeNull();
    expect(hhmmToMinutes("abc")).toBeNull();
  });

  it("vuelve a dar lo mismo en los dos sentidos", () => {
    for (const m of [0, 1, 59, 60, 540, 1080, 1439]) expect(hhmmToMinutes(minutesToHHmm(m))).toBe(m);
  });
});

describe("días de la semana", () => {
  it("pone y quita en el orden de la semana (lunes primero, domingo al final)", () => {
    expect(toggleWeekday([1, 2], 0)).toEqual([1, 2, 0]);
    expect(toggleWeekday([1, 2, 0], 1)).toEqual([2, 0]);
    expect(toggleWeekday([5], 3)).toEqual([3, 5]);
  });

  it("los describe", () => {
    expect(weekdaysLabel([1, 2, 3, 4, 5])).toBe("Lun a Vie");
    expect(weekdaysLabel([0, 1, 2, 3, 4, 5, 6])).toBe("Todos los días");
    expect(weekdaysLabel([1, 3, 6])).toBe("Lun, Mié, Sáb");
    expect(weekdaysLabel([6, 0])).toBe("Sáb, Dom");
    expect(weekdaysLabel([])).toBe("Ningún día");
    expect(windowSummary({ fromMinute: 540, toMinute: 1080, weekdays: [1, 2, 3, 4, 5] })).toBe("09:00–18:00 · Lun a Vie");
  });
});

describe("estados y esperas", () => {
  it("texto de las esperas del motor", () => {
    expect(waitingLabel("window")).toBe("Fuera de horario");
    expect(waitingLabel("hourly")).toBe("Límite por hora");
    expect(waitingLabel("daily")).toBe("Límite del día");
    expect(waitingLabel("batch_rest")).toBe("Descanso");
    expect(waitingLabel("disconnected")).toBe("Cuenta desconectada");
    expect(waitingLabel("")).toBe("");
    expect(waitingLabel("otra")).toBe("");
  });

  it("píldora de estado", () => {
    expect(stateInfo("running")).toEqual({ label: "En curso", tone: "ok" });
    expect(stateInfo("braked").tone).toBe("danger");
    expect(stateInfo("ready").label).toBe("Programada");
    expect(stateInfo("xx").label).toBe("Desconocido");
  });

  it("línea de estado: espera, próximo envío, freno, inicio", () => {
    expect(statusLine(camp(), { nextAt: 0, waiting: "window" }, NOW)).toEqual({ text: "Fuera de horario", tone: "warn" });
    expect(statusLine(camp(), { nextAt: 0, waiting: "disconnected" }, NOW).tone).toBe("danger");
    expect(statusLine(camp(), { nextAt: NOW + 60_000, waiting: "" }, NOW).text).toBe("Próximo envío 10:01");
    expect(statusLine(camp(), undefined, NOW).text).toBe("Enviando…");
    expect(statusLine(camp({ state: "braked", brakeReason: "Muchos fallos" }), undefined, NOW)).toEqual({
      text: "Frenada: Muchos fallos",
      tone: "danger",
    });
    expect(statusLine(camp({ state: "braked", brakeReason: "" }), undefined, NOW).text).toBe("Frenada: sin motivo");
    expect(statusLine(camp({ state: "ready", startAt: new Date(2026, 10, 12, 9, 30).getTime() }), undefined, NOW).text).toBe(
      "Inicia 12/11 09:30",
    );
    expect(statusLine(camp({ state: "done" }), undefined, NOW).text).toBe("");
    // La espera solo cuenta si corre: una campaña terminada no muestra restos.
    expect(statusLine(camp({ state: "done" }), { nextAt: 0, waiting: "daily" }, NOW).text).toBe("");
  });

  it("hora de hoy o fecha y hora", () => {
    expect(formatWhen(new Date(2026, 10, 11, 15, 5).getTime(), NOW)).toBe("15:05");
    expect(formatWhen(new Date(2026, 10, 13, 8, 0).getTime(), NOW)).toBe("13/11 08:00");
    expect(formatWhen(0, NOW)).toBe("");
  });

  it("progreso: enviados sobre el total de la cola", () => {
    expect(progressOf(camp().totals)).toEqual({ sent: 3, total: 10, pct: 30 });
    expect(progressOf({ queued: 0, sent: 0, failed: 0, skipped: 0, replied: 0, optOut: 0 })).toEqual({ sent: 0, total: 0, pct: 0 });
    expect(progressOf(null).total).toBe(0);
  });

  it("recuentos de las pestañas de la cola", () => {
    const t = camp().totals;
    expect(queueCount("pending", t)).toBe(6);
    expect(queueCount("sent", t)).toBe(3);
    expect(queueCount("failed", t)).toBe(1);
    expect(queueCount("skipped", t)).toBe(0);
  });

  it("ordena: en curso primero, luego lo más reciente", () => {
    const list = [
      camp({ id: "done", state: "done", updatedAt: 9 }),
      camp({ id: "draft", state: "draft", updatedAt: 5 }),
      camp({ id: "run-old", state: "running", updatedAt: 1 }),
      camp({ id: "run-new", state: "running", updatedAt: 3 }),
      camp({ id: "braked", state: "braked", updatedAt: 1 }),
    ];
    expect(sortCampaigns(list).map((c) => c.id)).toEqual(["run-new", "run-old", "braked", "draft", "done"]);
  });

  it("acciones del detalle por estado", () => {
    expect(campaignActions("running")).toMatchObject({ pause: true, resume: false, cancel: true, edit: false, remove: false });
    expect(campaignActions("paused")).toMatchObject({ pause: false, resume: true, cancel: true, edit: true, remove: true });
    expect(campaignActions("braked")).toMatchObject({ resume: true });
    expect(campaignActions("draft")).toMatchObject({ edit: true, cancel: false, remove: true });
    expect(campaignActions("done")).toMatchObject({ pause: false, resume: false, cancel: false, remove: true });
  });
});

describe("evento campaign:progress", () => {
  it("actualiza estado y totales y devuelve lo que no trae la lista", () => {
    const list = [camp(), camp({ id: "c2" })];
    const r = applyProgress(list, {
      campaignId: "c1",
      state: "braked",
      totals: { queued: 5, sent: 4, failed: 1, skipped: 0, replied: 1, optOut: 2 },
      nextAt: 99,
      waiting: "hourly",
      brakeReason: "Bajas",
    });
    expect(r).not.toBeNull();
    expect(r!.list[0].state).toBe("braked");
    expect(r!.list[0].totals.sent).toBe(4);
    expect(r!.list[0].brakeReason).toBe("Bajas");
    expect(r!.list[1]).toBe(list[1]);
    expect(r!.live).toEqual({ nextAt: 99, waiting: "hourly" });
    expect(list[0].state).toBe("running"); // no muta
  });

  it("campaña desconocida o sin id: null (la pantalla recarga)", () => {
    expect(applyProgress([camp()], { campaignId: "x" })).toBeNull();
    expect(applyProgress([camp()], {})).toBeNull();
  });
});

describe("revisión", () => {
  it("recuento por riesgo y omitidos (siempre completos)", () => {
    expect(riskCounts(review()).map((r) => [r.id, r.count])).toEqual([
      ["warm", 5],
      ["lukewarm", 3],
      ["cold", 2],
    ]);
    expect(riskCounts(null).every((r) => r.count === 0)).toBe(true);
    expect(skippedCounts(review()).map((s) => [s.id, s.count])).toEqual([
      ["optedOut", 1],
      ["noIdentity", 2],
      ["frequency", 0],
    ]);
    expect(skippedTotal(review())).toBe(3);
  });

  it("nivel → tono", () => {
    expect(levelTone("green")).toBe("ok");
    expect(levelTone("yellow")).toBe("warn");
    expect(levelTone("red")).toBe("danger");
    expect(levelTone(undefined)).toBe("muted");
  });

  it("estimación", () => {
    expect(formatEstimate({ seconds: 30, days: 1 })).toBe("menos de 1 min");
    expect(formatEstimate({ seconds: 20 * 60, days: 1 })).toBe("≈ 20 min");
    expect(formatEstimate({ seconds: 3600, days: 1 })).toBe("≈ 1 h");
    expect(formatEstimate({ seconds: 2 * 3600 + 5 * 60, days: 1 })).toBe("≈ 2 h 5 min");
    expect(formatEstimate({ seconds: 3 * 86400, days: 3 })).toBe("≈ 3 días");
    expect(formatEstimate(null)).toBe("");
  });

  it("la clave de revisión ignora el nombre pero no el texto", () => {
    const a = validForm();
    expect(reviewKey({ ...a, name: "Otro" }, null)).toBe(reviewKey(a, null));
    expect(reviewKey({ ...a, text: "Distinto" }, null)).not.toBe(reviewKey(a, null));
    expect(reviewKey(a, DEFAULT_PACING)).not.toBe(reviewKey(a, null));
  });

  it("vista previa: {nombre} y la primera opción de {a|b}", () => {
    expect(previewText("Hola {nombre}, {buenas|qué tal}")).toBe("Hola Ana, buenas");
    expect(previewText("{a|b|c} y {Nombre}", "Luis")).toBe("a y Luis");
    expect(previewText("sin etiquetas")).toBe("sin etiquetas");
  });
});

describe("cuentas de campañas", () => {
  const v = (id: string, channel: string, enabled = true) =>
    ({ account: { id, channel, label: id, color: "#000", enabled }, status: {}, capabilities: {}, unread: 0 }) as unknown as AccountView;
  it("solo WhatsApp y Telegram activas", () => {
    const list = [v("a", "whatsapp"), v("b", "telegram"), v("c", "messenger"), v("d", "instagram"), v("e", "whatsapp", false)];
    expect(campaignAccounts(list).map((x) => x.account.id)).toEqual(["a", "b"]);
  });
});

describe("validación por paso del asistente", () => {
  it("paso 1: nombre y cuenta", () => {
    expect(validateStep(1, emptyForm(NOW), NOW)).toBe("Ponle un nombre");
    expect(validateStep(1, { ...emptyForm(NOW), name: "  " }, NOW)).toBe("Ponle un nombre");
    expect(validateStep(1, { ...emptyForm(NOW), name: "X" }, NOW)).toBe("Elige una cuenta");
    expect(validateStep(1, { ...emptyForm(NOW), name: "X", accountId: "wa-1" }, NOW)).toBeNull();
  });

  it("paso 2: al menos una lista o un contacto", () => {
    expect(validateStep(2, validForm({ listIds: [], contactIds: [] }), NOW)).toBe("Elige una lista o un contacto");
    expect(validateStep(2, validForm({ listIds: [], contactIds: ["k1"] }), NOW)).toBeNull();
    expect(validateStep(2, validForm(), NOW)).toBeNull();
  });

  it("paso 3: mensaje o adjunto", () => {
    expect(validateStep(3, validForm({ text: " " }), NOW)).toBe("Escribe el mensaje o adjunta un archivo");
    expect(validateStep(3, validForm({ text: "", filePath: "C:\\a.jpg" }), NOW)).toBeNull();
    expect(validateStep(3, validForm(), NOW)).toBeNull();
  });

  it("paso 4: ventana, días, inicio y A/B", () => {
    expect(validateStep(4, validForm(), NOW)).toBeNull();
    expect(validateStep(4, validForm({ fromTime: "18:00", toTime: "09:00" }), NOW)).toBe("La hora de inicio debe ser menor que la de fin");
    expect(validateStep(4, validForm({ fromTime: "09:00", toTime: "09:00" }), NOW)).toBe("La hora de inicio debe ser menor que la de fin");
    expect(validateStep(4, validForm({ fromTime: "" }), NOW)).toBe("Revisa la ventana horaria");
    expect(validateStep(4, validForm({ weekdays: [] }), NOW)).toBe("Elige al menos un día");
    expect(validateStep(4, validForm({ startMode: "later", startAt: "2026-11-11T09:00" }), NOW)).toBe("El inicio ya pasó");
    expect(validateStep(4, validForm({ startMode: "later", startAt: "" }), NOW)).toBe("Elige fecha y hora de inicio");
    expect(validateStep(4, validForm({ startMode: "later", startAt: "2026-11-12T09:00" }), NOW)).toBeNull();
    expect(validateStep(4, validForm({ abEnabled: true }), NOW)).toBe("La prueba A/B necesita al menos dos mensajes");
    expect(validateStep(4, validForm({ abEnabled: true, variants: ["", "Otra"] }), NOW)).toBeNull();
  });

  it("el inicio «ahora» no mira la fecha", () => {
    expect(validateStep(4, validForm({ startMode: "now", startAt: "2000-01-01T00:00" }), NOW)).toBeNull();
  });

  it("primer paso incompleto", () => {
    expect(firstInvalidStep(emptyForm(NOW), NOW)).toBe(1);
    expect(firstInvalidStep(validForm({ listIds: [] }), NOW)).toBe(2);
    expect(firstInvalidStep(validForm({ text: "" }), NOW)).toBe(3);
    expect(firstInvalidStep(validForm({ weekdays: [] }), NOW)).toBe(4);
    expect(firstInvalidStep(validForm(), NOW)).toBeNull();
  });
});

describe("qué impide «Lanzar»", () => {
  it("un paso incompleto lo impide, con su motivo", () => {
    expect(launchBlockers(validForm({ name: "" }), review(), false, NOW)).toEqual(["Ponle un nombre"]);
    expect(launchBlockers(validForm({ listIds: [] }), review(), false, NOW)).toEqual(["Elige una lista o un contacto"]);
  });

  it("mientras revisa o sin revisión no se puede", () => {
    expect(launchBlockers(validForm(), null, true, NOW)).toEqual(["Revisando…"]);
    expect(launchBlockers(validForm(), review(), true, NOW)).toEqual(["Revisando…"]);
    expect(launchBlockers(validForm(), null, false, NOW)).toEqual(["Sin revisión"]);
  });

  it("nivel rojo lo impide y dice por qué; verde y amarillo no", () => {
    const red = review({
      level: "red",
      items: [
        { level: "red", code: "similar", message: "Las variantes son casi iguales" },
        { level: "yellow", code: "x", message: "Aviso" },
      ],
    });
    expect(launchBlockers(validForm(), red, false, NOW)).toEqual(["Las variantes son casi iguales"]);
    expect(launchBlockers(validForm(), review({ level: "red", items: [] }), false, NOW)).toEqual(["La revisión tiene errores"]);
    expect(launchBlockers(validForm(), review({ level: "yellow" }), false, NOW)).toEqual([]);
    expect(launchBlockers(validForm(), review({ level: "green" }), false, NOW)).toEqual([]);
  });

  it("sin destinatarios tampoco", () => {
    expect(launchBlockers(validForm(), review({ recipients: 0 }), false, NOW)).toEqual(["Sin destinatarios"]);
  });
});

describe("formulario ↔ Campaign", () => {
  it("forma la campaña con ventana en minutos, variantes sin vacías y días en orden", () => {
    const f = validForm({
      variants: ["B", " ", "C"],
      fromTime: "08:30",
      toTime: "17:00",
      weekdays: [0, 3, 1],
      startMode: "later",
      startAt: "2026-11-12T09:15",
      filePath: "C:\\x\\a.jpg",
    });
    const c = campaignFromForm(f, DEFAULT_PACING);
    expect(c.state).toBe("draft");
    expect(c.variants).toEqual(["B", "C"]);
    expect(c.window).toEqual({ fromMinute: 510, toMinute: 1020, weekdays: [1, 3, 0] });
    expect(c.startAt).toBe(new Date(2026, 10, 12, 9, 15).getTime());
    expect(c.pacing).toBe(DEFAULT_PACING);
    expect(c.filePath).toBe("C:\\x\\a.jpg");
  });

  it("«ahora» deja startAt en 0", () => {
    expect(campaignFromForm(validForm({ startMode: "now" }), DEFAULT_PACING).startAt).toBe(0);
  });

  it("al editar conserva estado y marcas", () => {
    const base = camp({ state: "paused", createdAt: 7, brakeReason: "x" });
    const c = campaignFromForm(formFromCampaign(base, NOW), DEFAULT_PACING, base);
    expect(c.state).toBe("paused");
    expect(c.createdAt).toBe(7);
    expect(c.id).toBe("c1");
  });

  it("de Campaign a formulario y de vuelta no cambia la ventana ni los días", () => {
    const base = camp({ window: { fromMinute: 600, toMinute: 1200, weekdays: [6, 0, 2] }, startAt: new Date(2026, 10, 15, 8, 0).getTime() });
    const f = formFromCampaign(base, NOW);
    expect(f.fromTime).toBe("10:00");
    expect(f.toTime).toBe("20:00");
    expect(f.weekdays).toEqual([2, 6, 0]);
    expect(f.startMode).toBe("later");
    const back = campaignFromForm(f, DEFAULT_PACING, base);
    expect(back.window).toEqual({ fromMinute: 600, toMinute: 1200, weekdays: [2, 6, 0] });
    expect(back.startAt).toBe(base.startAt);
  });

  it("formulario vacío: ventana 09:00–18:00 de lunes a viernes", () => {
    const f = emptyForm(NOW);
    expect([f.fromTime, f.toTime, f.weekdays, f.startMode]).toEqual(["09:00", "18:00", [1, 2, 3, 4, 5], "now"]);
  });
});
