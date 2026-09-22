"use strict";
const state = { projects: [], pid: null, view: null, maxSeq: 1, atSeq: null, mutations: [] };
const $ = (sel) => document.querySelector(sel);
const $$ = (sel) => Array.from(document.querySelectorAll(sel));

function toast(msg, isErr) {
  const t = $("#toast");
  t.textContent = msg;
  t.style.background = isErr ? "#7a1f1f" : "#10243a";
  t.classList.add("show");
  clearTimeout(t._h);
  t._h = setTimeout(() => t.classList.remove("show"), 3200);
}
async function api(path, opts) {
  const res = await fetch(path, opts);
  const text = await res.text();
  let body = text ? JSON.parse(text) : {};
  if (!res.ok) throw new Error(body.error || res.status + " " + res.statusText);
  return body;
}
const get = (p) => api(p);
const post = (p, body, method) => api(p, {
  method: method || "POST",
  headers: { "Content-Type": "application/json" },
  body: body === undefined ? undefined : JSON.stringify(body),
});

async function loadProjects(selectID) {
  const data = await get("/api/projects");
  state.projects = data.projects || [];
  const sel = $("#projectSel");
  sel.innerHTML = "";
  for (const p of state.projects) {
    const o = document.createElement("option");
    o.value = p.id;
    o.textContent = p.name + "  (" + p.id.slice(0, 10) + "…)";
    sel.appendChild(o);
  }
  if (selectID) state.pid = selectID;
  if (!state.pid && state.projects.length) state.pid = state.projects[state.projects.length - 1].id;
  if (state.pid) sel.value = state.pid;
}

async function loadView(seq) {
  if (!state.pid) return;
  const path = seq == null ? `/api/projects/${state.pid}/view` : `/api/projects/${state.pid}/view/${seq}`;
  state.view = await get(path);
  state.atSeq = seq;
  render();
}

async function loadHead() {
  if (!state.pid) return;
  const evs = await get(`/api/projects/${state.pid}/events`);
  state.maxSeq = evs.max_seq || 1;
  const slider = $("#historySlider");
  slider.max = state.maxSeq;
  slider.value = state.maxSeq;
  $("#historyLabel").textContent = "seq " + state.maxSeq + " / " + state.maxSeq + "（当前）";
  $("#viewMode").textContent = "";
  await loadView(null);
}

async function loadMutations() {
  const ids = ["M1_atmosphere_delay","M2_baseline_drift","M3_overlapping_peaks","M4_nonmonotonic_noise","M5_sampling_gap","M6_initial_mass_revision","M7_import_half_failure"];
  state.mutations = [];
  for (const id of ids) {
    try { state.mutations.push(await get("/api/case/mutation/" + id)); } catch (e) {}
  }
  renderMutationButtons();
}

function currentArt() {
  const v = state.view;
  if (!v) return null;
  if (state.atSeq == null) return v.artifacts.find((a) => a.id === v.current_artifact_id) || v.artifacts[v.artifacts.length - 1];
  return v.artifacts.find((a) => a.id === v.current_artifact_id) || v.artifacts[v.artifacts.length - 1];
}

function esc(s) { return String(s == null ? "" : s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c])); }
function fnum(x, d) { return x == null ? "—" : Number(x).toFixed(d == null ? 3 : d); }

function render() {
  const v = state.view;
  if (!v) return;
  const art = currentArt();
  renderFigure(v, art);
  renderLayers(v);
  renderParams(v, art);
  renderCandidates(v, art);
  renderResidue(v, art);
  renderRules(art);
  renderDecisions(v);
  renderEvents(v);
  if (state.atSeq != null) {
    $("#historyLabel").textContent = "seq " + state.atSeq + " / " + state.maxSeq;
    $("#viewMode").textContent = "历史只读视图（不被最新结果覆盖）";
  }
}

async function renderFigure(v, art) {
  const el = $("#figure");
  if (!art || !art.points || !art.points.length) { el.innerHTML = "<p>尚无派生结果，请先导入。</p>"; return; }
  const seq = state.atSeq == null ? "" : "/" + (state.atSeq);
  el.innerHTML = `<img alt="TG/DTG figure" src="/api/projects/${v.project_id}/figure${seq}?t=${Date.now()}"/>`;
}

