"use strict";
const state = {
  projects: [],
  pid: null,
  snap: null,
  manifest: null,
  bundles: {}, // case id -> bundle object
  atSeq: 0,
};

const $ = (id) => document.getElementById(id);
const esc = (s) => String(s ?? "").replace(/[&<>"]/g, (c) =>
  ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
const fmt = (v, d = 4) => (v === null || v === undefined || Number.isNaN(v))
  ? '<span class="muted">unknown</span>' : Number(v).toFixed(d);
const kgToMg = (v) => v === null || v === undefined ? null : v * 1e6;
const kToC = (v) => v === null || v === undefined ? null : v - 273.15;

async function api(path, opts) {
  const res = await fetch(path, opts);
  const text = await res.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch (e) { data = { raw: text }; }
  if (!res.ok) {
    banner((data && data.error) || ("HTTP " + res.status));
    throw new Error(data && data.error);
  }
  return data;
}

function banner(msg) {
  const b = $("banner");
  b.textContent = msg;
  b.hidden = !msg;
  if (msg) setTimeout(() => { b.hidden = true; }, 5000);
}

async function loadProjects(selectId) {
  const data = await api("/api/projects");
  state.projects = data.projects;
  const sel = $("projects");
  sel.innerHTML = "";
  if (!state.projects.length) {
    sel.innerHTML = '<option value="">（无项目）</option>';
    return;
  }
  for (const p of state.projects) {
    const o = document.createElement("option");
    o.value = p.id;
    o.textContent = `${p.name} (${p.events} events)`;
    sel.appendChild(o);
  }
  if (state.pid) sel.value = state.pid;
  else { state.pid = sel.value; }
}

async function loadSnapshot(seq) {
  state.atSeq = seq || 0;
  const suffix = seq ? `/events/${seq}` : "";
  state.snap = await api(`/api/projects/${state.pid}${suffix}`);
  render();
}

async function loadCases() {
  const data = await api("/api/cases");
  state.manifest = data.manifest;
  state.bundles["baseline-v1"] = data.baseline;
  for (const sb of data.bundles) state.bundles[sb.id] = sb.bundle;
  renderCaseBar();
}

function renderCaseBar() {
  const m = state.manifest;
  if (!m) return;
  const bar = $("caseBar");
  bar.innerHTML = "";
  const mk = (id, label, title) => {
    const b = document.createElement("button");
    b.className = "casebtn";
    b.textContent = label;
    b.title = title || "";
    b.onclick = async () => {
      $("importText").value = JSON.stringify(state.bundles[id], null, 2);
      await doImport();
    };
    bar.appendChild(b);
  };
  mk("baseline-v1", "① 导入完整基线（六层）", "samples/drift/furnace/gas/blank/initial_mass");
  m.scenarios.forEach((sc, i) => {
    const idx = ["②", "③", "④", "⑤", "⑥", "⑦", "⑧"][i] || "•";
    mk(sc.id, `${idx} ${sc.title}`, `${sc.description}\n改动的直接来源: ${sc.changed}`);
  });
}

async function doImport() {
  let payload = $("importText").value.trim();
  if (!payload) return banner("导入内容为空");
  $("importStatus").textContent = "导入中…";
  const res = await api(`/api/projects/${state.pid}/import`, {
    method: "POST", headers: { "Content-Type": "application/json" }, body: payload,
  });
  state.snap = res.current;
  const att = res.attempt;
  const parts = att.sections.map((s) => {
    const cls = s.fatal ? "section-bad" : "section-ok";
    return `<span class="${cls}">${esc(s.kind)}: ${s.ok ? "committed" : "REJECTED"}` +
      (s.rows_rejected ? ` (${s.rows_rejected} rejected rows)` : "") + `</span>`;
  });
  $("importStatus").innerHTML =
    (att.partial ? "⚠️ 半失败导入（partial） " : att.ok ? "✓ 导入成功 " : "✗ 导入失败 ") +
    parts.join(" · ");
  render();
}

$("importBtn").onclick = doImport;

$("newProject").onclick = async () => {
  const name = prompt("项目名称：", "TGA review " + new Date().toISOString().slice(0, 10));
  if (!name) return;
  const snap = await api("/api/projects", {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name, seed: 20260922 }),
  });
  state.pid = snap.project_id;
  await loadProjects();
  await loadSnapshot(0);
};

