// Copyright (c) 2019, The Emergent Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package axon

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/emer/emergent/emer"
	"github.com/emer/emergent/prjn"
	"github.com/emer/emergent/ringidx"
	"github.com/emer/emergent/weights"
	"github.com/emer/etable/etensor"
	"github.com/goki/ki/indent"
	"github.com/goki/ki/kit"
	"github.com/goki/mat32"
)

// axon.Prjn is a basic Axon projection with synaptic learning parameters
type Prjn struct {
	PrjnStru
	Com       SynComParams    `view:"inline" desc:"synaptic communication parameters: delay, probability of failure"`
	PrjnScale PrjnScaleParams `view:"inline" desc:"projection scaling parameters: modulates overall strength of projection, using both absolute and relative factors, with adaptation option to maintain target max conductances"`
	SWt       SWtParams       `view:"add-fields" desc:"slowly adapting structural weight value parameters, which control initial weight values and slower outer-loop adjustments, to differentiate."`
	Learn     LearnSynParams  `view:"add-fields" desc:"synaptic-level learning parameters for learning in the fast LWt values."`
	Syns      []Synapse       `desc:"synaptic state values, ordered by the sending layer units which owns them -- one-to-one with SConIdx array"`

	// misc state variables below:
	GScale   GScaleVals  `view:"inline" desc:"conductance scaling values"`
	SWtMeans []float32   `desc:"for each recv neuron, adapted target SWt mean value for this projection -- adapted by deviations from TrgAvg activity levels for each neuron.  Initialized based on SWt param settings."`
	Gidx     ringidx.FIx `inactive:"+" desc:"ring (circular) index for Gbuf buffer of synaptically delayed conductance increments.  The current time is always at the zero index, which is read and then shifted.  Len is delay+1."`
	Gbuf     []float32   `desc:"conductance ring buffer for each neuron * Gidx.Len, accessed through Gidx, and length Gidx.Len in size per neuron -- weights are added with conductance delay offsets."`
}

var KiT_Prjn = kit.Types.AddType(&Prjn{}, PrjnProps)

// AsAxon returns this prjn as a axon.Prjn -- all derived prjns must redefine
// this to return the base Prjn type, so that the AxonPrjn interface does not
// need to include accessors to all the basic stuff.
func (pj *Prjn) AsAxon() *Prjn {
	return pj
}

func (pj *Prjn) Defaults() {
	pj.Com.Defaults()
	pj.SWt.Defaults()
	pj.PrjnScale.Defaults()
	pj.Learn.Defaults()
}

// UpdateParams updates all params given any changes that might have been made to individual values
func (pj *Prjn) UpdateParams() {
	pj.Com.Update()
	pj.PrjnScale.Update()
	pj.SWt.Update()
	pj.Learn.Update()
	pj.Learn.LrateInit = pj.Learn.Lrate
}

// GScaleVals holds the conductance scaling and associated values needed for adapting scale
type GScaleVals struct {
	Scale     float32 `inactive:"+" desc:"scaling factor for integrating synaptic input conductances (G's), originally computed as a function of sending layer activity and number of connections, and typically adapted from there -- see Prjn.PrjnScale adapt params"`
	Orig      float32 `inactive:"+" desc:"original scaling factor computed based on initial layer activity, without any subsequent adaptation"`
	Rel       float32 `inactive:"+" desc:"normalized relative proportion of total receiving conductance for this projection: PrjnScale.Rel / sum(PrjnScale.Rel across relevant prjns)"`
	AvgMaxRel float32 `inactive:"+" desc:"actual relative contribution of this projection based on AvgMax values -- used for driving adaptation to maintain target relative values"`
	Err       float32 `inactive:"+" desc:"error that drove last adjustment in scale"`
	Avg       float32 `inactive:"+" desc:"average G value on this trial"`
	Max       float32 `inactive:"+" desc:"maximum G value on this trial"`
	AvgAvg    float32 `inactive:"+" desc:"running average of the Avg, integrated at ly.Act.Dt.TrlAvgTau"`
	AvgMax    float32 `inactive:"+" desc:"running average of the Max, integrated at ly.Act.Dt.TrlAvgTau -- used for computing AvgMaxRel, for adapting Scale"`
}

