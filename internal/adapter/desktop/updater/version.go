// Package updater actualiza dConnector desde su Release de GitHub: consulta la última versión, descarga el
// instalador, verifica su SHA-256 contra SHA256SUMS de esa misma Release y lanza un ayudante (el propio
// dConnector.exe con --apply-update) que instala encima y vuelve a abrir la aplicación. No envía ningún dato
// de la persona: solo una petición GET sin autenticar con el User-Agent «dConnector/<versión>».
package updater

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version es una versión semántica a.b.c con sufijo opcional («0.3.8-prueba»).
type Version struct {
	Major, Minor, Patch int
	Pre                 []string
}

var versionRE = regexp.MustCompile(`^v?(\d{1,6})\.(\d{1,6})\.(\d{1,6})(?:-([0-9A-Za-z][0-9A-Za-z.-]{0,63}))?(?:\+[0-9A-Za-z.-]{1,64})?$`)

// ParseVersion lee «0.3.8», «v0.3.8» o «0.3.8-prueba». Cualquier otra cosa («dev», rutas, vacío) es un error.
func ParseVersion(s string) (Version, error) {
	m := versionRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return Version{}, fmt.Errorf("versión no válida: %q", s)
	}
	v := Version{}
	v.Major, _ = strconv.Atoi(m[1])
	v.Minor, _ = strconv.Atoi(m[2])
	v.Patch, _ = strconv.Atoi(m[3])
	if m[4] != "" {
		v.Pre = strings.Split(m[4], ".")
	}
	return v, nil
}

// String devuelve la versión sin «v» ni metadatos de compilación.
func (v Version) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if len(v.Pre) > 0 {
		s += "-" + strings.Join(v.Pre, ".")
	}
	return s
}

// Compare devuelve -1, 0 o 1. Con el mismo a.b.c, un sufijo (prueba, rc1…) es MÁS ANTIGUO que la versión final.
func (v Version) Compare(o Version) int {
	for _, p := range [][2]int{{v.Major, o.Major}, {v.Minor, o.Minor}, {v.Patch, o.Patch}} {
		if p[0] != p[1] {
			if p[0] < p[1] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(v.Pre) == 0 && len(o.Pre) == 0:
		return 0
	case len(v.Pre) == 0:
		return 1
	case len(o.Pre) == 0:
		return -1
	}
	for i := 0; i < len(v.Pre) && i < len(o.Pre); i++ {
		if c := comparePre(v.Pre[i], o.Pre[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(v.Pre) < len(o.Pre):
		return -1
	case len(v.Pre) > len(o.Pre):
		return 1
	}
	return 0
}

func comparePre(a, b string) int {
	an, aerr := strconv.Atoi(a)
	bn, berr := strconv.Atoi(b)
	switch {
	case aerr == nil && berr == nil:
		if an < bn {
			return -1
		} else if an > bn {
			return 1
		}
		return 0
	case aerr == nil:
		return -1 // los numéricos van antes que los alfanuméricos
	case berr == nil:
		return 1
	}
	return strings.Compare(a, b)
}

// IsNewer dice si latest es más nueva que current. Si current es «dev» (o no se entiende), nunca: una
// compilación de desarrollo no se actualiza sola.
func IsNewer(latest, current string) bool {
	c, err := ParseVersion(current)
	if err != nil {
		return false
	}
	l, err := ParseVersion(latest)
	if err != nil {
		return false
	}
	return l.Compare(c) > 0
}