$("projects").onchange = async (e) => {
  state.pid = e.target.value;
  await loadSnapshot(0);
};

$("exportBtn").onclick = async () => {
  const url = `/api/projects/${state.pid}/export`;
  const a = document.createElement("a");
  a.href = url; a.download = "tga-project.json"; a.click();
};

$("importEventsFile").onchange = async (e) => {
  const f = e.target.files[0];
  if (!f) return;
  const text = await f.text();
  const snap = await api("/api/import-events", {
    method: "POST", headers: { "Content-Type": "application/json" }, body: text,
  });
  state.pid = snap.project_id;
  await loadProjects();
  await loadSnapshot(0);
  banner("事件包已导入：身份、顺序与来源关系按日志重放恢复");
};

function render() {
  const snap = state.snap;
  if (!snap) return;
  $("projects").value = snap.project_id;
  $("historyLink").hidden = state.atSeq === 0;
  $("historyLink").href = "#";
  $("historyLink").textContent = state.atSeq
    ? `历史视图 @seq ${state.atSeq}（点击返回当前）` : "";
  $("chart").src = `/api/projects/${snap.project_id}/chart/combined.svg?x=${snap.as_of_seq}`;
  renderRules(snap);
  renderCandidates(snap);
  renderResidues(snap);
  renderParams(snap);
  renderHistory(snap);
}

$("historyLink").onclick = async (e) => {
  e.preventDefault();
  if (state.atSeq) await loadSnapshot(0);
};

function renderRules(snap) {
  const el = $("rules");
  if (!snap.rule_events || !snap.rule_events.length) {
    el.innerHTML = '<p class="muted">尚无机器可读判定事件。</p>';
    return;
  }
  el.innerHTML = snap.rule_events.map((r) =>
    `<div class="rule ${r.severity === "warn" ? "warn" : "rulecode"}">` +
    `<b>${esc(r.code)}</b> · <span class="mono">${esc(r.subject)}</span> — ${esc(r.message)}</div>`
  ).join("");
}

function nodeDecision(snap, id) {
  return (snap.decisions || []).find((d) => d.target_id === id);
}

