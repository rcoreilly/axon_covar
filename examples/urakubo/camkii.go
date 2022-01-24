// Copyright (c) 2021 The Emergent Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"

	"github.com/emer/emergent/chem"
	"github.com/emer/etable/etable"
	"github.com/emer/etable/etensor"
)

// CaMVars are intracellular Ca-driven signaling variables for the
// CaMKII+CaM binding -- each can have different numbers of Ca bound
// Dupont = DupontHouartDekonnick03, has W* terms used in Genesis code
// stores N values -- Co = Concentration computed by volume as needed
type CaMVars struct {
	CaM         float64 `desc:"CaM = Ca calmodulin, [0-3]Ca bound but unbound to CaMKII"`
	CaM_CaMKII  float64 `desc:"CaMKII-CaM bound together = WBn in Dupont"`
	CaM_CaMKIIP float64 `desc:"CaMKIIP-CaM bound together, P = phosphorylated at Thr286 = WTn in Dupont"`
	CaM_DAPK1   float64 `desc:"DAPK1-CaM bound together, de-phosphorylated at S308 by CaN -- this is the active form for GluN2B binding"`
	CaM_DAPK1P  float64 `desc:"DAPK1-CaM bound together, P = phosphorylated at S308 -- this is the inactive form for GluN2B binding"`
}

func (cs *CaMVars) Init(vol float64) {
	cs.Zero()
}

func (cs *CaMVars) Zero() {
	cs.CaM = 0
	cs.CaM_CaMKII = 0
	cs.CaM_CaMKIIP = 0
	cs.CaM_DAPK1 = 0
	cs.CaM_DAPK1P = 0
}

func (cs *CaMVars) Integrate(d *CaMVars) {
	chem.Integrate(&cs.CaM, d.CaM)
	chem.Integrate(&cs.CaM_CaMKII, d.CaM_CaMKII)
	chem.Integrate(&cs.CaM_CaMKIIP, d.CaM_CaMKIIP)
	chem.Integrate(&cs.CaM_DAPK1, d.CaM_DAPK1)
	chem.Integrate(&cs.CaM_DAPK1P, d.CaM_DAPK1P)
}

// AutoPVars hold the auto-phosphorylation variables, for CaMKII and DAPK1
type AutoPVars struct {
	Act   float64 `desc:"total active CaMKII"`
	Total float64 `desc:"total CaMKII across all states"`
	K     float64 `desc:"rate constant for further autophosphorylation as function of current state"`
}

func (av *AutoPVars) Zero() {
	av.Act = 0
	av.Total = 0
	av.K = 0
}

// CaMKIIVars are intracellular Ca-driven signaling states
// for CaMKII binding and phosphorylation with CaM + Ca
// Dupont = DupontHouartDekonnick03, has W* terms used in Genesis code
// stores N values -- Co = Concentration computed by volume as needed
type CaMKIIVars struct {
	Ca          [4]CaMVars `desc:"increasing levels of Ca binding, 0-3"`
	CaMKII      float64    `desc:"unbound CaMKII = CaM kinase II -- WI in Dupont -- this is the inactive form for NMDA GluN2B binding"`
	CaMKIIP     float64    `desc:"unbound CaMKII P = phosphorylated at Thr286 -- shown with * in Figure S13 = WA in Dupont -- this is the active form for NMDA GluN2B binding"`
	DAPK1       float64    `desc:"unbound DAPK1, de-phosphorylated at S308 by CaN -- this is the active form for NMDA GluN2B binding"`
	DAPK1P      float64    `desc:"unbound DAPK1, P = phosphorylated at S308 -- this is the inactive form for NMDA GluN2B binding"`
	PP1Thr286C  float64    `desc:"PP1+CaMKIIP complex for PP1Thr286 enzyme reaction"`
	PP2AThr286C float64    `desc:"PP2A+CaMKIIP complex for PP2AThr286 enzyme reaction"`
	CaNS308C    float64    `desc:"CaN+DAPK1P complex for CaNS308 enzyme reaction"`

	CaMKIIauto AutoPVars `view:"inline" inactive:"+" desc:"auto-phosphorylation state"`
	DAPK1auto  AutoPVars `view:"inline" inactive:"+" desc:"auto-phosphorylation state"`

	// todo: add competitive GluNRB binding for CaMKII and DAPK1
}

