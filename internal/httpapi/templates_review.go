package httpapi

const reviewHTML = `<!doctype html>
<html lang="zh">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>复核 · {{.Project.Name}}</title>
<link rel="stylesheet" href="/static/app.css"/>
</head>
<body data-project="{{.Project.ID}}">
<header>
  <h1>{{.Project.Name}}</h1>
  <span class="sub">{{.Project.CaseID}} · 事件 {{len .Events}} · 节点 {{.TotalNodes}}（过期 {{.StaleCount}}）· {{.State.ParamVersion}}</span>
  <span style="flex:1"></span>
  <a href="/">← 项目列表</a>
  <button class="secondary" data-act="export">导出归档</button>
</header>
<main>
  <p class="muted"><span id="flash" class="pill"></span></p>

  <div class="card">
    <h2>① TG / DTG（同步时间轴：缺口不插值，峰为 DTG 极小值）</h2>
    {{.Panels.tgdtg}}
  </div>
  <div class="card">
    <h2>② 炉程序 vs 样品实测温度（温度滞后）</h2>
    {{.Panels.furnace}}
  </div>
  <div class="card">
    <h2>③ 气氛时间线（阀切换 ≠ 样品反应时间）</h2>
    {{.Panels.atmosphere}}
  </div>
  <div class="card">
    <h2>④ 阶段候选（可重叠；百分比不强制总和 100%）</h2>
    {{.Panels.stages}}
    <table style="margin-top:10px">
      <tr><th>候选</th><th>窗口 (s)</th><th>峰 T (°C)</th><th>失重 mg</th><th>失重 %</th><th>状态/拒绝原因</th><th>裁定</th></tr>
      {{$state := .State}}
      {{range $c := $state.Report.Candidates}}
      <tr>
        <td><b>{{$c.Key}}</b> <span class="muted">{{$c.Kind}}</span><br/><span class="id">{{$c.ID}}</span></td>
        <td>{{fmtMeasure $c.StartTime}} → {{fmtMeasure $c.EndTime}}</td>
        <td>{{if $c.PeakTemp.Valid}}{{printf "%.1f" (temC $c.PeakTemp.Value)}}{{else}}?{{end}}</td>
        <td>{{fmtMeasure $c.LossMg}}</td>
        <td>{{fmtPct $c.LossPct}}</td>
        <td>
          <span class="pill {{statusClass $c.Status}}">{{$c.Status}}</span>
          {{range $r := $c.RejectReason}}<div class="diag-warn">⚠ {{$r}}</div>{{end}}
          {{range $k := $c.Overlaps}}<div class="muted">overlaps {{$k}}</div>{{end}}
        </td>
        <td>
          <input id="rationale-{{$c.Key}}" placeholder="理由（写入事件）" style="min-width:160px"/>
          <div class="row" style="margin-top:4px">
            <button data-act="rule" data-key="{{$c.Key}}" data-decision="accept">接受</button>
            <button class="danger" data-act="rule" data-key="{{$c.Key}}" data-decision="reject">否定</button>
          </div>
        </td>
      </tr>
      {{end}}
    </table>
  </div>

  <div class="card">
    <h2>⑤ 残留率（每个可展开计算链，稳定身份）</h2>
    {{range $r := $state.Report.Residues}}
    <details>
      <summary>
        <b>{{$r.Label}}</b> — {{fmtPct $r.Percent}}
        ({{fmtMeasure $r.MassCorr}} @ {{fmtMeasure $r.At}}s, {{if $r.Temp.Valid}}{{printf "%.1f" (temC $r.Temp.Value)}} °C{{else}}?{{end}})
        {{if $r.Unknown}}<span class="pill warn">unknown</span>{{end}}
        <span class="id">{{$r.ID}}</span>
      </summary>
      <table>
        <tr><th>#</th><th>步骤</th><th>值</th><th>公式</th><th>直接来源</th></tr>
        {{range $ch := $r.Chain}}
        <tr>
          <td>{{$ch.Step}}</td><td>{{$ch.Label}}</td>
          <td>{{fmtMeasure $ch.Value}} {{if $ch.Value.Note}}<div class="muted">{{$ch.Value.Note}}</div>{{end}}</td>
          <td class="muted">{{$ch.Formula}}</td>
          <td>{{range $in := $ch.Inputs}}<div class="id">{{$in}}</div>{{end}}</td>
        </tr>
        {{end}}
      </table>
    </details>
    {{end}}
  </div>

  <div class="card">
    <h2>⑥ 分层来源（原始输入不可变；修订以父版本表达）</h2>
    <div class="kv">
    {{range $name, $ls := $state.Layers}}
      <div>{{$name}}</div>
      <div>
        {{if $ls.Current}}
          <span class="pill ok">current</span> <span class="id">{{$ls.Current.SourceID}}</span>
          <span class="muted">“{{$ls.Current.Label}}”</span>
          {{range $d := $ls.Current.Diagnostics}}<div class="diag-{{diagClass $d.Severity}}">[{{$d.Code}}] {{$d.Message}}</div>{{end}}
          {{if $ls.Replaced}}<div class="muted">被取代版本: {{range $r := $ls.Replaced}}<span class="id">{{$r}} </span>{{end}}</div>{{end}}
        {{else}}
          <span class="pill err">无已接受版本</span>
        {{end}}
      </div>
    {{end}}
    </div>
  </div>

  <div class="card">
    <h2>⑦ 导入 / 参数修订（六分层材料；半失败片段保留诊断）</h2>
    <details><summary>分层/混合导入（支持 # layer 分段文本）</summary>
      <form onsubmit="event.preventDefault();TGA.submitBundle(this)" style="margin:8px 0">
        <textarea name="text" rows="8" placeholder="# raw_samples&#10;time_s temp_C mass_mg&#10;..."></textarea>
        <div class="row" style="margin-top:6px"><button type="submit">提交导入（每层一个事件）</button></div>
      </form>
      <form onsubmit="event.preventDefault();TGA.submitLayer(this)" class="row" style="margin:8px 0">
        <select name="layer" style="width:200px">
          <option value="raw_samples">raw_samples</option>
          <option value="drift_correction">drift_correction</option>
          <option value="furnace_program">furnace_program</option>
          <option value="gas_valve_events">gas_valve_events</option>
          <option value="blank_curve">blank_curve</option>
          <option value="initial_mass">initial_mass</option>
        </select>
        <input name="text" placeholder="单层材料文本（新版本自动保留父版本链）" style="flex:1"/>
        <button type="submit" class="secondary">单层提交/修订</button>
      </form>
    </details>
    <details><summary>参数修订（新版本 + 父版本；局部输入变化只使相关结果过期）</summary>
      <form onsubmit="event.preventDefault();TGA.reviseParams(this)" style="margin:8px 0">
        <div class="row">
          <label>阀延迟 s<input data-param="ValveDelayS" type="number" step="1" value="{{$state.Params.ValveDelayS}}"/></label>
          <label>参考温度滞后 K<input data-param="ReferenceTempLagK" type="number" step="0.5" value="{{$state.Params.ReferenceTempLagK}}"/></label>
          <label>DTG 半窗<input data-param="DTGHalfWindow" type="number" value="{{$state.Params.DTGHalfWindow}}"/></label>
          <label>峰显著性 mg/s<input data-param="PeakProminenceMgS" type="number" step="0.0001" value="{{$state.Params.PeakProminenceMgS}}"/></label>
          <label>最小峰距 s<input data-param="MinPeakSeparationS" type="number" value="{{$state.Params.MinPeakSeparationS}}"/></label>
          <label>噪声阈值 mg/s<input data-param="NoiseThresholdMgS" type="number" step="0.0001" value="{{$state.Params.NoiseThresholdMgS}}"/></label>
          <label>阶段裕量 s<input data-param="StageMarginS" type="number" value="{{$state.Params.StageMarginS}}"/></label>
        </div>
        <input name="reason" placeholder="修订理由（写入事件）" style="margin:8px 0"/>
        <button type="submit">提交参数新版本</button>
      </form>
      <div class="muted">算法版本：{{$state.Report.AlgorithmVersion}} · 参数指纹：<span class="id">{{$state.ParamVersion}}</span></div>
    </details>
  </div>

  <div class="card">
    <h2>⑧ 裁定、冲突与撤销（只追加；回到旧事件恢复当时的结论）</h2>
    <table>
      <tr><th>裁定 id</th><th>候选</th><th>决定</th><th>状态</th><th>理由/冲突</th><th></th></tr>
      {{range $id, $rec := $state.Rulings}}
      <tr>
        <td class="id">{{$id}}</td>
        <td class="id">{{$rec.Payload.CandidateID}}</td>
        <td>{{$rec.Payload.Decision}}</td>
        <td>{{if $rec.Active}}<span class="pill ok">active</span>{{else}}<span class="pill warn">inactive{{if $rec.UndoneBy}} (undone){{end}}</span>{{end}}</td>
        <td>{{$rec.Payload.Rationale}}{{range $c := $rec.ConflictIDs}}<div class="diag-warn">conflict: <span class="id">{{$c}}</span></div>{{end}}</td>
        <td>{{if $rec.Active}}<button class="secondary" data-act="undo" data-ruling="{{$id}}">撤销</button>{{end}}</td>
      </tr>
      {{else}}<tr><td colspan="6" class="muted">尚无裁定。</td></tr>{{end}}
    </table>
  </div>

  <div class="card">
    <h2>⑨ 事件历史 / 时间旅行（点击任意事件查看当时依赖版本与可见结论）</h2>
    <div class="hist">
      <table>
      {{range $ev := .Events}}
        <tr class="ev">
          <td><b>#{{$ev.Seq}}</b> <a href="/projects/{{$.Project.ID}}?at={{$ev.Seq}}">{{$ev.Type}}</a></td>
          <td class="muted">{{$ev.At}}</td>
          <td class="id">{{$ev.ID}}</td>
        </tr>
      {{end}}
      </table>
    </div>
  </div>

  {{if .State.Diagnostics}}
  <div class="card">
    <h2>⑩ 诊断（无法转换的值留在原位，不为画图丢弃）</h2>
    {{range $d := $state.Diagnostics}}<div class="diag-{{diagClass $d.Severity}}">[{{$d.Severity}} {{$d.Code}} {{$d.Layer}}] {{$d.Message}}{{if $d.Fragment}} <span class="id">fragment: {{$d.Fragment}}</span>{{end}}</div>{{end}}
  </div>
  {{end}}
</main>
<script src="/static/app.js"></script>
</body></html>`