function renderCandidates(snap) {
  const peakEl = $("peakSets");
  peakEl.innerHTML = "";
  const stageEl = $("stages");
  stageEl.innerHTML = "";
  (snap.peak_sets || []).forEach((set) => {
    const box = document.createElement("div");
    box.className = "hypbox";
    box.innerHTML = `<h3>${esc(set.hypothesis)} · ${esc(set.label)} — ${set.peaks.length} 个 DTG 峰</h3>` +
      `<table><tr><th>#</th><th>t (s)</th><th>T (°C)</th><th>rate (1/s)</th><th>prominence</th><th>node id</th></tr>` +
      set.peaks.map((pk, i) => {
        const id = peakNodeId(snap, set.hypothesis, pk.index, i);
        const dec = nodeDecision(snap, id);
        return `<tr><td>${i + 1}</td><td>${fmt(pk.ts, 1)}</td>` +
          `<td>${fmt(kToC(pk.sample_temperature_k), 1)}</td>` +
          `<td>${fmt(pk.rate, 6)}</td><td>${fmt(pk.prominence, 6)}</td>` +
          `<td class="mono val-id">${esc(id)}</td></tr>` +
          `<tr><td colspan="6">${decisionLine(snap, id, "peak")} ${dec ? "" : decisionButtons(snap, id)}</td></tr>`;
      }).join("") + `</table>` +
      (set.merged_note ? `<p class="muted">${esc(set.merged_note)}</p>` : "");
    peakEl.appendChild(box);
  });

  Object.entries(snap.stages || {}).forEach(([hyp, stages]) => {
    const box = document.createElement("div");
    box.className = "hypbox";
    let rows = stages.map((st) => {
      const id = stageNodeIdReal(snap, hyp, st.index) || ("stage-"+hyp+"-"+st.index);
      const dec = nodeDecision(snap, id);
      const overlap = (st.overlaps_with || []).length
        ? ` · overlaps [${st.overlaps_with.join(",")}]` : "";
      const gap = st.crosses_gap
        ? ` · <span class="tag unknown">crosses gap: ${fmt(st.crosses_gap.from_t, 0)}–${fmt(st.crosses_gap.to_t, 0)}s</span>` : "";
      return `<tr><td>#${st.index}</td>` +
        `<td>${fmt(st.onset_t, 1)}–${fmt(st.end_t, 1)} (peak ${fmt(st.peak_t, 1)})</td>` +
        `<td>${fmt(kToC(st.onset_temp_k?.valueOf()), 0)}–${fmt(kToC(st.end_temp_k?.valueOf()), 0)} °C</td>` +
        `<td>${st.mass_loss_pct == null ? '<span class="muted">unknown</span>' : fmt(st.mass_loss_pct, 2) + "%"}</td>` +
        `<td><span class="tag ${st.status}">${st.status}</span>${overlap}${gap}</td></tr>` +
        `<tr><td colspan="5">${decisionLine(snap, id, "stage")} ${dec ? "" : decisionButtons(snap, id)}</td></tr>`;
    }).join("");
    box.innerHTML = `<h3>stages ${hyp}</h3><table>` +
      `<tr><th>#</th><th>window (s)</th><th>T (°C)</th><th>loss</th><th>status</th></tr>${rows}</table>`;
    stageEl.appendChild(box);
  });

  if (snap.reactions && snap.reactions.length) {
    const box = document.createElement("div");
    box.className = "hypbox";
    box.innerHTML = `<h3>反应窗（阀时间 ≠ 反应时间）</h3><table>` +
      `<tr><th>stage</th><th>valve t</th><th>gas</th><th>reaction window</th><th>peak t</th><th>match</th></tr>` +
      snap.reactions.filter((r) => /air/i.test(r.gas_to)).map((r) =>
        `<tr><td class="mono">${esc(r.stage_key)}</td><td>${fmt(r.valve_t, 0)}s</td>` +
        `<td>${esc(r.gas_from)}→${esc(r.gas_to)}</td>` +
        `<td>${fmt(r.reaction_t_lo, 1)}–${fmt(r.reaction_t_hi, 1)}s</td>` +
        `<td>${fmt(r.observed_peak_t, 1)}s</td>` +
        `<td>${r.peak_matches ? "✓" : '<span class="tag rejected">outside</span>'}</td></tr>`
      ).join("") + `</table>`;
    stageEl.appendChild(box);
  }
}

// ids must match pipeline node ID computation; fetch from nodes list instead
// where possible.
function findNodeId(snap, kind, predicate) {
  const n = (snap.nodes || []).find((x) => x.kind === kind && predicate(x));
  return n ? n.id : null;
}
function peakNodeId(snap, hyp, pointIndex, order) {
  // payload carries hypothesis + peak.index
  const n = (snap.nodes || []).find((x) => {
    if (x.kind !== "peak_candidate") return false;
    let d = {}; try { d = JSON.parse(x.data); } catch (e) {}
    return d.hypothesis === hyp && d.peak && d.peak.index === pointIndex;
  });
  return n ? n.id : `peak-${hyp}-${pointIndex}`;
}
// Actual stage id is content-hashed; look it up from nodes by payload.
function stageNodeIdReal(snap, hyp, idx) {
  const n = (snap.nodes || []).find((x) => {
    if (x.kind !== "stage_candidate") return false;
    let d = {}; try { d = JSON.parse(x.data); } catch (e) {}
    return d.hypothesis === hyp && d.index === idx;
  });
  return n ? n.id : null;
}
function decisionLine(snap, id, kind) {
  const dec = nodeDecision(snap, id);
  if (!dec) return `<span class="muted">未裁定 [${esc(id)}]</span>`;
  const tag = `<span class="tag ${dec.verdict}">${esc(dec.verdict)}</span>`;
  return `${tag} by ${esc(dec.by)} @seq ${dec.event_seq} — ${esc(dec.rationale || "")} ` +
    `<span class="mono muted">${esc(dec.id)}</span>`;
}

function decisionButtons(snap, id) {
  return `<button class="good" onclick="decide('${id}','accepted')">接受</button> ` +
         `<button class="danger" onclick="decide('${id}','rejected')">否定</button> ` +
         `<button class="warn" onclick="decide('${id}','adjusted')">参数调整</button>`;
}