// Init completes the initialization of values based on initially computed ones
func (gs *GScaleVals) Init() {
	gs.Orig = gs.Scale
	gs.AvgMaxRel = gs.Rel
	gs.Err = 0
	gs.Avg = 0
	gs.Max = 0
	gs.AvgAvg = 0 // 0 = use first
	gs.AvgMax = 0
}

func (pj *Prjn) SetClass(cls string) emer.Prjn         { pj.Cls = cls; return pj }
func (pj *Prjn) SetPattern(pat prjn.Pattern) emer.Prjn { pj.Pat = pat; return pj }
func (pj *Prjn) SetType(typ emer.PrjnType) emer.Prjn   { pj.Typ = typ; return pj }

// AllParams returns a listing of all parameters in the Layer
func (pj *Prjn) AllParams() string {
	str := "///////////////////////////////////////////////////\nPrjn: " + pj.Name() + "\n"
	b, _ := json.MarshalIndent(&pj.Com, "", " ")
	str += "Com: {\n " + JsonToParams(b)
	b, _ = json.MarshalIndent(&pj.SWt, "", " ")
	str += "SWt: {\n " + JsonToParams(b)
	b, _ = json.MarshalIndent(&pj.PrjnScale, "", " ")
	str += "PrjnScale: {\n " + JsonToParams(b)
	b, _ = json.MarshalIndent(&pj.Learn, "", " ")
	str += "Learn: {\n " + strings.Replace(JsonToParams(b), " XCal: {", "\n  XCal: {", -1)
	return str
}

func (pj *Prjn) SynVarNames() []string {
	return SynapseVars
}

// SynVarProps returns properties for variables
func (pj *Prjn) SynVarProps() map[string]string {
	return SynapseVarProps
}

// SynIdx returns the index of the synapse between given send, recv unit indexes
// (1D, flat indexes). Returns -1 if synapse not found between these two neurons.
// Requires searching within connections for receiving unit.
func (pj *Prjn) SynIdx(sidx, ridx int) int {
	nc := int(pj.SConN[sidx])
	st := int(pj.SConIdxSt[sidx])
	for ci := 0; ci < nc; ci++ {
		ri := int(pj.SConIdx[st+ci])
		if ri != ridx {
			continue
		}
		return int(st + ci)
	}
	return -1
}

// SynVarIdx returns the index of given variable within the synapse,
// according to *this prjn's* SynVarNames() list (using a map to lookup index),
// or -1 and error message if not found.
func (pj *Prjn) SynVarIdx(varNm string) (int, error) {
	return SynapseVarByName(varNm)
}

// SynVarNum returns the number of synapse-level variables
// for this prjn.  This is needed for extending indexes in derived types.
func (pj *Prjn) SynVarNum() int {
	return len(SynapseVars)
}

// SynVal1D returns value of given variable index (from SynVarIdx) on given SynIdx.
// Returns NaN on invalid index.
// This is the core synapse var access method used by other methods,
// so it is the only one that needs to be updated for derived layer types.
func (pj *Prjn) SynVal1D(varIdx int, synIdx int) float32 {
	if synIdx < 0 || synIdx >= len(pj.Syns) {
		return mat32.NaN()
	}
	if varIdx < 0 || varIdx >= pj.SynVarNum() {
		return mat32.NaN()
	}
	sy := &pj.Syns[synIdx]
	return sy.VarByIndex(varIdx)
}

// SynVals sets values of given variable name for each synapse, using the natural ordering
// of the synapses (sender based for Axon),
// into given float32 slice (only resized if not big enough).
// Returns error on invalid var name.
func (pj *Prjn) SynVals(vals *[]float32, varNm string) error {
	vidx, err := pj.AxonPrj.SynVarIdx(varNm)
	if err != nil {
		return err
	}
	ns := len(pj.Syns)
	if *vals == nil || cap(*vals) < ns {
		*vals = make([]float32, ns)
	} else if len(*vals) < ns {
		*vals = (*vals)[0:ns]
	}
	for i := range pj.Syns {
		(*vals)[i] = pj.AxonPrj.SynVal1D(vidx, i)
	}
	return nil
}

