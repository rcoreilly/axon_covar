// Copyright (c) 2019, The Emergent Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package axon

import (
	"github.com/goki/mat32"
)

///////////////////////////////////////////////////////////////////////
//  learn.go contains the learning params and functions for axon

// axon.LearnNeurParams manages learning-related parameters at the neuron-level.
// This is mainly the running average activations that drive learning
type LearnNeurParams struct {
	ActAvg  LrnActAvgParams `view:"inline" desc:"parameters for computing running average activations that drive learning"`
	AvgL    AvgLParams      `view:"inline" desc:"parameters for computing AvgL long-term running average"`
	CosDiff CosDiffParams   `view:"inline" desc:"parameters for computing cosine diff between minus and plus phase"`
}

func (ln *LearnNeurParams) Update() {
	ln.ActAvg.Update()
	ln.AvgL.Update()
	ln.CosDiff.Update()
}

func (ln *LearnNeurParams) Defaults() {
	ln.ActAvg.Defaults()
	ln.AvgL.Defaults()
	ln.CosDiff.Defaults()
}

// InitActAvg initializes the running-average activation values that drive learning.
// Called by InitWts (at start of learning).
func (ln *LearnNeurParams) InitActAvg(nrn *Neuron) {
	nrn.AvgSS = ln.ActAvg.Init
	nrn.AvgS = ln.ActAvg.Init
	nrn.AvgM = ln.ActAvg.Init
	nrn.AvgL = ln.AvgL.Init
	nrn.AvgSLrn = 0
	nrn.ActAvg = ln.ActAvg.Init
}

// AvgsFmAct updates the running averages based on current learning activation.
// Computed after new activation for current cycle is updated.
func (ln *LearnNeurParams) AvgsFmAct(nrn *Neuron) {
	// if ln.ActAvg.Spike {
	ln.ActAvg.AvgsFmAct(ln.ActAvg.SpikeG*nrn.Spike, &nrn.AvgSS, &nrn.AvgS, &nrn.AvgM, &nrn.AvgSLrn)
	// } else {
	// 	ln.ActAvg.AvgsFmAct(nrn.ActLrn, &nrn.AvgSS, &nrn.AvgS, &nrn.AvgM, &nrn.AvgSLrn)
	// }
}

// AvgLFmAct computes long-term average activation value, and learning factor, from current AvgM.
// Called at start of new alpha-cycle.
func (ln *LearnNeurParams) AvgLFmAvgM(nrn *Neuron) {
	ln.AvgL.AvgLFmAvgM(nrn.AvgM, &nrn.AvgL, &nrn.AvgLLrn)
}

///////////////////////////////////////////////////////////////////////
//  LearnSynParams

// axon.LearnSynParams manages learning-related parameters at the synapse-level.
type LearnSynParams struct {
	Learn     bool        `desc:"enable learning for this projection"`
	Lrate     float32     `desc:"current effective learning rate (multiplies DWt values, determining rate of change of weights)"`
	LrateInit float32     `desc:"initial learning rate -- this is set from Lrate in UpdateParams, which is called when Params are updated, and used in LrateMult to compute a new learning rate for learning rate schedules."`
	XCal      XCalParams  `view:"inline" desc:"parameters for the XCal learning rule"`
	WtSig     WtSigParams `view:"inline" desc:"parameters for the sigmoidal contrast weight enhancement"`
	WtBal     WtBalParams `view:"inline" desc:"parameters for balancing strength of weight increases vs. decreases"`
}

func (ls *LearnSynParams) Update() {
	ls.XCal.Update()
	ls.WtSig.Update()
	ls.WtBal.Update()
}

func (ls *LearnSynParams) Defaults() {
	ls.Learn = true
	ls.Lrate = 0.04
	ls.LrateInit = ls.Lrate
	ls.XCal.Defaults()
	ls.WtSig.Defaults()
	ls.WtBal.Defaults()
}

// LWtFmWt updates the linear weight value based on the current effective Wt value.
// effective weight is sigmoidally contrast-enhanced relative to the linear weight.
func (ls *LearnSynParams) LWtFmWt(syn *Synapse) {
	syn.LWt = ls.WtSig.LinFmSigWt(syn.Wt / syn.Scale) // must factor out scale too!
}

