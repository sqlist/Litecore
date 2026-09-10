package main

import (
	"math/rand"
	"strings"
)

type ChannelSample struct{ SignalPower, SINR float32 }

// ChannelModel is the replaceable boundary of the UE-side channel model.
// A model only produces radio measurements; admission thresholds belong to AMF.
type ChannelModel interface {
	Sample(rng *rand.Rand) ChannelSample
}

// ScenarioChannelModel preserves the CLI's stable/edge/degrading/mixed model.
// Another implementation can be injected without changing registration logic.
type ScenarioChannelModel struct {
	Scenario string
}

func NewScenarioChannelModel(scenario string) ChannelModel {
	return ScenarioChannelModel{Scenario: scenario}
}

func (m ScenarioChannelModel) Sample(rng *rand.Rand) ChannelSample {
	switch strings.ToLower(m.Scenario) {
	case "stable":
		return ChannelSample{SignalPower: float32(-82 + rng.NormFloat64()*2), SINR: float32(18 + rng.NormFloat64()*1.5)}
	case "edge":
		return ChannelSample{SignalPower: float32(-109 + rng.NormFloat64()*4), SINR: float32(2 + rng.NormFloat64()*2)}
	case "degrading":
		return ChannelSample{SignalPower: float32(-116 + rng.NormFloat64()*3), SINR: float32(-1 + rng.NormFloat64()*2)}
	default: // mixed: 70%稳定、20%边缘、10%恶化
		value := rng.Float64()
		if value < .7 {
			return ScenarioChannelModel{Scenario: "stable"}.Sample(rng)
		}
		if value < .9 {
			return ScenarioChannelModel{Scenario: "edge"}.Sample(rng)
		}
		return ScenarioChannelModel{Scenario: "degrading"}.Sample(rng)
	}
}
