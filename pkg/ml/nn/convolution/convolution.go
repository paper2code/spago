// Copyright 2019 spaGO Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package convolution

import (
	"io"
	"log"
	"saientist.dev/spago/pkg/mat"
	"saientist.dev/spago/pkg/ml/act"
	"saientist.dev/spago/pkg/ml/ag"
	"saientist.dev/spago/pkg/ml/nn"
	"sync"
)

type Model struct {
	K              []*nn.Param  `type:"weights"`
	B              []*nn.Param  `type:"biases"`
	Act            act.FuncName // output activation
	inputChannels  int
	outputChannels int
	xStride        int
	yStride        int
}

func New(kernelSizeX, kernelSizeY, xStride, yStride, inputChannels, outputChannels int, actFunc act.FuncName) *Model {
	paramsSize := inputChannels * outputChannels
	kernels := make([]*nn.Param, paramsSize, paramsSize)
	biases := make([]*nn.Param, paramsSize, paramsSize)
	for i := 0; i < paramsSize; i++ {
		kernels[i] = nn.NewParam(mat.NewEmptyDense(kernelSizeX, kernelSizeY))
		biases[i] = nn.NewParam(mat.NewEmptyVecDense(1))
	}
	return &Model{
		K:              kernels,
		B:              biases,
		Act:            actFunc,
		inputChannels:  inputChannels,
		outputChannels: outputChannels,
		xStride:        xStride,
		yStride:        yStride,
	}
}

func (m *Model) ForEachParam(callback func(param *nn.Param)) {
	nn.ForEachParam(m, callback)
}

func (m *Model) Serialize(w io.Writer) (int, error) {
	return nn.Serialize(m, w)
}

func (m *Model) Deserialize(r io.Reader) (int, error) {
	return nn.Deserialize(m, r)
}

// SetActivation sets the new activation and returns the previous one.
func (m *Model) SetActivation(a act.FuncName) act.FuncName {
	prev := m.Act
	m.Act = a
	return prev
}

type Processor struct {
	opt                     []interface{}
	model                   *Model
	g                       *ag.Graph
	ConcurrentOutputChannel bool
}

func (m *Model) NewProc(g *ag.Graph, opt ...interface{}) nn.Processor {
	p := &Processor{
		model:                   m,
		opt:                     opt,
		g:                       g,
		ConcurrentOutputChannel: true,
	}
	p.init(opt)
	return p
}

func (p *Processor) Model() nn.Model {
	return p.model
}

func (p *Processor) Graph() *ag.Graph {
	return p.g
}

func (p *Processor) RequiresFullSeq() bool {
	return true
}

func (p *Processor) Reset() {
	p.init(p.opt)
}

type Concurrency struct {
	Value bool
}

func (p *Processor) init(opt []interface{}) {
	for _, t := range opt {
		switch t := t.(type) {
		case Concurrency:
			p.ConcurrentOutputChannel = t.Value
		default:
			log.Fatal("convolution: invalid init options")
		}
	}
}

func (p *Processor) Forward(xs ...ag.Node) []ag.Node {
	if p.ConcurrentOutputChannel && p.model.outputChannels > 1 {
		return p.fwdConcurrent(xs)
	} else {
		return p.fwdSerial(xs)
	}
}

func (p *Processor) fwdSerial(xs []ag.Node) []ag.Node {
	ys := make([]ag.Node, p.model.outputChannels)
	for i := 0; i < p.model.outputChannels; i++ {
		ys[i] = p.forward(xs, i)
	}
	return ys
}

func (p *Processor) fwdConcurrent(xs []ag.Node) []ag.Node {
	ys := make([]ag.Node, p.model.outputChannels)
	var wg sync.WaitGroup
	wg.Add(p.model.outputChannels)
	for i := 0; i < p.model.outputChannels; i++ {
		go func(i int) {
			defer wg.Done()
			ys[i] = p.forward(xs, i)
		}(i)
	}
	wg.Wait()
	return ys
}

func (p *Processor) forward(xs []ag.Node, outputChannel int) ag.Node {
	offset := outputChannel * p.model.inputChannels
	out := nn.Conv2D(p.g, p.g.NewWrap(p.model.K[0+offset]), xs[0], p.model.xStride, p.model.yStride)
	out = p.g.AddScalar(out, p.g.NewWrap(p.model.B[0+offset]))
	for i := 1; i < len(xs); i++ {
		out = p.g.Add(out, nn.Conv2D(p.g, p.g.NewWrap(p.model.K[i+offset]), xs[i], p.model.xStride, p.model.yStride))
		out = p.g.AddScalar(out, p.g.NewWrap(p.model.B[i+offset]))
	}
	return act.F(p.g, p.model.Act, out)
}