// WtFmLWt updates the effective weight value based on the current linear Wt value.
// effective weight is sigmoidally contrast-enhanced relative to the linear weight.
func (ls *LearnSynParams) WtFmLWt(syn *Synapse) {
	syn.Wt = ls.WtSig.SigFmLinWt(syn.LWt)
	syn.Wt *= syn.Scale
}

// CHLdWt returns the error-driven and BCM Hebbian weight change components for the
// temporally eXtended Contrastive Attractor Learning (XCAL), CHL version
func (ls *LearnSynParams) CHLdWt(suAvgSLrn, suAvgM, ruAvgSLrn, ruAvgM, ruAvgL float32) (err, bcm float32) {
	srs := suAvgSLrn * ruAvgSLrn
	srm := suAvgM * ruAvgM
	bcm = ls.XCal.DWt(srs, ruAvgL)
	err = ls.XCal.DWt(srs, srm)
	return
}

// BCMdWt returns the BCM Hebbian weight change for AvgSLrn vs. AvgL
// long-term average floating activation on the receiver.
func (ls *LearnSynParams) BCMdWt(suAvgSLrn, ruAvgSLrn, ruAvgL float32) float32 {
	srs := suAvgSLrn * ruAvgSLrn
	return ls.XCal.DWt(srs, ruAvgL)
}

// WtFmDWt updates the synaptic weights from accumulated weight changes
// wbInc and wbDec are the weight balance factors, wt is the sigmoidal contrast-enhanced
// weight and lwt is the linear weight value
func (ls *LearnSynParams) WtFmDWt(wbInc, wbDec float32, dwt, wt, lwt *float32, scale float32) {
	if *dwt == 0 {
		if *wt == 0 { // restore failed wts
			*wt = scale * ls.WtSig.SigFmLinWt(*lwt)
		}
		return
	}
	// always doing softbound by default
	if *dwt > 0 {
		*dwt *= wbInc * (1 - *lwt)
	} else {
		*dwt *= wbDec * *lwt
	}
	*lwt += *dwt
	if *lwt < 0 {
		*lwt = 0
	} else if *lwt > 1 {
		*lwt = 1
	}
	*wt = scale * ls.WtSig.SigFmLinWt(*lwt)
	*dwt = 0
}

// LrnActAvgParams has rate constants for averaging over activations
// at different time scales, to produce the running average activation
// values that then drive learning in the XCAL learning rules.
// Is driven directly by spikes that increment running-average at super-short
// timescale.  Time cycle of 50 msec quarters / theta window learning works
// Cyc:50, SS:35 S:8, M:40 (best)
// Cyc:25, SS:20, S:4, M:20
type LrnActAvgParams struct {
	SpikeG float32 `def:"8" desc:"gain multiplier on spike: how much spike drives AvgSS value"`
	SSTau  float32 `def:"40" min:"1" desc:"time constant in cycles, which should be milliseconds typically (roughly, how long it takes for value to change significantly -- 1.4x the half-life), for continuously updating the super-short time-scale AvgSS value -- this is provides a pre-integration step before integrating into the AvgS short time scale -- it is particularly important for spiking -- in general 4 is the largest value without starting to impair learning, but a value of 7 can be combined with m_in_s = 0 with somewhat worse results"`
	STau   float32 `def:"10" min:"1" desc:"time constant in cycles, which should be milliseconds typically (roughly, how long it takes for value to change significantly -- 1.4x the half-life), for continuously updating the short time-scale AvgS value from the super-short AvgSS value (cascade mode) -- AvgS represents the plus phase learning signal that reflects the most recent past information"`
	MTau   float32 `def:"40" min:"1" desc:"time constant in cycles, which should be milliseconds typically (roughly, how long it takes for value to change significantly -- 1.4x the half-life), for continuously updating the medium time-scale AvgM value from the short AvgS value (cascade mode) -- AvgM represents the minus phase learning signal that reflects the expectation representation prior to experiencing the outcome (in addition to the outcome) -- the default value of 10 generally cannot be exceeded without impairing learning"`
	LrnM   float32 `def:"0.1,0" min:"0" max:"1" desc:"how much of the medium term average activation to mix in with the short (plus phase) to compute the Neuron AvgSLrn variable that is used for the unit's short-term average in learning. This is important to ensure that when unit turns off in plus phase (short time scale), enough medium-phase trace remains so that learning signal doesn't just go all the way to 0, at which point no learning would take place -- typically need faster time constant for updating S such that this trace of the M signal is lost -- can set SSTau=7 and set this to 0 but learning is generally somewhat worse"`
	Init   float32 `def:"0.15" min:"0" max:"1" desc:"initial value for average"`

	SSDt float32 `view:"-" json:"-" xml:"-" inactive:"+" desc:"rate = 1 / tau"`
	SDt  float32 `view:"-" json:"-" xml:"-" inactive:"+" desc:"rate = 1 / tau"`
	MDt  float32 `view:"-" json:"-" xml:"-" inactive:"+" desc:"rate = 1 / tau"`
	LrnS float32 `view:"-" json:"-" xml:"-" inactive:"+" desc:"1-LrnM"`
}