func (cs *CaMKIIVars) Init(vol float64) {
	for i := range cs.Ca {
		cs.Ca[i].Init(vol)
	}
	cs.Ca[0].CaM = chem.CoToN(80, vol)
	cs.CaMKII = chem.CoToN(20, vol)
	cs.CaMKIIP = 0 // WA
	cs.PP1Thr286C = 0
	cs.PP2AThr286C = 0

	cs.DAPK1 = chem.CoToN(20, vol) // total guess -- Goodell says "highly enriched"
	cs.DAPK1P = 0                  // assumption
	cs.CaNS308C = 0

	if InitBaseline {
		cs.Ca[0].CaM = chem.CoToN(78.31, vol)    // orig: 80
		cs.Ca[1].CaM = chem.CoToN(1.002, vol)    // orig: 0
		cs.Ca[2].CaM = chem.CoToN(0.006682, vol) // orig: 0
		cs.Ca[3].CaM = chem.CoToN(1.988-05, vol) // orig: 0
		cs.CaMKII = chem.CoToN(19.37, vol)       // orig: 20

		// todo DAPK1 baselines
	}

	cs.UpdtActive()
}

// Generate Code for Initializing
func (cs *CaMKIIVars) InitCode(vol float64, pre string) {
	for i := range cs.Ca {
		fmt.Printf("\tcs.%s.Ca[%d].CaM = chem.CoToN(%.4g, vol)\n", pre, i, chem.CoFmN(cs.Ca[i].CaM, vol))
		fmt.Printf("\tcs.%s.Ca[%d].CaM_CaMKII = chem.CoToN(%.4g, vol)\n", pre, i, chem.CoFmN(cs.Ca[i].CaM_CaMKII, vol))
		fmt.Printf("\tcs.%s.Ca[%d].CaM_CaMKIIP = chem.CoToN(%.4g, vol)\n", pre, i, chem.CoFmN(cs.Ca[i].CaM_CaMKIIP, vol))
		fmt.Printf("\tcs.%s.Ca[%d].CaM_DAPK1 = chem.CoToN(%.4g, vol)\n", pre, i, chem.CoFmN(cs.Ca[i].CaM_DAPK1, vol))
		fmt.Printf("\tcs.%s.Ca[%d].CaM_DAPK1P = chem.CoToN(%.4g, vol)\n", pre, i, chem.CoFmN(cs.Ca[i].CaM_DAPK1P, vol))
	}
	fmt.Printf("\tcs.%s.CaMKII = chem.CoToN(%.4g, vol)\n", pre, chem.CoFmN(cs.CaMKII, vol))
	fmt.Printf("\tcs.%s.CaMKIIP = chem.CoToN(%.4g, vol)\n", pre, chem.CoFmN(cs.CaMKIIP, vol))
	fmt.Printf("\tcs.%s.PP1Thr286C = chem.CoToN(%.4g, vol)\n", pre, chem.CoFmN(cs.PP1Thr286C, vol))
	fmt.Printf("\tcs.%s.PP2AThr286C = chem.CoToN(%.4g, vol)\n", pre, chem.CoFmN(cs.PP2AThr286C, vol))
	fmt.Printf("\tcs.%s.DAPK1 = chem.CoToN(%.4g, vol)\n", pre, chem.CoFmN(cs.DAPK1, vol))
	fmt.Printf("\tcs.%s.DAPK1P = chem.CoToN(%.4g, vol)\n", pre, chem.CoFmN(cs.DAPK1P, vol))
	fmt.Printf("\tcs.%s.CaNS308C = chem.CoToN(%.4g, vol)\n", pre, chem.CoFmN(cs.CaNS308C, vol))
}

func (cs *CaMKIIVars) Zero() {
	for i := range cs.Ca {
		cs.Ca[i].Zero()
	}
	cs.CaMKII = 0
	cs.CaMKIIP = 0
	cs.PP1Thr286C = 0
	cs.PP2AThr286C = 0
	cs.DAPK1 = 0
	cs.DAPK1P = 0
	cs.CaNS308C = 0
	cs.CaMKIIauto.Zero()
	cs.DAPK1auto.Zero()
}

