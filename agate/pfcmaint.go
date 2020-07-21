// Copyright (c) 2020, The Emergent Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package agate

import (
	"fmt"

	"github.com/chewxy/math32"
	"github.com/emer/leabra/leabra"
	"github.com/goki/ki/kit"
)

// PFCMaintLayer is the base layer type for BGate framework.
// Adds a dopamine variable to base Leabra layer type.
type PFCMaintLayer struct {
	leabra.PFCMaintLayer
	DA float32 `inactive:"+" desc:"dopamine value for this layer"`
}

var KiT_PFCMaintLayer = kit.Types.AddType(&PFCMaintLayer{}, leabra.PFCMaintLayerProps)

// DAPFCMaintLayer interface:

func (ly *PFCMaintLayer) GetDA() float32   { return ly.DA }
func (ly *PFCMaintLayer) SetDA(da float32) { ly.DA = da }

// UnitVarIdx returns the index of given variable within the Neuron,
// according to UnitVarNames() list (using a map to lookup index),
// or -1 and error message if not found.
func (ly *PFCMaintLayer) UnitVarIdx(varNm string) (int, error) {
	vidx, err := ly.PFCMaintLayer.UnitVarIdx(varNm)
	if err == nil {
		return vidx, err
	}
	if varNm != "DA" {
		return -1, fmt.Errorf("pcore.NeuronVars: variable named: %s not found", varNm)
	}
	nn := len(leabra.NeuronVars)
	return nn, nil
}

// UnitVal1D returns value of given variable index on given unit, using 1-dimensional index.
// returns NaN on invalid index.
// This is the core unit var access method used by other methods,
// so it is the only one that needs to be updated for derived layer types.
func (ly *PFCMaintLayer) UnitVal1D(varIdx int, idx int) float32 {
	nn := len(leabra.NeuronVars)
	if varIdx < 0 || varIdx > nn {
		return math32.NaN()
	}
	if varIdx < nn {
		return ly.PFCMaintLayer.UnitVal1D(varIdx, idx)
	}
	if idx < 0 || idx >= len(ly.Neurons) {
		return math32.NaN()
	}
	if varIdx != nn {
		return math32.NaN()
	}
	return ly.DA
}

func (ly *PFCMaintLayer) InitActs() {
	ly.PFCMaintLayer.InitActs()
	ly.DA = 0
}
