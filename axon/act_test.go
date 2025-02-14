// Copyright (c) 2019, The Emergent Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package axon

import (
	"testing"

	"github.com/goki/mat32"
)

// difTol is the numerical difference tolerance for comparing vs. target values
const difTol = float32(1.0e-8)

func TestActUpdt(t *testing.T) {
	geinc := []float32{.01, .02, .03, .04, .05, .1, .2, .3, .2}
	corge := []float32{0.01, 0.038, 0.090399995, 0.17232, 0.28785598, 0.48028478, 0.8342278, 1.4173822, 2.0839057}
	ge := make([]float32, len(geinc))
	corinet := []float32{0.006738626, 0.024927208, 0.056907285, 0.1016768, 0.1538688, 0.22306465, 0.42970985, 0, 0}
	inet := make([]float32, len(geinc))
	corvm := []float32{0.3023981, 0.311269, 0.33152068, 0.3677046, 0.42246217, 0.5018446, 0.65476626, 0.3, 0.3}
	vm := make([]float32, len(geinc))
	corspk := []float32{0, 0, 0, 0, 0, 0, 0, 1, 0}
	spk := make([]float32, len(geinc))
	coract := []float32{0, 0, 0, 0, 0, 0, 0, 0, 0}
	act := make([]float32, len(geinc))

	ac := ActParams{}
	ac.Defaults()
	ac.Gbar.L = 0.2 // correct default

	nrn := &Neuron{}
	ac.InitActs(nrn)

	for i := range geinc {
		nrn.GeRaw += geinc[i]
		ac.GeFmRaw(nrn, nrn.GeRaw, 1, 0.5)
		ac.GiFmRaw(nrn, nrn.GiRaw)
		ac.VmFmG(nrn)
		ac.ActFmG(nrn)
		ge[i] = nrn.Ge
		inet[i] = nrn.Inet
		vm[i] = nrn.Vm
		spk[i] = nrn.Spike
		act[i] = nrn.Act
		difge := mat32.Abs(ge[i] - corge[i])
		if difge > difTol { // allow for small numerical diffs
			t.Errorf("ge err: idx: %v, geinc: %v, ge: %v, corge: %v, dif: %v\n", i, geinc[i], ge[i], corge[i], difge)
		}
		difinet := mat32.Abs(inet[i] - corinet[i])
		if difinet > difTol { // allow for small numerical diffs
			t.Errorf("Inet err: idx: %v, geinc: %v, inet: %v, corinet: %v, dif: %v\n", i, geinc[i], inet[i], corinet[i], difinet)
		}
		difvm := mat32.Abs(vm[i] - corvm[i])
		if difvm > difTol { // allow for small numerical diffs
			t.Errorf("Vm err: idx: %v, geinc: %v, vm: %v, corvm: %v, dif: %v\n", i, geinc[i], vm[i], corvm[i], difvm)
		}
		difspk := mat32.Abs(spk[i] - corspk[i])
		if difspk > difTol { // allow for small numerical diffs
			t.Errorf("Spk err: idx: %v, geinc: %v, spk: %v, corspk: %v, dif: %v\n", i, geinc[i], spk[i], corspk[i], difspk)
		}
		difact := mat32.Abs(act[i] - coract[i])
		if difact > difTol { // allow for small numerical diffs
			t.Errorf("Act err: idx: %v, geinc: %v, act: %v, coract: %v, dif: %v\n", i, geinc[i], act[i], coract[i], difact)
		}
	}
	// fmt.Printf("ge vals: %v\n", ge)
	// fmt.Printf("Inet vals: %v\n", inet)
	// fmt.Printf("vm vals: %v\n", vm)
	// fmt.Printf("act vals: %v\n", act)
}