func (cs *CaMKIIVars) Integrate(d *CaMKIIVars) {
	for i := range cs.Ca {
		cs.Ca[i].Integrate(&d.Ca[i])
	}
	chem.Integrate(&cs.CaMKII, d.CaMKII)
	chem.Integrate(&cs.CaMKIIP, d.CaMKIIP)
	chem.Integrate(&cs.PP1Thr286C, d.PP1Thr286C)
	chem.Integrate(&cs.PP2AThr286C, d.PP2AThr286C)
	chem.Integrate(&cs.DAPK1, d.DAPK1)
	chem.Integrate(&cs.DAPK1P, d.DAPK1P)
	chem.Integrate(&cs.CaNS308C, d.CaNS308C)
	cs.UpdtActive()
}

// UpdtActive updates active
func (cs *CaMKIIVars) UpdtActive() {
	cs.UpdtCaMKIIActive()
	cs.UpdtDAPK1Active()
}

// UpdtCaMKIIActive updates active, total, and the Kauto auto-phosphorylation rate constant
// Code is from genesis_customizing/T286Phos/T286Phos.c and would be impossible to
// reconstruct without that source (my first guess was wildy off, based only on
// the supplement)
func (cs *CaMKIIVars) UpdtCaMKIIActive() {
	WI := cs.CaMKII
	WA := cs.CaMKIIP

	var WB, WT float64

	for i := 0; i < 3; i++ {
		WB += cs.Ca[i].CaM_CaMKII
		WT += cs.Ca[i].CaM_CaMKIIP
	}
	WB += cs.Ca[3].CaM_CaMKII
	WP := cs.Ca[3].CaM_CaMKIIP

	TotalW := WI + WB + WP + WT + WA
	Wb := WB / TotalW
	Wp := WP / TotalW
	Wt := WT / TotalW
	Wa := WA / TotalW
	cb := 0.75
	ct := 0.8
	ca := 0.8

	T := Wb + Wp + Wt + Wa
	tmp := T * (-0.22 + 1.826*T + -0.8*T*T)
	tmp *= 0.75 * (cb*Wb + Wp + ct*Wt + ca*Wa)
	if tmp < 0 {
		tmp = 0
	}
	cs.CaMKIIauto.K = 0.29 * tmp
	cs.CaMKIIauto.Act = cb*WB + WP + ct*WT + ca*WA
	cs.CaMKIIauto.Total = T
}

// UpdtDAPK1Active updates DAPK1
func (cs *CaMKIIVars) UpdtDAPK1Active() {
	WI := cs.DAPK1
	WA := cs.DAPK1P

	var WB, WT float64

	for i := 0; i < 3; i++ {
		WB += cs.Ca[i].CaM_DAPK1
		WT += cs.Ca[i].CaM_DAPK1P
	}
	WB += cs.Ca[3].CaM_DAPK1
	WP := cs.Ca[3].CaM_DAPK1P

	TotalW := WI + WB + WP + WT + WA
	Wb := WB / TotalW
	Wp := WP / TotalW
	Wt := WT / TotalW
	Wa := WA / TotalW
	cb := 0.75
	ct := 0.8
	ca := 0.8

	T := Wb + Wp + Wt + Wa
	tmp := T * (-0.22 + 1.826*T + -0.8*T*T)
	tmp *= 0.75 * (cb*Wb + Wp + ct*Wt + ca*Wa)
	if tmp < 0 {
		tmp = 0
	}
	cs.DAPK1auto.K = 0.29 * tmp
	cs.DAPK1auto.Act = cb*WB + WP + ct*WT + ca*WA
	cs.DAPK1auto.Total = T
}

