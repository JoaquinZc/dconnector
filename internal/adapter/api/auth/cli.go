package auth

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"golang.org/x/term"
)

const cliUsage = `Uso:
  dconnector-server user add <nombre> [--password-stdin]
  dconnector-server user passwd <nombre> [--password-stdin]
  dconnector-server user list
  dconnector-server user disable <nombre>
  dconnector-server user enable <nombre>

La clave se pide por la entrada estándar sin eco (dos veces), o se lee de una línea con --password-stdin.
`

// RunUserCLI ejecuta `dconnector-server user add|passwd|list|disable|enable …` sobre app.db.
// Devuelve el código de salida del proceso (0 bien, 1 error, 2 uso). Nunca imprime claves.
func RunUserCLI(args []string, db *sql.DB, in io.Reader, out, errOut io.Writer) int {
	if len(args) > 0 && args[0] == "user" {
		args = args[1:]
	}
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(errOut, cliUsage)
		return 2
	}
	cmd, rest := args[0], args[1:]
	stdin := false
	var names []string
	for _, a := range rest {
		switch {
		case a == "--password-stdin":
			stdin = true
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(errOut, "Opción desconocida: %s\n\n%s", a, cliUsage)
			return 2
		default:
			names = append(names, a)
		}
	}
	ctx := context.Background()
	need := func() (string, bool) {
		if len(names) != 1 {
			fmt.Fprintf(errOut, "Indique el nombre de usuario.\n\n%s", cliUsage)
			return "", false
		}
		return names[0], true
	}
	fail := func(err error) int {
		fmt.Fprintf(errOut, "Error: %s\n", err.Error())
		return 1
	}
	switch cmd {
	case "list":
		us, err := listUsers(ctx, db)
		if err != nil {
			return fail(err)
		}
		if len(us) == 0 {
			fmt.Fprintln(out, "No hay usuarios.")
			return 0
		}
		tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "USUARIO\tROL\tESTADO\tCREADO")
		for _, u := range us {
			st := "activo"
			if u.DisabledAt != 0 {
				st = "desactivado"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", u.Username, u.Role, st, time.UnixMilli(u.CreatedAt).Format("2006-01-02 15:04"))
		}
		_ = tw.Flush()
		return 0
	case "add", "passwd":
		name, ok := need()
		if !ok {
			return 2
		}
		if cmd == "add" {
			if err := validateUsername(name); err != nil {
				return fail(err)
			}
		}
		var existing user
		if cmd == "passwd" {
			u, err := getUser(ctx, db, name)
			if err != nil {
				return fail(err)
			}
			existing = u
		}
		pw, err := readPassword(in, errOut, stdin)
		if err != nil {
			return fail(err)
		}
		if err := validatePassword(pw); err != nil {
			return fail(err)
		}
		h, err := hashPassword(pw, defaultParams)
		if err != nil {
			return fail(err)
		}
		if cmd == "add" {
			if _, err := createUser(ctx, db, name, h, roleAdmin, time.Now()); err != nil {
				return fail(err)
			}
			fmt.Fprintf(out, "Usuario «%s» creado.\n", name)
			return 0
		}
		if err := setPassword(ctx, db, existing.ID, h); err != nil {
			return fail(err)
		}
		fmt.Fprintf(out, "Clave de «%s» cambiada; se cerraron sus sesiones.\n", existing.Username)
		return 0
	case "disable", "enable":
		name, ok := need()
		if !ok {
			return 2
		}
		u, err := getUser(ctx, db, name)
		if err != nil {
			return fail(err)
		}
		if err := setDisabled(ctx, db, u.ID, cmd == "disable", time.Now()); err != nil {
			return fail(err)
		}
		if cmd == "disable" {
			fmt.Fprintf(out, "Usuario «%s» desactivado; se cerraron sus sesiones.\n", u.Username)
		} else {
			fmt.Fprintf(out, "Usuario «%s» activado.\n", u.Username)
		}
		return 0
	}
	fmt.Fprintf(errOut, "Orden desconocida: %s\n\n%s", cmd, cliUsage)
	return 2
}

// readPassword lee la clave sin eco desde un terminal (con confirmación) o de una línea de la entrada.
func readPassword(in io.Reader, prompts io.Writer, fromStdin bool) (string, error) {
	if f, ok := in.(*os.File); ok && !fromStdin && term.IsTerminal(int(f.Fd())) {
		fmt.Fprint(prompts, "Clave: ")
		a, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(prompts)
		if err != nil {
			return "", errors.New("no se pudo leer la clave")
		}
		fmt.Fprint(prompts, "Repita la clave: ")
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(prompts)
		if err != nil {
			return "", errors.New("no se pudo leer la clave")
		}
		if string(a) != string(b) {
			return "", errors.New("las claves no coinciden")
		}
		return string(a), nil
	}
	line, err := bufio.NewReader(io.LimitReader(in, maxPasswordBytes+16)).ReadString('\n')
	if err != nil && (err != io.EOF || line == "") {
		return "", errors.New("no se recibió ninguna clave por la entrada estándar")
	}
	return strings.TrimRight(line, "\r\n"), nil
}
