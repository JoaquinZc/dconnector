package campaign

import (
	"testing"

	"dconnector/internal/core/model"
)

var testSending = SendingSettings{MinDelay: 20, MaxDelay: 40, DailyLimit: 200, ColdMinDelay: 60, ColdMaxDelay: 120, ColdDailyLimit: 20, TypingSpeed: 0}

func TestCountRisks(t *testing.T) {
	got := CountRisks([]string{"a", "b", "c", "d"}, map[string]Risk{"a": RiskWarm, "b": RiskCold, "c": RiskLukewarm})
	if got[RiskWarm] != 1 || got[RiskLukewarm] != 1 || got[RiskCold] != 2 {
		t.Errorf("conteo = %v", got)
	}
}

func TestWarmest(t *testing.T) {
	if Warmest(RiskCold, RiskWarm) != RiskWarm || Warmest(RiskLukewarm, RiskCold) != RiskLukewarm || Warmest(RiskWarm, RiskWarm) != RiskWarm {
		t.Error("Warmest")
	}
	if RiskRank(RiskWarm) >= RiskRank(RiskLukewarm) || RiskRank(RiskLukewarm) >= RiskRank(RiskCold) {
		t.Error("RiskRank")
	}
}

func TestEstimateOutreach(t *testing.T) {
	r := EstimateOutreach(map[Risk]int{RiskWarm: 10, RiskCold: 50}, 100, testSending)
	if r.Seconds != 10*30+50*90 {
		t.Errorf("segundos = %d", r.Seconds)
	}
	if r.Days != 3 { // 50 fríos a 20 por día
		t.Errorf("días = %d", r.Days)
	}
	if d := EstimateOutreach(map[Risk]int{RiskWarm: 450}, 0, testSending).Days; d != 3 {
		t.Errorf("límite diario: %d", d)
	}
	if d := EstimateOutreach(map[Risk]int{}, 0, testSending).Days; d != 0 {
		t.Errorf("sin destinatarios: %d", d)
	}
}

func TestOutreachWarnings(t *testing.T) {
	fast := SendingSettings{MinDelay: 10, ColdMinDelay: 10, ColdDailyLimit: 0}
	if got := OutreachWarnings(map[Risk]int{RiskWarm: 1, RiskCold: 9}, fast); len(got) != 4 {
		t.Errorf("avisos = %v", got)
	}
	if got := OutreachWarnings(map[Risk]int{RiskWarm: 10}, testSending); len(got) != 0 {
		t.Errorf("avisos = %v", got)
	}
	if got := OutreachWarnings(map[Risk]int{}, fast); len(got) != 0 {
		t.Errorf("sin destinatarios: %v", got)
	}
	got := OutreachWarnings(map[Risk]int{RiskWarm: 1, RiskCold: 9}, testSending)
	if len(got) != 1 || got[0] != "90 % de los destinatarios nunca ha hablado contigo. Es el principal motivo de bloqueo." {
		t.Errorf("avisos = %v", got)
	}
}

func TestSendingFromPacing(t *testing.T) {
	s := SendingFromPacing(model.DefaultPacing)
	if s.MinDelay != 20 || s.MaxDelay != 45 || s.ColdMinDelay != 60 || s.ColdMaxDelay != 120 ||
		s.DailyLimit != 200 || s.ColdDailyLimit != 20 || s.TypingSpeed != 300 {
		t.Errorf("ajustes = %+v", s)
	}
}