func (cs *CaMKIIVars) Log(dt *etable.Table, vol float64, row int, pre string) {
	dt.SetCellFloat(pre+"CaM", row, chem.CoFmN(cs.Ca[0].CaM, vol))
	dt.SetCellFloat(pre+"Ca3CaM", row, chem.CoFmN(cs.Ca[3].CaM, vol))
	dt.SetCellFloat(pre+"CaMKIIact", row, chem.CoFmN(cs.CaMKIIauto.Act, vol))
	dt.SetCellFloat(pre+"DAPK1act", row, chem.CoFmN(cs.DAPK1auto.Act, vol))
	// dt.SetCellFloat(pre+"CaCaM", row, chem.CoFmN(cs.Ca[1].CaM, vol))
	// dt.SetCellFloat(pre+"Ca2CaM", row, chem.CoFmN(cs.Ca[2].CaM, vol))
	// dt.SetCellFloat(pre+"Ca0CaM_CaMKII", row, chem.CoFmN(cs.Ca[0].CaM_CaMKII, vol))
	// dt.SetCellFloat(pre+"Ca1CaM_CaMKII", row, chem.CoFmN(cs.Ca[1].CaM_CaMKII, vol))
	// dt.SetCellFloat(pre+"Ca0CaM_CaMKIIP", row, chem.CoFmN(cs.Ca[0].CaM_CaMKIIP, vol))
	// dt.SetCellFloat(pre+"Ca1CaM_CaMKIIP", row, chem.CoFmN(cs.Ca[1].CaM_CaMKIIP, vol))
	// dt.SetCellFloat(pre+"CaMKII", row, chem.CoFmN(cs.CaMKII, vol))
	// dt.SetCellFloat(pre+"CaMKIIP", row, chem.CoFmN(cs.CaMKIIP, vol))
}

func (cs *CaMKIIVars) ConfigLog(sch *etable.Schema, pre string) {
	*sch = append(*sch, etable.Column{pre + "CaM", etensor.FLOAT64, nil, nil})
	*sch = append(*sch, etable.Column{pre + "Ca3CaM", etensor.FLOAT64, nil, nil})
	*sch = append(*sch, etable.Column{pre + "CaMKIIact", etensor.FLOAT64, nil, nil})
	*sch = append(*sch, etable.Column{pre + "DAPK1act", etensor.FLOAT64, nil, nil})
	// *sch = append(*sch, etable.Column{pre + "CaCaM", etensor.FLOAT64, nil, nil})
	// *sch = append(*sch, etable.Column{pre + "Ca2CaM", etensor.FLOAT64, nil, nil})
	// *sch = append(*sch, etable.Column{pre + "Ca0CaM_CaMKII", etensor.FLOAT64, nil, nil})
	// *sch = append(*sch, etable.Column{pre + "Ca1CaM_CaMKII", etensor.FLOAT64, nil, nil})
	// *sch = append(*sch, etable.Column{pre + "Ca0CaM_CaMKIIP", etensor.FLOAT64, nil, nil})
	// *sch = append(*sch, etable.Column{pre + "Ca1CaM_CaMKIIP", etensor.FLOAT64, nil, nil})
	// *sch = append(*sch, etable.Column{pre + "CaMKII", etensor.FLOAT64, nil, nil})
	// *sch = append(*sch, etable.Column{pre + "CaMKIIP", etensor.FLOAT64, nil, nil})
}

// CaMKIIState is overall intracellular Ca-driven signaling states
// for CaMKII in Cyt and PSD
// 32 state vars total
type CaMKIIState struct {
	Cyt CaMKIIVars `desc:"in cytosol -- volume = 0.08 fl = 48"`
	PSD CaMKIIVars `desc:"in PSD -- volume = 0.02 fl = 12"`
}

func (cs *CaMKIIState) Init() {
	cs.Cyt.Init(CytVol)
	cs.PSD.Init(PSDVol)

	if InitBaseline {
		// All vals below from 500 sec baseline
		// Note: all CaMKIIP = 0 after baseline
		vol := float64(CytVol)
		cs.Cyt.Ca[0].CaM_CaMKII = chem.CoToN(0.2612, vol)
		cs.Cyt.Ca[1].CaM_CaMKII = chem.CoToN(0.003344, vol)
		cs.Cyt.Ca[2].CaM_CaMKII = chem.CoToN(2.229e-05, vol)
		cs.Cyt.Ca[3].CaM = chem.CoToN(1.988e-05, vol)
		cs.Cyt.Ca[3].CaM_CaMKII = chem.CoToN(0.0014, vol)

		vol = PSDVol
		cs.PSD.Ca[0].CaM_CaMKII = chem.CoToN(1.991, vol)
		cs.PSD.Ca[1].CaM_CaMKII = chem.CoToN(0.02548, vol)
		cs.PSD.Ca[2].CaM_CaMKII = chem.CoToN(0.0001698, vol)
		cs.PSD.Ca[3].CaM = chem.CoToN(2.738e-05, vol)
		cs.PSD.Ca[3].CaM_CaMKII = chem.CoToN(0.01098, vol)
	}
}

