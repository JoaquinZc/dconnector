// Package facade: fachadas neutras (sin Wails) de los servicios del núcleo, un tipo por servicio de
// docs/14 §4.2, con los mismos métodos que internal/adapter/ui/wails (copiados, no movidos: la
// interfaz de escritorio sigue usando los de ui/wails). La API los expone por reflexión, así que todo
// método exportado de estos tipos es un método RPC: lo que no lo sea va en minúsculas o en Facade.
package facade