function renderLayers(v) {
  const tb = $("#layersTable tbody");
  tb.innerHTML = "";
  for (const l of v.layers) {
    const tr = document.createElement("tr");
    tr.innerHTML = `<td>${esc(l.kind)}</td>
      <td class="mono">${l.present ? esc(l.head_rev) : "<span class='tag unknown'>缺失</span>"}</td>
      <td class="mono">${esc(l.parent_rev || "—")}</td>
      <td>${esc(l.source_file || "—")}</td>`;
    tr.style.cursor = "pointer";
    tr.onclick = () => showProv("层 " + l.kind, { head_rev: l.head_rev, parent_rev: l.parent_rev, source_file: l.source_file, immutable: true });
    tb.appendChild(tr);
  }
}

function renderParams(v, art) {
  $("#paramsBox").textContent = JSON.stringify({
    params_rev_id: v.params_rev_id || "(默认)",
    fingerprint: art ? art.params_fingerprint : null,
    algorithm: art ? art.algorithm_version : null,
    params: v.params,
  }, null, 2);
  $("#paramsInput").value = JSON.stringify(v.params, null, 2);
}

function statusTag(c, v) {
  if (c.decision) {
    const cls = c.decision.kind;
    const conflict = c.conflict ? `<span class="tag conflict">冲突 ${esc(c.decision.conflict_of.slice(0, 8))}</span>` : "";
    return `<span class="tag ${cls}">${esc(c.decision.kind)} #${c.decision.seq}</span>${conflict}`;
  }
  if (c.status === "unknown") return `<span class="tag unknown">unknown</span>`;
  return `<span class="tag unknown">${esc(c.status)}</span>`;
}

function renderCandidates(v, art) {
  const box = $("#candidates");
  box.innerHTML = "";
  if (!art) { box.textContent = "无"; return; }
  const staleBanner = art.stale ? `<div class="stale">此结果对当前输入已过期：仍可查看，但不能冒充当前结果。</div>` : "";
  box.insertAdjacentHTML("beforeend", staleBanner);
  for (const c of art.candidates) {
    const div = document.createElement("div");
    div.className = "candrow " + (c.decision && c.decision.kind === "accept" ? "accepted" : c.decision && c.decision.kind === "reject" ? "rejected" : "");
    const gas = c.active_gas ? esc(c.active_gas) : "（阀事件之前）";
    div.innerHTML = `
      <div><b>${esc(c.option)}</b> · 窗口 ${fnum(c.start_s, 0)}–${fnum(c.end_s, 0)}s · 峰 ${fnum(c.peak_s, 0)}s @样品 ${fnum(c.peak_t_sample_c, 1)}°C · 气氛 ${gas} ${statusTag(c, v)}</div>
      <div>失重 ${fnum(c.loss_pct, 3)} % / ${fnum(c.loss_mg, 4)} mg；段末残留 ${fnum(c.residue_end_pct, 3)} %（可重叠，不强制总和100%）</div>
      <div>阀切换 ${c.valve_switch_s == null ? "—" : fnum(c.valve_switch_s, 0) + "s"} → 样品反应 ${c.reaction_onset_s == null ? "—" : fnum(c.reaction_onset_s, 0) + "s"}</div>
      <div class="ids">id=${esc(c.id)} group=${esc(c.group_id)}</div>
      <div class="toolbar">
        <button data-act="accept">接受</button>
        <button data-act="reject" class="danger">否定</button>
        <button data-act="prov">来源/计算链</button>
      </div>`;
    div.querySelector('[data-act=accept]').onclick = () => decide(art, c.id, "accept");
    div.querySelector('[data-act=reject]').onclick = () => decide(art, c.id, "reject");
    div.querySelector('[data-act=prov]').onclick = () => showProv("候选 " + c.option, candidateProv(art, c));
    box.appendChild(div);
  }
}

function candidateProv(art, c) {
  return {
    candidate_id: c.id,
    basis: art.basis,
    algorithm_version: art.algorithm_version,
    params_fingerprint: art.params_fingerprint,
    window: { start_s: c.start_s, end_s: c.end_s, basis: "t_furnace_s" },
    peak: { time_s: c.peak_s, t_sample_c: c.peak_t_sample_c, basis: "T_sample_C (一阶滞后)" },
    atmosphere: { active_gas: c.active_gas, valve_switch_s: c.valve_switch_s, reaction_onset_s: c.reaction_onset_s },
    loss: { pct: c.loss_pct, mg: c.loss_mg, note: "pct 与 mg 均由当前 m0 派生；不重归一化" },
    overlaps_with: c.overlaps_with,
    status: c.status,
    decision: c.decision || null,
  };
}

