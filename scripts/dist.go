//go:build ignore

// dist.go: herramienta de empaquetado multiplataforma de dConnector (docs/12-EMPAQUETADO.md).
// Solo usa la biblioteca estándar de Go: sirve igual en Windows y en un runner Linux, sin wix, powershell
// ni wails. Se ejecuta con `go run scripts/dist.go <subcomando> ...` desde la raíz del repositorio
// (el Taskfile lo hace por ti). Excluido de `go build ./...` por la etiqueta `ignore`.
//
// Subcomandos:
//
//	version                          imprime VERSION (sin saltos de línea)
//	rm <patrón>...                   borra archivos o carpetas (patrones glob; lo que no exista se ignora)
//	mkdir <ruta>...                  crea carpetas
//	cp <origen> <destino>            copia un archivo (crea la carpeta de destino)
//	gofmt                            falla si `gofmt -l` lista algún archivo
//	server -os -arch -version        empaqueta el servidor (zip en Windows, tar.gz en Linux) en dist/
//	echo -os -arch -version          empaqueta el echo-server (zip en Windows, tar.gz en Linux; ambos con las licencias)
//	sums -version V                  escribe dist/SHA256SUMS con los artefactos de esa versión
//	sums -verify                     comprueba dist/SHA256SUMS contra los archivos
//	docker -version V                construye la imagen y la arranca con su healthcheck (omite sin Docker)
//
// Los archivos salen reproducibles: marcas de tiempo fijas, sin propietario, orden estable.
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// stamp: marca de tiempo fija de todo lo que se empaqueta (reproducible). SOURCE_DATE_EPOCH la sustituye.
func stamp() time.Time {
	if s := os.Getenv("SOURCE_DATE_EPOCH"); s != "" {
		var n int64
		if _, err := fmt.Sscan(s, &n); err == nil && n > 315532800 {
			return time.Unix(n, 0).UTC()
		}
	}
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
}

func main() {
	if len(os.Args) < 2 {
		fail("falta el subcomando (version, rm, mkdir, cp, gofmt, server, echo, sums, docker)")
	}
	args := os.Args[2:]
	var err error
	switch os.Args[1] {
	case "version":
		var v string
		v, err = readVersion()
		if err == nil {
			fmt.Print(v)
		}
	case "rm":
		err = cmdRm(args)
	case "mkdir":
		for _, d := range args {
			if err = os.MkdirAll(d, 0o755); err != nil {
				break
			}
		}
	case "cp":
		if len(args) != 2 {
			err = errors.New("uso: cp <origen> <destino>")
		} else {
			err = copyFile(args[0], args[1])
		}
	case "gofmt":
		err = cmdGofmt()
	case "server":
		err = cmdPack(args, false)
	case "echo":
		err = cmdPack(args, true)
	case "sums":
		err = cmdSums(args)
	case "docker":
		err = cmdDocker(args)
	default:
		err = fmt.Errorf("subcomando desconocido: %s", os.Args[1])
	}
	if err != nil {
		fail(err.Error())
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "ERROR:", msg)
	os.Exit(1)
}