func (cs *CaMKIIState) InitCode() {
	fmt.Printf("\nCaMKIIState:\n")
	cs.Cyt.InitCode(CytVol, "Cyt")
	cs.PSD.InitCode(PSDVol, "PSD")
}

func (cs *CaMKIIState) Zero() {
	cs.Cyt.Zero()
	cs.PSD.Zero()
}

func (cs *CaMKIIState) Integrate(d *CaMKIIState) {
	cs.Cyt.Integrate(&d.Cyt)
	cs.PSD.Integrate(&d.PSD)
}

func (cs *CaMKIIState) Log(dt *etable.Table, row int) {
	cs.Cyt.Log(dt, CytVol, row, "Cyt_")
	cs.PSD.Log(dt, PSDVol, row, "PSD_")
}

func (cs *CaMKIIState) ConfigLog(sch *etable.Schema) {
	cs.Cyt.ConfigLog(sch, "Cyt_")
	cs.PSD.ConfigLog(sch, "PSD_")
}

// CaMKIIParams are the parameters governing the Ca+CaM binding
type CaMKIIParams struct {
	CaCaM01        chem.React   `desc:"1: Ca+CaM -> 1CaCaM = CaM-bind-Ca"`
	CaCaM12        chem.React   `desc:"2: Ca+1CaM -> 2CaCaM = CaMCa-bind-Ca"`
	CaCaM23        chem.React   `desc:"3: Ca+2CaM -> 3CaCaM = CaMCa2-bind-Ca"`
	CaMCaMKII      chem.React   `desc:"4: CaM+CaMKII -> CaM-CaMKII [0-2] -- kIB_kBI_[0-2] -- WI = plain CaMKII, WBn = CaM bound"`
	CaMCaMKII3     chem.React   `desc:"5: 3CaCaM+CaMKII -> 3CaCaM-CaMKII = kIB_kBI_3"`
	CaCaM23_CaMKII chem.React   `desc:"6: Ca+2CaCaM-CaMKII -> 3CaCaM-CaMKII = CaMCa2-bind-Ca"`
	CaCaM_CaMKIIP  chem.React   `desc:"8: Ca+nCaCaM-CaMKIIP -> n+1CaCaM-CaMKIIP = kTP_PT_*"`
	CaMCaMKIIP     chem.React   `desc:"9: CaM+CaMKIIP -> CaM-CaMKIIP = kAT_kTA"` // note: typo in SI3 for top PP1, PP2A
	PP1Thr286      chem.Enz     `desc:"10: PP1 dephosphorylating CaMKIIP"`
	PP2AThr286     chem.Enz     `desc:"11: PP2A dephosphorylating CaMKIIP"`
	CaNS308        chem.Enz     `desc:"CaN dephosphorylating DAPK1P"`
	CaMDiffuse     chem.Diffuse `desc:"CaM diffusion between Cyt and PSD"`
	CaMKIIDiffuse  chem.Diffuse `desc:"CaMKII diffusion between Cyt and PSD -- symmetric, just WI"`
	CaMKIIPDiffuse chem.Diffuse `desc:"CaMKIIP diffusion between Cyt and PSD -- asymmetric, everything else"`
}