async function decide(targetID, verdict) {
  const by = prompt("审阅者标识：", "reviewer-li") || "reviewer";
  const rationale = prompt("判定依据（将写入历史）：", "") || "";
  let adjustment = null;
  if (verdict === "adjusted") {
    const v = prompt("调整后的 onset 时间（秒，可留空取消）：", "");
    if (v) adjustment = { onset_t: Number(v) };
  }
  const data = await api(`/api/projects/${state.pid}/decide`, {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ by, target_kind: "node", target_id: targetID,
      verdict, rationale, adjustment }),
  });
  if (data.conflict) banner(`已记录冲突 ${data.conflict.id}：需要解决后才会生效`);
  await loadSnapshot(state.atSeq);
}

function renderResidues(snap) {
  const el = $("residues");
  el.innerHTML = "";
  Object.entries(snap.residues || {}).forEach(([hyp, r]) => {
    const id = (snap.nodes || []).find((x) => x.kind === "residue" &&
      (() => { try { return JSON.parse(x.data).hypothesis === hyp; } catch (e) { return false; } })())?.id
      || "residue-" + hyp;
    const dec = nodeDecision(snap, id);
    const valLine = r.value == null
      ? `<span class="tag unknown">unknown</span>`
      : `<b>${fmt(r.percent, 3)} %</b> <span class="muted">(fraction ${fmt(r.value, 5)} ± ${fmt(r.uncertainty, 5)})</span>`;
    const rejectLine = (r.reject_reasons || []).length
      ? `<ul class="muted">${r.reject_reasons.map((x) => `<li>${esc(x)}</li>`).join("")}</ul>` : "";
    const chain = (r.chain || []).map((c) =>
      `<tr><td>${c.order}</td><td>${esc(c.label)}</td>` +
      `<td>${fmt(c.value, 6)} ${esc(c.unit)}</td>` +
      `<td class="mono">${esc(c.path)}${c.node_id ? " @ " + esc(c.node_id) : ""}</td></tr>`
    ).join("");
    const box = document.createElement("div");
    box.className = "hypbox";
    box.innerHTML = `<h3>residue ${hyp} <span class="tag ${r.status}">${r.status}</span> ${valLine}</h3>` +
      rejectLine +
      `<div class="muted">tail ${fmt(r.tail_from_t, 1)}–${fmt(r.tail_to_t, 1)}s · ${r.n_points} measured points · ` +
      `m0 = ${fmt(kgToMg(r.initial_mass_kg), 4)} mg <span class="mono">[${esc(id)}]</span></div>` +
      `<details><summary>展开计算链（每个显示值可溯源）</summary>` +
      `<div class="chain"><table><tr><th>#</th><th>步骤</th><th>值</th><th>路径/节点身份</th></tr>${chain}</table></div></details>` +
      `<div>${decisionLine(snap, id, "residue")}</div>` +
      `<div>${dec ? "" : decisionButtons(snap, id)}</div>`;
    el.appendChild(box);
  });
  // conflicts panel
  const conflicts = snap.conflicts || [];
  if (conflicts.length) {
    const box = document.createElement("div");
    box.className = "hypbox";
    box.innerHTML = `<h3>冲突解决</h3>` + conflicts.map((c) => {
      const da = (snap.decisions || []).find((d) => d.id === c.decision_a);
      const db2 = (snap.decisions || []).find((d) => d.id === c.decision_b);
      const dl = (x) => x ? `${x.verdict} by ${x.by} @seq ${x.event_seq}` : c.decision_a;
      return `<div class="chain"><b class="mono">${esc(c.id)}</b> on ${esc(c.target_id)}<br>` +
        `A: ${esc(dl(da))} <code>${esc(c.decision_a)}</code><br>` +
        `B: ${esc(dl(db2))} <code>${esc(c.decision_b)}</code><br>` +
        (c.winner ? `已解决，winner=<code>${esc(c.winner)}</code>：${esc(c.reason || "")}` :
          `<button onclick="resolveConflict('${c.id}','${c.decision_a}')">采纳 A</button>
             <button onclick="resolveConflict('${c.id}','${c.decision_b}')">采纳 B</button>`) +
        `</div>`;
    }).join("");
    el.appendChild(box);
  }
}

async function resolveConflict(cid, winner) {
  const reason = prompt("冲突解决理由（写入历史）：", "") || "";
  await api(`/api/projects/${state.pid}/conflicts/${cid}/resolve`, {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ winner_id: winner, reason }),
  });
  await loadSnapshot(state.atSeq);
}

