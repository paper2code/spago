// Copyright 2019 spaGO Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package radam

import (
	"github.com/nlpodyssey/spago/pkg/mat"
	"github.com/nlpodyssey/spago/pkg/ml/nn"
	"github.com/nlpodyssey/spago/pkg/ml/optimizers/gd"
	"math"
)

var _ gd.MethodConfig = &Config{}

type Config struct {
	gd.MethodConfig
	StepSize float64
	Beta1    float64
	Beta2    float64
	Epsilon  float64
}

func NewConfig(stepSize, beta1, beta2, epsilon float64) Config {
	if !(beta1 >= 0.0 && beta1 < 1.0) {
		panic("adam: `beta1` must be in the range [0.0, 1.0)")
	}
	if !(beta2 >= 0.0 && beta2 < 1.0) {
		panic("adam: `beta2` must be in the range [0.0, 1.0)")
	}
	return Config{
		StepSize: stepSize,
		Beta1:    beta1,
		Beta2:    beta2,
		Epsilon:  epsilon,
	}
}

func NewDefaultConfig() Config {
	return Config{
		StepSize: 0.001,
		Beta1:    0.9,
		Beta2:    0.999,
		Epsilon:  1.0e-8,
	}
}

var _ gd.Method = &RAdam{}

type RAdam struct {
	Config
	RoMax    float64 // The maximum length of the approximated SMA.
	TimeStep int
}

func New(c Config) *RAdam {
	adam := &RAdam{
		Config:   c,
		RoMax:    2.0/(1.0-c.Beta2) - 1.0,
		TimeStep: 1.0,
	}
	return adam
}

func (o *RAdam) Label() int {
	return gd.RAdam
}

const (
	m    int = 0
	v    int = 1
	buf1 int = 2
	buf2 int = 3
	buf3 int = 4
)

func (o *RAdam) NewSupport(r, c int) *nn.Payload {
	supp := make([]mat.Matrix, 5)
	supp[m] = mat.NewEmptyDense(r, c)
	supp[v] = mat.NewEmptyDense(r, c)
	supp[buf1] = mat.NewEmptyDense(r, c)
	supp[buf2] = mat.NewEmptyDense(r, c)
	supp[buf3] = mat.NewEmptyDense(r, c)
	return &nn.Payload{
		Label: o.Label(),
		Data:  supp,
	}
}

func (o *RAdam) IncBatch() {
	o.TimeStep++
}

func (o *RAdam) Delta(param *nn.Param) mat.Matrix {
	return o.calcDelta(param.Grad(), gd.GetOrSetPayload(param, o).Data)
}

func (o *RAdam) calcDelta(grads mat.Matrix, supp []mat.Matrix) mat.Matrix {
	updateM(grads, supp, o.Beta1)
	updateV(grads, supp, o.Beta2)
	sqrtB2T := math.Sqrt(1.0 - math.Pow(o.Beta2, float64(o.TimeStep)))
	alpha := o.calcAlpha()
	buf := supp[v].Sqrt().AddScalarInPlace(o.Epsilon * sqrtB2T)
	defer mat.ReleaseDense(buf.(*mat.Dense))
	suppDiv := supp[m].Div(buf)
	defer mat.ReleaseDense(suppDiv.(*mat.Dense))
	supp[buf3].ProdMatrixScalarInPlace(suppDiv, alpha)
	return supp[buf3]
}

// m = m*beta1 + grads*(1.0-beta1)
func updateM(grads mat.Matrix, supp []mat.Matrix, beta1 float64) {
	supp[m].ProdScalarInPlace(beta1)
	supp[buf1].ProdMatrixScalarInPlace(grads, 1.0-beta1)
	supp[m].AddInPlace(supp[buf1])
}

// v = v*beta2 + (grads*grads)*(1.0-beta2)
func updateV(grads mat.Matrix, supp []mat.Matrix, beta2 float64) {
	supp[v].ProdScalarInPlace(beta2)
	sqGrad := grads.Prod(grads)
	defer mat.ReleaseDense(sqGrad.(*mat.Dense))
	supp[buf2].ProdMatrixScalarInPlace(sqGrad, 1.0-beta2)
	supp[v].AddInPlace(supp[buf2])
}

func (o *RAdam) calcAlpha() float64 {
	timeStep := float64(o.TimeStep)
	b1T := math.Pow(o.Beta1, timeStep)
	b2T := math.Pow(o.Beta2, timeStep)
	ro := o.RoMax - 2.0*timeStep*b2T/(1.0-b2T)
	rect := 1.0
	if ro > 4.0 { // i.e. if the variance is tractable
		rect = math.Sqrt((ro - 4.0) * (ro - 2.0) * o.RoMax / ((o.RoMax - 4.0) * (o.RoMax - 2.0) * ro))
	}
	return o.StepSize * rect * math.Sqrt(1.0-b2T) / (1.0 - b1T)
}