// SynVal returns value of given variable name on the synapse
// between given send, recv unit indexes (1D, flat indexes).
// Returns mat32.NaN() for access errors (see SynValTry for error message)
func (pj *Prjn) SynVal(varNm string, sidx, ridx int) float32 {
	vidx, err := pj.AxonPrj.SynVarIdx(varNm)
	if err != nil {
		return mat32.NaN()
	}
	synIdx := pj.SynIdx(sidx, ridx)
	return pj.AxonPrj.SynVal1D(vidx, synIdx)
}

// SetSynVal sets value of given variable name on the synapse
// between given send, recv unit indexes (1D, flat indexes)
// returns error for access errors.
func (pj *Prjn) SetSynVal(varNm string, sidx, ridx int, val float32) error {
	vidx, err := pj.AxonPrj.SynVarIdx(varNm)
	if err != nil {
		return err
	}
	synIdx := pj.SynIdx(sidx, ridx)
	if synIdx < 0 || synIdx >= len(pj.Syns) {
		return err
	}
	sy := &pj.Syns[synIdx]
	sy.SetVarByIndex(vidx, val)
	if varNm == "Wt" {
		sy.SWt = sy.Wt
		sy.LWt = 0.5
	}
	return nil
}

///////////////////////////////////////////////////////////////////////
//  Weights File

// WriteWtsJSON writes the weights from this projection from the receiver-side perspective
// in a JSON text format.  We build in the indentation logic to make it much faster and
// more efficient.
func (pj *Prjn) WriteWtsJSON(w io.Writer, depth int) {
	slay := pj.Send.(AxonLayer).AsAxon()
	rlay := pj.Recv.(AxonLayer).AsAxon()
	nr := len(rlay.Neurons)
	w.Write(indent.TabBytes(depth))
	w.Write([]byte("{\n"))
	depth++
	w.Write(indent.TabBytes(depth))
	w.Write([]byte(fmt.Sprintf("\"From\": %q,\n", slay.Name())))
	w.Write(indent.TabBytes(depth))
	w.Write([]byte(fmt.Sprintf("\"MetaData\": {\n")))
	depth++
	w.Write(indent.TabBytes(depth))
	w.Write([]byte(fmt.Sprintf("\"GScale\": \"%g\"\n", pj.GScale.Scale)))
	depth--
	w.Write(indent.TabBytes(depth))
	w.Write([]byte("},\n"))
	w.Write(indent.TabBytes(depth))
	w.Write([]byte(fmt.Sprintf("\"Rs\": [\n")))
	depth++
	for ri := 0; ri < nr; ri++ {
		nc := int(pj.RConN[ri])
		st := int(pj.RConIdxSt[ri])
		w.Write(indent.TabBytes(depth))
		w.Write([]byte("{\n"))
		depth++
		w.Write(indent.TabBytes(depth))
		w.Write([]byte(fmt.Sprintf("\"Ri\": %v,\n", ri)))
		w.Write(indent.TabBytes(depth))
		w.Write([]byte(fmt.Sprintf("\"N\": %v,\n", nc)))
		w.Write(indent.TabBytes(depth))
		w.Write([]byte("\"Si\": [ "))
		for ci := 0; ci < nc; ci++ {
			si := pj.RConIdx[st+ci]
			w.Write([]byte(fmt.Sprintf("%v", si)))
			if ci == nc-1 {
				w.Write([]byte(" "))
			} else {
				w.Write([]byte(", "))
			}
		}
		w.Write([]byte("],\n"))
		w.Write(indent.TabBytes(depth))
		w.Write([]byte("\"Wt\": [ "))
		for ci := 0; ci < nc; ci++ {
			rsi := pj.RSynIdx[st+ci]
			sy := &pj.Syns[rsi]
			w.Write([]byte(strconv.FormatFloat(float64(sy.Wt), 'g', weights.Prec, 32)))
			if ci == nc-1 {
				w.Write([]byte(" "))
			} else {
				w.Write([]byte(", "))
			}
		}
		w.Write([]byte("]\n"))
		depth--
		w.Write(indent.TabBytes(depth))
		if ri == nr-1 {
			w.Write([]byte("}\n"))
		} else {
			w.Write([]byte("},\n"))
		}
	}
	depth--
	w.Write(indent.TabBytes(depth))
	w.Write([]byte("]\n"))
	depth--
	w.Write(indent.TabBytes(depth))
	w.Write([]byte("}")) // note: leave unterminated as outer loop needs to add , or just \n depending
}