// AvgsFmAct computes averages based on current act
func (aa *LrnActAvgParams) AvgsFmAct(act float32, avgSS, avgS, avgM, avgSLrn *float32) {
	*avgSS += aa.SSDt * (act - *avgSS)
	*avgS += aa.SDt * (*avgSS - *avgS)
	*avgM += aa.MDt * (*avgS - *avgM)

	*avgSLrn = aa.LrnS**avgS + aa.LrnM**avgM
}

func (aa *LrnActAvgParams) Update() {
	aa.SSDt = 1 / aa.SSTau
	aa.SDt = 1 / aa.STau
	aa.MDt = 1 / aa.MTau
	aa.LrnS = 1 - aa.LrnM
}

func (aa *LrnActAvgParams) Defaults() {
	aa.SpikeG = 8
	aa.SSTau = 40 // 20 for 25 cycle qtr
	aa.STau = 10
	aa.MTau = 40 // 20 for 25 cycle qtr
	aa.LrnM = 0.1
	aa.Init = 0.15
	aa.Update()

}

// AvgLParams are parameters for computing the long-term floating average value, AvgL
// which is used for driving BCM-style hebbian learning in XCAL -- this form of learning
// increases contrast of weights and generally decreases overall activity of neuron,
// to prevent "hog" units -- it is computed as a running average of the (gain multiplied)
// medium-time-scale average activation at the end of the alpha-cycle.
// Also computes an adaptive amount of BCM learning, AvgLLrn, based on AvgL.
type AvgLParams struct {
	Init   float32 `def:"0.4" min:"0" max:"1" desc:"initial AvgL value at start of training"`
	Gain   float32 `def:"1.5,2,2.5,3,4,5" min:"0" desc:"gain multiplier on activation used in computing the running average AvgL value that is the key floating threshold in the BCM Hebbian learning rule -- when using the DELTA_FF_FB learning rule, it should generally be 2x what it was before with the old XCAL_CHL rule, i.e., default of 5 instead of 2.5 -- it is a good idea to experiment with this parameter a bit -- the default is on the high-side, so typically reducing a bit from initial default is a good direction"`
	Min    float32 `def:"0.2" min:"0" desc:"miniumum AvgL value -- running average cannot go lower than this value even when it otherwise would due to inactivity -- default value is generally good and typically does not need to be changed"`
	Tau    float32 `def:"10" min:"1" desc:"time constant for updating the running average AvgL -- AvgL moves toward gain*act with this time constant on every alpha-cycle - longer time constants can also work fine, but the default of 10 allows for quicker reaction to beneficial weight changes"`
	LrnMax float32 `def:"0.5" min:"0" desc:"maximum AvgLLrn value, which is amount of learning driven by AvgL factor -- when AvgL is at its maximum value (i.e., gain, as act does not exceed 1), then AvgLLrn will be at this maximum value -- by default, strong amounts of this homeostatic Hebbian form of learning can be used when the receiving unit is highly active -- this will then tend to bring down the average activity of units -- the default of 0.5, in combination with the err_mod flag, works well for most models -- use around 0.0004 for a single fixed value (with err_mod flag off)"`
	LrnMin float32 `def:"0.0001,0.0004" min:"0" desc:"miniumum AvgLLrn value (amount of learning driven by AvgL factor) -- if AvgL is at its minimum value, then AvgLLrn will be at this minimum value -- neurons that are not overly active may not need to increase the contrast of their weights as much -- use around 0.0004 for a single fixed value (with err_mod flag off)"`
	ErrMod bool    `def:"true" desc:"modulate amount learning by normalized level of error within layer"`
	ModMin float32 `def:"0.01" viewif:"ErrMod=true" desc:"minimum modulation value for ErrMod-- ensures a minimum amount of self-organizing learning even for network / layers that have a very small level of error signal"`

	Dt      float32 `view:"-" json:"-" xml:"-" inactive:"+" desc:"rate = 1 / tau"`
	LrnFact float32 `view:"-" json:"-" xml:"-" inactive:"+" desc:"(LrnMax - LrnMin) / (Gain - Min)"`
}

