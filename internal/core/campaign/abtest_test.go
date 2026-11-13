package campaign

import (
	"fmt"
	"reflect"
	"testing"
)

func TestABSampleTakesSamePercentPerVariant(t *testing.T) {
	var assigned []Assignment
	variantOf := map[string]int{}
	for i := 0; i < 31; i++ {
		id := fmt.Sprintf("c%d", i)
		assigned = append(assigned, Assignment{ID: id, Variant: i % 3})
		variantOf[id] = i % 3
	}
	s := ABSample(assigned, 20)
	per := [3]int{}
	for id := range s {
		per[variantOf[id]]++
	}
	if per != [3]int{3, 2, 2} {
		t.Errorf("por variante = %v", per)
	}
	// Son los primeros de cada variante, en el orden dado.
	if !s["c0"] || !s["c3"] || !s["c6"] || s["c9"] {
		t.Errorf("no tomó los primeros: %v", s)
	}
	if got := ABSample([]Assignment{{ID: "a", Variant: 0}}, 5); len(got) != 1 {
		t.Errorf("al menos 1: %v", got)
	}
	if got := ABSample(nil, 20); len(got) != 0 {
		t.Errorf("vacío: %v", got)
	}
}

func TestPickWinnersAndScore(t *testing.T) {
	stats := []VariantStat{
		{Variant: 0, Sent: 10, Read: 8, Responded: 1},
		{Variant: 1, Sent: 10, Read: 5, Responded: 3},
		{Variant: 2, Sent: 10, Read: 9, Responded: 3, OptOuts: 2},
	}
	if got := PickWinners(stats, 2); !reflect.DeepEqual(got, []int{1, 0}) {
		t.Errorf("ganadoras = %v", got)
	}
	if got := VariantScore(VariantStat{}); got != -1 {
		t.Errorf("sin envíos = %v", got)
	}
	// Las que no enviaron no compiten; a igualdad, la de letra menor.
	tie := []VariantStat{{Variant: 3, Sent: 5, Responded: 1}, {Variant: 1, Sent: 5, Responded: 1}, {Variant: 2}}
	if got := PickWinners(tie, 5); !reflect.DeepEqual(got, []int{1, 3}) {
		t.Errorf("empate = %v", got)
	}
	if got := PickWinners(stats, 0); len(got) != 1 {
		t.Errorf("mínimo una: %v", got)
	}
	if got := PickWinners(nil, 2); len(got) != 0 {
		t.Errorf("sin datos: %v", got)
	}
}

func TestNormalizeABConfig(t *testing.T) {
	got := NormalizeABConfig(ABConfig{SamplePct: 90, WaitHours: 0, Winners: 5})
	if got != (ABConfig{SamplePct: 50, WaitHours: 1, Winners: 2}) {
		t.Errorf("normalizada = %+v", got)
	}
	if NormalizeABConfig(ABDefaults) != ABDefaults {
		t.Error("los valores por defecto ya son válidos")
	}
}

func TestParseABConfig(t *testing.T) {
	if ParseABConfig("") != nil || ParseABConfig("no-json") != nil {
		t.Error("vacío o dañado debe dar nil")
	}
	c := ParseABConfig(`{"samplePct":30}`)
	if c == nil || *c != (ABConfig{SamplePct: 30, WaitHours: 4, Winners: 2}) {
		t.Errorf("config = %+v", c)
	}
}