// ReadWtsJSON reads the weights from this projection from the receiver-side perspective
// in a JSON text format.  This is for a set of weights that were saved *for one prjn only*
// and is not used for the network-level ReadWtsJSON, which reads into a separate
// structure -- see SetWts method.
func (pj *Prjn) ReadWtsJSON(r io.Reader) error {
	pw, err := weights.PrjnReadJSON(r)
	if err != nil {
		return err // note: already logged
	}
	return pj.SetWts(pw)
}

// SetWts sets the weights for this projection from weights.Prjn decoded values
func (pj *Prjn) SetWts(pw *weights.Prjn) error {
	if pw.MetaData != nil {
		if gs, ok := pw.MetaData["GScale"]; ok {
			pv, _ := strconv.ParseFloat(gs, 32)
			pj.GScale.Scale = float32(pv)
		}
	}
	var err error
	for i := range pw.Rs {
		pr := &pw.Rs[i]
		for si := range pr.Si {
			er := pj.SetSynVal("Wt", pr.Si[si], pr.Ri, pr.Wt[si]) // updates lin wt
			if er != nil {
				err = er
			}
		}
	}
	return err
}

// Build constructs the full connectivity among the layers as specified in this projection.
// Calls PrjnStru.BuildStru and then allocates the synaptic values in Syns accordingly.
func (pj *Prjn) Build() error {
	if err := pj.BuildStru(); err != nil {
		return err
	}
	pj.Syns = make([]Synapse, len(pj.SConIdx))
	rsh := pj.Recv.Shape()
	rlen := rsh.Len()
	pj.Gidx.Len = pj.Com.Delay + 1
	pj.Gidx.Zi = 0
	pj.Gbuf = make([]float32, rlen*pj.Gidx.Len)
	pj.SWtMeans = make([]float32, rlen)
	return nil
}

//////////////////////////////////////////////////////////////////////////////////////
//  Init methods

// SetWtsFunc initializes synaptic Wt value using given function
// based on receiving and sending unit indexes.
// Strongly suggest calling SWtRescale after.
func (pj *Prjn) SetWtsFunc(wtFun func(si, ri int, send, recv *etensor.Shape) float32) {
	rsh := pj.Recv.Shape()
	rn := rsh.Len()
	ssh := pj.Send.Shape()

	for ri := 0; ri < rn; ri++ {
		nc := int(pj.RConN[ri])
		st := int(pj.RConIdxSt[ri])
		for ci := 0; ci < nc; ci++ {
			si := int(pj.RConIdx[st+ci])
			rsi := pj.RSynIdx[st+ci]
			sy := &pj.Syns[rsi]
			wt := wtFun(si, ri, ssh, rsh)
			sy.SWt = wt
			sy.Wt = wt
			sy.LWt = 0.5
		}
	}
}

// InitWtsSyn initializes weight values based on WtInit randomness parameters
// for an individual synapse.
// It also updates the linear weight value based on the sigmoidal weight value.
func (pj *Prjn) InitWtsSyn(syn *Synapse, mean float32) {
	wtv := pj.SWt.Init.RndVar()
	syn.Wt = mean + wtv
	syn.SWt = pj.SWt.ClipSWt(mean + pj.SWt.Init.SPct*wtv)
	rwt := syn.Wt / syn.SWt
	syn.LWt = pj.SWt.LinFmSigWt(rwt) // should preserve current Wt val
	syn.DWt = 0
}

// InitWts initializes weight values according to SWt params,
// enforcing current constraints.
func (pj *Prjn) InitWts() {
	pj.AxonPrj.InitGbuf()
	rlay := pj.Recv.(AxonLayer).AsAxon()
	for ri := range rlay.Neurons {
		nrn := &rlay.Neurons[ri]
		if nrn.IsOff() {
			continue
		}
		smn := pj.SWt.Init.Mean
		pj.SWtMeans[ri] = smn

		nc := int(pj.RConN[ri])
		st := int(pj.RConIdxSt[ri])
		rsidxs := pj.RSynIdx[st : st+nc]

		for ci := range rsidxs {
			rsi := rsidxs[ci]
			sy := &pj.Syns[rsi]
			pj.InitWtsSyn(sy, smn)
		}
	}
	pj.SWtRescale()
}