// AvgLFmAvgM computes long-term average activation value, and learning factor, from given
// medium-scale running average activation avgM
func (al *AvgLParams) AvgLFmAvgM(avgM float32, avgL, lrn *float32) {
	*avgL += al.Dt * (al.Gain*avgM - *avgL)
	if *avgL < al.Min {
		*avgL = al.Min
	}
	*lrn = al.LrnFact * (*avgL - al.Min)
}

// ErrModFmLayErr computes AvgLLrn multiplier from layer cosine diff avg statistic
func (al *AvgLParams) ErrModFmLayErr(layCosDiffAvg float32) float32 {
	mod := float32(1)
	if !al.ErrMod {
		return mod
	}
	mod *= mat32.Max(layCosDiffAvg, al.ModMin)
	return mod
}

func (al *AvgLParams) Update() {
	al.Dt = 1 / al.Tau
	al.LrnFact = (al.LrnMax - al.LrnMin) / (al.Gain - al.Min)
}

func (al *AvgLParams) Defaults() {
	al.Init = 0.4
	al.Gain = 2.5
	al.Min = 0.2
	al.Tau = 10
	al.LrnMax = 0.5
	al.LrnMin = 0.0001
	al.ErrMod = true
	al.ModMin = 0.01
	al.Update()
}

//////////////////////////////////////////////////////////////////////////////////////
//  CosDiffParams

// CosDiffParams specify how to integrate cosine of difference between plus and minus phase activations
// Used to modulate amount of hebbian learning, and overall learning rate.
type CosDiffParams struct {
	Tau float32 `def:"100" min:"1" desc:"time constant in alpha-cycles (roughly how long significant change takes, 1.4 x half-life) for computing running average CosDiff value for the layer, CosDiffAvg = cosine difference between ActM and ActP -- this is an important statistic for how much phase-based difference there is between phases in this layer -- it is used in standard X_COS_DIFF modulation of l_mix in AxonConSpec, and for modulating learning rate as a function of predictability in the DeepAxon predictive auto-encoder learning -- running average variance also computed with this: cos_diff_var"`
	//   bool          lrate_mod; // modulate learning rate in this layer as a function of the cos_diff on this alpha-cycle relative to running average cos_diff values (see avg_tau) -- lrate_mod = cos_diff_lrate_mult * (cos_diff / cos_diff_avg) -- if this layer is less predictable than previous alpha-cycles, we don't learn as much
	//   float         lrmod_z_thr; // #DEF_-1.5 #CONDSHOW_ON_lrate_mod&&!lrmod_fm_trc threshold for setting learning rate modulation to zero, as function of z-normalized cos_diff value on this alpha-cycle -- normalization computed using incrementally computed average and variance values -- this essentially has the network ignoring alpha-cycles where the diff was significantly below average -- replaces the manual unlearnable alpha-cycle mechanism
	//   bool          set_net_unlrn;  // #CONDSHOW_ON_lrate_mod&&!lrmod_fm_trc set the network-level unlearnable_alpha-cycle flag based on our learning rate modulation factor -- only makes sense for one layer to do this

	Dt  float32 `inactive:"+" view:"-" json:"-" xml:"-" desc:"rate constant = 1 / Tau"`
	DtC float32 `inactive:"+" view:"-" json:"-" xml:"-" desc:"complement of rate constant = 1 - Dt"`
}