func (cp *CaMKIIParams) Defaults() {
	// note: following are all in Cyt -- PSD is 4x for first values
	// See React docs for more info
	cp.CaCaM01.SetVol(51.202, CytVol, 200) // 1: 51.202 μM-1 = 1.0667, PSD 4.2667 = CaM-bind-Ca
	cp.CaCaM12.SetVol(133.3, CytVol, 1000) // 2: 133.3 μM-1 = 2.7771, PSD 11.108 = CaMCa-bind-Ca
	cp.CaCaM23.SetVol(25.6, CytVol, 400)   // 3: 25.6 μM-1 = 0.53333, PSD 2.1333 = CaMCa2-bind-Ca
	cp.CaMCaMKII.SetVol(0.0004, CytVol, 1) // 4: 0.0004 μM-1 = 8.3333e-6, PSD 3.3333e-5 = kIB_kBI_[0-2]
	cp.CaMCaMKII3.SetVol(8, CytVol, 1)     // 5: 8 μM-1 = 0.16667, PSD 3.3333e-5 = kIB_kBI_3

	cp.CaCaM23_CaMKII.SetVol(25.6, CytVol, 0.02) // 6: 25.6 μM-1 = 0.53333, PSD 2.1333 = CaMCa2-bind-Ca
	cp.CaCaM_CaMKIIP.SetVol(1, CytVol, 1)        // 8: 1 μM-1 = 0.020834, PSD 0.0833335 = kTP_PT_*
	cp.CaMCaMKIIP.SetVol(8, CytVol, 0.001)       // 9: 8 μM-1 = 0.16667, PSD 0.66667 = kAT_kTA

	cp.PP1Thr286.SetKmVol(11, CytVol, 1.34, 0.335)  // 10: 11 μM Km = 0.0031724
	cp.PP2AThr286.SetKmVol(11, CytVol, 1.34, 0.335) // 11: 11 μM Km = 0.0031724

	cp.CaMDiffuse.SetSym(130.0 / 0.0225)
	cp.CaMKIIDiffuse.SetSym(6.0 / 0.0225)
	cp.CaMKIIPDiffuse.Set(6.0/0.0225, 0.6/0.0225)
}

// StepCaMKII does the bulk of Ca + CaM + CaMKII binding reactions, in a given region
// cCa, nCa = current next Ca
func (cp *CaMKIIParams) StepCaMKII(vol float64, c, d *CaMKIIVars, cCa, pp1, pp2a float64, dCa, dpp1, dpp2a *float64) {
	kf := CytVol / vol
	cp.CaCaM01.StepK(kf, c.Ca[0].CaM, cCa, c.Ca[1].CaM, &d.Ca[0].CaM, dCa, &d.Ca[1].CaM) // 1
	cp.CaCaM12.StepK(kf, c.Ca[1].CaM, cCa, c.Ca[2].CaM, &d.Ca[1].CaM, dCa, &d.Ca[2].CaM) // 2
	cp.CaCaM23.StepK(kf, c.Ca[2].CaM, cCa, c.Ca[3].CaM, &d.Ca[2].CaM, dCa, &d.Ca[3].CaM) // 3

	cp.CaCaM01.StepK(kf, c.Ca[0].CaM_CaMKII, cCa, c.Ca[1].CaM_CaMKII, &d.Ca[0].CaM_CaMKII, dCa, &d.Ca[1].CaM_CaMKII)        // 1
	cp.CaCaM12.StepK(kf, c.Ca[1].CaM_CaMKII, cCa, c.Ca[2].CaM_CaMKII, &d.Ca[1].CaM_CaMKII, dCa, &d.Ca[2].CaM_CaMKII)        // 2
	cp.CaCaM23_CaMKII.StepK(kf, c.Ca[2].CaM_CaMKII, cCa, c.Ca[3].CaM_CaMKII, &d.Ca[2].CaM_CaMKII, dCa, &d.Ca[3].CaM_CaMKII) // 6

	for i := 0; i < 3; i++ {
		cp.CaMCaMKII.StepK(kf, c.Ca[i].CaM, c.CaMKII, c.Ca[i].CaM_CaMKII, &d.Ca[i].CaM, &d.CaMKII, &d.Ca[i].CaM_CaMKII) // 4
	}
	cp.CaMCaMKII3.StepK(kf, c.Ca[3].CaM, c.CaMKII, c.Ca[3].CaM_CaMKII, &d.Ca[3].CaM, &d.CaMKII, &d.Ca[3].CaM_CaMKII) // 5

	cp.CaMCaMKIIP.StepK(kf, c.Ca[0].CaM, c.CaMKIIP, c.Ca[0].CaM_CaMKIIP, &d.Ca[0].CaM, &d.CaMKIIP, &d.Ca[0].CaM_CaMKIIP) // 9
	for i := 0; i < 3; i++ {
		cp.CaCaM_CaMKIIP.StepK(kf, c.Ca[i].CaM_CaMKIIP, cCa, c.Ca[i+1].CaM_CaMKIIP, &d.Ca[i].CaM_CaMKIIP, dCa, &d.Ca[i+1].CaM_CaMKIIP) // 8
	}

	// cs, ce, cc, cp -> ds, de, dc, dp
	cp.PP1Thr286.StepK(kf, c.CaMKIIP, pp1, c.PP1Thr286C, c.CaMKII, &d.CaMKIIP, dpp1, &d.PP1Thr286C, &d.CaMKII) // 10
	if dpp2a != nil {
		cp.PP2AThr286.StepK(kf, c.CaMKIIP, pp2a, c.PP2AThr286C, c.CaMKII, &d.CaMKIIP, dpp2a, &d.PP2AThr286C, &d.CaMKII) // 11
	}

	for i := 0; i < 4; i++ {
		cc := &c.Ca[i]
		dc := &d.Ca[i]
		dc.CaM_CaMKIIP += c.CaMKIIauto.K * cc.CaM_CaMKII // forward only autophos
		// cs, ce, cc, cp -> ds, de, dc, dp
		cp.PP1Thr286.StepK(kf, cc.CaM_CaMKIIP, pp1, c.PP1Thr286C, cc.CaM_CaMKII, &dc.CaM_CaMKIIP, dpp1, &d.PP1Thr286C, &dc.CaM_CaMKII) // 10
		if dpp2a != nil {
			cp.PP2AThr286.StepK(kf, cc.CaM_CaMKIIP, pp2a, c.PP2AThr286C, cc.CaM_CaMKII, &dc.CaM_CaMKIIP, dpp2a, &d.PP2AThr286C, &dc.CaM_CaMKII) // 11
		}
	}
}

