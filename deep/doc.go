// Copyright (c) 2019, The Emergent Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package deep provides the DeepLeabra variant of Leabra, which performs predictive
learning by attempting to predict the activation states over the Pulvinar nucleus
of the thalamus (in posterior sensory cortex), which are driven phasically every
100 msec by deep layer 5 intrinsic bursting (5IB) neurons that have strong focal
(essentially 1-to-1) connections onto the Pulvinar Thalamic Relay Cell (TRC)
neurons.

This package has 3 specialized Layer types:

* SuperLayer: implements the superficial layer neurons, which function just
  like standard leabra.Layer neurons, while also directly computing the
  Burst activation signal that reflects the deep layer 5IB bursting activation,
  via thresholding of the superficial layer activations
  (Bursting is thought to have a higher threshold).

* CTLayer: implements the layer 6 regular spiking CT corticothalamic neurons
  that project into the thalamus.  They receive the Burst activation via a
  CTCtxtPrjn projection type, typically once every 100 msec, and integrate
  that in the CtxtGe value, which is added to other excitatory conductance
  inputs to drive the overall activation (Act) of these neurons.
  Due to the bursting nature of the Burst inputs, this causes these CT layer
  neurons to reflect what the superficial layers encoded on the *previous*
  timestep -- thus they represent a temporally-delayed context state.

  CTLayer can send Context via self projections to reflect the extensive
  deep-to-deep lateral connectivity that provides more extensive temporal
  context information.

* TRCLayer: implement the TRC (Pulvinar) neurons, upon which the prediction
  generated by CTLayer projections is projected in the minus phase.  This is
  computed via standard Act-driven projections that integrate into standard Ge
  excitatory input in TRC neurons.  The 5IB Burst-driven plus-phase "outcome"
  activation state is driven by direct access to the corresponding driver SuperLayer
  (not via standard projection mechanisms).

Wiring diagram:

  SuperLayer --Burst--> TRCLayer
    |                      ^
 CTCtxt          /- Back -/
   |            /
   v           /
 CTLayer -----/  (typically only for higher->lower)

Timing:

The alpha-cycle quarter(s) when Burst is updated and broadcast is set in
BurstQtr (defaults to Q4, can also be e.g., Q2 and Q4 for beta frequency updating).
During this quarter(s), the Burst value is computed in SuperLayer, and this is
continuously accessed by TRCLayer neurons to drive plus-phase outcome states.

At the *end* of the burst quarter(s), in the QuarterFinal method,
CTCtxt projections convey the Burst signal from Super to CTLayer neurons,
where it is integrated into the Ctxt value representing the temporally-delayed
context information.

*/
package deep