func (cd *CosDiffParams) Update() {
	cd.Dt = 1 / cd.Tau
	cd.DtC = 1 - cd.Dt
}

func (cd *CosDiffParams) Defaults() {
	cd.Tau = 100
	cd.Update()
}

// AvgVarFmCos updates the average and variance from current cosine diff value
func (cd *CosDiffParams) AvgVarFmCos(avg, vr *float32, cos float32) {
	if *avg == 0 { // first time -- set
		*avg = cos
		*vr = 0
	} else {
		del := cos - *avg
		incr := cd.Dt * del
		*avg += incr
		// following is magic exponentially-weighted incremental variance formula
		// derived by Finch, 2009: Incremental calculation of weighted mean and variance
		if *vr == 0 {
			*vr = 2 * cd.DtC * del * incr
		} else {
			*vr = cd.DtC * (*vr + del*incr)
		}
	}
}

// LrateMod computes learning rate modulation based on cos diff vals
// func (cd *CosDiffParams) LrateMod(cos, avg, vr float32) float32 {
// 	if vr <= 0 {
// 		return 1
// 	}
// 	zval := (cos - avg) / mat32.Sqrt(vr) // stdev = sqrt of var
// 	// z-normal value is starting point for learning rate factor
// 	//    if zval < lrmod_z_thr {
// 	// 	return 0
// 	// }
// 	return 1
// }

//////////////////////////////////////////////////////////////////////////////////////
//  CosDiffStats

// CosDiffStats holds cosine-difference statistics at the layer level
type CosDiffStats struct {
	Cos        float32 `desc:"cosine (normalized dot product) activation difference between ActP and ActM on this alpha-cycle for this layer -- computed by CosDiffFmActs at end of QuarterFinal for quarter = 3"`
	Avg        float32 `desc:"running average of cosine (normalized dot product) difference between ActP and ActM -- computed with CosDiff.Tau time constant in QuarterFinal, and used for modulating BCM Hebbian learning (see AvgLrn) and overall learning rate"`
	Var        float32 `desc:"running variance of cosine (normalized dot product) difference between ActP and ActM -- computed with CosDiff.Tau time constant in QuarterFinal, used for modulating overall learning rate"`
	AvgLrn     float32 `desc:"1 - Avg and 0 for non-Hidden layers"`
	ModAvgLLrn float32 `desc:"1 - AvgLrn and 0 for non-Hidden layers -- this is the value of Avg used for AvgLParams ErrMod modulation of the AvgLLrn factor if enabled"`
}

func (cd *CosDiffStats) Init() {
	cd.Cos = 0
	cd.Avg = 0
	cd.Var = 0
	cd.AvgLrn = 0
	cd.ModAvgLLrn = 0
}

//////////////////////////////////////////////////////////////////////////////////////
//  XCalParams

// XCalParams are parameters for temporally eXtended Contrastive Attractor Learning function (XCAL)
// which is the standard learning equation for axon .
type XCalParams struct {
	MLrn    float32 `def:"1" min:"0" desc:"multiplier on learning based on the medium-term floating average threshold which produces error-driven learning -- this is typically 1 when error-driven learning is being used, and 0 when pure Hebbian learning is used. The long-term floating average threshold is provided by the receiving unit"`
	SetLLrn bool    `def:"false" desc:"if true, set a fixed AvgLLrn weighting factor that determines how much of the long-term floating average threshold (i.e., BCM, Hebbian) component of learning is used -- this is useful for setting a fully Hebbian learning connection, e.g., by setting MLrn = 0 and LLrn = 1. If false, then the receiving unit's AvgLLrn factor is used, which dynamically modulates the amount of the long-term component as a function of how active overall it is"`
	LLrn    float32 `viewif:"SetLLrn" desc:"fixed l_lrn weighting factor that determines how much of the long-term floating average threshold (i.e., BCM, Hebbian) component of learning is used -- this is useful for setting a fully Hebbian learning connection, e.g., by setting MLrn = 0 and LLrn = 1."`
	DRev    float32 `def:"0.1" min:"0" max:"0.99" desc:"proportional point within LTD range where magnitude reverses to go back down to zero at zero -- err-driven svm component does better with smaller values, and BCM-like mvl component does better with larger values -- 0.1 is a compromise"`
	DThr    float32 `def:"0.0001,0.01" min:"0" desc:"minimum LTD threshold value below which no weight change occurs -- this is now *relative* to the threshold"`
	LrnThr  float32 `def:"0.01" desc:"xcal learning threshold -- don't learn when sending unit activation is below this value in both phases -- due to the nature of the learning function being 0 when the sr coproduct is 0, it should not affect learning in any substantial way -- nonstandard learning algorithms that have different properties should ignore it"`

	DRevRatio float32 `inactive:"+" view:"-" json:"-" xml:"-" desc:"-(1-DRev)/DRev -- multiplication factor in learning rule -- builds in the minus sign!"`
}