func readVersion() (string, error) {
	b, err := os.ReadFile("VERSION")
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(string(b))
	if !regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.]+)?$`).MatchString(v) {
		return "", fmt.Errorf("VERSION no es a.b.c[-sufijo]: %q", v)
	}
	return v, nil
}

func cmdRm(patterns []string) error {
	for _, p := range patterns {
		ms, err := filepath.Glob(p)
		if err != nil {
			return err
		}
		for _, m := range ms {
			if err := os.RemoveAll(m); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o755)
}

func cmdGofmt() error {
	skip := map[string]bool{".git": true, "node_modules": true, "bin": true, "dist": true, "frontend": true}
	targets := []string{}
	ents, err := os.ReadDir(".")
	if err != nil {
		return err
	}
	for _, e := range ents {
		switch {
		case e.IsDir() && !skip[e.Name()] && !strings.HasPrefix(e.Name(), "."):
			targets = append(targets, e.Name())
		case !e.IsDir() && strings.HasSuffix(e.Name(), ".go"):
			targets = append(targets, e.Name())
		}
	}
	if _, err := os.Stat("frontend/embed.go"); err == nil {
		targets = append(targets, "frontend/embed.go")
	}
	out, err := exec.Command("gofmt", append([]string{"-l"}, targets...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("gofmt: %v: %s", err, out)
	}
	if s := strings.TrimSpace(string(out)); s != "" {
		return fmt.Errorf("gofmt -l lista archivos sin formato:\n%s", s)
	}
	fmt.Println("gofmt: sin diferencias")
	return nil
}

// ---------------------------------------------------------------------------------------------------------
// Paquetes del servidor y del echo-server

// archName: nombre de la arquitectura en los archivos (x64, arm64, x86).
func archName(goarch string) (string, error) {
	switch goarch {
	case "amd64", "x64":
		return "x64", nil
	case "arm64":
		return "arm64", nil
	case "386", "x86":
		return "x86", nil
	}
	return "", fmt.Errorf("arquitectura desconocida: %s", goarch)
}

// entry: un archivo del paquete.
type entry struct {
	src, dest string
	mode      int64
	tmpl      bool // sustituye @VERSION@
	crlf      bool // fines de línea de Windows
	bom       bool // UTF-8 con BOM
	optional  bool // si falta el origen, avisa y sigue
	bin       bool // ejecutable: se comprueba su arquitectura
}

// packed: lo que ya se leyó de un entry.
type packed struct {
	entry
	data []byte
}

func loadEntries(es []entry, version string) ([]packed, error) {
	var out []packed
	for _, e := range es {
		b, err := os.ReadFile(e.src)
		if err != nil {
			if e.optional && errors.Is(err, os.ErrNotExist) {
				fmt.Fprintf(os.Stderr, "AVISO: no existe %s; no se incluye en el paquete.\n", e.src)
				continue
			}
			return nil, err
		}
		if e.tmpl {
			b = bytes.ReplaceAll(b, []byte("@VERSION@"), []byte(version))
		}
		if e.crlf {
			b = bytes.ReplaceAll(bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")), []byte("\n"), []byte("\r\n"))
		}
		if e.bom && !bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}) {
			b = append([]byte{0xEF, 0xBB, 0xBF}, b...)
		}
		out = append(out, packed{e, b})
	}
	return out, nil
}

func cmdPack(args []string, echo bool) error {
	fs := flag.NewFlagSet("pack", flag.ContinueOnError)
	goos := fs.String("os", "", "windows o linux")
	goarch := fs.String("arch", "", "amd64 o arm64")
	version := fs.String("version", "", "versión (por defecto, VERSION)")
	dist := fs.String("dist", "dist", "carpeta de salida")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *version == "" {
		v, err := readVersion()
		if err != nil {
			return err
		}
		*version = v
	}
	if *goos != "windows" && *goos != "linux" {
		return fmt.Errorf("--os debe ser windows o linux (es %q)", *goos)
	}
	an, err := archName(*goarch)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*dist, 0o755); err != nil {
		return err
	}
	win := *goos == "windows"
	ext := ""
	if win {
		ext = ".exe"
	}

	if echo {
		// El echo-server enlaza github.com/coder/websocket (ISC): al redistribuir el binario hay que acompañar el aviso de
		// licencia, así que va siempre en un archivo con LICENCIAS.txt, LICENSE y THIRD-PARTY-NOTICES (zip en Windows).
		src := filepath.Join("bin", "echo", *goos+"-"+*goarch, "echo-server"+ext)
		root := fmt.Sprintf("echo-server-%s-%s-%s", *version, *goos, an)
		docs := []entry{
			{src: "LICENCIAS.txt", dest: "LICENCIAS.txt", mode: 0o644},
			{src: "LICENSE", dest: "LICENSE", mode: 0o644, optional: true},
			{src: "THIRD-PARTY-NOTICES", dest: "THIRD-PARTY-NOTICES", mode: 0o644, optional: true},
		}
		bin := entry{src: src, dest: "echo-server" + ext, mode: 0o755, bin: true}
		if win {
			return writeAndVerify("zip", filepath.Join(*dist, root+".zip"), root, *version, "windows", *goarch, append([]entry{bin}, docs...))
		}
		return writeAndVerify("tar.gz", filepath.Join(*dist, root+".tar.gz"), root, *version, "linux", *goarch, append([]entry{bin}, docs...))
	}

	root := fmt.Sprintf("dConnector-server-%s-%s-%s", *version, *goos, an)
	srvDir := filepath.Join("bin", "server", *goos+"-"+*goarch)
	if win {
		return writeAndVerify("zip", filepath.Join(*dist, root+".zip"), root, *version, "windows", *goarch, []entry{
			{src: filepath.Join(srvDir, "dConnector-server.exe"), dest: "dConnector-server.exe", mode: 0o755, bin: true},
			{src: filepath.Join("build", "server", "LEEME-SERVIDOR-windows.txt"), dest: "LEEME-SERVIDOR.txt", mode: 0o644, tmpl: true, crlf: true, bom: true},
			{src: "LICENCIAS.txt", dest: "LICENCIAS.txt", mode: 0o644},
			{src: "LICENSE", dest: "LICENSE", mode: 0o644, optional: true},
			{src: "THIRD-PARTY-NOTICES", dest: "THIRD-PARTY-NOTICES", mode: 0o644, optional: true},
		})
	}
	return writeAndVerify("tar.gz", filepath.Join(*dist, root+".tar.gz"), root, *version, "linux", *goarch, []entry{
		{src: filepath.Join(srvDir, "dconnector-server"), dest: "dconnector-server", mode: 0o755, bin: true},
		{src: filepath.Join("deploy", "systemd", "dconnector.service"), dest: "dconnector.service", mode: 0o644},
		{src: filepath.Join("deploy", "dconnector.env.example"), dest: "dconnector.env.example", mode: 0o644},
		{src: filepath.Join("build", "server", "LEEME-SERVIDOR-linux.txt"), dest: "LEEME-SERVIDOR.txt", mode: 0o644, tmpl: true},
		{src: "LICENCIAS.txt", dest: "LICENCIAS.txt", mode: 0o644},
		{src: "LICENSE", dest: "LICENSE", mode: 0o644, optional: true},
		{src: "THIRD-PARTY-NOTICES", dest: "THIRD-PARTY-NOTICES", mode: 0o644, optional: true},
	})
}

func size(p string) string {
	st, err := os.Stat(p)
	if err != nil {
		return "?"
	}
	return fmt.Sprintf("%.1f MB", float64(st.Size())/1048576)
}

// writeAndVerify crea el archivo y lo vuelve a leer comprobando nombres, modos, contenido y arquitectura.
func writeAndVerify(format, out, root, version, goos, goarch string, es []entry) error {
	items, err := loadEntries(es, version)
	if err != nil {
		return err
	}
	for _, it := range items {
		if it.bin {
			if err := checkBinary(it.data, goos, goarch, it.src); err != nil {
				return err
			}
		}
	}
	_ = os.Remove(out)
	switch format {
	case "zip":
		err = writeZip(out, root, items)
	default:
		err = writeTarGz(out, root, items)
	}
	if err != nil {
		return err
	}
	if err := verifyArchive(format, out, root, goos, goarch, items); err != nil {
		return fmt.Errorf("comprobación de %s: %w", out, err)
	}
	fmt.Printf("Creado y verificado: %s (%s, %d archivos)\n", out, size(out), len(items))
	return nil
}

func writeZip(out, root string, items []packed) error {
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	zw.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) { return flate.NewWriter(w, flate.BestCompression) })
	for _, it := range items {
		h := &zip.FileHeader{Name: root + "/" + it.dest, Method: zip.Deflate, Modified: stamp()}
		h.SetMode(os.FileMode(it.mode))
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		if _, err := w.Write(it.data); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return f.Close()
}

func writeTarGz(out, root string, items []packed) error {
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewWriterLevel(f, gzip.BestCompression)
	if err != nil {
		return err
	}
	gz.Header.ModTime = time.Time{} // sin marca: reproducible
	gz.Header.OS = 255
	tw := tar.NewWriter(gz)
	hdr := func(h *tar.Header) *tar.Header {
		h.Uid, h.Gid, h.Uname, h.Gname = 0, 0, "root", "root"
		h.ModTime = stamp()
		h.Format = tar.FormatPAX
		return h
	}
	if err := tw.WriteHeader(hdr(&tar.Header{Typeflag: tar.TypeDir, Name: root + "/", Mode: 0o755})); err != nil {
		return err
	}
	for _, it := range items {
		if err := tw.WriteHeader(hdr(&tar.Header{Typeflag: tar.TypeReg, Name: root + "/" + it.dest, Mode: it.mode, Size: int64(len(it.data))})); err != nil {
			return err
		}
		if _, err := tw.Write(it.data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return f.Close()
}

func verifyArchive(format, out, root, goos, goarch string, items []packed) error {
	type got struct {
		mode int64
		data []byte
	}
	seen := map[string]got{}
	add := func(name string, mode int64, data []byte) error {
		if strings.Contains(name, `\`) {
			return fmt.Errorf("ruta con «\\»: %q", name)
		}
		if !strings.HasPrefix(name, root+"/") {
			return fmt.Errorf("fuera de la carpeta raíz %q: %q", root, name)
		}
		if _, dup := seen[name]; dup {
			return fmt.Errorf("entrada duplicada: %q", name)
		}
		seen[name] = got{mode, data}
		return nil
	}
	if format == "zip" {
		zr, err := zip.OpenReader(out)
		if err != nil {
			return err
		}
		defer zr.Close()
		for _, zf := range zr.File {
			rc, err := zf.Open()
			if err != nil {
				return err
			}
			b, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return err
			}
			if err := add(zf.Name, int64(zf.Mode().Perm()), b); err != nil {
				return err
			}
		}
	} else {
		f, err := os.Open(out)
		if err != nil {
			return err
		}
		defer f.Close()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		tr := tar.NewReader(gz)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			if h.Typeflag == tar.TypeDir {
				if h.Name != root+"/" || h.Mode != 0o755 {
					return fmt.Errorf("carpeta inesperada %q modo %o", h.Name, h.Mode)
				}
				continue
			}
			b, err := io.ReadAll(tr)
			if err != nil {
				return err
			}
			if err := add(h.Name, h.Mode, b); err != nil {
				return err
			}
		}
	}
	if len(seen) != len(items) {
		return fmt.Errorf("tiene %d archivos y se esperaban %d", len(seen), len(items))
	}
	for _, it := range items {
		name := root + "/" + it.dest
		g, ok := seen[name]
		if !ok {
			return fmt.Errorf("falta %s", name)
		}
		if g.mode != it.mode {
			return fmt.Errorf("%s: modo %o y se esperaba %o", name, g.mode, it.mode)
		}
		if sha256.Sum256(g.data) != sha256.Sum256(it.data) {
			return fmt.Errorf("%s: el contenido leído no coincide con el origen", name)
		}
		if it.bin {
			if err := checkBinary(g.data, goos, goarch, name); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkBinary exige que el ejecutable sea del sistema y la arquitectura pedidos (encabezado PE o ELF).
func checkBinary(b []byte, goos, goarch, what string) error {
	if goos == "windows" {
		want := map[string]uint16{"amd64": 0x8664, "arm64": 0xAA64, "386": 0x14C}[goarch]
		if len(b) < 0x40 || b[0] != 'M' || b[1] != 'Z' {
			return fmt.Errorf("%s no es un ejecutable de Windows (PE)", what)
		}
		pe := int(binary.LittleEndian.Uint32(b[0x3C:]))
		if pe+6 > len(b) || string(b[pe:pe+4]) != "PE\x00\x00" {
			return fmt.Errorf("%s: encabezado PE inválido", what)
		}
		if m := binary.LittleEndian.Uint16(b[pe+4:]); m != want {
			return fmt.Errorf("%s: PE de máquina 0x%X y se esperaba 0x%X (%s)", what, m, want, goarch)
		}
		return nil
	}
	want := map[string]uint16{"amd64": 62, "arm64": 183}[goarch]
	if len(b) < 20 || !bytes.HasPrefix(b, []byte("\x7fELF")) {
		return fmt.Errorf("%s no es un ejecutable de Linux (ELF)", what)
	}
	if m := binary.LittleEndian.Uint16(b[18:]); m != want {
		return fmt.Errorf("%s: ELF de máquina %d y se esperaba %d (%s)", what, m, want, goarch)
	}
	return nil
}

// ---------------------------------------------------------------------------------------------------------
// SHA256SUMS

func cmdSums(args []string) error {
	fs := flag.NewFlagSet("sums", flag.ContinueOnError)
	version := fs.String("version", "", "versión (por defecto, VERSION)")
	dist := fs.String("dist", "dist", "carpeta de artefactos")
	verify := fs.Bool("verify", false, "solo comprobar SHA256SUMS")
	if err := fs.Parse(args); err != nil {
		return err
	}
	sumsPath := filepath.Join(*dist, "SHA256SUMS")
	if *verify {
		return verifySums(*dist, sumsPath)
	}
	if *version == "" {
		v, err := readVersion()
		if err != nil {
			return err
		}
		*version = v
	}
	ents, err := os.ReadDir(*dist)
	if err != nil {
		return err
	}
	re := regexp.MustCompile(`^(dConnector|dConnector-server|echo-server)-` + regexp.QuoteMeta(*version) + `-(x64|x86|arm64|portable|windows|linux)(-[A-Za-z0-9]+)*\.(msi|zip|tar\.gz|exe)$`)
	var names []string
	for _, e := range ents {
		if !e.IsDir() && re.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return fmt.Errorf("no hay artefactos de la versión %s en %s", *version, *dist)
	}
	sort.Strings(names)
	var sb strings.Builder
	for _, n := range names {
		h, err := fileSum(filepath.Join(*dist, n))
		if err != nil {
			return err
		}
		fmt.Fprintf(&sb, "%s  %s\n", h, n)
	}
	if err := os.WriteFile(sumsPath, []byte(sb.String()), 0o644); err != nil {
		return err
	}
	fmt.Printf("Creado: %s (%d artefactos)\n", sumsPath, len(names))
	return verifySums(*dist, sumsPath)
}

func fileSum(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func verifySums(dist, sumsPath string) error {
	b, err := os.ReadFile(sumsPath)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	prev := ""
	for _, ln := range lines {
		m := regexp.MustCompile(`^([0-9a-f]{64})  ([^/\\ ]+)$`).FindStringSubmatch(ln)
		if m == nil {
			return fmt.Errorf("línea inválida en SHA256SUMS: %q", ln)
		}
		if m[2] <= prev {
			return fmt.Errorf("SHA256SUMS no está en orden estable en %q", m[2])
		}
		prev = m[2]
		got, err := fileSum(filepath.Join(dist, m[2]))
		if err != nil {
			return err
		}
		if got != m[1] {
			return fmt.Errorf("el hash de %s no coincide", m[2])
		}
	}
	fmt.Printf("SHA256SUMS verificado (%d archivos).\n", len(lines))
	return nil
}

// ---------------------------------------------------------------------------------------------------------
// Docker

func cmdDocker(args []string) error {
	fs := flag.NewFlagSet("docker", flag.ContinueOnError)
	version := fs.String("version", "", "versión (por defecto, VERSION)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *version == "" {
		v, err := readVersion()
		if err != nil {
			return err
		}
		*version = v
	}
	if _, err := exec.LookPath("docker"); err != nil {
		fmt.Println("AVISO: docker no está instalado; se omite la imagen.")
		return nil
	}
	if out, err := exec.Command("docker", "info", "--format", "{{.ServerVersion}}").CombinedOutput(); err != nil {
		fmt.Printf("AVISO: el demonio de Docker no responde; se omite la imagen. (%s)\n", strings.TrimSpace(string(out)))
		return nil
	}
	tag := "dconnector-server:" + *version
	run := func(name string, a ...string) (string, error) {
		c := exec.Command(name, a...)
		var buf bytes.Buffer
		c.Stdout, c.Stderr = &buf, &buf
		err := c.Run()
		return buf.String(), err
	}
	build := exec.Command("docker", "build", "-f", "deploy/Dockerfile", "--build-arg", "VERSION="+*version, "-t", tag, ".")
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("docker build: %w", err)
	}
	fmt.Println("Imagen construida:", tag, "(no se publica)")

	if out, err := run("docker", "run", "--rm", tag, "--version"); err != nil || strings.TrimSpace(out) != *version {
		return fmt.Errorf("`--version` de la imagen dio %q (se esperaba %s): %v", strings.TrimSpace(out), *version, err)
	}
	pw := make([]byte, 12)
	_, _ = rand.Read(pw)
	name := fmt.Sprintf("dconnector-check-%d", os.Getpid())
	if out, err := run("docker", "run", "-d", "--name", name, "-e", "DCONNECTOR_ADMIN_PASSWORD=Prueba-"+hex.EncodeToString(pw), tag); err != nil {
		return fmt.Errorf("docker run: %v: %s", err, out)
	}
	defer func() { _, _ = run("docker", "rm", "-f", name) }()
	deadline := time.Now().Add(45 * time.Second)
	for {
		out, err := run("docker", "exec", name, "/dconnector-server", "healthcheck")
		if err == nil {
			fmt.Println("healthcheck de la imagen: OK")
			return nil
		}
		if time.Now().After(deadline) {
			logs, _ := run("docker", "logs", "--tail", "30", name)
			return fmt.Errorf("el healthcheck de la imagen no respondió en 45 s: %s\n%s", strings.TrimSpace(out), logs)
		}
		time.Sleep(time.Second)
	}
}

var _ = path.Join // (rutas de archivo comprimido usan «/» literal)
