// Copyright (c) 2020, The Emergent Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package deep

import (
	"fmt"

	"github.com/emer/axon/axon"
	"github.com/goki/ki/kit"
	"github.com/goki/mat32"
)

// CTLayer implements the corticothalamic projecting layer 6 deep neurons
// that project to the TRC pulvinar neurons, to generate the predictions.
// They receive phasic input representing 5IB bursting via CTCtxtPrjn inputs
// from SuperLayer and also from self projections.
type CTLayer struct {
	axon.Layer           // access as .Layer
	CtxtGeGain float32   `def:"0.2" desc:"gain factor for context excitatory input, which is constant as compared to the spiking input from other projections, so it must be downscaled accordingly"`
	CtxtGes    []float32 `desc:"slice of context (temporally delayed) excitatory conducances."`
}

var KiT_CTLayer = kit.Types.AddType(&CTLayer{}, LayerProps)

func (ly *CTLayer) Defaults() {
	ly.Layer.Defaults()
	ly.Act.Decay.Act = 0 // deep doesn't decay!
	ly.Act.Decay.Glong = 0
	ly.Act.Decay.KNa = 0
	ly.Typ = CT
	ly.CtxtGeGain = 0.2
}

func (ly *CTLayer) Class() string {
	return "CT " + ly.Cls
}

// Build constructs the layer state, including calling Build on the projections.
func (ly *CTLayer) Build() error {
	err := ly.Layer.Build()
	if err != nil {
		return err
	}
	ly.CtxtGes = make([]float32, len(ly.Neurons))
	return nil
}

func (ly *CTLayer) InitActs() {
	ly.Layer.InitActs()
	for ni := range ly.CtxtGes {
		ly.CtxtGes[ni] = 0
	}
}

// GFmInc integrates new synaptic conductances from increments sent during last SendGDelta.
func (ly *CTLayer) GFmInc(ltime *axon.Time) {
	cyc := ltime.Cycle // for bursting
	if ly.IsTarget() {
		cyc = ltime.PhaseCycle
	}
	ly.RecvGInc(ltime)
	for ni := range ly.Neurons {
		nrn := &ly.Neurons[ni]
		if nrn.IsOff() {
			continue
		}

		geRaw := nrn.GeRaw + ly.CtxtGeGain*ly.CtxtGes[ni]

		nrn.NMDA = ly.Act.NMDA.NMDA(nrn.NMDA, geRaw, nrn.NMDASyn)
		nrn.Gnmda = ly.Act.NMDA.Gnmda(nrn.NMDA, nrn.VmDend)
		// note: GABAB integrated in ActFmG one timestep behind, b/c depends on integrated Gi inhib

		// note: each step broken out here so other variants can add extra terms to Raw
		ly.Act.GeFmRaw(nrn, geRaw, nrn.Gnmda, cyc, nrn.ActM)
		nrn.GeRaw = 0
		ly.Act.GiFmRaw(nrn, nrn.GiRaw)
		nrn.GiRaw = 0
	}
}

// SendCtxtGe sends activation over CTCtxtPrjn projections to integrate
// CtxtGe excitatory conductance on CT layers.
// This should be called at the end of the 5IB Bursting phase via Network.CTCtxt
// Satisfies the CtxtSender interface.
func (ly *CTLayer) SendCtxtGe(ltime *axon.Time) {
	for ni := range ly.Neurons {
		nrn := &ly.Neurons[ni]
		if nrn.IsOff() {
			continue
		}
		if nrn.Act > 0.1 {
			for _, sp := range ly.SndPrjns {
				if sp.IsOff() {
					continue
				}
				ptyp := sp.Type()
				if ptyp != CTCtxt {
					continue
				}
				pj, ok := sp.(*CTCtxtPrjn)
				if !ok {
					continue
				}
				pj.SendCtxtGe(ni, nrn.Act)
			}
		}
	}
}

// CtxtFmGe integrates new CtxtGe excitatory conductance from projections, and computes
// overall Ctxt value, only on Deep layers.
// This should be called at the end of the 5IB Bursting phase via Network.CTCtxt
func (ly *CTLayer) CtxtFmGe(ltime *axon.Time) {
	for ni := range ly.CtxtGes {
		ly.CtxtGes[ni] = 0
	}
	for _, p := range ly.RcvPrjns {
		if p.IsOff() {
			continue
		}
		ptyp := p.Type()
		if ptyp != CTCtxt {
			continue
		}
		pj, ok := p.(*CTCtxtPrjn)
		if !ok {
			continue
		}
		pj.RecvCtxtGeInc()
	}
}

// UnitVarNames returns a list of variable names available on the units in this layer
func (ly *CTLayer) UnitVarNames() []string {
	return NeuronVarsAll
}

// UnitVarIdx returns the index of given variable within the Neuron,
// according to UnitVarNames() list (using a map to lookup index),
// or -1 and error message if not found.
func (ly *CTLayer) UnitVarIdx(varNm string) (int, error) {
	vidx, err := ly.Layer.UnitVarIdx(varNm)
	if err == nil {
		return vidx, err
	}
	if varNm != "CtxtGe" {
		return -1, fmt.Errorf("deep.CTLayer: variable named: %s not found", varNm)
	}
	nn := ly.Layer.UnitVarNum()
	return nn, nil
}

// UnitVal1D returns value of given variable index on given unit, using 1-dimensional index.
// returns NaN on invalid index.
// This is the core unit var access method used by other methods,
// so it is the only one that needs to be updated for derived layer types.
func (ly *CTLayer) UnitVal1D(varIdx int, idx int) float32 {
	nn := ly.Layer.UnitVarNum()
	if varIdx < 0 || varIdx > nn { // nn = CtxtGes
		return mat32.NaN()
	}
	if varIdx < nn {
		return ly.Layer.UnitVal1D(varIdx, idx)
	}
	if idx < 0 || idx >= len(ly.Neurons) {
		return mat32.NaN()
	}
	return ly.CtxtGes[idx]
}

// UnitVarNum returns the number of Neuron-level variables
// for this layer.  This is needed for extending indexes in derived types.
func (ly *CTLayer) UnitVarNum() int {
	return ly.Layer.UnitVarNum() + 1
}