func (xc *XCalParams) Update() {
	if xc.DRev > 0 {
		xc.DRevRatio = -(1 - xc.DRev) / xc.DRev
	} else {
		xc.DRevRatio = -1
	}
}

func (xc *XCalParams) Defaults() {
	xc.MLrn = 1
	xc.SetLLrn = false
	xc.LLrn = 1
	xc.DRev = 0.1
	xc.DThr = 0.0001
	xc.LrnThr = 0.01
	xc.Update()
}

// DWt is the XCAL function for weight change -- the "check mark" function -- no DGain, no ThrPMin
func (xc *XCalParams) DWt(srval, thrP float32) float32 {
	var dwt float32
	if srval < xc.DThr {
		dwt = 0
	} else if srval > thrP*xc.DRev {
		dwt = (srval - thrP)
	} else {
		dwt = srval * xc.DRevRatio
	}
	return dwt
}

// LongLrate returns the learning rate for long-term floating average component (BCM)
func (xc *XCalParams) LongLrate(avgLLrn float32) float32 {
	if xc.SetLLrn {
		return xc.LLrn
	}
	return avgLLrn
}

//////////////////////////////////////////////////////////////////////////////////////
//  WtSigParams

// WtSigParams are sigmoidal weight contrast enhancement function parameters
type WtSigParams struct {
	Gain float32 `def:"1,6" min:"0" desc:"gain (contrast, sharpness) of the weight contrast function (1 = linear)"`
	Off  float32 `def:"1" min:"0" desc:"offset of the function (1=centered at .5, >1=higher, <1=lower) -- 1 is standard for XCAL"`
}

func (ws *WtSigParams) Update() {
}

func (ws *WtSigParams) Defaults() {
	ws.Gain = 6
	ws.Off = 1
}

// SigFun is the sigmoid function for value w in 0-1 range, with gain and offset params
func SigFun(w, gain, off float32) float32 {
	if w <= 0 {
		return 0
	}
	if w >= 1 {
		return 1
	}
	return (1 / (1 + mat32.Pow((off*(1-w))/w, gain)))
}

// SigFun61 is the sigmoid function for value w in 0-1 range, with default gain = 6, offset = 1 params
func SigFun61(w float32) float32 {
	if w <= 0 {
		return 0
	}
	if w >= 1 {
		return 1
	}
	pw := (1 - w) / w
	return (1 / (1 + pw*pw*pw*pw*pw*pw))
}

// SigInvFun is the inverse of the sigmoid function
func SigInvFun(w, gain, off float32) float32 {
	if w <= 0 {
		return 0
	}
	if w >= 1 {
		return 1
	}
	return 1.0 / (1.0 + mat32.Pow((1.0-w)/w, 1/gain)/off)
}

// SigInvFun61 is the inverse of the sigmoid function, with default gain = 6, offset = 1 params
func SigInvFun61(w float32) float32 {
	if w <= 0 {
		return 0
	}
	if w >= 1 {
		return 1
	}
	rval := 1.0 / (1.0 + mat32.Pow((1.0-w)/w, 1.0/6.0))
	return rval
}