// SWtRescale rescales the SWt values to preserve the target overall mean value
func (pj *Prjn) SWtRescale() {
	if pj.SWt.Adapt.SubNorm {
		pj.SWtRescaleSub()
	} else {
		pj.SWtRescaleDiv()
	}
}

// SWtRescaleDiv rescales the SWt values to preserve the target overall mean value
// Divisive normalization mode.
func (pj *Prjn) SWtRescaleDiv() {
	rlay := pj.Recv.(AxonLayer).AsAxon()
	for ri := range rlay.Neurons {
		nrn := &rlay.Neurons[ri]
		if nrn.IsOff() {
			continue
		}
		smn := pj.SWtMeans[ri]
		nc := int(pj.RConN[ri])
		st := int(pj.RConIdxSt[ri])
		rsidxs := pj.RSynIdx[st : st+nc]

		var nmin, nmax int
		var sum float32
		for ci := range rsidxs {
			rsi := rsidxs[ci]
			swt := pj.Syns[rsi].SWt
			sum += swt
			if swt <= pj.SWt.Limit.SWt.Min {
				nmin++
			} else if swt >= pj.SWt.Limit.SWt.Max {
				nmax++
			}
		}
		if nc <= 1 {
			continue
		}
		amn := sum / float32(nc)
		mdf := smn / amn // divisive
		if mdf == 1 {
			continue
		}
		if mdf > 1 { // need to increase
			if nmax > 0 && nmax < nc {
				amn = sum / float32(nc-nmax)
				mdf = smn / amn
			}
			for ci := range rsidxs {
				rsi := rsidxs[ci]
				sy := &pj.Syns[rsi]
				if sy.SWt <= pj.SWt.Limit.SWt.Max {
					sy.SWt = pj.SWt.ClipSWt(sy.SWt * mdf)
					sy.Wt = pj.SWt.WtVal(sy.SWt, sy.LWt)
				}
			}
		} else {
			if nmin > 0 && nmin < nc {
				amn = sum / float32(nc-nmin)
				mdf = smn / amn
			}
			for ci := range rsidxs {
				rsi := rsidxs[ci]
				sy := &pj.Syns[rsi]
				if sy.SWt >= pj.SWt.Limit.SWt.Min {
					sy.SWt = pj.SWt.ClipSWt(sy.SWt * mdf)
					sy.Wt = pj.SWt.WtVal(sy.SWt, sy.LWt)
				}
			}
		}
	}
}

// SWtRescaleSub rescales the SWt values to preserve the target overall mean value
// Subtractive normalization mode.
func (pj *Prjn) SWtRescaleSub() {
	rlay := pj.Recv.(AxonLayer).AsAxon()
	for ri := range rlay.Neurons {
		nrn := &rlay.Neurons[ri]
		if nrn.IsOff() {
			continue
		}
		smn := pj.SWtMeans[ri]
		nc := int(pj.RConN[ri])
		st := int(pj.RConIdxSt[ri])
		rsidxs := pj.RSynIdx[st : st+nc]

		var nmin, nmax int
		var sum float32
		for ci := range rsidxs {
			rsi := rsidxs[ci]
			swt := pj.Syns[rsi].SWt
			sum += swt
			if swt <= pj.SWt.Limit.SWt.Min {
				nmin++
			} else if swt >= pj.SWt.Limit.SWt.Max {
				nmax++
			}
		}
		if nc <= 1 {
			continue
		}
		amn := sum / float32(nc)
		mdf := smn - amn // subtractive
		if mdf == 0 {
			continue
		}
		if mdf > 0 { // need to increase
			if nmax > 0 && nmax < nc {
				amn = sum / float32(nc-nmax)
				mdf = smn - amn
			}
			for ci := range rsidxs {
				rsi := rsidxs[ci]
				sy := &pj.Syns[rsi]
				if sy.SWt <= pj.SWt.Limit.SWt.Max {
					sy.SWt = pj.SWt.ClipSWt(sy.SWt + mdf)
					sy.Wt = pj.SWt.WtVal(sy.SWt, sy.LWt)
				}
			}
		} else {
			if nmin > 0 && nmin < nc {
				amn = sum / float32(nc-nmin)
				mdf = smn - amn
			}
			for ci := range rsidxs {
				rsi := rsidxs[ci]
				sy := &pj.Syns[rsi]
				if sy.SWt >= pj.SWt.Limit.SWt.Min {
					sy.SWt = pj.SWt.ClipSWt(sy.SWt + mdf)
					sy.Wt = pj.SWt.WtVal(sy.SWt, sy.LWt)
				}
			}
		}
	}
}

