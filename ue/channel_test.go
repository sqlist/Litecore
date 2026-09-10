package main

import (
	"math/rand"
	"testing"
)

func TestStableAndDegradingScenarios(t *testing.T) {
	stable := NewScenarioChannelModel("stable").Sample(rand.New(rand.NewSource(42)))
	bad := NewScenarioChannelModel("degrading").Sample(rand.New(rand.NewSource(42)))
	if stable.SignalPower <= -110 || stable.SINR <= 0 {
		t.Fatalf("stable sample should pass: %+v", stable)
	}
	if bad.SignalPower >= stable.SignalPower {
		t.Fatalf("degrading should be weaker: stable=%+v bad=%+v", stable, bad)
	}
}

type fixedChannelModel struct {
	sample ChannelSample
}

func (m fixedChannelModel) Sample(*rand.Rand) ChannelSample { return m.sample }

func TestChannelModelCanBeReplaced(t *testing.T) {
	var model ChannelModel = fixedChannelModel{sample: ChannelSample{SignalPower: -95, SINR: 7}}
	got := model.Sample(rand.New(rand.NewSource(1)))
	if got.SignalPower != -95 || got.SINR != 7 {
		t.Fatalf("unexpected replacement model sample: %+v", got)
	}
}