// SigFmLinWt returns sigmoidal contrast-enhanced weight from linear weight
func (ws *WtSigParams) SigFmLinWt(lw float32) float32 {
	if ws.Gain == 1 && ws.Off == 1 {
		return lw
	}
	if ws.Gain == 6 && ws.Off == 1 {
		return SigFun61(lw)
	}
	return SigFun(lw, ws.Gain, ws.Off)
}

// LinFmSigWt returns linear weight from sigmoidal contrast-enhanced weight
func (ws *WtSigParams) LinFmSigWt(sw float32) float32 {
	if ws.Gain == 1 && ws.Off == 1 {
		return sw
	}
	if ws.Gain == 6 && ws.Off == 1 {
		return SigInvFun61(sw)
	}
	return SigInvFun(sw, ws.Gain, ws.Off)
}

//////////////////////////////////////////////////////////////////////////////////////
//  WtBalParams

// WtBalParams are weight balance soft renormalization params:
// maintains overall weight balance by progressively penalizing weight increases as a function of
// how strong the weights are overall (subject to thresholding) and long time-averaged activation.
// Plugs into soft bounding function.
type WtBalParams struct {
	On     bool    `desc:"perform weight balance soft normalization?  if so, maintains overall weight balance across units by progressively penalizing weight increases as a function of amount of averaged receiver weight above a high threshold (hi_thr) and long time-average activation above an act_thr -- this is generally very beneficial for larger models where hog units are a problem, but not as much for smaller models where the additional constraints are not beneficial -- uses a sigmoidal function: WbInc = 1 / (1 + HiGain*(WbAvg - HiThr) + ActGain * (nrn.ActAvg - ActThr)))"`
	Targs  bool    `def:"true" desc:"apply soft bounding to target layers -- appears to be beneficial but still testing"`
	AvgThr float32 `viewif:"On" def:"0.25" desc:"threshold on weight value for inclusion into the weight average that is then subject to the further HiThr threshold for then driving a change in weight balance -- this AvgThr allows only stronger weights to contribute so that weakening of lower weights does not dilute sensitivity to number and strength of strong weights"`
	HiThr  float32 `viewif:"On" def:"0.4" desc:"high threshold on weight average (subject to AvgThr) before it drives changes in weight increase vs. decrease factors"`
	HiGain float32 `viewif:"On" def:"4" desc:"gain multiplier applied to above-HiThr thresholded weight averages -- higher values turn weight increases down more rapidly as the weights become more imbalanced"`
	LoThr  float32 `viewif:"On" def:"0.4" desc:"low threshold on weight average (subject to AvgThr) before it drives changes in weight increase vs. decrease factors"`
	LoGain float32 `viewif:"On" def:"6,0" desc:"gain multiplier applied to below-lo_thr thresholded weight averages -- higher values turn weight increases up more rapidly as the weights become more imbalanced -- generally beneficial but sometimes not -- worth experimenting with either 6 or 0"`
}

func (wb *WtBalParams) Update() {
}

func (wb *WtBalParams) Defaults() {
	wb.On = false
	wb.Targs = true
	wb.AvgThr = 0.25
	wb.HiThr = 0.4
	wb.HiGain = 4
	wb.LoThr = 0.4
	wb.LoGain = 6
}

// WtBal computes weight balance factors for increase and decrease based on extent
// to which weights and average act exceed thresholds
func (wb *WtBalParams) WtBal(wbAvg float32) (fact, inc, dec float32) {
	inc = 1
	dec = 1
	if wbAvg < wb.LoThr {
		if wbAvg < wb.AvgThr {
			wbAvg = wb.AvgThr // prevent extreme low if everyone below thr
		}
		fact = wb.LoGain * (wb.LoThr - wbAvg)
		dec = 1 / (1 + fact)
		inc = 2 - dec
	} else if wbAvg > wb.HiThr {
		fact = wb.HiGain * (wbAvg - wb.HiThr)
		inc = 1 / (1 + fact) // gets sigmoidally small toward 0 as fact gets larger -- is quick acting but saturates -- apply pressure earlier..
		dec = 2 - inc        // as inc goes down, dec goes up..  sum to 2
	}
	return fact, inc, dec
}