// StepDiffuse does Cyt <-> PSD diffusion
func (cp *CaMKIIParams) StepDiffuse(c, d *CaMKIIState) {
	for i := 0; i < 4; i++ {
		cc := &c.Cyt.Ca[i]
		cd := &c.PSD.Ca[i]
		dc := &d.Cyt.Ca[i]
		dd := &d.PSD.Ca[i]
		cp.CaMDiffuse.Step(cc.CaM, cd.CaM, CytVol, PSDVol, &dc.CaM, &dd.CaM)
		cp.CaMKIIPDiffuse.Step(cc.CaM_CaMKII, cd.CaM_CaMKII, CytVol, PSDVol, &dc.CaM_CaMKII, &dd.CaM_CaMKII)
		cp.CaMKIIPDiffuse.Step(cc.CaM_CaMKIIP, cd.CaM_CaMKIIP, CytVol, PSDVol, &dc.CaM_CaMKIIP, &dd.CaM_CaMKIIP)
	}
	cp.CaMKIIDiffuse.Step(c.Cyt.CaMKII, c.PSD.CaMKII, CytVol, PSDVol, &d.Cyt.CaMKII, &d.PSD.CaMKII)
	cp.CaMKIIPDiffuse.Step(c.Cyt.CaMKIIP, c.PSD.CaMKIIP, CytVol, PSDVol, &d.Cyt.CaMKIIP, &d.PSD.CaMKIIP)
}

// Step does one step of CaMKII updating, c=current, d=delta
// pp2a = current cyt pp2a
func (cp *CaMKIIParams) Step(c, d *CaMKIIState, cCa, dCa *CaState, pp1, dpp1 *PP1State, pp2a float64, dpp2a *float64) {
	cp.StepCaMKII(CytVol, &c.Cyt, &d.Cyt, cCa.Cyt, pp1.Cyt.PP1act, pp2a, &dCa.Cyt, &dpp1.Cyt.PP1act, dpp2a)
	cp.StepCaMKII(PSDVol, &c.PSD, &d.PSD, cCa.PSD, pp1.PSD.PP1act, 0, &dCa.PSD, &dpp1.PSD.PP1act, nil)
	cp.StepDiffuse(c, d)
}
