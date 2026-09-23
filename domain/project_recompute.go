package domain

// recompute 依据受影响集合做局部失效并重算。
// 已存在的旧派生结果通过 artifactLog 标记为 stale(可查看但非当前),
// 新结果带新的稳定身份。
func (p *Project) recompute(ev *Event, affected []string) {
	isAffected := map[string]bool{}
	for _, a := range affected {
		isAffected[a] = true
	}
	if isAffected["corrected"] {
		cs := BuildCorrectedSeries(p.layers, p.params)
		p.frame.Corrected = cs
		p.frame.CorrectedID = stableID("cs", FingerprintParams(p.params),
			hashText(canonicalRaw(p.layers)))
	}
	if isAffected["dtg"] || isAffected["corrected"] {
		dtg := BuildDTG(p.frame.Corrected, p.params)
		p.frame.DTG = dtg
		p.frame.DTGID = stableID("dtg", p.frame.CorrectedID,
			FingerprintParams(p.params))
	}
	if isAffected["stages"] || isAffected["dtg"] || isAffected["corrected"] {
		fc := BuildFurnaceCurve(p.layers.FurnaceSegs)
		p.frame.Furnace = fc
		deps := DependencyRefs{
			CorrectedSeriesID: p.frame.CorrectedID,
			DTGID:             p.frame.DTGID,
			ParamFingerprint:  FingerprintParams(p.params),
			ParamVersionID:    p.paramVerID,
			SubmissionIDs:     p.currentSubmissionIDs(),
		}
		p.frame.Stages = BuildStages(p.frame.Corrected, p.frame.DTG, fc,
			p.layers.GasEvents, p.params, ReactionTempC, deps)
		p.artifactLog = append(p.artifactLog, ArtifactStatus{
			ID:   stableID("stageset", p.frame.DTGID, FingerprintParams(p.params)),
			Kind: "stages", Current: true, Stale: false,
			ProducedAtSeq: ev.Seq, Viewable: true,
		})
	}
	if isAffected["residue"] || isAffected["dtg"] || isAffected["corrected"] {
		im, ok := ResolveInitialMass(p.layers)
		if ok {
			imSrc := p.layerSource[string(LayerInitialMass)]
			p.frame.Residue = ComputeResidue(p.frame.Corrected, im, imSrc,
				DependencyRefs{
					CorrectedSeriesID: p.frame.CorrectedID,
					ParamFingerprint:  FingerprintParams(p.params),
				})
		}
	}
	// 帧元数据
	p.frame.Params = p.params
	p.frame.ParamFingerprint = FingerprintParams(p.params)
	p.frame.ParamVersionID = p.paramVerID
	p.frame.SubmissionSeq = len(p.Submissions)
	// 汇总诊断
	p.frame.Diagnostics = append(p.frame.Diagnostics[:0:0], p.lastDiags...)
	fr := copyFrame(p.frame)
	ev.Derived = &fr
}

func (p *Project) currentSubmissionIDs() []string {
	var out []string
	seen := map[string]bool{}
	for _, l := range []string{
		string(LayerSamples), string(LayerDrift), string(LayerBlank),
		string(LayerFurnace), string(LayerGas), string(LayerInitialMass),
	} {
		if id := p.layerSource[l]; id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// canonicalRaw 生成当前原始层的稳定文本表示(供内容寻址)。
func canonicalRaw(l LayerData) string {
	var b []byte
	for _, s := range l.Samples {
		b = append(b, []byte("S"+itoa(s.Seq)+":"+s.Time.RawText+":"+
			s.Mass.RawText+":"+s.Temp.RawText+"\n")...)
	}
	for _, a := range l.DriftAnchors {
		b = append(b, []byte("D"+itoa(a.Seq)+":"+a.Time.RawText+":"+a.Offset.RawText+"\n")...)
	}
	for _, bp := range l.Blank {
		b = append(b, []byte("B"+itoa(bp.Seq)+":"+bp.Time.RawText+":"+bp.Mass.RawText+"\n")...)
	}
	for _, g := range l.GasEvents {
		b = append(b, []byte("G"+itoa(g.Seq)+":"+g.RawText+"\n")...)
	}
	for _, f := range l.FurnaceSegs {
		b = append(b, []byte("F"+itoa(f.Seq)+":"+f.Start.RawText+"-"+f.End.RawText+"\n")...)
	}
	for _, m := range l.InitialMasses {
		b = append(b, []byte("M"+itoa(m.Seq)+":"+m.Mass.RawText+":"+m.Basis+"\n")...)
	}
	return string(b)
}

func copyFrame(f DerivedFrame) DerivedFrame {
	return f
}
