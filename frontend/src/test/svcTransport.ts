// Ayuda de pruebas: sustituye el transporte (`@/api/transport`) por un objeto `{ ServicioService: { Método: fn } }`,
// el mismo formato con el que las pruebas simulaban los enlaces de Wails. Así los componentes y los stores se
// prueban sin servidor y las llamadas siguen viéndose como `svc.AccountsService.List`.
type Fn = (...a: never[]) => unknown;
type Svc = Record<string, Record<string, Fn | undefined> | undefined>;

export function svcTransport(svc: Svc) {
  const call = async (service: string, method: string, args: unknown[]) => {
    const fn = svc[`${service}Service`]?.[method];
    if (typeof fn !== "function") throw new TypeError(`${service}.${method} is not a function`);
    return (fn as (...a: unknown[]) => unknown)(...args);
  };
  return { rpc: call, desktopCall: call, canUseDesktop: () => true };
}
