package campaign

import (
	"reflect"
	"strings"
	"testing"
)

var ana = TemplateContact{
	FullName:    "Ana Pérez",
	PhoneNumber: "593990000001",
	Metadata:    map[string]string{"empresa": "ACME", "mensaje_new_year": "Feliz año", "deuda": "120"},
}

func TestNormalizeKey(t *testing.T) {
	if got := NormalizeKey("Teléfono"); got != "telefono" {
		t.Errorf("Teléfono → %q", got)
	}
	if got := NormalizeKey("  Mensaje new year "); got != "mensaje_new_year" {
		t.Errorf("Mensaje new year → %q", got)
	}
	if got := NormalizeKey("AÑO Ñandú"); got != "ano_nandu" {
		t.Errorf("AÑO Ñandú → %q", got)
	}
}

func TestRenderTemplateSubstitutes(t *testing.T) {
	r := RenderTemplate("Hola {{Nombre}} ({{ Teléfono }}) de {{EMPRESA}}: {{Mensaje new year}}, debes {{deuda}}", ana, nil)
	if want := "Hola Ana Pérez (593990000001) de ACME: Feliz año, debes 120"; r.Text != want {
		t.Errorf("texto = %q, quería %q", r.Text, want)
	}
	if len(r.Missing) != 0 {
		t.Errorf("missing = %v", r.Missing)
	}
}

func TestRenderTemplateMissingTag(t *testing.T) {
	r := RenderTemplate("Hola {{nombre}}, te esperamos en {{ciudad}}.", ana, nil)
	if want := "Hola Ana Pérez, te esperamos en."; r.Text != want {
		t.Errorf("texto = %q, quería %q", r.Text, want)
	}
	if strings.Contains(r.Text, "{{") {
		t.Error("quedó la etiqueta literal")
	}
	if !reflect.DeepEqual(r.Missing, []string{"ciudad"}) {
		t.Errorf("missing = %v", r.Missing)
	}
}

func TestRenderTemplateDefaultValue(t *testing.T) {
	r := RenderTemplate("Te esperamos en {{ciudad|tu ciudad}}, {{empresa|cliente}}", ana, nil)
	if want := "Te esperamos en tu ciudad, ACME"; r.Text != want {
		t.Errorf("texto = %q, quería %q", r.Text, want)
	}
	if len(r.Missing) != 0 {
		t.Errorf("missing = %v", r.Missing)
	}
	// Un valor por defecto vacío también cuenta como valor.
	if r := RenderTemplate("a{{x|}}b", ana, nil); r.Text != "ab" || len(r.Missing) != 0 {
		t.Errorf("default vacío: %q %v", r.Text, r.Missing)
	}
}

func TestRenderTemplateCleansGaps(t *testing.T) {
	if got := RenderTemplate("Hola {{apodo}} , {{nombre}}  !", ana, nil).Text; got != "Hola, Ana Pérez!" {
		t.Errorf("texto = %q", got)
	}
	if got := RenderTemplate("Hola {{nombre}}\n\nSaludos", ana, nil).Text; got != "Hola Ana Pérez\n\nSaludos" {
		t.Errorf("saltos de línea: %q", got)
	}
}

func TestRenderTemplateWithoutMetadata(t *testing.T) {
	if got := RenderTemplate("Hola {{nombre}}", TemplateContact{FullName: "Beto"}, nil).Text; got != "Hola Beto" {
		t.Errorf("texto = %q", got)
	}
}

func TestExtractTags(t *testing.T) {
	got := ExtractTags("{{Nombre}} {{nombre}} {{Ciudad|x}} {{ Teléfono }}")
	if want := []string{"nombre", "ciudad", "telefono"}; !reflect.DeepEqual(got, want) {
		t.Errorf("tags = %v, quería %v", got, want)
	}
	if got := ExtractTags("sin etiquetas"); len(got) != 0 {
		t.Errorf("tags = %v", got)
	}
}

func TestFirstName(t *testing.T) {
	cases := map[string]string{
		"ING. JUAN PABLO PÉREZ": "Juan",
		"Dra. maría López":      "María",
		"JuanPa Rodríguez":      "JuanPa",
		"  ":                    "",
		"Sr. Lic.":              "",
		"don Pedro":             "Pedro",
		"McDonald":              "McDonald",
	}
	for in, want := range cases {
		if got := FirstName(in); got != want {
			t.Errorf("FirstName(%q) = %q, quería %q", in, got, want)
		}
	}
}

func TestFieldsFromContact(t *testing.T) {
	c := TemplateContact{FullName: "Ing. Juan Pérez", PhoneNumber: "593990000001", Metadata: map[string]string{"Ciudad": "Quito"}}
	got := RenderTemplate("Hola {{primer_nombre}} de {{Ciudad}}, tu número {{Teléfono}}", c, nil).Text
	if want := "Hola Juan de Quito, tu número 593990000001"; got != want {
		t.Errorf("texto = %q, quería %q", got, want)
	}
}

func TestTrato(t *testing.T) {
	c := TemplateContact{FullName: "Ana Pérez"}
	general := TratoSettings("amigo/amiga", "nombre")
	if got := RenderTemplate("Hola {{trato}}", c, general).Text; got != "Hola Ana" {
		t.Errorf("modo nombre: %q", got)
	}
	general = TratoSettings("amigo/amiga", "general")
	c.SalutationForm = "F"
	if got := RenderTemplate("Hola {{trato}}", c, general).Text; got != "Hola amiga" {
		t.Errorf("modo general F: %q", got)
	}
	c.SalutationForm = "M"
	if got := RenderTemplate("Hola {{trato}}", c, general).Text; got != "Hola amigo" {
		t.Errorf("modo general M: %q", got)
	}
	c.Nickname = "JuanPa"
	if got := RenderTemplate("Hola {{trato}} / {{apodo}}", c, general).Text; got != "Hola JuanPa / JuanPa" {
		t.Errorf("apodo: %q", got)
	}
}

func TestBuildVariablesFixedFieldsWin(t *testing.T) {
	v := BuildVariables(TemplateContact{FullName: " Ana ", PhoneNumber: "1", Metadata: map[string]string{"Nombre": "Otro", "Edad": " 30 "}})
	if v["nombre"] != "Ana" || v["telefono"] != "1" || v["edad"] != "30" || v["primer_nombre"] != "Ana" {
		t.Errorf("variables = %v", v)
	}
	if _, ok := v["apodo"]; ok {
		t.Error("no debería haber apodo")
	}
}