function renderResidue(v, art) {
  const box = $("#residue");
  if (!art) { box.textContent = "无"; return; }
  const r = art.residue;
  const val = r.residue_pct == null ? "unknown" : fnum(r.residue_pct, 3) + " %";
  box.innerHTML = `<div><b data-id="${esc(r.id)}" class="mono">最终残留率：${val}</b>
    <span class="tag ${r.status === "ok" ? "accept" : "unknown"}">${esc(r.status)}</span>
    <button id="btnChain">展开计算链</button></div><ul class="chain" hidden></ul>`;
  const ul = box.querySelector("ul");
  for (const n of r.chain) {
    const li = document.createElement("li");
    li.innerHTML = `<b>${esc(n.label)}</b> = ${n.value == null ? "—" : fnum(n.value, 5)} ${esc(n.unit)}
      <span class="tag unknown">${esc(n.basis)}</span>
      ${n.status !== "ok" ? `<span class="tag conflict">${esc(n.status)}</span>` : ""}
      ${n.source_rev ? `<div class="ids">来源 ${esc(n.source_kind)} @ ${esc(n.source_rev)}</div>` : ""}
      ${n.formula ? `<div>${esc(n.formula)}</div>` : ""}
      ${(n.diagnostics || []).map((d) => `<div class="stale">${esc(d)}</div>`).join("")}
      <div class="ids">${esc(n.id)}</div>`;
    li.style.cursor = "pointer";
    li.onclick = () => showProv("计算节点 " + n.label, n);
    ul.appendChild(li);
  }
  $("#btnChain").onclick = () => { ul.hidden = !ul.hidden; };
}

function renderRules(art) {
  const ul = $("#rules");
  ul.innerHTML = "";
  if (!art) return;
  for (const j of art.judgements) {
    const li = document.createElement("li");
    li.innerHTML = `<b>${esc(j.code)}</b> [${esc(j.status)}] ${esc(j.message)}
      <div class="ids">${(j.refs || []).map(esc).join(" · ")}</div>`;
    ul.appendChild(li);
  }
}

function renderDecisions(v) {
  const ul = $("#decisionLog");
  ul.innerHTML = "";
  for (const d of v.decision_log) {
    const li = document.createElement("li");
    li.innerHTML = `#${d.seq} <b>${esc(d.kind)}</b> → ${esc(d.target_id)} ${d.undone ? '<span class="tag reject">已撤销</span>' : ""}
      ${d.supersedes ? `<span class="tag unknown">取代 ${esc(d.supersedes.slice(0, 8))}</span>` : ""}
      ${d.conflict_of ? `<span class="tag conflict">与 ${esc(d.conflict_of.slice(0, 8))} 冲突</span>` : ""}
      <div>${esc(d.rationale)}</div><div class="ids">${esc(d.id)}</div>
      ${!d.undone ? `<button data-undo="${esc(d.id)}">撤销</button>` : ""}`;
    const u = li.querySelector("[data-undo]");
    if (u) u.onclick = () => undo(d.id);
    ul.appendChild(li);
  }
}

function renderEvents(v) {
  get(`/api/projects/${v.project_id}/events`).then((d) => {
    const ol = $("#eventList");
    ol.innerHTML = "";
    for (const e of d.events) {
      const li = document.createElement("li");
      li.innerHTML = `#${e.seq} ${esc(e.type)} <span class="ids">${esc(e.at)}</span>`;
      li.style.cursor = "pointer";
      li.title = e.payload;
      li.onclick = () => showProv("事件 #" + e.seq + " " + e.type, JSON.parse(e.payload));
      ol.appendChild(li);
    }
  });
}

async function decide(art, target, kind) {
  const rationale = prompt(`${kind === "accept" ? "接受" : "否定"}该阶段候选。请记录人工依据：`, kind === "accept" ? "复核通过：峰形与气氛窗口一致" : "否定：与其它候选/证据冲突");
  if (rationale == null) return;
  try {
    const d = await post(`/api/projects/${state.pid}/decide`, { artifact_id: art.id, target_id: target, kind, rationale });
    if (d.conflict_of) toast("已记录，但与既有接受决定冲突（已写入冲突历史）");
    else toast("决定已作为新事件写入历史");
    await loadHead();
  } catch (e) { toast(e.message, true); }
}

async function undo(decisionID) {
  try {
    await post(`/api/projects/${state.pid}/undo`, { decision_id: decisionID });
    toast("撤销已写入历史");
    await loadHead();
  } catch (e) { toast(e.message, true); }
}

function renderMutationButtons() {
  const box = $("#mutationBtns");
  box.innerHTML = "";
  for (const m of state.mutations) {
    const b = document.createElement("button");
    b.textContent = m.title + " · " + m.id;
    b.title = m.scenario + "（直接来源：" + m.changes_direct_source + "）";
    b.onclick = () => applyMutation(m);
    box.appendChild(b);
  }
}