// InitWtSym initializes weight symmetry -- is given the reciprocal projection where
// the Send and Recv layers are reversed.
func (pj *Prjn) InitWtSym(rpjp AxonPrjn) {
	rpj := rpjp.AsAxon()
	slay := pj.Send.(AxonLayer).AsAxon()
	ns := int32(len(slay.Neurons))
	for si := int32(0); si < ns; si++ {
		nc := pj.SConN[si]
		st := pj.SConIdxSt[si]
		for ci := int32(0); ci < nc; ci++ {
			sy := &pj.Syns[st+ci]
			ri := pj.SConIdx[st+ci]
			// now we need to find the reciprocal synapse on rpj!
			// look in ri for sending connections
			rsi := ri
			if len(rpj.SConN) == 0 {
				continue
			}
			rsnc := rpj.SConN[rsi]
			if rsnc == 0 {
				continue
			}
			rsst := rpj.SConIdxSt[rsi]
			rist := rpj.SConIdx[rsst]        // starting index in recv prjn
			ried := rpj.SConIdx[rsst+rsnc-1] // ending index
			if si < rist || si > ried {      // fast reject -- prjns are always in order!
				continue
			}
			// start at index proportional to si relative to rist
			up := int32(0)
			if ried > rist {
				up = int32(float32(rsnc) * float32(si-rist) / float32(ried-rist))
			}
			dn := up - 1

			for {
				doing := false
				if up < rsnc {
					doing = true
					rrii := rsst + up
					rri := rpj.SConIdx[rrii]
					if rri == si {
						rsy := &rpj.Syns[rrii]
						rsy.Wt = sy.Wt
						rsy.LWt = sy.LWt
						rsy.SWt = sy.SWt
						// note: if we support SymFmTop then can have option to go other way
						break
					}
					up++
				}
				if dn >= 0 {
					doing = true
					rrii := rsst + dn
					rri := rpj.SConIdx[rrii]
					if rri == si {
						rsy := &rpj.Syns[rrii]
						rsy.Wt = sy.Wt
						rsy.LWt = sy.LWt
						rsy.SWt = sy.SWt
						// note: if we support SymFmTop then can have option to go other way
						break
					}
					dn--
				}
				if !doing {
					break
				}
			}
		}
	}
}

// InitGbuf initializes the G buffer values to 0
func (pj *Prjn) InitGbuf() {
	for ri := range pj.Gbuf {
		pj.Gbuf[ri] = 0
	}
}

//////////////////////////////////////////////////////////////////////////////////////
//  Act methods

// SendSpike sends a spike from sending neuron index si,
// to add to buffer on receivers.
func (pj *Prjn) SendSpike(si int) {
	sc := pj.GScale.Scale
	del := pj.Com.Delay
	sz := del + 1
	di := pj.Gidx.Idx(del) // index in buffer to put new values -- end of line
	nc := pj.SConN[si]
	st := pj.SConIdxSt[si]
	syns := pj.Syns[st : st+nc]
	scons := pj.SConIdx[st : st+nc]
	for ci := range syns {
		ri := scons[ci]
		pj.Gbuf[int(ri)*sz+di] += sc * syns[ci].Wt // todo: extra mult here -- premultiply is better
	}
}

// RecvGInc increments the receiver's GeRaw or GiRaw from that of all the projections.
func (pj *Prjn) RecvGInc(ltime *Time) {
	if ltime.PlusPhase {
		pj.RecvGIncNoStats()
	} else {
		pj.RecvGIncStats()
	}
}

