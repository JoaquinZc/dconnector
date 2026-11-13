//go:build windows

package winnotify

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// wnfQuietHoursActiveProfile es el nombre del estado WNF WNF_SHEL_QUIETHOURS_ACTIVE_PROFILE_CHANGED: el shell
// de Windows lo actualiza cuando entra o sale del modo «No molestar» (asistente de concentración). Su valor es
// el perfil activo: 0 = ninguno (desactivado); cualquier otro valor = activado (aquí solo importa que no
// sea 0). Es una API no documentada de ntdll (NtQueryWnfStateData), de solo lectura; si falla, el estado
// queda «desconocido» y no se afirma nada.
const wnfQuietHoursActiveProfile uint64 = 0x0D83063EA3BF1C75

var (
	ntdll                       = windows.NewLazySystemDLL("ntdll.dll")
	procNtQueryWnfStateData     = ntdll.NewProc("NtQueryWnfStateData")
	shell32                     = windows.NewLazySystemDLL("shell32.dll")
	procSHQueryUserNotification = shell32.NewProc("SHQueryUserNotificationState")
)

// winQuiet lee el estado real de Windows: «No molestar» (WNF) y SHQueryUserNotificationState.
type winQuiet struct{}

func (winQuiet) DoNotDisturb() (uint32, error) { return readWNFUint32(wnfQuietHoursActiveProfile) }

func (winQuiet) UserState() (int, error) {
	if err := procSHQueryUserNotification.Find(); err != nil {
		return 0, err
	}
	var state int32
	hr, _, _ := procSHQueryUserNotification.Call(uintptr(unsafe.Pointer(&state)))
	if hr != 0 {
		return 0, fmt.Errorf("SHQueryUserNotificationState: HRESULT 0x%08X", uint32(hr))
	}
	return int(state), nil
}

// readWNFUint32 lee los datos de un estado WNF como un número de 32 bits.
func readWNFUint32(name uint64) (uint32, error) {
	if err := procNtQueryWnfStateData.Find(); err != nil {
		return 0, err
	}
	var (
		buf   [16]byte
		size  = uint32(len(buf))
		stamp uint32
	)
	// NtQueryWnfStateData(StateName*, TypeId*, ExplicitScope*, ChangeStamp*, Buffer*, BufferSize*)
	status, _, _ := procNtQueryWnfStateData.Call(
		uintptr(unsafe.Pointer(&name)), 0, 0,
		uintptr(unsafe.Pointer(&stamp)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if uint32(status) != 0 {
		return 0, fmt.Errorf("NtQueryWnfStateData: NTSTATUS 0x%08X", uint32(status))
	}
	if size < 4 || size > uint32(len(buf)) {
		return 0, fmt.Errorf("NtQueryWnfStateData: tamaño de datos inesperado (%d bytes)", size)
	}
	return binary.LittleEndian.Uint32(buf[:4]), nil
}