const PARAM_FIELDS = [
  ["atmosphere_delay_s", "气氛延迟 s"], ["lag_tau_s", "热滞后 tau s"],
  ["gap_threshold_s", "缺口阈值 s"], ["dtg_window_s", "DTG 窗口 s"],
  ["smooth_window_s", "平滑窗口 s"], ["peak_prominence", "峰显著性"],
  ["peak_min_sep_s", "峰最小间隔 s"], ["min_peak_rate", "最小峰速率"],
  ["residue_tail_s", "残留尾部 s"], ["overlap_tol_s", "重叠容差 s"],
  ["temp_lag_per_k_per_s", "温滞 K/s"],
];

function renderParams(snap) {
  const p = snap.current_params;
  $("paramsInfo").innerHTML =
    `<div>algorithm: <b>${esc(p.algorithm_version)}</b></div>` +
    `<div class="mono">fingerprint: ${esc(snap.param_fingerprint)}</div>` +
    `<div class="mono">current set: ${esc(snap.current_set_id)}</div>`;
  $("paramsForm").innerHTML = '<div class="paramgrid">' + PARAM_FIELDS.map(([k, label]) =>
    `<label>${label}<input type="number" step="any" id="p_${k}" value="${p[k] ?? ""}"></label>`
  ).join("") + "</div>";
}

$("reviseParamsBtn").onclick = async () => {
  const base = state.snap.current_params;
  for (const [k] of PARAM_FIELDS) {
    const v = $("p_" + k).value;
    if (v !== "") base[k] = Number(v);
  }
  const reason = prompt("参数修订原因（新版本保留父版本）：", "") || "parameter revision";
  const snap = await api(`/api/projects/${state.pid}/params`, {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ params: base, reason }),
  });
  state.snap = snap;
  render();
  banner("参数修订已提交：仅依赖新参数的节点取得新身份");
};

function renderHistory(snap) {
  const el = $("history");
  const undone = new Set((snap.undos || []).map((u) => u.undone_seq));
  el.innerHTML = (snap.events || []).map((ev) => {
    let detail = "";
    try {
      const p = JSON.parse(ev.payload);
      if (ev.type === "committed") detail = `${p.kind} v${p.version}${p.parent_id ? " (parent " + p.parent_id.slice(0, 14) + "…)" : ""}`;
      if (ev.type === "decision") detail = `${p.verdict} ${p.target_id} by ${p.by}`;
      if (ev.type === "param_revised") detail = `params ${p.fingerpoint || p.fingerprint}`;
      if (ev.type === "import_attempted") detail = (p.partial ? "PARTIAL " : p.ok ? "OK " : "FAILED ") + (p.sections || []).map((s) => s.kind).join(",");
      if (ev.type === "derived") detail = `set ${(p.set_id || "").slice(0, 20)}… (${(p.node_ids || []).length} nodes)`;
      if (ev.type === "conflict") detail = `conflict ${p.id} on ${p.target_id}`;
      if (ev.type === "resolved") detail = `resolve → ${p.winner}`;
      if (ev.type === "undid") detail = `undo seq ${p.undone_seq} (${p.reason || ""})`;
      if (ev.type === "project_created") detail = p.name;
    } catch (e) {}
    const isUndone = ev.type === "decision" && undone.has(ev.seq);
    const undoBtn = ev.type === "decision" && !isUndone && state.atSeq === 0
      ? ` <button class="warn" onclick="undoDecision(${ev.seq})">撤销</button>` : "";
    return `<div class="ev ${isUndone ? "undone" : ""}" onclick="travel(${ev.seq})">` +
      `<b>#${ev.seq}</b> ${esc(ev.type)} <span class="muted">${esc(ev.at)}</span><br>` +
      `<span class="mono">${esc(detail)}</span> ${undoBtn}</div>`;
  }).join("");
}

async function travel(seq) {
  await loadSnapshot(seq);
  banner(`已回到事件 #${seq}：显示当时的依赖版本与结论（只读历史视图）`);
}

async function undoDecision(seq) {
  const reason = prompt("撤销原因：", "") || "undone";
  await api(`/api/projects/${state.pid}/undo`, {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ decision_seq: seq, reason }),
  });
  await loadSnapshot(0);
}

(async function init() {
  await loadProjects();
  await loadCases();
  if (state.pid) await loadSnapshot(0);
})();