// RecvGIncStats is called every cycle during minus phase,
// to increment GeRaw or GiRaw, and also collect stats about conductances.
func (pj *Prjn) RecvGIncStats() {
	rlay := pj.Recv.(AxonLayer).AsAxon()
	del := pj.Com.Delay
	sz := del + 1
	zi := pj.Gidx.Zi
	var max, avg float32
	var n int
	if pj.Typ == emer.Inhib {
		for ri := range rlay.Neurons {
			bi := ri*sz + zi
			rn := &rlay.Neurons[ri]
			g := pj.Gbuf[bi]
			rn.GiRaw += g
			pj.Gbuf[bi] = 0
			if g > max {
				max = g
			}
			if g > 0 {
				avg += g
				n++
			}
		}
	} else {
		for ri := range rlay.Neurons {
			bi := ri*sz + zi
			rn := &rlay.Neurons[ri]
			g := pj.Gbuf[bi]
			rn.GeRaw += g
			pj.Gbuf[bi] = 0
			if g > max {
				max = g
			}
			if g > 0 {
				avg += g
				n++
			}
		}
	}
	if n > 0 {
		avg /= float32(n)
		pj.GScale.Avg = avg
		if pj.GScale.AvgAvg == 0 {
			pj.GScale.AvgAvg = avg
		} else {
			pj.GScale.AvgAvg += pj.PrjnScale.AvgDt * (avg - pj.GScale.AvgAvg)
		}
		pj.GScale.Max = max
		if pj.GScale.AvgMax == 0 {
			pj.GScale.AvgMax = max
		} else {
			pj.GScale.AvgMax += pj.PrjnScale.AvgDt * (max - pj.GScale.AvgMax)
		}
	}
	pj.Gidx.Shift(1) // rotate buffer
}

// RecvGIncNoStats is plus-phase version without stats
func (pj *Prjn) RecvGIncNoStats() {
	rlay := pj.Recv.(AxonLayer).AsAxon()
	del := pj.Com.Delay
	sz := del + 1
	zi := pj.Gidx.Zi
	if pj.Typ == emer.Inhib {
		for ri := range rlay.Neurons {
			bi := ri*sz + zi
			rn := &rlay.Neurons[ri]
			g := pj.Gbuf[bi]
			rn.GiRaw += g
			pj.Gbuf[bi] = 0
		}
	} else {
		for ri := range rlay.Neurons {
			bi := ri*sz + zi
			rn := &rlay.Neurons[ri]
			g := pj.Gbuf[bi]
			rn.GeRaw += g
			pj.Gbuf[bi] = 0
		}
	}
	pj.Gidx.Shift(1) // rotate buffer
}

//////////////////////////////////////////////////////////////////////////////////////
//  Learn methods

// DWt computes the weight change (learning) -- on sending projections
func (pj *Prjn) DWt() {
	if !pj.Learn.Learn {
		return
	}
	slay := pj.Send.(AxonLayer).AsAxon()
	rlay := pj.Recv.(AxonLayer).AsAxon()
	lr := pj.Learn.Lrate
	for si := range slay.Neurons {
		sn := &slay.Neurons[si]
		if sn.AvgS < pj.Learn.XCal.LrnThr && sn.AvgM < pj.Learn.XCal.LrnThr {
			continue
		}
		nc := int(pj.SConN[si])
		st := int(pj.SConIdxSt[si])
		syns := pj.Syns[st : st+nc]
		scons := pj.SConIdx[st : st+nc]
		for ci := range syns {
			sy := &syns[ci]
			ri := scons[ci]
			rn := &rlay.Neurons[ri]
			err := pj.Learn.CHLdWt(sn.AvgSLrn, sn.AvgM, rn.AvgSLrn, rn.AvgM)
			// sb immediately -- enters into zero sum
			if err > 0 {
				err *= (1 - sy.LWt)
			} else {
				err *= sy.LWt
			}
			sy.DWt += lr * err
		}
	}
}