async function applyMutation(m) {
  try {
    if (m.layer.kind === "__params__") {
      const cur = state.view.params;
      const override = m.layer.data;
      const merged = Object.assign({}, cur, override);
      delete merged.reason;
      const note = override.reason || m.scenario;
      const r = await post(`/api/projects/${state.pid}/params`, merged);
      toast("参数修订已生成新版本：" + m.id + " — " + note);
    } else {
      // build a one-layer bundle revision against that kind's head
      const layerHead = state.view.layers.find((l) => l.kind === m.layer.kind);
      const bundle = {
        format_version: "tga.bundle/v1",
        name: "mutation:" + m.id,
        layers: [{ kind: m.layer.kind, parent_revision: layerHead ? layerHead.head_rev : "", source_file: "case/mutation/" + m.id + ".json", data: m.layer.data }],
      };
      const r = await post(`/api/projects/${state.pid}/import`, bundle);
      toast(`单源改动 ${m.id}：状态 ${r.status}；诊断 ${r.diagnostics.length} 条（影响范围应限于直接依赖）`);
    }
    await loadHead();
  } catch (e) { toast(e.message, true); }
}

function showProv(title, obj) {
  $("#provTitle").textContent = title + " — 稳定身份 / 来源 / 计算步骤";
  $("#provBody").textContent = typeof obj === "string" ? obj : JSON.stringify(obj, null, 2);
  $("#provDialog").showModal();
}

async function exportProject() {
  const res = await fetch(`/api/projects/${state.pid}/export`);
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = "tga-export-" + state.pid.slice(0, 12) + ".json";
  a.click();
  URL.revokeObjectURL(url);
  state._exported = await blob.text();
  toast("已导出（含原始层版本、事件、历史派生结果与决定）");
}

async function reimportExport(text) {
  const res = await fetch("/api/import-export", { method: "POST", headers: { "Content-Type": "application/json" }, body: text });
  const body = await res.json();
  if (!res.ok) throw new Error(body.error || res.statusText);
  return body;
}

function wire() {
  $("#provClose").onclick = () => $("#provDialog").close();
  $("#btnLoadCase").onclick = async () => {
    try {
      const r = await post("/api/case/load", {});
      toast("离线案例已导入：" + r.project_id.slice(0, 10));
      await loadProjects(r.project_id);
      await loadHead();
    } catch (e) { toast(e.message, true); }
  };
  $("#projectSel").onchange = async (e) => { state.pid = e.target.value; await loadHead(); };
  $("#btnRefresh").onclick = () => loadHead();
  $("#btnExport").onclick = exportProject;
  $("#btnDelete").onclick = async () => {
    if (!confirm("删除本地数据库中的该项目？可随后用导出文件再导入验证身份保持。")) return;
    await api(`/api/projects/${state.pid}`, { method: "DELETE" });
    state.pid = null;
    await loadProjects();
    state.view = null;
    render();
    toast("本地项目已删除");
  };
  $("#importFile").onchange = async (e) => {
    const file = e.target.files[0];
    if (!file) return;
    const text = await file.text();
    try {
      const r = await reimportExport(text);
      toast(`再导入完成：${r.restored}，事件 ${r.events}、层版本 ${r.revisions}、派生结果 ${r.artifacts}（身份/顺序保持）`);
      await loadProjects(r.restored);
      await loadHead();
    } catch (err) { toast(err.message, true); }
    e.target.value = "";
  };
  $("#btnReimportCase").onclick = async () => {
    if (!state._exported) { toast("请先点“导出”生成导出文件", true); return; }
    const r = await reimportExport(state._exported);
    toast(`删除后再导入：${r.restored}`);
    await loadProjects(r.restored);
    await loadHead();
  };
  $("#btnParams").onclick = async () => {
    try {
      const p = JSON.parse($("#paramsInput").value);
      await post(`/api/projects/${state.pid}/params`, p);
      toast("参数修订成功（旧派生结果保留为过期）");
      await loadHead();
    } catch (e) { toast(e.message, true); }
  };
  $("#historySlider").oninput = async (e) => {
    const seq = Number(e.target.value);
    $("#historyLabel").textContent = "seq " + seq + " / " + state.maxSeq;
    await loadView(seq);
  };
  $("#btnHead").onclick = loadHead;
}

async function boot() {
  wire();
  await loadMutations();
  await loadProjects();
  if (state.pid) await loadHead();
}
boot();
