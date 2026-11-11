package httpapi

import (
	"math"

	"github.com/xmz14/lll/backend-go/internal/cloud/store"
)

type allocationDiagnostic struct {
	Status string   `json:"status"`
	PValue *float64 `json:"pValue,omitempty"`
}

func checkExperimentAllocation(treatmentPercent int, counts store.ExperimentCohort) allocationDiagnostic {
	switch {
	case counts.Both > 0:
		return allocationDiagnostic{Status: "crossover"}
	case counts.ControlOnly == 0 && counts.TreatmentOnly == 0:
		return allocationDiagnostic{Status: "no_observations"}
	case treatmentPercent == 0 || treatmentPercent == 100:
		if (treatmentPercent == 0 && counts.TreatmentOnly > 0) || (treatmentPercent == 100 && counts.ControlOnly > 0) {
			return allocationDiagnostic{Status: "mismatch"}
		}
		return allocationDiagnostic{Status: "single_arm"}
	}

	control, treatment := float64(counts.ControlOnly), float64(counts.TreatmentOnly)
	expectedControl := (control + treatment) * float64(100-treatmentPercent) / 100
	expectedTreatment := (control + treatment) * float64(treatmentPercent) / 100
	if expectedControl < 10 || expectedTreatment < 10 {
		return allocationDiagnostic{Status: "insufficient"}
	}
	// The two-arm Pearson chi-squared statistic has one degree of freedom.
	difference := treatment - expectedTreatment
	chiSquared := difference * difference * (1/expectedControl + 1/expectedTreatment)
	p := math.Erfc(math.Sqrt(chiSquared / 2))
	status := "no_mismatch_detected"
	if p < .001 {
		status = "mismatch"
	}
	return allocationDiagnostic{Status: status, PValue: &p}
}