// DWtSubMean subtracts a portion of the mean recv DWt per projection
func (pj *Prjn) DWtSubMean() {
	if !pj.Learn.Learn || pj.Learn.XCal.SubMean == 0 {
		return
	}
	rlay := pj.Recv.(AxonLayer).AsAxon()
	if rlay.AxonLay.IsTarget() {
		return
	}
	thr := pj.Learn.XCal.DWtThr * pj.Learn.Lrate
	sm := pj.Learn.XCal.SubMean
	for ri := range rlay.Neurons {
		nc := int(pj.RConN[ri])
		if nc < 1 {
			continue
		}
		st := int(pj.RConIdxSt[ri])
		rsidxs := pj.RSynIdx[st : st+nc]
		sumDWt := float32(0)
		nnz := 0 // non-zero
		for ci := range rsidxs {
			rsi := rsidxs[ci]
			dw := pj.Syns[rsi].DWt
			if dw > thr || dw < -thr {
				sumDWt += dw
				nnz++
			}
		}
		if nnz > 1 {
			sumDWt /= float32(nnz)
			for ci := range rsidxs {
				rsi := rsidxs[ci]
				sy := &pj.Syns[rsi]
				if sy.DWt > thr || sy.DWt < -thr {
					sy.DWt -= sm * sumDWt
				}
			}
		}
	}
}

// WtFmDWt updates the synaptic weight values from delta-weight changes -- on sending projections
func (pj *Prjn) WtFmDWt() {
	if !pj.Learn.Learn {
		return
	}
	for si := range pj.Syns {
		sy := &pj.Syns[si]
		pj.SWt.WtFmDWt(&sy.DWt, &sy.Wt, &sy.LWt, sy.SWt)
		pj.Com.Fail(&sy.Wt)
	}
}

// SlowAdapt does the slow adaptation: SynScale
func (pj *Prjn) SlowAdapt() {
	pj.SWtFmWt()
}

// SWtFmWt updates structural, slowly-adapting SWt value based on current learned weight values
// and updated AvgDif value for difference from TrgAvg target average activation.
func (pj *Prjn) SWtFmWt() {
	if !pj.Learn.Learn {
		return
	}
	rlay := pj.Recv.(AxonLayer).AsAxon()
	if rlay.AxonLay.IsTarget() {
		return
	}
	lr := pj.SWt.Adapt.Lrate
	sb := pj.SWt.Limit.SoftBound
	for ri := range rlay.Neurons {
		nrn := &rlay.Neurons[ri]
		if nrn.IsOff() {
			continue
		}
		dadif := -lr * nrn.AvgDif
		smn := pj.SWtMeans[ri]
		if sb {
			if dadif >= 0 {
				pj.SWtMeans[ri] += (pj.SWt.Limit.Mean.Max - smn) * dadif
			} else {
				pj.SWtMeans[ri] += (smn - pj.SWt.Limit.Mean.Min) * dadif
			}
		} else {
			pj.SWtMeans[ri] = pj.SWt.Limit.Mean.ClipVal(smn + dadif)
		}

		nc := int(pj.RConN[ri])
		st := int(pj.RConIdxSt[ri])
		rsidxs := pj.RSynIdx[st : st+nc]
		for ci := range rsidxs {
			rsi := rsidxs[ci]
			sy := &pj.Syns[rsi]

			dswt := lr * (sy.Wt - sy.SWt)
			if sb {
				if dswt >= 0 {
					sy.SWt += (pj.SWt.Limit.SWt.Max - sy.SWt) * dswt
				} else {
					sy.SWt += (sy.SWt - pj.SWt.Limit.SWt.Min) * dswt
				}
			} else {
				sy.SWt = pj.SWt.ClipSWt(sy.SWt + dswt)
			}
			rwt := sy.Wt / sy.SWt
			sy.LWt = pj.SWt.LinFmSigWt(rwt) // should preserve current Wt val
		}
	}

	pj.SWtRescale()
	// Note: Rescale recomputes weights after rescaling: this actually changes Wt values
	// as function of rescaling changes, using current LWt values
}

// LrateMult sets the new Lrate parameter for Prjns to LrateInit * mult.
// Useful for implementing learning rate schedules.
func (pj *Prjn) LrateMult(mult float32) {
	pj.Learn.Lrate = pj.Learn.LrateInit * mult
}
